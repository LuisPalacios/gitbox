package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// ---------------------------------------------------------------------------
// TUI integration tests — drive the real Bubble Tea event loop with
// credentials from test-gitbox.json. Skipped when -short or fixture missing.
// ---------------------------------------------------------------------------

func TestIntegration_TUI_DashboardLoadsAccounts(t *testing.T) {
	fixture := requireIntegration(t)

	tm, _ := newIntegrationTestModel(t, fixture)

	// The dashboard repo list shows account keys as section headings.
	// Check that accounts with sources appear (the ANSI compressor may
	// omit card titles but the repo list is always fully rendered).
	keysWithSources := make([]string, 0)
	for sKey := range fixture.Config.Sources {
		src := fixture.Config.Sources[sKey]
		if _, ok := fixture.Config.Accounts[src.Account]; ok {
			keysWithSources = append(keysWithSources, src.Account)
		}
	}
	if len(keysWithSources) == 0 {
		t.Skip("no accounts with sources")
	}

	teatest.WaitFor(
		t, tm.Output(),
		func(bts []byte) bool {
			clean := stripANSI(bts)
			for _, key := range keysWithSources {
				if !bytes.Contains(clean, []byte(key)) {
					return false
				}
			}
			return true
		},
		teatest.WithDuration(10*time.Second),
		teatest.WithCheckInterval(200*time.Millisecond),
	)
}

func TestIntegration_TUI_CredentialStatus(t *testing.T) {
	fixture := requireIntegration(t)

	tm, _ := newIntegrationTestModel(t, fixture)

	// After credential check completes, the dashboard badge changes from "···"
	// to the credential type label (e.g. "token", "ssh"). Wait for at least one.
	for accountKey, acct := range fixture.Config.Accounts {
		if !fixture.HasToken(accountKey) {
			continue
		}
		credType := acct.DefaultCredentialType
		if credType == "" {
			continue
		}
		waitForText(t, tm, credType, 15*time.Second)
		return // one verified badge is enough
	}
	t.Skip("no account with token and credential type")
}

func TestIntegration_TUI_NavigateToAccount(t *testing.T) {
	fixture := requireIntegration(t)

	tm, _ := newIntegrationTestModel(t, fixture)

	// Wait for dashboard repo list to render (account key as section heading).
	keys := sortedAccountKeys(fixture.Config)
	firstWithSource := ""
	for _, k := range keys {
		for _, src := range fixture.Config.Sources {
			if src.Account == k {
				firstWithSource = k
				break
			}
		}
		if firstWithSource != "" {
			break
		}
	}
	if firstWithSource == "" {
		t.Skip("no account with sources")
	}
	waitForText(t, tm, firstWithSource, 5*time.Second)

	// Press Enter to open the first account card (cursor starts at 0).
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// The account detail screen renders provider, username, and hint keys.
	// The ANSI compressor sends the full screen since it's a new layout.
	// Wait for the hint bar which is always at the bottom.
	waitForText(t, tm, "ESC back", 5*time.Second)

	// Press Esc to return to dashboard.
	tm.Send(tea.KeyMsg{Type: tea.KeyEscape})

	// Dashboard re-renders. Wait for the status bar text.
	waitForText(t, tm, "clones", 5*time.Second)
}

func TestIntegration_TUI_AccountCredentialVerified(t *testing.T) {
	fixture := requireIntegration(t)

	// Need a token-based account so the env var credential resolves directly.
	accountKey, ok := firstTokenAccount(fixture)
	if !ok {
		t.Skip("no account with token credential type")
	}

	tm, _ := newIntegrationTestModel(t, fixture)

	// Wait for dashboard, then navigate to the target account card.
	waitForText(t, tm, accountKey, 5*time.Second)
	idx := accountCardIndex(fixture, accountKey)
	for i := 0; i < idx; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyRight})
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Account detail: wait for credential check to complete.
	// Token accounts show "● OK" when StatusOK (PrimaryDetail is "OK").
	waitForText(t, tm, "OK", 15*time.Second)
}

func TestIntegration_TUI_Discovery(t *testing.T) {
	fixture := requireIntegration(t)

	// Use a token-based account so discovery can resolve the PAT via env var.
	accountKey, ok := firstTokenAccount(fixture)
	if !ok {
		t.Skip("no account with token credential type")
	}

	tm, _ := newIntegrationTestModel(t, fixture)

	// Navigate to the target account card.
	waitForText(t, tm, accountKey, 5*time.Second)
	idx := accountCardIndex(fixture, accountKey)
	for i := 0; i < idx; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyRight})
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// On account detail, press 'd' for discovery.
	waitForText(t, tm, "Account: "+accountKey, 5*time.Second)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})

	// Wait for discovery screen title.
	waitForText(t, tm, "Discover Repos: "+accountKey, 5*time.Second)

	// Wait for the API to return repos — summary line shows "new repos".
	waitForText(t, tm, "new repos", 20*time.Second)
}

func TestIntegration_TUI_Settings(t *testing.T) {
	fixture := requireIntegration(t)

	tm, _ := newIntegrationTestModel(t, fixture)

	// Wait for dashboard to load.
	keys := sortedAccountKeys(fixture.Config)
	if len(keys) > 0 {
		waitForText(t, tm, keys[0], 5*time.Second)
	}

	// Press 's' to open settings.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})

	// Settings screen renders "Settings" title.
	waitForText(t, tm, "Settings", 5*time.Second)

	// Press Esc to return to dashboard.
	tm.Send(tea.KeyMsg{Type: tea.KeyEscape})

	// Dashboard re-renders with account keys.
	if len(keys) > 0 {
		waitForText(t, tm, keys[0], 5*time.Second)
	}
}

func TestIntegration_TUI_MirrorsTab(t *testing.T) {
	fixture := requireIntegration(t)

	tm, _ := newIntegrationTestModel(t, fixture)

	// Wait for dashboard Accounts tab to load.
	keys := sortedAccountKeys(fixture.Config)
	if len(keys) > 0 {
		waitForText(t, tm, keys[0], 5*time.Second)
	}

	// Press Tab to switch to Mirrors tab.
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})

	// The tab header shows "Mirrors". The dashboard renders both tab labels
	// but the active one gets styled differently. Check that "Mirrors" appears
	// in the output after the tab switch.
	teatest.WaitFor(
		t, tm.Output(),
		func(bts []byte) bool {
			clean := string(stripANSI(bts))
			// After tab switch, mirror group names or "No mirrors" should appear.
			// At minimum, the tab header "Mirrors" is rendered.
			return strings.Contains(clean, "Mirrors") || strings.Contains(clean, "mirror")
		},
		teatest.WithDuration(5*time.Second),
		teatest.WithCheckInterval(200*time.Millisecond),
	)

	// Press Tab again to return to Accounts.
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	if len(keys) > 0 {
		waitForText(t, tm, keys[0], 5*time.Second)
	}
}
