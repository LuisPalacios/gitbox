package tui

import (
	"strings"
	"testing"
)

func TestAccountAdd_Render(t *testing.T) {
	cfg := newTestConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Navigate to account add screen.
	m = sendMsg(m, switchScreenMsg{screen: screenAccountAdd})
	if m.screen != screenAccountAdd {
		t.Fatalf("expected screenAccountAdd, got %d", m.screen)
	}

	view := m.View()
	for _, want := range []string{"Account key", "Provider", "URL", "Username"} {
		if !strings.Contains(view, want) {
			t.Errorf("account add View missing %q", want)
		}
	}
}

func TestAccountAdd_SaveAccount(t *testing.T) {
	cfg := newTestConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Navigate to account add.
	m = sendMsg(m, switchScreenMsg{screen: screenAccountAdd})

	// Programmatically fill the form fields (bypasses keystroke simulation).
	m.accountAdd.form.Fields[addFieldKey].TextInput.SetValue("test-github")
	// Provider is already "github" by default (first option).
	m.accountAdd.form.Fields[addFieldURL].TextInput.SetValue("https://github.com")
	m.accountAdd.form.Fields[addFieldUsername].TextInput.SetValue("testuser")
	m.accountAdd.form.Fields[addFieldName].TextInput.SetValue("Test User")
	m.accountAdd.form.Fields[addFieldEmail].TextInput.SetValue("test@example.com")
	// Credential type is already "gcm" by default (first option).

	// Execute saveAccount directly (simulates form submission).
	cmd := m.accountAdd.saveAccount()
	if cmd == nil {
		t.Fatal("expected command from saveAccount")
	}
	msg := cmd()
	added, ok := msg.(accountAddedMsg)
	if !ok {
		t.Fatalf("expected accountAddedMsg, got %T", msg)
	}
	if added.err != nil {
		t.Fatalf("save account error: %v", added.err)
	}
	if added.key != "test-github" {
		t.Errorf("expected key %q, got %q", "test-github", added.key)
	}

	// External verification: config file should have the new account.
	assertConfigHasAccount(t, env.CfgPath, "test-github")
	// A matching source should also be created.
	assertConfigHasSource(t, env.CfgPath, "test-github")
}

func TestAccountAdd_DuplicateKey(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccountAdd})

	// Validate the key field with an existing key.
	validator := m.accountAdd.form.Fields[addFieldKey].ValidateFn
	if validator == nil {
		t.Fatal("expected validation function on key field")
	}
	errStr := validator("github-alice")
	if errStr == "" {
		t.Error("expected validation error for duplicate key 'github-alice'")
	}
	if !strings.Contains(errStr, "already exists") {
		t.Errorf("expected 'already exists' in error, got %q", errStr)
	}
}

func TestAccountAdd_InvalidKey(t *testing.T) {
	cfg := newTestConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, switchScreenMsg{screen: screenAccountAdd})

	validator := m.accountAdd.form.Fields[addFieldKey].ValidateFn

	tests := []struct {
		key     string
		wantErr bool
	}{
		{"", true},               // empty
		{"-bad", true},           // leading hyphen
		{"has spaces", true},     // spaces
		{"good-key", false},      // valid
		{"MyAccount123", false},  // valid
	}
	for _, tt := range tests {
		errStr := validator(tt.key)
		if tt.wantErr && errStr == "" {
			t.Errorf("expected error for key %q, got none", tt.key)
		}
		if !tt.wantErr && errStr != "" {
			t.Errorf("unexpected error for key %q: %s", tt.key, errStr)
		}
	}
}
