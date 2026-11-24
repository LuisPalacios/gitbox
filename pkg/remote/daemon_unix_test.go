//go:build linux || darwin

package remote

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as a daemon: with GBX_FAKE_DAEMON set to a state
// folder it takes that folder's lock, says "locked" and waits to be
// signalled. GBX_FAKE_DAEMON_STUBBORN=1 makes it ignore SIGTERM.
func init() {
	dir := os.Getenv("GBX_FAKE_DAEMON")
	if dir == "" {
		return
	}
	if os.Getenv("GBX_FAKE_DAEMON_STUBBORN") == "1" {
		signal.Ignore(syscall.SIGTERM)
	}
	if _, err := LockDaemon(dir); err != nil {
		fmt.Println("lock failed")
		os.Exit(3)
	}
	fmt.Println("locked")
	time.Sleep(time.Hour)
	os.Exit(0)
}

// fakeDaemon is a running stand-in for a daemon of the state folder dir.
type fakeDaemon struct {
	cmd    *exec.Cmd
	exited chan error
}

// startFakeDaemon runs exe (the test binary or a copy of it) as the daemon
// of dir and waits until it holds the lock.
func startFakeDaemon(t *testing.T, exe, dir string, stubborn bool) *fakeDaemon {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "GBX_FAKE_DAEMON="+dir)
	if stubborn {
		cmd.Env = append(cmd.Env, "GBX_FAKE_DAEMON_STUBBORN=1")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	d := &fakeDaemon{cmd: cmd, exited: make(chan error, 1)}
	line, _ := bufio.NewReader(out).ReadString('\n')
	go func() { _, _ = io.Copy(io.Discard, out); d.exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-d.exited })
	if line != "locked\n" {
		t.Fatalf("the fake daemon said %q", line)
	}
	return d
}

// ended reports whether the fake daemon ended within d. A process that
// ended is seen once only.
func (d *fakeDaemon) ended(within time.Duration) bool {
	select {
	case err := <-d.exited:
		d.exited <- err
		return true
	case <-time.After(within):
		return false
	}
}

// namedServer copies the test binary to dir/gitbox-server, the name the
// host scripts check before they signal a pid.
func namedServer(t *testing.T, dir string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, BinaryName)
	if err := os.WriteFile(exe, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return exe
}

// shortDir is a folder with a short path: a socket path must stay within
// 104 bytes on macOS, where t.TempDir is long.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "gbx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestPrepareStateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), StateDirName)
	if err := CheckStateDir(dir); err != nil {
		t.Errorf("a missing state folder = %v, want no error", err)
	}
	if err := PrepareStateDir(dir); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("state folder: %v, %v; want mode 0700", info, err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CheckStateDir(dir); ReasonOf(err) != ReasonStateUnsafe {
		t.Errorf("CheckStateDir of a 0755 folder = %v, want state_unsafe", err)
	}
	if err := PrepareStateDir(dir); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Errorf("PrepareStateDir left mode %o, want 0700", info.Mode().Perm())
	}

	link := filepath.Join(t.TempDir(), StateDirName)
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateDir(link); ReasonOf(err) != ReasonStateUnsafe {
		t.Errorf("a state folder that is a link = %v, want state_unsafe", err)
	}
	file := filepath.Join(t.TempDir(), StateDirName)
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateDir(file); ReasonOf(err) != ReasonStateUnsafe {
		t.Errorf("a state folder that is a file = %v, want state_unsafe", err)
	}
}

func TestLockDaemon(t *testing.T) {
	dir := t.TempDir()
	lock, err := LockDaemon(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockDaemon(dir); !errors.Is(err, ErrDaemonRunning) {
		t.Errorf("a second lock = %v, want ErrDaemonRunning", err)
	}
	if pid, alive := DaemonAlive(dir); !alive || pid != os.Getpid() {
		t.Errorf("DaemonAlive = %d, %v; want this process", pid, alive)
	}
	if info, err := os.Stat(filepath.Join(dir, PIDName)); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("daemon.pid: %v, %v; want mode 0600", info, err)
	}
	lock.Release()
	if _, alive := DaemonAlive(dir); alive {
		t.Error("the daemon is alive after its lock was released")
	}
	if exists(filepath.Join(dir, PIDName)) {
		t.Error("daemon.pid outlived the lock")
	}
}

func TestListenSocketAndPeerUID(t *testing.T) {
	path := filepath.Join(shortDir(t), SocketName)
	// A socket left by a daemon that died is replaced.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := ListenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket: %v, %v; want mode 0600", info, err)
	}
	accepted := make(chan int, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			accepted <- -2
			return
		}
		defer c.Close()
		uid, err := PeerUID(c)
		if err != nil {
			uid = -3
		}
		accepted <- uid
	}()
	c, err := DialSocket(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if uid := <-accepted; uid != os.Getuid() {
		t.Errorf("peer uid = %d, want %d", uid, os.Getuid())
	}
	if _, err := DialSocket(filepath.Join(shortDir(t), SocketName), time.Second); ReasonOf(err) != ReasonUnreachable {
		t.Errorf("dialling no socket = %v, want unreachable", err)
	}
}

