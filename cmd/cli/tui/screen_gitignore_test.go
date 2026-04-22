package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/gitignore"
	tea "github.com/charmbracelet/bubbletea"
)

// isolateGitignoreHome redirects HOME and GIT_CONFIG_GLOBAL into a temp
// dir so the gitignore screen never touches the real ~/.gitconfig or
// ~/.gitignore_global. Returns the resolved home dir.
func isolateGitignoreHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	return home
}

// drainGitignoreInit fires the screen's Init() once and feeds the
// resulting message back through Update so the model is fully loaded.
func drainGitignoreInit(t *testing.T, m gitignoreModel) gitignoreModel {
	t.Helper()
	cmd := m.Init()
	if cmd == nil {
		return m
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	return updated
}

func TestGitignoreScreen_FreshHome_NeedsAction(t *testing.T) {
	isolateGitignoreHome(t)
	root := newTestModel(t, t.TempDir()+"/cfg.json")

	g := newGitignoreModel(root.theme, 80, 24)
	g = drainGitignoreInit(t, g)

	if !g.loaded {
		t.Fatal("expected screen loaded after Init()")
	}
	if !g.status.NeedsAction {
		t.Errorf("expected NeedsAction=true on fresh home, got status=%+v", g.status)
	}
	view := g.View()
	if !strings.Contains(view, "Global Gitignore") {
		t.Errorf("View missing title:\n%s", view)
	}
	if !strings.Contains(view, "enter install") {
		t.Errorf("View missing install hint:\n%s", view)
	}
}

func TestGitignoreScreen_AlreadyInstalled_NoAction(t *testing.T) {
	isolateGitignoreHome(t)
	if _, err := gitignore.Install(); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	root := newTestModel(t, t.TempDir()+"/cfg.json")
	g := newGitignoreModel(root.theme, 80, 24)
	g = drainGitignoreInit(t, g)

	if g.status.NeedsAction {
		t.Errorf("expected NeedsAction=false after install, got %+v", g.status)
	}
	view := g.View()
	if !strings.Contains(view, "up to date") {
		t.Errorf("View missing 'up to date' state:\n%s", view)
	}
}

func TestGitignoreScreen_EnterInstallsAndShowsResult(t *testing.T) {
	home := isolateGitignoreHome(t)
	root := newTestModel(t, t.TempDir()+"/cfg.json")

	g := newGitignoreModel(root.theme, 80, 24)
	g = drainGitignoreInit(t, g)

	if !g.status.NeedsAction {
		t.Fatalf("expected NeedsAction=true before install")
	}

	// First Enter primes confirmPending.
	updated, _ := g.Update(tea.KeyMsg{Type: tea.KeyEnter})
	g = updated
	if !g.confirmPending {
		t.Fatalf("expected confirmPending=true after first Enter")
	}

	// Second Enter triggers installGitignoreCmd; run it and feed the result back.
	_, cmd := g.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected install cmd after confirm Enter")
	}
	msg := cmd()
	g = func() gitignoreModel {
		// Update returns a model + new cmd (the post-install re-check).
		// We feed both back through Update so the screen's status reflects
		// the freshly-installed state.
		gNext, follow := g.Update(msg)
		gNext.installing = false
		if follow != nil {
			if checkMsg := follow(); checkMsg != nil {
				gNext, _ = gNext.Update(checkMsg)
			}
		}
		return gNext
	}()

	if g.installErr != "" {
		t.Fatalf("install error: %s", g.installErr)
	}
	if g.result == nil || !g.result.Updated {
		t.Errorf("expected result.Updated=true, got %+v", g.result)
	}
	if g.status.NeedsAction {
		t.Errorf("expected NeedsAction=false after install, got %+v", g.status)
	}
	if _, err := os.Stat(filepath.Join(home, ".gitignore_global")); err != nil {
		t.Errorf("expected ~/.gitignore_global to exist after install: %v", err)
	}
}

func TestDashboard_GitignoreHint_AppearsWhenNeeded(t *testing.T) {
	isolateGitignoreHome(t)
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Manually deliver the async gitignore status check result.
	s, err := gitignore.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	m = sendMsg(m, gitignoreStatusMsg{status: gitignoreStatusInfoFromPkg(s)})

	if !m.dashboard.gitignoreNeedsAction {
		t.Fatalf("expected gitignoreNeedsAction=true after fresh-home check")
	}
	view := m.View()
	if !strings.Contains(view, "G gitignore!") {
		t.Errorf("expected dashboard footer to surface 'G gitignore!' hint:\n%s", view)
	}
}
