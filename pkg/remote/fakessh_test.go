package remote

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests run the real remote scripts through a fake ssh that executes
// the remote command with the local sh, in a home folder of the test's own.
// They prove the scripts before the sshd integration lane does, on every
// POSIX host.

const fakeSSH = `#!/bin/sh
mode=run
while [ $# -gt 0 ]; do
  case "$1" in
    -T) shift ;;
    -o|-e|-F) shift 2 ;;
    -G) mode=G; shift ;;
    -O) mode=O; shift 2 ;;
    *) break ;;
  esac
done
alias=$1; shift
case $mode in
  G) [ "$alias" = broken ] && { echo "Bad configuration option: x" >&2; exit 255; }
     echo "hostname $alias"; echo "controlpath none"
     [ -n "$FAKE_SSH_AGENT" ] && echo "identityagent $FAKE_SSH_AGENT"
     exit 0 ;;
  O) exit 255 ;;
esac
if [ "$alias" = untrusted ]; then echo "Host key verification failed." >&2; exit 255; fi
[ -n "$FAKE_SSH_DELAY" ] && sleep "$FAKE_SSH_DELAY"
[ -n "$FAKE_SSH_BANNER" ] && cat "$FAKE_SSH_BANNER"
PATH="$FAKE_SSH_BIN:$PATH" HOME="$FAKE_SSH_HOME" exec /bin/sh -c "$*"
`

// fakeServer stands in for gitbox-server: --version logs that it ran,
// serve and proxy echo their input after a greeting. args.log keeps how it
// was started.
const fakeServer = `#!/bin/sh
echo ran >> "$HOME/ran.log"
echo "$*" >> "$HOME/args.log"
case "$1" in
  --version) echo "v1 test"; exit 0 ;;
  --doctor) echo '{"version":"v1"}'; exit 0 ;;
  serve|proxy) printf 'SERVED\n'; exec cat ;;
esac
exit 2
`

type fake struct {
	home   string
	opts   Options
	log    *Log
	bin    []byte
	pin    string
	build  string
	server string // the final binary's path
}

func newFake(t *testing.T, serverScript string) *fake {
	t.Helper()
	requireSh(t)
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte(fakeSSH), 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SSH_HOME", home)
	// The fake host is Linux, whatever runs the test, unless FAKE_UNAME_S
	// and FAKE_UNAME_M say otherwise.
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	uname := "#!/bin/sh\ncase \"$1\" in -s) echo \"${FAKE_UNAME_S:-Linux}\" ;; -m) echo \"${FAKE_UNAME_M:-x86_64}\" ;; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "uname"), []byte(uname), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SSH_BIN", bin)
	t.Setenv("FAKE_SSH_BANNER", "")
	f := &fake{home: home, log: NewLog(nil), bin: []byte(serverScript)}
	sum := sha256.Sum256(f.bin)
	f.pin = hex.EncodeToString(sum[:])
	f.opts = Options{
		ClientID: "client-a", Version: "v1", Commit: strings.Repeat("c", 40), Log: f.log,
		Target: func(goos, goarch string) (Target, error) {
			return Target{SHA256: f.pin, Size: int64(len(f.bin)), Load: func() ([]byte, error) { return f.bin, nil }}, nil
		},
	}
	SetSSHPath(&f.opts, ssh)
	f.build = BuildName(f.opts.Version, f.opts.Commit, f.pin)
	f.server = filepath.Join(home, ".gitbox-server", f.build, BinaryName)
	return f
}

func (f *fake) runs(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.home, "ran.log"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "ran\n")
}

func codeOf(err error) Code {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return ""
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

// echo checks that the session speaks to the fake server.
func echo(t *testing.T, s *Session) {
	t.Helper()
	conn := s.Conn()
	br := bufio.NewReader(conn)
	if line, err := br.ReadString('\n'); err != nil || line != "SERVED\n" {
		t.Fatalf("greeting = %q, %v", line, err)
	}
	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatal(err)
	}
	if line, err := br.ReadString('\n'); err != nil || line != "ping\n" {
		t.Fatalf("echo = %q, %v", line, err)
	}
}

