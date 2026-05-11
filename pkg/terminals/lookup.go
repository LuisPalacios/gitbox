package terminals

// EXECUTION pillar (issue #72): consult the user's terminal config files
// (wezterm.lua launch_menu, Windows Terminal settings.json) at launch time
// and replace the generic ArgsTemplate with the matched entry's argv (and
// env splice for WezTerm). Falls back transparently to the generic template
// when no match is found, no config file exists, or the terminal isn't
// installed — that's the right behaviour for shells the user hasn't wired
// into their terminal config.

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/LuisPalacios/gitbox/pkg/harness"
	"github.com/LuisPalacios/gitbox/pkg/launch"
)

// LaunchOverride is the user-config-derived launch shape returned by
// LookupForLaunch when a match is found. Argv is a template (still subject
// to {path}/{shell_*}/{command} expansion via launch.ResolveArgs) so the
// caller's existing token-substitution path stays in one place. Env are
// extra environment variables that should be spliced on top of the parent
// env before exec.Start; nil means "no env override".
type LaunchOverride struct {
	Argv []string
	Env  map[string]string
}

// LookupForLaunch finds a user-config entry that matches the given gitbox
// (terminalID, shellID) tuple. shellName is the gitbox shell's display Name
// — used for fuzzy matching against the entry label/profile name (e.g.
// "PowerShell 7" matches gitbox shell "pwsh", "WSL — Ubuntu-24.04" matches
// "wsl-ubuntu-24-04" by suffix-after-em-dash split).
//
// Returns (override, true) when a match is found; (zero, false) otherwise.
// A false result means the caller should keep using its generic ArgsTemplate
// — that's the correct fallback behaviour, not an error.
//
// Bare-shell DIRECT profiles (terminalID == "") never match — they have no
// terminal config to look up.
func LookupForLaunch(terminalID, shellID, shellName string) (LaunchOverride, bool) {
	if terminalID == "" || shellID == "" {
		return LaunchOverride{}, false
	}
	switch terminalID {
	case "wt":
		return lookupWTProfile(shellID, shellName, wtSettingsCandidates())
	case "wezterm":
		return lookupWeztermEntry(shellID, shellName, weztermLuaCandidates())
	}
	return LaunchOverride{}, false
}

// ─── Wezterm launch_menu lookup ───────────────────────────────────────────

// lookupWeztermEntry walks `paths` in order, returning the first user-config
// entry that matches (shellID, shellName). Paths are wezterm.lua candidates;
// the first one that opens cleanly is treated as authoritative — WezTerm
// itself only consumes one config, so we mirror that.
func lookupWeztermEntry(shellID, shellName string, paths []string) (LaunchOverride, bool) {
	for _, p := range paths {
		entries, ok := cachedWeztermEntries(p)
		if !ok {
			continue
		}
		for _, e := range entries {
			if !matchesShell(e.Label, shellID, shellName) {
				continue
			}
			argv := []string{"start", "--cwd", launch.TokenPath, "--"}
			argv = append(argv, e.Args...)
			return LaunchOverride{Argv: argv, Env: e.Env}, true
		}
		// Found a config file but no matching entry → stop scanning further
		// candidates. Falling through would let a stale fallback config win
		// over the user's actual one.
		return LaunchOverride{}, false
	}
	return LaunchOverride{}, false
}

// ─── Windows Terminal settings.json lookup ────────────────────────────────

// wtProfileLite is the slice of a WT profile gitbox needs at launch time.
// Pulled out into its own type so the cache can store it independent of the
// pkg/config.TerminalProfile shape that DiscoverWTProfiles emits.
type wtProfileLite struct {
	Name        string
	CommandLine string
	Hidden      bool
	Source      string
}

