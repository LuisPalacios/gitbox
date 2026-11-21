package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return true
	}

	// Low-level network operation error (connection refused, network unreachable, etc.).
	_, ok := errors.AsType[*net.OpError](err)
	return ok
}

// ProviderClass classifies why a provider API request failed.
type ProviderClass string

const (
	ClassUnauthorized ProviderClass = "unauthorized"
	ClassScopeDenied  ProviderClass = "scope_denied"
	ClassRateLimited  ProviderClass = "rate_limited"
	ClassNotFound     ProviderClass = "not_found"
	ClassServerError  ProviderClass = "server_error"
	ClassOffline      ProviderClass = "offline"
	ClassTimeout      ProviderClass = "timeout"
	ClassUnknown      ProviderClass = "unknown"
)

// maxErrorBody bounds the response body a ProviderError keeps.
const maxErrorBody = 512

// ProviderError is the error of every provider API request. Its text is built
// only from allowlisted parts (the HTTP method, the host and the status), so
// it is safe to show, log and store: it never holds the request path or
// query, a header or the response body, any of which can carry a token.
type ProviderError struct {
	Status   int           // HTTP status; 0 when no response arrived
	Class    ProviderClass // why it failed
	Provider string        // provider name, when the caller knows it
	Detail   string        // allowlisted description; this is the error text

	// body is the start of the response body, bounded to maxErrorBody. It
	// never leaves the package through Error: only IsAlreadyExists reads it.
	body string
	// cause is the transport failure without the URL that net/http adds.
	cause error
}

func (e *ProviderError) Error() string { return e.Detail }

func (e *ProviderError) Unwrap() error { return e.cause }

// hostOf returns the host of a request URL, the only part of it that an
// error may show.
func hostOf(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return "the server"
}

// statusError builds the error for a response with an unexpected status.
func statusError(method, rawURL string, resp *http.Response) *ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	e := &ProviderError{Status: resp.StatusCode, body: string(body)}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		e.Class = ClassUnauthorized
		e.Detail = "authentication failed (HTTP 401) — token is invalid or expired"
		return e
	case resp.StatusCode == http.StatusForbidden:
		e.Class = ClassScopeDenied
		e.Detail = "insufficient permissions (HTTP 403) — token needs broader scopes"
		return e
	case resp.StatusCode == http.StatusNotFound:
		e.Class = ClassNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Class = ClassRateLimited
	case resp.StatusCode >= 500:
		e.Class = ClassServerError
	default:
		e.Class = ClassUnknown
	}
	e.Detail = fmt.Sprintf("HTTP %d from %s (%s)", resp.StatusCode, hostOf(rawURL), method)
	return e
}

// transportError builds the error for a request that got no response. The
// *url.Error that net/http returns repeats the whole URL, so only what it
// wraps is kept.
func transportError(method, rawURL string, err error) *ProviderError {
	cause := err
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		cause = urlErr.Err
	}
	e := &ProviderError{Class: ClassOffline, cause: cause}
	what := "cannot reach"
	netErr, isNet := errors.AsType[net.Error](err)
	switch {
	case errors.Is(err, context.Canceled):
		e.Class, what = ClassUnknown, "cancelled request to"
	case errors.Is(err, context.DeadlineExceeded), isNet && netErr.Timeout():
		e.Class, what = ClassTimeout, "timed out reaching"
	}
	e.Detail = fmt.Sprintf("%s %s (%s)", what, hostOf(rawURL), method)
	return e
}

// requestError is a request that could not even be built, which means the
// URL is malformed.
func requestError(method, rawURL string) *ProviderError {
	return &ProviderError{Class: ClassUnknown, Detail: fmt.Sprintf("invalid request to %s (%s)", hostOf(rawURL), method)}
}

// decodeError is a successful response whose body is not the expected JSON.
func decodeError(method, rawURL string) *ProviderError {
	return &ProviderError{Status: http.StatusOK, Class: ClassUnknown,
		Detail: fmt.Sprintf("unexpected response from %s (%s)", hostOf(rawURL), method)}
}

func hasStatus(err error, status int) bool {
	e, ok := errors.AsType[*ProviderError](err)
	return ok && e.Status == status
}

// IsForbiddenError reports whether err came from an HTTP 403 response.
func IsForbiddenError(err error) bool { return hasStatus(err, http.StatusForbidden) }

// IsUnauthorized reports whether err came from an HTTP 401 response: the
// server rejected the credential.
func IsUnauthorized(err error) bool { return hasStatus(err, http.StatusUnauthorized) }

// IsNotFound reports whether err came from an HTTP 404 response.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsAlreadyExists reports whether a create request failed because the
// resource is already there: a 409, or a 4xx whose body says so.
func IsAlreadyExists(err error) bool {
	e, ok := errors.AsType[*ProviderError](err)
	if !ok {
		return false
	}
	if e.Status == http.StatusConflict {
		return true
	}
	return e.Status >= 400 && e.Status < 500 && strings.Contains(strings.ToLower(e.body), "already exist")
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
