package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestAccount_Render(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Navigate to account detail for github-alice.
	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})
	if m.screen != screenAccount {
		t.Fatalf("expected screenAccount, got %d", m.screen)
	}

	view := m.View()
	// Should show account details.
	for _, want := range []string{"github-alice", "github", "alice"} {
		if !strings.Contains(view, want) {
			t.Errorf("account View missing %q", want)
		}
	}
}

func TestAccount_OpenBrowser_Keybinding(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})

	m = sendKey(m, "b")

	m = sendMsg(m, openBrowserDoneMsg{err: nil})
	if m.account.statusMsg != "Opened in browser." {
		t.Errorf("expected statusMsg 'Opened in browser.', got %q", m.account.statusMsg)
	}
}

func TestAccount_OpenBrowser_Error(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})
	m = sendKey(m, "b")

	m = sendMsg(m, openBrowserDoneMsg{err: fmt.Errorf("no browser found")})
	if m.account.errMsg != "no browser found" {
		t.Errorf("expected errMsg 'no browser found', got %q", m.account.errMsg)
	}
}

func TestAccount_OpenFolder_MissingErrors(t *testing.T) {
	// Account folder does not exist on disk — openAccountFolderCmd should
	// return an openFolderDoneMsg carrying an error, surfaced as m.errMsg.
	gitFolder := filepath.Join(t.TempDir(), "never-created")
	cfg := newDummyConfig(t, gitFolder)
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})

	// Pressing `o` returns a tea.Cmd; run it and feed the resulting msg back.
	cmd := openAccountFolderCmd(filepath.Join(gitFolder, "github-alice"))
	msg := cmd()
	m = sendMsg(m, msg)

	if !strings.Contains(m.account.errMsg, "does not exist") {
		t.Errorf("expected errMsg about missing folder, got %q", m.account.errMsg)
	}
}

func TestAccount_OpenFolder_SuccessSetsStatus(t *testing.T) {
	// Create the account folder; openAccountFolderCmd should stat-succeed and
	// attempt to launch the file manager. The Start() error is returned as
	// the command result's err — on CI / non-desktop boxes xdg-open / open /
	// explorer may or may not exist. We accept either: success sets
	// statusMsg, failure sets errMsg with something other than "does not
	// exist".
	gitFolder := t.TempDir()
	cfg := newDummyConfig(t, gitFolder)
	// Create <gitFolder>/github-alice.
	if err := os.MkdirAll(filepath.Join(gitFolder, "github-alice"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})

	cmd := openAccountFolderCmd(filepath.Join(gitFolder, "github-alice"))
	msg := cmd()
	m = sendMsg(m, msg)

	if strings.Contains(m.account.errMsg, "does not exist") {
		t.Errorf("folder exists but errMsg says it doesn't: %q", m.account.errMsg)
	}
}

func TestAccount_LowercaseO_StillOpensFolder(t *testing.T) {
	// Regression guard for the `o` / `O` split: lowercase o must remain
	// bound to "open folder" even after the launcher work, preserving the
	// existing muscle memory from #39.
	gitFolder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gitFolder, "github-alice"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := newDummyConfig(t, gitFolder)
	cfg.Global.Terminals = []config.TerminalEntry{{Name: "Git Bash", Command: "/bin/wt"}}
	cfg.Global.AIHarnesses = []config.AIHarnessEntry{{Name: "Claude", Command: "/bin/claude"}}
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})
	m = sendKey(m, "o")

	if m.account.launcher.active {
		t.Error("lowercase 'o' activated launcher overlay; it must stay bound to open-folder")
	}
}

func TestAccount_CapitalO_OpensLauncher(t *testing.T) {
	// Capital O is the new launcher key on the account screen. Folder must
	// exist so the guard doesn't bail before the overlay activates.
	gitFolder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gitFolder, "github-alice"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := newDummyConfig(t, gitFolder)
	cfg.Global.Terminals = []config.TerminalEntry{{Name: "Git Bash", Command: "/bin/wt"}}
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})
	m = sendKey(m, "O")

	if !m.account.launcher.active {
		t.Fatal("capital 'O' did not activate launcher overlay")
	}

	// Overlay path should be <gitFolder>/github-alice.
	wantPath := filepath.Join(gitFolder, "github-alice")
	if m.account.launcher.path != wantPath {
		t.Errorf("launcher path = %q, want %q", m.account.launcher.path, wantPath)
	}

	// Esc closes without dispatching a command.
	m = sendKey(m, "esc")
	if m.account.launcher.active {
		t.Error("esc did not close the launcher overlay")
	}
}

func TestAccount_CapitalO_GuardsMissingFolder(t *testing.T) {
	// When <global.folder>/<accountKey> doesn't exist, the overlay must
	// refuse with a clear hint rather than activate and then dispatch
	// launch commands that would fail against a nonexistent working dir.
	cfg := newDummyConfig(t, filepath.Join(t.TempDir(), "never-created"))
	cfg.Global.Terminals = []config.TerminalEntry{{Name: "Git Bash", Command: "/bin/wt"}}
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})
	m = sendKey(m, "O")

	if m.account.launcher.active {
		t.Error("launcher activated despite missing account folder")
	}
	if !strings.Contains(m.account.errMsg, "does not exist") {
		t.Errorf("expected guard errMsg, got %q", m.account.errMsg)
	}
}

func TestAccount_Lowercase_e_StillOpensEditForm(t *testing.T) {
	// Regression guard: adding launcher keys must not steal lowercase `e`
	// from the existing edit-form shortcut.
	cfg := newDummyConfig(t, "/tmp/test-git")
	cfg.Global.Editors = []config.EditorEntry{{Name: "VS Code", Command: "/bin/code"}}
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})
	m = sendKey(m, "e")

	if m.account.view != accountViewEdit {
		t.Errorf("lowercase 'e' did not open edit form; view = %d", m.account.view)
	}
}

func TestAccount_BackToDashboard(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Navigate to account screen.
	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})

	// Press Esc → should go back to dashboard.
	m = sendMsg(m, switchScreenMsg{screen: screenDashboard})
	if m.screen != screenDashboard {
		t.Errorf("expected dashboard after back, got %d", m.screen)
	}
}
