package main

import (
	"context"
	"net"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// The network guard keeps the app from working against a server it cannot
// reach. Before any fetch, credential check or upstream probe goes out, one
// short TCP connect per server decides whether the server is there. When it
// is not, everything queued for that server stops at once, the window is told
// (network:state) and a slow background watcher keeps trying. When the server
// answers again the watcher clears the state and resumes the work by itself.
const (
	// networkDialTimeout bounds the one connect that decides reachability.
	networkDialTimeout = 3 * time.Second
	// networkDialRetry is the pause before the one retry of a connect that
	// was refused at once.
	networkDialRetry = 500 * time.Millisecond
	// networkFresh is how long a successful connect vouches for a server, so
	// a cycle over many clones connects once per server.
	networkFresh = 10 * time.Second
	// networkWatchInterval is how often the watcher retries servers that are
	// down: calm, one connect per server.
	networkWatchInterval = 15 * time.Second
)

// networkGuard is the reachability state. The zero value is ready to use.
type networkGuard struct {
	mu       sync.Mutex
	fresh    map[string]time.Time      // addr -> until when it is known reachable
	down     map[string]bool           // addrs known unreachable
	flights  map[string]*networkFlight // connects in progress, shared by callers
	watching bool
}

type networkFlight struct {
	done chan struct{}
	ok   bool
}

// serverAddr returns the host:port that tells whether an account's server is
// reachable: the one in its URL. "" means the URL cannot be read, and the
// account is then never held back.
func serverAddr(acct config.Account) string {
	u, err := url.Parse(acct.URL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// dialServer is the one connect that decides reachability.
func (a *App) dialServer(ctx context.Context, addr string) error {
	if a.refreshHooks != nil {
		if a.refreshHooks.dial == nil {
			return nil // tests that don't care about the network
		}
		return a.refreshHooks.dial(ctx, addr)
	}
	ctx, cancel := context.WithTimeout(ctx, networkDialTimeout)
	defer cancel()
	started := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil && time.Since(started) < time.Second {
		// A connect refused at once right after launch is often macOS Local
		// Network privacy still checking the new process: try once more.
		select {
		case <-ctx.Done():
			return err
		case <-time.After(networkDialRetry):
		}
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	return conn.Close()
}

// reachable reports whether the server at addr answers. A server known to be
// down answers false at once, without a connect: only the watcher and a new
// cycle (forgetNetworkDown) try it again. Callers that ask about the same
// server at the same time share one connect.
func (a *App) reachable(ctx context.Context, addr string) bool {
	if addr == "" {
		return true
	}
	g := &a.network
	g.mu.Lock()
	if g.down[addr] {
		g.mu.Unlock()
		return false
	}
	if time.Now().Before(g.fresh[addr]) {
		g.mu.Unlock()
		return true
	}
	if f := g.flights[addr]; f != nil {
		g.mu.Unlock()
		select {
		case <-f.done:
			return f.ok
		case <-ctx.Done():
			return false
		}
	}
	f := &networkFlight{done: make(chan struct{})}
	if g.flights == nil {
		g.flights = make(map[string]*networkFlight)
	}
	g.flights[addr] = f
	g.mu.Unlock()

	err := a.dialServer(ctx, addr)
	cancelled := ctx.Err() != nil

	g.mu.Lock()
	delete(g.flights, addr)
	f.ok = err == nil
	changed := false
	switch {
	case err == nil:
		if g.fresh == nil {
			g.fresh = make(map[string]time.Time)
		}
		g.fresh[addr] = time.Now().Add(networkFresh)
	case !cancelled: // a cancelled connect says nothing about the server
		if g.down == nil {
			g.down = make(map[string]bool)
		}
		g.down[addr] = true
		changed = true
	}
	g.mu.Unlock()
	close(f.done)
	if changed {
		a.publishNetworkState()
		a.watchNetwork()
	}
	return f.ok
}

// forgetNetworkDown makes the next reachable call connect again to every
// server. A fetch cycle starts with it, so Retry and the periodic fetch
// always test the network afresh. It reports whether any server was down.
func (a *App) forgetNetworkDown() bool {
	g := &a.network
	g.mu.Lock()
	defer g.mu.Unlock()
	wasDown := len(g.down) > 0
	clear(g.down)
	clear(g.fresh)
	return wasDown
}

// downServers returns the host names of the servers known to be down, sorted.
func (a *App) downServers() []string {
	g := &a.network
	g.mu.Lock()
	defer g.mu.Unlock()
	hosts := make([]string, 0, len(g.down))
	for addr := range g.down {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	slices.Sort(hosts)
	return hosts
}

// publishNetworkState tells the window which servers are down. An empty list
// means the network is back.
func (a *App) publishNetworkState() {
	hosts := a.downServers()
	a.sink().Emit("network:state", map[string]any{"offline": len(hosts) > 0, "hosts": hosts})
}

// watchNetwork starts the watcher unless it is already running. Every
// networkWatchInterval it connects once to each server that is down. When one
// answers, it clears it, tells the window and resumes the work with a fetch
// cycle. It ends when no server is down or the app closes.
func (a *App) watchNetwork() {
	g := &a.network
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	g.mu.Lock()
	if a.refreshClosed || g.watching || len(g.down) == 0 {
		g.mu.Unlock()
		return
	}
	g.watching = true
	g.mu.Unlock()
	ctx := a.refreshContextLocked()
	a.refreshWG.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(networkWatchInterval):
			}
			g.mu.Lock()
			addrs := make([]string, 0, len(g.down))
			for addr := range g.down {
				addrs = append(addrs, addr)
			}
			g.mu.Unlock()

			recovered := false
			for _, addr := range addrs {
				if a.dialServer(ctx, addr) != nil || ctx.Err() != nil {
					continue
				}
				g.mu.Lock()
				if g.down[addr] {
					delete(g.down, addr)
					recovered = true
				}
				g.mu.Unlock()
			}
			g.mu.Lock()
			remaining := len(g.down)
			if remaining == 0 {
				g.watching = false
			}
			g.mu.Unlock()
			if recovered {
				a.publishNetworkState()
				// Pick the work up where the outage stopped it.
				go func() { _ = a.FetchAllRepos() }()
			}
			if remaining == 0 {
				return
			}
		}
	})
}
