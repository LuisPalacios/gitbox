//go:build !gitboxserver

package main

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/remote"
)

// pipeConn is a gitbox-server reached over in-memory pipes: what the real
// dialer returns, without ssh.
type pipeConn struct {
	rwc  io.ReadWriteCloser
	done chan struct{}
	stop func()
}

type pipeRWC struct {
	io.Reader
	w io.WriteCloser
}

func (p pipeRWC) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipeRWC) Close() error                { return p.w.Close() }

func (c *pipeConn) Conn() io.ReadWriteCloser { return c.rwc }
func (c *pipeConn) Done() <-chan struct{}    { return c.done }
func (c *pipeConn) Err() error               { return nil }
func (c *pipeConn) Close() error             { c.stop(); <-c.done; return nil }

// pipeServer starts a server on the config at path and returns a dialer
// that connects to it, and how many times it was called.
func pipeServer(t *testing.T, path string) (remoteDialer, func() int, func() *pipeConn) {
	t.Helper()
	var mu sync.Mutex
	dials := 0
	var last *pipeConn
	dial := func(ctx context.Context, alias string, o remote.Options) (remoteConn, error) {
		host := newServerApp(path)
		srv := newRPCServer(host, io.Discard)
		sctx, cancel := context.WithCancel(context.Background())
		host.be().Init(sctx)
		c2sR, c2sW := io.Pipe()
		s2cR, s2cW := io.Pipe()
		c := &pipeConn{rwc: pipeRWC{s2cR, c2sW}, done: make(chan struct{})}
		c.stop = func() { _ = c2sW.Close(); _ = s2cW.Close() }
		go func() {
			_ = srv.Serve(sctx, c2sR, s2cW)
			host.be().Close()
			cancel()
			_ = s2cW.Close()
			_ = c2sR.Close()
			close(c.done)
		}()
		mu.Lock()
		dials++
		last = c
		mu.Unlock()
		return c, nil
	}
	return dial, func() int { mu.Lock(); defer mu.Unlock(); return dials },
		func() *pipeConn { mu.Lock(); defer mu.Unlock(); return last }
}

// remoteApp is a remote window's App with no Wails runtime, its client
// preferences in a file of their own.
func remoteApp(t *testing.T, dial remoteDialer) (*App, *recordingSink) {
	t.Helper()
	local := seedConfig(t, "client.json", func(c *config.Config) {
		c.Global.Remotes = []config.RemoteEntry{{Alias: "my-server"}}
	})
	a := NewApp()
	a.prefs = newFilePrefs(local)
	a.remoteAlias, a.windowMode, a.experimentalRemote = "my-server", "remote", true
	a.remote = newRemoteSession(a, "my-server")
	a.remote.dial = dial
	a.backend = selectBackend(a, true)
	return a, &recordingSink{}
}

func states(sink *recordingSink) []string {
	var out []string
	for _, e := range sink.named("remote:state") {
		out = append(out, e.args[0].(RemoteStateDTO).State)
	}
	return out
}

