//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// The daemon and the proxy of #247, in this process: a daemon on a short
// socket path, proxies whose stdin and stdout are pipes, and the rpc client
// a window uses. Nothing here changes the working folder.

// daemonLog is a log the daemon and the test share.
type daemonLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *daemonLog) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *daemonLog) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// daemonHost is a build folder with its state folder, and a socket path
// short enough for macOS.
type daemonHost struct {
	buildDir, stateDir, socket, cfgPath string
	log                                 *daemonLog
}

func newDaemonHost(t *testing.T) *daemonHost {
	t.Helper()
	build := filepath.Join(t.TempDir(), "v1-"+strings.Repeat("a", 40)+"-12345678")
	h := &daemonHost{buildDir: build, stateDir: remote.StateDir(build), cfgPath: filepath.Join(t.TempDir(), "gitbox.json"), log: &daemonLog{}}
	if err := os.MkdirAll(build, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := remote.PrepareStateDir(h.stateDir); err != nil {
		t.Fatal(err)
	}
	short, err := os.MkdirTemp("/tmp", "gbx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	h.socket = filepath.Join(short, remote.SocketName)
	return h
}

func (h *daemonHost) config(idle time.Duration) daemonConfig {
	return daemonConfig{socket: h.socket, stateDir: h.stateDir, idle: idle, cfgPath: h.cfgPath, log: h.log}
}

// daemonRun is a daemon started by a test: ended is closed when runDaemon
// returned, and err is what it returned.
type daemonRun struct {
	ended chan struct{}
	err   error
}

// startDaemon runs a daemon until the test ends.
func (h *daemonHost) startDaemon(t *testing.T, d daemonConfig) *daemonRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	d.ready = ready
	run := &daemonRun{ended: make(chan struct{})}
	go func() { run.err = runDaemon(ctx, d); close(run.ended) }()
	select {
	case <-ready:
	case <-run.ended:
		cancel()
		t.Fatalf("the daemon ended at once: %v\n%s", run.err, h.log.String())
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("the daemon did not start")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-run.ended:
		case <-time.After(15 * time.Second):
			t.Error("the daemon did not stop")
		}
	})
	return run
}

// env is a proxy of this host. spawn fails unless the test sets it.
func (h *daemonHost) env(clientID string) proxyEnv {
	return proxyEnv{
		clientID: clientID, buildDir: h.buildDir, socket: h.socket, startWait: 5 * time.Second,
		spawn: func(string) (<-chan error, error) {
			return nil, &remote.DaemonError{Reason: remote.ReasonStartFailed, Detail: "no daemon in this test"}
		},
		newApp: func() *App { return newServerApp(h.cfgPath) },
	}
}

// proxied is a window's client through one proxy.
type proxied struct {
	client  *rpc.Client
	proxy   chan int
	stdoutW *io.PipeWriter
	log     *daemonLog
}

// close ends the session as ssh does when the window closes it, and waits
// for the proxy to exit.
func (p *proxied) close(t *testing.T) int {
	t.Helper()
	_ = p.client.Close()
	select {
	case code := <-p.proxy:
		return code
	case <-time.After(15 * time.Second):
		t.Fatal("the proxy did not exit")
		return -1
	}
}

func runProxied(t *testing.T, env proxyEnv, opts rpc.ClientOptions) *proxied {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	p := &proxied{proxy: make(chan int, 1), stdoutW: stdoutW, log: &daemonLog{}}
	go func() {
		code := runProxy(env, stdinR, stdoutW, p.log)
		_ = stdoutW.Close()
		_ = stdinR.Close()
		p.proxy <- code
	}()
	opts.Version, opts.Commit = version, commit
	client, err := rpc.Dial(stdoutR, stdinW, opts)
	if err != nil {
		t.Fatalf("dial through the proxy: %v\n%s", err, p.log.String())
	}
	p.client = client
	return p
}

