//go:build (linux || darwin) && !gitboxserver

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// A remote window against an in-process daemon (#247), through the real
// proxy, with pipes where ssh would be. Cutting the pipes is what a dead
// ssh, a closed laptop lid or a network blip does to the window: these
// tests are the automated stand-in for the lid-close smoke.

// daemonLink is one ssh session of the window: the proxy's stdin and
// stdout.
type daemonLink struct {
	rwc    pipeRWC
	done   chan struct{}
	stdinW *io.PipeWriter
	outR   *io.PipeReader
	cutErr atomic.Bool
}

func (c *daemonLink) Conn() io.ReadWriteCloser { return c.rwc }
func (c *daemonLink) Done() <-chan struct{}    { return c.done }
func (c *daemonLink) Close() error             { _ = c.stdinW.Close(); <-c.done; return nil }

func (c *daemonLink) Err() error {
	if c.cutErr.Load() {
		return &remote.Error{Code: remote.CodeSSHFailed, Detail: "ssh ended"}
	}
	return nil
}

// cut ends the session the way a killed ssh does: both streams break at
// once.
func (c *daemonLink) cut() {
	c.cutErr.Store(true)
	_ = c.stdinW.CloseWithError(errors.New("ssh killed"))
	_ = c.outR.CloseWithError(errors.New("ssh killed"))
	<-c.done
}

// daemonDialer dials through a proxy of h. env builds each proxy's
// environment; fail makes the next dials fail.
type daemonDialer struct {
	mu    sync.Mutex
	links []*daemonLink
	dials int
	fail  bool
}

func (d *daemonDialer) last() *daemonLink {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.links[len(d.links)-1]
}

func (d *daemonDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dials
}

func (d *daemonDialer) failNext(fail bool) {
	d.mu.Lock()
	d.fail = fail
	d.mu.Unlock()
}

func (d *daemonDialer) dialer(env func(clientID string) proxyEnv) remoteDialer {
	return func(ctx context.Context, alias string, o remote.Options) (remoteConn, error) {
		d.mu.Lock()
		d.dials++
		fail := d.fail
		d.mu.Unlock()
		if !o.Daemon {
			return nil, errors.New("a remote window must ask for the daemon")
		}
		if fail {
			return nil, &remote.Error{Code: remote.CodeSSHTimeout, Detail: "the host does not answer"}
		}
		stdinR, stdinW := io.Pipe()
		outR, outW := io.Pipe()
		link := &daemonLink{rwc: pipeRWC{outR, stdinW}, done: make(chan struct{}), stdinW: stdinW, outR: outR}
		go func() {
			runProxy(env(o.ClientID), stdinR, outW, io.Discard)
			_ = outW.Close()
			_ = stdinR.Close()
			close(link.done)
		}()
		d.mu.Lock()
		d.links = append(d.links, link)
		d.mu.Unlock()
		return link, nil
	}
}

// waitFor polls cond for up to 10 seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func logHas(a *App, sub string) bool {
	for _, line := range a.RemoteLog() {
		if strings.Contains(line, sub) {
			return true
		}
	}
	return false
}

