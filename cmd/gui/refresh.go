package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/pathkey"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

type statusFlight struct {
	stream  uint64
	done    chan struct{}
	results []StatusResult
	emit    bool
	dirty   bool
}
type fetchFlight struct {
	done chan struct{}
	err  error
}
type repoFetchFlight struct {
	done         chan struct{}
	cfg          *config.Config
	source, repo string
	err          error
}
type credentialFlight struct {
	done   chan struct{}
	result CredentialStatus
}

// I inject work at the app boundary so concurrency tests need no Wails runtime.
type refreshHooks struct {
	status     func(*config.Config) []status.RepoStatus
	fetch      func(context.Context, string) (string, error)
	heal       func(*config.Config, string, string) heal.Report
	credential func(context.Context, config.Account, string, *config.Config) credential.StatusResult
	pull       func(ctx context.Context, repoPath string) error
	pullCheck  func(context.Context, string) (git.PullSkipReason, git.RepoStatus, error)
	// dial replaces the connect that decides whether a server is reachable.
	// With hooks set and dial nil, every server counts as reachable.
	dial func(ctx context.Context, addr string) error
}

// I detach only the inputs refresh workers read, preserving configured order.
func (a *App) snapshotRefreshConfig() *config.Config {
	cfg, _ := a.refreshSnapshot()
	return cfg
}

func (a *App) refreshSnapshot() (*config.Config, uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.refreshConfigLocked(), a.configRevision
}

func (a *App) refreshConfigLocked() *config.Config {
	c := a.cfg
	if c == nil {
		return &config.Config{Accounts: map[string]config.Account{}, Sources: map[string]config.Source{}}
	}
	out := &config.Config{Global: config.GlobalConfig{Roots: append([]string(nil), c.Global.Roots...)}, Accounts: make(map[string]config.Account, len(c.Accounts)), Sources: make(map[string]config.Source, len(c.Sources)), SourceOrder: append([]string(nil), c.OrderedSourceKeys()...)}
	if c.Global.CredentialSSH != nil {
		v := *c.Global.CredentialSSH
		out.Global.CredentialSSH = &v
	}
	if c.Global.CredentialGCM != nil {
		v := *c.Global.CredentialGCM
		out.Global.CredentialGCM = &v
	}
	if c.Global.CredentialToken != nil {
		v := *c.Global.CredentialToken
		out.Global.CredentialToken = &v
	}
	if len(c.SourceOrder) == 0 {
		sort.Strings(out.SourceOrder)
	}
	for key, acct := range c.Accounts {
		if acct.SSH != nil {
			v := *acct.SSH
			acct.SSH = &v
		}
		if acct.GCM != nil {
			v := *acct.GCM
			acct.GCM = &v
		}
		out.Accounts[key] = acct
	}
	for key, src := range c.Sources {
		dst := config.Source{Account: src.Account, Folder: src.Folder, Root: src.Root, Repos: make(map[string]config.Repo, len(src.Repos)), RepoOrder: append([]string(nil), src.OrderedRepoKeys()...)}
		if len(src.RepoOrder) == 0 {
			sort.Strings(dst.RepoOrder)
		}
		maps.Copy(dst.Repos, src.Repos)
		out.Sources[key] = dst
	}
	return out
}

func (a *App) refreshContextLocked() context.Context {
	if a.refreshCtx == nil {
		a.refreshCtx, a.refreshCancel = context.WithCancel(context.Background())
	}
	return a.refreshCtx
}
func (a *App) refreshContext() context.Context {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	return a.refreshContextLocked()
}

// stopRefresh cancels every piece of work in flight and waits for it to
// stop, for shutdownWait at most. The cancel already reached the git
// processes and what they started, so a worker still running after that is
// stuck outside git, and closing the app does not wait for it.
func (a *App) stopRefresh() {
	a.refreshMu.Lock()
	a.refreshClosed = true
	a.refreshContextLocked()
	a.refreshCancel()
	a.refreshMu.Unlock()
	done := make(chan struct{})
	go func() {
		a.refreshWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownWait):
	}
}

// goTracked runs work as part of what stopRefresh waits for. It reports
// false, without running it, when the app is closing.
func (a *App) goTracked(work func()) bool {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.refreshClosed {
		return false
	}
	a.refreshWG.Go(work)
	return true
}

