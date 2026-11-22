package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// The closed list may only hold read-only backend methods until the phase
// that validates mutations lands (P5). Adding one here by mistake would
// expose it to every client.
func TestServedMethodsAreReadOnly(t *testing.T) {
	if !slices.IsSorted(servedMethods) || len(slices.Compact(slices.Clone(servedMethods))) != len(servedMethods) {
		t.Errorf("servedMethods must be sorted and without repeats: %v", servedMethods)
	}
	backend := reflect.TypeFor[Backend]()
	for _, name := range servedMethods {
		if _, ok := backend.MethodByName(name); !ok {
			t.Errorf("%s is served but is not a Backend method", name)
			continue
		}
		info, ok := classOf(name)
		switch {
		case !ok:
			t.Errorf("%s is served but has no classification", name)
		case info.Class != classRemote && info.Class != classMixed:
			t.Errorf("%s is served but is classified %s: only REMOTE and MIXED methods run on the server", name, info.Class)
		case info.Mutating:
			t.Errorf("%s is served but changes state on the host: mutations wait for their validation (P5)", name)
		}
	}
	// Every served method has a handler, and nothing else does.
	srv := newRPCServer(newServerApp(filepath.Join(t.TempDir(), "gitbox.json")), io.Discard)
	if len(srv.Handlers) != len(servedMethods) {
		t.Errorf("%d handlers for %d served methods", len(srv.Handlers), len(servedMethods))
	}
}

