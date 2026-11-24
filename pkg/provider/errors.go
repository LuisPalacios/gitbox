package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	ClassForbidden    ProviderClass = "forbidden"
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
	Status     int           // HTTP status; 0 when no response arrived
	Class      ProviderClass // why it failed
	Provider   string        // provider name, when the caller knows it
	Detail     string        // allowlisted description; this is the error text
	RetryAfter time.Duration // server-requested delay; 0 when unavailable

	// body is the start of the response body, bounded to maxErrorBody. It
	// never leaves the package through Error. Only known provider codes and
	// the already-exists predicate use it.
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
	case resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == http.StatusForbidden &&
			(resp.Header.Get("X-RateLimit-Remaining") == "0" || hasBodyCode(body, "rate_limited")):
		e.Class = ClassRateLimited
		e.RetryAfter = retryAfter(resp.Header, time.Now())
	case resp.StatusCode == http.StatusForbidden:
		e.Class = ClassForbidden
		if scopeDenied(resp.Header, body) {
			e.Class = ClassScopeDenied
			e.Detail = "insufficient permissions (HTTP 403) — token lacks a required scope"
			return e
		}
	case resp.StatusCode == http.StatusNotFound:
		e.Class = ClassNotFound
	case resp.StatusCode >= 500 && resp.StatusCode < 600:
		e.Class = ClassServerError
	default:
		e.Class = ClassUnknown
	}
	e.Detail = fmt.Sprintf("HTTP %d from %s (%s)", resp.StatusCode, hostOf(rawURL), method)
	return e
}

// hasBodyCode recognizes provider error codes without echoing untrusted text.
func hasBodyCode(body []byte, want string) bool {
	var payload struct {
		Code    string          `json:"code"`
		Error   json.RawMessage `json:"error"`
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	var nested struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(payload.Error, &nested)
	return payload.Code == want || nested.Code == want ||
		string(payload.Error) == strconv.Quote(want) || string(payload.Message) == strconv.Quote(want)
}

func scopeDenied(headers http.Header, body []byte) bool {
	if hasBodyCode(body, "insufficient_scope") {
		return true
	}
	accepted := headers.Get("X-Accepted-OAuth-Scopes")
	if accepted == "" {
		return false
	}
	// This GitHub header also appears when the token already has a
	// matching scope. In that case a 403 has a different cause.
	granted := strings.Split(headers.Get("X-OAuth-Scopes"), ",")
	for required := range strings.SplitSeq(accepted, ",") {
		for _, scope := range granted {
			if strings.TrimSpace(required) == strings.TrimSpace(scope) {
				return false
			}
		}
	}
	return true
}

func retryAfter(headers http.Header, now time.Time) time.Duration {
	if value := headers.Get("Retry-After"); value != "" {
		if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
			return time.Duration(seconds) * time.Second
		}
		if date, err := http.ParseTime(value); err == nil {
			return max(0, date.Sub(now))
		}
	}
	if reset, err := strconv.ParseInt(headers.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		return max(0, time.Unix(reset, 0).Sub(now))
	}
	return 0
}

// transportError builds the error for a request that got no response. The
// *url.Error that net/http returns repeats the whole URL, so only what it
// wraps is kept.
func transportError(method, rawURL string, err error) *ProviderError {
	cause := err
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		cause = urlErr.Err
	}
	e := &ProviderError{Class: ClassUnknown, cause: cause}
	what := "could not contact"
	netErr, isNet := errors.AsType[net.Error](err)
	switch {
	case errors.Is(err, context.Canceled):
		e.Class, what = ClassUnknown, "cancelled request to"
	case errors.Is(err, context.DeadlineExceeded), isNet && netErr.Timeout():
		e.Class, what = ClassTimeout, "timed out reaching"
	case IsNetworkError(err):
		e.Class, what = ClassOffline, "cannot reach"
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

func isRateLimited(err error) bool {
	e, ok := errors.AsType[*ProviderError](err)
	return ok && e.Class == ClassRateLimited
}

// IsScopeDenied reports whether the provider signaled a missing token scope.
func IsScopeDenied(err error) bool {
	e, ok := errors.AsType[*ProviderError](err)
	return ok && e.Class == ClassScopeDenied
}

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