// healRepo repairs a clone's configuration before network work. Its reads
// are bounded and stop when the app closes.
func (a *App) healRepo(cfg *config.Config, source, repo string) heal.Report {
	if a.refreshHooks != nil && a.refreshHooks.heal != nil {
		return a.refreshHooks.heal(cfg, source, repo)
	}
	if a.readOnly {
		return heal.Report{} // the clone stays as it is
	}
	ctx, cancel := context.WithTimeout(a.refreshContext(), healReadTimeout)
	defer cancel()
	return heal.RepoContext(ctx, cfg, source, repo)
}
func (a *App) overlayStatus(local []StatusResult) []StatusResult {
	out := make([]StatusResult, len(local))
	copy(out, local)
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, check := range a.statusNetwork {
		if live, ok := statusRepoIdentity(a.cfg, key); !ok || live != check.identity {
			delete(a.statusNetwork, key)
		}
	}
	for i := range out {
		row := &out[i]
		if a.upstreamGone[row.Source+"/"+row.Repo] {
			row.State = status.UpstreamGone.String()
		}
		row.CheckedAt = a.networkCheckedAt(*row)
	}
	return out
}

func (a *App) requestStatus(emit, dirty bool) *statusFlight {
	return a.requestStatusStream(emit, dirty, 0)
}

func (a *App) requestStatusStream(emit, dirty bool, stream uint64) *statusFlight {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.refreshClosed {
		done := make(chan struct{})
		close(done)
		return &statusFlight{done: done}
	}
	if call := a.statusRun; call != nil {
		if stream != 0 {
			call.stream = stream
			call.dirty = true
		}
		call.emit = call.emit || emit
		call.dirty = call.dirty || dirty
		return call
	}
	call := &statusFlight{done: make(chan struct{}), emit: emit, stream: stream}
	a.statusRun = call
	a.refreshWG.Go(func() {
		for {
			a.refreshMu.Lock()
			call.dirty = false
			pass := call.stream
			a.refreshMu.Unlock()
			cfg, revision := a.refreshSnapshot()
			sweepStarted := time.Now()
			convert := func(raw []status.RepoStatus) []StatusResult {
				rows := make([]StatusResult, len(raw))
				for i, r := range raw {
					rows[i] = toStatusResult(r)
					rows[i].sweepStarted = sweepStarted
				}
				return rows
			}
			var publish func([]status.RepoStatus)
			if pass != 0 {
				publish = func(raw []status.RepoStatus) {
					a.refreshMu.Lock()
					defer a.refreshMu.Unlock()
					if !a.refreshClosed && !call.dirty && call.stream == pass {
						a.sink().Emit("status:batch", StatusBatch{PassID: pass, Revision: revision, Items: a.overlayStatus(convert(raw))})
					}
				}
			}
			var raw []status.RepoStatus
			if a.refreshHooks != nil && a.refreshHooks.status != nil {
				raw = a.refreshHooks.status(cfg)
				if publish != nil {
					for i := 0; i < len(raw); i += 8 {
						publish(raw[i:min(i+8, len(raw))])
					}
				}
			} else {
				raw, _ = status.CheckAllBatches(a.refreshContext(), cfg, statusTimeout, publish)
			}
			local := convert(raw)
			current, currentRevision := a.refreshSnapshot()
			a.refreshMu.Lock()
			if !a.refreshClosed && (call.dirty || revision != currentRevision || !reflect.DeepEqual(cfg, current)) {
				a.refreshMu.Unlock()
				continue
			}
			a.cachedStatus = local
			a.cachedStatusConfig = cfg
			call.results = local
			if call.emit && !a.refreshClosed {
				a.sink().Emit("status:updated", a.overlayStatus(local))
			}
			if pass != 0 && !a.refreshClosed {
				a.sink().Emit("status:batch", StatusBatch{PassID: pass, Revision: revision, Done: true})
			}
			a.statusRun = nil
			close(call.done)
			a.refreshMu.Unlock()
			return
		}
	})
	return call
}

// StatusBatch belongs to one renderer request and one observed configuration.
type StatusBatch struct {
	PassID   uint64         `json:"passID"`
	Revision uint64         `json:"revision"`
	Items    []StatusResult `json:"items"`
	Done     bool           `json:"done"`
}

// BeginStatus starts a shared local sweep without waiting for every clone.
func (a *App) BeginStatus(passID uint64) error { return a.be().BeginStatus(passID) }

// GetAllStatus shares an active local sweep; a later independent request stays fresh.
func (a *App) GetAllStatus() []StatusResult { return a.be().GetAllStatus() }