// TestRemoteWindowReconnectsToTheDaemon: the link drops in the middle of
// a clone and while a fleet fetch finishes on the host. The window
// connects again by itself, gets the fetch events it missed, and the
// clone's retry, with its first request id, returns the daemon's result:
// the clone ran once and its done event reached the window once.
func TestRemoteWindowReconnectsToTheDaemon(t *testing.T) {
	h := newDaemonHost(t)
	var host *App
	var clones atomic.Int32
	cloneStarted, release := make(chan struct{}, 1), make(chan struct{})
	d := h.config(time.Hour)
	d.prepare = func(a *App, srv *rpc.Server) {
		host = a
		srv.Handlers["CloneRepo"] = rpc.Func(func(ctx context.Context, source, repo, root string) error {
			clones.Add(1)
			cloneStarted <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			emit(a.newOperation(), "clone:done", cloneDoneDTO{Source: source, Repo: repo, Path: "/host/git/" + repo})
			return nil
		})
	}
	h.startDaemon(t, d)

	dialer := &daemonDialer{}
	a, sink := remoteApp(t, dialer.dialer(h.env))
	a.remote.policy = remote.ReconnectPolicy{Backoff: []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}}
	a.Startup(context.Background())
	a.events = sink
	a.remote.start()
	waitState(t, a, remoteConnected)
	if !logHas(a, "connect: the host started its daemon") && !logHas(a, "connect: joined the host's daemon") {
		t.Errorf("the log does not say how the host serves the session:\n%s", strings.Join(a.RemoteLog(), "\n"))
	}

	a.CloneRepo("acct", "org/repo", "")
	<-cloneStarted
	dialer.last().cut()
	// While the window is away, a fleet fetch ends on the host.
	emit(host.newOperation(), "fetch:done", fetchDoneDTO{fetchEventDTO: fetchEventDTO{Source: "acct", Repo: "org/repo"}})
	emit(host.newOperation(), "fetch:alldone", fetchAllDoneDTO{})

	waitFor(t, "the window to connect again", func() bool { return dialer.count() >= 2 && a.RemoteInfo().State == remoteConnected })
	var sawAttempt bool
	for _, e := range sink.named("remote:state") {
		if st := e.args[0].(RemoteStateDTO); st.State == remoteConnecting && st.Attempt == 1 && st.Attempts == 3 {
			sawAttempt = true
		}
	}
	if !sawAttempt {
		t.Error("the window never said it was connecting again (attempt 1 of 3)")
	}
	waitFor(t, "the missed fetch events", func() bool { return len(sink.named("fetch:alldone")) == 1 && len(sink.named("fetch:done")) == 1 })
	if !logHas(a, "connect: resumed with 2 missed events") {
		t.Errorf("the log does not say the window resumed:\n%s", strings.Join(a.RemoteLog(), "\n"))
	}

	close(release)
	waitFor(t, "clone:done", func() bool { return len(sink.named("clone:done")) >= 1 })
	time.Sleep(200 * time.Millisecond)
	dones := sink.named("clone:done")
	if len(dones) != 1 || clones.Load() != 1 {
		t.Fatalf("%d clone:done events after %d clones, want one of each", len(dones), clones.Load())
	}
	if raw, _ := json.Marshal(dones[0].args[0]); !strings.Contains(string(raw), `"path":"/host/git/org/repo"`) || strings.Contains(string(raw), `"error"`) {
		t.Errorf("clone:done = %s, want the daemon's result", raw)
	}
	a.Shutdown(context.Background())
}

// TestRemoteWindowDisconnectLeavesHostWorkRunning (D2): Disconnect and
// closing the window detach without cancelling; the host's work goes on
// and its outcome arrives with the next connection.
func TestRemoteWindowDisconnectLeavesHostWorkRunning(t *testing.T) {
	h := newDaemonHost(t)
	var cancelled atomic.Bool
	started, release, finished := make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	d := h.config(time.Hour)
	d.prepare = func(a *App, srv *rpc.Server) {
		srv.Handlers["PullRepo"] = rpc.Func(func(ctx context.Context, source, repo string) error {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				cancelled.Store(true)
				return ctx.Err()
			}
			emit(a.newOperation(), "pull:done", pullDoneDTO{Source: source, Repo: repo})
			close(finished)
			return nil
		})
	}
	h.startDaemon(t, d)
	dialer := &daemonDialer{}
	a, sink := remoteApp(t, dialer.dialer(h.env))
	a.Startup(context.Background())
	a.events = sink
	a.remote.start()
	waitState(t, a, remoteConnected)

	a.PullRepo("acct", "org/repo")
	<-started
	a.RemoteDisconnect()
	waitState(t, a, remoteDisconnected)
	close(release)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the pull did not finish on the host after Disconnect")
	}
	if cancelled.Load() {
		t.Fatal("Disconnect cancelled the host's pull")
	}
	if n := len(sink.named("pull:done")); n != 0 {
		t.Errorf("%d pull:done events while disconnected, want none yet", n)
	}
	a.RemoteReconnect()
	waitState(t, a, remoteConnected)
	waitFor(t, "the pull's outcome", func() bool { return len(sink.named("pull:done")) == 1 })
	time.Sleep(200 * time.Millisecond)
	if n := len(sink.named("pull:done")); n != 1 {
		t.Errorf("%d pull:done events, want the host's one only", n)
	}
	a.Shutdown(context.Background())
}

