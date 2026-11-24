//go:build sshd_integration || remote_host

package remote_test

// The integration tests of remote mode against a real sshd. The sshd lane
// (itdocker_test.go, tag sshd_integration) gives each test a fresh
// container; the real-host lane (ithost_test.go, tag remote_host, #245)
// runs the tests that need no container of their own against a host with a
// disposable account, such as a Mac with Remote Login. "Never executed"
// means the line count of the sentinel's $HOME/.gitbox-e2e/ran.log did not
// change.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// it is what the harness's TestMain prepared for every test.
var it struct {
	server          []byte
	pin             string
	version, commit string
	// target is the server's platform, "linux/amd64", and goos its first
	// half.
	target, goos string
	// keyDir is a short folder for keys and ssh control sockets.
	keyDir string
}

// testHome is the home of the test user in the sshd containers.
const testHome = "/home/test"

// loadServer reads the server that the lane's script built for target.
func loadServer(target string) error {
	it.version, it.commit = os.Getenv("GITBOX_IT_VERSION"), os.Getenv("GITBOX_IT_COMMIT")
	serverPath := os.Getenv("GITBOX_IT_SERVER")
	if serverPath == "" || it.version == "" || it.commit == "" {
		return errors.New("GITBOX_IT_SERVER, GITBOX_IT_VERSION and GITBOX_IT_COMMIT are required")
	}
	var err error
	if it.server, err = os.ReadFile(serverPath); err != nil {
		return fmt.Errorf("reading the server: %w", err)
	}
	sum := sha256.Sum256(it.server)
	it.pin = hex.EncodeToString(sum[:])
	it.target = target
	it.goos, _, _ = strings.Cut(target, "/")
	return nil
}

