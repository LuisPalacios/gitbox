package main

// Terminal Profiles backbone (issue #69 — v2.1 schema).
//
// This file is additive on top of the legacy SyncTerminals / OpenInTerminal
// path that still lives in app.go. It owns:
//
//   - Shell detection (PATH lookup, Program Files\Git fallback for git-bash,
//     `open -a` resolution on macOS, Homebrew-augmented PATH, login-shell
//     identification on Unix, per-distro WSL discovery on Windows).
//   - Profile composition from detected (terminal × shell) combinations,
//     Windows Terminal `settings.json` profiles, and WezTerm
//     `wezterm.lua` `launch_menu` entries.
//   - The `OpenProfile` / `SaveTerminalProfiles` / `RedetectProfiles`
//     bridge methods exposed to the Wails frontend.
//
// The Svelte UI under cmd/gui/frontend wires up these bridges in a follow-up
// commit (Commit 4 of #69); the legacy launcher path keeps working until then
// so the branch stays bisect-friendly.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/harness"
	"github.com/LuisPalacios/gitbox/pkg/launch"
)

// ─── Shell detection ──────────────────────────────────────────────────────

// resolveShellSpec resolves a markdown-driven shell candidate to a (command,
// args, ok) triple. Mirrors resolveTerminalSpec — the per-OS branch keeps the
// platform-specific lookups (PATH, Program Files, Homebrew PATH) out of the
// markdown.
func resolveShellSpec(s harness.ShellSpec) (string, []string, bool) {
	if isWindows() {
		return resolveWindowsShellSpec(s)
	}
	if isDarwin() {
		return resolveDarwinShellSpec(s)
	}
	return resolveLinuxShellSpec(s)
}

func resolveWindowsShellSpec(s harness.ShellSpec) (string, []string, bool) {
	// PATH covers cmd.exe, powershell.exe, pwsh.exe, wsl.exe in modern installs.
	if p, err := exec.LookPath(s.Command); err == nil {
		return p, appendCopy(s.Args), true
	}
	switch s.Command {
	case "git-bash.exe":
		// Git for Windows installs git-bash.exe under Program Files but does
		// not always wire it into PATH.
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root == "" {
				continue
			}
			cand := filepath.Join(root, "Git", "git-bash.exe")
			if _, err := os.Stat(cand); err == nil {
				return cand, appendCopy(s.Args), true
			}
		}
	case "pwsh.exe":
		// PowerShell 7 ships under Program Files\PowerShell\7.
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root == "" {
				continue
			}
			cand := filepath.Join(root, "PowerShell", "7", "pwsh.exe")
			if _, err := os.Stat(cand); err == nil {
				return cand, appendCopy(s.Args), true
			}
		}
	}
	return "", nil, false
}

func resolveDarwinShellSpec(s harness.ShellSpec) (string, []string, bool) {
	p, err := lookPathWithBrewPATH(s.Command)
	if err != nil {
		return "", nil, false
	}
	return p, appendCopy(s.Args), true
}

func resolveLinuxShellSpec(s harness.ShellSpec) (string, []string, bool) {
	p, err := lookPathWithBrewPATH(s.Command)
	if err != nil {
		return "", nil, false
	}
	return p, appendCopy(s.Args), true
}

// shellSpecMatchesCurrentOS mirrors terminalSpecMatchesCurrentOS — Linux row
// matches any non-Windows-non-macOS Unix host so *BSD users still get a usable
// shell list.
func shellSpecMatchesCurrentOS(os string) bool {
	switch os {
	case "Windows":
		return isWindows()
	case "macOS":
		return isDarwin()
	case "Linux":
		return !isWindows() && !isDarwin()
	}
	return false
}

// platformShells returns the resolved ShellEntry list for this host, in
// markdown order. The bare `wsl.exe` entry is dropped when at least one
// per-distro WSL entry is appended (caller decides — see SyncProfiles).
func platformShells() []config.ShellEntry {
	specs := harness.KnownShells()
	var out []config.ShellEntry
	seen := make(map[string]bool)
	for _, s := range specs {
		if !shellSpecMatchesCurrentOS(s.OS) {
			continue
		}
		cmd, args, ok := resolveShellSpec(s)
		if !ok {
			continue
		}
		id := shellIDFromSpec(s)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, config.ShellEntry{
			ID:      id,
			Name:    s.Name,
			Command: cmd,
			Args:    args,
		})
	}
	return out
}

// shellIDFromSpec maps a known shell spec to the stable id that the rest of
// the system uses (config IDs, profile compositions, migrator output). Keep
// in sync with pkg/config/migrate_terminals.go::inferShellFromLegacy so a
// migrated profile and a freshly-detected one collide on the same id.
func shellIDFromSpec(s harness.ShellSpec) string {
	switch strings.ToLower(filepath.Base(s.Command)) {
	case "cmd.exe":
		return "cmd"
	case "powershell.exe":
		return "powershell"
	case "pwsh.exe", "pwsh":
		return "pwsh"
	case "git-bash.exe":
		return "git-bash"
	case "wsl.exe":
		return "wsl"
	case "bash":
		return "bash"
	case "zsh":
		return "zsh"
	case "fish":
		return "fish"
	}
	return slugifyASCII(s.Name)
}

