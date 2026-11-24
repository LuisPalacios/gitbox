// Package credential handles credential storage and retrieval for gitbox accounts.
//
// Three credential types are supported:
//   - token: PAT stored in ~/.config/gitbox/credentials/<accountKey>
//   - gcm:   Git Credential Manager handles git auth; token extracted via "git credential fill" for API access
//   - ssh:   SSH key pairs for git auth; API access requires a separate PAT (same file storage)
package credential

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// StoreToken writes the legacy raw-token format with one trailing newline.
// GetToken accepts both this format and git-credential-store URLs. Call
// StoreTokenWithAccount when the account is known so Git can use the file too.
func StoreToken(accountKey, token string) error {
	if err := writeCredentialFileAtomic(CredentialFilePath(accountKey), []byte(strings.TrimSpace(token)+"\n")); err != nil {
		return fmt.Errorf("storing token for %q: %w", accountKey, err)
	}
	return nil
}

// StoreTokenWithAccount writes a PAT as a git-credential-store URL directly,
// without an intermediate raw-token file or environment-variable resolution.
func StoreTokenWithAccount(accountKey, token string, acct config.Account) error {
	if err := writeCredentialFileAtomic(CredentialFilePath(accountKey), credentialLine(acct, strings.TrimSpace(token))); err != nil {
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
	// Go opens Windows files without delete sharing. Coordinate our readers
	// with replacement so a concurrent token lookup cannot block a writer.
	credentialFileMu.Lock()
	data, err := os.ReadFile(path)
	credentialFileMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("token not found for %q: %w", accountKey, err)
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		return "", fmt.Errorf("credential file empty for %q", accountKey)
	}

	// Try parsing as git-credential-store format: https://user:token@host
	if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
		if tok := storeLineToken(line); tok != "" {
			return tok, nil
		}
	}

	// Fall back to raw token (single line, no URL format).
	return line, nil
}

// storeLineToken is the password of a git-credential-store line, or "".
// Git rewrites the line it approves with the port's colon escaped
// (http://user:token@host%3a3000), which url.Parse refuses in a host, so
// the user information is read on its own (#242).
func storeLineToken(line string) string {
	first, _, _ := strings.Cut(line, "\n")
	if u, err := url.Parse(first); err == nil && u.User != nil {
		if tok, ok := u.User.Password(); ok && tok != "" {
			return tok
		}
		return ""
	}
	_, rest, _ := strings.Cut(first, "://")
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return ""
	}
	_, pass, ok := strings.Cut(rest[:at], ":")
	if !ok {
		return ""
	}
	tok, err := url.PathUnescape(pass)
	if err != nil {
		return ""
	}
	return tok
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
	if token, source, ok := EnvironmentToken(accountKey); ok {
		return token, source, nil
	}
	// 3. Credential file.
	tok, err := GetToken(accountKey)
	if err != nil {
		return "", "", fmt.Errorf("token not found for account %q: checked env var %s, GIT_TOKEN, and credential file",
			accountKey, EnvVarName(accountKey))
	}
	return tok, "credential file", nil
}

// EnvironmentToken returns the account's effective environment override.
func EnvironmentToken(accountKey string) (token, source string, ok bool) {
	// 1. Account-specific env var.
	envName := EnvVarName(accountKey)
	if val := os.Getenv(envName); val != "" {
		return val, fmt.Sprintf("environment variable %s", envName), true
	}

	// 2. Generic fallback env var.
	if val := os.Getenv("GIT_TOKEN"); val != "" {
		return val, "environment variable GIT_TOKEN", true
	}

	return "", "", false
}

// ResolveGCMToken extracts a stored credential from Git Credential Manager
// by running "git credential fill" with the account's URL and username.
// The username is required to distinguish between multiple accounts on the same host.
// GCM stores OAuth tokens that double as bearer tokens for provider REST APIs.
func ResolveGCMToken(accountURL, username string) (token, source string, err error) {
	return ResolveGCMTokenContext(context.Background(), accountURL, username)
}