// lookupWTProfile returns a `-w 0 nt --profile "<name>" -d {path}` argv
// when the settings.json carries a profile whose Name matches the gitbox
// shell. Disabled-source / hidden profiles are skipped (matches WT's own
// menu).
//
// The `-w 0 nt` prefix pins the launch to the most recent existing WT
// window (creating one if none exists) and uses the explicit `new-tab`
// subcommand. Without it, `wt.exe --profile X -d <path>` always allocates
// a fresh window — and when the user has `firstWindowPreference:
// persistedWindowLayout` set in settings.json, WT then fires the saved-
// layout restore in PARALLEL with our launch, ending up with two windows
// (one with our requested tab in the repo dir, one restoring the previous
// session). `-w 0 nt` keeps the launch inside a single window.
//
// We pass argv through as-is — WT itself reads the rest of the profile's
// fields (font, colors, commandline, …) from settings.json when it sees
// `--profile`, so gitbox doesn't have to re-implement any of that.
func lookupWTProfile(shellID, shellName string, paths []string) (LaunchOverride, bool) {
	for _, p := range paths {
		profiles, ok := cachedWTProfiles(p)
		if !ok {
			continue
		}
		for _, prof := range profiles {
			if !matchesShell(prof.Name, shellID, shellName) {
				continue
			}
			argv := []string{"-w", "0", "nt", "--profile", prof.Name, "-d", launch.TokenPath}
			return LaunchOverride{Argv: argv}, true
		}
		return LaunchOverride{}, false
	}
	return LaunchOverride{}, false
}

// ─── Cache (mtime-invalidated, in-process) ────────────────────────────────

type weztermCacheEntry struct {
	mtime   int64
	size    int64
	entries []harness.WeztermLaunchMenuEntry
}

type wtCacheEntry struct {
	mtime    int64
	size     int64
	profiles []wtProfileLite
}

var (
	weztermCacheMu sync.Mutex
	weztermCache   = map[string]*weztermCacheEntry{}

	wtCacheMu sync.Mutex
	wtCache   = map[string]*wtCacheEntry{}
)