// ─── WSL distro discovery ─────────────────────────────────────────────────

// discoverWSLDistros returns the list of installed WSL distributions, in the
// order `wsl --list --quiet` reports them. Returns nil on non-Windows hosts,
// when wsl.exe is missing, or when no distros are installed — none of those
// are errors, just absence of WSL state.
func discoverWSLDistros() []string {
	if !isWindows() {
		return nil
	}
	cmd := exec.Command("wsl.exe", "--list", "--quiet")
	git.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	text := decodeWSLBytes(out)
	var distros []string
	for _, line := range strings.Split(text, "\n") {
		name := strings.TrimSpace(line)
		// `wsl --list --quiet` prints a trailing empty line; some builds also
		// emit a "(Default)" suffix that --quiet was supposed to suppress.
		// Normalise both forms here so the rest of the code treats the list
		// as a clean set of names.
		name = strings.TrimSuffix(name, " (Default)")
		if name == "" {
			continue
		}
		distros = append(distros, name)
	}
	return distros
}

// wslDistroShells returns one ShellEntry per installed WSL distro. The bare
// `wsl.exe` row is intentionally not emitted by this function — when at least
// one distro exists the per-distro entries are the better default; the bare
// `wsl.exe` PATH row is then redundant.
func wslDistroShells(wslCmd string, distros []string) []config.ShellEntry {
	if wslCmd == "" || len(distros) == 0 {
		return nil
	}
	out := make([]config.ShellEntry, 0, len(distros))
	for _, d := range distros {
		out = append(out, config.ShellEntry{
			ID:      "wsl-" + slugifyASCII(d),
			Name:    "WSL — " + d,
			Command: wslCmd,
			Args:    []string{"-d", d},
		})
	}
	return out
}

// decodeWSLBytes converts wsl.exe's UTF-16 LE output (BOM-prefixed in modern
// builds, sometimes BOM-less) into UTF-8. Falls back to the raw string when
// the bytes don't look like UTF-16 — a few `wsl.exe` ports emit UTF-8 already.
func decodeWSLBytes(b []byte) string {
	// UTF-16 LE BOM (FF FE).
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		return decodeUTF16LE(b[2:])
	}
	// Heuristic for BOM-less UTF-16 LE: even length and the second byte
	// (high byte of first code unit) is zero — typical for ASCII content.
	if len(b) >= 2 && len(b)%2 == 0 && b[1] == 0 {
		return decodeUTF16LE(b)
	}
	return strings.TrimPrefix(string(b), "\ufeff")
}

func decodeUTF16LE(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u16))
}

// ─── Login-shell detection (macOS / Linux) ────────────────────────────────

// loginShellID returns the shell id that matches the current user's login
// shell as reported by /etc/passwd (or `dscl` on macOS via os/user). Used to
// pick a sensible Default profile on Unix hosts. Returns ("", false) on
// Windows, when the user's shell is not in the known set, or when probing
// fails.
func loginShellID() (string, bool) {
	if isWindows() {
		return "", false
	}
	shellPath := strings.TrimSpace(os.Getenv("SHELL"))
	if shellPath == "" {
		// Fallback: read the login shell from the system user database.
		// os/user.Current resolves the entry; not every platform exposes
		// the shell field, so we degrade silently when missing.
		if u, err := user.Current(); err == nil {
			shellPath = readLoginShellFromPasswd(u.Username)
		}
	}
	if shellPath == "" {
		return "", false
	}
	switch strings.ToLower(filepath.Base(shellPath)) {
	case "bash":
		return "bash", true
	case "zsh":
		return "zsh", true
	case "fish":
		return "fish", true
	}
	return "", false
}

// readLoginShellFromPasswd parses /etc/passwd for the given username and
// returns the trailing shell field, or "" on miss. Best-effort — some systems
// (sssd, ldap, dscl on macOS) don't ship the entry to /etc/passwd. The
// $SHELL env var is the primary signal; this is just a fallback.
func readLoginShellFromPasswd(username string) string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) >= 7 && fields[0] == username {
			return strings.TrimSpace(fields[6])
		}
	}
	return ""
}

// ─── Terminal-app detection (v2.1 shape) ──────────────────────────────────

// platformTerminalApps returns the resolved TerminalApp list for this host.
// The args template comes from terminalArgsTemplateFor — sourced from the
// terminal directory but with shell tokens substituted in so each app knows
// how to splice a shell at launch time.
func platformTerminalApps() []config.TerminalApp {
	specs := harness.KnownTerminals()
	var out []config.TerminalApp
	seen := make(map[string]bool)
	for _, s := range specs {
		if !terminalSpecMatchesCurrentOS(s.OS) {
			continue
		}
		cmd, _, ok := resolveTerminalSpec(s)
		if !ok {
			continue
		}
		id, app := terminalAppFromSpec(s, cmd)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, app)
	}
	return out
}

