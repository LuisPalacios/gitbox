package terminals

import (
	"github.com/LuisPalacios/gitbox/pkg/config"
)

// Sync reconciles cfg.Global.TerminalApps + Shells + TerminalProfiles with
// what the catalog says is installed on the host. The sync is idempotent —
// it returns false when the resulting JSON would be byte-identical, so the
// caller can skip a config.Save round-trip.
//
// Pipeline:
//
//  1. Detect installed apps + shells from the catalog.
//  2. Compose auto-derived Profiles per OS rules (Windows: T×S, mac/Linux:
//     T-only, Windows fallback: bare-shells when no modern Terminal).
//  3. Discover Windows Terminal settings.json profiles (Windows).
//  4. Discover WezTerm launch_menu entries (any OS).
//  5. Merge with the existing config — preserves user toggles, hand-edits,
//     user-added entries, migrated v2.0 entries.
//  6. Mark a sensible Default if none survives the merge.
//  7. Diff the result against cfg; only mutate when changed.
//
// `goos` is taken as a parameter so tests can exercise all three OS branches
// from any host (matches the same pattern in compose.go).
func Sync(cfg *config.Config, goos string) bool {
	if cfg == nil {
		return false
	}
	apps := DetectTerminals(goos)
	shells := DetectShells(goos)
	composed := ComposeProfiles(apps, shells, goos)

	profiles := composed
	if goos == "windows" {
		if wt := DiscoverWTProfiles(); len(wt) > 0 {
			profiles = mergeProfilesByID(wt, profiles)
		}
	}
	if wez := DiscoverWeztermProfiles(); len(wez) > 0 {
		profiles = mergeProfilesByID(wez, profiles)
	}

	if defaultProfileMissing(cfg.Global.TerminalProfiles) {
		applyInitialDefault(profiles, goos)
	}

	merged := MergeWithExisting(apps, shells, profiles, cfg.Global.TerminalApps,
		cfg.Global.Shells, cfg.Global.TerminalProfiles)

	if profilesPayloadEqual(merged.Apps, merged.Shells, merged.Profiles,
		cfg.Global.TerminalApps, cfg.Global.Shells, cfg.Global.TerminalProfiles) {
		return false
	}
	cfg.Global.TerminalApps = merged.Apps
	cfg.Global.Shells = merged.Shells
	cfg.Global.TerminalProfiles = merged.Profiles
	return true
}

// mergeProfilesByID appends `additions` to `base`, dropping additions whose
// id collides with an existing base entry. Order is preserved so the caller
// controls relative priority. WT-imports + WezTerm-imports are layered on
// top of the catalog-composed set so WT-profile-derived rows win id
// collisions when applicable.
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

// defaultProfileMissing reports whether none of `profiles` carries Default.
func defaultProfileMissing(profiles []config.TerminalProfile) bool {
	for _, p := range profiles {
		if p.Default {
			return false
		}
	}
	return true
}

// applyInitialDefault picks a sensible Default among newly-composed profiles
// when the persisted set has no Default. On Unix, prefer a Profile whose
// (implicit) shell matches the login shell — but composeUnix emits T-only
// rows so the caller has no shell-id signal; we just pick the first row.
// On Windows, prefer the row with the modern Terminal × login-shell-id pair.
func applyInitialDefault(profiles []config.TerminalProfile, goos string) {
	if len(profiles) == 0 {
		return
	}
	if shellID, ok := LoginShellID(goos); ok && goos == "windows" {
		for i := range profiles {
			if profiles[i].ShellID == shellID {
				profiles[i].Default = true
				return
			}
		}
	}
	profiles[0].Default = true
}

// profilesPayloadEqual reports whether two (apps, shells, profiles) triples
// are byte-identical. Used to short-circuit Sync when nothing changed.
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
		argsEqual(a.ArgsTemplate, b.ArgsTemplate)
}

func shellsEqual(a, b config.ShellEntry) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Command == b.Command &&
		argsEqual(a.Args, b.Args)
}

func profilesEqual(a, b config.TerminalProfile) bool {
	return a.ID == b.ID && a.Name == b.Name && a.TerminalID == b.TerminalID &&
		a.ShellID == b.ShellID && a.Default == b.Default && a.Preferred == b.Preferred &&
		a.Hidden == b.Hidden && a.Source == b.Source && argsEqual(a.Args, b.Args)
}
