package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// IsNetworkError returns true if the error indicates a connectivity problem
// (DNS failure, connection refused, timeout, network unreachable) rather than
// an authentication or server-side issue.
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}

	// Context timeout or cancellation (e.g., 15-second deadline exceeded).
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	// DNS resolution failure.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Low-level network operation error (connection refused, network unreachable, etc.).
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	return false
}

// IsForbiddenError reports whether err came from an HTTP 403 response.
// doGet/doPost/doDelete encode this as "insufficient permissions (HTTP 403)".
func IsForbiddenError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 403")
}

// ActionScope identifies a gitbox-level operation for scope lookup.
// Used to translate an HTTP 403 into "you need scope X for provider Y".
type ActionScope string

const (
	ActionDeleteRepo ActionScope = "delete_repo"
	ActionCreateRepo ActionScope = "create_repo"
	ActionListRepos  ActionScope = "list_repos"
	ActionMirror     ActionScope = "mirror"
)

// InsufficientScopesError wraps a 403 with actionable guidance: which
// scope(s) the token needs and where to regenerate it. Callers should
// use errors.As to detect this and surface the Remediation fields to
// the user.
type InsufficientScopesError struct {
	Provider       string      // "github" / "gitlab" / ...
	Action         ActionScope // what the caller was trying to do
	RequiredScopes []string    // scope names, in the provider's vocabulary
	BaseURL        string      // account URL (for the regen link)
	cause          error       // original error (for errors.Unwrap)
}

func (e *InsufficientScopesError) Error() string {
	scopes := strings.Join(e.RequiredScopes, ", ")
	return fmt.Sprintf("%s on %s needs token scope: %s", e.Action, e.Provider, scopes)
}

// Unwrap preserves the original error chain for callers that still
// want to reach the underlying HTTPError / IsForbiddenError / etc.
func (e *InsufficientScopesError) Unwrap() error { return e.cause }

// RemediationURL returns the provider-specific PAT creation URL so
// the UI can link the user straight there.
func (e *InsufficientScopesError) RemediationURL() string {
	return TokenCreationURL(e.Provider, e.BaseURL)
}

// ScopesForAction returns the scope names a provider needs for a
// given action, keyed off our internal ActionScope vocabulary.
// Returns nil when the (provider, action) combination is unknown —
// callers should fall back to a generic "needs broader scopes" hint.
func ScopesForAction(providerName string, action ActionScope) []string {
	switch providerName {
	case "github":
		switch action {
		case ActionDeleteRepo:
			return []string{"delete_repo"}
		case ActionCreateRepo, ActionListRepos, ActionMirror:
			return []string{"repo"}
		}
	case "gitlab":
		// GitLab uses a single coarse scope for everything write-side.
		return []string{"api"}
	case "gitea", "forgejo":
		switch action {
		case ActionDeleteRepo, ActionCreateRepo, ActionMirror:
			return []string{"write:repository"}
		case ActionListRepos:
			return []string{"read:repository"}
		}
	case "bitbucket":
		switch action {
		case ActionDeleteRepo:
			return []string{"repository:delete"}
		case ActionCreateRepo:
			return []string{"repository:admin"}
		case ActionListRepos:
			return []string{"repository"}
		}
	}
	return nil
}
