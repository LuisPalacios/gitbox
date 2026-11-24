package main

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// The network guard keeps the app from working against a server it cannot
// reach. The rule is one: the moment a server stops answering, everything
// that uses it stops.
//
//   - Before any network work starts (fetch, pull, clone, credential check,
//     upstream, PR and mirror sweeps), one short TCP connect per server
//     decides whether the server is there. If it is not, the work is not
//     attempted.
//   - While work is in flight against a server, a monitor connects to it
//     every few seconds. If the connect fails, the work in flight is
//     cancelled and nothing queued for that server starts.
//   - The window is told (network:state) and shows it with a Retry.
//   - A slow watcher keeps trying the servers that are down. When one
//     answers, it clears the state and resumes the work by itself.
const (
	// networkDialTimeout bounds the one connect that decides reachability.
	networkDialTimeout = 3 * time.Second
	// networkDialRetry is the pause before the one retry of a connect that
	// was refused at once.
	networkDialRetry = 500 * time.Millisecond
	// networkFresh is how long a successful connect vouches for a server, so
	// a run over many clones connects once per server.
	networkFresh = 10 * time.Second
	// networkMonitorInterval is how often a server with work in flight is
	// checked.
	networkMonitorInterval = 3 * time.Second
	// networkWatchInterval is how often the watcher retries servers that are
	// down: calm, one connect per server.
	networkWatchInterval = 30 * time.Second
)

// networkGuard is the reachability state. The zero value is ready to use.
type networkGuard struct {
	mu      sync.Mutex
	fresh   map[string]time.Time      // addr -> until when it is known reachable
	down    map[string]bool           // addrs known unreachable
	flights map[string]*networkFlight // connects in progress, shared by callers
	// ops are the pieces of work in flight per server; each is cancelled
	// when its server goes down.
	ops        map[string]map[*networkOp]bool
	watching   bool
	monitoring bool
}

type networkFlight struct {
	done chan struct{}
	ok   bool
}

type networkOp struct{ cancel context.CancelFunc }

// serverAddr returns the host:port that tells whether an account's server is
// reachable: the one in its URL, or the proxy's when the environment routes
// that URL through one (a direct connect would then fail on a working
// network). "" means the URL cannot be read, and the account is then never
// held back.
func serverAddr(acct config.Account) string {
	return serverAddrVia(acct, http.ProxyFromEnvironment)
}

func serverAddrVia(acct config.Account, proxyFor func(*http.Request) (*url.URL, error)) string {
	u, err := url.Parse(acct.URL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	if proxy, err := proxyFor(&http.Request{URL: u}); err == nil && proxy != nil && proxy.Hostname() != "" {
		u = proxy
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
// down answers false at once, without a connect: only the watcher and Retry
// (forgetNetworkDown) try it again. Callers that ask about the same server
// at the same time share one connect.
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
	return a.connect(ctx, addr) // unlocks g.mu
}

// confirmReachable connects to addr now, whatever was known. Work that just
// failed for lack of a connection calls it, and so does the monitor: a
// failure here is what stops everything else on that server.
func (a *App) confirmReachable(ctx context.Context, addr string) bool {
	if addr == "" {
		return true
	}
	g := &a.network
	g.mu.Lock()
	if g.down[addr] {
		g.mu.Unlock()
		return false
	}
	return a.connect(ctx, addr) // unlocks g.mu
}

// connect runs the connect to addr, or joins the one in progress, and
// records the answer. The caller holds g.mu; connect releases it.
func (a *App) connect(ctx context.Context, addr string) bool {
	g := &a.network
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
	var stopped []*networkOp
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
		delete(g.fresh, addr)
		for op := range g.ops[addr] {
			stopped = append(stopped, op)
		}
	}
	g.mu.Unlock()
	close(f.done)
	if err != nil && !cancelled {
		// The server is gone: stop what is in flight against it, tell the
		// window and start watching for its return.
		for _, op := range stopped {
			op.cancel()
		}
		a.publishNetworkState()
		a.watchNetwork()
	}
	return f.ok
}

// beginNetworkOp registers a piece of network work against addr. It returns
// a context that is cancelled the moment that server goes down, and the
// function to call when the work ends. ok is false, and nothing is
// registered, when the server is not reachable: the work must not start.
func (a *App) beginNetworkOp(parent context.Context, addr string) (ctx context.Context, end func(), ok bool) {
	if !a.reachable(parent, addr) {
		return parent, func() {}, false
	}
	if addr == "" {
		return parent, func() {}, true
	}
	ctx, cancel := context.WithCancel(parent)
	op := &networkOp{cancel: cancel}
	g := &a.network
	g.mu.Lock()
	if g.down[addr] { // it went down between the check and now
		g.mu.Unlock()
		cancel()
		return parent, func() {}, false
	}
	if g.ops == nil {
		g.ops = make(map[string]map[*networkOp]bool)
	}
	if g.ops[addr] == nil {
		g.ops[addr] = make(map[*networkOp]bool)
	}
	g.ops[addr][op] = true
	g.mu.Unlock()
	a.monitorNetwork()
	return ctx, func() {
		g.mu.Lock()
		delete(g.ops[addr], op)
		if len(g.ops[addr]) == 0 {
			delete(g.ops, addr)
		}
		g.mu.Unlock()
		cancel()
	}, true
}

// monitorNetwork starts the monitor unless it is already running. While any
// work is in flight it connects every networkMonitorInterval to each server
// that has some. A failed connect marks the server down, which cancels that
// work. It ends when no work is in flight.
func (a *App) monitorNetwork() {
	g := &a.network
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	g.mu.Lock()
	if a.refreshClosed || g.monitoring || len(g.ops) == 0 {
		g.mu.Unlock()
		return
	}
	g.monitoring = true
	g.mu.Unlock()
	ctx := a.refreshContextLocked()
	a.refreshWG.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(networkMonitorInterval):
			}
			g.mu.Lock()
			addrs := make([]string, 0, len(g.ops))
			for addr := range g.ops {
				addrs = append(addrs, addr)
			}
			if len(addrs) == 0 {
				g.monitoring = false
				g.mu.Unlock()
				return
			}
			g.mu.Unlock()
			var wg sync.WaitGroup
			for _, addr := range addrs {
				wg.Go(func() { a.confirmReachable(ctx, addr) })
			}
			wg.Wait()
		}
	})
}

// forgetNetworkDown makes the next reachable call connect again to every
// server. Retry and each new fetch cycle start with it, so they always test
// the network afresh. It reports whether any server was down.
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
	emit(a.newOperation(), "network:state", networkStateDTO{Offline: len(hosts) > 0, Hosts: hosts})
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
