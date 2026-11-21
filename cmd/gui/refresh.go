package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

type statusFlight struct {
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
	// dial replaces the connect that decides whether a server is reachable.
	// With hooks set and dial nil, every server counts as reachable.
	dial func(ctx context.Context, addr string) error
}

// I detach only the inputs refresh workers read, preserving configured order.
func (a *App) snapshotRefreshConfig() *config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
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
	ctx, cancel := context.WithTimeout(a.refreshContext(), healReadTimeout)
	defer cancel()
	return heal.RepoContext(ctx, cfg, source, repo)
}
func (a *App) overlayStatus(local []StatusResult) []StatusResult {
	out := make([]StatusResult, len(local))
	copy(out, local)
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range out {
		if a.upstreamGone[out[i].Source+"/"+out[i].Repo] {
			out[i].State = status.UpstreamGone.String()
		}
	}
	return out
}

func (a *App) requestStatus(emit, dirty bool) *statusFlight {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.refreshClosed {
		done := make(chan struct{})
		close(done)
		return &statusFlight{done: done}
	}
	if call := a.statusRun; call != nil {
		call.emit = call.emit || emit
		call.dirty = call.dirty || dirty
		return call
	}
	call := &statusFlight{done: make(chan struct{}), emit: emit}
	a.statusRun = call
	a.refreshWG.Go(func() {
		for {
			a.refreshMu.Lock()
			call.dirty = false
			a.refreshMu.Unlock()
			cfg := a.snapshotRefreshConfig()
			check := func(cfg *config.Config) []status.RepoStatus {
				// A cancelled sweep is incomplete; it only happens when the
				// app closes, and nothing is published then.
				results, _ := status.CheckAllContext(a.refreshContext(), cfg, statusTimeout)
				return results
			}
			if a.refreshHooks != nil && a.refreshHooks.status != nil {
				check = a.refreshHooks.status
			}
			raw := check(cfg)
			local := make([]StatusResult, len(raw))
			for i, r := range raw {
				local[i] = toStatusResult(r)
			}
			current := a.snapshotRefreshConfig()
			a.refreshMu.Lock()
			if !a.refreshClosed && (call.dirty || !reflect.DeepEqual(cfg, current)) {
				a.refreshMu.Unlock()
				continue
			}
			a.cachedStatus = local
			a.cachedStatusConfig = cfg
			call.results = local
			if call.emit && !a.refreshClosed {
				a.sink().Emit("status:updated", a.overlayStatus(local))
			}
			a.statusRun = nil
			close(call.done)
			a.refreshMu.Unlock()
			return
		}
	})
	return call
}

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
	current := a.snapshotRefreshConfig()
	s, ok := cfg.Sources[source]
	n, exists := current.Sources[source]
	if !ok || !exists || s.Account != n.Account || s.Folder != n.Folder || s.Root != n.Root {
		return false
	}
	old, ok := s.Repos[repo]
	now, exists := n.Repos[repo]
	return ok && exists && old == now && reflect.DeepEqual(cfg.Accounts[s.Account], current.Accounts[n.Account]) && reflect.DeepEqual(cfg.Global, current.Global)
}

