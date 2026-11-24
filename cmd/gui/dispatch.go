package main

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// fetchDispatcher limits transfers across bulk and individual requests.
// Its zero value is ready to use.
type fetchDispatcher struct {
	waiting atomic.Int64 // all hosts waiting for a transfer slot
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

func (d *fetchDispatcher) acquire(ctx context.Context, host string) (func(), int, error) {
	depth := int(d.waiting.Add(1) - 1)
	defer d.waiting.Add(-1)
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
		return nil, depth, ctx.Err()
	}

	select {
	case globalSlots <- struct{}{}:
	case <-ctx.Done():
		<-hostSlots
		return nil, depth, ctx.Err()
	}

	release := func() {
		<-globalSlots
		<-hostSlots
	}
	// A free slot and cancellation can both be ready in select.
	if err := ctx.Err(); err != nil {
		release()
		return nil, depth, err
	}
	return release, depth, nil
}

// repoRef identifies one configured clone across fleet operations.
type repoRef struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
}

// dispatchByHost drains host queues independently. A busy host never takes
// the worker slots of another one. Transfers use fetchDispatcher to share
// global and per-host slots across overlapping runs.
func dispatchByHost(ctx context.Context, cfg *config.Config, refs []repoRef, work func(repoRef)) {
	queues := make(map[string][]repoRef)
	for _, ref := range refs {
		host := fetchHostname(cfg.Accounts[cfg.Sources[ref.Source].Account].URL)
		queues[host] = append(queues[host], ref)
	}
	var workers sync.WaitGroup
	for _, jobs := range queues {
		var next atomic.Int64
		for range min(len(jobs), fetchPerHostConcurrency) {
			workers.Go(func() {
				for index := next.Add(1) - 1; index < int64(len(jobs)) && ctx.Err() == nil; index = next.Add(1) - 1 {
					work(jobs[index])
				}
			})
		}
	}
	workers.Wait()
}
