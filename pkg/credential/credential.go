// Package credential handles credential storage and retrieval for gitbox accounts.
//
// Three credential types are supported:
//   - token: PAT stored in ~/.config/gitbox/credentials/<accountKey>.token
//   - gcm:   Git Credential Manager handles git auth; token extracted via "git credential fill" for API access
//   - ssh:   SSH key pairs for git auth; API access requires a separate PAT (same file storage)
package credential

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// StoreToken stores a PAT in the git-credential-store format file.
// Location: ~/.config/gitbox/credentials/<accountKey>
// The same file is used by both gitbox (API calls) and git CLI (clone/push/pull).
// To write the credential-store format, the account config is needed — use
// StoreTokenWithAccount. This function is a convenience for cases where only
// the raw token needs to be stored (it writes just the token, and
// WriteCredentialFile should be called afterward to update the git format).
func StoreToken(accountKey, token string) error {
	// Write raw token to a temp marker so GetToken can find it before
	// WriteCredentialFile runs. GetToken reads both formats.
	path := CredentialFilePath(accountKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating credentials directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), 0o600); err != nil {
		return fmt.Errorf("storing token for %q: %w", accountKey, err)
	}
	return nil
}

// GetToken retrieves a PAT from ~/.config/gitbox/credentials/<accountKey>.
// Supports both git-credential-store format (https://user:TOKEN@host) and
// raw token (single line). This allows the same file to serve both git CLI
// and gitbox API calls.
func GetToken(accountKey string) (string, error) {
	path := CredentialFilePath(accountKey)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("token not found for %q: %w", accountKey, err)
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		return "", fmt.Errorf("credential file empty for %q", accountKey)
	}

	// Try parsing as git-credential-store format: https://user:token@host
	if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
		if u, err := url.Parse(line); err == nil && u.User != nil {
			if tok, ok := u.User.Password(); ok && tok != "" {
				return tok, nil
			}
		}
	}

	// Fall back to raw token (single line, no URL format).
	return line, nil
}

// DeleteToken removes the credential file for the given account.
func DeleteToken(accountKey string) error {
	path := CredentialFilePath(accountKey)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing token for %q: %w", accountKey, err)
	}
	return nil
}

// EnvVarName returns the environment variable name for an account key.
// Convention: GITBOX_TOKEN_<ACCOUNT_KEY> (uppercased, hyphens → underscores).
func EnvVarName(accountKey string) string {
	key := strings.ToUpper(accountKey)
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, ".", "_")
	return "GITBOX_TOKEN_" + key
}

// ResolveToken resolves a PAT for the given account using the priority chain:
//  1. Environment variable (GITBOX_TOKEN_<KEY>)
//  2. Generic GIT_TOKEN env var (single-account CI fallback)
//  3. Credential file (~/.config/gitbox/credentials/<accountKey>)
//
// Returns the token string and the source description, or an error.
func ResolveToken(acct config.Account, accountKey string) (token, source string, err error) {
	// 1. Account-specific env var.
	envName := EnvVarName(accountKey)
	if val := os.Getenv(envName); val != "" {
		return val, fmt.Sprintf("environment variable %s", envName), nil
	}

	// 2. Generic fallback env var.
	if val := os.Getenv("GIT_TOKEN"); val != "" {
		return val, "environment variable GIT_TOKEN", nil
	}

	// 3. Credential file.
	tok, err := GetToken(accountKey)
	if err != nil {
		return "", "", fmt.Errorf("token not found for account %q: checked env var %s, GIT_TOKEN, and credential file",
			accountKey, envName)
	}
	return tok, "credential file", nil
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

	cmd := exec.Command(git.GitBin(), "credential", "fill")
	cmd.Stdin = strings.NewReader(input)
	// Run from home dir so repo-local .git/config credential helpers
	// don't override the global GCM helper. Home has ~/.gitconfig with
	// per-host settings but no .git/config to interfere.
	cmd.Dir, _ = os.UserHomeDir()
	// Use git.Environ() to ensure Homebrew dirs are on PATH (macOS).
	// Without this, git-credential-manager is not found on macOS because
	// the system git (/usr/bin/git) doesn't ship GCM and GUI/SSH sessions
	// inherit a minimal PATH. Do not replace with bare os.Environ().
	cmd.Env = append(git.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_ASKPASS=",
	)
	git.HideWindow(cmd)
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

// ResolveMirrorToken resolves a portable PAT suitable for use by remote servers
// (e.g., Forgejo pushing to GitHub). Unlike ResolveAPIToken, this never returns
// GCM OAuth tokens since those are machine-local and can't be used by third-party
// servers.
//
// For all credential types, it checks env vars and OS keyring only (ResolveToken).
// GCM accounts must have a separate PAT stored via "gitbox account credential setup".
func ResolveMirrorToken(acct config.Account, accountKey string) (token, source string, err error) {
	tok, src, err := ResolveToken(acct, accountKey)
	if err != nil {
		if acct.DefaultCredentialType == "gcm" {
			return "", "", fmt.Errorf("mirror token not found for GCM account %q: mirrors require a PAT stored via 'gitbox account credential setup %s' (GCM OAuth tokens are machine-local and can't be used by remote servers)",
				accountKey, accountKey)
		}
		return "", "", err
	}
	return tok, src, nil
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

// CanOpenBrowser reports whether the current session can likely open a web
// browser for OAuth flows (GCM credential setup). It returns false for SSH
// sessions and headless Linux environments where no display server is available.
func CanOpenBrowser() bool {
	if runtime.GOOS == "windows" {
		return true
	}
	// SSH session detection: SSH_CLIENT or SSH_TTY set means remote access.
	if os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "" {
		// On macOS, SSH sessions can still open browsers (via `open`).
		if runtime.GOOS == "darwin" {
			return true
		}
		// Linux SSH: only if a display server is available.
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	}
	return true
}

// EnsureGlobalGCMConfig sets ~/.gitconfig global GCM credential settings
// (helper, credentialStore) needed before interactive git credential fill/approve.
// Per-host settings (provider, useHttpPath, username) are per-repo, not global.
func EnsureGlobalGCMConfig(global config.GlobalConfig) {
	if global.CredentialGCM != nil {
		if global.CredentialGCM.Helper != "" {
			_ = git.GlobalConfigSet("credential.helper", global.CredentialGCM.Helper)
		}
		if global.CredentialGCM.CredentialStore != "" {
			_ = git.GlobalConfigSet("credential.credentialStore", global.CredentialGCM.CredentialStore)
		}
	}
}
