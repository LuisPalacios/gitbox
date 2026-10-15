package terminals

import (
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// TestSyncIdempotent — running Sync twice on the same input must not flip
// `changed` back to true on the second call. Detection produces deterministic
// output and merge preserves that, so a no-op cycle is the contract.
//
// This test does not actually probe the host (Probe results depend on the
// machine running the tests). Instead it exercises the merge pipeline directly
// with a pre-populated cfg + composed Profiles via a fake Sync invocation.
//
// Real probe behaviour is exercised in detect_test.go (where present).
func TestSyncIdempotent(t *testing.T) {
	// Build a cfg that already matches what a hypothetical Sync would emit:
	// 1 T×S pair + 1 hidden DIRECT bare-pwsh Profile (issue #71).
	cfg := &config.Config{}
	cfg.Global.TerminalApps = []config.TerminalApp{fakeWTApp}
	cfg.Global.Shells = []config.ShellEntry{fakePwshShell}
	cfg.Global.TerminalProfiles = []config.TerminalProfile{
		{ID: "wt+pwsh", Name: "Windows Terminal — PowerShell 7", TerminalID: "wt", ShellID: "pwsh", Source: "detected", Default: true},
		{ID: "bare-pwsh", Name: "PowerShell 7 (direct)", TerminalID: "", ShellID: "pwsh", Source: "detected", Hidden: true},
	}

	merged := MergeWithExisting(
		[]config.TerminalApp{fakeWTApp},
		[]config.ShellEntry{fakePwshShell},
		ComposeProfiles([]config.TerminalApp{fakeWTApp}, []config.ShellEntry{fakePwshShell}, "windows"),
		cfg.Global.TerminalApps,
		cfg.Global.Shells,
		cfg.Global.TerminalProfiles,
	)

	if !profilesPayloadEqual(merged.Apps, merged.Shells, merged.Profiles,
		cfg.Global.TerminalApps, cfg.Global.Shells, cfg.Global.TerminalProfiles) {
		t.Errorf("expected merged payload to be byte-identical to existing cfg, got %+v", merged.Profiles)
	}
}

// TestSyncHiddenSurvivesCatalogGrowth — the user hides Mintty; on the next
// boot the catalog has grown to include Ghostty (or whatever). Sync must
// pick up Ghostty as visible AND keep Mintty hidden.
func TestSyncHiddenSurvivesCatalogGrowth(t *testing.T) {
	prevApps := []config.TerminalApp{fakeWTApp}
	prevShells := []config.ShellEntry{fakePwshShell}
	prevProfiles := []config.TerminalProfile{
		{ID: "wt+pwsh", Name: "WT — PWSH", TerminalID: "wt", ShellID: "pwsh", Source: "detected", Hidden: true},
	}
	// Detection now also reports a freshly-installed Alacritty (catalog growth).
	detApps := []config.TerminalApp{
		fakeWTApp,
		{ID: "alacritty", Name: "Alacritty", Command: "alacritty.exe", ArgsTemplate: fakeWTApp.ArgsTemplate},
	}
	detProfiles := ComposeProfiles(detApps, prevShells, "windows")

	merged := MergeWithExisting(detApps, prevShells, detProfiles, prevApps, prevShells, prevProfiles)

	// Hidden flag preserved on wt+pwsh.
	hiddenSeen := false
	alacrittySeen := false
	for _, p := range merged.Profiles {
		if p.ID == "wt+pwsh" && p.Hidden {
			hiddenSeen = true
		}
		if p.ID == "alacritty+pwsh" && !p.Hidden {
			alacrittySeen = true
		}
	}
	if !hiddenSeen {
		t.Error("expected Hidden=true to survive on wt+pwsh")
	}
	if !alacrittySeen {
		t.Error("expected new alacritty+pwsh to appear unhidden")
	}
}

// TestSyncWTDiscoveredCollidesWithCatalog — when WT settings.json discovery
// emits a Profile with the same ID as a catalog-composed one, mergeProfilesByID
// must keep the base (catalog-composed) entry, not the addition. This is the
// "WT user-named profiles win id collisions" guarantee from the issue.
func TestSyncWTDiscoveredCollidesWithCatalog(t *testing.T) {
	// Both share the same id "wt+pwsh".
	base := []config.TerminalProfile{
		{ID: "wt+pwsh", Name: "Catalog row", Source: "detected"},
	}
	additions := []config.TerminalProfile{
		{ID: "wt+pwsh", Name: "WT-discovered row", Source: "wt-profile"},
	}
	got := mergeProfilesByID(additions, base)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry after collision, got %d", len(got))
	}
	if got[0].Name != "Catalog row" || got[0].Source != "detected" {
		t.Errorf("expected base to win, got %+v", got[0])
	}
}

