package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/diag"
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
func (s *localSession) snapshotRefreshConfig() *config.Config {
	cfg, _ := s.refreshSnapshot()
	return cfg
}

func (s *localSession) refreshSnapshot() (*config.Config, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshConfigLocked(), s.configRevision
}

func (s *localSession) refreshConfigLocked() *config.Config {
	c := s.cfg
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

func (s *localSession) refreshContextLocked() context.Context {
	if s.refreshCtx == nil {
		s.refreshCtx, s.refreshCancel = context.WithCancel(context.Background())
	}
	return s.refreshCtx
}
func (s *localSession) refreshContext() context.Context {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshContextLocked()
}

// stopRefresh cancels every piece of work in flight and waits for it to
// stop, for shutdownWait at most. The cancel already reached the git
// processes and what they started, so a worker still running after that is
// stuck outside git, and closing the app does not wait for it.
func (s *localSession) stopRefresh() {
	s.refreshMu.Lock()
	s.refreshClosed = true
	s.refreshContextLocked()
	s.refreshCancel()
	s.refreshMu.Unlock()
	done := make(chan struct{})
	go func() {
		s.refreshWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownWait):
	}
}

// goTracked runs work as part of what stopRefresh waits for. It reports
// false, without running it, when the app is closing.
func (s *localSession) goTracked(work func()) bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.refreshClosed {
		return false
	}
	s.refreshWG.Go(work)
	return true
}

// healRepo repairs a clone's configuration before network work. Its reads
// are bounded and stop when the app closes.
func (s *localSession) healRepo(cfg *config.Config, source, repo string) heal.Report {
	return s.healRepoContext(s.refreshContext(), cfg, source, repo)
}

func (s *localSession) healRepoContext(ctx context.Context, cfg *config.Config, source, repo string) heal.Report {
	if s.refreshHooks != nil && s.refreshHooks.heal != nil {
		return s.refreshHooks.heal(cfg, source, repo)
	}
	if s.app.isReadOnly() {
		return heal.Report{} // the clone stays as it is
	}
	ctx, cancel := context.WithTimeout(ctx, healReadTimeout)
	defer cancel()
	return heal.RepoContext(ctx, cfg, source, repo)
}
func (s *localSession) overlayStatus(local []StatusResult) []StatusResult {
	out := make([]StatusResult, len(local))
	copy(out, local)
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, check := range s.statusNetwork {
		if live, ok := statusRepoIdentity(s.cfg, key); !ok || live != check.identity {
			delete(s.statusNetwork, key)
		}
	}
	for key, identity := range s.pullRepairs {
		if live, ok := statusRepoIdentity(s.cfg, key); !ok || live != identity {
			delete(s.pullRepairs, key)
		}
	}
	for i := range out {
		row := &out[i]
		if s.upstreamGone[row.Source+"/"+row.Repo] {
			row.State = status.UpstreamGone.String()
		}
		row.CheckedAt = s.networkCheckedAt(*row)
	}
	return out
}

func (s *localSession) requestStatus(emit, dirty bool) *statusFlight {
	return s.requestStatusStream(emit, dirty, 0)
}