// TestRemoteWindowGivesUpAfterTheAttempts: when every attempt fails the
// window shows the failure, and a long call in flight fails with it.
func TestRemoteWindowGivesUpAfterTheAttempts(t *testing.T) {
	h := newDaemonHost(t)
	started := make(chan struct{}, 1)
	d := h.config(time.Hour)
	d.prepare = func(a *App, srv *rpc.Server) {
		srv.Handlers["PullRepo"] = rpc.Func(func(ctx context.Context, source, repo string) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
	}
	h.startDaemon(t, d)
	dialer := &daemonDialer{}
	a, sink := remoteApp(t, dialer.dialer(h.env))
	a.remote.policy = remote.ReconnectPolicy{Backoff: []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}}
	a.Startup(context.Background())
	a.events = sink
	a.remote.start()
	waitState(t, a, remoteConnected)
	a.PullRepo("acct", "org/repo")
	<-started
	dialer.failNext(true)
	dialer.last().cut()
	st := waitState(t, a, remoteFailed)
	if dialer.count() != 4 || st.Code != string(remote.CodeSSHTimeout) {
		t.Errorf("after %d dials the state is %+v, want three attempts that failed with ssh_timeout", dialer.count(), st)
	}
	waitFor(t, "the pull to fail in the window", func() bool { return len(sink.named("pull:done")) == 1 })
	a.Shutdown(context.Background())
}

// TestRemoteWindowWithoutTheDaemon (D4): a host that serves the session
// in the proxy's own process says so, the window writes one log line, and
// a dropped link fails as it did before the daemon: no attempt follows.
func TestRemoteWindowWithoutTheDaemon(t *testing.T) {
	h := newDaemonHost(t)
	unsafe := func(clientID string) proxyEnv {
		env := h.env(clientID)
		env.socket, env.enterErr = "", &remote.DaemonError{Reason: remote.ReasonStateUnsafe, Detail: "the state folder is open to other users"}
		return env
	}
	dialer := &daemonDialer{}
	a, sink := remoteApp(t, dialer.dialer(unsafe))
	a.Startup(context.Background())
	a.events = sink
	a.remote.start()
	waitState(t, a, remoteConnected)
	if !logHas(a, "connect: the host serves this session without the daemon (state_unsafe): work stops if the link drops") {
		t.Errorf("the log does not say the session has no daemon:\n%s", strings.Join(a.RemoteLog(), "\n"))
	}
	if _, err := a.GetConfig(); err != nil {
		t.Errorf("GetConfig on the fallback session = %v", err)
	}
	dialer.last().cut()
	waitState(t, a, remoteFailed)
	time.Sleep(100 * time.Millisecond)
	if dialer.count() != 1 {
		t.Errorf("%d dials after the fallback session dropped, want no reconnect", dialer.count())
	}
	a.Shutdown(context.Background())
}

// cancelGrace is long enough for a cancellation to cross the pipes.
const cancelGrace = 300 * time.Millisecond

// TestRemoteWindowCloseNeverCancelsHostChecks (D2): a fleet check the
// window started keeps running on the daemon when the window closes.
func TestRemoteWindowCloseNeverCancelsHostChecks(t *testing.T) {
	h := newDaemonHost(t)
	var cancelled atomic.Bool
	started, release, finished := make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	d := h.config(time.Hour)
	d.prepare = func(a *App, srv *rpc.Server) {
		srv.Handlers["RunFleetCheck"] = rpc.Func(func(ctx context.Context, name string) error {
			started <- struct{}{}
			select {
			case <-release:
				close(finished)
			case <-ctx.Done():
				cancelled.Store(true)
			}
			return ctx.Err()
		})
	}
	h.startDaemon(t, d)
	dialer := &daemonDialer{}
	a, sink := remoteApp(t, dialer.dialer(h.env))
	a.Startup(context.Background())
	a.events = sink
	a.remote.start()
	waitState(t, a, remoteConnected)
	go func() { _, _ = a.RunTask(taskFetch) }()
	<-started
	// The window's checks stop as they do when it closes, while the link is
	// still up to carry a cancellation if one were sent.
	a.tasks.stop()
	time.Sleep(cancelGrace)
	a.Shutdown(context.Background())
	close(release)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the check did not finish on the host")
	}
	if cancelled.Load() {
		t.Error("closing the window cancelled the host's check")
	}
}

