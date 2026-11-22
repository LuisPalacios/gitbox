package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
)

func TestDispatchCancelWhileWaitingForHost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var d fetchDispatcher
		host := "same.example"
		var releases []func()
		for range fetchPerHostConcurrency {
			release, err := d.acquire(t.Context(), host)
			if err != nil {
				t.Fatal(err)
			}
			releases = append(releases, release)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			release, err := d.acquire(ctx, host)
			if release != nil {
				release()
			}
			done <- err
		}()
		synctest.Wait()
		if got := len(d.global); got != fetchPerHostConcurrency {
			t.Errorf("waiter took a global slot: %d occupied, want %d", got, fetchPerHostConcurrency)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("host waiter returned %v, want context.Canceled", err)
		}
		for _, release := range releases {
			release()
		}
	})
}

func TestDispatchCancelWhileWaitingForGlobalSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var d fetchDispatcher
		var releases []func()
		for i := range fetchAllConcurrency {
			release, err := d.acquire(t.Context(), fmt.Sprintf("host-%d.example", i))
			if err != nil {
				t.Fatal(err)
			}
			releases = append(releases, release)
		}
		ctx, cancel := context.WithCancel(t.Context())
		host := "queued.example"
		done := make(chan error, 1)
		go func() {
			release, err := d.acquire(ctx, host)
			if release != nil {
				release()
			}
			done <- err
		}()
		synctest.Wait()
		if got := len(d.perHost[host]); got != 1 {
			t.Fatalf("waiter has %d host slots before cancellation, want one", got)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("global waiter returned %v, want context.Canceled", err)
		}
		if got := len(d.perHost[host]); got != 0 {
			t.Errorf("cancelled waiter left %d host slots occupied", got)
		}
		for _, release := range releases {
			release()
		}
	})
}

func TestDispatchHostname(t *testing.T) {
	for _, tt := range []struct{ url, want string }{
		{"https://EXample.COM.:8443/path", "example.com"},
		{"http://example.com/org", "example.com"},
		{"https://[2001:DB8::1]:8443/", "2001:db8::1"},
		{"file:///tmp/clone", ""},
		{"https://[", ""},
		{"", ""},
	} {
		if got := fetchHostname(tt.url); got != tt.want {
			t.Errorf("fetchHostname(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}