func (s *localSession) requestStatusStream(publishFinal, dirty bool, stream uint64) *statusFlight {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.refreshClosed {
		done := make(chan struct{})
		close(done)
		return &statusFlight{done: done}
	}
	if call := s.statusRun; call != nil {
		if stream != 0 {
			call.stream = stream
			call.dirty = true
		}
		call.emit = call.emit || publishFinal
		call.dirty = call.dirty || dirty
		return call
	}
	call := &statusFlight{done: make(chan struct{}), emit: publishFinal, stream: stream}
	s.statusRun = call
	s.refreshWG.Go(func() {
		for {
			s.refreshMu.Lock()
			call.dirty = false
			pass := call.stream
			s.refreshMu.Unlock()
			cfg, revision := s.refreshSnapshot()
			op := s.app.newOperation().atRevision(revision)
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
					s.refreshMu.Lock()
					defer s.refreshMu.Unlock()
					if !s.refreshClosed && !call.dirty && call.stream == pass {
						emit(op, "status:batch", StatusBatch{PassID: pass, Revision: revision, Items: s.overlayStatus(convert(raw))})
					}
				}
			}
			var raw []status.RepoStatus
			var err error
			if s.refreshHooks != nil && s.refreshHooks.status != nil {
				raw = s.refreshHooks.status(cfg)
				if publish != nil {
					for i := 0; i < len(raw); i += 8 {
						publish(raw[i:min(i+8, len(raw))])
					}
				}
			} else {
				raw, err = status.CheckAllBatches(s.refreshContext(), cfg, statusTimeout, publish)
			}
			s.app.recordActivity(diag.Status, sweepStarted, time.Now(), nil, "", err)
			local := convert(raw)
			current, currentRevision := s.refreshSnapshot()
			s.refreshMu.Lock()
			if !s.refreshClosed && (call.dirty || revision != currentRevision || !reflect.DeepEqual(cfg, current)) {
				s.refreshMu.Unlock()
				continue
			}
			s.cachedStatus = local
			s.cachedStatusConfig = cfg
			call.results = local
			if call.emit && !s.refreshClosed {
				emit(op, "status:updated", s.overlayStatus(local))
			}
			if pass != 0 && !s.refreshClosed {
				emit(op, "status:batch", StatusBatch{PassID: pass, Revision: revision, Done: true})
			}
			s.statusRun = nil
			close(call.done)
			s.refreshMu.Unlock()
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
func (s *localSession) getAllStatusLocal() []StatusResult {
	call := s.requestStatus(false, false)
	<-call.done
	return s.overlayStatus(call.results)
}

// RefreshStatus retains at most one pending invalidation while local work runs.
func (a *App) RefreshStatus() { a.be().RefreshStatus() }

// refreshStatusLocal is the local backend's RefreshStatus.
func (s *localSession) refreshStatusLocal() {
	s.requestStatus(true, true)
	s.probeUpstreamExistenceAsync()
}

// I overlay provider results without spawning another fleet of git processes.
func (s *localSession) emitCachedStatus() {
	current, revision := s.refreshSnapshot()
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.cachedStatusConfig != nil && reflect.DeepEqual(current, s.cachedStatusConfig) && !s.refreshClosed {
		emit(s.app.newOperation().atRevision(revision), "status:updated", s.overlayStatus(s.cachedStatus))
	}
}

func (s *localSession) sameRefreshRepo(cfg *config.Config, source, repo string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sameRefreshRepoLocked(cfg, source, repo)
}

func (s *localSession) sameRefreshRepoLocked(cfg *config.Config, source, repo string) bool {
	current := s.cfg
	if current == nil {
		return false
	}
	src, ok := cfg.Sources[source]
	n, exists := current.Sources[source]
	if !ok || !exists || src.Account != n.Account || src.Folder != n.Folder || src.Root != n.Root {
		return false
	}
	old, ok := src.Repos[repo]
	now, exists := n.Repos[repo]
	account, hadAccount := cfg.Accounts[src.Account]
	live, hasAccount := current.Accounts[n.Account]
	return ok && exists && old == now && hadAccount && hasAccount && reflect.DeepEqual(account, live) &&
		slices.Equal(cfg.Global.Roots, current.Global.Roots) &&
		reflect.DeepEqual(cfg.Global.CredentialGCM, current.Global.CredentialGCM) &&
		reflect.DeepEqual(cfg.Global.CredentialSSH, current.Global.CredentialSSH) &&
		reflect.DeepEqual(cfg.Global.CredentialToken, current.Global.CredentialToken)
}

func (s *localSession) fetchOne(cfg *config.Config, revision uint64, source, repo string, lost *sync.Map) error {
	op := s.app.newOperation().atRevision(revision)
	src, ok := cfg.Sources[source]
	if !ok {
		err := fmt.Errorf("source %q not found", source)
		emit(op, "fetch:done", fetchDoneDTO{fetchEventDTO: fetchEventDTO{Source: source, Repo: repo, Error: userMsg(err)}})
		return err
	}
	r, ok := src.Repos[repo]
	if !ok {
		err := fmt.Errorf("repo %q not found", repo)
		emit(op, "fetch:done", fetchDoneDTO{fetchEventDTO: fetchEventDTO{Source: source, Repo: repo, Error: userMsg(err)}})
		return err
	}
	account := cfg.Accounts[src.Account]
	addr := serverAddr(account)
	ctx := s.refreshContext()
	path := status.RepoPath(cfg, source, repo, r)
	key := pathkey.Compare(path)
	for {
		s.refreshMu.Lock()
		if s.refreshClosed {
			s.refreshMu.Unlock()
			return context.Canceled
		}
		if s.repoFetches == nil {
			s.repoFetches = make(map[string]*repoFetchFlight)
		}
		if active := s.repoFetches[key]; active != nil {
			s.refreshMu.Unlock()
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
		s.repoFetches[key] = call
		s.refreshMu.Unlock()
		var err error
		if lost != nil && fetchEndpointLost(lost, addr) {
			started := time.Now()
			err = skipUnreachable(op, source, repo, addr)
			s.app.recordActivity(diag.Fetch, started, time.Now(), nil, path, err)
		} else {
			err = s.performFetch(op, cfg, source, repo, path, lost)
			if lost != nil && addr != "" && isConnectionFailure(err) {
				lost.Store(addr, true)
			}
		}
		s.refreshMu.Lock()
		call.err = err
		delete(s.repoFetches, key)
		close(call.done)
		s.refreshMu.Unlock()
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

func (s *localSession) performFetch(op eventOperation, cfg *config.Config, source, repo, path string, lost *sync.Map) (err error) {
	started := time.Now()
	var queueDepth *int
	defer func() { s.app.recordActivity(diag.Fetch, started, time.Now(), queueDepth, path, err) }()
	ctx := s.refreshContext()
	report, unlock, err := s.app.prepareClone(ctx, cfg, repoRef{source, repo}, path, func() {
		emit(op, "fetch:start", fetchEventDTO{Source: source, Repo: repo})
	}, nil)
	if err != nil {
		emit(op, "fetch:done", fetchDoneDTO{fetchEventDTO: fetchEventDTO{Source: source, Repo: repo, Error: userMsg(err)}})
		return err
	}
	defer unlock()
	account := cfg.Accounts[cfg.Sources[source].Account]
	addr := serverAddr(account)
	if len(report.Trust) > 0 {
		err = report.Trust[0].Err()
	} else if len(report.Warnings) > 0 {
		err = fmt.Errorf("repository maintenance: %s", strings.Join(report.Warnings, "; "))
	} else {
		remote := "origin"
		if cfg.Sources[source].Repos[repo].FetchAllRemotes {
			remote = "--all"
		}
		fetch := func(ctx context.Context, path string) (string, error) {
			return git.FetchCapturedContext(ctx, path, remote)
		}
		if s.refreshHooks != nil && s.refreshHooks.fetch != nil {
			fetch = s.refreshHooks.fetch
		}
		err = s.onServer(addr, func(ctx context.Context) error {
			release, depth, err := s.fetches.acquire(ctx, fetchHostname(account.URL))
			queueDepth = &depth
			if err != nil {
				return err
			}
			defer release()
			if lost != nil && fetchEndpointLost(lost, addr) {
				return errServerUnreachable
			}
			if !s.sameRefreshRepo(cfg, source, repo) {
				return errCloneSettingsChanged
			}
			ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
			defer cancel()
			started := time.Now()
			_, err = fetch(ctx, path)
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
	payload := fetchDoneDTO{fetchEventDTO: fetchEventDTO{Source: source, Repo: repo}}
	if report.Skipped == "" && !s.app.isReadOnly() {
		payload.RepairWarnings = append([]string{}, report.OtherWarnings()...)
	}
	if errors.Is(err, errServerUnreachable) {
		payload.Error, payload.Code, payload.Host = unreachableDetails(addr)
		payload.Skipped = true
	} else if err != nil {
		describeFetchFailure(&payload.fetchEventDTO, err)
		if git.IsUpstreamGoneError(err) {
			payload.UpstreamGone = true
		}
	}
	if (err == nil || git.IsUpstreamGoneError(err)) && s.sameRefreshRepo(cfg, source, repo) {
		s.recordFetchStatus(cfg, source, repo, err != nil, time.Now())
	}
	emit(s.currentRepoOperation(op, cfg, source, repo), "fetch:done", payload)
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
func (s *localSession) onServer(addr string, work func(context.Context) error) error {
	return s.onServerCtx(s.refreshContext(), addr, work)
}

// onServerCtx is onServer for work that parent can also stop, such as a
// pull a remote window asked for: its request ends with the connection.
func (s *localSession) onServerCtx(parent context.Context, addr string, work func(context.Context) error) error {
	ctx, end, ok := s.beginNetworkOp(parent, addr)
	if !ok {
		return errServerUnreachable
	}
	err := work(ctx)
	stopped := ctx.Err() != nil && parent.Err() == nil
	end()
	if err == nil {
		return nil
	}
	if stopped || isConnectionFailure(err) && !s.confirmReachable(parent, addr) {
		return errServerUnreachable
	}
	return err
}

// unreachableDetails reports an operation the network guard stopped or
// skipped because its server did not answer.
func unreachableDetails(addr string) (message, code, host string) {
	host, _, _ = net.SplitHostPort(addr)
	return userMsg(errServerUnreachable), "git." + string(git.CodeOffline), host
}

// skipUnreachable reports a clone as failed without trying it: its server is
// unreachable.
func skipUnreachable(op eventOperation, source, repo, addr string) error {
	payload := fetchDoneDTO{fetchEventDTO: fetchEventDTO{Source: source, Repo: repo}}
	payload.Error, payload.Code, payload.Host = unreachableDetails(addr)
	payload.Skipped = true
	emit(op, "fetch:done", payload)
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
func (s *localSession) probeServers(ctx context.Context, cfg *config.Config) {
	var wg sync.WaitGroup
	seen := make(map[string]bool)
	for _, src := range cfg.Sources {
		addr := serverAddr(cfg.Accounts[src.Account])
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		wg.Go(func() { s.reachable(ctx, addr) })
	}
	wg.Wait()
}

// anyServerReachable reports whether at least one account's server answers.
func (s *localSession) anyServerReachable(ctx context.Context, cfg *config.Config) bool {
	for _, acct := range cfg.Accounts {
		if s.reachable(ctx, serverAddr(acct)) {
			return true
		}
	}
	return len(cfg.Accounts) == 0
}

// describeFetchFailure adds what the frontend needs to explain a failed
// fetch: the scrubbed text and, as data, the code, the server that could not
// be reached and whether the connect was refused at once. The frontend
// groups failures by these facts, never by reading the text.
func describeFetchFailure(payload *fetchEventDTO, err error) {
	ue := toUserError(err)
	payload.Error, payload.Code = ue.String(), ue.Code
	if ge, ok := errors.AsType[*git.GitError](err); ok {
		payload.Host = ge.Host
	}
	payload.UnsafeConfig = unsafeConfigOf(err)
	payload.RefusedFast = git.IsRefusedFast(err)
}

// FetchAllRepos joins the active cycle, including its final status and provider work.
func (a *App) fetchAllRepos() error { return a.be().FetchAllRepos() }

// fetchAllReposLocal is the local backend's FetchAllRepos.
func (s *localSession) fetchAllReposLocal() error {
	return s.fetchAllReposContext(s.refreshContext())
}

func (s *localSession) fetchAllReposContext(ctx context.Context) error {
	s.refreshMu.Lock()
	if s.refreshClosed {
		s.refreshMu.Unlock()
		return context.Canceled
	}
	if active := s.fetchRun; active != nil {
		s.refreshMu.Unlock()
		select {
		case <-active.done:
			return active.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	call := &fetchFlight{done: make(chan struct{})}
	s.fetchRun = call
	s.refreshWG.Go(func() {
		op := s.app.newOperation()
		emit(op, "fetch:allstart", any(nil))
		err := s.fetchCycle()
		payload := fetchAllDoneDTO{}
		if err != nil {
			payload.Error = userMsg(err)
		}
		emit(op.currentRevision(), "fetch:alldone", payload)
		s.refreshMu.Lock()
		call.err = err
		s.fetchRun = nil
		close(call.done)
		s.refreshMu.Unlock()
	})
	s.refreshMu.Unlock()
	select {
	case <-call.done:
		return call.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *localSession) fetchCycle() (err error) {
	started := time.Now()
	defer func() { s.app.recordActivity(diag.FetchAll, started, time.Now(), nil, "", err) }()
	var errs []error
	for range 2 {
		cfg, revision := s.refreshSnapshot()
		// One short connect per server decides, before anything is queued,
		// which servers are there. Nothing goes out to one that is not.
		ctx := s.refreshContext()
		wasDown := s.forgetNetworkDown()
		s.probeServers(ctx, cfg)
		if wasDown && len(s.downServers()) == 0 {
			s.publishNetworkState() // the network is back
		}
		var network sync.WaitGroup
		reachable := s.anyServerReachable(ctx, cfg)
		if reachable {
			network.Go(func() { s.probeUpstreamExistence() })
		}
		network.Go(func() { s.verifyRefreshCredentials(cfg) })
		errs = s.fetchClones(ctx, cfg, revision)
		// I publish local changes before waiting for remote checks.
		local := s.requestStatus(true, reachable)
		<-local.done
		network.Wait()
		s.emitCachedStatus()
		if s.refreshContext().Err() != nil {
			return s.refreshContext().Err()
		}
		if reflect.DeepEqual(cfg, s.snapshotRefreshConfig()) {
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

func (s *localSession) fetchClones(ctx context.Context, cfg *config.Config, revision uint64) []error {
	var clones []repoRef
	for _, source := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[source]
		for _, repo := range src.OrderedRepoKeys() {
			if git.IsRepo(status.RepoPath(cfg, source, repo, src.Repos[repo])) {
				clones = append(clones, repoRef{source, repo})
			}
		}
	}

	var mu sync.Mutex
	var errs []error
	// A connection can fail after the reachability check. Skip the
	// remaining jobs for that endpoint this cycle.
	var lost sync.Map
	dispatchByHost(ctx, cfg, clones, func(ref repoRef) {
		addr := serverAddr(cfg.Accounts[cfg.Sources[ref.Source].Account])
		var err error
		if fetchEndpointLost(&lost, addr) || !s.reachable(ctx, addr) {
			err = skipUnreachable(s.app.newOperation().atRevision(revision), ref.Source, ref.Repo, addr)
		} else {
			err = s.fetchOne(cfg, revision, ref.Source, ref.Repo, &lost)
		}
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	})
	return errs
}

func (s *localSession) verifyRefreshCredentials(cfg *config.Config) {
	results := make(map[string]CredentialStatus, len(cfg.Accounts))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	ctx := s.refreshContext()
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
			result := s.credentialVerify(cfg, key)
			mu.Lock()
			results[key] = result
			mu.Unlock()
		})
	}
	wg.Wait()
	current := s.snapshotRefreshConfig()
	for key, result := range results {
		if !s.credentialStillCurrent(cfg, key, result.revision) {
			delete(results, key)
		}
	}
	if reflect.DeepEqual(cfg.Accounts, current.Accounts) && reflect.DeepEqual(cfg.Global, current.Global) {
		emit(s.app.newOperation(), "credentials:updated", results)
	}
}

func (s *localSession) credentialVerify(cfg *config.Config, key string) CredentialStatus {
	for {
		acct, ok := cfg.Accounts[key]
		if !ok {
			return CredentialStatus{Status: "error", Message: "account not found"}
		}
		s.mu.Lock()
		revision := s.credentialRevision[key]
		s.mu.Unlock()
		checkIdentity := credentialCheckIdentity(cfg, key, revision)
		identity := key + "\x00" + checkIdentity
		s.refreshMu.Lock()
		if s.credentialRuns == nil {
			s.credentialRuns = make(map[string]*credentialFlight)
		}
		if active := s.credentialRuns[identity]; active != nil {
			s.refreshMu.Unlock()
			<-active.done
			if s.credentialStillCurrent(cfg, key, revision) {
				return active.result
			}
			cfg = s.snapshotRefreshConfig()
			continue
		}
		call := &credentialFlight{done: make(chan struct{})}
		s.credentialRuns[identity] = call
		s.refreshMu.Unlock()
		check := credential.CheckContext
		if s.refreshHooks != nil && s.refreshHooks.credential != nil {
			check = s.refreshHooks.credential
		}
		ctx, cancel := context.WithTimeout(s.refreshContext(), 30*time.Second)
		started := time.Now()
		var r credential.StatusResult
		if !s.reachable(ctx, serverAddr(acct)) {
			// The server is not there: say so at once instead of letting
			// the check, and its retries, time out against it.
			r = credential.StatusResult{Primary: credential.StatusOffline, Overall: credential.StatusOffline,
				PrimaryDetail: userMsg(errServerUnreachable), PrimaryFailure: errServerUnreachable}
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
		completed := ctx.Err() == nil
		cancel()
		result := CredentialStatus{Status: r.Overall.String(), Message: userText(r.PrimaryDetail), Primary: r.Primary.String(), PrimaryMsg: userText(r.PrimaryDetail), PAT: r.PAT.String(), PATMsg: userText(r.PATDetail), Problem: problem(r.PrimaryFailure), revision: revision}
		if !r.ExpiresAt.IsZero() {
			result.ExpiresAt = r.ExpiresAt.UTC().Format(time.RFC3339)
		}
		s.refreshMu.Lock()
		if !started.Before(s.proofChecks[key].started) {
			verification := heal.Verification{Failed: true}
			if cached := s.proofChecks[key]; cached.identity == checkIdentity {
				verification.CheckedAt, verification.ExpiresAt = cached.value.CheckedAt, cached.value.ExpiresAt
			}
			if completed && r.Primary == credential.StatusOK {
				verification = heal.Verification{CheckedAt: time.Now(), ExpiresAt: r.ExpiresAt}
			}
			if s.proofChecks == nil {
				s.proofChecks = make(map[string]proofVerification)
			}
			s.proofChecks[key] = proofVerification{identity: checkIdentity, started: started, value: verification}
		}
		call.result = result
		delete(s.credentialRuns, identity)
		close(call.done)
		s.refreshMu.Unlock()
		if s.credentialStillCurrent(cfg, key, revision) || s.refreshContext().Err() != nil {
			return result
		}
		cfg = s.snapshotRefreshConfig()
	}
}

func (s *localSession) credentialStillCurrent(cfg *config.Config, key string, revision uint64) bool {
	s.mu.Lock()
	sameRevision := s.credentialRevision[key] == revision
	s.mu.Unlock()
	current := s.snapshotRefreshConfig()
	return sameRevision && reflect.DeepEqual(cfg.Accounts[key], current.Accounts[key]) && reflect.DeepEqual(cfg.Global.CredentialSSH, current.Global.CredentialSSH)
}

func (s *localSession) bumpCredentialRevisionLocked(key string) {
	if s.credentialRevision == nil {
		s.credentialRevision = make(map[string]uint64)
	}
	s.credentialRevision[key]++
}

func (s *localSession) invalidateCredential(key string) {
	s.mu.Lock()
	s.bumpCredentialRevisionLocked(key)
	s.mu.Unlock()
	s.probeMu.Lock()
	s.lastProbeAt = time.Time{}
	s.probeMu.Unlock()
}

// CredentialVerify is explicitly fresh after each completed check; only active work is shared.
func (a *App) CredentialVerify(key string) CredentialStatus { return a.be().CredentialVerify(key) }

// credentialVerifyLocal is the local backend's CredentialVerify.
func (s *localSession) credentialVerifyLocal(key string) CredentialStatus {
	s.refreshMu.Lock()
	if s.refreshClosed {
		s.refreshMu.Unlock()
		return CredentialStatus{Status: "offline", Message: context.Canceled.Error()}
	}
	s.refreshWG.Add(1)
	s.refreshMu.Unlock()
	defer s.refreshWG.Done()
	return s.credentialVerify(s.snapshotRefreshConfig(), key)
}

// I coalesce stale reconcile requests into one follow-up after the current
// refresh. The follow-up cannot schedule itself again when edits keep landing.
func (s *localSession) retryReconcileAfterRefresh() {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.refreshClosed {
		return
	}
	s.mu.Lock()
	if s.reconcilePending {
		s.mu.Unlock()
		return
	}
	s.reconcilePending = true
	s.mu.Unlock()
	ctx := s.refreshContextLocked()
	var waits []<-chan struct{}
	if s.fetchRun != nil {
		waits = append(waits, s.fetchRun.done)
	}
	if s.statusRun != nil {
		waits = append(waits, s.statusRun.done)
	}
	s.refreshWG.Go(func() {
		defer func() {
			s.mu.Lock()
			s.reconcilePending = false
			s.mu.Unlock()
		}()
		for _, done := range waits {
			select {
			case <-ctx.Done():
				return
			case <-done:
			}
		}
		if ctx.Err() == nil {
			s.runReconcilePass(false, s.app.reconcileServices())
		}
	})
}
