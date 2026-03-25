// Package credential handles credential storage and retrieval for gitbox accounts.
//
// Three credential types are supported:
//   - token: PAT stored in the OS keyring (Windows Credential Manager, macOS Keychain, Linux Secret Service)
//   - gcm:   Git Credential Manager handles git auth; token extracted via "git credential fill" for API access
//   - ssh:   SSH key pairs for git auth; API access requires a separate token or GCM fallback
package credential

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/zalando/go-keyring"
)

const serviceName = "gitbox"

// StoreToken stores a PAT in the OS keyring for the given account.
func StoreToken(accountKey, token string) error {
	if err := keyring.Set(serviceName, accountKey, token); err != nil {
		return fmt.Errorf("storing token for %q: %w", accountKey, err)
	}
	return nil
}

// GetToken retrieves a PAT from the OS keyring for the given account.
// Returns the token or an error if not found.
func GetToken(accountKey string) (string, error) {
	token, err := keyring.Get(serviceName, accountKey)
	if err != nil {
		return "", fmt.Errorf("token not found in keyring for %q: %w", accountKey, err)
	}
	return token, nil
}

// DeleteToken removes a PAT from the OS keyring for the given account.
func DeleteToken(accountKey string) error {
	if err := keyring.Delete(serviceName, accountKey); err != nil {
		return fmt.Errorf("removing token for %q: %w", accountKey, err)
	}
	return nil
}

// EnvVarName returns the environment variable name for an account key.
// Convention: GITBOX_TOKEN_<ACCOUNT_KEY> (uppercased, hyphens → underscores).
// If the account has a custom env_var set in TokenConfig, that takes priority.
func EnvVarName(accountKey string, tokenCfg *config.TokenConfig) string {
	if tokenCfg != nil && tokenCfg.EnvVar != "" {
		return tokenCfg.EnvVar
	}
	key := strings.ToUpper(accountKey)
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, ".", "_")
	return "GITBOX_TOKEN_" + key
}

// ResolveToken resolves a PAT for the given account using the priority chain:
//  1. Environment variable (convention-based or custom from TokenConfig)
//  2. Generic GIT_TOKEN env var (single-account CI fallback)
//  3. OS keyring (Windows Credential Manager, macOS Keychain, Linux Secret Service)
//
// Returns the token string and the source description, or an error.
func ResolveToken(acct config.Account, accountKey string) (token, source string, err error) {
	// 1. Account-specific env var.
	envName := EnvVarName(accountKey, acct.Token)
	if val := os.Getenv(envName); val != "" {
		return val, fmt.Sprintf("environment variable %s", envName), nil
	}

	// 2. Generic fallback env var.
	if val := os.Getenv("GIT_TOKEN"); val != "" {
		return val, "environment variable GIT_TOKEN", nil
	}

	// 3. OS keyring.
	tok, err := GetToken(accountKey)
	if err != nil {
		return "", "", fmt.Errorf("token not found for account %q: checked env var %s, GIT_TOKEN, and OS keyring",
			accountKey, envName)
	}
	return tok, "OS keyring", nil
}

// ResolveGCMToken extracts a stored credential from Git Credential Manager
// by running "git credential fill" with the account's URL and username.
// The username is required to distinguish between multiple accounts on the same host.
// GCM stores OAuth tokens that double as bearer tokens for provider REST APIs.
func ResolveGCMToken(accountURL, username string) (token, source string, err error) {
	u, err := url.Parse(accountURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid account URL %q: %w", accountURL, err)
	}

	input := fmt.Sprintf("protocol=%s\nhost=%s\nusername=%s\n\n", u.Scheme, u.Host, username)

	cmd := exec.Command("git", "credential", "fill")
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_ASKPASS=",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("git credential fill failed: %w (is GCM installed and authenticated?)", err)
	}

	// Parse output for password=<token>.
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "password=") {
			tok := strings.TrimPrefix(line, "password=")
			if tok != "" {
				return tok, "GCM (git credential fill)", nil
			}
		}
	}

	return "", "", fmt.Errorf("GCM returned no password for %s", u.Host)
}

// ResolveAPIToken resolves an API token for the given account based on its
// credential type:
//   - token: resolves from env vars / OS keyring (ResolveToken)
//   - gcm:   extracts from GCM via "git credential fill", falls back to ResolveToken
//   - ssh:   tries ResolveToken (optional — user may have stored a PAT for discovery)
//
// Returns the token, source description, or an error.
func ResolveAPIToken(acct config.Account, accountKey string) (token, source string, err error) {
	switch acct.DefaultCredentialType {
	case "gcm":
		// Try GCM first (primary for this credential type).
		tok, src, err := ResolveGCMToken(acct.URL, acct.Username)
		if err == nil {
			return tok, src, nil
		}
		// Fall back to env vars / keyring.
		return ResolveToken(acct, accountKey)

	case "ssh":
		// SSH can't do API auth natively. Try env vars / keyring as fallback.
		return ResolveToken(acct, accountKey)

	default:
		// token type: env vars / keyring.
		return ResolveToken(acct, accountKey)
	}
}