func call(t *testing.T, c *rpc.Client, method string, result any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Call(ctx, method, nil, result); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

func TestDaemonServesItsProxies(t *testing.T) {
	h := newDaemonHost(t)
	h.startDaemon(t, h.config(time.Hour))
	p := runProxied(t, h.env("client-a"), rpc.ClientOptions{})
	hello := p.client.Server()
	if hello.Daemon == nil || hello.Daemon.Instance == "" || !hello.Daemon.Resumed || hello.Fallback != "" {
		t.Fatalf("hello = %+v, want a daemon that was already running", hello)
	}
	var path string
	call(t, p.client, "GetConfigPath", &path)
	if path != h.cfgPath {
		t.Errorf("GetConfigPath = %q, want %q", path, h.cfgPath)
	}
	if !strings.Contains(p.log.String(), "proxy: joined the daemon") || !strings.Contains(p.log.String(), "serving client client-a") {
		t.Errorf("the proxy log says:\n%s", p.log.String())
	}
	if code := p.close(t); code != serverExitOK {
		t.Errorf("the proxy exited with %d", code)
	}
	if info, err := os.Stat(h.socket); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the socket: %v, %v; want it still there, mode 0600", info, err)
	}

	// A second session reaches the same daemon and resumes from the first.
	second := runProxied(t, h.env("client-a"), rpc.ClientOptions{Resume: &rpc.ResumePoint{Instance: hello.Daemon.Instance}})
	defer second.close(t)
	if got := second.client.Server().Daemon; got == nil || got.Instance != hello.Daemon.Instance {
		t.Errorf("the second session reached %+v, want instance %s", got, hello.Daemon.Instance)
	}
	if !second.client.Resumed().Resumed {
		t.Error("the second session did not resume")
	}
}

func TestProxyStartsTheDaemon(t *testing.T) {
	h := newDaemonHost(t)
	env := h.env("client-a")
	env.spawn = func(stateDir string) (<-chan error, error) {
		if stateDir != h.stateDir {
			t.Errorf("spawn in %s, want %s", stateDir, h.stateDir)
		}
		h.startDaemon(t, h.config(time.Hour))
		return make(chan error), nil
	}
	p := runProxied(t, env, rpc.ClientOptions{})
	defer p.close(t)
	if d := p.client.Server().Daemon; d == nil || d.Resumed {
		t.Errorf("hello = %+v, want a daemon this proxy started", d)
	}
	if !strings.Contains(p.log.String(), "proxy: started the daemon") {
		t.Errorf("the proxy log says:\n%s", p.log.String())
	}
}

