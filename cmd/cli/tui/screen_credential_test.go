package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newGCMConfig returns a config with a GCM account for credential screen tests.
func newGCMConfig(t *testing.T, gitFolder string) *config.Config {
	t.Helper()
	cfg := newTestConfig(t, gitFolder)
	cfg.Accounts = map[string]config.Account{
		"github-gcmuser": {
			Provider:              "github",
			URL:                   "https://github.com",
			Username:              "gcmuser",
			Name:                  "GCM User",
			Email:                 "gcm@example.com",
			DefaultCredentialType: "gcm",
		},
	}
	cfg.Sources = map[string]config.Source{
		"gcm-repos": {
			Account: "github-gcmuser",
			Repos:   map[string]config.Repo{},
		},
	}
	cfg.SourceOrder = []string{"gcm-repos"}
	return cfg
}

// navigateToCredentialScreen sets up a model and navigates to the credential
// screen for the given account key. Returns the model on the credential screen.
func navigateToCredentialScreen(t *testing.T, cfgPath, accountKey string) model {
	t.Helper()
	m := newTestModel(t, cfgPath)
	m = initModel(t, m)
	m = sendMsg(m, switchScreenMsg{screen: screenCredential, accountKey: accountKey})
	if m.screen != screenCredential {
		t.Fatalf("expected screenCredential, got %d", m.screen)
	}
	return m
}

// ---------------------------------------------------------------------------
// GCM menu rendering
// ---------------------------------------------------------------------------

func TestCredential_GCM_MenuRender(t *testing.T) {
	cfg := newGCMConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := navigateToCredentialScreen(t, env.CfgPath, "github-gcmuser")

	view := m.View()

	// Should show account key and credential type.
	for _, want := range []string{"github-gcmuser", "gcm"} {
		if !strings.Contains(view, want) {
			t.Errorf("credential menu View missing %q", want)
		}
	}

	// On desktop, should show browser auth hint.
	if credential.CanOpenBrowser() {
		if !strings.Contains(view, "browser auth") {
			t.Errorf("expected 'browser auth' hint on desktop, got:\n%s", view)
		}
	}
}

// ---------------------------------------------------------------------------
// GCM setup view rendering
// ---------------------------------------------------------------------------

func TestCredential_GCM_SetupView(t *testing.T) {
	cfg := newGCMConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := navigateToCredentialScreen(t, env.CfgPath, "github-gcmuser")

	// Simulate navigating to setup view.
	m.credential.view = credViewSetup
	m.credential.busy = false
	m.credential.resultOK = false

	view := m.View()

	if credential.CanOpenBrowser() {
		// Desktop: should show "enter authenticate" or prompt to press enter.
		if !strings.Contains(view, "authenticate") {
			t.Errorf("expected 'authenticate' in GCM setup view on desktop, got:\n%s", view)
		}
	} else {
		// SSH/headless: should show desktop session message.
		if !strings.Contains(view, "desktop session") {
			t.Errorf("expected 'desktop session' in GCM setup view on headless, got:\n%s", view)
		}
	}
}

