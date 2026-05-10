package terminals

import (
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// TestMergePreservesUserToggles is the core invariant: if the user marked a
// Profile Hidden / Default / Preferred / renamed it, those flags survive a
// re-detect even when the catalog re-emits the row from scratch.
func TestMergePreservesUserToggles(t *testing.T) {
	det := []config.TerminalProfile{
		{ID: "wt+pwsh", Name: "Windows Terminal — PowerShell 7", TerminalID: "wt", ShellID: "pwsh", Source: "detected"},
	}
	prev := []config.TerminalProfile{
		{ID: "wt+pwsh", Name: "Custom name", TerminalID: "wt", ShellID: "pwsh",
			Default: true, Preferred: true, Hidden: false, Source: "detected",
			Args: []string{"-d", "{path}", "--always-on-top"}},
	}
	got := MergeWithExisting(nil, nil, det, nil, nil, prev)
	if len(got.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(got.Profiles))
	}
	p := got.Profiles[0]
	if p.Name != "Custom name" {
		t.Errorf("name lost: got %q", p.Name)
	}
	if !p.Default || !p.Preferred {
		t.Errorf("flags lost: default=%v preferred=%v", p.Default, p.Preferred)
	}
	if len(p.Args) != 3 || p.Args[2] != "--always-on-top" {
		t.Errorf("args lost: %+v", p.Args)
	}
}

// TestMergeCarriesUserAddedProfiles is the contract that user-added Profiles
// (Source = "user") survive even when the detected set drops them — they
// are not catalog-derived, so re-detect can't bring them back.
func TestMergeCarriesUserAddedProfiles(t *testing.T) {
	det := []config.TerminalProfile{
		{ID: "wt+pwsh", Name: "WT — PWSH", TerminalID: "wt", ShellID: "pwsh", Source: "detected"},
	}
	prev := []config.TerminalProfile{
		{ID: "my-custom", Name: "My pet profile", TerminalID: "wt", ShellID: "fish", Source: "user"},
	}
	got := MergeWithExisting(nil, nil, det, nil, nil, prev)
	if len(got.Profiles) != 2 {
		t.Fatalf("expected 2 profiles (1 detected + 1 user), got %d: %+v", len(got.Profiles), got.Profiles)
	}
	found := false
	for _, p := range got.Profiles {
		if p.ID == "my-custom" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("user Profile dropped from merge")
	}
}

// TestMergeCarriesMigratedProfiles guards the v2.0-migration safety net:
// when the migrator stamps a legacy bare-shell-on-Windows entry as
// Source="migrated", it must survive even if the new composition rules
// (which suppress bare-shell auto-Profiles) don't re-emit it.
func TestMergeCarriesMigratedProfiles(t *testing.T) {
	det := []config.TerminalProfile{} // composition emits nothing for this case
	prev := []config.TerminalProfile{
		{ID: "legacy-pwsh", Name: "PowerShell", TerminalID: "legacy-pwsh", Source: "migrated"},
	}
	got := MergeWithExisting(nil, nil, det, nil, nil, prev)
	if len(got.Profiles) != 1 {
		t.Fatalf("expected 1 carried-forward migrated profile, got %d", len(got.Profiles))
	}
	if got.Profiles[0].ID != "legacy-pwsh" {
		t.Errorf("wrong carry-forward: %+v", got.Profiles[0])
	}
}

// TestMergeDropsStaleDetectedProfiles ensures Source="detected" entries the
// host can no longer reach are dropped — that's the whole point of re-detect.
func TestMergeDropsStaleDetectedProfiles(t *testing.T) {
	det := []config.TerminalProfile{
		{ID: "wt+pwsh", Name: "WT — PWSH", TerminalID: "wt", ShellID: "pwsh", Source: "detected"},
	}
	prev := []config.TerminalProfile{
		{ID: "wt+pwsh", Name: "WT — PWSH", TerminalID: "wt", ShellID: "pwsh", Source: "detected"},
		{ID: "alacritty+pwsh", Name: "Alacritty — PWSH", TerminalID: "alacritty", ShellID: "pwsh", Source: "detected"},
	}
	got := MergeWithExisting(nil, nil, det, nil, nil, prev)
	if len(got.Profiles) != 1 {
		t.Fatalf("expected 1 (alacritty dropped), got %d", len(got.Profiles))
	}
	if got.Profiles[0].ID != "wt+pwsh" {
		t.Errorf("wrong survivor: %+v", got.Profiles[0])
	}
}

// TestMergeUserAddedAppSurvives covers the carry-forward path for apps —
// user adds a custom TerminalApp not in the catalog, and a re-detect must
// keep it (otherwise their bound user Profiles can no longer launch).
func TestMergeUserAddedAppSurvives(t *testing.T) {
	det := []config.TerminalApp{
		{ID: "wt", Name: "Windows Terminal", Command: "wt.exe"},
	}
	prev := []config.TerminalApp{
		{ID: "wt", Name: "Windows Terminal", Command: "wt.exe"},
		{ID: "my-custom-term", Name: "Custom Terminal", Command: "/opt/my-term"},
	}
	got := MergeWithExisting(det, nil, nil, prev, nil, nil)
	if len(got.Apps) != 2 {
		t.Fatalf("expected 2 apps (1 detected + 1 user), got %d: %+v", len(got.Apps), got.Apps)
	}
}

// TestEnforceSingleDefault keeps exactly one Default in the resulting slice.
func TestEnforceSingleDefault(t *testing.T) {
	profiles := []config.TerminalProfile{
		{ID: "a", Default: true},
		{ID: "b", Default: true},
		{ID: "c", Default: true},
	}
	enforceSingleDefault(profiles)
	cnt := 0
	for _, p := range profiles {
		if p.Default {
			cnt++
		}
	}
	if cnt != 1 {
		t.Errorf("expected 1 Default, got %d", cnt)
	}
}

// TestEnforceSingleDefaultPromotesFirstNonHidden when no entry is Default,
// the first non-hidden one is promoted to Default — keeps the launcher
// usable on a fresh config without surprising the user with a Hidden-row
// default.
func TestEnforceSingleDefaultPromotesFirstNonHidden(t *testing.T) {
	profiles := []config.TerminalProfile{
		{ID: "a", Hidden: true},
		{ID: "b", Hidden: false},
	}
	enforceSingleDefault(profiles)
	if profiles[0].Default {
		t.Error("expected hidden row to NOT be promoted")
	}
	if !profiles[1].Default {
		t.Error("expected non-hidden row to be promoted")
	}
}
