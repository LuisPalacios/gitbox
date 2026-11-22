//go:build sshd_integration

package remote_test

// The sshd integration tests of the bootstrap (#239). scripts/remote-it.sh
// builds the gitbox-sshd image and the linux/amd64 server with the
// e2e_sentinel tag, then runs:
//
//	go test -race -tags sshd_integration ./pkg/remote/
//
// Each test starts its own container on an --internal Docker network (no
// egress), reached by its IP, and talks to it through the real system ssh
// with a config file of its own. "Never executed" means the line count of
// the sentinel's $HOME/.gitbox-e2e/ran.log did not change.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

var it struct {
	image, network  string
	server          []byte
	pin             string
	version, commit string
	keyDir          string
	pubKey          string
}

const testHome = "/home/test"

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	fail := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, "sshd integration: "+format+"\n", args...)
		return 1
	}
	it.image = os.Getenv("GITBOX_IT_IMAGE")
	if it.image == "" {
		it.image = "gitbox-sshd"
	}
	it.version, it.commit = os.Getenv("GITBOX_IT_VERSION"), os.Getenv("GITBOX_IT_COMMIT")
	serverPath := os.Getenv("GITBOX_IT_SERVER")
	if serverPath == "" || it.version == "" || it.commit == "" {
		return fail("GITBOX_IT_SERVER, GITBOX_IT_VERSION and GITBOX_IT_COMMIT are required; run scripts/remote-it.sh")
	}
	var err error
	if it.server, err = os.ReadFile(serverPath); err != nil {
		return fail("reading the server: %v", err)
	}
	sum := sha256.Sum256(it.server)
	it.pin = hex.EncodeToString(sum[:])

	// Control sockets need a short path.
	if it.keyDir, err = os.MkdirTemp("/tmp", "gbxit"); err != nil {
		return fail("%v", err)
	}
	defer os.RemoveAll(it.keyDir)
	key := filepath.Join(it.keyDir, "id")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "gitbox-it", "-f", key).CombinedOutput(); err != nil {
		return fail("ssh-keygen: %v %s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		return fail("%v", err)
	}
	it.pubKey = strings.TrimSpace(string(pub))

	it.network = "gbx-it-" + strconv.Itoa(os.Getpid())
	if out, err := exec.Command("docker", "network", "create", "--internal", it.network).CombinedOutput(); err != nil {
		return fail("docker network create: %v %s", err, out)
	}
	defer func() { _ = exec.Command("docker", "network", "rm", it.network).Run() }()
	return m.Run()
}

type ctOpts struct {
	noexecHome bool
	shell      string
	bashrc     string
}