func TestCredential_GCM_SetupViewBusy(t *testing.T) {
	cfg := newGCMConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := navigateToCredentialScreen(t, env.CfgPath, "github-gcmuser")

	// Simulate busy state (browser open, waiting for auth).
	m.credential.view = credViewSetup
	m.credential.busy = true

	view := m.View()
	if !strings.Contains(view, "Opening browser") {
		t.Errorf("expected 'Opening browser' in busy GCM setup view, got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// credSetupDoneMsg handling
// ---------------------------------------------------------------------------

func TestCredential_GCM_SetupDoneSuccess(t *testing.T) {
	cfg := newGCMConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := navigateToCredentialScreen(t, env.CfgPath, "github-gcmuser")
	m.credential.view = credViewSetup
	m.credential.busy = true

	// Simulate successful GCM auth completion.
	m = sendMsg(m, credSetupDoneMsg{accountKey: "github-gcmuser", gcmUsername: "gcmuser"})

	if m.credential.busy {
		t.Error("expected busy=false after credSetupDoneMsg")
	}
	if m.credential.errMsg != "" {
		t.Errorf("expected no error, got %q", m.credential.errMsg)
	}
	if !m.credential.resultOK {
		t.Error("expected resultOK=true after successful GCM auth")
	}
}

func TestCredential_GCM_SetupDoneNeedsPAT(t *testing.T) {
	cfg := newGCMConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := navigateToCredentialScreen(t, env.CfgPath, "github-gcmuser")
	m.credential.view = credViewSetup
	m.credential.busy = true

	// Simulate GCM auth OK but API needs PAT.
	m = sendMsg(m, credSetupDoneMsg{accountKey: "github-gcmuser", needsPAT: true, gcmUsername: "gcmuser"})

	if m.credential.busy {
		t.Error("expected busy=false after credSetupDoneMsg with needsPAT")
	}
	if !m.credential.forceTokenSetup {
		t.Error("expected forceTokenSetup=true when needsPAT")
	}
	if m.credential.view != credViewSetup {
		t.Errorf("expected credViewSetup, got %d", m.credential.view)
	}

	// Should show info message about PAT requirement.
	view := m.View()
	if !strings.Contains(view, "PAT") {
		t.Errorf("expected PAT info in view after needsPAT, got:\n%s", view)
	}
}

func TestCredential_GCM_SetupDoneError(t *testing.T) {
	cfg := newGCMConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := navigateToCredentialScreen(t, env.CfgPath, "github-gcmuser")
	m.credential.view = credViewSetup
	m.credential.busy = true

	// Simulate GCM auth failure.
	m = sendMsg(m, credSetupDoneMsg{
		accountKey: "github-gcmuser",
		err:        fmt.Errorf("GCM authentication failed for gcmuser@github.com"),
	})

	if m.credential.busy {
		t.Error("expected busy=false after error")
	}
	if m.credential.errMsg == "" {
		t.Error("expected errMsg to be set after error")
	}
	if m.credential.resultOK {
		t.Error("expected resultOK=false after error")
	}

	view := m.View()
	if !strings.Contains(view, "GCM authentication failed") {
		t.Errorf("expected error message in view, got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Type selection → GCM flow
// ---------------------------------------------------------------------------

func TestCredential_GCM_TypeSelectTriggersSetup(t *testing.T) {
	// Start with an account that has NO credential type (goes to type selection).
	cfg := newGCMConfig(t, "/tmp/test-git")
	acct := cfg.Accounts["github-gcmuser"]
	acct.DefaultCredentialType = ""
	cfg.Accounts["github-gcmuser"] = acct

	env := setupTestEnvWithConfig(t, cfg)
	m := navigateToCredentialScreen(t, env.CfgPath, "github-gcmuser")

	// Should start on type selection view.
	if m.credential.view != credViewType {
		t.Fatalf("expected credViewType for unconfigured account, got %d", m.credential.view)
	}

	// Simulate selecting GCM type via credChangedMsg (the type form submit).
	// Update the account to have GCM set (as the form handler would do).
	acct.DefaultCredentialType = "gcm"
	cfg.Accounts["github-gcmuser"] = acct
	m = sendMsg(m, credChangedMsg{msgs: []string{"Credential type set to gcm"}})

	if credential.CanOpenBrowser() {
		// On desktop: should transition to setup view for browser auth.
		if m.credential.view != credViewSetup {
			t.Errorf("expected credViewSetup after GCM type selection on desktop, got %d", m.credential.view)
		}
	} else {
		// On headless: should stay on menu with info message.
		if m.credential.view != credViewMenu {
			t.Errorf("expected credViewMenu after GCM type selection on headless, got %d", m.credential.view)
		}
	}
}

// ---------------------------------------------------------------------------
// Back navigation
// ---------------------------------------------------------------------------

func TestCredential_GCM_BackFromSetup(t *testing.T) {
	cfg := newGCMConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := navigateToCredentialScreen(t, env.CfgPath, "github-gcmuser")
	m.credential.view = credViewSetup

	// Press Esc → should return to menu (not leave the screen).
	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-gcmuser"})
	if m.screen != screenAccount {
		t.Errorf("expected screenAccount after back from credential, got %d", m.screen)
	}
}
