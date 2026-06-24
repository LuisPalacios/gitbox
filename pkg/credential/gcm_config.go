package credential

import (
	"fmt"
	"path"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// GlobalGCMConfigStatus reports whether the global ~/.gitconfig is correctly
// wired for Git Credential Manager: a top-level credential.helper and a
// credential.credentialStore matching the gitbox configuration (or the
// OS-appropriate defaults when gitbox has not set them yet).
//
// The GCM fill path falls through to /dev/tty when no helper is registered
// for the target host; in a GUI process that surfaces as the cryptic
// "Device not configured" error. Surfacing this status lets gitbox warn
// the user and offer a one-click fix.
type GlobalGCMConfigStatus struct {
	HasHelper               bool   `json:"hasHelper"`
	HelperValue             string `json:"helperValue"`
	ExpectedHelper          string `json:"expectedHelper"`
	HasCredentialStore      bool   `json:"hasCredentialStore"`
	CredentialStoreValue    string `json:"credentialStoreValue"`
	ExpectedCredentialStore string `json:"expectedCredentialStore"`
	NeedsFix                bool   `json:"needsFix"`
}

// IsGlobalGCMConfigNeeded reports whether at least one account in cfg uses
// GCM as its default credential type. Callers gate the warning UI on this
// so pure SSH/token setups never see the banner.
func IsGlobalGCMConfigNeeded(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	for _, acct := range cfg.Accounts {
		if acct.DefaultCredentialType == "gcm" {
			return true
		}
	}
	return false
}

// expectedGCMDefaults returns the helper + credentialStore gitbox wants to
// see in ~/.gitconfig, preferring the values persisted in gitbox.json and
// falling back to OS defaults when they are absent.
func expectedGCMDefaults(cfg *config.Config) (helper, store string) {
	helper = DefaultCredentialHelper()
	store = DefaultCredentialStore()
	if cfg == nil {
		return helper, store
	}
	if gcm := cfg.Global.CredentialGCM; gcm != nil {
		if gcm.Helper != "" {
			helper = gcm.Helper
		}
		if gcm.CredentialStore != "" {
			store = gcm.CredentialStore
		}
	}
	return helper, store
}

// CheckGlobalGCMConfig reads the current state of ~/.gitconfig and compares
// it against the expected helper + credentialStore. NeedsFix is true when
// either value is missing or does not match expectations.
//
// CheckGlobalGCMConfig does not consider whether the warning should be
// surfaced — callers must combine it with IsGlobalGCMConfigNeeded.
func CheckGlobalGCMConfig(cfg *config.Config) GlobalGCMConfigStatus {
	s := GlobalGCMConfigStatus{}
	s.ExpectedHelper, s.ExpectedCredentialStore = expectedGCMDefaults(cfg)

	// credential.helper is a multi-value key. Apply git's reset semantics (an
	// empty value clears everything listed before it) to get the effective
	// helper chain. The macOS Homebrew GCM cask's `git-credential-manager
	// configure` appends an absolute path after a reset, leaving a chain like
	// ["manager", "", "/usr/local/share/gcm-core/git-credential-manager"].
	values, _ := git.GlobalConfigGetAll("credential.helper")
	eff := effectiveHelpers(values)
	if len(eff) > 0 {
		s.HasHelper = true
		s.HelperValue = eff[len(eff)-1]
	}
	if v, err := git.GlobalConfigGet("credential.credentialStore"); err == nil && v != "" {
		s.HasCredentialStore = true
		s.CredentialStoreValue = v
	}

	if !s.HasHelper || !helperSatisfies(eff, s.ExpectedHelper) {
		s.NeedsFix = true
	}
	if !s.HasCredentialStore || s.CredentialStoreValue != s.ExpectedCredentialStore {
		s.NeedsFix = true
	}
	return s
}

// effectiveHelpers reduces a multi-value credential.helper list to the helpers
// git actually uses, honoring its reset rule: an empty value discards every
// helper listed before it; non-empty values are appended.
func effectiveHelpers(values []string) []string {
	var eff []string
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			eff = nil
			continue
		}
		eff = append(eff, v)
	}
	return eff
}

// helperSatisfies reports whether the effective helper chain meets expectations.
// When the expected helper is Git Credential Manager (the default), any helper
// that resolves to GCM counts — including an absolute path to the binary, the
// form `git-credential-manager configure` writes on macOS. When the expected
// helper is something else (a cfg override like "store"), an exact match is
// required.
func helperSatisfies(eff []string, expected string) bool {
	expectGCM := isGCMHelper(expected)
	for _, h := range eff {
		if expectGCM {
			if isGCMHelper(h) {
				return true
			}
		} else if strings.TrimSpace(h) == strings.TrimSpace(expected) {
			return true
		}
	}
	return false
}

// isGCMHelper reports whether a credential.helper value refers to Git Credential
// Manager, whether written as the short name git resolves on PATH ("manager",
// legacy "manager-core") or as an absolute/relative path to the binary (e.g.
// /usr/local/share/gcm-core/git-credential-manager, as the macOS Homebrew cask's
// `git-credential-manager configure` writes).
func isGCMHelper(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	// Normalize Windows separators so a path written with backslashes is
	// recognized even when checked on a non-Windows host (and in tests).
	v = strings.ReplaceAll(v, "\\", "/")
	base := strings.ToLower(path.Base(v))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "manager", "manager-core", "git-credential-manager":
		return true
	default:
		return false
	}
}

// FixGlobalGCMConfig repairs the global gitconfig so GCM-backed fills work:
//
//  1. When cfg.Global.CredentialGCM is nil or empty, backfill OS defaults
//     and persist them to gitbox.json (cfgPath).
//  2. Write credential.helper and credential.credentialStore to ~/.gitconfig.
//
// The persist-then-write order means a gitconfig write failure still leaves
// gitbox.json healed, so a user retry succeeds. The function also supersedes
// the older EnsureGlobalGCMConfig which silently no-op'd when gitbox.json
// lacked defaults.
func FixGlobalGCMConfig(cfg *config.Config, cfgPath string) error {
	if cfg == nil {
		return fmt.Errorf("cfg is nil")
	}

	helper, store := expectedGCMDefaults(cfg)

	// Backfill cfg.Global.CredentialGCM so future callers see populated defaults
	// and subsequent fixes are idempotent. Save only when we actually changed
	// something, to avoid touching the file unnecessarily.
	changed := false
	if cfg.Global.CredentialGCM == nil {
		cfg.Global.CredentialGCM = &config.GCMGlobal{}
		changed = true
	}
	if cfg.Global.CredentialGCM.Helper == "" {
		cfg.Global.CredentialGCM.Helper = helper
		changed = true
	}
	if cfg.Global.CredentialGCM.CredentialStore == "" {
		cfg.Global.CredentialGCM.CredentialStore = store
		changed = true
	}
	if changed && cfgPath != "" {
		if err := config.Save(cfg, cfgPath); err != nil {
			return fmt.Errorf("saving gitbox config: %w", err)
		}
	}

	if err := git.GlobalConfigSet("credential.helper", helper); err != nil {
		return fmt.Errorf("setting global credential.helper: %w", err)
	}
	if err := git.GlobalConfigSet("credential.credentialStore", store); err != nil {
		return fmt.Errorf("setting global credential.credentialStore: %w", err)
	}
	return nil
}
