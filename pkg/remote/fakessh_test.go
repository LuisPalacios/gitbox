package remote

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
     echo "hostname $alias"; echo "controlpath none"; exit 0 ;;
  O) exit 255 ;;
esac
if [ "$alias" = untrusted ]; then echo "Host key verification failed." >&2; exit 255; fi
[ -n "$FAKE_SSH_BANNER" ] && cat "$FAKE_SSH_BANNER"
PATH="$FAKE_SSH_BIN:$PATH" HOME="$FAKE_SSH_HOME" exec /bin/sh -c "$*"
`

// fakeServer stands in for gitbox-server: --version logs that it ran,
// serve echoes its input after a greeting.
const fakeServer = `#!/bin/sh
echo ran >> "$HOME/ran.log"
case "$1" in
  --version) echo "v1 test"; exit 0 ;;
  --doctor) echo '{"version":"v1"}'; exit 0 ;;
  serve) printf 'SERVED\n'; exec cat ;;
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
	// The fake host is Linux, whatever runs the test.
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	uname := "#!/bin/sh\ncase \"$1\" in -s) echo Linux ;; -m) echo x86_64 ;; esac\n"
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