func (a *App) fetchOne(cfg *config.Config, source, repo string) error {
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
	path := status.RepoPath(cfg, source, repo, r)
	key := filepath.Clean(path)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		key = strings.ToLower(key)
	}
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
			<-active.done
			if active.source == source && active.repo == repo && reflect.DeepEqual(active.cfg, cfg) {
				return active.err
			}
			continue
		}
		call := &repoFetchFlight{done: make(chan struct{}), cfg: cfg, source: source, repo: repo}
		a.repoFetches[key] = call
		a.refreshMu.Unlock()
		a.refreshMu.Lock()
		if a.fetchSlots == nil {
			a.fetchSlots = make(chan struct{}, fetchAllConcurrency)
		}
		slots := a.fetchSlots
		a.refreshMu.Unlock()
		var err error
		select {
		case slots <- struct{}{}:
			err = a.performFetch(cfg, source, repo, path)
			<-slots
		case <-a.refreshContext().Done():
			err = context.Canceled
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
	if !a.sameRefreshRepo(cfg, source, repo) {
		a.maintenanceMu.RUnlock()
		err := fmt.Errorf("repository settings changed during refresh")
		a.sink().Emit("fetch:done", map[string]string{"source": source, "repo": repo, "error": userMsg(err)})
		return err
	}
	a.sink().Emit("fetch:start", map[string]string{"source": source, "repo": repo})
	report := a.healRepo(cfg, source, repo)
	a.maintenanceMu.RUnlock()
	var err error
	addr := serverAddr(cfg.Accounts[cfg.Sources[source].Account])
	if len(report.Warnings) > 0 {
		err = fmt.Errorf("repository maintenance: %s", strings.Join(report.Warnings, "; "))
	} else {
		fetch := git.FetchCapturedContext
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
	if errors.Is(err, errServerUnreachable) {
		describeUnreachable(payload, addr)
	} else if err != nil {
		describeFetchFailure(payload, err)
		if git.IsUpstreamGoneError(err) {
			payload["upstreamGone"] = true
		}
	}
	if a.sameRefreshRepo(cfg, source, repo) {
		if err == nil {
			a.markUpstreamGone(source, repo, false)
		} else if git.IsUpstreamGoneError(err) {
			a.markUpstreamGone(source, repo, true)
		}
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
	parent := a.refreshContext()
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
	a.refreshMu.Lock()
	if a.refreshClosed {
		a.refreshMu.Unlock()
		return context.Canceled
	}
	if active := a.fetchRun; active != nil {
		a.refreshMu.Unlock()
		<-active.done
		return active.err
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
	<-call.done
	return call.err
}

func (a *App) fetchCycle() error {
	var errs []error
	for range 2 {
		errs = nil
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
		if a.anyServerReachable(ctx, cfg) {
			network.Go(func() { a.probeUpstreamExistence() })
		}
		network.Go(func() { a.verifyRefreshCredentials(cfg) })
		sem := make(chan struct{}, fetchAllConcurrency)
		var workers sync.WaitGroup
		var errorsMu sync.Mutex
		fail := func(err error) {
			errorsMu.Lock()
			errs = append(errs, err)
			errorsMu.Unlock()
		}
		// lost holds the servers a fetch of this cycle could not reach
		// although the connect passed: the rest of their queue is skipped.
		var lost sync.Map
		for _, source := range cfg.SourceOrder {
			src := cfg.Sources[source]
			addr := serverAddr(cfg.Accounts[src.Account])
			for _, repo := range src.RepoOrder {
				path := status.RepoPath(cfg, source, repo, src.Repos[repo])
				if !git.IsRepo(path) {
					continue
				}
				if _, gone := lost.Load(addr); gone || !a.reachable(ctx, addr) {
					fail(a.skipUnreachable(source, repo, addr))
					continue
				}
				// Waiting for a turn ends when the app closes: nothing new
				// starts then.
				if ctx.Err() != nil {
					fail(ctx.Err())
					continue
				}
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					fail(ctx.Err())
					continue
				}
				workers.Go(func() {
					defer func() { <-sem }()
					if _, gone := lost.Load(addr); gone {
						fail(a.skipUnreachable(source, repo, addr))
						return
					}
					if err := a.fetchOne(cfg, source, repo); err != nil {
						if isConnectionFailure(err) && addr != "" {
							lost.Store(addr, true)
						}
						fail(err)
					}
				})
			}
		}
		workers.Wait()
		// I publish local changes before waiting for remote checks.
		local := a.requestStatus(true, true)
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

func (a *App) invalidateCredential(key string) {
	a.mu.Lock()
	if a.credentialRevision == nil {
		a.credentialRevision = make(map[string]uint64)
	}
	a.credentialRevision[key]++
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