// globalKeys returns the raw JSON of each key under "global" in a config
// file.
func globalKeys(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Global map[string]json.RawMessage `json:"global"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file.Global
}

func hostConfig(t *testing.T, theme string) string {
	t.Helper()
	return seedConfig(t, "gitbox.json", func(c *config.Config) {
		c.Global.Theme = theme
		c.Global.Language = "es"
		c.Global.Editors = []config.EditorEntry{{Name: "VS Code", Command: "/usr/local/bin/code"}}
		c.Global.Remotes = []config.RemoteEntry{{Alias: "build-box", ServerDir: "~/srv"}}
		c.Global.Window = &config.WindowState{X: 10, Y: 20, Width: 900, Height: 700}
		c.Global.IgnoredClones = []string{"/host/ignored"}
	})
}

// carriesGlobalConfig reports whether a value of type t holds the "global"
// section of a config, at any depth.
func carriesGlobalConfig(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	if t == reflect.TypeFor[config.GlobalConfig]() {
		return true
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return carriesGlobalConfig(t.Elem(), seen)
	case reflect.Struct:
		for f := range t.Fields() {
			if carriesGlobalConfig(f.Type, seen) {
				return true
			}
		}
	}
	return false
}

// What the server sends carries the host's domain data and none of its
// client preferences. The test finds by itself every Backend method whose
// result holds a config, so a new one cannot leak them unnoticed.
func TestServerSendsNoClientPrefs(t *testing.T) {
	path := hostConfig(t, "dark")
	a := newServerApp(path)
	newRPCServer(a, io.Discard)
	a.be().Init(context.Background())
	host := reflect.ValueOf(Backend(hostBackend{localBackend{a}}))

	var checked []string
	for m := range reflect.TypeFor[Backend]().Methods() {
		carries := false
		for out := range m.Type.Outs() {
			carries = carries || carriesGlobalConfig(out, map[reflect.Type]bool{})
		}
		if !carries {
			continue
		}
		checked = append(checked, m.Name)
		if m.Type.NumIn() != 0 {
			t.Errorf("%s returns a config and takes parameters: teach this test to call it", m.Name)
			continue
		}
		results := host.MethodByName(m.Name).Call(nil)
		if err, _ := reflect.TypeAssert[error](results[len(results)-1]); err != nil {
			t.Errorf("%s: %v", m.Name, err)
			continue
		}
		sent, err := json.Marshal(results[0].Interface())
		if err != nil {
			t.Fatal(err)
		}
		for _, pref := range []string{`"theme":"dark"`, `"language":"es"`, "/usr/local/bin/code", "build-box", `"window":{`} {
			if strings.Contains(string(sent), pref) {
				t.Errorf("%s sent the host's client preference %s", m.Name, pref)
			}
		}
		if !strings.Contains(string(sent), "/host/ignored") || !strings.Contains(string(sent), `"accounts":{"acct"`) {
			t.Errorf("%s lost domain data: %s", m.Name, sent)
		}
	}
	want := []string{"GetConfig", "GetStartupSnapshot", "RedetectProfiles", "ReloadConfig"}
	if !slices.Equal(checked, want) {
		t.Errorf("methods that return a config: %v, want %v", checked, want)
	}

	// The file itself keeps the host's preferences.
	if got := string(globalKeys(t, path)["theme"]); got != `"dark"` {
		t.Errorf("the host's file lost its theme: %s", got)
	}
}

// A server that saves a domain change never changes the host's own client
// preferences, even when the host changed them after the server loaded the
// file.
func TestServerKeepsHostPrefs(t *testing.T) {
	run := func(t *testing.T, server bool) (before, after map[string]json.RawMessage) {
		path := hostConfig(t, "dark")
		var a *App
		if server {
			// A server that saves, as P5 runs it: until then the server is
			// read-only (TestReadOnlyServerSavesNothing).
			a = newServerApp(path)
			a.readOnly = false
			newRPCServer(a, io.Discard)
		} else {
			a = &App{cfgPath: path}
		}
		a.be().Init(context.Background())

		// Meanwhile the host's own GitboxApp changes its preferences.
		host := loadConfig(t, path)
		host.Global.Theme = "light"
		host.Global.Editors = append(host.Global.Editors, config.EditorEntry{Name: "Zed", Command: "/usr/local/bin/zed"})
		host.Global.Remotes = append(host.Global.Remotes, config.RemoteEntry{Alias: "second-box"})
		if err := config.Save(host, path); err != nil {
			t.Fatal(err)
		}
		before = globalKeys(t, path)

		if err := a.AddRoot(t.TempDir()); err != nil {
			t.Fatalf("AddRoot: %v", err)
		}
		return before, globalKeys(t, path)
	}

	t.Run("server", func(t *testing.T) {
		before, after := run(t, true)
		for _, key := range []string{"theme", "language", "editors", "remotes", "window"} {
			if !bytes.Equal(before[key], after[key]) {
				t.Errorf("%s changed:\n before %s\n after  %s", key, before[key], after[key])
			}
		}
		var roots []string
		if err := json.Unmarshal(after["roots"], &roots); err != nil || len(roots) != 2 {
			t.Errorf("the domain change was not saved: roots %s", after["roots"])
		}
	})

	// Without the server's rule the same sequence reverts the host's
	// changes: this is what the rule prevents.
	t.Run("an ordinary app would revert them", func(t *testing.T) {
		before, after := run(t, false)
		if bytes.Equal(before["theme"], after["theme"]) {
			t.Skip("the ordinary app no longer overwrites the file's preferences; the control is moot")
		}
	})
}

func TestServerMain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	run := func(args ...string) (code int, stdout, stderr string) {
		var out, errOut bytes.Buffer
		code = serverMain(args, strings.NewReader(""), &out, &errOut)
		return code, out.String(), errOut.String()
	}

	if code, out, _ := run("--version"); code != serverExitOK || out != version+" "+commit+"\n" {
		t.Errorf("--version: exit %d, %q", code, out)
	}

	code, out, _ := run("--doctor")
	var report serverDoctorReport
	if err := json.Unmarshal([]byte(out), &report); code != serverExitOK || err != nil {
		t.Fatalf("--doctor: exit %d, %v, %q", code, err, out)
	}
	if report.OS == "" || report.Arch == "" || report.Home == "" || !strings.HasSuffix(report.ConfigPath, "gitbox.json") ||
		!report.ConfigDirWritable || !report.ConfigDirExec || !strings.HasPrefix(report.GitVersion, "git version") || report.CredentialFindings == nil {
		t.Errorf("--doctor report is incomplete: %+v", report)
	}

	for _, args := range [][]string{
		{}, {"serve"}, {"serve", "--stdio", "--client-id"}, {"serve", "--stdio", "--client-id", "bad id"},
		{"serve", "--stdio", "--client-id", "-oProxyCommand=x", "extra"}, {"--version", "extra"}, {"bogus"},
	} {
		code, out, errOut := run(args...)
		if code != serverExitUsage || out != "" || !strings.Contains(errOut, "usage:") {
			t.Errorf("%v: exit %d, stdout %q; want the usage on stderr and nothing on stdout", args, code, out)
		}
	}
}

func TestProbeDir(t *testing.T) {
	dir := t.TempDir()
	if w, x := probeDir(dir); !w || !x {
		t.Errorf("a fresh temp dir reads writable %v, executable %v", w, x)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the probe left %d files behind", len(entries))
	}
	if w, x := probeDir(filepath.Join(dir, "missing")); w || x {
		t.Errorf("a missing dir reads writable %v, executable %v", w, x)
	}
	missing := filepath.Join(dir, "a", "b", "c")
	if got := nearestDir(missing); got != dir {
		t.Errorf("nearestDir(%s) = %s, want %s", missing, got, dir)
	}
}

// serve answers a client and reports how the connection ended through its
// exit code, which is all a parent process sees.
func TestServeExitCodes(t *testing.T) {
	start := func(t *testing.T, opts rpc.ClientOptions) (*rpc.Client, <-chan int, error) {
		t.Helper()
		c2sR, c2sW := io.Pipe()
		s2cR, s2cW := io.Pipe()
		exit := make(chan int, 1)
		go func() {
			code := serve(newServerApp(hostConfig(t, "dark")), "test-client", c2sR, s2cW, io.Discard)
			_ = s2cW.Close()
			exit <- code
		}()
		client, err := rpc.Dial(s2cR, c2sW, opts)
		if err != nil {
			_ = c2sW.Close()
		}
		return client, exit, err
	}
	wait := func(t *testing.T, exit <-chan int) int {
		t.Helper()
		select {
		case code := <-exit:
			return code
		case <-time.After(15 * time.Second):
			t.Fatal("the server did not exit")
			return -1
		}
	}

	t.Run("a clean end", func(t *testing.T) {
		client, exit, err := start(t, rpc.ClientOptions{Version: version, Commit: commit})
		if err != nil {
			t.Fatal(err)
		}
		var path string
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Call(ctx, "GetConfigPath", nil, &path); err != nil || !strings.HasSuffix(path, "gitbox.json") {
			t.Errorf("GetConfigPath = %q, %v", path, err)
		}
		if err := client.Call(ctx, "AddRoot", []any{"/tmp/x"}, nil); !rpc.IsCode(err, rpc.ErrCodeUnsupported) {
			t.Errorf("a method outside the closed list = %v, want unsupported", err)
		}
		if err := client.Close(); err != nil {
			t.Error(err)
		}
		if code := wait(t, exit); code != serverExitOK {
			t.Errorf("exit %d after the client closed, want %d", code, serverExitOK)
		}
	})

	t.Run("a different build", func(t *testing.T) {
		_, exit, err := start(t, rpc.ClientOptions{Version: version, Commit: "another-commit"})
		if !rpc.IsCode(err, rpc.ErrCodeVersionMismatch) {
			t.Errorf("Dial = %v, want the version mismatch", err)
		}
		if code := wait(t, exit); code != serverExitMismatch {
			t.Errorf("exit %d for a different build, want %d", code, serverExitMismatch)
		}
	})
}

// A server runs read-only until P5 (#240): its background workers run, and
// what the harness watcher detects reaches the client as an event, but
// neither it, the client's SyncAIHarnesses and RefreshAIHarnesses (#243),
// nor a refused save changes the host's gitbox.json.
func TestReadOnlyServerSavesNothing(t *testing.T) {
	if len(knownAIHarnesses) == 0 {
		t.Skip("no known harness to detect")
	}
	// Detection finds a harness the host's config does not list, so a
	// background save would change the file.
	stubHarnessLookup(t, map[string]string{knownAIHarnesses[0].Command: "/opt/bin/" + knownAIHarnesses[0].Command})
	path := contractConfig(t)
	before := stamp(t, path)

	host := newServerApp(path)
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	var serverLog lockedBuffer
	served := make(chan int, 1)
	go func() {
		code := serve(host, "", c2sR, s2cW, &serverLog)
		_ = s2cW.Close()
		served <- code
	}()
	events := &recordingSink{}
	remote, err := dialRemoteBackend(s2cR, c2sW, func() EventSink { return events }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	// The watcher's first pass runs after harnessInitialDelay and keeps
	// what it found in memory.
	detected := func() bool {
		host.mu.Lock()
		defer host.mu.Unlock()
		return host.cfg != nil && len(host.cfg.Global.AIHarnesses) > 0
	}
	deadline := time.Now().Add(harnessInitialDelay + 10*time.Second)
	for !detected() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !detected() {
		t.Fatal("the harness watcher did not run on the server")
	}
	if err := host.be().SetNestedScanDepth(3); toUserError(err).Code != "remote.read_only" {
		t.Errorf("a save on the read-only server = %v, want remote.read_only", err)
	}
	// The window runs the harness detection on the host too (#243): it is
	// served, it is not a mutation, and its result stays in memory. The
	// watcher's pass already reported what it found.
	waitFor := time.Now().Add(10 * time.Second)
	for len(events.named("harnesses:updated")) == 0 && time.Now().Before(waitFor) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(events.named("harnesses:updated")) == 0 {
		t.Error("the watcher's harnesses:updated did not reach the window")
	}
	remote.SyncAIHarnesses()
	remote.RefreshAIHarnesses()
	if err := remote.LastError(); err != nil {
		t.Errorf("the harness detection did not run on the server: %v", err)
	}
	if fetchErr := remote.FetchAllRepos(); fetchErr != nil {
		t.Errorf("FetchAllRepos: %v", fetchErr)
	}
	eventually := time.Now().Add(10 * time.Second)
	for len(events.named("fetch:alldone")) == 0 && time.Now().Before(eventually) {
		time.Sleep(20 * time.Millisecond)
	}

	remote.Close()
	select {
	case code := <-served:
		if code != serverExitOK {
			t.Errorf("serve ended with %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop")
	}
	if after := stamp(t, path); after != before {
		t.Errorf("the read-only server changed the host's gitbox.json")
	}
	if !strings.Contains(serverLog.String(), "fetch skips the repair") {
		t.Errorf("the server did not log that fetch skips the repair of clones: %s", serverLog.String())
	}
}

// lockedBuffer is a bytes.Buffer that several goroutines may write.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// The sshd test of a read-only session (pkg/remote, #240) calls every
// served method; this keeps its list in step with servedMethods.
func TestIntegrationCallsEveryServedMethod(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "pkg", "remote", "integration_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "var readOnlyCalls = ")
	if start < 0 {
		t.Fatal("readOnlyCalls not found in pkg/remote/integration_test.go")
	}
	block := src[start : start+strings.Index(src[start:], "\n}\n")]
	var called []string
	for _, m := range regexp.MustCompile(`\{"(\w+)", \[\]any\{`).FindAllStringSubmatch(block, -1) {
		called = append(called, m[1])
	}
	if !slices.Equal(called, servedMethods) {
		t.Errorf("the sshd test calls %v,\nthe server serves %v", called, servedMethods)
	}
}
