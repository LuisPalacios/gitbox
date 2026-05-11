package terminals

import (
	"github.com/LuisPalacios/gitbox/pkg/config"
)

// MergePayload is the result of merging freshly-detected/-composed values
// with what is already persisted in config.
type MergePayload struct {
	Apps     []config.TerminalApp
	Shells   []config.ShellEntry
	Profiles []config.TerminalProfile
}

// MergeWithExisting reconciles freshly-detected apps/shells/profiles with
// what is already persisted. The merge preserves user customisations across
// re-detect:
//
//   - User-set flags (Default, Preferred, Hidden) — keyed by stable ID.
//   - User-renamed Names + hand-edited Args overrides — keyed by stable ID.
//   - User-added entries (Profile.Source == "user") survive verbatim
//     regardless of whether the detected set still includes them.
//   - Migrated v2.0 entries (Profile.Source == "migrated") survive verbatim
//     so a user who hand-edited their legacy config (bare-shell Profiles on
//     Windows, etc.) doesn't lose work after the new composition rules
//     drop them from the auto-derived set.
//
// Catalog-detected entries (Source == "detected") that the host can no
// longer reach are dropped — that's the whole point of re-detect.
//
// WT-imported and WezTerm-imported Profiles arrive in `detProfiles` from
// the discover-* functions every Sync; nothing special is needed for them
// here beyond the standard ID-keyed merge.
func MergeWithExisting(detApps []config.TerminalApp, detShells []config.ShellEntry,
	detProfiles []config.TerminalProfile, prevApps []config.TerminalApp,
	prevShells []config.ShellEntry, prevProfiles []config.TerminalProfile) MergePayload {

	mergedApps, seenApp := mergeApps(detApps, prevApps)
	mergedShells, seenShell := mergeShells(detShells, prevShells)
	mergedProfiles, seenProfile := mergeProfiles(detProfiles, prevProfiles)

	// Carry-forward: user-added apps + shells whose ID isn't in the detected
	// set this round, so their dependent Profiles still launch. Catalog ids
	// that the user no longer has installed (or that the catalog deprecated —
	// see deprecatedTerminalIDs) get dropped here.
	for _, a := range prevApps {
		if seenApp[a.ID] {
			continue
		}
		if !inCatalogTerminalIDs(a.ID) {
			mergedApps = append(mergedApps, a)
			seenApp[a.ID] = true
		}
	}
	for _, s := range prevShells {
		if seenShell[s.ID] {
			continue
		}
		if !inCatalogShellIDs(s.ID) {
			mergedShells = append(mergedShells, s)
			seenShell[s.ID] = true
		}
	}

	// Carry-forward: user-added + migrated profiles whose ID isn't in the
	// detected set this round. A `migrated` Profile whose TerminalID no
	// longer resolves to a current terminal_app is broken (the launcher
	// can't reach its terminal) — issue #71 surfaced these as ghost rows
	// referencing terminals like "wt" after the user uninstalled them. Drop
	// those, but keep `user`-added Profiles unconditionally (the user knows
	// what they want even if the row is currently unlaunchable).
	for _, p := range prevProfiles {
		if seenProfile[p.ID] {
			continue
		}
		switch p.Source {
		case "user":
			mergedProfiles = append(mergedProfiles, p)
		case "migrated":
			if p.TerminalID == "" || seenApp[p.TerminalID] {
				mergedProfiles = append(mergedProfiles, p)
			}
		}
	}

	enforceSingleDefault(mergedProfiles)

	return MergePayload{Apps: mergedApps, Shells: mergedShells, Profiles: mergedProfiles}
}