// cachedWeztermEntries returns parsed launch_menu entries for `path`,
// re-reading and re-parsing only when the file's mtime/size has changed.
// Returns ok=false when the file can't be read; a successful read with a
// missing/empty launch_menu yields (nil, true) so the caller stops walking
// the candidate list (the user's authoritative wezterm.lua exists, it just
// has no launch_menu to consult).
func cachedWeztermEntries(path string) ([]harness.WeztermLaunchMenuEntry, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	mtime := st.ModTime().UnixNano()
	size := st.Size()

	weztermCacheMu.Lock()
	defer weztermCacheMu.Unlock()
	if c, ok := weztermCache[path]; ok && c.mtime == mtime && c.size == size {
		return c.entries, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	entries, perr := harness.ParseWeztermLaunchMenu(data)
	if perr != nil {
		// Best-effort parse: a malformed wezterm.lua (or one without a
		// launch_menu block) becomes a treatment-equivalent of "no
		// launch_menu" so launch falls back to generic templates instead
		// of erroring at the user.
		entries = nil
	}
	weztermCache[path] = &weztermCacheEntry{mtime: mtime, size: size, entries: entries}
	return entries, true
}

// cachedWTProfiles returns parsed WT profiles for `path` with the same
// mtime+size invalidation contract as cachedWeztermEntries.
func cachedWTProfiles(path string) ([]wtProfileLite, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	mtime := st.ModTime().UnixNano()
	size := st.Size()

	wtCacheMu.Lock()
	defer wtCacheMu.Unlock()
	if c, ok := wtCache[path]; ok && c.mtime == mtime && c.size == size {
		return c.profiles, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	profiles, perr := parseWTProfilesLite(data)
	if perr != nil {
		profiles = nil
	}
	wtCache[path] = &wtCacheEntry{mtime: mtime, size: size, profiles: profiles}
	return profiles, true
}

// parseWTProfilesLite is a slimmed-down counterpart to parseWTProfiles that
// returns the raw fields needed at launch time without going through the
// pkg/config.TerminalProfile shape.
func parseWTProfilesLite(data []byte) ([]wtProfileLite, error) {
	clean := stripJSONComments(data)
	var doc struct {
		DisabledProfileSources []string `json:"disabledProfileSources"`
		Profiles               struct {
			List []struct {
				Name        string `json:"name"`
				Hidden      *bool  `json:"hidden,omitempty"`
				Source      string `json:"source,omitempty"`
				CommandLine string `json:"commandline,omitempty"`
			} `json:"list"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(clean, &doc); err != nil {
		return nil, err
	}
	disabled := make(map[string]bool, len(doc.DisabledProfileSources))
	for _, s := range doc.DisabledProfileSources {
		disabled[s] = true
	}
	out := make([]wtProfileLite, 0, len(doc.Profiles.List))
	for _, p := range doc.Profiles.List {
		if p.Name == "" {
			continue
		}
		hidden := false
		if p.Hidden != nil {
			hidden = *p.Hidden
		}
		if hidden {
			continue
		}
		if p.Source != "" && disabled[p.Source] {
			continue
		}
		out = append(out, wtProfileLite{
			Name:        p.Name,
			CommandLine: p.CommandLine,
			Hidden:      hidden,
			Source:      p.Source,
		})
	}
	return out, nil
}

// resetLookupCachesForTest drops every cached entry so tests can mutate
// fixture files between assertions without colliding with stale state.
// Exported via _test.go file linkage; not part of the public API.
func resetLookupCachesForTest() {
	weztermCacheMu.Lock()
	weztermCache = map[string]*weztermCacheEntry{}
	weztermCacheMu.Unlock()
	wtCacheMu.Lock()
	wtCache = map[string]*wtCacheEntry{}
	wtCacheMu.Unlock()
}

// ─── Shell-name fuzzy matching ────────────────────────────────────────────

// matchesShell reports whether the given user-config entry name corresponds
// to the gitbox shell identified by (shellID, shellName).
//
// The match priority is:
//
//  1. Direct: the normalised entry name equals the normalised shell display
//     Name. Catches "PowerShell 7" ≡ "PowerShell 7" and similar.
//  2. Em-dash suffix: gitbox names like "WSL — Ubuntu-24.04" also match an
//     entry whose name normalises to just the suffix ("Ubuntu-24.04"). This
//     is how power users tend to label WSL distros in their wezterm.lua /
//     settings.json — by distro, not the gitbox-style "WSL — " prefix.
//  3. Pattern fallback: the per-shell-id substring patterns table picks up
//     common variations (pwsh ≈ "powershell 7"/"pwsh", cmd ≈ "command
//     prompt"/"cmd exe", git-bash ≈ "git bash", etc.).
//
// All comparisons are normalised: lowercased and reduced to runs-of-
// alphanumerics-separated-by-single-spaces so display variations like
// "PowerShell 7" / "powershell-7" / "powershell  7" are equivalent.
func matchesShell(entryName, shellID, shellName string) bool {
	eNorm := normalizeName(entryName)
	if eNorm == "" {
		return false
	}
	if eNorm == normalizeName(shellName) {
		return true
	}
	if suffix := nameAfterEmDash(shellName); suffix != "" {
		if eNorm == normalizeName(suffix) {
			return true
		}
	}
	for _, pat := range shellMatchPatterns[shellID] {
		if strings.Contains(eNorm, pat) {
			return true
		}
	}
	// wsl-<distro> ids like "wsl-ubuntu-24-04" let us derive a distro slug
	// from the id itself when the display Name doesn't carry an em-dash.
	if strings.HasPrefix(shellID, "wsl-") {
		distro := strings.TrimPrefix(shellID, "wsl-")
		distro = strings.ReplaceAll(distro, "-", " ")
		distro = strings.TrimSpace(distro)
		if distro != "" && strings.Contains(eNorm, distro) {
			return true
		}
	}
	return false
}

// normalizeName lower-cases s and collapses runs of non-alphanumerics into
// a single space, trimming leading/trailing whitespace. Result is stable
// across locale and matches the form shellMatchPatterns is written in.
func normalizeName(s string) string {
	if s == "" {
		return ""
	}
	out := make([]rune, 0, len(s))
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out = append(out, r)
			prevSpace = false
		default:
			if !prevSpace {
				out = append(out, ' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimRight(string(out), " ")
}

// nameAfterEmDash returns the substring after the first em-dash (` — `) in
// `s`, or "" when there is none. Mirrors the gitbox display convention of
// "<family> — <variant>" (e.g. "WSL — Ubuntu-24.04").
func nameAfterEmDash(s string) string {
	idx := strings.Index(s, "— ")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(s[idx+len("— "):])
}

// shellMatchPatterns lists fallback substring patterns checked against the
// normalised user-config-entry name when neither the direct nor em-dash-
// suffix comparisons match. Keys are gitbox shell IDs.
//
// Patterns are intentionally narrow to avoid spurious matches across shells
// (e.g. a WT profile literally named "PowerShell" still maps to powershell-5,
// not pwsh, because the pwsh patterns require the "7"). When a user's actual
// entry sits at the boundary, the user can rename it on either side to break
// the tie.
var shellMatchPatterns = map[string][]string{
	"pwsh":       {"powershell 7", "powershell core", "pwsh"},
	"powershell": {"powershell 5", "windows powershell"},
	"cmd":        {"command prompt", "cmd exe"},
	"git-bash":   {"git bash"},
	"wsl":        {"wsl"},
}
