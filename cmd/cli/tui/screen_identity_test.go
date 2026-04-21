package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	tea "github.com/charmbracelet/bubbletea"
)

// isolateGitconfig redirects "git config --global" at a sandbox file for the
// test. Mirrors the helper used in pkg/credential/gcm_config_test.go.
func isolateGitconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".gitconfig")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("creating sandbox gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", path)
	return path
}

func navigateToIdentityScreen(t *testing.T, cfgPath string) model {
	t.Helper()
	m := newTestModel(t, cfgPath)
	m = initModel(t, m)
	m = sendMsg(m, switchScreenMsg{screen: screenIdentity})
	if m.screen != screenIdentity {
		t.Fatalf("expected screenIdentity, got %d", m.screen)
	}
	return m
}

func TestIdentity_NoWarningsWhenCleanAndNoGCM(t *testing.T) {
	isolateGitconfig(t)
	cfg := newDummyConfig(t, t.TempDir()) // token accounts only
	env := setupTestEnvWithConfig(t, cfg)

	m := navigateToIdentityScreen(t, env.CfgPath)
	// Drive the init batch commands.
	m = sendMsg(m, globalIdentityMsg{hasName: false, hasEmail: false})
	m = sendMsg(m, globalGCMMsg{
		needed: credential.IsGlobalGCMConfigNeeded(cfg),
		status: credential.CheckGlobalGCMConfig(cfg),
	})

	view := m.identity.View()
	if !strings.Contains(view, "No global identity set") {
		t.Errorf("expected clean identity message, got:\n%s", view)
	}
	// No GCM account → GCM section should be hidden entirely.
	if strings.Contains(view, "Credential helper (GCM)") {
		t.Errorf("GCM section should be hidden when no GCM account exists; got:\n%s", view)
	}
}

func TestIdentity_GCMSectionShownWhenNeededAndMissing(t *testing.T) {
	isolateGitconfig(t)
	cfg := newGCMConfig(t, t.TempDir()) // one GCM account; Global.CredentialGCM nil
	env := setupTestEnvWithConfig(t, cfg)

	m := navigateToIdentityScreen(t, env.CfgPath)
	m = sendMsg(m, globalIdentityMsg{hasName: false, hasEmail: false})
	m = sendMsg(m, globalGCMMsg{
		needed: credential.IsGlobalGCMConfigNeeded(cfg),
		status: credential.CheckGlobalGCMConfig(cfg),
	})

	view := m.identity.View()
	if !strings.Contains(view, "Credential helper (GCM)") {
		t.Errorf("expected GCM section header in view, got:\n%s", view)
	}
	if !strings.Contains(view, "missing GCM settings") {
		t.Errorf("expected 'missing GCM settings' warning, got:\n%s", view)
	}
	if !strings.Contains(view, "(missing)") {
		t.Errorf("expected (missing) marker next to expected values, got:\n%s", view)
	}
}

func TestIdentity_GCMFixFlow(t *testing.T) {
	gitconfigPath := isolateGitconfig(t)
	cfg := newGCMConfig(t, t.TempDir())
	env := setupTestEnvWithConfig(t, cfg)

	m := navigateToIdentityScreen(t, env.CfgPath)
	m = sendMsg(m, globalIdentityMsg{})
	m = sendMsg(m, globalGCMMsg{
		needed: true,
		status: credential.CheckGlobalGCMConfig(cfg),
	})

	// Focus the GCM section.
	m = sendSpecialKey(m, tea.KeyDown)
	if m.identity.activeSection != sectionGCM {
		t.Fatalf("expected sectionGCM after Down, got %d", m.identity.activeSection)
	}

	// Press Enter — enters confirm state.
	m = sendSpecialKey(m, tea.KeyEnter)
	if !m.identity.gcmConfirmPending {
		t.Fatalf("expected gcmConfirmPending after first Enter")
	}

	// Second Enter — fires fix command. Execute it manually.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("expected tea.Cmd after confirming fix, got nil")
	}
	m = sendMsg(m, cmd())

	if m.identity.gcmErrMsg != "" {
		t.Fatalf("unexpected fix error: %s", m.identity.gcmErrMsg)
	}
	if !m.identity.gcmFixed {
		t.Fatal("expected gcmFixed after successful fix")
	}

	// Verify ~/.gitconfig actually wrote the expected values.
	if helper, _ := git.GlobalConfigGet("credential.helper"); helper != credential.DefaultCredentialHelper() {
		t.Errorf("credential.helper = %q, want %q (gitconfig path: %s)", helper, credential.DefaultCredentialHelper(), gitconfigPath)
	}
	if store, _ := git.GlobalConfigGet("credential.credentialStore"); store != credential.DefaultCredentialStore() {
		t.Errorf("credential.credentialStore = %q, want %q", store, credential.DefaultCredentialStore())
	}

	// Verify gitbox.json persisted the defaults.
	loaded, err := config.Load(env.CfgPath)
	if err != nil {
		t.Fatalf("loading cfg after fix: %v", err)
	}
	if loaded.Global.CredentialGCM == nil || loaded.Global.CredentialGCM.Helper != credential.DefaultCredentialHelper() {
		t.Errorf("gitbox.json did not backfill Helper; got %+v", loaded.Global.CredentialGCM)
	}
}

func TestIdentity_GCMConfirmCancelledByN(t *testing.T) {
	isolateGitconfig(t)
	cfg := newGCMConfig(t, t.TempDir())
	env := setupTestEnvWithConfig(t, cfg)

	m := navigateToIdentityScreen(t, env.CfgPath)
	m = sendMsg(m, globalIdentityMsg{})
	m = sendMsg(m, globalGCMMsg{needed: true, status: credential.CheckGlobalGCMConfig(cfg)})

	m = sendSpecialKey(m, tea.KeyDown)  // focus GCM
	m = sendSpecialKey(m, tea.KeyEnter) // confirm prompt
	if !m.identity.gcmConfirmPending {
		t.Fatal("expected confirm prompt")
	}

	m = sendKey(m, "n")
	if m.identity.gcmConfirmPending {
		t.Error("expected confirm cleared after 'n'")
	}
	if m.identity.gcmFixed {
		t.Error("should not mark fixed after cancel")
	}
}