func TestFakeInstallThenStart(t *testing.T) {
	f := newFake(t, fakeServer)
	installs := 0
	f.opts.OnInstall = func() { installs++ }
	s, err := Dial(ctx(t), "host", f.opts)
	if err != nil {
		t.Fatalf("Dial: %v\n%s", err, strings.Join(f.log.Lines(), "\n"))
	}
	echo(t, s)
	_ = s.Close()
	if s.Err() != nil {
		t.Errorf("a closed session reports %v", s.Err())
	}
	info, err := os.Stat(f.server)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("final binary: %v %v", info, err)
	}
	if dir, _ := os.Stat(filepath.Dir(f.server)); dir.Mode().Perm() != 0o700 {
		t.Errorf("build folder mode %v", dir.Mode().Perm())
	}
	if installs != 1 {
		t.Errorf("installs = %d", installs)
	}
	// The second connect finds the server and does not install.
	s, err = Dial(ctx(t), "host", f.opts)
	if err != nil {
		t.Fatal(err)
	}
	echo(t, s)
	_ = s.Close()
	if installs != 1 {
		t.Errorf("the second connect installed again")
	}
	if p := s.Probe(); p.OS != "linux" || p.Arch != "amd64" || p.Home != f.home {
		t.Errorf("probe = %+v", p)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(f.server), tmpPrefix+"*"))
	if len(leftovers) != 0 {
		t.Errorf("temporary files left: %v", leftovers)
	}
}

func TestFakeWrongPinNeverRuns(t *testing.T) {
	f := newFake(t, fakeServer)
	f.pin = strings.Repeat("0", 64)
	_, err := Dial(ctx(t), "host", f.opts)
	if codeOf(err) != CodeHashMismatch {
		t.Fatalf("err = %v, want hash_mismatch", err)
	}
	if exists(f.server) {
		t.Error("a final binary exists after a hash mismatch")
	}
	if n := f.runs(t); n != 0 {
		t.Errorf("the uploaded file ran %d times", n)
	}
	tmps, _ := filepath.Glob(filepath.Join(f.home, ".gitbox-server", "*", tmpPrefix+"*"))
	if len(tmps) != 0 {
		t.Errorf("the rejected upload was kept: %v", tmps)
	}
}

func TestFakeCorruptUpload(t *testing.T) {
	f := newFake(t, fakeServer)
	SetUploadFilter(&f.opts, func(r io.Reader) io.Reader {
		data, _ := io.ReadAll(r)
		data[len(data)/2] ^= 0xff
		return strings.NewReader(string(data))
	})
	if _, err := Dial(ctx(t), "host", f.opts); codeOf(err) != CodeHashMismatch {
		t.Fatalf("err = %v, want hash_mismatch", err)
	}
	if exists(f.server) || f.runs(t) != 0 {
		t.Error("a corrupt upload was installed or ran")
	}
}