// TestProxyFallsBackWithoutTheDaemon: a state folder that is not the
// user's alone makes the proxy serve the session itself, visibly: the
// session works, the handshake names the reason, the server log says so,
// and the doctor report shows it.
func TestProxyFallsBackWithoutTheDaemon(t *testing.T) {
	h := newDaemonHost(t)
	if err := os.Chmod(h.stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := h.env("client-a")
	env.socket, env.enterErr = "", remote.CheckStateDir(h.stateDir)
	if remote.ReasonOf(env.enterErr) != remote.ReasonStateUnsafe {
		t.Fatalf("CheckStateDir = %v, want state_unsafe", env.enterErr)
	}
	p := runProxied(t, env, rpc.ClientOptions{})
	hello := p.client.Server()
	if hello.Daemon != nil || hello.Fallback != remote.ReasonStateUnsafe {
		t.Errorf("hello = %+v, want no daemon and the fallback state_unsafe", hello)
	}
	var path string
	call(t, p.client, "GetConfigPath", &path)
	if path != h.cfgPath {
		t.Errorf("the fallback session answers GetConfigPath = %q", path)
	}
	if !strings.Contains(p.log.String(), "the daemon is unavailable (state_unsafe)") {
		t.Errorf("the server log does not name the fallback:\n%s", p.log.String())
	}
	if code := p.close(t); code != serverExitOK {
		t.Errorf("the fallback session exited with %d", code)
	}
	if got := daemonDoctor(h.buildDir); got.State != doctorDaemonUnsafe || got.Running || got.LastFallback != remote.ReasonStateUnsafe {
		t.Errorf("doctor = %+v, want unsafe, not running, last fallback state_unsafe", got)
	}
}

func TestDaemonDoctor(t *testing.T) {
	h := newDaemonHost(t)
	if got := daemonDoctor(""); got.State != doctorDaemonNotInstalled {
		t.Errorf("doctor of a binary not installed = %+v", got)
	}
	if err := os.Remove(h.stateDir); err != nil {
		t.Fatal(err)
	}
	if got := daemonDoctor(h.buildDir); got.State != doctorDaemonAbsent || got.Running {
		t.Errorf("doctor before the first daemon = %+v", got)
	}
	if err := remote.PrepareStateDir(h.stateDir); err != nil {
		t.Fatal(err)
	}
	h.startDaemon(t, h.config(time.Hour))
	if got := daemonDoctor(h.buildDir); got.State != doctorDaemonOK || !got.Running || got.LastFallback != "" {
		t.Errorf("doctor with a daemon = %+v", got)
	}
}

func TestDaemonRefusesAnotherUser(t *testing.T) {
	h := newDaemonHost(t)
	d := h.config(time.Hour)
	d.peerUID = func(net.Conn) (int, error) { return 4242, nil }
	h.startDaemon(t, d)
	c, err := remote.DialSocket(h.socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = remote.WriteProxyHello(c, "intruder", false)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if n, err := c.Read(make([]byte, 16)); n != 0 || err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("a peer of another uid read %d bytes, %v; want the connection closed", n, err)
	}
	if !strings.Contains(h.log.String(), "daemon: refused a connection from uid 4242") || strings.Contains(h.log.String(), "intruder") {
		t.Errorf("the daemon log says:\n%s", h.log.String())
	}
}

func TestDaemonEndsWhenIdle(t *testing.T) {
	h := newDaemonHost(t)
	run := h.startDaemon(t, h.config(200*time.Millisecond))
	select {
	case <-run.ended:
		if run.err != nil {
			t.Fatal(run.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("an idle daemon did not end")
	}
	if _, err := os.Lstat(h.socket); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the socket outlived the daemon: %v", err)
	}
	if _, alive := remote.DaemonAlive(h.stateDir); alive {
		t.Error("the lock outlived the daemon")
	}
	if !strings.Contains(h.log.String(), "nothing running; stopping") {
		t.Errorf("the daemon log says:\n%s", h.log.String())
	}
}

func TestSecondDaemonLeavesTheFirst(t *testing.T) {
	h := newDaemonHost(t)
	h.startDaemon(t, h.config(time.Hour))
	if err := runDaemon(t.Context(), h.config(time.Hour)); err != nil {
		t.Errorf("a second daemon = %v, want it to end quietly", err)
	}
	if _, alive := remote.DaemonAlive(h.stateDir); !alive {
		t.Error("the second daemon took the first one's state")
	}
}

// TestDaemonDetachLeavesNoGoroutines is the leak check of #247: after
// sessions attach and detach, the daemon is back to its baseline.
func TestDaemonDetachLeavesNoGoroutines(t *testing.T) {
	h := newDaemonHost(t)
	h.startDaemon(t, h.config(time.Hour))
	var point rpc.ResumePoint
	cycle := func() {
		p := runProxied(t, h.env("client-a"), rpc.ClientOptions{Resume: &point})
		var path string
		call(t, p.client, "GetConfigPath", &path)
		point = p.client.ResumePoint()
		p.close(t)
	}
	cycle()
	time.Sleep(200 * time.Millisecond)
	base := runtime.NumGoroutine()
	for range 20 {
		cycle()
	}
	deadline := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base {
		t.Errorf("%d goroutines after 20 sessions, %d before", n, base)
	}
}

func TestParseRunArgs(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want time.Duration
		ok   bool
	}{
		{nil, daemonIdle, true},
		{[]string{"--idle", "90m"}, 90 * time.Minute, true},
		{[]string{"--idle", "10ms"}, 0, false},
		{[]string{"--idle"}, 0, false},
		{[]string{"--other", "1h"}, 0, false},
	} {
		got, ok := parseRunArgs(tt.args)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("parseRunArgs(%q) = %v, %v", tt.args, got, ok)
		}
	}
}