// fleetFetchHost is a daemon whose fetch-all stands in for the real one:
// it counts its runs, emits the events of a run and holds before the
// last ones until the test releases it.
type fleetFetchHost struct {
	repos    []string
	runs     atomic.Int32
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func newFleetFetchHost(t *testing.T, h *daemonHost) *fleetFetchHost {
	f := &fleetFetchHost{
		repos:   []string{"org/a", "org/b", "org/c", "org/d"},
		started: make(chan struct{}, 4), release: make(chan struct{}), finished: make(chan struct{}, 4),
	}
	d := h.config(time.Hour)
	d.prepare = func(host *App, srv *rpc.Server) {
		srv.Handlers["RunFleetCheck"] = rpc.Func(func(ctx context.Context, name string) error {
			if name != taskFetch {
				return errTaskUnknown
			}
			f.runs.Add(1)
			op := host.newOperation()
			emit(op, "fetch:allstart", any(nil))
			f.started <- struct{}{}
			<-f.release
			for _, repo := range f.repos {
				emit(host.newOperation(), "fetch:done", fetchDoneDTO{fetchEventDTO: fetchEventDTO{Source: "acct", Repo: repo}})
			}
			emit(op, "fetch:alldone", fetchAllDoneDTO{})
			f.finished <- struct{}{}
			return nil
		})
	}
	h.startDaemon(t, d)
	return f
}

// connectingAt reports whether the window says, within d, that it is
// connecting again, attempt 1 of attempts.
func connectingAt(a *App, d time.Duration, attempts int) bool {
	for deadline := time.Now().Add(d); ; time.Sleep(5 * time.Millisecond) {
		st := a.RemoteInfo()
		if st.State == remoteConnecting && st.Attempt == 1 && st.Attempts == attempts {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
	}
}

// TestRemoteWindowFetchAllSurvivesACut is the lid-close smoke through the
// entry point of the top bar's Fetch all, App.RunTask: the link drops in
// the middle of the run. The window says at once that it is connecting
// again, the call waits for the next connection, its repeat joins the
// host's run instead of starting another, and the window gets every
// event of the run once: replayed when the run ended while it was away,
// live otherwise.
func TestRemoteWindowFetchAllSurvivesACut(t *testing.T) {
	for _, tc := range []struct {
		name string
		away bool // the run ends before the window is back
	}{
		{"the run ends while the window is away", true},
		{"the run ends after the window is back", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newDaemonHost(t)
			f := newFleetFetchHost(t, h)
			dialer := &daemonDialer{}
			a, sink := remoteApp(t, dialer.dialer(h.env))
			a.remote.policy = remote.ReconnectPolicy{Backoff: []time.Duration{300 * time.Millisecond, time.Second, time.Second}}
			a.Startup(context.Background())
			a.events = sink
			a.remote.start()
			waitState(t, a, remoteConnected)

			type outcome struct {
				result string
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := a.RunTask(taskFetch)
				done <- outcome{result, err}
			}()
			<-f.started
			dialer.failNext(tc.away)
			dialer.last().cut()
			if !connectingAt(a, 150*time.Millisecond, 3) {
				t.Errorf("150ms after the cut the window says %+v, want connecting, attempt 1 of 3", a.RemoteInfo())
			}
			// A call made now waits for the next connection instead of
			// failing with not_connected.
			osDone := make(chan string, 1)
			go func() { osDone <- a.be().GetOS() }()
			if tc.away {
				close(f.release)
				<-f.finished
				dialer.failNext(false)
			}
			waitFor(t, "the window to connect again", func() bool { return dialer.count() >= 2 && a.RemoteInfo().State == remoteConnected })
			if !tc.away {
				close(f.release)
			}

			select {
			case got := <-done:
				if got.err != nil || got.result != "finished" {
					t.Errorf("RunTask = %q, %v; want finished, nil", got.result, got.err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("RunTask did not return after the run ended on the host")
			}
			if got := <-osDone; got != runtime.GOOS {
				t.Errorf("a call made while connecting again returned %q (%v), want the host's OS", got, a.be().(*RemoteBackend).LastError())
			}
			waitFor(t, "fetch:alldone", func() bool { return len(sink.named("fetch:alldone")) >= 1 })
			time.Sleep(200 * time.Millisecond)
			if n := f.runs.Load(); n != 1 {
				t.Errorf("the host ran fetch-all %d times, want once", n)
			}
			if n, m := len(sink.named("fetch:done")), len(sink.named("fetch:alldone")); n != len(f.repos) || m != 1 {
				t.Errorf("the window got %d fetch:done and %d fetch:alldone, want %d and 1", n, m, len(f.repos))
			}
			states := sink.named("remote:state")
			if last := states[len(states)-1].args[0].(RemoteStateDTO); last.State != remoteConnected || !last.Resumed {
				t.Errorf("the last state is %+v, want connected and resumed", last)
			}
			a.Shutdown(context.Background())
		})
	}
}

// frozenLink is a session with a daemon that can stop answering without
// ending, as a link whose network went away does: freeze drops what
// crosses it in either direction, and nothing closes.
type frozenLink struct {
	rwc     pipeRWC
	done    chan struct{}
	frozen  chan struct{}
	closers []io.Closer
	wg      sync.WaitGroup
	once    sync.Once
}

func newFrozenLink(ctx context.Context, srv *rpc.Server, clientID string) *frozenLink {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	l := &frozenLink{rwc: pipeRWC{s2cR, c2sW}, done: make(chan struct{}), frozen: make(chan struct{}),
		closers: []io.Closer{c2sR, c2sW, s2cR, s2cW, inR, inW, outR, outW}}
	relay := func(src io.Reader, dst io.Writer) {
		buf := make([]byte, 32<<10)
		for {
			n, err := src.Read(buf)
			if err != nil {
				return
			}
			select {
			case <-l.frozen:
			default:
				if _, err := dst.Write(buf[:n]); err != nil {
					return
				}
			}
		}
	}
	l.wg.Go(func() { relay(c2sR, inW) })
	l.wg.Go(func() { relay(outR, s2cW) })
	l.wg.Go(func() { _ = srv.ServeConn(ctx, inR, outW, rpc.Peer{ClientID: clientID}) })
	return l
}

func (l *frozenLink) freeze()                  { close(l.frozen) }
func (l *frozenLink) Conn() io.ReadWriteCloser { return l.rwc }
func (l *frozenLink) Done() <-chan struct{}    { return l.done }
func (l *frozenLink) Err() error               { return nil }

func (l *frozenLink) Close() error {
	l.once.Do(func() {
		for _, c := range l.closers {
			_ = c.Close()
		}
		l.wg.Wait()
		close(l.done)
	})
	return nil
}

// TestRemoteWindowReconnectsAfterAHeartbeatDeath: the link to the daemon
// stops answering while its ssh goes on running, as after a laptop lid
// closed. The heartbeat declares it dead within three missed pings and a
// margin, and the window starts connecting again at once instead of
// waiting for ssh to give up. Time is virtual (testing/synctest).
func TestRemoteWindowReconnectsAfterAHeartbeatDeath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		srv := &rpc.Server{
			Handlers: map[string]rpc.Handler{}, ToUserError: toUserError,
			Self:       rpc.Hello{Version: version, Commit: commit, OS: "linux", Arch: "amd64"},
			Persistent: &rpc.Persistent{Instance: "dheartbeat"},
		}
		var mu sync.Mutex
		var links []*frozenLink
		dial := func(_ context.Context, _ string, o remote.Options) (remoteConn, error) {
			mu.Lock()
			defer mu.Unlock()
			if len(links) > 0 {
				return nil, &remote.Error{Code: remote.CodeSSHTimeout, Detail: "the host does not answer"}
			}
			links = append(links, newFrozenLink(ctx, srv, o.ClientID))
			return links[0], nil
		}
		a, sink := remoteApp(t, dial)
		a.events = sink
		a.remote.start()
		synctest.Wait()
		if st := a.RemoteInfo(); st.State != remoteConnected {
			t.Fatalf("state %+v, want connected", st)
		}

		links[0].freeze()
		frozen := time.Now()
		// The last answered ping may be one interval old when the link
		// freezes; a margin covers the polling.
		limit := time.Duration(rpc.MaxMissedPings+1)*rpc.PingInterval + time.Second
		for !connectingAt(a, 0, 3) && time.Since(frozen) < limit+5*time.Second {
			time.Sleep(100 * time.Millisecond)
		}
		if took := time.Since(frozen); !connectingAt(a, 0, 3) || took > limit {
			t.Errorf("%v after the link froze the window says %+v, want connecting, attempt 1 of 3, within %v", took, a.RemoteInfo(), limit)
		}

		// Every attempt fails: the window gives up and shows why.
		for a.RemoteInfo().State != remoteFailed && time.Since(frozen) < 2*time.Minute {
			time.Sleep(time.Second)
		}
		if st := a.RemoteInfo(); st.State != remoteFailed {
			t.Errorf("state %+v, want failed after the attempts", st)
		}
		a.remote.disconnect()
		_ = links[0].Close()
		cancel()
	})
}
