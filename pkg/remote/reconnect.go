package remote

import (
	"context"
	"errors"
	"time"
)

// ReconnectPolicy is how a client tries again after its link died (#247):
// one attempt after each wait in Backoff, in order. The heartbeat of
// pkg/rpc declares the link dead; this only paces the attempts that follow.
type ReconnectPolicy struct {
	Backoff []time.Duration
}

// DefaultReconnect makes three attempts within about 20 seconds of the
// dead link, plus the time each connect takes.
var DefaultReconnect = ReconnectPolicy{Backoff: []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}}

// Attempts is how many attempts the policy makes.
func (p ReconnectPolicy) Attempts() int { return len(p.Backoff) }

// errNoAttempts is what Run returns for a policy with no attempt.
var errNoAttempts = errors.New("the reconnect policy makes no attempt")

// Run calls attempt for attempts 1 to Attempts, each after its wait, until
// one succeeds, and returns nil then. Otherwise it returns the last
// attempt's error, or ctx's when ctx ends first.
func (p ReconnectPolicy) Run(ctx context.Context, attempt func(ctx context.Context, n int) error) error {
	err := errNoAttempts
	for i, wait := range p.Backoff {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if err = attempt(ctx, i+1); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return err
}