func mergeApps(det, prev []config.TerminalApp) ([]config.TerminalApp, map[string]bool) {
	priorByID := make(map[string]config.TerminalApp, len(prev))
	for _, a := range prev {
		priorByID[a.ID] = a
	}
	out := make([]config.TerminalApp, 0, len(det))
	seen := make(map[string]bool, len(det))
	for _, a := range det {
		if prior, ok := priorByID[a.ID]; ok {
			if prior.Name != "" {
				a.Name = prior.Name
			}
			// Preserve hand-edited args_template UNLESS the persisted form
			// matches a known-broken template from a previous catalog
			// revision. Older gitbox versions seeded entries that the
			// catalog now corrects (e.g. mintty's `-d {path}` interpreted
			// by mintty as --daemon followed by an unparseable positional)
			// and the user has no way to tell whether their args were
			// authored or just inherited. When a match is found, fall
			// through to the catalog template; otherwise honour what's
			// already on disk.
			if len(prior.ArgsTemplate) > 0 && !argsEqual(prior.ArgsTemplate, a.ArgsTemplate) {
				if !isKnownStaleArgsTemplate(a.ID, prior.ArgsTemplate) {
					a.ArgsTemplate = append([]string(nil), prior.ArgsTemplate...)
				}
			}
		}
		out = append(out, a)
		seen[a.ID] = true
	}
	return out, seen
}

// knownStaleArgsTemplates maps a terminal app id to the set of args_template
// shapes that gitbox itself seeded in earlier versions and now considers
// broken. On Sync, a persisted entry whose ArgsTemplate exactly matches one
// of these is refreshed from the current catalog instead of being preserved
// as a "user customisation". The exact-equality check is deliberate — a
// user who edited the template even slightly should still keep their edit.
var knownStaleArgsTemplates = map[string][][]string{
	// mintty: pre-issue-#71 the catalog included `-d {path}`. mintty's
	// `-d` is `--daemon`, not a directory flag, so the substituted repo
	// path falls through to the positional command-to-exec slot and
	// mintty exits with "No such file or directory" (exit 126). The new
	// catalog drops `-d {path}` and lets openTerminalRawAt set the
	// working directory via cmd.Dir on the non-console launch branch.
	"mintty": {
		{"-w", "max", "-d", "{path}", "--", "{shell_command}", "{shell_args}"},
	},
	// wezterm + alacritty on macOS: pre-#72-follow-up the catalog used
	// `open -a <App>` for every mac terminal. WezTerm and Alacritty don't
	// register as folder-openers in their Info.plist, so `open -a WezTerm
	// <folder>` either spawns the path as a positional command (WezTerm)
	// or surfaces "cannot open in 'folder' format" (Alacritty). The new
	// catalog probes the bundle's internal CLI binary (Alacritty) or
	// uses `open --args --cwd …` (WezTerm; direct exec of the CLI binary
	// doesn't bootstrap a GUI through macOS Launch Services).
	//
	// The second stale shape for wezterm — `["start", "--cwd", "{path}"]`
	// against a Command pointing at <bundle>/Contents/MacOS/wezterm — was
	// the first attempt at fixing it (74efc42). It looked plausible but
	// silently no-ops on macOS because that binary is the CLI multi-tool,
	// not a GUI launcher: invoked from a Cocoa app it can't register the
	// spawned wezterm-gui with the window server.
	"wezterm": {
		{"-a", "WezTerm"},
		{"start", "--cwd", "{path}"},
	},
	"alacritty": {
		{"-a", "Alacritty"},
	},
	// xterm on Linux: the previous template `-e {shell_command}
	// {shell_args}` assumed an explicit shell, but Unix profiles in the
	// v2.1 model carry implicit shell (composeUnix sets ShellID=""), so
	// both tokens collapse to zero items and xterm errors with "-e:
	// option requires argument". The catalog now ships an empty argv
	// for xterm; cmd.Dir = path handles cwd, $SHELL handles the shell.
	"xterm": {
		{"-e", "{shell_command}", "{shell_args}"},
	},
}

// isKnownStaleArgsTemplate reports whether `args` exactly matches one of
// the catalog's previous-revision templates for `id`. Used by mergeApps to
// gate args_template carry-forward.
func isKnownStaleArgsTemplate(id string, args []string) bool {
	for _, stale := range knownStaleArgsTemplates[id] {
		if argsEqual(stale, args) {
			return true
		}
	}
	return false
}

