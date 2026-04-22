package tui

import (
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/move"
)

// TestMoveRepoModel_EntryGuard_NotCloned covers the friendly refusal
// when the user presses M on a repo that has never been cloned.
func TestMoveRepoModel_EntryGuard_NotCloned(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git-never-created")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)
	m = sendMsg(m, switchScreenMsg{
		screen:    screenRepos,
		sourceKey: "alice-repos",
		repoKey:   "alice/hello-world",
	})

	m = sendKey(m, "M")

	if m.screen == screenMoveRepo {
		t.Fatal("expected screen NOT to switch when clone is missing")
	}
	if !strings.Contains(m.repos.errMsg, "Clone the repo first") {
		t.Errorf("expected clone-first hint, got %q", m.repos.errMsg)
	}
}

// TestMoveRepoModel_EmptyState: when only one account is configured,
// the model should show a helpful empty-state rather than crashing.
func TestMoveRepoModel_EmptyState(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	// Keep only alice — drop bob.
	for k := range cfg.Accounts {
		if k != "github-alice" {
			delete(cfg.Accounts, k)
		}
	}
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Jump straight to the move screen.
	m = sendMsg(m, switchScreenMsg{
		screen:    screenMoveRepo,
		sourceKey: "alice-repos",
		repoKey:   "alice/hello-world",
		repoPath:  "/tmp/test-git/alice-repos/alice/hello-world",
	})

	if m.screen != screenMoveRepo {
		t.Fatalf("expected screenMoveRepo, got %d", m.screen)
	}
	view := m.View()
	if !strings.Contains(view, "No other accounts are configured") {
		t.Errorf("expected empty-state message in view, got:\n%s", view)
	}
}

// TestPhaseLabel sanity-checks the phase-to-human mapping.
func TestPhaseLabel(t *testing.T) {
	cases := map[move.Phase]string{
		move.PhasePreflight:    "Preflight",
		move.PhasePushMirror:   "Push mirror",
		move.PhaseUpdateConfig: "Update config",
		move.PhaseDone:         "Done",
	}
	for in, want := range cases {
		if got := phaseLabel(in); got != want {
			t.Errorf("phaseLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
