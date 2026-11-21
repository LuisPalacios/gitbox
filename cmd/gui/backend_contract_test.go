package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// wireTap copies everything written through it, so a test can look at what
// really crossed the wire.
type wireTap struct {
	w  io.Writer
	mu sync.Mutex
	b  bytes.Buffer
}

func (t *wireTap) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.b.Write(p)
	t.mu.Unlock()
	return t.w.Write(p)
}

func (t *wireTap) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.b.String()
}

// remoteLink is a RemoteBackend talking to a real server over in-memory
// pipes, the way GitboxApp talks to a gitbox-server over its stdin and
// stdout.
type remoteLink struct {
	Remote *RemoteBackend
	// Host is the server's App: the same code gitbox-server runs.
	Host   *App
	Events *recordingSink
	// Sent is everything the server wrote to the client.
	Sent   *wireTap
	served chan error
}

// serveRemote starts a server on the config at path and connects a
// RemoteBackend to it. extra adds handlers that only a test needs.
func serveRemote(t *testing.T, path string, extra map[string]rpc.Handler) *remoteLink {
	t.Helper()
	host := newServerApp(path)
	srv := newRPCServer(host, io.Discard)
	maps.Copy(srv.Handlers, extra)
	ctx, cancel := context.WithCancel(context.Background())
	host.be().Init(ctx)

	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	l := &remoteLink{Host: host, Events: &recordingSink{}, Sent: &wireTap{w: s2cW}, served: make(chan error, 1)}
	go func() {
		err := srv.Serve(ctx, c2sR, l.Sent)
		host.be().Close()
		_ = s2cW.Close()
		_ = c2sR.Close()
		l.served <- err
	}()
	remote, err := dialRemoteBackend(s2cR, c2sW, func() EventSink { return l.Events }, io.Discard)
	if err != nil {
		cancel()
		t.Fatalf("dial: %v", err)
	}
	l.Remote = remote
	t.Cleanup(func() {
		remote.Close()
		cancel()
	})
	return l
}

