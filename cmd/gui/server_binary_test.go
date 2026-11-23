package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// buildServerBinary compiles gitbox-server the way a release does: this
// package with the gitboxserver tag and cgo off.
func buildServerBinary(t *testing.T) string {
	t.Helper()
	name := "gitbox-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-tags", "gitboxserver", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building gitbox-server: %v\n%s", err, out)
	}
	return bin
}

// The real gitbox-server, as a separate process, answers the real client
// over its stdin and stdout, and exits when its input ends. This is the
// whole path of the remote mode except the SSH connection in the middle.
func TestGitboxServerBinary(t *testing.T) {
	bin := buildServerBinary(t)

	// The host: its gitbox.json where the server looks for it.
	xdg := t.TempDir()
	seed, err := os.ReadFile(contractConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(xdg, "gitbox"), 0o700); err != nil {
		t.Fatal(err)
	}
	hostConfigPath := filepath.Join(xdg, "gitbox", "gitbox.json")
	if err := os.WriteFile(hostConfigPath, seed, 0o600); err != nil {
		t.Fatal(err)
	}

	// On a real host the login shell may print a banner before the server
	// starts; the client must get past it.
	server := exec.Command(bin, "serve", "--stdio", "--client-id", "binary-test")
	if runtime.GOOS != "windows" {
		server = exec.Command("sh", "-c", `echo "Welcome to this host"; echo; exec "$0" serve --stdio --client-id binary-test`, bin)
	}
	server.Env = append(os.Environ(), "XDG_CONFIG_HOME="+xdg)
	stdin, err := server.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := server.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	server.Stderr = &stderr
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	t.Cleanup(func() {
		_ = server.Process.Kill()
	})

	remote, err := dialRemoteBackend(stdout, stdin, func() EventSink { return nopSink{} }, io.Discard)
	if err != nil {
		_ = stdin.Close()
		t.Fatalf("dial: %v\nserver stderr: %s", err, stderr.String())
	}
	if hello := remote.client.Server(); hello.OS != runtime.GOOS || hello.Arch != runtime.GOARCH || hello.PID != server.Process.Pid {
		t.Errorf("the server introduced itself as %+v, but it is pid %d on %s/%s", hello, server.Process.Pid, runtime.GOOS, runtime.GOARCH)
	}

	if got := remote.GetConfigPath(); got != hostConfigPath {
		t.Errorf("GetConfigPath = %q, want the host's %q", got, hostConfigPath)
	}
	cfg, err := remote.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if len(cfg.Accounts) != 1 || len(cfg.Sources["acct"].Repos) != 1 {
		t.Errorf("GetConfig lost the host's accounts or repos: %+v", cfg)
	}
	if cfg.Global.Theme != "" || cfg.Global.Editors != nil || cfg.Global.Remotes != nil {
		t.Errorf("the server sent the host's client preferences: %+v", cfg.Global)
	}
	statuses := remote.GetAllStatus()
	// The fixture clone has no remote, so git reports it without upstream:
	// the server ran git on the host's folder and read its answer.
	if len(statuses) != 1 || statuses[0].Repo != "org/repo" || statuses[0].State != "no upstream" || statuses[0].Branch != "main" {
		t.Errorf("GetAllStatus = %+v, want the one clone, on main with no upstream", statuses)
	}
	if err := remote.ChangeCredentialType("acct", "ssh"); toUserError(err).Code != "remote.not_implemented_remote" {
		t.Errorf("ChangeCredentialType = %v, want not_implemented_remote", err)
	}
	if err := remote.LastError(); err != nil {
		t.Errorf("a call failed: %v", err)
	}

	// The end of its input is the server's signal to exit.
	remote.Close()
	go func() { exited <- server.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("the server exited with %v\nstderr: %s", err, stderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("the server was still running 15 seconds after its input ended\nstderr: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "serving client binary-test") {
		t.Errorf("the server did not log its client: %q", stderr.String())
	}

	// A server of another build is refused, and says so with its exit code.
	other := exec.Command(bin, "serve", "--stdio")
	other.Env = append(os.Environ(), "XDG_CONFIG_HOME="+xdg)
	in, _ := other.StdinPipe()
	out, _ := other.StdoutPipe()
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	_, err = rpc.Dial(out, in, rpc.ClientOptions{Version: version, Commit: "another-build"})
	if !rpc.IsCode(err, rpc.ErrCodeVersionMismatch) {
		t.Errorf("Dial with another build = %v, want the version mismatch", err)
	}
	_ = in.Close()
	if code := exitCode(other.Wait()); code != serverExitMismatch {
		t.Errorf("exit code %d for another build, want %d", code, serverExitMismatch)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
