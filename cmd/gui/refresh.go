package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
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
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
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
	emit       func(string, ...any)
}

// I detach only the inputs refresh workers read, preserving configured order.
func (a *App) snapshotRefreshConfig() *config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.cfg
	if c == nil {
		return &config.Config{Accounts: map[string]config.Account{}, Sources: map[string]config.Source{}}
	}
	out := &config.Config{Global: config.GlobalConfig{Folder: c.Global.Folder}, Accounts: make(map[string]config.Account, len(c.Accounts)), Sources: make(map[string]config.Source, len(c.Sources)), SourceOrder: append([]string(nil), c.OrderedSourceKeys()...)}
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
		dst := config.Source{Account: src.Account, Folder: src.Folder, Repos: make(map[string]config.Repo, len(src.Repos)), RepoOrder: append([]string(nil), src.OrderedRepoKeys()...)}
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
func (a *App) stopRefresh() {
	a.refreshMu.Lock()
	a.refreshClosed = true
	a.refreshContextLocked()
	a.refreshCancel()
	a.refreshMu.Unlock()
	a.refreshWG.Wait()
}
func (a *App) emitRefreshEvent(name string, args ...any) {
	if a.refreshHooks != nil && a.refreshHooks.emit != nil {
		a.refreshHooks.emit(name, args...)
		return
	}
	if a.ctx != nil {
		wailsrt.EventsEmit(a.ctx, name, args...)
	}
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
			check := status.CheckAll
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
				a.emitRefreshEvent("status:updated", a.overlayStatus(local))
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
func (a *App) GetAllStatus() []StatusResult {
	call := a.requestStatus(false, false)
	<-call.done
	return a.overlayStatus(call.results)
}

// RefreshStatus retains at most one pending invalidation while local work runs.
func (a *App) RefreshStatus() { a.requestStatus(true, true); a.probeUpstreamExistenceAsync() }

// I overlay provider results without spawning another fleet of git processes.
func (a *App) emitCachedStatus() {
	current := a.snapshotRefreshConfig()
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.cachedStatusConfig != nil && reflect.DeepEqual(current, a.cachedStatusConfig) && !a.refreshClosed {
		a.emitRefreshEvent("status:updated", a.overlayStatus(a.cachedStatus))
	}
}

func (a *App) sameRefreshRepo(cfg *config.Config, source, repo string) bool {
	current := a.snapshotRefreshConfig()
	s, ok := cfg.Sources[source]
	n, exists := current.Sources[source]
	if !ok || !exists || s.Account != n.Account || s.Folder != n.Folder {
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
		a.emitRefreshEvent("fetch:done", map[string]string{"source": source, "repo": repo, "error": err.Error()})
		return err
	}
	r, ok := src.Repos[repo]
	if !ok {
		err := fmt.Errorf("repo %q not found", repo)
		a.emitRefreshEvent("fetch:done", map[string]string{"source": source, "repo": repo, "error": err.Error()})
		return err
	}
	path := status.ResolveRepoPath(config.ExpandTilde(cfg.Global.Folder), src.EffectiveFolder(source), repo, r)
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

// refusedFastRetryDelay is how long a fetch refused within milliseconds
// waits before its one retry.
const refusedFastRetryDelay = 3 * time.Second

func (a *App) performFetch(cfg *config.Config, source, repo, path string) error {
	a.maintenanceMu.RLock()
	if !a.sameRefreshRepo(cfg, source, repo) {
		a.maintenanceMu.RUnlock()
		err := fmt.Errorf("repository settings changed during refresh")
		a.emitRefreshEvent("fetch:done", map[string]string{"source": source, "repo": repo, "error": err.Error()})
		return err
	}
	a.emitRefreshEvent("fetch:start", map[string]string{"source": source, "repo": repo})
	repair := heal.Repo
	if a.refreshHooks != nil && a.refreshHooks.heal != nil {
		repair = a.refreshHooks.heal
	}
	report := repair(cfg, source, repo)
	a.maintenanceMu.RUnlock()
	var err error
	if len(report.Warnings) > 0 {
		err = fmt.Errorf("repository maintenance: %s", strings.Join(report.Warnings, "; "))
	} else {
		fetch := git.FetchCapturedContext
		if a.refreshHooks != nil && a.refreshHooks.fetch != nil {
			fetch = a.refreshHooks.fetch
		}
		ctx := a.refreshContext()
		_, err = fetch(ctx, path)
		if git.IsRefusedFast(err) {
			// A connect refused within milliseconds right after launch is
			// usually macOS Local Network privacy still checking the new
			// process; one retry a moment later rides it out. A server
			// that is really down times out instead and isn't retried.
			select {
			case <-ctx.Done():
			case <-time.After(refusedFastRetryDelay):
				_, err = fetch(ctx, path)
			}
		}
	}
	payload := map[string]any{"source": source, "repo": repo}
	if err != nil {
		payload["error"] = err.Error()
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
	a.emitRefreshEvent("fetch:done", payload)
	return err
}

// FetchAllRepos joins the active cycle, including its final status and provider work.
func (a *App) FetchAllRepos() error {
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
		a.emitRefreshEvent("fetch:allstart")
		err := a.fetchCycle()
		payload := map[string]string{}
		if err != nil {
			payload["error"] = err.Error()
		}
		a.emitRefreshEvent("fetch:alldone", payload)
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
		var network sync.WaitGroup
		network.Go(func() { a.probeUpstreamExistence() })
		network.Go(func() { a.verifyRefreshCredentials(cfg) })
		sem := make(chan struct{}, fetchAllConcurrency)
		var workers sync.WaitGroup
		var errorsMu sync.Mutex
		for _, source := range cfg.SourceOrder {
			src := cfg.Sources[source]
			for _, repo := range src.RepoOrder {
				path := status.ResolveRepoPath(config.ExpandTilde(cfg.Global.Folder), src.EffectiveFolder(source), repo, src.Repos[repo])
				if !git.IsRepo(path) {
					continue
				}
				sem <- struct{}{}
				workers.Go(func() {
					defer func() { <-sem }()
					if err := a.fetchOne(cfg, source, repo); err != nil {
						errorsMu.Lock()
						errs = append(errs, err)
						errorsMu.Unlock()
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
	for key := range cfg.Accounts {
		sem <- struct{}{}
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
		a.emitRefreshEvent("credentials:updated", results)
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
		r := check(ctx, acct, key, cfg)
		if r.Overall == credential.StatusOffline && time.Since(started) < time.Second {
			// Offline within a second means the connection was refused, not
			// timed out: right after launch macOS can refuse LAN connections
			// for a moment, so check once more a little later, as fetches do.
			select {
			case <-ctx.Done():
			case <-time.After(refusedFastRetryDelay):
				r = check(ctx, acct, key, cfg)
			}
		}
		cancel()
		result := CredentialStatus{Status: r.Overall.String(), Message: r.PrimaryDetail, Primary: r.Primary.String(), PrimaryMsg: r.PrimaryDetail, PAT: r.PAT.String(), PATMsg: r.PATDetail, revision: revision}
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
func (a *App) CredentialVerify(key string) CredentialStatus {
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