func waitState(t *testing.T, a *App, want string) RemoteStateDTO {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := a.RemoteInfo()
		if st.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("state %q, want %q; log:\n%s", st.State, want, strings.Join(a.RemoteLog(), "\n"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRemoteLifecycle walks a remote window through its states with a
// dialer that needs no ssh.
func TestRemoteLifecycle(t *testing.T) {
	dial, dials, last := pipeServer(t, contractConfig(t))
	a, sink := remoteApp(t, dial)
	if _, ok := a.be().(*RemoteBackend); !ok {
		t.Fatalf("a remote window's backend is %T", a.be())
	}

	// Startup returns without dialing.
	a.Startup(context.Background())
	a.events = sink
	if dials() != 0 {
		t.Fatal("Startup dialed")
	}

	// Before the handshake every method answers not_connected.
	if _, err := a.GetConfig(); !codeIs(err, "not_connected") {
		t.Errorf("GetConfig before the connection = %v, want not_connected", err)
	}
	if a.IsFirstRun(); !codeIs(a.be().(*RemoteBackend).LastError(), "not_connected") {
		t.Errorf("IsFirstRun before the connection did not fail with not_connected")
	}

	a.remote.start()
	st := waitState(t, a, remoteConnected)
	if dials() != 1 {
		t.Errorf("dials = %d", dials())
	}
	if got := states(sink); !slices.Equal(got, []string{remoteConnecting, remoteConnected}) {
		t.Errorf("remote:state went %v, want connecting then connected", got)
	}
	if st.Alias != "my-server" || st.ServerVersion != formatAppVersion(version, commit) || st.ServerOS == "" {
		t.Errorf("connected state = %+v", st)
	}
	cfg, err := a.GetConfig()
	if err != nil || len(cfg.Accounts) != 1 {
		t.Errorf("GetConfig once connected = %+v, %v", cfg, err)
	}
	if err := a.AddRoot(t.TempDir()); !codeIs(err, "read_only") {
		t.Errorf("AddRoot = %v, want read_only", err)
	}
	if len(sink.named("remote:log")) == 0 || len(a.RemoteLog()) == 0 {
		t.Error("the connection log is empty")
	}

	// A second start while connected does nothing.
	a.RemoteReconnect()
	if dials() != 1 {
		t.Error("Reconnect while connected dialed again")
	}

	// Disconnect stops the server and the methods fail again.
	a.RemoteDisconnect()
	waitState(t, a, remoteDisconnected)
	select {
	case <-last().done:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not stop on disconnect")
	}
	if _, err := a.GetConfig(); !codeIs(err, "not_connected") {
		t.Errorf("GetConfig after the disconnect = %v, want not_connected", err)
	}

	// Reconnect, then the server dies under the window.
	a.RemoteReconnect()
	waitState(t, a, remoteConnected)
	last().stop()
	st = waitState(t, a, remoteFailed)
	if st.Code != "server_crashed" || st.Hint != "error.remote.server_crashed" {
		t.Errorf("after the server died: %+v", st)
	}
	if _, err := a.GetConfig(); !codeIs(err, "not_connected") {
		t.Errorf("GetConfig after the crash = %v, want not_connected", err)
	}
	a.Shutdown(context.Background())
}

func TestRemoteLifecycleDialFailure(t *testing.T) {
	a, sink := remoteApp(t, func(context.Context, string, remote.Options) (remoteConn, error) {
		return nil, &remote.Error{Code: remote.CodeSSHHostKey, Detail: "ssh does not trust the host key of my-server"}
	})
	a.events = sink
	a.remote.start()
	st := waitState(t, a, remoteFailed)
	if st.Code != "ssh_hostkey" || st.Hint != "error.remote.ssh_hostkey" || !strings.Contains(st.Detail, "host key") {
		t.Errorf("failed state = %+v", st)
	}
	if got := states(sink); !slices.Equal(got, []string{remoteConnecting, remoteFailed}) {
		t.Errorf("remote:state went %v", got)
	}
	if _, err := a.GetConfig(); !codeIs(err, "not_connected") {
		t.Errorf("GetConfig after the failure = %v, want not_connected", err)
	}
	// The installing state shows while the server is installed.
	a2, sink2 := remoteApp(t, func(_ context.Context, _ string, o remote.Options) (remoteConn, error) {
		o.OnInstall()
		return nil, &remote.Error{Code: remote.CodeNoexec, Detail: "noexec"}
	})
	a2.events = sink2
	a2.remote.start()
	waitState(t, a2, remoteFailed)
	if got := states(sink2); !slices.Equal(got, []string{remoteConnecting, remoteInstalling, remoteFailed}) {
		t.Errorf("remote:state went %v", got)
	}
}

// A server may not emit the client's own LOCAL events: remote:state could
// fake the connected view, update:quit could close the window.
func TestRelayDropsLocalEvents(t *testing.T) {
	link := serveRemote(t, contractConfig(t), nil)
	link.Host.sink().Emit("remote:state", RemoteStateDTO{State: remoteConnected, Alias: "forged"})
	link.Host.sink().Emit("update:quit")
	link.Host.sink().Emit("status:updated", []StatusResult{})
	deadline := time.Now().Add(5 * time.Second)
	for len(link.Events.named("status:updated")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(link.Events.named("status:updated")) != 1 {
		t.Fatal("the REMOTE event was not relayed")
	}
	for _, name := range []string{"remote:state", "update:quit"} {
		if got := link.Events.named(name); len(got) != 0 {
			t.Errorf("the LOCAL event %s from a server reached the window: %v", name, got)
		}
	}
}

func TestParseRemoteFlags(t *testing.T) {
	cases := []struct {
		args    []string
		want    remoteFlags
		wantErr string
	}{
		{nil, remoteFlags{}, ""},
		{[]string{"--experimental-remote"}, remoteFlags{experimental: true}, ""},
		{[]string{"--remote", "my-server", "--experimental-remote"}, remoteFlags{alias: "my-server", experimental: true}, ""},
		{[]string{"--remote=my-server", "--experimental-remote"}, remoteFlags{alias: "my-server", experimental: true}, ""},
		{[]string{"--remote", "my-server", "--experimental-remote", "--test-mode", "--prefs-file=/tmp/x.json"},
			remoteFlags{alias: "my-server", experimental: true, testMode: true, prefsFile: "/tmp/x.json"}, ""},
		{[]string{"--remote", "my-server"}, remoteFlags{}, "needs --experimental-remote"},
		{[]string{"--remote"}, remoteFlags{}, "needs the ssh_config alias"},
		{[]string{"--remote", "-oProxyCommand=x", "--experimental-remote"}, remoteFlags{}, "not a valid"},
		{[]string{"--remote", "my-server", "--experimental-remote", "--prefs-file=/tmp/x.json"}, remoteFlags{}, "only for a remote window in test mode"},
		{[]string{"--test-mode", "--prefs-file=/tmp/x.json"}, remoteFlags{}, "only for a remote window in test mode"},
	}
	for _, c := range cases {
		got, err := parseRemoteFlags(c.args)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%q: err = %v, want %q", c.args, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
}

func TestRemoteWindowArgs(t *testing.T) {
	if got := remoteWindowArgs("my-server", false, "/home/me/gitbox.json"); !slices.Equal(got, []string{"--remote", "my-server", "--experimental-remote"}) {
		t.Errorf("production args = %q", got)
	}
	got := remoteWindowArgs("my-server", true, "/tmp/test/gitbox.json")
	if !slices.Equal(got, []string{"--remote", "my-server", "--experimental-remote", "--test-mode", "--prefs-file=/tmp/test/gitbox.json"}) {
		t.Errorf("test-mode args = %q", got)
	}
	for _, args := range [][]string{got, remoteWindowArgs("my-server", false, "")} {
		if _, err := parseRemoteFlags(args); err != nil {
			t.Errorf("the spawned window would refuse its own args %q: %v", args, err)
		}
		if slices.ContainsFunc(args, func(s string) bool { return strings.HasPrefix(s, "--parent-pid") }) {
			t.Errorf("a remote window must not be tied to this process: %q", args)
		}
	}
}

func TestRemotesSettings(t *testing.T) {
	path := seedConfig(t, "gitbox.json", nil)
	a := &App{cfg: loadConfig(t, path), cfgPath: path, cfgLoaded: true}
	var checked []string
	a.checkAlias = func(_ context.Context, alias string) error {
		checked = append(checked, alias)
		if alias == "broken" {
			return &remote.Error{Code: remote.CodeSSHConfig, Detail: "ssh cannot read the configuration of broken"}
		}
		return nil
	}

	if got := a.ListRemotes(); got.Enabled || len(got.Remotes) != 0 {
		t.Errorf("ListRemotes with remote mode off = %+v", got)
	}
	if res := a.AddRemote("my-server", ""); res.Hint != "error.remote.not_enabled" {
		t.Errorf("AddRemote with remote mode off = %+v", res)
	}
	if res := a.ConnectRemote("my-server"); res.Hint != "error.remote.not_enabled" {
		t.Errorf("ConnectRemote with remote mode off = %+v", res)
	}

	// The key turns it on like the flag does. It is a client preference, so
	// it goes through the store: a key set only in memory would give way to
	// the file at the next save (#240).
	if err := a.clientPrefs().Update(func(p *config.ClientPrefs) { p.ExperimentalRemote = true }); err != nil {
		t.Fatal(err)
	}
	if !a.ListRemotes().Enabled {
		t.Error("experimental_remote did not enable the Remotes section")
	}
	if res := a.AddRemote(" my-server ", "~/srv/gitbox"); res.Error != "" {
		t.Fatalf("AddRemote = %+v", res)
	}
	for _, bad := range []struct{ alias, dir, hint string }{
		{"my-server", "", ""},        // duplicate
		{"-oProxyCommand=x", "", ""}, // not an alias
		{"other", "relative", ""},    // server_dir not absolute
		{"other", "~/", "error.remote.server_dir_unsafe"},
		{"broken", "", "error.remote.ssh_config"},
	} {
		res := a.AddRemote(bad.alias, bad.dir)
		if res.Error == "" || (bad.hint != "" && res.Hint != bad.hint) {
			t.Errorf("AddRemote(%q, %q) = %+v", bad.alias, bad.dir, res)
		}
	}
	if !slices.Equal(checked, []string{"my-server", "broken"}) {
		t.Errorf("ssh -G ran for %q, want only the aliases that passed validation", checked)
	}
	got := loadConfig(t, path).Global.Remotes
	if len(got) != 1 || got[0].Alias != "my-server" || got[0].ServerDir != "~/srv/gitbox" {
		t.Fatalf("remotes on disk = %+v", got)
	}
	if list := a.ListRemotes(); len(list.Remotes) != 1 || list.Remotes[0] != (RemoteEntryDTO{"my-server", "~/srv/gitbox"}) {
		t.Errorf("ListRemotes = %+v", list)
	}
	if res := a.ConnectRemote("nope"); res.Hint != "error.remote.unknown_alias" {
		t.Errorf("ConnectRemote of an unknown alias = %+v", res)
	}
	if res := a.RemoveRemote("my-server"); res.Error != "" {
		t.Fatalf("RemoveRemote = %+v", res)
	}
	if got := loadConfig(t, path).Global.Remotes; len(got) != 0 {
		t.Errorf("remotes after the removal = %+v", got)
	}
	if res := a.RemoveRemote("my-server"); res.Hint != "error.remote.unknown_alias" {
		t.Errorf("RemoveRemote twice = %+v", res)
	}

	// The window-only methods are harmless in the main window.
	if st := a.RemoteInfo(); st.State != remoteDisconnected {
		t.Errorf("RemoteInfo in the main window = %+v", st)
	}
	if res := a.RemoteReinstall(); res.Error == "" {
		t.Errorf("RemoteReinstall in the main window = %+v", res)
	}
	a.RemoteDisconnect()
	a.RemoteReconnect()
}

func TestRemoteFailureCodes(t *testing.T) {
	for _, c := range []struct {
		err  error
		code string
	}{
		{&remote.Error{Code: remote.CodeHashMismatch, Detail: "x"}, "hash_mismatch"},
		{&remoteError{remoteUnstampedBuild, "x"}, "unstamped_build"},
		{errors.New("pipe closed"), "server_crashed"},
	} {
		code, hint, _ := remoteFailure(c.err)
		if code != c.code || hint != "error.remote."+c.code {
			t.Errorf("%v = %s, %s", c.err, code, hint)
		}
	}
}

// A read-only remote window keeps every mutation on this side of the wire,
// including the one the frontend starts by itself (the focus-driven
// Reconcile), while its client preferences still save to this machine's own
// gitbox.json (#240). The harness refresh is a read since P6 (#243): it
// reaches the host, whose read-only server keeps what it finds in memory.
func TestRemoteWindowReadOnly(t *testing.T) {
	in, out, calls := answerEverything(t)
	a, _ := remoteApp(t, nil)
	if err := a.remote.backend.connect(in, out); err != nil {
		t.Fatal(err)
	}
	defer a.remote.backend.Close()
	local := a.prefs.(*filePrefs).path
	before := len(calls())

	// Mutations, typed and automatic.
	if err := a.AddRoot("/srv/x"); !codeIs(err, "read_only") {
		t.Errorf("AddRoot = %v, want read_only", err)
	}
	if err := a.DeleteRepo("acct", "org/repo"); !codeIs(err, "read_only") {
		t.Errorf("DeleteRepo = %v, want read_only", err)
	}
	a.Reconcile()
	a.PullRepo("acct", "org/repo")
	if got := calls()[before:]; len(got) != 0 {
		t.Errorf("mutations reached the wire: %+v", got)
	}

	// Client preferences save here and nothing is sent.
	if err := a.SetTheme("light"); err != nil {
		t.Errorf("SetTheme: %v", err)
	}
	if err := a.SetLanguage("es"); err != nil {
		t.Errorf("SetLanguage: %v", err)
	}
	if err := a.SetUIMode("simple"); err != nil {
		t.Errorf("SetUIMode: %v", err)
	}
	if err := a.SetCollapsed(config.CollapsedState{Sidebar: true, Sources: []string{"acct"}}); err != nil {
		t.Errorf("SetCollapsed: %v", err)
	}
	if got := calls()[before:]; len(got) != 0 {
		t.Errorf("a client preference reached the wire: %+v", got)
	}
	saved := loadConfig(t, local).Global
	if saved.Theme != "light" || saved.Language != "es" || saved.UIMode != "simple" || saved.Collapsed == nil || !saved.Collapsed.Sidebar {
		t.Errorf("the client preferences were not saved locally: theme %q, language %q, ui %q, collapsed %+v", saved.Theme, saved.Language, saved.UIMode, saved.Collapsed)
	}

	// A read still goes to the host, the harness refresh too, with no
	// request id: it is not a mutation.
	a.GetAllStatus()
	a.RefreshAIHarnesses()
	if got := calls()[before:]; len(got) != 2 || got[0].Method != "GetAllStatus" || got[1].Method != "RefreshAIHarnesses" || got[1].RequestID != "" {
		t.Errorf("the reads did not reach the host as reads: %+v", got)
	}
}
