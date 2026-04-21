package credential

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// isolateGlobalGitconfig points GIT_CONFIG_GLOBAL at a fresh empty file inside
// t.TempDir, so "git config --global" reads/writes only that file for the
// duration of the test. Returns the path so tests can snapshot / inspect it.
func isolateGlobalGitconfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitconfig")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("creating sandbox gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", path)
	return path
}

func TestIsGlobalGCMConfigNeeded(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{"nil cfg", nil, false},
		{"no accounts", &config.Config{Accounts: map[string]config.Account{}}, false},
		{"only ssh account", &config.Config{Accounts: map[string]config.Account{
			"a": {DefaultCredentialType: "ssh"},
		}}, false},
		{"only token account", &config.Config{Accounts: map[string]config.Account{
			"a": {DefaultCredentialType: "token"},
		}}, false},
		{"one gcm account", &config.Config{Accounts: map[string]config.Account{
			"a": {DefaultCredentialType: "ssh"},
			"b": {DefaultCredentialType: "gcm"},
		}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsGlobalGCMConfigNeeded(tc.cfg); got != tc.want {
				t.Errorf("IsGlobalGCMConfigNeeded = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckGlobalGCMConfig_Missing(t *testing.T) {
	isolateGlobalGitconfig(t)
	cfg := &config.Config{}

	s := CheckGlobalGCMConfig(cfg)
	if s.HasHelper {
		t.Errorf("HasHelper = true, want false")
	}
	if s.HasCredentialStore {
		t.Errorf("HasCredentialStore = true, want false")
	}
	if !s.NeedsFix {
		t.Errorf("NeedsFix = false, want true (both keys missing)")
	}
	if s.ExpectedHelper != DefaultCredentialHelper() {
		t.Errorf("ExpectedHelper = %q, want %q", s.ExpectedHelper, DefaultCredentialHelper())
	}
	if s.ExpectedCredentialStore != DefaultCredentialStore() {
		t.Errorf("ExpectedCredentialStore = %q, want %q", s.ExpectedCredentialStore, DefaultCredentialStore())
	}
}

func TestCheckGlobalGCMConfig_Mismatched(t *testing.T) {
	isolateGlobalGitconfig(t)
	if err := git.GlobalConfigSet("credential.helper", "store"); err != nil {
		t.Fatalf("seeding helper: %v", err)
	}
	if err := git.GlobalConfigSet("credential.credentialStore", "plaintext"); err != nil {
		t.Fatalf("seeding store: %v", err)
	}

	s := CheckGlobalGCMConfig(&config.Config{})
	if !s.HasHelper || s.HelperValue != "store" {
		t.Errorf("helper state = (%v,%q), want (true,\"store\")", s.HasHelper, s.HelperValue)
	}
	if !s.HasCredentialStore || s.CredentialStoreValue != "plaintext" {
		t.Errorf("store state = (%v,%q), want (true,\"plaintext\")", s.HasCredentialStore, s.CredentialStoreValue)
	}
	if !s.NeedsFix {
		t.Errorf("NeedsFix = false, want true (values differ from defaults)")
	}
}

func TestCheckGlobalGCMConfig_OK(t *testing.T) {
	isolateGlobalGitconfig(t)
	if err := git.GlobalConfigSet("credential.helper", DefaultCredentialHelper()); err != nil {
		t.Fatalf("seeding helper: %v", err)
	}
	if err := git.GlobalConfigSet("credential.credentialStore", DefaultCredentialStore()); err != nil {
		t.Fatalf("seeding store: %v", err)
	}

	s := CheckGlobalGCMConfig(&config.Config{})
	if s.NeedsFix {
		t.Errorf("NeedsFix = true, want false (gitconfig matches defaults)")
	}
}

func TestCheckGlobalGCMConfig_RespectsCfgOverrides(t *testing.T) {
	isolateGlobalGitconfig(t)
	// Cfg overrides the default helper to "store" — "store" should then be
	// the expected value and a gitconfig of "store" should satisfy NeedsFix=false.
	cfg := &config.Config{
		Global: config.GlobalConfig{
			CredentialGCM: &config.GCMGlobal{Helper: "store", CredentialStore: "plaintext"},
		},
	}
	if err := git.GlobalConfigSet("credential.helper", "store"); err != nil {
		t.Fatalf("seeding helper: %v", err)
	}
	if err := git.GlobalConfigSet("credential.credentialStore", "plaintext"); err != nil {
		t.Fatalf("seeding store: %v", err)
	}

	s := CheckGlobalGCMConfig(cfg)
	if s.ExpectedHelper != "store" || s.ExpectedCredentialStore != "plaintext" {
		t.Errorf("expected overrides = (%q,%q), want (store,plaintext)", s.ExpectedHelper, s.ExpectedCredentialStore)
	}
	if s.NeedsFix {
		t.Errorf("NeedsFix = true, want false (cfg overrides match gitconfig)")
	}
}

func TestFixGlobalGCMConfig_BackfillsDefaults(t *testing.T) {
	isolateGlobalGitconfig(t)
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "gitbox.json")

	cfg := &config.Config{
		Schema:   "https://example.com/schema.json",
		Version:  2,
		Accounts: map[string]config.Account{},
		Sources:  map[string]config.Source{},
	}

	if err := FixGlobalGCMConfig(cfg, cfgPath); err != nil {
		t.Fatalf("FixGlobalGCMConfig: %v", err)
	}

	// cfg.Global.CredentialGCM populated with defaults.
	if cfg.Global.CredentialGCM == nil {
		t.Fatal("cfg.Global.CredentialGCM is nil after fix")
	}
	if cfg.Global.CredentialGCM.Helper != DefaultCredentialHelper() {
		t.Errorf("Helper = %q, want %q", cfg.Global.CredentialGCM.Helper, DefaultCredentialHelper())
	}
	if cfg.Global.CredentialGCM.CredentialStore != DefaultCredentialStore() {
		t.Errorf("CredentialStore = %q, want %q", cfg.Global.CredentialGCM.CredentialStore, DefaultCredentialStore())
	}

	// gitbox.json persisted.
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("gitbox.json not saved: %v", err)
	}

	// ~/.gitconfig (sandbox) updated.
	helper, err := git.GlobalConfigGet("credential.helper")
	if err != nil || helper != DefaultCredentialHelper() {
		t.Errorf("git config credential.helper = %q, err=%v, want %q", helper, err, DefaultCredentialHelper())
	}
	store, err := git.GlobalConfigGet("credential.credentialStore")
	if err != nil || store != DefaultCredentialStore() {
		t.Errorf("git config credential.credentialStore = %q, err=%v, want %q", store, err, DefaultCredentialStore())
	}
}

func TestFixGlobalGCMConfig_OverwritesExistingHelper(t *testing.T) {
	isolateGlobalGitconfig(t)
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "gitbox.json")

	if err := git.GlobalConfigSet("credential.helper", "store"); err != nil {
		t.Fatalf("seeding helper: %v", err)
	}

	cfg := &config.Config{
		Schema:   "https://example.com/schema.json",
		Version:  2,
		Accounts: map[string]config.Account{},
		Sources:  map[string]config.Source{},
	}

	if err := FixGlobalGCMConfig(cfg, cfgPath); err != nil {
		t.Fatalf("FixGlobalGCMConfig: %v", err)
	}

	helper, err := git.GlobalConfigGet("credential.helper")
	if err != nil {
		t.Fatalf("reading helper: %v", err)
	}
	if helper != DefaultCredentialHelper() {
		t.Errorf("helper = %q, want %q (Fix should overwrite existing value)", helper, DefaultCredentialHelper())
	}
}

func TestFixGlobalGCMConfig_NoCfgSaveWhenAlreadyPopulated(t *testing.T) {
	isolateGlobalGitconfig(t)
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "gitbox.json")

	cfg := &config.Config{
		Schema:  "https://example.com/schema.json",
		Version: 2,
		Global: config.GlobalConfig{
			CredentialGCM: &config.GCMGlobal{
				Helper:          DefaultCredentialHelper(),
				CredentialStore: DefaultCredentialStore(),
			},
		},
		Accounts: map[string]config.Account{},
		Sources:  map[string]config.Source{},
	}

	if err := FixGlobalGCMConfig(cfg, cfgPath); err != nil {
		t.Fatalf("FixGlobalGCMConfig: %v", err)
	}

	// gitbox.json should NOT be created — cfg was already populated, nothing to persist.
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Errorf("gitbox.json unexpectedly created (err=%v); fix should not save when cfg unchanged", err)
	}
}

func TestFixGlobalGCMConfig_NilCfg(t *testing.T) {
	if err := FixGlobalGCMConfig(nil, ""); err == nil {
		t.Errorf("FixGlobalGCMConfig(nil) returned nil, want error")
	}
}