// terminalAppFromSpec maps a markdown terminal row to a stable TerminalApp
// id + record. Mirrors pkg/config/migrate_terminals.go::inferTerminalApp so
// detected and migrated entries collide on the same id; that lets the merge
// step in SyncProfiles preserve user customisations across a re-detect.
func terminalAppFromSpec(s harness.TerminalSpec, resolvedCmd string) (string, config.TerminalApp) {
	switch strings.ToLower(filepath.Base(s.Command)) {
	case "wt.exe":
		return "wt", config.TerminalApp{
			ID:           "wt",
			Name:         "Windows Terminal",
			Command:      resolvedCmd,
			ArgsTemplate: []string{"-d", launch.TokenPath, launch.TokenShellCommand, launch.TokenShellArgs},
		}
	case "wezterm.exe", "wezterm-gui.exe", "wezterm", "wezterm-gui":
		return "wezterm", config.TerminalApp{
			ID:           "wezterm",
			Name:         "WezTerm",
			Command:      resolvedCmd,
			ArgsTemplate: []string{"start", "--cwd", launch.TokenPath, "--", launch.TokenShellCommand, launch.TokenShellArgs},
		}
	case "open":
		// macOS `open -a <App>` — the App handles its own shell.
		if len(s.Args) >= 2 && s.Args[0] == "-a" {
			appName := s.Args[1]
			id := "macapp-" + slugifyASCII(appName)
			return id, config.TerminalApp{
				ID:           id,
				Name:         appName,
				Command:      resolvedCmd,
				ArgsTemplate: []string{"-a", appName},
			}
		}
	case "gnome-terminal":
		return "gnome-terminal", config.TerminalApp{
			ID:           "gnome-terminal",
			Name:         "GNOME Terminal",
			Command:      resolvedCmd,
			ArgsTemplate: []string{"--working-directory=" + launch.TokenPath, "--", launch.TokenShellCommand, launch.TokenShellArgs},
		}
	case "konsole":
		return "konsole", config.TerminalApp{
			ID:           "konsole",
			Name:         "Konsole",
			Command:      resolvedCmd,
			ArgsTemplate: []string{"--workdir", launch.TokenPath, "-e", launch.TokenShellCommand, launch.TokenShellArgs},
		}
	case "kitty":
		return "kitty", config.TerminalApp{
			ID:           "kitty",
			Name:         "Kitty",
			Command:      resolvedCmd,
			ArgsTemplate: []string{"--directory=" + launch.TokenPath, launch.TokenShellCommand, launch.TokenShellArgs},
		}
	case "alacritty":
		return "alacritty", config.TerminalApp{
			ID:           "alacritty",
			Name:         "Alacritty",
			Command:      resolvedCmd,
			ArgsTemplate: []string{"--working-directory", launch.TokenPath, "-e", launch.TokenShellCommand, launch.TokenShellArgs},
		}
	case "xfce4-terminal":
		return "xfce4-terminal", config.TerminalApp{
			ID:           "xfce4-terminal",
			Name:         "Xfce Terminal",
			Command:      resolvedCmd,
			ArgsTemplate: []string{"--working-directory=" + launch.TokenPath},
		}
	case "terminator":
		return "terminator", config.TerminalApp{
			ID:           "terminator",
			Name:         "Terminator",
			Command:      resolvedCmd,
			ArgsTemplate: []string{"--working-directory=" + launch.TokenPath},
		}
	case "git-bash.exe":
		// Git Bash is really a shell-with-its-own-window — keep it as a
		// terminal app so users who relied on the legacy entry still see it
		// in the list, but the args template treats the shell tokens as a
		// no-op (Git Bash launches its own bash automatically).
		return "git-bash", config.TerminalApp{
			ID:           "git-bash",
			Name:         "Git Bash",
			Command:      resolvedCmd,
			ArgsTemplate: []string{"--cd=" + launch.TokenPath},
		}
	case "cmd.exe", "powershell.exe", "pwsh.exe", "wsl.exe":
		// Bare-shell rows in the legacy directory are really Shells, not
		// Terminal apps. Drop them from the v2.1 TerminalApp list — the
		// shell directory + per-distro WSL discovery covers them. Migration
		// of legacy configs preserves them as standalone profiles.
		return "", config.TerminalApp{}
	}
	return slugifyASCII(s.Name), config.TerminalApp{
		ID:      slugifyASCII(s.Name),
		Name:    s.Name,
		Command: resolvedCmd,
	}
}

// ─── Profile parsers ──────────────────────────────────────────────────────