type ctOpts struct {
	noexecHome bool
	shell      string
	// bashrc is what the login shell prints before every command: the
	// container's ~/.bashrc, a real host's rc file for its shell.
	bashrc string
	// netAdmin lets the test shape the container's network with tc (#295).
	netAdmin bool
	// gitDaemon serves the fixture repository on git://127.0.0.1:9418.
	gitDaemon bool
	// gitHost makes the container the git host of an SSH account (#244):
	// the git user, with git-shell, owns org/small.git.
	gitHost bool
}

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// q quotes s as one word of a POSIX shell.
func q(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (c *container) runs() int {
	c.t.Helper()
	out := c.sh("cat " + q(c.home()+"/.gitbox-e2e/ran.log") + " 2>/dev/null || true")
	if out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}

// The host commands below differ between GNU coreutils (the containers)
// and BSD (macOS).

// mode is the permission bits of path in octal, such as "700".
func (c *container) mode(path string) string {
	c.t.Helper()
	if c.goos() == "darwin" {
		return c.sh("stat -f %Lp " + q(path))
	}
	return c.sh("stat -c %a " + q(path))
}

// age sets the modification time of path to d ago.
func (c *container) age(path string, d time.Duration) {
	c.t.Helper()
	ago := "$(( $(date +%s) - " + strconv.Itoa(int(d.Seconds())) + " ))"
	if c.goos() == "darwin" {
		c.sh("touch -t \"$(date -r " + ago + " +%Y%m%d%H%M.%S)\" " + q(path))
		return
	}
	c.sh("touch -d @" + ago + " " + q(path))
}

// sha256 is the hex SHA-256 of a file of the host.
func (c *container) sha256(path string) string {
	c.t.Helper()
	tool := "sha256sum "
	if c.goos() == "darwin" {
		tool = "shasum -a 256 "
	}
	return strings.Fields(c.sh(tool + q(path)))[0]
}

// terminalShell is a login shell of TestRemoteTerminalOverSSH.
type terminalShell struct {
	shell  string
	family remote.Family
}

// session is the client side of one test against one container.
type session struct {
	c     *container
	opts  remote.Options
	log   *remote.Log
	build string
	root  string
	final string
}

const alias = "it-host"

func (c *container) client(t *testing.T, clientID string, knownHost bool) *session {
	t.Helper()
	return c.clientWith(t, clientID, knownHost, "")
}

// clientWith is client with extra options in the Host block of its ssh
// config, such as ForwardAgent (#244).
func (c *container) clientWith(t *testing.T, clientID string, knownHost bool, extra string) *session {
	t.Helper()
	dir := t.TempDir()
	known := filepath.Join(dir, "known_hosts")
	content := ""
	if knownHost {
		content = c.knownHosts()
	}
	if err := os.WriteFile(known, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config")
	conf := fmt.Sprintf("Host %s\n%s  IdentitiesOnly yes\n  IdentityAgent none\n  UserKnownHostsFile %s\n  GlobalKnownHostsFile /dev/null\n%s",
		alias, c.hostBlock(), known, extra)
	if err := os.WriteFile(cfg, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	controlDir, err := os.MkdirTemp(it.keyDir, "cm")
	if err != nil {
		t.Fatal(err)
	}
	s := &session{c: c, log: remote.NewLog(nil)}
	s.opts = remote.Options{
		ClientID: clientID, Version: it.version, Commit: it.commit, Log: s.log, ControlDir: controlDir,
		ServerDir: c.serverDir(),
		Target: func(goos, goarch string) (remote.Target, error) {
			if goos+"/"+goarch != it.target {
				return remote.Target{}, fmt.Errorf("only %s is built for the tests", it.target)
			}
			return remote.Target{SHA256: it.pin, Size: int64(len(it.server)), Load: func() ([]byte, error) { return it.server, nil }}, nil
		},
	}
	remote.SetConfigFile(&s.opts, cfg)
	s.build = remote.BuildName(it.version, it.commit, it.pin)
	s.root = c.serverRoot()
	s.final = s.root + "/" + s.build + "/" + remote.BinaryName
	return s
}

func (s *session) dump(t *testing.T) {
	t.Helper()
	t.Logf("connection log:\n%s", strings.Join(s.log.Lines(), "\n"))
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// connected is a server that answered the handshake.
type connected struct {
	sess   *remote.Session
	client *rpc.Client
}

func (c connected) close() {
	_ = c.client.Close()
	_ = c.sess.Close()
}

func (s *session) connect(t *testing.T) connected {
	t.Helper()
	c, err := s.connectErr(testCtx(t))
	if err != nil {
		s.dump(t)
		t.Fatal(err)
	}
	return c
}

// connectErr dials and runs the handshake. It may run on any goroutine.
func (s *session) connectErr(ctx context.Context) (connected, error) {
	sess, err := remote.Dial(ctx, alias, s.opts)
	if err != nil {
		return connected{}, fmt.Errorf("Dial: %w", err)
	}
	conn := sess.Conn()
	client, err := rpc.Dial(conn, conn, rpc.ClientOptions{Version: it.version, Commit: it.commit, Log: s.log})
	if err != nil {
		_ = sess.Close()
		return connected{}, fmt.Errorf("handshake: %w", err)
	}
	if hello := client.Server(); hello.Version != it.version || hello.Commit != it.commit || hello.OS != it.goos {
		_ = client.Close()
		_ = sess.Close()
		return connected{}, fmt.Errorf("server hello = %+v", hello)
	}
	return connected{sess, client}, nil
}

func (s *session) dialErr(t *testing.T) error {
	t.Helper()
	sess, err := remote.Dial(testCtx(t), alias, s.opts)
	if err == nil {
		_ = sess.Close()
		t.Fatal("Dial succeeded")
	}
	return err
}

func codeOf(err error) remote.Code {
	if e, ok := errors.AsType[*remote.Error](err); ok {
		return e.Code
	}
	return ""
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitFor polls cond until it holds or d passes.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
	return true
}

func (s *session) logHas(sub string) bool {
	for _, l := range s.log.Lines() {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func TestInstallNoInternet(t *testing.T) {
	c := startContainer(t, ctOpts{})
	c.requireNoEgress()
	s := c.client(t, "client-a", true)
	conn := s.connect(t)
	defer conn.close()
	if !c.exists(s.final) {
		t.Fatal("no final binary after the install")
	}
	if b, d := c.mode(s.final), c.mode(filepath.Dir(s.final)); b != "700" || d != "700" {
		t.Errorf("modes of the binary and its folder = %s and %s, want 700 and 700", b, d)
	}
}

func TestRejectCorruptBinary(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.client(t, "client-a", true)
	before := c.runs()
	remote.SetUploadFilter(&s.opts, func(r io.Reader) io.Reader {
		data, _ := io.ReadAll(r)
		data = bytes.Clone(data)
		for i := len(data) / 3; i < len(data)/3+16; i++ {
			data[i] ^= 0x5a
		}
		return bytes.NewReader(data)
	})
	if err := s.dialErr(t); codeOf(err) != remote.CodeHashMismatch {
		s.dump(t)
		t.Fatalf("err = %v, want hash_mismatch", err)
	}
	if c.exists(s.final) {
		t.Error("a final binary exists")
	}
	if c.runs() != before {
		t.Error("the corrupt upload ran")
	}
}

func TestRejectWrongHash(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.client(t, "client-a", true)
	wrong := strings.Repeat("f", 64)
	before := c.runs()
	s.opts.Target = func(string, string) (remote.Target, error) {
		return remote.Target{SHA256: wrong, Size: int64(len(it.server)), Load: func() ([]byte, error) { return it.server, nil }}, nil
	}
	if err := s.dialErr(t); codeOf(err) != remote.CodeHashMismatch {
		s.dump(t)
		t.Fatalf("err = %v, want hash_mismatch", err)
	}
	if !s.logHas("install: uploaded file hash " + it.pin + ", pin " + wrong + ", result mismatch") {
		s.dump(t)
		t.Error("the mismatch was not caught at the install's hash check")
	}
	if n := c.runs() - before; n != 0 {
		t.Errorf("ran.log grew by %d lines: the trial run executed a file with the wrong hash", n)
	}
	build := remote.BuildName(it.version, it.commit, wrong)
	if c.exists(s.root + "/" + build + "/" + remote.BinaryName) {
		t.Error("a final binary exists")
	}
}

func TestRejectModifiedBetweenStarts(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.client(t, "client-a", true)
	s.connect(t).close()
	before := c.runs()
	c.sh("printf x >> " + s.final)
	if err := s.dialErr(t); codeOf(err) != remote.CodeHashMismatch {
		s.dump(t)
		t.Fatalf("err = %v, want hash_mismatch", err)
	}
	if after := c.runs(); after != before {
		t.Errorf("ran.log went from %d to %d lines: the modified binary ran", before, after)
	}
}

func TestPreviousVersionInstalled(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.client(t, "client-a", true)
	old := fmt.Sprintf("v0.0.1-%s-%s", strings.Repeat("0", 40), "deadbeef")
	oldDir := s.root + "/" + old
	c.sh("umask 077 && mkdir -p " + oldDir + " && printf old > " + oldDir + "/gitbox-server && touch " + oldDir + "/in-use.old-client")

	conn := s.connect(t)
	eventually(t, "the first collection", func() bool { return s.logHas("gc: keeping " + old + ", in use") })
	conn.close()
	if !c.exists(oldDir) {
		t.Fatal("the previous version with a live marker was collected")
	}

	c.age(oldDir+"/in-use.old-client", 11*time.Minute)
	conn = s.connect(t)
	defer conn.close()
	eventually(t, "the previous version to be collected", func() bool { return !c.exists(oldDir) })
	if !c.exists(s.final) {
		t.Error("the current version was collected")
	}
}

func TestConcurrentInstall(t *testing.T) {
	c := startContainer(t, ctOpts{})
	a, b := c.client(t, "client-a", true), c.client(t, "client-b", true)
	var wg sync.WaitGroup
	conns := make([]connected, 2)
	errs := make([]error, 2)
	ctx := testCtx(t)
	for i, s := range []*session{a, b} {
		wg.Go(func() { conns[i], errs[i] = s.connectErr(ctx) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			[]*session{a, b}[i].dump(t)
			t.Fatalf("client %d: %v", i, err)
		}
	}
	dir := filepath.Dir(a.final)
	if got := c.sh("ls " + dir + "/gitbox-server | wc -l"); got != "1" {
		t.Errorf("%s final binaries", got)
	}
	if got := c.sha256(a.final); got != it.pin {
		t.Errorf("final hash %s, want %s", got, it.pin)
	}
	if got := c.sh("ls -A " + dir + " | sort | tr '\\n' ' '"); got != "gitbox-server in-use.client-a in-use.client-b" {
		t.Errorf("build folder holds %q: want one binary, two markers and no temporary file", got)
	}
	conns[0].close()
	eventually(t, "client-a's marker to go", func() bool { return !c.exists(dir + "/in-use.client-a") })
	if !c.exists(dir + "/in-use.client-b") {
		t.Error("client-b's marker went with client-a's server")
	}
	// Another start collects garbage and keeps the version in use.
	third := c.client(t, "client-c", true)
	conn := third.connect(t)
	eventually(t, "the third collection", func() bool { return third.logHas("gitbox-server: serving client client-c") })
	time.Sleep(time.Second)
	conn.close()
	conns[1].close()
	if !c.exists(a.final) {
		t.Error("the version in use was collected")
	}
}

func TestInterruptedUpload(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.client(t, "client-a", true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	remote.SetUploadFilter(&s.opts, func(r io.Reader) io.Reader {
		return &killAt{r: r, at: len(it.server) / 2, kill: cancel}
	})
	if _, err := remote.Dial(ctx, alias, s.opts); err == nil {
		t.Fatal("the interrupted install succeeded")
	}
	dir := filepath.Dir(s.final)
	tmps := c.sh("ls -A " + dir + " 2>/dev/null | grep '^.gitbox-server\\..*\\.tmp$' || true")
	if tmps == "" || strings.Contains(tmps, "\n") {
		s.dump(t)
		t.Fatalf("temporary files after the kill: %q, want exactly one", tmps)
	}
	if c.exists(s.final) {
		t.Fatal("a final binary exists after the interrupted upload")
	}
	c.age(dir+"/"+tmps, 2*time.Hour)

	s2 := c.client(t, "client-a", true)
	conn := s2.connect(t)
	defer conn.close()
	eventually(t, "the abandoned upload to be removed", func() bool { return !c.exists(dir + "/" + tmps) })
}

// killAt passes the upload through until at bytes went by, then kills the
// ssh process through the context, as a dropped connection would.
type killAt struct {
	r    io.Reader
	at   int
	n    int
	kill func()
	done bool
}

func (k *killAt) Read(p []byte) (int, error) {
	if k.done {
		time.Sleep(time.Second)
		return 0, io.ErrUnexpectedEOF
	}
	if k.n+len(p) > k.at {
		p = p[:k.at-k.n]
	}
	n, err := k.r.Read(p)
	k.n += n
	if k.n >= k.at {
		k.done = true
		go func() {
			// Let the first half reach the host before the kill.
			time.Sleep(time.Second)
			k.kill()
		}()
	}
	return n, err
}

func TestNoexecHome(t *testing.T) {
	c := startContainer(t, ctOpts{noexecHome: true})
	diff := func() string {
		lines := strings.Split(docker(t, "diff", c.id), "\n")
		slices.Sort(lines)
		return strings.Join(lines, "\n")
	}
	before := diff()
	s := c.client(t, "client-a", true)
	err := s.dialErr(t)
	if codeOf(err) != remote.CodeNoexec || !strings.Contains(err.Error(), "server_dir") {
		s.dump(t)
		t.Fatalf("err = %v, want noexec with the server_dir hint", err)
	}
	if after := diff(); after != before {
		t.Errorf("files changed outside the home:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := c.sh("ls -A /tmp"); got != "" {
		t.Errorf("/tmp holds %q", got)
	}
	if c.exists(s.final) {
		t.Error("a final binary exists on a noexec home")
	}
}

func TestNoisyBashrc(t *testing.T) {
	bashrc := "head -c 65536 /dev/zero | tr '\\0' o\nhead -c 65536 /dev/zero | tr '\\0' e >&2\n"
	c := startContainer(t, ctOpts{bashrc: bashrc})
	s := c.client(t, "client-a", true)
	conn := s.connect(t)
	defer conn.close()
	if !s.logHas("[banner: 65536 bytes]") {
		s.dump(t)
		t.Error("the banner was not measured")
	}
	if !s.logHas("install: uploaded file hash " + it.pin + ", pin " + it.pin + ", result ok") {
		s.dump(t)
		t.Error("the install's hash check read the wrong value")
	}
	if !s.logHas("start: hash " + it.pin + " matches the pin") {
		s.dump(t)
		t.Error("the start's hash check read the wrong value")
	}
}

func TestBannerOverflow(t *testing.T) {
	c := startContainer(t, ctOpts{bashrc: "head -c 1048577 /dev/zero | tr '\\0' b\n"})
	s := c.client(t, "client-a", true)
	if err := s.dialErr(t); codeOf(err) != remote.CodeBannerOverflow {
		s.dump(t)
		t.Fatalf("err = %v, want banner_overflow", err)
	}
}

func TestStaleGCLock(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.client(t, "client-a", true)
	lock := s.root + "/.gc.lock"
	c.sh("umask 077 && mkdir -p " + lock + " && echo '{}' > " + lock + "/owner")
	c.age(lock, 10*time.Minute)
	conn := s.connect(t)
	defer conn.close()
	eventually(t, "the stale lock to be broken", func() bool { return s.logHas("gc: breaking a stale lock") })
	eventually(t, "the lock to be released", func() bool { return !c.exists(lock) })
	logFile := s.root + "/logs/" + s.build + ".log"
	if got := c.sh("cat " + logFile); !strings.Contains(got, "gc: breaking a stale lock") {
		t.Errorf("the server log does not record the broken lock:\n%s", got)
	}
}

func TestUnknownHostFailsFast(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.client(t, "client-a", false)
	t0 := time.Now()
	err := s.dialErr(t)
	if codeOf(err) != remote.CodeSSHHostKey {
		s.dump(t)
		t.Fatalf("err = %v, want ssh_hostkey", err)
	}
	if d := time.Since(t0); d > 12*time.Second {
		t.Errorf("took %s, more than ConnectTimeout plus 2 s", d)
	}
	if c.exists(s.root) {
		t.Error("something was installed on a host whose key is unknown")
	}
}

func TestFishLoginShell(t *testing.T) {
	c := startContainer(t, ctOpts{shell: "/usr/bin/fish"})
	if got := c.sh("getent passwd test | cut -d: -f7"); got != "/usr/bin/fish" {
		t.Fatalf("login shell is %q", got)
	}
	s := c.client(t, "client-a", true)
	conn := s.connect(t)
	defer conn.close()
	if !s.logHas("shell /usr/bin/fish") {
		s.dump(t)
		t.Error("the probe did not report fish")
	}
}

// TestRemoteTerminalOverSSH runs the remote command of a terminal (#243)
// through a real ssh into bash and fish login shells, the way ssh -t hands
// it to them: for each hostile folder, the external pwd after the cd prints
// exactly the folder's bytes, the login-shell form lands there too, and no
// injected command leaves a marker M behind. A path with CR or LF never gets this far
// (TestRemoteTerminalArgv).
func TestRemoteTerminalOverSSH(t *testing.T) {
	names := []string{
		"it's here", "$(id)", "`id`", "a;rm -rf ~", "a&&b", "%PATH%", "$env:HOME", `a b\c`,
		`back\\slash\'quote`, "$(touch M)", "`touch M`", "a;touch M", "a&&touch M", "a|touch M",
		"x'; touch M; '", "a & echo x > M & b", "ünïcödé ✓", "-dash",
	}
	for _, tc := range terminalShells(t) {
		t.Run(filepath.Base(tc.shell)+"-"+string(tc.family), func(t *testing.T) {
			c := startContainer(t, ctOpts{shell: tc.shell})
			s := c.client(t, "client-a", true)
			ssh, err := remote.SSHPath(s.opts)
			if err != nil {
				t.Fatal(err)
			}
			base := c.home() + "/hostile"
			for _, name := range names {
				dir := base + "/" + name
				c.sh("mkdir -p -- " + q(dir))
				for _, program := range [][]string{{"pwd"}, nil} {
					cmdline, err := remote.RemoteCommand(tc.family, dir, program)
					if err != nil {
						t.Fatal(err)
					}
					argv, err := remote.TerminalArgv(ssh, alias, cmdline)
					if err != nil {
						t.Fatal(err)
					}
					// The production argv is ssh -t -- <alias> <command>;
					// the test adds its config file and, with no terminal
					// here, -T instead of -t. The command is the same word.
					args := append(remote.ConfigArgs(s.opts), "-T", "-o", "BatchMode=yes", "--", argv[3], argv[4])
					cmd := exec.Command(ssh, args...)
					// The login-shell form reads its commands from stdin. The
					// external /bin/pwd prints getcwd() byte for byte; fish's
					// builtin pwd renders a non-ASCII folder in the locale,
					// and the container has none (C), so it would print
					// mojibake for the right folder.
					cmd.Stdin = strings.NewReader("/bin/pwd\nexit\n")
					out, err := cmd.Output()
					if got := strings.TrimSpace(string(out)); err != nil || got != dir {
						t.Errorf("%q (program %q):\n command %s\n printed %q, %v", name, program, cmdline, got, err)
					}
				}
			}
			// An injected touch M runs in the folder or, if the cd failed,
			// in the home.
			if found := c.sh("find " + q(base) + " -name M; find " + q(c.home()) + " -maxdepth 1 -name M"); found != "" {
				t.Errorf("an injected command ran:\n%s", found)
			}
		})
	}
}

// readOnlyCalls are the methods a gitbox-server serves to a read-only
// client (#240), with arguments for the host that
// TestReadOnlySessionLeavesConfigUntouched seeds. TestIntegrationCallsEveryServedMethod
// in cmd/gui keeps the list equal to servedMethods.
var readOnlyCalls = []struct {
	method string
	params []any
}{
	{"AccountDeletionImpact", []any{"acct"}},
	{"AccountFolder", []any{"acct"}},
	{"BeginStatus", []any{1}},
	{"CheckGlobalGCMConfig", []any{}},
	{"CheckGlobalGitignore", []any{}},
	{"CheckGlobalIdentity", []any{}},
	{"CheckMirrorCredentials", []any{"acct"}},
	{"CheckMoveReadiness", []any{"acct", "acct", false}},
	{"CredentialSSHAgentReport", []any{"acct"}},
	{"CredentialVerify", []any{"acct"}},
	{"Discover", []any{"acct"}},
	{"DiscoverMirrors", []any{}},
	{"DoctorPrecheck", []any{"token"}},
	{"DoctorRun", []any{}},
	{"FetchAllRepos", []any{}},
	{"FetchRepo", []any{"acct", "org/repo"}},
	{"GetAllStatus", []any{}},
	{"GetCheckGlobalGitignore", []any{}},
	{"GetCollapsed", []any{}},
	{"GetConfig", []any{}},
	{"GetConfigLoadError", []any{}},
	{"GetConfigPath", []any{}},
	{"GetMirrorStatus", []any{"none"}},
	{"GetNestedScanDepth", []any{}},
	{"GetOS", []any{}},
	{"GetPRSettings", []any{}},
	{"GetPRsForRepo", []any{"acct", "org/repo"}},
	{"GetRepoDetail", []any{"acct", "org/repo"}},
	{"GetStartupSnapshot", []any{}},
	{"GetTokenGuide", []any{"acct"}},
	{"IsFirstRun", []any{}},
	{"IsGlobalGCMConfigNeeded", []any{}},
	{"ListAccountOrgs", []any{"acct"}},
	{"ListConfigBackups", []any{}},
	{"ListMoveDestinationOwners", []any{"acct"}},
	{"ListRemoteRepos", []any{"acct"}},
	{"ListRoots", []any{}},
	{"OpenAccountInBrowser", []any{"acct"}},
	{"OpenRepoInBrowser", []any{"acct", "org/repo"}},
	{"PreflightMove", []any{map[string]any{"sourceSourceKey": "acct", "sourceRepoKey": "org/repo", "destAccountKey": "acct", "destOwner": "alice", "destRepoName": "copy"}}},
	{"PreviewClonePath", []any{"acct", "org/first"}},
	{"PreviewRoot", []any{testHome + "/git"}},
	{"PreviewSweep", []any{"acct", "org/repo"}},
	{"RecommendedGlobalGitignoreBody", []any{}},
	{"RefreshAIHarnesses", []any{}},
	{"RefreshAllPRs", []any{}},
	{"RefreshStatus", []any{}},
	{"ReloadConfig", []any{}},
	{"RepoNesting", []any{}},
	{"RepoWorkspaceFiles", []any{"acct", "org/repo"}},
	{"RunFleetCheck", []any{"mirror-status"}},
	{"ScanFolderForClones", []any{testHome + "/git"}},
	{"SuggestAccountKey", []any{"gitea", "alice"}},
	{"SyncAIHarnesses", []any{}},
	{"ValidateRemotePath", []any{"~"}},
}

// readOnlyHostConfig is the host's gitbox.json: one account on a server
// the container cannot reach, one clone, and an AI harness that detection
// will mark missing, so a background save would change the file.
const readOnlyHostConfig = `{
  "version": 5,
  "global": {
    "roots": ["` + testHome + `/git"],
    "theme": "dark",
    "ai_harnesses": [{"name": "Seeded bot", "command": "/opt/seeded-bot", "source": "user"}]
  },
  "accounts": {
    "acct": {"provider": "gitea", "url": "https://git.example.invalid", "username": "alice", "name": "Alice", "email": "a@example.invalid", "default_credential_type": "token"}
  },
  "sources": {
    "acct": {"account": "acct", "repos": {"org/repo": {}}}
  }
}
`

// The reads change nothing on the host (#240): after every served read, a
// fetch-all and the harness watcher's first pass, the host's gitbox.json
// holds the same domain (the watcher may save what it detected, #294), the
// clone's .git/config is untouched (#295 serves its repair), and a
// repository mutation is refused by the server itself.
func TestReadsLeaveTheDomainUntouched(t *testing.T) {
	c := startContainer(t, ctOpts{})
	cfgDir := testHome + "/.config/gitbox"
	cfgPath := cfgDir + "/gitbox.json"
	clone := testHome + "/git/acct/org/repo"
	c.sh("mkdir -p " + cfgDir + " && echo " + base64.StdEncoding.EncodeToString([]byte(readOnlyHostConfig)) + " | base64 -d > " + cfgPath)
	c.sh("git init -q --bare " + testHome + "/origin.git" +
		" && git init -q -b main " + clone +
		" && git -C " + clone + " -c user.name=T -c user.email=t@e commit -q --allow-empty -m first" +
		" && git -C " + clone + " remote add origin " + testHome + "/origin.git" +
		" && git -C " + clone + " push -q origin main")
	gitConfig := func() string { return c.sh("sha256sum " + clone + "/.git/config | cut -d' ' -f1") }
	domain := func() map[string]any {
		var m map[string]any
		if err := json.Unmarshal([]byte(c.sh("cat "+cfgPath)), &m); err != nil {
			t.Fatalf("the host's gitbox.json does not parse: %v", err)
		}
		if g, ok := m["global"].(map[string]any); ok {
			delete(g, "ai_harnesses")
		}
		return m
	}
	before, cloneBefore := domain(), gitConfig()

	s := c.client(t, "client-a", true)
	sess, err := remote.Dial(testCtx(t), alias, s.opts)
	if err != nil {
		s.dump(t)
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []string
	conn := sess.Conn()
	client, err := rpc.Dial(conn, conn, rpc.ClientOptions{
		Version: it.version, Commit: it.commit, Log: s.log,
		OnEvent: func(name string, _ []json.RawMessage) {
			mu.Lock()
			events = append(events, name)
			mu.Unlock()
		},
	})
	if err != nil {
		_ = sess.Close()
		t.Fatal(err)
	}
	seen := func(name string) bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(events, name)
	}

	for _, call := range readOnlyCalls {
		ctx, cancel := context.WithTimeout(testCtx(t), 90*time.Second)
		var out json.RawMessage
		if err := client.Call(ctx, call.method, call.params, &out); err != nil && rpc.IsCode(err, rpc.ErrCodeUnsupported) {
			t.Errorf("%s is not served", call.method)
		}
		cancel()
	}
	// A method outside the closed list is refused by the server itself,
	// not only by the client: the account launchers run on the client.
	ctx, cancel := context.WithTimeout(testCtx(t), 30*time.Second)
	if err := client.Call(ctx, "OpenAccountFolder", []any{"acct"}, nil); !rpc.IsCode(err, rpc.ErrCodeUnsupported) {
		t.Errorf("OpenAccountFolder = %v, want unsupported", err)
	}
	cancel()

	// Fetch-all ran to its end, and the session stays open past the
	// harness watcher's first pass (about 2 s after the server starts),
	// which reports the seeded harness missing.
	eventually(t, "fetch:alldone", func() bool { return seen("fetch:alldone") })
	eventually(t, "harnesses:updated from the watcher", func() bool { return seen("harnesses:updated") })
	time.Sleep(3 * time.Second)
	_ = client.Close()
	_ = sess.Close()

	if after := domain(); !reflect.DeepEqual(after, before) {
		s.dump(t)
		t.Errorf("the reads changed the host's domain:\nbefore %v\nafter  %v", before, after)
	}
	// Since #295 a fetch on the host repairs the clone's .git/config first,
	// as on a desktop: only the domain must stay as it was.
	if after := gitConfig(); after == cloneBefore {
		t.Errorf("the clone's .git/config is unchanged: fetch did not repair it")
	}
	if !c.exists(clone) {
		t.Error("a read removed the clone")
	}
}

// mutationCalls are the mutations a gitbox-server serves (#294, #295, #242), in
// an order where each one finds what it needs, against the host that
// TestMutationsOverSSH seeds. TestIntegrationCallsEveryServedMethod in
// cmd/gui keeps the set equal to the served mutations. RestoreFromBackup
// gets the path of a backup the earlier saves made.
var mutationCalls = []struct {
	method string
	params []any
	// mayFail: the host has no provider to reach, so the call fails with
	// the domain's error; it must still be served.
	mayFail bool
}{
	{"AdoptFirstProject", []any{"acct", "org/first", testHome + "/git/picked"}, false},
	{"AddRoot", []any{testHome + "/git2"}, false},
	{"SetDefaultRoot", []any{testHome + "/git2"}, false},
	{"SetDefaultRoot", []any{testHome + "/git"}, false},
	{"RemoveRoot", []any{testHome + "/git2", false}, false},
	{"InitRoot", []any{testHome + "/git"}, false},
	{"SetNestedScanDepth", []any{3}, false},
	{"SetPRBadgesEnabled", []any{false}, false},
	{"SetPRIncludeDrafts", []any{true}, false},
	{"SetCheckGlobalGitignore", []any{false}, false},
	{"AddAccount", []any{map[string]any{"key": "other", "provider": "gitea", "url": "https://git2.example.invalid", "username": "bob", "name": "Bob", "email": "b@example.invalid", "credentialType": "token"}}, false},
	{"UpdateAccount", []any{map[string]any{"key": "other", "provider": "gitea", "url": "https://git2.example.invalid", "username": "bob", "name": "Bob B", "email": "b@example.invalid"}}, false},
	{"RenameAccount", []any{"other", "other2"}, false},
	{"AddMirrorGroup", []any{"m", "acct", "other2"}, false},
	{"AddMirrorRepo", []any{"m", "org/repo", "push", "src"}, false},
	{"DeleteMirrorRepo", []any{"m", "org/repo"}, false},
	{"DeleteMirrorGroup", []any{"m"}, false},
	{"DiscardAccount", []any{"other2"}, false},
	{"AddAccount", []any{map[string]any{"key": "third", "provider": "gitea", "url": "https://git3.example.invalid", "username": "carol", "name": "Carol", "email": "c@example.invalid", "credentialType": "token"}}, false},
	{"DeleteAccount", []any{"third"}, false},
	{"AddDiscoveredRepos", []any{"acct", []string{"org/new"}}, false},
	{"AddDiscoveredReposToFolder", []any{"acct", []string{"org/new2"}, testHome + "/custom"}, false},
	{"SetRepoContainer", []any{"acct", "org/repo", true}, false},
	{"SetRepoWorkspace", []any{"acct", "org/repo", ""}, false},
	{"IgnoreClone", []any{testHome + "/git/stray", true}, false},
	{"RemoveGlobalIdentity", []any{}, false},
	{"FixGlobalGCMConfig", []any{}, false},
	{"InstallGlobalGitignore", []any{}, false},
	{"AcknowledgeConfigError", []any{}, false},
	{"RepairConfig", []any{}, false},
	{"RestoreFromBackup", []any{"<backup>"}, false},
	// The repository mutations (#295). The host's provider is unreachable:
	// the long ones end in their done event with that error and answer.
	{"CloneRepo", []any{"acct", "org/none", ""}, false},
	{"PullRepo", []any{"acct", "org/repo"}, false},
	{"ConfirmSweep", []any{"acct", "org/repo", map[string]any{}}, false},
	{"Reconcile", []any{}, false},
	{"AdoptClones", []any{[]any{}}, false},
	{"SetupMirrorRepo", []any{"none", "org/repo"}, false},
	{"ApplyDiscoveredMirrors", []any{}, true},
	{"CreateNewRepo", []any{"acct", "", "made", "", true, false, ""}, true},
	{"MoveRepo", []any{map[string]any{"sourceSourceKey": "acct", "sourceRepoKey": "org/repo", "destAccountKey": "nobody", "destOwner": "alice", "destRepoName": "moved"}}, false},
	{"DeleteRepo", []any{"acct", "org/repo"}, false},
	// The token credentials (#242). GCM is served only to be refused.
	{"CredentialStoreToken", []any{"acct", "it-token"}, false},
	{"ChangeCredentialType", []any{"acct", "token"}, false},
	{"CredentialSetupGCM", []any{"acct"}, true},
	// The SSH keys (#244): the key is made on the host; the provider is
	// unreachable, so the connection is not verified.
	{"CredentialSetupSSH", []any{"acct"}, false},
	{"CredentialRegenerateSSH", []any{"acct"}, false},
	{"CredentialDelete", []any{"acct"}, false},
}

// TestMutationsOverSSH runs every served config mutation once over a real
// ssh session (#294). Each succeeds, the host's gitbox.json parses after
// every step and keeps the host's own theme, and the end state holds what
// the sequence left.
func TestMutationsOverSSH(t *testing.T) {
	c := startContainer(t, ctOpts{})
	cfgPath := seedMutationHost(c)
	picked := testHome + "/git/picked"
	c.sh("git init -q " + picked + " && git -C " + picked + " remote add origin https://git.example.invalid/org/first.git")
	cfgDir := filepath.Dir(cfgPath)
	conn := c.client(t, "client-m", true).connect(t)
	defer conn.close()

	host := func() map[string]any {
		var m map[string]any
		if err := json.Unmarshal([]byte(c.sh("cat "+cfgPath)), &m); err != nil {
			t.Fatalf("the host's gitbox.json does not parse: %v", err)
		}
		return m
	}
	for i, call := range mutationCalls {
		params := call.params
		if call.method == "RestoreFromBackup" {
			params = []any{c.sh("ls -1t " + cfgDir + "/gitbox-*.json | head -1")}
		}
		ctx, cancel := context.WithTimeout(testCtx(t), 60*time.Second)
		var out json.RawMessage
		err := conn.client.Call(ctx, call.method, params, &out, rpc.WithRequestID(fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)))
		cancel()
		if err != nil && (!call.mayFail || rpc.IsCode(err, rpc.ErrCodeUnsupported)) {
			t.Fatalf("step %d, %s: %v", i+1, call.method, err)
		}
		if g, _ := host()["global"].(map[string]any); g["theme"] != "dark" {
			t.Errorf("step %d, %s: the host's theme is %v, want its own dark", i+1, call.method, g["theme"])
		}
	}
	end := host()
	g, _ := end["global"].(map[string]any)
	accounts, _ := end["accounts"].(map[string]any)
	if g["nested_scan_depth"] != float64(3) || len(accounts) != 1 {
		t.Errorf("end state: depth %v, accounts %v", g["nested_scan_depth"], accounts)
	}
	if !c.exists(testHome + "/.gitignore_global") {
		t.Error("InstallGlobalGitignore wrote no ~/.gitignore_global")
	}
}

// hostAccounts reads the account keys of the host's gitbox.json; the file
// must parse.
func hostAccounts(t *testing.T, c *container, cfgPath string) []string {
	t.Helper()
	var m struct {
		Accounts map[string]json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(c.sh("cat "+cfgPath)), &m); err != nil {
		t.Fatalf("the host's gitbox.json does not parse: %v", err)
	}
	keys := make([]string, 0, len(m.Accounts))
	for k := range m.Accounts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// seedMutationHost writes the host config TestMutationsOverSSH uses and
// returns its path.
func seedMutationHost(c *container) string {
	cfgDir := testHome + "/.config/gitbox"
	cfgPath := cfgDir + "/gitbox.json"
	c.sh("mkdir -p " + cfgDir + " " + testHome + "/git/acct/org/repo && echo " + base64.StdEncoding.EncodeToString([]byte(readOnlyHostConfig)) + " | base64 -d > " + cfgPath)
	return cfgPath
}

func addAccountParams(key string) []any {
	return []any{map[string]any{"key": key, "provider": "gitea", "url": "https://" + key + ".example.invalid", "username": key, "name": key, "email": key + "@example.invalid", "credentialType": "token"}}
}

// TestCutSSHMidSave kills the client's ssh while AddAccount is in flight,
// 20 times at different delays (#294). The host's gitbox.json always
// parses and holds either the accounts it had or those plus the new one:
// a save is old or new, never torn. The lock never outlives the server.
func TestCutSSHMidSave(t *testing.T) {
	c := startContainer(t, ctOpts{})
	cfgPath := seedMutationHost(c)
	s := c.client(t, "client-cut", true)
	for i := range 20 {
		before := hostAccounts(t, c, cfgPath)
		key := fmt.Sprintf("cut%02d", i)
		conn := s.connect(t)
		done := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(testCtx(t), 30*time.Second)
			defer cancel()
			done <- conn.client.Call(ctx, "AddAccount", addAccountParams(key), nil, rpc.WithRequestID(fmt.Sprintf("00000000-0000-4000-8000-%012d", 100+i)))
		}()
		time.Sleep(time.Duration((i*7)%45) * time.Millisecond)
		remote.KillSession(conn.sess)
		<-done
		conn.close()
		if !waitFor(20*time.Second, func() bool { return !c.exists(cfgPath + ".lock") }) {
			t.Fatalf("cut %d: the config lock outlived the cut\nprocesses:\n%s\nserver log:\n%s", i, c.sh("ps -eo pid,stat,args | grep '[g]itbox' || true"), c.sh("tail -n 40 "+s.root+"/logs/"+s.build+".log 2>/dev/null || true"))
		}
		after := hostAccounts(t, c, cfgPath)
		withNew := slices.Sorted(slices.Values(append(slices.Clone(before), key)))
		if !slices.Equal(after, before) && !slices.Equal(after, withNew) {
			t.Fatalf("cut %d: accounts %v, want %v or %v", i, after, before, withNew)
		}
	}
}

// TestConfigChangedConflictOverSSH: two clients, so two servers, read the
// same host config; one adds an account, then the other's add is refused
// with config.changed, and its retry after the reload keeps both (#294).
func TestConfigChangedConflictOverSSH(t *testing.T) {
	c := startContainer(t, ctOpts{})
	cfgPath := seedMutationHost(c)
	a := c.client(t, "client-a", true).connect(t)
	defer a.close()
	b := c.client(t, "client-b", true).connect(t)
	defer b.close()
	call := func(conn connected, id int, params []any) error {
		ctx, cancel := context.WithTimeout(testCtx(t), 30*time.Second)
		defer cancel()
		return conn.client.Call(ctx, "AddAccount", params, nil, rpc.WithRequestID(fmt.Sprintf("00000000-0000-4000-8000-%012d", id)))
	}
	if err := call(a, 201, addAccountParams("froma")); err != nil {
		t.Fatalf("the first client's add = %v", err)
	}
	err := call(b, 202, addAccountParams("fromb"))
	if e, ok := errors.AsType[*rpc.Error](err); !ok || e.Data.Code != "config.changed" {
		t.Fatalf("the second client's add after the first = %v, want config.changed", err)
	}
	if err := call(b, 203, addAccountParams("fromb")); err != nil {
		t.Fatalf("the retry after the reload = %v", err)
	}
	if got := hostAccounts(t, c, cfgPath); !slices.Contains(got, "froma") || !slices.Contains(got, "fromb") {
		t.Errorf("accounts %v, want both adds kept", got)
	}
}

// cloneHostConfig is the host's gitbox.json of the fault tests (#295): one
// account whose server is the container's git daemon, so the reachability
// check connects, and the fixture repository, not cloned yet. The account
// uses a GCM HTTP origin. The fixture serves that repository without
// authentication through git daemon on the same host and port.
const cloneHostConfig = `{
  "version": 5,
  "global": {"roots": ["` + testHome + `/git"], "theme": "dark"},
  "accounts": {
    "acct": {"provider": "gitea", "url": "http://127.0.0.1:9418", "username": "alice", "name": "Alice", "email": "a@example.invalid", "default_credential_type": "gcm"}
  },
  "sources": {
    "acct": {"account": "acct", "repos": {"org/fixture": {}}}
  }
}
`

// fixtureClone is where the fixture clones on the host.
const fixtureClone = testHome + "/git/acct/org/fixture"

// seedCloneHost writes cloneHostConfig and the rewrite of the fixture's
// origin, and returns the config's path.
func seedCloneHost(c *container) string {
	cfgDir := testHome + "/.config/gitbox"
	cfgPath := cfgDir + "/gitbox.json"
	c.sh("mkdir -p " + cfgDir + " " + testHome + "/git && echo " + base64.StdEncoding.EncodeToString([]byte(cloneHostConfig)) + " | base64 -d > " + cfgPath +
		" && git config --global url.git://127.0.0.1:9418/org/fixture.git.insteadOf http://alice@127.0.0.1:9418/org/fixture.git")
	return cfgPath
}

// progressEvent is one clone:progress as the client received it.
type progressEvent struct {
	at      time.Time
	phase   string
	percent int
}

// eventLog records the events a client receives.
type eventLog struct {
	mu       sync.Mutex
	names    []string
	payloads map[string][]json.RawMessage
	progress []progressEvent
	onEvent  func(name string, payload json.RawMessage)
}

func (l *eventLog) record(name string, payload []json.RawMessage) {
	l.mu.Lock()
	l.names = append(l.names, name)
	if l.payloads == nil {
		l.payloads = map[string][]json.RawMessage{}
	}
	var raw json.RawMessage
	if len(payload) == 1 {
		var envelope struct {
			Schema  int             `json:"schema"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(payload[0], &envelope) == nil && envelope.Schema == 1 {
			raw = envelope.Payload
		}
	}
	l.payloads[name] = append(l.payloads[name], raw)
	if name == "clone:progress" {
		var p struct {
			Phase   string `json:"phase"`
			Percent int    `json:"percent"`
		}
		_ = json.Unmarshal(raw, &p)
		l.progress = append(l.progress, progressEvent{time.Now(), p.Phase, p.Percent})
	}
	hook := l.onEvent
	l.mu.Unlock()
	if hook != nil {
		hook(name, raw)
	}
}

// last is the latest payload of name, or nil.
func (l *eventLog) last(name string) json.RawMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	if p := l.payloads[name]; len(p) > 0 {
		return p[len(p)-1]
	}
	return nil
}

// connectEvents is connect with the server's events recorded in log.
func (s *session) connectEvents(t *testing.T, log *eventLog) connected {
	t.Helper()
	sess, err := remote.Dial(testCtx(t), alias, s.opts)
	if err != nil {
		s.dump(t)
		t.Fatal(err)
	}
	conn := sess.Conn()
	client, err := rpc.Dial(conn, conn, rpc.ClientOptions{Version: it.version, Commit: it.commit, Log: s.log, OnEvent: log.record})
	if err != nil {
		_ = sess.Close()
		t.Fatal(err)
	}
	return connected{sess, client}
}

// gitProcesses lists the test user's git processes on the host.
func (c *container) gitProcesses() string {
	return strings.TrimSpace(c.sh("pgrep -a -u test -x git || true"))
}

// doneError is the error field of a done event's payload.
func doneError(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var p struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("a done event does not decode: %v (%s)", err, raw)
	}
	return p.Error
}

// TestCutSSHMidClone kills the client's ssh once a clone of the 60 MB
// fixture is past 30 % (#295). The server stops git, which removes its
// folder, so after a reconnect the host's gitbox.json is the one before the
// clone, no git process is left, the folder is gone or Reconcile reports it
// incomplete (never adopted), and the same clone then succeeds.
func TestCutSSHMidClone(t *testing.T) {
	c := startContainer(t, ctOpts{netAdmin: true, gitDaemon: true})
	cfgPath := seedCloneHost(c)
	// The daemon serves on the loopback: slowing it keeps the clone running
	// for several seconds.
	docker(t, "exec", c.id, "tc", "qdisc", "add", "dev", "lo", "root", "netem", "rate", "40mbit")
	before := c.sh("sha256sum " + cfgPath)
	s := c.client(t, "client-cut-clone", true)

	log := &eventLog{}
	cut := make(chan struct{})
	var once sync.Once
	log.onEvent = func(name string, raw json.RawMessage) {
		var p struct {
			Phase   string `json:"phase"`
			Percent int    `json:"percent"`
		}
		if name == "clone:progress" && json.Unmarshal(raw, &p) == nil && strings.HasPrefix(p.Phase, "Receiving") && p.Percent > 30 {
			once.Do(func() { close(cut) })
		}
	}
	conn := s.connectEvents(t, log)
	done := make(chan error, 1)
	go func() {
		done <- conn.client.Call(testCtx(t), "CloneRepo", []any{"acct", "org/fixture", ""}, nil, rpc.WithRequestID("00000000-0000-4000-8000-000000000301"))
	}()
	select {
	case <-cut:
	case err := <-done:
		t.Fatalf("the clone ended before it passed 30 %%: %v; progress %+v", err, log.progress)
	case <-time.After(90 * time.Second):
		t.Fatalf("the clone never passed 30 %%; progress %+v", log.progress)
	}
	remote.KillSession(conn.sess)
	<-done
	conn.close()

	if !waitFor(20*time.Second, func() bool { return c.gitProcesses() == "" }) {
		t.Fatalf("git still runs on the host after the cut:\n%s", c.gitProcesses())
	}

	// Reconnect, as the window does, and look at the host.
	log2 := &eventLog{}
	conn2 := s.connectEvents(t, log2)
	defer conn2.close()
	if after := c.sh("sha256sum " + cfgPath); after != before {
		t.Errorf("the host's gitbox.json changed with the cut clone:\n%s", c.sh("cat "+cfgPath))
	}
	var pass struct {
		Adopted    int `json:"adopted"`
		Incomplete []struct {
			Path string `json:"path"`
		} `json:"incomplete"`
	}
	if err := conn2.client.Call(testCtx(t), "Reconcile", nil, &pass, rpc.WithRequestID("00000000-0000-4000-8000-000000000302")); err != nil {
		t.Fatal(err)
	}
	if pass.Adopted != 0 {
		t.Errorf("Reconcile adopted %d clones after the cut", pass.Adopted)
	}
	if c.exists(fixtureClone) {
		if len(pass.Incomplete) != 1 || pass.Incomplete[0].Path != fixtureClone {
			t.Fatalf("the cut clone's folder is still there and Reconcile does not report it: %+v", pass)
		}
		// I delete what the cut left, as the window asks me to.
		c.sh("rm -rf " + fixtureClone)
		t.Log("the cut left an incomplete clone, and Reconcile reported it")
	}
	if after := c.sh("sha256sum " + cfgPath); after != before {
		t.Errorf("the host's gitbox.json changed after the reconcile:\n%s", c.sh("cat "+cfgPath))
	}

	if err := conn2.client.Call(testCtx(t), "CloneRepo", []any{"acct", "org/fixture", ""}, nil, rpc.WithRequestID("00000000-0000-4000-8000-000000000303")); err != nil {
		t.Fatalf("the repeated clone = %v", err)
	}
	if msg := doneError(t, log2.last("clone:done")); msg != "" {
		t.Fatalf("the repeated clone failed: %s", msg)
	}
	c.sh("git -C " + fixtureClone + " rev-parse --verify -q HEAD") // fails the test without a commit
}

// TestLatency300ms adds 300 ms to every round trip between client and
// host (#295): clone, pull and fetch-all complete without timing out, and
// the clone's progress arrives coalesced, one event per rpc.ProgressInterval
// at most.
func TestLatency300ms(t *testing.T) {
	c := startContainer(t, ctOpts{netAdmin: true, gitDaemon: true})
	seedCloneHost(c)
	docker(t, "exec", c.id, "tc", "qdisc", "add", "dev", "eth0", "root", "netem", "delay", "300ms")
	s := c.client(t, "client-latency", true)
	log := &eventLog{}
	conn := s.connectEvents(t, log)
	defer conn.close()

	call := func(id int, method string, params []any) time.Duration {
		t.Helper()
		started := time.Now()
		if err := conn.client.Call(testCtx(t), method, params, nil, rpc.WithRequestID(fmt.Sprintf("00000000-0000-4000-8000-%012d", 400+id))); err != nil {
			t.Fatalf("%s over 300 ms = %v", method, err)
		}
		return time.Since(started)
	}
	took := call(1, "CloneRepo", []any{"acct", "org/fixture", ""})
	if msg := doneError(t, log.last("clone:done")); msg != "" {
		t.Fatalf("the clone failed: %s", msg)
	}
	call(2, "PullRepo", []any{"acct", "org/fixture"})
	if msg := doneError(t, log.last("pull:done")); msg != "" {
		t.Fatalf("the pull failed: %s", msg)
	}
	call(3, "FetchAllRepos", []any{})
	eventually(t, "fetch:alldone", func() bool { return log.last("fetch:alldone") != nil })
	if msg := doneError(t, log.last("fetch:alldone")); msg != "" {
		t.Fatalf("fetch-all failed: %s", msg)
	}

	log.mu.Lock()
	progress := slices.Clone(log.progress)
	log.mu.Unlock()
	if len(progress) == 0 {
		t.Fatal("no clone:progress arrived")
	}
	// Coalesced: no more events than intervals in the clone, with one of
	// slack at each end.
	if most := int(took/rpc.ProgressInterval) + 2; len(progress) > most {
		t.Errorf("%d progress events in %v, want at most %d", len(progress), took, most)
	}
}
