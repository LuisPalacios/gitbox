package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
		info, ok := methodClass[name]
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

// What the server sends carries the host's domain data and none of its
// client preferences.
func TestServerSendsNoClientPrefs(t *testing.T) {
	path := hostConfig(t, "dark")
	a := newServerApp(path)
	newRPCServer(a, io.Discard)
	a.be().Init(context.Background())
	host := hostBackend{localBackend{a}}

	check := func(what string, dto ConfigDTO, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		g := dto.Global
		if g.Theme != "" || g.Language != "" || g.Editors != nil || g.Remotes != nil || g.Window != nil {
			t.Errorf("%s sent client preferences: theme %q, language %q, editors %v, remotes %v, window %v",
				what, g.Theme, g.Language, g.Editors, g.Remotes, g.Window)
		}
		if len(g.IgnoredClones) != 1 || len(g.Roots) != 1 || len(dto.Accounts) != 1 {
			t.Errorf("%s lost domain data: %+v", what, g)
		}
	}
	dto, err := host.GetConfig()
	check("GetConfig", dto, err)
	snap, err := host.GetStartupSnapshot()
	check("GetStartupSnapshot", snap.Config, err)
	dto, err = host.ReloadConfig()
	check("ReloadConfig", dto, err)

	// The file itself is untouched by reading.
	if got := string(globalKeys(t, path)["theme"]); got != `"dark"` {
		t.Errorf("the host's file changed by being read: theme %s", got)
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
			a = newServerApp(path)
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