// parseWTProfilesAsProfiles is the v2.1 sibling of parseWTProfiles in app.go.
// Each visible WT profile becomes a TerminalProfile whose Args carry the
// `--profile "<name>" -d {path}` template — Windows Terminal handles shell
// selection itself, so ShellID stays empty.
func parseWTProfilesAsProfiles(data []byte, _ string) ([]config.TerminalProfile, error) {
	clean := stripJSONComments(data)
	var doc struct {
		DisabledProfileSources []string `json:"disabledProfileSources"`
		Profiles               struct {
			List []struct {
				Name   string `json:"name"`
				Hidden *bool  `json:"hidden,omitempty"`
				Source string `json:"source,omitempty"`
			} `json:"list"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(clean, &doc); err != nil {
		return nil, err
	}
	if len(doc.Profiles.List) == 0 {
		return nil, errors.New("no profiles in settings.json")
	}
	disabled := make(map[string]bool, len(doc.DisabledProfileSources))
	for _, s := range doc.DisabledProfileSources {
		disabled[s] = true
	}
	var out []config.TerminalProfile
	for _, p := range doc.Profiles.List {
		if p.Name == "" {
			continue
		}
		if p.Hidden != nil && *p.Hidden {
			continue
		}
		if p.Source != "" && disabled[p.Source] {
			continue
		}
		out = append(out, config.TerminalProfile{
			ID:         "wt+" + slugifyASCII(p.Name),
			Name:       p.Name,
			TerminalID: "wt",
			Args:       []string{"--profile", p.Name, "-d", launch.TokenPath, launch.TokenCommand},
			Source:     "wt-profile",
		})
	}
	if len(out) == 0 {
		return nil, errors.New("no visible WT profiles")
	}
	return out, nil
}

// discoverWTProfilesAsProfiles is the v2.1 sibling of discoverWTProfiles —
// returns the parsed profile list or an error when wt.exe / settings.json /
// the JSON shape can't be located.
func discoverWTProfilesAsProfiles() ([]config.TerminalProfile, error) {
	wtCmd, ok := wtExePath()
	if !ok {
		return nil, errors.New("wt.exe not found")
	}
	for _, path := range wtSettingsCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		profiles, err := parseWTProfilesAsProfiles(data, wtCmd)
		if err != nil {
			return nil, err
		}
		return profiles, nil
	}
	return nil, errors.New("settings.json not found")
}

// weztermLuaCandidates returns the canonical wezterm.lua locations for the
// current host, in WezTerm's own lookup order. WezTerm itself searches
// $WEZTERM_CONFIG_FILE first, then $XDG_CONFIG_HOME/wezterm/wezterm.lua, then
// $HOME/.config/wezterm/wezterm.lua, then $HOME/.wezterm.lua. We mirror that
// so a user's existing layout just works.
func weztermLuaCandidates() []string {
	var out []string
	if explicit := os.Getenv("WEZTERM_CONFIG_FILE"); explicit != "" {
		out = append(out, explicit)
	}
	home, _ := os.UserHomeDir()
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		out = append(out, filepath.Join(xdg, "wezterm", "wezterm.lua"))
	}
	if home != "" {
		out = append(out,
			filepath.Join(home, ".config", "wezterm", "wezterm.lua"),
			filepath.Join(home, ".wezterm.lua"),
		)
	}
	return out
}

// discoverWeztermProfiles parses the user's wezterm.lua (if any) and returns
// one TerminalProfile per `launch_menu` entry. The Args slice carries the
// entry's argv via the `start --cwd {path} -- <argv...>` shape — that maps
// cleanly to `wezterm-gui start --cwd /repo -- bash -l` and bypasses the
// generic shell tokens since WezTerm handles its own argv. Returns an empty
// slice (no error) when the lua file is missing or has no launch_menu — that
// is a normal config, not a failure.
func discoverWeztermProfiles() []config.TerminalProfile {
	for _, path := range weztermLuaCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entries, err := harness.ParseWeztermLaunchMenu(data)
		if err != nil {
			// Swallow malformed-file errors — surface them via the GUI's
			// "Re-detect" status line instead of crashing startup.
			return nil
		}
		out := make([]config.TerminalProfile, 0, len(entries))
		for _, e := range entries {
			label := e.Label
			if label == "" && len(e.Args) > 0 {
				label = e.Args[0]
			}
			if label == "" {
				continue
			}
			args := []string{"start", "--cwd", launch.TokenPath, "--"}
			args = append(args, e.Args...)
			out = append(out, config.TerminalProfile{
				ID:         "wezterm+" + slugifyASCII(label),
				Name:       "WezTerm — " + label,
				TerminalID: "wezterm",
				Args:       args,
				Source:     "wezterm-launchmenu",
			})
		}
		return out
	}
	return nil
}

// ─── SyncProfiles ─────────────────────────────────────────────────────────

// SyncProfiles reconciles config's TerminalApps + Shells + TerminalProfiles
// arrays with what is currently installed on the host. It preserves user
// customisations (Default / Preferred / Hidden flags, renamed Profiles, hand-
// added entries) by id-merging the freshly-detected set with the persisted
// one.
//
// The sync is idempotent and side-effect-free when nothing changed — the
// config is only re-saved when the resulting JSON differs.
func (a *App) SyncProfiles() {
	a.mu.Lock()
	defer a.mu.Unlock()

	apps := platformTerminalApps()
	shells := platformShells()

	// Per-distro WSL: replace the bare wsl.exe row with one row per distro
	// when discovery returns a non-empty list. The bare row stays as a
	// fallback only when no distros are reachable.
	if isWindows() {
		distros := discoverWSLDistros()
		if len(distros) > 0 {
			var wslCmd string
			filtered := shells[:0]
			for _, s := range shells {
				if s.ID == "wsl" {
					wslCmd = s.Command
					continue
				}
				filtered = append(filtered, s)
			}
			shells = filtered
			shells = append(shells, wslDistroShells(wslCmd, distros)...)
		}
	}

	profiles := composeProfiles(apps, shells)

	// Append WT-discovered profiles ahead of generic compositions so the
	// user's personal WT profiles win id collisions.
	if isWindows() {
		if wt, err := discoverWTProfilesAsProfiles(); err == nil {
			profiles = mergeProfilesByID(wt, profiles)
		}
	}
	if wez := discoverWeztermProfiles(); len(wez) > 0 {
		profiles = mergeProfilesByID(wez, profiles)
	}

	// Mark a sensible default on a fresh config: prefer the host's login
	// shell on Unix, otherwise the first composed profile.
	if defaultProfileMissing(a.cfg.Global.TerminalProfiles) {
		applyDefaultProfile(profiles)
	}

	merged := mergeWithExisting(apps, shells, profiles, a.cfg.Global.TerminalApps,
		a.cfg.Global.Shells, a.cfg.Global.TerminalProfiles)

	if !profilesPayloadEqual(merged.apps, merged.shells, merged.profiles,
		a.cfg.Global.TerminalApps, a.cfg.Global.Shells, a.cfg.Global.TerminalProfiles) {
		a.cfg.Global.TerminalApps = merged.apps
		a.cfg.Global.Shells = merged.shells
		a.cfg.Global.TerminalProfiles = merged.profiles
		_ = a.saveConfig()
	}
}

// composeProfiles produces one TerminalProfile per (terminal × applicable
// shell) pair. Terminals whose ArgsTemplate has no shell tokens (`open -a`,
// `git-bash --cd`) get a single shell-less profile — they handle their own
// shell selection.
func composeProfiles(apps []config.TerminalApp, shells []config.ShellEntry) []config.TerminalProfile {
	var out []config.TerminalProfile
	for _, app := range apps {
		if !templateAcceptsShell(app.ArgsTemplate) {
			out = append(out, config.TerminalProfile{
				ID:         app.ID,
				Name:       app.Name,
				TerminalID: app.ID,
				Source:     "detected",
			})
			continue
		}
		for _, sh := range shells {
			out = append(out, config.TerminalProfile{
				ID:         app.ID + "+" + sh.ID,
				Name:       app.Name + " — " + sh.Name,
				TerminalID: app.ID,
				ShellID:    sh.ID,
				Source:     "detected",
			})
		}
	}
	return out
}

// templateAcceptsShell reports whether the args template references either
// shell token. Terminals with no shell tokens are launched as-is and handle
// their own shell (Terminal.app, Git Bash, …).
func templateAcceptsShell(tmpl []string) bool {
	for _, a := range tmpl {
		if a == launch.TokenShellCommand || a == launch.TokenShellArgs {
			return true
		}
	}
	return false
}

// mergeProfilesByID appends `additions` to `base`, dropping additions whose
// id collides with an existing base entry. The order is preserved so the
// caller controls relative priority.
func mergeProfilesByID(additions, base []config.TerminalProfile) []config.TerminalProfile {
	seen := make(map[string]bool, len(base))
	for _, p := range base {
		seen[p.ID] = true
	}
	out := make([]config.TerminalProfile, 0, len(base)+len(additions))
	out = append(out, base...)
	for _, p := range additions {
		if seen[p.ID] {
			continue
		}
		out = append(out, p)
	}
	return out
}

func defaultProfileMissing(profiles []config.TerminalProfile) bool {
	for _, p := range profiles {
		if p.Default {
			return false
		}
	}
	return true
}

// applyDefaultProfile picks a sensible Default among newly-composed profiles:
// prefer the host's login shell paired with the first detected terminal, then
// fall back to the first profile in the list. Returns silently when the list
// is empty.
func applyDefaultProfile(profiles []config.TerminalProfile) {
	if len(profiles) == 0 {
		return
	}
	if shellID, ok := loginShellID(); ok {
		for i := range profiles {
			if profiles[i].ShellID == shellID {
				profiles[i].Default = true
				return
			}
		}
	}
	profiles[0].Default = true
}

type mergedProfilesPayload struct {
	apps     []config.TerminalApp
	shells   []config.ShellEntry
	profiles []config.TerminalProfile
}

// mergeWithExisting reconciles freshly-detected apps/shells/profiles with
// what is already persisted in config. User-set flags (Default, Preferred,
// Hidden), renamed Names, and hand-added entries survive a re-detect; entries
// the host can no longer reach are dropped.
func mergeWithExisting(detApps []config.TerminalApp, detShells []config.ShellEntry,
	detProfiles []config.TerminalProfile, prevApps []config.TerminalApp,
	prevShells []config.ShellEntry, prevProfiles []config.TerminalProfile) mergedProfilesPayload {

	priorApps := indexApps(prevApps)
	mergedApps := make([]config.TerminalApp, 0, len(detApps))
	seenApp := make(map[string]bool)
	for _, a := range detApps {
		if prior, ok := priorApps[a.ID]; ok {
			if prior.Name != "" {
				a.Name = prior.Name
			}
			if len(prior.ArgsTemplate) > 0 && !argsTemplateIsDefault(prior.ArgsTemplate, a.ArgsTemplate) {
				// Preserve user-customised templates verbatim.
				a.ArgsTemplate = append([]string(nil), prior.ArgsTemplate...)
			}
		}
		mergedApps = append(mergedApps, a)
		seenApp[a.ID] = true
	}

	priorShells := indexShells(prevShells)
	mergedShells := make([]config.ShellEntry, 0, len(detShells))
	seenShell := make(map[string]bool)
	for _, s := range detShells {
		if prior, ok := priorShells[s.ID]; ok {
			if prior.Name != "" {
				s.Name = prior.Name
			}
			if len(prior.Args) > 0 && !argsEqualSlices(prior.Args, s.Args) {
				s.Args = append([]string(nil), prior.Args...)
			}
		}
		mergedShells = append(mergedShells, s)
		seenShell[s.ID] = true
	}

	priorProfiles := indexProfiles(prevProfiles)
	mergedProfiles := make([]config.TerminalProfile, 0, len(detProfiles))
	seenProfile := make(map[string]bool)
	for _, p := range detProfiles {
		if prior, ok := priorProfiles[p.ID]; ok {
			if prior.Name != "" {
				p.Name = prior.Name
			}
			p.Default = prior.Default
			p.Preferred = prior.Preferred
			p.Hidden = prior.Hidden
			if len(prior.Args) > 0 {
				p.Args = append([]string(nil), prior.Args...)
			}
		}
		mergedProfiles = append(mergedProfiles, p)
		seenProfile[p.ID] = true
	}

	// Carry user-added entries (Source="user") and migrated/legacy profiles
	// whose terminal id no longer resolves to a detected app — the user might
	// be working on a host where the terminal is temporarily unavailable.
	for _, p := range prevProfiles {
		if seenProfile[p.ID] {
			continue
		}
		if p.Source == "user" || p.Source == "migrated" {
			mergedProfiles = append(mergedProfiles, p)
		}
	}
	for _, a := range prevApps {
		if !seenApp[a.ID] {
			// Keep user-added apps so their bound profiles still launch.
			if !standardAppID(a.ID) {
				mergedApps = append(mergedApps, a)
			}
		}
	}
	for _, s := range prevShells {
		if !seenShell[s.ID] {
			if !standardShellID(s.ID) {
				mergedShells = append(mergedShells, s)
			}
		}
	}

	// Ensure exactly one Default survives across the merge.
	enforceSingleDefault(mergedProfiles)

	return mergedProfilesPayload{apps: mergedApps, shells: mergedShells, profiles: mergedProfiles}
}

// enforceSingleDefault keeps only the first Default flag in the slice. When
// no entry is marked Default and the slice is non-empty, the first entry is
// promoted so the launcher always has a default to call.
func enforceSingleDefault(profiles []config.TerminalProfile) {
	first := -1
	for i, p := range profiles {
		if p.Default {
			if first < 0 {
				first = i
			} else {
				profiles[i].Default = false
			}
		}
	}
	if first < 0 && len(profiles) > 0 {
		profiles[0].Default = true
	}
}

func standardAppID(id string) bool {
	switch id {
	case "wt", "wezterm", "git-bash", "gnome-terminal", "konsole",
		"kitty", "alacritty", "xfce4-terminal", "terminator":
		return true
	}
	return strings.HasPrefix(id, "macapp-")
}

func standardShellID(id string) bool {
	switch id {
	case "cmd", "powershell", "pwsh", "git-bash", "wsl",
		"bash", "zsh", "fish":
		return true
	}
	return strings.HasPrefix(id, "wsl-")
}

func indexApps(apps []config.TerminalApp) map[string]config.TerminalApp {
	out := make(map[string]config.TerminalApp, len(apps))
	for _, a := range apps {
		out[a.ID] = a
	}
	return out
}

func indexShells(shells []config.ShellEntry) map[string]config.ShellEntry {
	out := make(map[string]config.ShellEntry, len(shells))
	for _, s := range shells {
		out[s.ID] = s
	}
	return out
}

func indexProfiles(profiles []config.TerminalProfile) map[string]config.TerminalProfile {
	out := make(map[string]config.TerminalProfile, len(profiles))
	for _, p := range profiles {
		out[p.ID] = p
	}
	return out
}

func argsTemplateIsDefault(prior, fresh []string) bool {
	return argsEqualSlices(prior, fresh)
}

func argsEqualSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func profilesPayloadEqual(a1 []config.TerminalApp, s1 []config.ShellEntry, p1 []config.TerminalProfile,
	a2 []config.TerminalApp, s2 []config.ShellEntry, p2 []config.TerminalProfile) bool {
	if len(a1) != len(a2) || len(s1) != len(s2) || len(p1) != len(p2) {
		return false
	}
	for i := range a1 {
		if !appsEqual(a1[i], a2[i]) {
			return false
		}
	}
	for i := range s1 {
		if !shellsEqual(s1[i], s2[i]) {
			return false
		}
	}
	for i := range p1 {
		if !profilesEqual(p1[i], p2[i]) {
			return false
		}
	}
	return true
}

func appsEqual(a, b config.TerminalApp) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Command == b.Command &&
		argsEqualSlices(a.ArgsTemplate, b.ArgsTemplate)
}

func shellsEqual(a, b config.ShellEntry) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Command == b.Command &&
		argsEqualSlices(a.Args, b.Args)
}

func profilesEqual(a, b config.TerminalProfile) bool {
	return a.ID == b.ID && a.Name == b.Name && a.TerminalID == b.TerminalID &&
		a.ShellID == b.ShellID && a.Default == b.Default && a.Preferred == b.Preferred &&
		a.Hidden == b.Hidden && a.Source == b.Source && argsEqualSlices(a.Args, b.Args)
}

// ─── DTOs (mirrored in cmd/gui/frontend/src/lib/types.ts) ─────────────────

// TerminalAppInfo is the wire shape for a TerminalApp shipped to the Svelte
// frontend. Mirrors config.TerminalApp byte-for-byte but uses JSON-friendly
// field names and explicit slice copying so frontend mutations can't leak
// back into shared config state.
type TerminalAppInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Command      string   `json:"command"`
	ArgsTemplate []string `json:"args_template"`
}

// ShellInfo is the wire shape for a Shell shipped to the Svelte frontend.
type ShellInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// TerminalProfileInfo is the wire shape for a TerminalProfile shipped to the
// Svelte frontend. Carries every flag the Gear-panel UI needs to render the
// table (Default radio, Preferred star, Hidden eye, Source provenance) and
// the per-profile Args override that WT-profile and migrated rows depend on.
type TerminalProfileInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	TerminalID string   `json:"terminal"`
	ShellID    string   `json:"shell"`
	Args       []string `json:"args"`
	Default    bool     `json:"default"`
	Preferred  bool     `json:"preferred"`
	Hidden     bool     `json:"hidden"`
	Source     string   `json:"source"`
}

// ListTerminalApps returns the persisted TerminalApp list as a DTO slice for
// the Svelte frontend. Snapshot-style — no mutation channel back; the
// frontend persists edits via SaveTerminalProfilesDTO.
func (a *App) ListTerminalApps() []TerminalAppInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]TerminalAppInfo, 0, len(a.cfg.Global.TerminalApps))
	for _, app := range a.cfg.Global.TerminalApps {
		out = append(out, TerminalAppInfo{
			ID:           app.ID,
			Name:         app.Name,
			Command:      app.Command,
			ArgsTemplate: append([]string(nil), app.ArgsTemplate...),
		})
	}
	return out
}

// ListShells returns the persisted Shell list as a DTO slice.
func (a *App) ListShells() []ShellInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]ShellInfo, 0, len(a.cfg.Global.Shells))
	for _, s := range a.cfg.Global.Shells {
		out = append(out, ShellInfo{
			ID:      s.ID,
			Name:    s.Name,
			Command: s.Command,
			Args:    append([]string(nil), s.Args...),
		})
	}
	return out
}

// ListTerminalProfiles returns the persisted TerminalProfile list as a DTO
// slice in on-disk order. The frontend renders this as the Gear-panel
// profile table and (filtered to Preferred/Default) as the per-row
// LauncherMenu submenu.
func (a *App) ListTerminalProfiles() []TerminalProfileInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]TerminalProfileInfo, 0, len(a.cfg.Global.TerminalProfiles))
	for _, p := range a.cfg.Global.TerminalProfiles {
		out = append(out, TerminalProfileInfo{
			ID:         p.ID,
			Name:       p.Name,
			TerminalID: p.TerminalID,
			ShellID:    p.ShellID,
			Args:       append([]string(nil), p.Args...),
			Default:    p.Default,
			Preferred:  p.Preferred,
			Hidden:     p.Hidden,
			Source:     p.Source,
		})
	}
	return out
}

// SaveTerminalProfilesDTO is the bridge-friendly counterpart to
// SaveTerminalProfiles — accepts the DTO shape the frontend already speaks
// (JSON-tagged structs, no nil slices) and converts to config types before
// persisting. The two-form split exists so direct Go callers (tests, other
// packages) keep using the typed config slices.
func (a *App) SaveTerminalProfilesDTO(apps []TerminalAppInfo, shells []ShellInfo, profiles []TerminalProfileInfo) error {
	cfgApps := make([]config.TerminalApp, 0, len(apps))
	for _, app := range apps {
		cfgApps = append(cfgApps, config.TerminalApp{
			ID:           app.ID,
			Name:         app.Name,
			Command:      app.Command,
			ArgsTemplate: append([]string(nil), app.ArgsTemplate...),
		})
	}
	cfgShells := make([]config.ShellEntry, 0, len(shells))
	for _, s := range shells {
		cfgShells = append(cfgShells, config.ShellEntry{
			ID:      s.ID,
			Name:    s.Name,
			Command: s.Command,
			Args:    append([]string(nil), s.Args...),
		})
	}
	cfgProfiles := make([]config.TerminalProfile, 0, len(profiles))
	for _, p := range profiles {
		cfgProfiles = append(cfgProfiles, config.TerminalProfile{
			ID:         p.ID,
			Name:       p.Name,
			TerminalID: p.TerminalID,
			ShellID:    p.ShellID,
			Args:       append([]string(nil), p.Args...),
			Default:    p.Default,
			Preferred:  p.Preferred,
			Hidden:     p.Hidden,
			Source:     p.Source,
		})
	}
	return a.SaveTerminalProfiles(cfgApps, cfgShells, cfgProfiles)
}

// ─── Bridge methods ───────────────────────────────────────────────────────

// OpenProfile launches the given profile in the given folder. Resolves the
// profile by id, looks up its terminal app + shell, expands the args
// template via pkg/launch.ResolveArgs, and routes the launch through the
// existing openTerminalAt path so the Windows console-flash workaround stays
// in one place.
func (a *App) OpenProfile(path, profileID string) error {
	if profileID == "" {
		return fmt.Errorf("profile id is required")
	}
	a.mu.Lock()
	profile, app, shell, err := a.lookupProfileLocked(profileID)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	template := profile.Args
	if len(template) == 0 {
		template = app.ArgsTemplate
	}
	args := launch.ResolveArgs(launch.ProfileArgs{
		Template:     template,
		Path:         path,
		ShellCommand: shell.Command,
		ShellArgs:    shell.Args,
	})
	return openTerminalRawAt(path, app.Command, args)
}

// OpenAccountProfile launches the given profile in the account's parent
// folder. Mirror of OpenAccountInTerminal for the v2.1 Profile model — the
// frontend's account-kebab calls this directly with the resolved profile id.
func (a *App) OpenAccountProfile(accountKey, profileID string) error {
	path, err := a.resolveAccountFolder(accountKey)
	if err != nil {
		return err
	}
	return a.OpenProfile(path, profileID)
}

// lookupProfileLocked resolves a profile id into its (profile, app, shell)
// triple under the App mutex. Caller is responsible for holding a.mu.
func (a *App) lookupProfileLocked(profileID string) (config.TerminalProfile, config.TerminalApp, config.ShellEntry, error) {
	for _, p := range a.cfg.Global.TerminalProfiles {
		if p.ID != profileID {
			continue
		}
		var app config.TerminalApp
		for _, t := range a.cfg.Global.TerminalApps {
			if t.ID == p.TerminalID {
				app = t
				break
			}
		}
		if app.ID == "" {
			return p, app, config.ShellEntry{}, fmt.Errorf("terminal %q for profile %q not found", p.TerminalID, p.ID)
		}
		var shell config.ShellEntry
		if p.ShellID != "" {
			for _, s := range a.cfg.Global.Shells {
				if s.ID == p.ShellID {
					shell = s
					break
				}
			}
		}
		return p, app, shell, nil
	}
	return config.TerminalProfile{}, config.TerminalApp{}, config.ShellEntry{}, fmt.Errorf("profile %q not found", profileID)
}

// SaveTerminalProfiles overwrites the persisted terminal-profile arrays with
// the caller-supplied set, normalises the Default flag (exactly one), and
// persists. Used by the Gear-panel section in the GUI.
func (a *App) SaveTerminalProfiles(apps []config.TerminalApp, shells []config.ShellEntry, profiles []config.TerminalProfile) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	enforceSingleDefault(profiles)
	a.cfg.Global.TerminalApps = append([]config.TerminalApp(nil), apps...)
	a.cfg.Global.Shells = append([]config.ShellEntry(nil), shells...)
	a.cfg.Global.TerminalProfiles = append([]config.TerminalProfile(nil), profiles...)
	return a.saveConfig()
}

// RedetectProfiles re-runs SyncProfiles and returns the refreshed config DTO.
// The Gear-panel "Re-detect" button binds to this so the user can reflect
// host changes (installed a new terminal, added a WSL distro, edited
// wezterm.lua) without restarting the GUI.
func (a *App) RedetectProfiles() ConfigDTO {
	a.SyncProfiles()
	return a.GetConfig()
}

// ─── Plain-launch helper (no harness splice) ──────────────────────────────

// openTerminalRawAt launches a terminal command + args in a folder without
// any token expansion — args are passed straight through. The Windows
// `cmd.exe /C start "" /D <path>` wrapper and HideWindow rule still apply,
// matching openTerminalAt.
func openTerminalRawAt(path, command string, args []string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	var cmd *exec.Cmd
	if isWindows() {
		startArgs := make([]string, 0, 6+len(args))
		startArgs = append(startArgs, "/C", "start", "", "/D", path, command)
		startArgs = append(startArgs, args...)
		cmd = exec.Command("cmd.exe", startArgs...)
		git.HideWindow(cmd)
		cmd.Env = sanitizeWindowsTerminalEnv(git.Environ())
	} else {
		cmd = exec.Command(command, args...)
		cmd.Dir = path
		cmd.Env = git.Environ()
	}
	return cmd.Start()
}

// ─── Misc helpers ─────────────────────────────────────────────────────────

// slugifyASCII lower-cases s and replaces every non-[a-z0-9] run with a
// single hyphen, trimming leading/trailing hyphens. Mirrors the migrator's
// slugify so detected and migrated ids agree byte-for-byte.
func slugifyASCII(s string) string {
	if s == "" {
		return ""
	}
	out := make([]rune, 0, len(s))
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out = append(out, r)
			prevDash = false
		default:
			if !prevDash && len(out) > 0 {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	res := string(out)
	return strings.TrimRight(res, "-")
}

// sortProfilesByName returns a new slice sorted alphabetically. Convenience
// helper for callers that want a deterministic display order without
// disturbing the on-disk preserved order.
func sortProfilesByName(profiles []config.TerminalProfile) []config.TerminalProfile {
	out := append([]config.TerminalProfile(nil), profiles...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}