// getAllStatusLocal is the local backend's GetAllStatus.
func (a *App) getAllStatusLocal() []StatusResult {
	call := a.requestStatus(false, false)
	<-call.done
	return a.overlayStatus(call.results)
}

// RefreshStatus retains at most one pending invalidation while local work runs.
func (a *App) RefreshStatus() { a.be().RefreshStatus() }

// refreshStatusLocal is the local backend's RefreshStatus.
func (a *App) refreshStatusLocal() { a.requestStatus(true, true); a.probeUpstreamExistenceAsync() }

// I overlay provider results without spawning another fleet of git processes.
func (a *App) emitCachedStatus() {
	current := a.snapshotRefreshConfig()
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.cachedStatusConfig != nil && reflect.DeepEqual(current, a.cachedStatusConfig) && !a.refreshClosed {
		a.sink().Emit("status:updated", a.overlayStatus(a.cachedStatus))
	}
}

func (a *App) sameRefreshRepo(cfg *config.Config, source, repo string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.cfg
	if current == nil {
		return false
	}
	s, ok := cfg.Sources[source]
	n, exists := current.Sources[source]
	if !ok || !exists || s.Account != n.Account || s.Folder != n.Folder || s.Root != n.Root {
		return false
	}
	old, ok := s.Repos[repo]
	now, exists := n.Repos[repo]
	account, hadAccount := cfg.Accounts[s.Account]
	live, hasAccount := current.Accounts[n.Account]
	return ok && exists && old == now && hadAccount && hasAccount && reflect.DeepEqual(account, live) &&
		slices.Equal(cfg.Global.Roots, current.Global.Roots) &&
		reflect.DeepEqual(cfg.Global.CredentialGCM, current.Global.CredentialGCM) &&
		reflect.DeepEqual(cfg.Global.CredentialSSH, current.Global.CredentialSSH) &&
		reflect.DeepEqual(cfg.Global.CredentialToken, current.Global.CredentialToken)
}

func (a *App) fetchOne(cfg *config.Config, source, repo string, lost *sync.Map) error {
	src, ok := cfg.Sources[source]
	if !ok {
		err := fmt.Errorf("source %q not found", source)
		a.sink().Emit("fetch:done", map[string]string{"source": source, "repo": repo, "error": userMsg(err)})
		return err
	}
	r, ok := src.Repos[repo]
	if !ok {
		err := fmt.Errorf("repo %q not found", repo)
		a.sink().Emit("fetch:done", map[string]string{"source": source, "repo": repo, "error": userMsg(err)})
		return err
	}
	account := cfg.Accounts[src.Account]
	host := fetchHostname(account.URL)
	addr := serverAddr(account)
	ctx := a.refreshContext()
	path := status.RepoPath(cfg, source, repo, r)
	key := pathkey.Compare(path)
	for {
		a.refreshMu.Lock()
		if a.refreshClosed {
			a.refreshMu.Unlock()
			return context.Canceled
		}
		if a.repoFetches == nil {
			a.repoFetches = make(map[string]*repoFetchFlight)
		}
		if active := a.repoFetches[key]; active != nil {
			a.refreshMu.Unlock()
			select {
			case <-active.done:
			case <-ctx.Done():
				return ctx.Err()
			}
			if active.source == source && active.repo == repo && reflect.DeepEqual(active.cfg, cfg) {
				if lost != nil && addr != "" && isConnectionFailure(active.err) {
					lost.Store(addr, true)
				}
				return active.err
			}
			continue
		}
		call := &repoFetchFlight{done: make(chan struct{}), cfg: cfg, source: source, repo: repo}
		a.repoFetches[key] = call
		a.refreshMu.Unlock()
		release, err := a.fetches.acquire(ctx, host)
		if err == nil {
			if lost != nil && fetchEndpointLost(lost, addr) {
				err = a.skipUnreachable(source, repo, addr)
			} else {
				err = a.performFetch(cfg, source, repo, path)
				if lost != nil && addr != "" && isConnectionFailure(err) {
					lost.Store(addr, true)
				}
			}
			release()
		}
		a.refreshMu.Lock()
		call.err = err
		delete(a.repoFetches, key)
		close(call.done)
		a.refreshMu.Unlock()
		return err
	}
}