func TestFakeModifiedBetweenStarts(t *testing.T) {
	f := newFake(t, fakeServer)
	s, err := Dial(ctx(t), "host", f.opts)
	if err != nil {
		t.Fatal(err)
	}
	echo(t, s)
	_ = s.Close()
	before := f.runs(t)
	file, err := os.OpenFile(f.server, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("#")
	_ = file.Close()
	if _, err := Dial(ctx(t), "host", f.opts); codeOf(err) != CodeHashMismatch {
		t.Fatalf("err = %v, want hash_mismatch", err)
	}
	if f.runs(t) != before {
		t.Error("a modified server ran")
	}
}

func TestFakeNoexec(t *testing.T) {
	// A file that cannot run answers the trial run with 126, like a noexec
	// mount.
	f := newFake(t, "#!/bin/sh\nexit 126\n")
	_, err := Dial(ctx(t), "host", f.opts)
	if codeOf(err) != CodeNoexec || !strings.Contains(err.Error(), "server_dir") {
		t.Fatalf("err = %v, want noexec with the server_dir hint", err)
	}
	if exists(f.server) {
		t.Error("a binary that cannot run was installed")
	}
}

func TestFakeBanner(t *testing.T) {
	f := newFake(t, fakeServer)
	banner := filepath.Join(t.TempDir(), "banner")
	if err := os.WriteFile(banner, []byte(strings.Repeat("welcome\n", 8192)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SSH_BANNER", banner)
	s, err := Dial(ctx(t), "host", f.opts)
	if err != nil {
		t.Fatalf("Dial with a 64 KiB banner: %v", err)
	}
	echo(t, s)
	_ = s.Close()
	if err := os.WriteFile(banner, []byte(strings.Repeat("x", maxBanner+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(ctx(t), "host", f.opts); codeOf(err) != CodeBannerOverflow {
		t.Fatalf("err = %v, want banner_overflow", err)
	}
}

func TestFakeSSHFailures(t *testing.T) {
	f := newFake(t, fakeServer)
	if _, err := Dial(ctx(t), "untrusted", f.opts); codeOf(err) != CodeSSHHostKey {
		t.Errorf("unknown host: %v", err)
	}
	if err := CheckAlias(ctx(t), "broken", f.opts); codeOf(err) != CodeSSHConfig {
		t.Errorf("broken config: %v", err)
	}
	if err := CheckAlias(ctx(t), "host", f.opts); err != nil {
		t.Errorf("CheckAlias: %v", err)
	}
	o := f.opts
	SetSSHPath(&o, "")
	t.Setenv("PATH", t.TempDir())
	if _, err := Dial(ctx(t), "host", o); codeOf(err) != CodeSSHMissing {
		t.Errorf("no ssh: %v", err)
	}
}

func TestFakeDoctorAndReinstall(t *testing.T) {
	f := newFake(t, fakeServer)
	if _, err := Doctor(ctx(t), "host", f.opts); err == nil {
		t.Error("doctor ran without an installed server")
	}
	s, err := Dial(ctx(t), "host", f.opts)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	report, err := Doctor(ctx(t), "host", f.opts)
	if err != nil || string(report) != `{"version":"v1"}` {
		t.Fatalf("Doctor = %q, %v", report, err)
	}
	other := filepath.Join(f.home, ".gitbox-server", buildDir(9), BinaryName)
	touch(t, other, 0, time.Now())
	if err := Reinstall(ctx(t), "host", f.opts); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Dir(f.server)) || !exists(other) {
		t.Error("reinstall did not remove exactly this build")
	}
}

func TestFakeUninstall(t *testing.T) {
	now := time.Now()
	t.Run("keeps what gitbox did not create", func(t *testing.T) {
		f := newFake(t, fakeServer)
		f.opts.ServerDir = "~/srv"
		root := filepath.Join(f.home, "srv")
		touch(t, filepath.Join(root, buildDir(1), BinaryName), 0, now)
		touch(t, filepath.Join(root, LogsDir, buildDir(1)+".log"), 0, now)
		touch(t, filepath.Join(root, lockName, lockOwner), 0, now)
		touch(t, filepath.Join(root, tmpPrefix+"x"+tmpSuffix), 0, now)
		touch(t, filepath.Join(root, "foreign.txt"), 0, now)
		touch(t, filepath.Join(root, "not-a-build", "file"), 0, now)
		res, err := Uninstall(ctx(t), "host", f.opts, false)
		if err != nil || !res.Done || !res.RootKept {
			t.Fatalf("Uninstall = %+v, %v\n%s", res, err, strings.Join(f.log.Lines(), "\n"))
		}
		for _, p := range []string{buildDir(1), LogsDir, lockName, tmpPrefix + "x" + tmpSuffix} {
			if exists(filepath.Join(root, p)) {
				t.Errorf("%s survived the uninstall", p)
			}
		}
		for _, p := range []string{"foreign.txt", "not-a-build/file"} {
			if !exists(filepath.Join(root, p)) {
				t.Errorf("%s, which gitbox did not create, was deleted", p)
			}
		}
	})
	t.Run("removes a root that only gitbox used", func(t *testing.T) {
		f := newFake(t, fakeServer)
		s, err := Dial(ctx(t), "host", f.opts)
		if err != nil {
			t.Fatal(err)
		}
		_ = s.Close()
		res, err := Uninstall(ctx(t), "host", f.opts, false)
		if err != nil || !res.Done || res.RootKept {
			t.Fatalf("Uninstall = %+v, %v", res, err)
		}
		if exists(filepath.Join(f.home, ".gitbox-server")) {
			t.Error("the root survived")
		}
	})
	t.Run("asks when another client is live", func(t *testing.T) {
		f := newFake(t, fakeServer)
		root := filepath.Join(f.home, ".gitbox-server")
		touch(t, filepath.Join(root, buildDir(1), markerPrefix+"client-b"), time.Minute, now)
		touch(t, filepath.Join(root, buildDir(1), markerPrefix+"client-a"), time.Minute, now)
		touch(t, filepath.Join(root, buildDir(2), markerPrefix+"client-c"), 20*time.Minute, now)
		res, err := Uninstall(ctx(t), "host", f.opts, false)
		if err != nil || res.Done || res.OtherClients != 1 {
			t.Fatalf("Uninstall = %+v, %v", res, err)
		}
		if !exists(root) {
			t.Fatal("it deleted without the confirmation")
		}
		if res, err = Uninstall(ctx(t), "host", f.opts, true); err != nil || !res.Done || exists(root) {
			t.Fatalf("forced Uninstall = %+v, %v", res, err)
		}
	})
	t.Run("refuses the home folder", func(t *testing.T) {
		f := newFake(t, fakeServer)
		touch(t, filepath.Join(f.home, "precious"), 0, now)
		touch(t, filepath.Join(f.home, buildDir(1), BinaryName), 0, now)
		f.opts.ServerDir = "~/"
		if _, err := Uninstall(ctx(t), "host", f.opts, true); codeOf(err) != CodeServerDirUnsafe {
			t.Errorf("~/ root: %v", err)
		}
		// A root that is the home folder through a link is refused by the
		// host side too.
		if err := os.Symlink(f.home, filepath.Join(f.home, "link")); err != nil {
			t.Fatal(err)
		}
		f.opts.ServerDir = "~/link"
		if _, err := Uninstall(ctx(t), "host", f.opts, true); codeOf(err) != CodeServerDirUnsafe {
			t.Errorf("a link to the home: %v", err)
		}
		if _, err := Dial(ctx(t), "host", f.opts); codeOf(err) != CodeServerDirUnsafe {
			t.Errorf("install into a link to the home: %v", err)
		}
		if !exists(filepath.Join(f.home, "precious")) || !exists(filepath.Join(f.home, buildDir(1))) {
			t.Error("files in the home folder were deleted")
		}
	})
}

// TestFakeStartAnswerAfterLongBanner pins a race: the start command exits
// right after a short answer that follows a long banner, and that answer
// must still be read after ssh has exited.
func TestFakeStartAnswerAfterLongBanner(t *testing.T) {
	f := newFake(t, fakeServer)
	banner := filepath.Join(t.TempDir(), "banner")
	if err := os.WriteFile(banner, []byte(strings.Repeat("noise\n", 40000)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SSH_BANNER", banner)
	r, err := newRunner("host", f.opts)
	if err != nil {
		t.Fatal(err)
	}
	l, err := newLayout("")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 30 {
		s, missing, err := r.start(ctx(t), l, f.build, f.pin, "serve")
		if err != nil || !missing || s != nil {
			t.Fatalf("start %d: missing %v, err %v\n%s", i, missing, err, strings.Join(f.log.Lines(), "\n"))
		}
	}
}

// A server_dir can be a shared folder with a logs/ of its own: uninstall
// deletes only the build logs in it, like garbage collection does.
func TestFakeUninstallKeepsForeignLogs(t *testing.T) {
	now := time.Now()
	f := newFake(t, fakeServer)
	f.opts.ServerDir = "~/work"
	root := filepath.Join(f.home, "work")
	logs := filepath.Join(root, LogsDir)
	touch(t, filepath.Join(root, buildDir(1), BinaryName), 0, now)
	own := []string{buildDir(1) + ".log", buildDir(1) + ".log.1", buildDir(1) + ".log.3", buildDir(2) + ".log.2"}
	for _, name := range own {
		touch(t, filepath.Join(logs, name), 0, now)
	}
	foreign := []string{"notes.txt", "app.log", "app.log.1", buildDir(1) + ".log.9"}
	for _, name := range foreign {
		touch(t, filepath.Join(logs, name), 0, now)
	}
	res, err := Uninstall(ctx(t), "host", f.opts, false)
	if err != nil || !res.Done || !res.RootKept {
		t.Fatalf("Uninstall = %+v, %v\n%s", res, err, strings.Join(f.log.Lines(), "\n"))
	}
	for _, name := range own {
		if exists(filepath.Join(logs, name)) {
			t.Errorf("the build log %s survived the uninstall", name)
		}
	}
	for _, name := range foreign {
		if !exists(filepath.Join(logs, name)) {
			t.Errorf("logs/%s, which gitbox did not create, was deleted", name)
		}
	}
	if exists(filepath.Join(root, buildDir(1))) {
		t.Error("the build folder survived the uninstall")
	}

	// A logs/ that held only build logs goes with them.
	for _, name := range foreign {
		_ = os.Remove(filepath.Join(logs, name))
	}
	touch(t, filepath.Join(logs, buildDir(3)+".log"), 0, now)
	if res, err := Uninstall(ctx(t), "host", f.opts, false); err != nil || !res.Done || res.RootKept {
		t.Fatalf("second Uninstall = %+v, %v", res, err)
	}
	if exists(root) {
		t.Error("a root left with only gitbox's logs was kept")
	}
}

// A final file that exists but cannot be hashed is reported as a hash
// mismatch: it never starts and is never replaced by a new install.
func TestFakeUnreadableFinalNotReinstalled(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 000 file")
	}
	f := newFake(t, fakeServer)
	s, err := Dial(ctx(t), "host", f.opts)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	want, err := os.ReadFile(f.server)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.server, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.server, 0o700) })
	runs := f.runs(t)
	installs := 0
	f.opts.OnInstall = func() { installs++ }
	before := len(f.log.Lines())

	_, err = Dial(ctx(t), "host", f.opts)
	if codeOf(err) != CodeHashMismatch {
		t.Fatalf("err = %v, want hash_mismatch\n%s", err, strings.Join(f.log.Lines(), "\n"))
	}
	for _, line := range f.log.Lines()[before:] {
		if strings.Contains(line, "install:") {
			t.Errorf("an install step ran: %s", line)
		}
	}
	if installs != 0 || f.runs(t) != runs {
		t.Errorf("installs %d, runs %d → %d", installs, runs, f.runs(t))
	}
	info, err := os.Stat(f.server)
	if err != nil || info.Mode().Perm() != 0 {
		t.Fatalf("final file: %v, %v; want it in place with mode 000", info, err)
	}
	if err := os.Chmod(f.server, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(f.server); err != nil || !bytes.Equal(got, want) {
		t.Errorf("the final file's bytes changed: %v", err)
	}
	if tmps, _ := filepath.Glob(filepath.Join(filepath.Dir(f.server), tmpPrefix+"*")); len(tmps) != 0 {
		t.Errorf("an upload happened: %v", tmps)
	}
}

// TestSlowLoginNamesTheAgent: a first ssh that has not logged in after
// loginWait calls OnSlowLogin once, with 1Password named when the alias's
// IdentityAgent, or SSH_AUTH_SOCK without one, is its socket. A fast login
// calls nothing.
func TestSlowLoginNamesTheAgent(t *testing.T) {
	f := newFake(t, fakeServer)
	old := loginWait
	loginWait = 100 * time.Millisecond
	t.Cleanup(func() { loginWait = old })
	const onePassword = "/Users/x/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"
	for _, tc := range []struct {
		name, agent, authSock, delay string
		want                         []string
	}{
		{name: "fast login", agent: onePassword},
		{name: "slow, 1Password in the ssh config", agent: onePassword, delay: "1", want: []string{AgentOnePassword}},
		{name: "slow, 1Password in SSH_AUTH_SOCK", authSock: "/home/x/.1password/agent.sock", delay: "1", want: []string{AgentOnePassword}},
		{name: "slow, another agent", agent: "/tmp/ssh-x/agent.1", delay: "1", want: []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FAKE_SSH_AGENT", tc.agent)
			t.Setenv("SSH_AUTH_SOCK", tc.authSock)
			t.Setenv("FAKE_SSH_DELAY", tc.delay)
			o := f.opts
			o.ControlDir = t.TempDir()
			var mu sync.Mutex
			var got []string
			o.OnSlowLogin = func(agent string) { mu.Lock(); got = append(got, agent); mu.Unlock() }
			r, err := newRunner("host", o)
			if err != nil {
				t.Fatal(err)
			}
			r.setupControl(ctx(t))
			if _, err := r.probe(ctx(t)); err != nil {
				t.Fatalf("probe: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(got, tc.want) {
				t.Errorf("OnSlowLogin calls = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCancelEndsAStalledLogin: a cancelled operation ends even when a
// descendant of ssh still holds its stdout. The fake's delay runs sleep as
// such a child, the way a ProxyCommand would.
func TestCancelEndsAStalledLogin(t *testing.T) {
	f := newFake(t, fakeServer)
	t.Setenv("FAKE_SSH_DELAY", "30")
	c, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	_, err := Uninstall(c, "host", f.opts, false)
	if took := time.Since(t0); took > 10*time.Second {
		t.Errorf("Uninstall took %s after the cancel", took)
	}
	if codeOf(err) != CodeSSHTimeout {
		t.Errorf("Uninstall = %v (%s), want %s", err, codeOf(err), CodeSSHTimeout)
	}
}

// TestLoginLimitStopsUninstall: an uninstall whose ssh never logs in, as
// with an agent that waits for an approval nobody gives, reports the slow
// login once with the agent named and stops at loginLimit with
// CodeSSHLogin, long before the operation's own deadline.
func TestLoginLimitStopsUninstall(t *testing.T) {
	f := newFake(t, fakeServer)
	oldWait, oldLimit := loginWait, loginLimit
	loginWait, loginLimit = 100*time.Millisecond, 600*time.Millisecond
	t.Cleanup(func() { loginWait, loginLimit = oldWait, oldLimit })
	t.Setenv("FAKE_SSH_AGENT", "/Users/x/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock")
	t.Setenv("FAKE_SSH_DELAY", "30")
	o := f.opts
	o.ControlDir = t.TempDir()
	var mu sync.Mutex
	var slow []string
	o.OnSlowLogin = func(agent string) { mu.Lock(); slow = append(slow, agent); mu.Unlock() }
	t0 := time.Now()
	_, err := Uninstall(ctx(t), "host", o, false)
	if took := time.Since(t0); took > 10*time.Second {
		t.Errorf("Uninstall took %s", took)
	}
	if codeOf(err) != CodeSSHLogin || !strings.Contains(err.Error(), "1Password") {
		t.Errorf("Uninstall = %v (%s), want %s naming 1Password", err, codeOf(err), CodeSSHLogin)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(slow, []string{AgentOnePassword}) {
		t.Errorf("OnSlowLogin calls = %q, want one for 1Password", slow)
	}
}

// TestLoginLimitOnlyBoundsTheLogin: once the first ssh logged in, a slow
// remote command is not a slow login: the limit and the hint are gone.
func TestLoginLimitOnlyBoundsTheLogin(t *testing.T) {
	f := newFake(t, fakeServer)
	oldWait, oldLimit := loginWait, loginLimit
	loginWait, loginLimit = 100*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { loginWait, loginLimit = oldWait, oldLimit })
	o := f.opts
	called := false
	o.OnSlowLogin = func(string) { called = true }
	r, err := newRunner("host", o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.run(ctx(t), "first", `printf "\0GBX\0"`, nil); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := r.run(ctx(t), "slow", `sleep 1; printf "\0GBX\0x\0"`, nil); err != nil {
		t.Fatalf("a slow command after the login failed: %v", err)
	}
	if called {
		t.Error("OnSlowLogin ran for a command after the login")
	}
}