// ResolveGCMTokenContext extracts a cached GCM credential without prompting and
// bounds the subprocess to 15 seconds or the caller's earlier cancellation.
func ResolveGCMTokenContext(ctx context.Context, accountURL, username string) (token, source string, err error) {
	return resolveGCMToken(ctx, accountURL, username, nil)
}

// ResolveConfiguredGCMToken uses the same helper settings as managed clones.
func ResolveConfiguredGCMToken(ctx context.Context, acct config.Account, global config.GlobalConfig) (string, string, error) {
	return resolveGCMToken(ctx, acct.URL, acct.Username, GCMHelperArgs(acct, global))
}

func resolveGCMToken(ctx context.Context, accountURL, username string, settings []string) (token, source string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	u, err := url.Parse(accountURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid account URL %q: %w", accountURL, err)
	}

	input := fmt.Sprintf("protocol=%s\nhost=%s\nusername=%s\n\n", u.Scheme, u.Host, username)

	// Run from home dir so repo-local .git/config credential helpers
	// don't override the global GCM helper. Home has ~/.gitconfig with
	// per-host settings but no .git/config to interfere.
	home, _ := os.UserHomeDir()
	// The non-interactive environment keeps Homebrew dirs on PATH (macOS):
	// without them git-credential-manager is not found, because the system
	// git (/usr/bin/git) doesn't ship GCM and GUI/SSH sessions inherit a
	// minimal PATH.
	res, err := git.Run(ctx, git.Request{Dir: home, Args: append(settings, "credential", "fill"), Stdin: strings.NewReader(input)})
	out := res.Stdout
	if err != nil {
		if ctx.Err() != nil {
			return "", "", fmt.Errorf("git credential fill failed: %w", ctx.Err())
		}
		return "", "", fmt.Errorf("git credential fill failed: %w (is GCM installed and authenticated?)", err)
	}

	// Parse output for password=<token>.
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if tok, ok := strings.CutPrefix(line, "password="); ok {
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
// For all credential types, it checks env vars and the credential file only (ResolveToken).
// GCM accounts must have a separate PAT stored in the account's credential settings.
func ResolveMirrorToken(acct config.Account, accountKey string) (token, source string, err error) {
	tok, src, err := ResolveToken(acct, accountKey)
	if err != nil {
		if acct.DefaultCredentialType == "gcm" {
			return "", "", fmt.Errorf("mirror token not found for GCM account %q: mirrors require a PAT stored in the account's credential settings (GCM OAuth tokens are machine-local and can't be used by remote servers)",
				accountKey)
		}
		return "", "", err
	}
	return tok, src, nil
}

// ResolveAPIToken resolves an API token for the given account based on its
// credential type:
//   - token: resolves from env vars / the credential file (ResolveToken)
//   - gcm:   extracts from GCM via "git credential fill", falls back to ResolveToken
//   - ssh:   tries ResolveToken (optional — user may have stored a PAT for discovery)
//
// Returns the token, source description, or an error.
func ResolveAPIToken(acct config.Account, accountKey string) (token, source string, err error) {
	return ResolveAPITokenContext(context.Background(), acct, accountKey)
}

// ResolveAPITokenContext resolves an API token and cancels any GCM subprocess
// when the caller cancels its operation.
func ResolveAPITokenContext(ctx context.Context, acct config.Account, accountKey string) (token, source string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	switch acct.DefaultCredentialType {
	case "gcm":
		// Try GCM first (primary for this credential type).
		tok, src, err := ResolveGCMTokenContext(ctx, acct.URL, acct.Username)
		if err == nil {
			return tok, src, nil
		}
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		// Fall back to env vars / the credential file.
		return ResolveToken(acct, accountKey)

	case "ssh":
		// SSH can't do API auth natively. Try env vars / the credential file as fallback.
		return ResolveToken(acct, accountKey)

	default:
		// token type: env vars / the credential file.
		return ResolveToken(acct, accountKey)
	}
}