// retryDelays is the backoff for a network check that couldn't reach its
// server: the quick retries ride out macOS refusing LAN connections right
// after launch, the later ones a short network blip.
var retryDelays = []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, time.Second, 3 * time.Second}

// retryBudget caps how long after the first attempt a check keeps
// retrying. A server that is really down times out after longer than this
// on its own, so it isn't retried at all.
const retryBudget = 10 * time.Second

// retryWithBackoff calls again after each delay in retryDelays while again
// reports a failure worth retrying, the run is not cancelled, and the
// budget since started isn't spent.
func retryWithBackoff(ctx context.Context, started time.Time, again func() bool) {
	for _, d := range retryDelays {
		if time.Since(started) > retryBudget {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
		if !again() {
			return
		}
	}
}

func (a *App) performFetch(cfg *config.Config, source, repo, path string) error {
	a.maintenanceMu.RLock()
	report, current := a.repairUnderMaintenance(cfg, source, repo, func() {
		a.sink().Emit("fetch:start", map[string]string{"source": source, "repo": repo})
	})
	a.maintenanceMu.RUnlock()
	if !current {
		err := fmt.Errorf("repository settings changed during refresh")
		a.sink().Emit("fetch:done", map[string]string{"source": source, "repo": repo, "error": userMsg(err)})
		return err
	}
	var err error
	addr := serverAddr(cfg.Accounts[cfg.Sources[source].Account])
	if len(report.Warnings) > 0 {
		err = fmt.Errorf("repository maintenance: %s", strings.Join(report.Warnings, "; "))
	} else {
		remote := "origin"
		if cfg.Sources[source].Repos[repo].FetchAllRemotes {
			remote = "--all"
		}
		fetch := func(ctx context.Context, path string) (string, error) {
			return git.FetchCapturedContext(ctx, path, remote)
		}
		if a.refreshHooks != nil && a.refreshHooks.fetch != nil {
			fetch = a.refreshHooks.fetch
		}
		err = a.onServer(addr, func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
			defer cancel()
			started := time.Now()
			_, err := fetch(ctx, path)
			if git.IsRefusedFast(err) {
				// A connect refused within milliseconds right after launch is
				// usually macOS Local Network privacy still checking the new
				// process; a few quick retries ride it out. A server that is
				// really down times out instead and isn't retried.
				retryWithBackoff(ctx, started, func() bool {
					_, err = fetch(ctx, path)
					return git.IsRefusedFast(err)
				})
			}
			return err
		})
	}
	payload := map[string]any{"source": source, "repo": repo}
	if report.Skipped == "" && !a.readOnly {
		payload["repairWarnings"] = append([]string{}, report.Warnings...)
	}
	if errors.Is(err, errServerUnreachable) {
		describeUnreachable(payload, addr)
	} else if err != nil {
		describeFetchFailure(payload, err)
		if git.IsUpstreamGoneError(err) {
			payload["upstreamGone"] = true
		}
	}
	if (err == nil || git.IsUpstreamGoneError(err)) && a.sameRefreshRepo(cfg, source, repo) {
		a.recordFetchStatus(cfg, source, repo, err != nil, time.Now())
	}
	a.sink().Emit("fetch:done", payload)
	return err
}

// errServerUnreachable is the failure of network work that was not attempted,
// or was stopped, because its server does not answer.
var errServerUnreachable = errors.New("cannot reach the server")

// onServer runs a piece of network work against the server at addr under the
// network guard. The work does not start when the server is unreachable, and
// it is cancelled the moment the server stops answering; either way the
// result is errServerUnreachable. Work that fails for lack of a connection
// makes the guard test the server at once, which stops everything else that
// uses it.
func (a *App) onServer(addr string, work func(context.Context) error) error {
	return a.onServerCtx(a.refreshContext(), addr, work)
}

// onServerCtx is onServer for work that parent can also stop, such as a
// pull a remote window asked for: its request ends with the connection.
func (a *App) onServerCtx(parent context.Context, addr string, work func(context.Context) error) error {
	ctx, end, ok := a.beginNetworkOp(parent, addr)
	if !ok {
		return errServerUnreachable
	}
	err := work(ctx)
	stopped := ctx.Err() != nil && parent.Err() == nil
	end()
	if err == nil {
		return nil
	}
	if stopped || isConnectionFailure(err) && !a.confirmReachable(parent, addr) {
		return errServerUnreachable
	}
	return err
}

