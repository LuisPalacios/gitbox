package provider

import (
	"context"
	"errors"
	"net"
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