type container struct {
	t  *testing.T
	id string
	ip string
}

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func startContainer(t *testing.T, o ctOpts) *container {
	t.Helper()
	args := []string{"run", "-d", "--rm", "--network", it.network, "-e", "AUTHORIZED_KEY=" + it.pubKey}
	if o.shell != "" {
		args = append(args, "-e", "LOGIN_SHELL="+o.shell)
	}
	if o.bashrc != "" {
		args = append(args, "-e", "BASHRC_B64="+base64.StdEncoding.EncodeToString([]byte(o.bashrc)))
	}
	if o.noexecHome {
		args = append(args, "--tmpfs", testHome+":noexec,uid=1000,gid=1000,mode=0755")
	}
	c := &container{t: t, id: docker(t, append(args, it.image)...)}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", c.id).Run() })
	c.ip = docker(t, "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", c.id)
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(c.ip, "22"), time.Second)
		if err == nil {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			line, _ := bufio.NewReader(conn).ReadString('\n')
			_ = conn.Close()
			if strings.HasPrefix(line, "SSH-") {
				return c
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("sshd in %s did not answer: %v\n%s", c.ip, err, docker(t, "logs", c.id))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// sh runs a command in the container as the test user.
func (c *container) sh(script string) string {
	c.t.Helper()
	out, err := exec.Command("docker", "exec", "-u", "test", "-e", "HOME="+testHome, c.id, "sh", "-c", script).CombinedOutput()
	if err != nil {
		c.t.Fatalf("in container: %s: %v\n%s", script, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (c *container) exists(path string) bool {
	return exec.Command("docker", "exec", c.id, "test", "-e", path).Run() == nil
}

func (c *container) runs() int {
	c.t.Helper()
	out := c.sh("cat " + testHome + "/.gitbox-e2e/ran.log 2>/dev/null || true")
	if out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
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
	dir := t.TempDir()
	known := filepath.Join(dir, "known_hosts")
	content := ""
	if knownHost {
		content = c.ip + " " + docker(t, "exec", c.id, "cat", "/etc/ssh/ssh_host_ed25519_key.pub") + "\n"
	}
	if err := os.WriteFile(known, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config")
	conf := fmt.Sprintf("Host %s\n  HostName %s\n  User test\n  IdentityFile %s\n  IdentitiesOnly yes\n  IdentityAgent none\n  UserKnownHostsFile %s\n  GlobalKnownHostsFile /dev/null\n",
		alias, c.ip, filepath.Join(it.keyDir, "id"), known)
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
		Target: func(goos, goarch string) (remote.Target, error) {
			if goos != "linux" || goarch != "amd64" {
				return remote.Target{}, errors.New("only linux/amd64 is built for the tests")
			}
			return remote.Target{SHA256: it.pin, Size: int64(len(it.server)), Load: func() ([]byte, error) { return it.server, nil }}, nil
		},
	}
	remote.SetConfigFile(&s.opts, cfg)
	s.build = remote.BuildName(it.version, it.commit, it.pin)
	s.root = testHome + "/.gitbox-server"
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
	if hello := client.Server(); hello.Version != it.version || hello.Commit != it.commit || hello.OS != "linux" {
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
	// No default route: the routing table has no 00000000 destination.
	for _, line := range strings.Split(c.sh("cat /proc/net/route"), "\n")[1:] {
		if f := strings.Fields(line); len(f) > 1 && f[1] == "00000000" {
			t.Fatalf("the container has a default route: %s", line)
		}
	}
	s := c.client(t, "client-a", true)
	conn := s.connect(t)
	defer conn.close()
	if !c.exists(s.final) {
		t.Fatal("no final binary after the install")
	}
	if got := c.sh("stat -c %a " + s.final + " " + filepath.Dir(s.final)); got != "700\n700" {
		t.Errorf("modes of the binary and its folder = %q, want 700 and 700", got)
	}
}

func TestRejectCorruptBinary(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.client(t, "client-a", true)
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
	if c.runs() != 0 {
		t.Error("the corrupt upload ran")
	}
}

func TestRejectWrongHash(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.client(t, "client-a", true)
	wrong := strings.Repeat("f", 64)
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
	if n := c.runs(); n != 0 {
		t.Errorf("ran.log has %d lines: the trial run executed a file with the wrong hash", n)
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

	c.sh("touch -d '11 minutes ago' " + oldDir + "/in-use.old-client")
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
	if got := strings.Fields(c.sh("sha256sum " + a.final))[0]; got != it.pin {
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
	c.sh("touch -d '2 hours ago' " + dir + "/" + tmps)

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
	c.sh("umask 077 && mkdir -p " + lock + " && echo '{}' > " + lock + "/owner && touch -d '10 minutes ago' " + lock)
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

// readOnlyCalls are the methods a gitbox-server serves to a read-only
// client (#240), with arguments for the host that
// TestReadOnlySessionLeavesConfigUntouched seeds. TestIntegrationCallsEveryServedMethod
// in cmd/gui keeps the list equal to servedMethods.
var readOnlyCalls = []struct {
	method string
	params []any
}{
	{"AccountDeletionImpact", []any{"acct"}},
	{"CheckGlobalGCMConfig", []any{}},
	{"CheckGlobalGitignore", []any{}},
	{"CheckGlobalIdentity", []any{}},
	{"CheckMirrorCredentials", []any{"acct"}},
	{"CheckMoveReadiness", []any{"acct", "acct", false}},
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
	{"PreviewSweep", []any{"acct", "org/repo"}},
	{"RecommendedGlobalGitignoreBody", []any{}},
	{"RefreshAllPRs", []any{}},
	{"RefreshStatus", []any{}},
	{"ReloadConfig", []any{}},
	{"RepoNesting", []any{}},
	{"RepoWorkspaceFiles", []any{"acct", "org/repo"}},
	{"ScanFolderForClones", []any{testHome + "/git"}},
	{"SuggestAccountKey", []any{"gitea", "alice"}},
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

// A read-only session changes nothing on the host (#240): after every
// served method, a fetch-all and the harness watcher's first pass, the
// host's gitbox.json has the same bytes and modification time, and its
// folder holds the same files.
func TestReadOnlySessionLeavesConfigUntouched(t *testing.T) {
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
	fingerprint := func() string {
		return c.sh("sha256sum " + cfgPath + " && stat -c '%Y.%y' " + cfgPath + " && ls -la --time-style=+%s " + cfgDir + " | grep -v ' [.][.]$'")
	}
	before, cloneBefore := fingerprint(), gitConfig()

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
	// A mutation is refused by the server itself, not only by the client.
	ctx, cancel := context.WithTimeout(testCtx(t), 30*time.Second)
	if err := client.Call(ctx, "AddRoot", []any{testHome + "/elsewhere"}, nil, rpc.WithRequestID("00000000-0000-4000-8000-000000000001")); !rpc.IsCode(err, rpc.ErrCodeUnsupported) {
		t.Errorf("AddRoot on a read-only server = %v, want unsupported", err)
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

	if after := fingerprint(); after != before {
		s.dump(t)
		t.Errorf("the host's config changed:\nbefore\n%s\nafter\n%s", before, after)
	}
	if after := gitConfig(); after != cloneBefore {
		t.Errorf("the clone's .git/config changed: fetch repaired it on a read-only server")
	}
	if c.exists(testHome + "/elsewhere") {
		t.Error("the refused AddRoot created its folder")
	}
}