func TestStopDaemon(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("SIGTERM", func(t *testing.T) {
		dir := t.TempDir()
		d := startFakeDaemon(t, self, dir, false)
		if stopped, err := StopDaemon(dir, 5*time.Second); !stopped || err != nil {
			t.Fatalf("StopDaemon = %v, %v", stopped, err)
		}
		if !d.ended(5 * time.Second) {
			t.Error("the daemon still runs")
		}
	})
	t.Run("SIGKILL after the wait", func(t *testing.T) {
		dir := t.TempDir()
		d := startFakeDaemon(t, self, dir, true)
		started := time.Now()
		if stopped, err := StopDaemon(dir, 300*time.Millisecond); !stopped || err != nil {
			t.Fatalf("StopDaemon = %v, %v", stopped, err)
		}
		if !d.ended(5*time.Second) || time.Since(started) < 300*time.Millisecond {
			t.Error("a daemon that ignores SIGTERM was not killed after the wait")
		}
	})
	t.Run("no daemon", func(t *testing.T) {
		dir := t.TempDir()
		// A pid file with no lock holder names no daemon: never signalled.
		if err := os.WriteFile(filepath.Join(dir, PIDName), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		if stopped, err := StopDaemon(dir, time.Second); stopped || err != nil {
			t.Errorf("StopDaemon with no daemon = %v, %v", stopped, err)
		}
	})
}

func TestCollectGarbageStopsTheDaemonOfAnOldBuild(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	now := time.Now()
	cur, old := buildDir(1), buildDir(2)
	touch(t, filepath.Join(root, cur, BinaryName), 0, now)
	touch(t, filepath.Join(root, old, BinaryName), 0, now)
	current := startFakeDaemon(t, self, StateDir(filepath.Join(root, cur)), false)
	previous := startFakeDaemon(t, self, StateDir(filepath.Join(root, old)), false)
	log := &lines{}
	if err := CollectGarbage(GC{Root: root, Current: cur, Owner: Marker{PID: 1, ClientID: "c"}, Now: now, Logf: log.logf, StopWait: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if !previous.ended(5*time.Second) || exists(filepath.Join(root, old)) {
		t.Errorf("the old build's daemon or folder survived:\n%s", strings.Join(log.l, "\n"))
	}
	if !log.has("gc: stopped the daemon of " + old) {
		t.Errorf("the log does not say the daemon was stopped:\n%s", strings.Join(log.l, "\n"))
	}
	if current.ended(100*time.Millisecond) || !exists(filepath.Join(root, cur, BinaryName)) {
		t.Error("the current build's daemon or folder was touched")
	}
}

func TestSpawnDetached(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	done, err := SpawnDetached("/bin/sh", []string{"-c", `printf "%s %s %s" "$$" "$(ps -o pgid= -p $$ | tr -d " ")" "$(pwd -P)" > out`}, dir)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the child did not end")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(string(data))
	real, _ := filepath.EvalSymlinks(dir)
	if len(f) != 3 || f[0] != f[1] || f[2] != real {
		t.Errorf("child said %q: want its own process group, in %s", data, real)
	}
	if f[1] == strconv.Itoa(syscall.Getpgrp()) {
		t.Error("the child stayed in the test's process group")
	}
}

// TestFakeUninstallStopsDaemons: uninstall and reinstall stop the daemon of
// each build they delete, on the host, before the folder goes.
func TestFakeUninstallStopsDaemons(t *testing.T) {
	now := time.Now()
	for _, op := range []string{"uninstall", "reinstall"} {
		t.Run(op, func(t *testing.T) {
			f := newFake(t, fakeServer)
			s, err := Dial(ctx(t), "host", f.opts)
			if err != nil {
				t.Fatal(err)
			}
			_ = s.Close()
			buildFolder := filepath.Dir(f.server)
			exe := namedServer(t, filepath.Join(t.TempDir(), "bin"))
			d := startFakeDaemon(t, exe, StateDir(buildFolder), false)
			touch(t, filepath.Join(buildFolder, "in-use.client-a"), 20*time.Minute, now)
			switch op {
			case "uninstall":
				if res, err := Uninstall(ctx(t), "host", f.opts, false); err != nil || !res.Done {
					t.Fatalf("Uninstall = %+v, %v", res, err)
				}
			case "reinstall":
				if err := Reinstall(ctx(t), "host", f.opts); err != nil {
					t.Fatal(err)
				}
			}
			if !d.ended(10 * time.Second) {
				t.Errorf("%s left the daemon running", op)
			}
			if exists(buildFolder) {
				t.Errorf("%s left the build folder", op)
			}
		})
	}
}

// TestFakeDialThroughProxy: with Daemon set the host starts the session
// through gitbox-server proxy.
func TestFakeDialThroughProxy(t *testing.T) {
	f := newFake(t, fakeServer)
	f.opts.Daemon = true
	s, err := Dial(ctx(t), "host", f.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	echo(t, s)
	data, err := os.ReadFile(filepath.Join(f.home, "args.log"))
	if err != nil || !strings.Contains(string(data), "proxy --client-id client-a") {
		t.Errorf("the server ran as %q, want proxy --client-id client-a", data)
	}
}