// describeUnreachable fills the payload of work that did not run, or was
// stopped, because its server does not answer: the same facts as a failed
// fetch, plus skipped.
func describeUnreachable(payload map[string]any, addr string) {
	payload["error"] = userMsg(errServerUnreachable)
	payload["code"] = "git." + string(git.CodeOffline)
	payload["skipped"] = true
	if host, _, err := net.SplitHostPort(addr); err == nil {
		payload["host"] = host
	}
}

// skipUnreachable reports a clone as failed without trying it: its server is
// unreachable.
func (a *App) skipUnreachable(source, repo, addr string) error {
	payload := map[string]any{"source": source, "repo": repo}
	describeUnreachable(payload, addr)
	a.sink().Emit("fetch:done", payload)
	return errServerUnreachable
}

// isConnectionFailure reports whether a fetch failed because the server
// could not be reached, as opposed to failing against a server it reached.
func isConnectionFailure(err error) bool {
	ge, ok := errors.AsType[*git.GitError](err)
	return ok && (ge.Code == git.CodeOffline || ge.Code == git.CodeTimeout)
}

// probeServers connects once to the server of every account that has clones,
// all at the same time, so the answer for each is known (and shared by every
// later reachable call) within one connect timeout.
func (a *App) probeServers(ctx context.Context, cfg *config.Config) {
	var wg sync.WaitGroup
	seen := make(map[string]bool)
	for _, src := range cfg.Sources {
		addr := serverAddr(cfg.Accounts[src.Account])
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		wg.Go(func() { a.reachable(ctx, addr) })
	}
	wg.Wait()
}

// anyServerReachable reports whether at least one account's server answers.
func (a *App) anyServerReachable(ctx context.Context, cfg *config.Config) bool {
	for _, acct := range cfg.Accounts {
		if a.reachable(ctx, serverAddr(acct)) {
			return true
		}
	}
	return len(cfg.Accounts) == 0
}

// describeFetchFailure adds what the frontend needs to explain a failed
// fetch: the scrubbed text and, as data, the code, the server that could not
// be reached and whether the connect was refused at once. The frontend
// groups failures by these facts, never by reading the text.
func describeFetchFailure(payload map[string]any, err error) {
	ue := toUserError(err)
	payload["error"] = ue.String()
	if ue.Code != "" {
		payload["code"] = ue.Code
	}
	if ge, ok := errors.AsType[*git.GitError](err); ok && ge.Host != "" {
		payload["host"] = ge.Host
	}
	if git.IsRefusedFast(err) {
		payload["refusedFast"] = true
	}
}

// FetchAllRepos joins the active cycle, including its final status and provider work.
func (a *App) FetchAllRepos() error { return a.be().FetchAllRepos() }

// fetchAllReposLocal is the local backend's FetchAllRepos.
func (a *App) fetchAllReposLocal() error {
	return a.fetchAllReposContext(a.refreshContext())
}

