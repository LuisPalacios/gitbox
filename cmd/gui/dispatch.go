package main

import (
	"context"
	"net/url"
	"strings"
	"sync"
)

// fetchDispatcher limits transfers across bulk and individual requests.
// Its zero value is ready to use.
type fetchDispatcher struct {
	mu      sync.Mutex
	global  chan struct{}
	perHost map[string]chan struct{}
}

func fetchHostname(accountURL string) string {
	u, err := url.Parse(accountURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}

func (d *fetchDispatcher) acquire(ctx context.Context, host string) (func(), error) {
	d.mu.Lock()
	if d.global == nil {
		d.global = make(chan struct{}, fetchAllConcurrency)
		d.perHost = make(map[string]chan struct{})
	}
	hostSlots := d.perHost[host]
	if hostSlots == nil {
		hostSlots = make(chan struct{}, fetchPerHostConcurrency)
		d.perHost[host] = hostSlots
	}
	globalSlots := d.global
	d.mu.Unlock()

	select {
	case hostSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case globalSlots <- struct{}{}:
	case <-ctx.Done():
		<-hostSlots
		return nil, ctx.Err()
	}

	release := func() {
		<-globalSlots
		<-hostSlots
	}
	// A free slot and cancellation can both be ready in select.
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
