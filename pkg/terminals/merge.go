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

	// Carry-forward: user-added + migrated profiles whose ID isn't in the
	// detected set this round. Hidden / renamed / arg-overrides came along
	// with the standard merge above already.
	for _, p := range prevProfiles {
		if seenProfile[p.ID] {
			continue
		}
		if p.Source == "user" || p.Source == "migrated" {
			mergedProfiles = append(mergedProfiles, p)
		}
	}

	// Carry-forward: user-added apps + shells whose ID isn't in the detected
	// set this round, so their dependent Profiles still launch.
	for _, a := range prevApps {
		if seenApp[a.ID] {
			continue
		}
		if !inCatalogTerminalIDs(a.ID) {
			mergedApps = append(mergedApps, a)
		}
	}
	for _, s := range prevShells {
		if seenShell[s.ID] {
			continue
		}
		if !inCatalogShellIDs(s.ID) {
			mergedShells = append(mergedShells, s)
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
			if len(prior.ArgsTemplate) > 0 && !argsEqual(prior.ArgsTemplate, a.ArgsTemplate) {
				a.ArgsTemplate = append([]string(nil), prior.ArgsTemplate...)
			}
		}
		out = append(out, a)
		seen[a.ID] = true
	}
	return out, seen
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
func inCatalogTerminalIDs(id string) bool {
	for _, list := range [][]CatalogTerminal{windowsTerminals, darwinTerminals, linuxTerminals} {
		for _, c := range list {
			if c.ID == id {
				return true
			}
		}
	}
	return false
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