// TestSyncAddsToEmptyConfig covers the fresh-install path: cfg starts with
// empty arrays, Sync must populate them and report changed=true.
func TestSyncAddsToEmptyConfig(t *testing.T) {
	cfg := &config.Config{}
	merged := MergeWithExisting(
		[]config.TerminalApp{fakeWTApp},
		[]config.ShellEntry{fakePwshShell},
		ComposeProfiles([]config.TerminalApp{fakeWTApp}, []config.ShellEntry{fakePwshShell}, "windows"),
		cfg.Global.TerminalApps,
		cfg.Global.Shells,
		cfg.Global.TerminalProfiles,
	)
	if profilesPayloadEqual(merged.Apps, merged.Shells, merged.Profiles,
		cfg.Global.TerminalApps, cfg.Global.Shells, cfg.Global.TerminalProfiles) {
		t.Error("expected merged payload to differ from empty cfg")
	}
	// 1 T×S pair (wt+pwsh) + 1 hidden DIRECT bare-pwsh (issue #71).
	if len(merged.Apps) != 1 || len(merged.Shells) != 1 || len(merged.Profiles) != 2 {
		t.Errorf("unexpected counts: apps=%d shells=%d profiles=%d", len(merged.Apps), len(merged.Shells), len(merged.Profiles))
	}
}

func TestApplyDetection_PreservesEditsDuringDetection(t *testing.T) {
	detected := MergePayload{
		Apps:     []config.TerminalApp{fakeWTApp},
		Shells:   []config.ShellEntry{fakePwshShell},
		Profiles: ComposeProfiles([]config.TerminalApp{fakeWTApp}, []config.ShellEntry{fakePwshShell}, "windows"),
	}
	cfg := &config.Config{}
	ApplyDetection(cfg, detected, "windows")
	// These changes represent edits saved after detection started.
	cfg.Global.TerminalApps[0].Name = "My terminal"
	cfg.Global.TerminalProfiles[0].Name = "My profile"
	cfg.Global.TerminalProfiles[0].Hidden = true
	cfg.Global.TerminalProfiles[0].Preferred = true
	cfg.Global.TerminalProfiles = append(cfg.Global.TerminalProfiles, config.TerminalProfile{
		ID: "custom", Name: "Custom", Source: "user", Default: true,
	})
	cfg.Global.TerminalProfiles[0].Default = false
	if ApplyDetection(cfg, detected, "windows") {
		t.Fatal("unchanged detection should preserve current edits without saving")
	}
	if cfg.Global.TerminalApps[0].Name != "My terminal" || cfg.Global.TerminalProfiles[0].Name != "My profile" ||
		!cfg.Global.TerminalProfiles[0].Hidden || !cfg.Global.TerminalProfiles[0].Preferred {
		t.Fatal("current customizations lost")
	}
	last := cfg.Global.TerminalProfiles[len(cfg.Global.TerminalProfiles)-1]
	if last.ID != "custom" || !last.Default {
		t.Fatalf("current user profile/default lost: %+v", last)
	}
}

func TestApplyDetection_InitialDefaultDoesNotMutateDetection(t *testing.T) {
	detected := MergePayload{Profiles: []config.TerminalProfile{{ID: "first", Source: "detected"}}}
	cfg := &config.Config{}
	if !ApplyDetection(cfg, detected, "linux") || !cfg.Global.TerminalProfiles[0].Default {
		t.Fatal("initial detection must set a default")
	}
	if detected.Profiles[0].Default {
		t.Fatal("applying detection must not mutate reusable detection results")
	}
	if ApplyDetection(cfg, detected, "linux") {
		t.Fatal("second identical detection must not write config")
	}
}