func (a *App) fetchAllReposContext(ctx context.Context) error {
	a.refreshMu.Lock()
	if a.refreshClosed {
		a.refreshMu.Unlock()
		return context.Canceled
	}
	if active := a.fetchRun; active != nil {
		a.refreshMu.Unlock()
		select {
		case <-active.done:
			return active.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	call := &fetchFlight{done: make(chan struct{})}
	a.fetchRun = call
	a.refreshWG.Go(func() {
		a.sink().Emit("fetch:allstart")
		err := a.fetchCycle()
		payload := map[string]string{}
		if err != nil {
			payload["error"] = userMsg(err)
		}
		a.sink().Emit("fetch:alldone", payload)
		a.refreshMu.Lock()
		call.err = err
		a.fetchRun = nil
		close(call.done)
		a.refreshMu.Unlock()
	})
	a.refreshMu.Unlock()
	select {
	case <-call.done:
		return call.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) fetchCycle() error {
	var errs []error
	for range 2 {
		cfg := a.snapshotRefreshConfig()
		// One short connect per server decides, before anything is queued,
		// which servers are there. Nothing goes out to one that is not.
		ctx := a.refreshContext()
		wasDown := a.forgetNetworkDown()
		a.probeServers(ctx, cfg)
		if wasDown && len(a.downServers()) == 0 {
			a.publishNetworkState() // the network is back
		}
		var network sync.WaitGroup
		reachable := a.anyServerReachable(ctx, cfg)
		if reachable {
			network.Go(func() { a.probeUpstreamExistence() })
		}
		network.Go(func() { a.verifyRefreshCredentials(cfg) })
		errs = a.fetchClones(ctx, cfg)
		// I publish local changes before waiting for remote checks.
		local := a.requestStatus(true, reachable)
		<-local.done
		network.Wait()
		a.emitCachedStatus()
		if a.refreshContext().Err() != nil {
			return a.refreshContext().Err()
		}
		if reflect.DeepEqual(cfg, a.snapshotRefreshConfig()) {
			break
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d repository fetches failed: %w", len(errs), errs[0])
	}
	return nil
}

func fetchEndpointLost(lost *sync.Map, addr string) bool {
	_, gone := lost.Load(addr)
	return gone
}

// fetchClones drains each host's queue independently, so waiting for a
// busy host never blocks the collection or dispatch of another host's repos.
func (a *App) fetchClones(ctx context.Context, cfg *config.Config) []error {
	type job struct {
		source string
		repo   string
		addr   string
	}
	queues := make(map[string][]job)
	for _, source := range cfg.SourceOrder {
		src := cfg.Sources[source]
		account := cfg.Accounts[src.Account]
		host, addr := fetchHostname(account.URL), serverAddr(account)
		for _, repo := range src.RepoOrder {
			if git.IsRepo(status.RepoPath(cfg, source, repo, src.Repos[repo])) {
				queues[host] = append(queues[host], job{source, repo, addr})
			}
		}
	}

	var workers sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	// A connection can fail even after a successful reachability check.
	// In that case skip the rest of the jobs for that endpoint this cycle.
	var lost sync.Map
	for _, jobs := range queues {
		var next atomic.Int64
		for range min(len(jobs), fetchPerHostConcurrency) {
			workers.Go(func() {
				for index := next.Add(1) - 1; index < int64(len(jobs)) && ctx.Err() == nil; index = next.Add(1) - 1 {
					j := jobs[index]
					var err error
					if fetchEndpointLost(&lost, j.addr) || !a.reachable(ctx, j.addr) {
						err = a.skipUnreachable(j.source, j.repo, j.addr)
					} else {
						err = a.fetchOne(cfg, j.source, j.repo, &lost)
					}
					if err != nil {
						mu.Lock()
						errs = append(errs, err)
						mu.Unlock()
					}
				}
			})
		}
	}
	workers.Wait()
	return errs
}

func (a *App) verifyRefreshCredentials(cfg *config.Config) {
	results := make(map[string]CredentialStatus, len(cfg.Accounts))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	ctx := a.refreshContext()
	for key := range cfg.Accounts {
		if ctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			continue
		}
		wg.Go(func() {
			defer func() { <-sem }()
			result := a.credentialVerify(cfg, key)
			mu.Lock()
			results[key] = result
			mu.Unlock()
		})
	}
	wg.Wait()
	current := a.snapshotRefreshConfig()
	for key, result := range results {
		if !a.credentialStillCurrent(cfg, key, result.revision) {
			delete(results, key)
		}
	}
	if reflect.DeepEqual(cfg.Accounts, current.Accounts) && reflect.DeepEqual(cfg.Global, current.Global) {
		a.sink().Emit("credentials:updated", results)
	}
}

func (a *App) credentialVerify(cfg *config.Config, key string) CredentialStatus {
	for {
		acct, ok := cfg.Accounts[key]
		if !ok {
			return CredentialStatus{Status: "error", Message: "account not found"}
		}
		a.mu.Lock()
		revision := a.credentialRevision[key]
		a.mu.Unlock()
		fingerprint, _ := json.Marshal(struct {
			Key      string
			Account  config.Account
			SSH      *config.SSHGlobal
			Revision uint64
		}{key, acct, cfg.Global.CredentialSSH, revision})
		identity := string(fingerprint)
		a.refreshMu.Lock()
		if a.credentialRuns == nil {
			a.credentialRuns = make(map[string]*credentialFlight)
		}
		if active := a.credentialRuns[identity]; active != nil {
			a.refreshMu.Unlock()
			<-active.done
			if a.credentialStillCurrent(cfg, key, revision) {
				return active.result
			}
			cfg = a.snapshotRefreshConfig()
			continue
		}
		call := &credentialFlight{done: make(chan struct{})}
		a.credentialRuns[identity] = call
		a.refreshMu.Unlock()
		check := credential.CheckContext
		if a.refreshHooks != nil && a.refreshHooks.credential != nil {
			check = a.refreshHooks.credential
		}
		ctx, cancel := context.WithTimeout(a.refreshContext(), 30*time.Second)
		started := time.Now()
		var r credential.StatusResult
		if !a.reachable(ctx, serverAddr(acct)) {
			// The server is not there: say so at once instead of letting
			// the check, and its retries, time out against it.
			r = credential.StatusResult{Primary: credential.StatusOffline, Overall: credential.StatusOffline,
				PrimaryDetail: userMsg(errServerUnreachable)}
		} else if r = check(ctx, acct, key, cfg); r.Overall == credential.StatusOffline {
			// Offline is a network failure (refused, unreachable, DNS):
			// retry with backoff to ride out a refusal right after launch
			// or a short disconnect. Authentication errors aren't retried:
			// a rejected token stays rejected, and repeated failed logins
			// can trip a provider's rate limits.
			retryWithBackoff(ctx, started, func() bool {
				r = check(ctx, acct, key, cfg)
				return r.Overall == credential.StatusOffline
			})
		}
		cancel()
		result := CredentialStatus{Status: r.Overall.String(), Message: r.PrimaryDetail, Primary: r.Primary.String(), PrimaryMsg: r.PrimaryDetail, PAT: r.PAT.String(), PATMsg: r.PATDetail, revision: revision}
		if !r.ExpiresAt.IsZero() {
			result.ExpiresAt = r.ExpiresAt.UTC().Format(time.RFC3339)
		}
		a.refreshMu.Lock()
		call.result = result
		delete(a.credentialRuns, identity)
		close(call.done)
		a.refreshMu.Unlock()
		if a.credentialStillCurrent(cfg, key, revision) || a.refreshContext().Err() != nil {
			return result
		}
		cfg = a.snapshotRefreshConfig()
	}
}

func (a *App) credentialStillCurrent(cfg *config.Config, key string, revision uint64) bool {
	a.mu.Lock()
	sameRevision := a.credentialRevision[key] == revision
	a.mu.Unlock()
	current := a.snapshotRefreshConfig()
	return sameRevision && reflect.DeepEqual(cfg.Accounts[key], current.Accounts[key]) && reflect.DeepEqual(cfg.Global.CredentialSSH, current.Global.CredentialSSH)
}

func (a *App) bumpCredentialRevisionLocked(key string) {
	if a.credentialRevision == nil {
		a.credentialRevision = make(map[string]uint64)
	}
	a.credentialRevision[key]++
}

func (a *App) invalidateCredential(key string) {
	a.mu.Lock()
	a.bumpCredentialRevisionLocked(key)
	a.mu.Unlock()
	a.probeMu.Lock()
	a.lastProbeAt = time.Time{}
	a.probeMu.Unlock()
}

// CredentialVerify is explicitly fresh after each completed check; only active work is shared.
func (a *App) CredentialVerify(key string) CredentialStatus { return a.be().CredentialVerify(key) }

// credentialVerifyLocal is the local backend's CredentialVerify.
func (a *App) credentialVerifyLocal(key string) CredentialStatus {
	a.refreshMu.Lock()
	if a.refreshClosed {
		a.refreshMu.Unlock()
		return CredentialStatus{Status: "offline", Message: context.Canceled.Error()}
	}
	a.refreshWG.Add(1)
	a.refreshMu.Unlock()
	defer a.refreshWG.Done()
	return a.credentialVerify(a.snapshotRefreshConfig(), key)
}

// I coalesce stale reconcile requests into one follow-up after the current
// refresh. The follow-up cannot schedule itself again when edits keep landing.
func (a *App) retryReconcileAfterRefresh() {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.refreshClosed {
		return
	}
	a.mu.Lock()
	if a.reconcilePending {
		a.mu.Unlock()
		return
	}
	a.reconcilePending = true
	a.mu.Unlock()
	ctx := a.refreshContextLocked()
	var waits []<-chan struct{}
	if a.fetchRun != nil {
		waits = append(waits, a.fetchRun.done)
	}
	if a.statusRun != nil {
		waits = append(waits, a.statusRun.done)
	}
	a.refreshWG.Go(func() {
		defer func() {
			a.mu.Lock()
			a.reconcilePending = false
			a.mu.Unlock()
		}()
		for _, done := range waits {
			select {
			case <-ctx.Done():
				return
			case <-done:
			}
		}
		if ctx.Err() == nil {
			a.reconcilePass(false)
		}
	})
}