func mergeShells(det, prev []config.ShellEntry) ([]config.ShellEntry, map[string]bool) {
	priorByID := make(map[string]config.ShellEntry, len(prev))
	for _, s := range prev {
		priorByID[s.ID] = s
	}
	out := make([]config.ShellEntry, 0, len(det))
	seen := make(map[string]bool, len(det))
	for _, s := range det {
		if prior, ok := priorByID[s.ID]; ok {
			if prior.Name != "" {
				s.Name = prior.Name
			}
			if len(prior.Args) > 0 && !argsEqual(prior.Args, s.Args) {
				s.Args = append([]string(nil), prior.Args...)
			}
		}
		out = append(out, s)
		seen[s.ID] = true
	}
	return out, seen
}

func mergeProfiles(det, prev []config.TerminalProfile) ([]config.TerminalProfile, map[string]bool) {
	priorByID := make(map[string]config.TerminalProfile, len(prev))
	for _, p := range prev {
		priorByID[p.ID] = p
	}
	out := make([]config.TerminalProfile, 0, len(det))
	seen := make(map[string]bool, len(det))
	for _, p := range det {
		if prior, ok := priorByID[p.ID]; ok {
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
		out = append(out, p)
		seen[p.ID] = true
	}
	return out, seen
}

// EnforceSingleDefault keeps only the first Default flag in the slice. When
// no entry is marked Default and the slice is non-empty, the first non-hidden
// entry (or the first entry if all are hidden) is promoted so the launcher
// always has a default to call. Exposed so cmd/gui's SaveTerminalProfiles
// can normalize the user's payload before persisting.
func EnforceSingleDefault(profiles []config.TerminalProfile) {
	enforceSingleDefault(profiles)
}

// enforceSingleDefault is the internal worker behind EnforceSingleDefault;
// kept lowercase for use within this package's hot paths.
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
		// Promote the first non-hidden entry; fall back to index 0.
		for i, p := range profiles {
			if !p.Hidden {
				profiles[i].Default = true
				return
			}
		}
		profiles[0].Default = true
	}
}

// inCatalogTerminalIDs reports whether `id` is a stable id used by any of
// the per-OS terminal catalogs. User-added apps with custom ids fall outside
// this set and survive the merge as carry-forward entries. Stays in sync
// with the catalog tables in catalog.go.
//
// The deprecatedTerminalIDs set extends the check with ids that the legacy
// (pre-issue-#71) code emitted as Terminals but the new catalog reclassifies
// (e.g. "git-bash" — Git Bash is a Shell now, not a Terminal). Treating them
// as catalog-owned makes the merge drop them from prevApps when they're not
// re-detected, instead of carrying them forward as if user-added.
func inCatalogTerminalIDs(id string) bool {
	for _, list := range [][]CatalogTerminal{windowsTerminals, darwinTerminals, linuxTerminals} {
		for _, c := range list {
			if c.ID == id {
				return true
			}
		}
	}
	return deprecatedTerminalIDs[id]
}

// deprecatedTerminalIDs lists Terminal IDs the legacy code emitted but that
// the new catalog has reclassified. Anything in here gets dropped from the
// terminal_apps[] list on first Sync after upgrade so legacy entries do not
// pollute the Manager.
var deprecatedTerminalIDs = map[string]bool{
	// Issue #71: Git Bash is a Shell, not a Terminal. The integrated mintty
	// terminal that ships with Git for Windows is detected separately under
	// the "mintty" catalog id.
	"git-bash": true,
}

// inCatalogShellIDs reports whether `id` is a catalog shell id. Per-distro
// WSL ids ("wsl-<distro>") are treated as catalog ids since they're emitted
// by DetectShells and would otherwise be carried-forward forever.
func inCatalogShellIDs(id string) bool {
	for _, list := range [][]CatalogShell{windowsShells, darwinShells, linuxShells} {
		for _, c := range list {
			if c.ID == id {
				return true
			}
		}
	}
	if len(id) > 4 && id[:4] == "wsl-" {
		return true
	}
	return false
}

// argsEqual reports whether two argv slices are element-wise equal.
func argsEqual(a, b []string) bool {
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
