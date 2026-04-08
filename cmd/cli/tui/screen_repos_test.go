package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestReposModel_OpenBrowser(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Navigate to repos screen via switchScreenMsg.
	m = sendMsg(m, switchScreenMsg{
		screen:    screenRepos,
		sourceKey: "alice-repos",
		repoKey:   "alice/hello-world",
	})

	if m.screen != screenRepos {
		t.Fatalf("expected screenRepos, got %d", m.screen)
	}

	// View should show the "b open browser" hint.
	view := m.View()
	if !strings.Contains(view, "b open browser") {
		t.Errorf("repos View missing 'b open browser' hint")
	}
}

func TestReposModel_OpenBrowser_Keybinding(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{
		screen:    screenRepos,
		sourceKey: "alice-repos",
		repoKey:   "alice/hello-world",
	})

	// Press "b" to open browser — should produce a tea.Cmd (async).
	m = sendKey(m, "b")

	// The openBrowserDoneMsg should set resultMsg on success.
	m = sendMsg(m, openBrowserDoneMsg{err: nil})
	if m.repos.resultMsg != "Opened in browser." {
		t.Errorf("expected resultMsg 'Opened in browser.', got %q", m.repos.resultMsg)
	}
}

func TestRepoWebURL_ViaConfig(t *testing.T) {
	// Verify the URL construction matches config data.
	cfg := newDummyConfig(t, "/tmp/test-git")
	source := cfg.Sources["alice-repos"]
	acct := cfg.Accounts[source.Account]

	want := "https://github.com/alice/hello-world"
	got := acct.URL + "/" + "alice/hello-world"
	if got != want {
		t.Errorf("web URL = %q, want %q", got, want)
	}
}

func TestReposModel_OpenBrowser_SelfHosted(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Navigate to the forgejo (self-hosted) repo.
	m = sendMsg(m, switchScreenMsg{
		screen:    screenRepos,
		sourceKey: "bob-repos",
		repoKey:   "bob/my-project",
	})

	m = sendKey(m, "b")

	// Simulate success.
	m = sendMsg(m, openBrowserDoneMsg{err: nil})
	if m.repos.resultMsg != "Opened in browser." {
		t.Errorf("expected resultMsg 'Opened in browser.', got %q", m.repos.resultMsg)
	}
}

func TestReposModel_OpenBrowser_Error(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	cfg.Sources["err-repos"] = config.Source{
		Account: "github-alice",
		Repos:   map[string]config.Repo{"alice/fail-repo": {}},
	}
	cfg.SourceOrder = append(cfg.SourceOrder, "err-repos")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{
		screen:    screenRepos,
		sourceKey: "err-repos",
		repoKey:   "alice/fail-repo",
	})

	m = sendKey(m, "b")

	// Simulate browser open failure.
	m = sendMsg(m, openBrowserDoneMsg{err: fmt.Errorf("no browser found")})
	if m.repos.errMsg != "no browser found" {
		t.Errorf("expected errMsg 'no browser found', got %q", m.repos.errMsg)
	}
}
