// Package testutil provides isolated Git fixtures for tests.
package testutil

import (
	"context"
	"sync"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// RunnerFunc adapts a scripted function to git.Runner.
type RunnerFunc func(context.Context, git.Request) (git.Result, error)

func (f RunnerFunc) Run(ctx context.Context, req git.Request) (git.Result, error) {
	return f(ctx, req)
}

// Barrier stops one scripted operation after it announces entry.
type Barrier struct {
	Entered <-chan struct{}
	enter   func()
	wait    <-chan struct{}
	Release func()
}

func NewBarrier() Barrier {
	entered, release := make(chan struct{}), make(chan struct{})
	return Barrier{
		Entered: entered,
		enter:   func() { close(entered) },
		wait:    release,
		Release: sync.OnceFunc(func() { close(release) }),
	}
}

func (b Barrier) Arrive(ctx context.Context) error {
	b.enter()
	select {
	case <-b.wait:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