// contractConfig is a host config with a real clone on disk, client
// preferences and an account, so every call of the table has something to
// say.
func contractConfig(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	clone := filepath.Join(t.TempDir(), "clone")
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main", clone},
		{"-C", clone, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "--quiet", "--allow-empty", "-m", "first"},
	} {
		if out, err := exec.Command(git.GitBin(), args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return seedConfig(t, "gitbox.json", func(c *config.Config) {
		c.Global.Theme = "dark"
		c.Global.Editors = []config.EditorEntry{{Name: "VS Code", Command: "/usr/local/bin/code"}}
		c.Global.Remotes = []config.RemoteEntry{{Alias: "build-box"}}
		src := c.Sources["acct"]
		src.Repos = map[string]config.Repo{"org/repo": {CloneFolder: clone}}
		src.RepoOrder = []string{"org/repo"}
		c.Sources["acct"] = src
	})
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// canonJSON is v as JSON with every object's keys sorted, so a struct and the
// map it becomes after crossing the wire compare equal.
func canonJSON(t *testing.T, v any) string {
	t.Helper()
	var generic any
	if err := json.Unmarshal([]byte(asJSON(t, v)), &generic); err != nil {
		t.Fatal(err)
	}
	return asJSON(t, generic)
}

// TestBackendContract runs one table of calls against two backends: the
// local one, as a server exposes it, and a RemoteBackend connected to a
// server on the same config. Every call must give the same result and the
// same error, and emit the same events. P4 and P5 extend the table as they
// extend the server's list.
func TestBackendContract(t *testing.T) {
	path := contractConfig(t)

	local := &App{cfgPath: path, events: &recordingSink{}}
	local.be().Init(context.Background())
	t.Cleanup(local.closeLocal)
	direct := Backend(hostBackend{localBackend{local}})

	link := serveRemote(t, path, nil)

	table := []struct {
		name string
		call func(Backend) (any, error)
	}{
		{"GetConfig", func(b Backend) (any, error) { return b.GetConfig() }},
		{"GetAllStatus", func(b Backend) (any, error) { return b.GetAllStatus(), nil }},
		{"ListRoots", func(b Backend) (any, error) { return b.ListRoots(), nil }},
		{"GetConfigPath", func(b Backend) (any, error) { return b.GetConfigPath(), nil }},
		{"IsFirstRun", func(b Backend) (any, error) { return b.IsFirstRun(), nil }},
		{"SuggestAccountKey", func(b Backend) (any, error) { return b.SuggestAccountKey("github", "alice"), nil }},
		{"SuggestAccountKey for a taken key", func(b Backend) (any, error) { return b.SuggestAccountKey("github", "acct"), nil }},
	}
	covered := make(map[string]bool)
	for _, tt := range table {
		covered[strings.Fields(tt.name)[0]] = true
		t.Run(tt.name, func(t *testing.T) {
			want, wantErr := tt.call(direct)
			got, gotErr := tt.call(link.Remote)
			if (wantErr == nil) != (gotErr == nil) || (wantErr != nil && userMsg(wantErr) != userMsg(gotErr)) {
				t.Fatalf("errors differ: local %v, remote %v", wantErr, gotErr)
			}
			if w, g := asJSON(t, want), asJSON(t, got); w != g {
				t.Errorf("results differ:\n local  %s\n remote %s", w, g)
			}
			if w := asJSON(t, want); w == "null" || w == `""` || w == "[]" {
				t.Errorf("the call returned nothing (%s): the comparison proves nothing", w)
			}
			if err := link.Remote.LastError(); err != nil {
				t.Errorf("the remote call recorded a failure: %v", err)
			}
		})
	}
	for _, name := range servedMethods {
		if !covered[name] {
			t.Errorf("%s is served but the contract table does not call it", name)
		}
	}

	// The same events, in the same order.
	localEvents, remoteEvents := local.events.(*recordingSink), link.Events
	if w, g := eventsJSON(t, localEvents), eventsJSON(t, remoteEvents); w != g {
		t.Errorf("events differ:\n local  %s\n remote %s", w, g)
	}

	// What crossed the wire held no client preference of the host.
	for _, leaked := range []string{"/usr/local/bin/code", "build-box", `"theme"`} {
		if strings.Contains(link.Sent.String(), leaked) {
			t.Errorf("the server sent the host's client preference %q", leaked)
		}
	}
}

func eventsJSON(t *testing.T, sink *recordingSink) string {
	t.Helper()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	type ev struct {
		Name string `json:"name"`
		Args []any  `json:"args"`
	}
	out := make([]ev, 0, len(sink.events))
	for _, e := range sink.events {
		args := e.args
		if args == nil {
			args = []any{}
		}
		out = append(out, ev{e.name, args})
	}
	return canonJSON(t, out)
}

// The server's events reach the window with the same name and payload, and
// a name this build does not know is dropped.
func TestRemoteEventsAreRelayed(t *testing.T) {
	link := serveRemote(t, contractConfig(t), nil)
	statuses := []StatusResult{{Source: "acct", Repo: "org/repo", State: "clean", Branch: "main"}}
	link.Host.sink().Emit("status:updated", statuses)
	link.Host.sink().Emit("made:up", "by a hostile server")
	link.Host.sink().Emit("fetch:done", map[string]any{"source": "acct", "repo": "org/repo", "error": "git fetch: offline"})

	deadline := time.Now().Add(5 * time.Second)
	for len(link.Events.named("fetch:done")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := link.Events.named("made:up"); len(got) != 0 {
		t.Errorf("an unknown event reached the window: %v", got)
	}
	got := link.Events.named("status:updated")
	if len(got) != 1 || canonJSON(t, got[0].args) != canonJSON(t, []any{statuses}) {
		t.Errorf("status:updated arrived %d times with a payload that differs from %s", len(got), canonJSON(t, []any{statuses}))
	}
	if len(link.Events.named("fetch:done")) != 1 {
		t.Errorf("fetch:done was not relayed")
	}
}

// An error crosses the wire only after the boundary scrubbed it, and the
// client scrubs it again: nothing of the token fixture is ever sent or shown.
func TestRemoteErrorsCarryNoSecrets(t *testing.T) {
	const token = "ghp_CANARYCANARYCANARYCANARY0123456789"
	link := serveRemote(t, contractConfig(t), map[string]rpc.Handler{
		"TestFail": rpc.Func(func() error {
			return fmt.Errorf("fetching https://alice:%s@git.example.org/x.git: Authorization: Bearer %s", token, token)
		}),
		"TestGitFail": rpc.Func(func() (string, error) {
			return "", fmt.Errorf("refresh: %w", &git.GitError{Op: "fetch", Code: git.CodeAuthFailed, Hint: "error.git.auth_failed", Detail: "exit 128"})
		}),
	})

	err := link.Remote.invoke("TestFail", nil)
	if err == nil {
		t.Fatal("the call must fail")
	}
	for _, text := range []string{err.Error(), userMsg(err), fmt.Sprintf("%+v", toUserError(err)), link.Sent.String()} {
		if strings.Contains(text, token) || strings.Contains(text, "alice:") {
			t.Errorf("the token or the URL credentials survived: %s", text)
		}
	}

	// A typed error keeps its code and hint across the wire.
	err = link.Remote.invoke("TestGitFail", new(string))
	ue := toUserError(err)
	if ue.Code != "git.auth_failed" || ue.Hint != "error.git.auth_failed" || ue.Detail != "refresh: git fetch: auth_failed (exit 128)" {
		t.Errorf("the typed error arrived as %+v", ue)
	}
}

// A hostile server cannot put anything it likes in the code and the hint.
func TestToUserErrorDistrustsTheServer(t *testing.T) {
	err := &rpc.Error{Code: rpc.CodeApplication}
	err.Data.Code = "git.offline\"><script>"
	err.Data.Hint = strings.Repeat("h", 200)
	err.Data.Detail = "see https://bob:s3cr3tValue0123456789abcdef@evil.example/\x1b[31m"
	ue := toUserError(err)
	if ue.Code != "" || ue.Hint != "" {
		t.Errorf("a malformed code or hint was kept: %+v", ue)
	}
	if strings.Contains(ue.Detail, "s3cr3t") || strings.Contains(ue.Detail, "\x1b") {
		t.Errorf("the detail was not scrubbed: %q", ue.Detail)
	}
}

// A method the server does not serve yet fails as unsupported, and one with
// no error to return answers its zero value and keeps the failure.
func TestRemoteMethodOutsideTheClosedList(t *testing.T) {
	link := serveRemote(t, contractConfig(t), nil)
	if err := link.Remote.AddRoot(t.TempDir()); !rpc.IsCode(err, rpc.ErrCodeUnsupported) {
		t.Errorf("AddRoot = %v, want unsupported", err)
	}
	if roots := link.Remote.ListRoots(); len(roots) != 1 {
		t.Errorf("the refused mutation changed the host: %d roots", len(roots))
	}
	if depth := link.Remote.GetNestedScanDepth(); depth != 0 {
		t.Errorf("GetNestedScanDepth = %d, want the zero value for a method that is not served", depth)
	}
	if err := link.Remote.LastError(); !rpc.IsCode(err, rpc.ErrCodeUnsupported) {
		t.Errorf("LastError = %v, want the failure of the call that had no error to return", err)
	}
}

// recordedCall is one request as a fake server saw it.
type recordedCall struct {
	Method    string
	Params    string
	RequestID string
}

// answerEverything is a server that accepts any method and answers null. It
// returns the requests it saw.
func answerEverything(t *testing.T) (in io.Reader, out io.WriteCloser, calls func() []recordedCall) {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	var mu sync.Mutex
	var seen []recordedCall
	go func() {
		defer s2cW.Close()
		reader := bufio.NewReader(c2sR)
		// The handshake, by hand: magic, hello, the client's answer, verdict.
		_, _ = s2cW.Write(rpc.Magic)
		hello, _ := json.Marshal(rpc.Hello{Version: version, Commit: commit, OS: "testos", Arch: "testarch", PID: 1})
		_ = rpc.WriteFrame(s2cW, hello)
		if _, err := rpc.ReadFrame(reader); err != nil {
			return
		}
		_ = rpc.WriteFrame(s2cW, []byte(`{"ok":true}`))
		for {
			body, err := rpc.ReadFrame(reader)
			if err != nil {
				return
			}
			var req struct {
				ID     *int64          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
				Meta   struct {
					RequestID string `json:"requestId"`
				} `json:"meta"`
			}
			if json.Unmarshal(body, &req) != nil || req.ID == nil {
				continue
			}
			if !strings.HasPrefix(req.Method, "$/") {
				mu.Lock()
				seen = append(seen, recordedCall{req.Method, string(req.Params), req.Meta.RequestID})
				mu.Unlock()
			}
			_ = rpc.WriteFrame(s2cW, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":null}`, *req.ID)))
		}
	}()
	return s2cR, c2sW, func() []recordedCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedCall(nil), seen...)
	}
}

// Each of RemoteBackend's methods must call the server method of its own
// name, pass every argument in order, and carry a request id exactly when
// the method changes state on the host. The lines are written by hand, one
// per method: this is what catches a wrong name or a dropped argument.
func TestRemoteBackendCallsEachMethodByName(t *testing.T) {
	in, out, calls := answerEverything(t)
	remote, err := dialRemoteBackend(in, out, func() EventSink { return nopSink{} }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()

	iface := reflect.TypeFor[Backend]()
	value := reflect.ValueOf(remote)
	checked := 0
	for m := range iface.Methods() {
		if m.Name == "Init" || m.Name == "Start" || m.Name == "Close" {
			continue
		}
		// One distinct, recognisable value per argument.
		args := make([]reflect.Value, m.Type.NumIn())
		wantParams := make([]any, m.Type.NumIn())
		for a := range m.Type.NumIn() {
			args[a] = distinctValue(m.Type.In(a), a)
			wantParams[a] = args[a].Interface()
		}
		before := len(calls())
		value.MethodByName(m.Name).Call(args)
		got := calls()
		if len(got) != before+1 {
			t.Errorf("%s made %d calls, want 1", m.Name, len(got)-before)
			continue
		}
		call := got[len(got)-1]
		if call.Method != m.Name {
			t.Errorf("%s called the server method %q", m.Name, call.Method)
		}
		if want := asJSON(t, wantParams); call.Params != want {
			t.Errorf("%s sent params %s, want %s", m.Name, call.Params, want)
		}
		if mutating := methodClass[m.Name].Mutating; (call.RequestID != "") != mutating {
			t.Errorf("%s: request id %q, but the method is classified mutating=%v", m.Name, call.RequestID, mutating)
		}
		checked++
	}
	if checked < 100 {
		t.Errorf("only %d methods were checked", checked)
	}
	ids := make(map[string]bool)
	for _, c := range calls() {
		if c.RequestID != "" {
			if ids[c.RequestID] {
				t.Errorf("request id %s was used twice", c.RequestID)
			}
			ids[c.RequestID] = true
		}
	}
}

// distinctValue returns a value of type t that differs by argument position,
// so swapped or dropped arguments show.
func distinctValue(t reflect.Type, position int) reflect.Value {
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.String:
		v.SetString(fmt.Sprintf("arg-%d", position))
	case reflect.Bool:
		v.SetBool(position%2 == 0)
	case reflect.Int, reflect.Int64:
		v.SetInt(int64(100 + position))
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			v.Set(reflect.ValueOf([]string{fmt.Sprintf("item-%d", position)}))
		} else {
			v.Set(reflect.MakeSlice(t, 0, 0))
		}
	}
	return v
}

// Closing the backend ends the connection and the server, and leaves no
// goroutine behind on either side.
func TestRemoteBackendCloseLeavesNoGoroutines(t *testing.T) {
	path := contractConfig(t)
	runtime.GC()
	base := runtime.NumGoroutine()
	for range 3 {
		link := serveRemote(t, path, nil)
		if roots := link.Remote.ListRoots(); len(roots) != 1 {
			t.Fatalf("ListRoots = %v", roots)
		}
		link.Remote.Close()
		select {
		case err := <-link.served:
			if err != nil {
				t.Errorf("the server ended with %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the server did not stop after the client closed")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base {
		buf := make([]byte, 1<<16)
		t.Errorf("%d goroutines are left, %d before the test:\n%s", n, base, buf[:runtime.Stack(buf, true)])
	}
}

// The end of the connection is the server's only shutdown signal. It must
// reach a git process that a handler started and that would never end by
// itself.
func TestServerEndOfInputStopsGit(t *testing.T) {
	path := contractConfig(t)
	repo := loadConfig(t, path).Sources["acct"].Repos["org/repo"].CloneFolder
	gitRunning := make(chan struct{})
	gitEnded := make(chan error, 1)
	link := serveRemote(t, path, map[string]rpc.Handler{
		"TestHang": rpc.Func(func(ctx context.Context) error {
			// git cat-file --batch waits for requests on an input that
			// never delivers any.
			pr, pw := io.Pipe()
			defer pw.Close()
			close(gitRunning)
			_, err := git.Run(ctx, git.Request{Dir: repo, Args: []string{"cat-file", "--batch"}, Stdin: pr})
			gitEnded <- err
			return err
		}),
	})
	pending := make(chan error, 1)
	go func() { pending <- link.Remote.invoke("TestHang", nil) }()
	select {
	case <-gitRunning:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler never started")
	}
	time.Sleep(200 * time.Millisecond) // let git start

	started := time.Now()
	link.Remote.Close()
	select {
	case err := <-gitEnded:
		if err == nil {
			t.Error("the hung git ended without an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("git was still running 10 seconds after the connection ended")
	}
	select {
	case <-link.served:
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop")
	}
	if spent := time.Since(started); spent > rpc.ShutdownGrace+2*time.Second {
		t.Errorf("stopping took %v", spent)
	}
	if err := <-pending; err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the pending call ended with %v", err)
	}
}

// A window on a remote host shows its own version, and learns the server's
// from the handshake.
func TestStartupSnapshotCarriesTheServerVersion(t *testing.T) {
	// GetStartupSnapshot is not served until P4: let this server answer it.
	var link *remoteLink
	link = serveRemote(t, contractConfig(t), map[string]rpc.Handler{
		"GetStartupSnapshot": rpc.Func(func() (StartupSnapshot, error) {
			snap, err := hostBackend{localBackend{link.Host}}.GetStartupSnapshot()
			snap.AppVersion = "the-server-says-so"
			return snap, err
		}),
	})
	a := &App{backend: link.Remote, prefs: newFilePrefs(seedConfig(t, "client.json", nil))}
	if got := a.GetAppVersion(); got != a.getAppVersionLocal() {
		t.Errorf("GetAppVersion = %q, want this build's own %q", got, a.getAppVersionLocal())
	}
	remoteSnap, err := a.GetStartupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if remoteSnap.AppVersion != a.getAppVersionLocal() {
		t.Errorf("the snapshot's AppVersion = %q, want this build's own %q", remoteSnap.AppVersion, a.getAppVersionLocal())
	}
	if want := formatAppVersion(version, commit); remoteSnap.ServerVersion != want {
		t.Errorf("the snapshot's ServerVersion = %q, want %q", remoteSnap.ServerVersion, want)
	}
	if len(remoteSnap.Config.Accounts) != 1 {
		t.Errorf("the snapshot lost the host's config: %+v", remoteSnap.Config)
	}

	// A local window says nothing about a server.
	local := &App{cfgPath: contractConfig(t)}
	local.be().Init(context.Background())
	snap, err := local.GetStartupSnapshot()
	if err != nil || snap.ServerVersion != "" || snap.AppVersion == "" {
		t.Errorf("local snapshot: server %q, app %q, %v", snap.ServerVersion, snap.AppVersion, err)
	}
	if strings.Contains(asJSON(t, snap), "serverVersion") {
		t.Error("a local snapshot must not carry the serverVersion key")
	}
}
