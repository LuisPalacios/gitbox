//go:build remote_host && !sshd_integration

package remote_test

// The harness of the real-host lane (#245). scripts/remote-host-it.sh
// builds the server with the e2e_sentinel tag and the fake forge for the
// host's platform, then runs:
//
//	GITBOX_REMOTE_TEST_HOST=<alias> go test -race -tags remote_host ./pkg/remote/
//
// The alias must reach a disposable account with key-only ssh: the tests
// write and delete files in its home. The harness resolves the alias with
// ssh -G and then reads none of the user's ssh files but the key and the
// known_hosts file: it writes its own ssh config with IdentityAgent none.
// It refuses to start unless the account holds ~/.gitbox-it-host, which
// its setup creates, and unless it is a different account from this one.
//
// Only the tests in hostTests run here; the rest need a container of their
// own (a noexec home, tc, a git host) and skip.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/remote"
)

// hostTests are the tests that run against a real host.
var hostTests = map[string]bool{
	"TestInstallNoInternet": true, "TestRejectCorruptBinary": true, "TestRejectWrongHash": true,
	"TestRejectModifiedBetweenStarts": true, "TestPreviousVersionInstalled": true, "TestConcurrentInstall": true,
	"TestInterruptedUpload": true, "TestNoisyBashrc": true, "TestStaleGCLock": true,
	"TestTokenCanary": true, "TestRemoteTerminalOverSSH": true,
}

var hostIT struct {
	// block is the body of the ssh config Host block that reaches the host,
	// and known the content of its known_hosts file.
	block, known string
	// cfg is the ssh config of the harness's own commands.
	cfg string
	// home, shell and goos describe the account.
	home, shell, goos string
	// run is this run's folder on the host.
	run string

	mu sync.Mutex
	n  int
}

// markerFile marks an account as disposable.
const markerFile = ".gitbox-it-host"

// outMarker starts the output of a host command: a login shell may print
// anything before it (TestNoisyBashrc's rc file does).
const outMarker = "@@gitbox-it-out@@\n"

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	fail := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, "remote_host: "+format+"\n", args...)
		return 1
	}
	name := os.Getenv("GITBOX_REMOTE_TEST_HOST")
	target := os.Getenv("GITBOX_IT_TARGET")
	if name == "" || target == "" {
		return fail("GITBOX_REMOTE_TEST_HOST and GITBOX_IT_TARGET are required; run scripts/remote-host-it.sh")
	}
	if !remote.ValidAlias(name) {
		return fail("%q is not a valid ssh alias", name)
	}
	if err := loadServer(target); err != nil {
		return fail("%v; run scripts/remote-host-it.sh", err)
	}
	var err error
	if it.keyDir, err = os.MkdirTemp("/tmp", "gbxit"); err != nil {
		return fail("%v", err)
	}
	defer os.RemoveAll(it.keyDir)

	resolved, err := exec.Command("ssh", "-G", name).Output()
	if err != nil {
		return fail("ssh -G %s: %v", name, err)
	}
	conf := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(resolved))
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), " ")
		if _, seen := conf[k]; !seen {
			conf[k] = v
		}
	}
	host, port, login, identity := conf["hostname"], conf["port"], conf["user"], conf["identityfile"]
	knownFile := os.Getenv("GITBOX_REMOTE_TEST_KNOWN_HOSTS")
	if knownFile == "" {
		knownFile, _, _ = strings.Cut(conf["userknownhostsfile"], " ")
	}
	if knownFile == "" || knownFile == "/dev/null" || knownFile == "none" {
		return fail("%s has no known_hosts file to check the host key; set GITBOX_REMOTE_TEST_KNOWN_HOSTS", name)
	}
	knownFile = expandHome(knownFile)
	known, err := os.ReadFile(knownFile)
	if err != nil {
		return fail("reading %s: %v", knownFile, err)
	}
	hostIT.known = string(known)
	hostIT.block = fmt.Sprintf("  HostName %s\n  Port %s\n  User %s\n  IdentityFile %s\n", host, port, login, identity)
	hostIT.cfg = filepath.Join(it.keyDir, "admin")
	admin := fmt.Sprintf("Host %s\n%s  IdentitiesOnly yes\n  IdentityAgent none\n  UserKnownHostsFile %s\n  GlobalKnownHostsFile /dev/null\n"+
		"  StrictHostKeyChecking yes\n  UpdateHostKeys no\n  BatchMode yes\n  ControlMaster auto\n  ControlPath %s\n  ControlPersist 60\n",
		alias, hostIT.block, knownFile, filepath.Join(it.keyDir, "admin-%C"))
	if err := os.WriteFile(hostIT.cfg, []byte(admin), 0o600); err != nil {
		return fail("%v", err)
	}
	defer func() { _ = exec.Command("ssh", "-F", hostIT.cfg, "-O", "exit", alias).Run() }()

	out, err := hostRun(`id -un; printf '%s\n' "$HOME" "$SHELL"; uname -s; [ -f "$HOME/`+markerFile+`" ] && echo marked || echo unmarked`, nil)
	if err != nil {
		return fail("reaching %s: %v\n%s", name, err, out)
	}
	f := strings.Split(out, "\n")
	if len(f) != 5 {
		return fail("%s gave an unexpected answer: %q", name, out)
	}
	remoteUser, home, shell, system, mark := f[0], f[1], f[2], f[3], f[4]
	if mark != "marked" {
		return fail("%s has no ~/%s: the tests only run against a disposable account set up for them (see docs/testing.md)", name, markerFile)
	}
	if !strings.HasPrefix(home, "/") || len(home) < 2 || strings.ContainsAny(home, "'\n") {
		return fail("%s has an unusable home %q", name, home)
	}
	if me, err := user.Current(); err == nil && isLocal(host) && (remoteUser == me.Username || home == me.HomeDir) {
		return fail("%s is this account: the tests only run against a disposable account of their own", name)
	}
	switch system {
	case "Darwin":
		hostIT.goos = "darwin"
	case "Linux":
		hostIT.goos = "linux"
	default:
		return fail("%s runs %s", name, system)
	}
	if hostIT.goos != it.goos {
		return fail("%s runs %s but the server was built for %s", name, hostIT.goos, it.target)
	}
	hostIT.home, hostIT.shell = home, shell
	hostIT.run = home + "/.gitbox-it/" + strconv.Itoa(os.Getpid())
	if out, err := hostRun("umask 077 && mkdir -p "+q(hostIT.run), nil); err != nil {
		return fail("creating the run folder: %v\n%s", err, out)
	}
	defer func() { _, _ = hostRun("rm -rf "+q(hostIT.run), nil) }()
	fmt.Fprintf(os.Stderr, "remote_host: %s is %s@%s, %s, login shell %s\n", name, remoteUser, host, it.target, shell)
	return m.Run()
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, rest)
		}
	}
	return p
}

// isLocal reports whether host names this machine.
func isLocal(host string) bool {
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	name, err := os.Hostname()
	return err == nil && (strings.EqualFold(host, name) || strings.EqualFold(host, strings.Split(name, ".")[0]))
}

// hostRun runs a POSIX sh script on the host and returns what it printed
// after outMarker, trimmed. The login shell only execs sh.
func hostRun(script string, stdin []byte) (string, error) {
	cmd := exec.Command("ssh", "-F", hostIT.cfg, "-T", "--", alias, "exec /bin/sh -c "+q("printf '"+strings.TrimSuffix(outMarker, "\n")+"\\n'; "+script))
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := stdout.String()
	if i := strings.Index(out, outMarker); i >= 0 {
		out = out[i+len(outMarker):]
	} else if err == nil {
		err = errors.New("the host printed no output marker")
	}
	if err != nil {
		return strings.TrimSpace(out + "\n" + tail(stderr.String())), err
	}
	return strings.TrimSpace(out), nil
}

func tail(s string) string {
	if len(s) > 4096 {
		return s[len(s)-4096:]
	}
	return s
}

type container struct {
	t *testing.T
	// dir is the test's folder on the host.
	dir string
	// id is always empty: the tests that pass it to docker need a
	// container and skip before they use it.
	id string
}

// rcFile is the file the host's login shell reads before an ssh command:
// zsh reads ~/.zshenv, not ~/.zshrc, when it is not interactive.
func rcFile() string {
	switch filepath.Base(hostIT.shell) {
	case "zsh":
		return ".zshenv"
	case "bash":
		return ".bashrc"
	}
	return ""
}

// startContainer gives the test a clean state on the real host: a folder
// of its own for the server and fixtures, and no gitbox config, clone root
// or hostile folders in the home.
func startContainer(t *testing.T, o ctOpts) *container {
	t.Helper()
	root, _, _ := strings.Cut(t.Name(), "/")
	if !hostTests[root] {
		t.Skip("runs in the sshd lane only: it needs a container of its own")
	}
	if o.noexecHome || o.netAdmin || o.gitDaemon || o.gitHost {
		t.Skip("needs a container of its own")
	}
	if o.shell != "" && o.shell != hostIT.shell {
		t.Skipf("the host's login shell is %s, not %s", hostIT.shell, o.shell)
	}
	hostIT.mu.Lock()
	hostIT.n++
	c := &container{t: t, dir: hostIT.run + "/" + strconv.Itoa(hostIT.n)}
	hostIT.mu.Unlock()
	home := q(hostIT.home)
	reset := "rm -rf " + home + "/.config/gitbox " + home + "/git " + home + "/hostile"
	c.sh("umask 077 && mkdir -p " + q(c.dir) + " && " + reset)
	rc := rcFile()
	if o.bashrc != "" {
		if rc == "" {
			t.Skipf("no rc file is known for %s", hostIT.shell)
		}
		path, saved := q(hostIT.home+"/"+rc), q(c.dir+"/rc.orig")
		c.sh("if [ -e " + path + " ]; then cp -p " + path + " " + saved + "; fi")
		c.writeFileAsTest(hostIT.home+"/"+rc, o.bashrc)
		t.Cleanup(func() {
			_, _ = hostRun("if [ -e "+saved+" ]; then mv -f "+saved+" "+path+"; else rm -f "+path+"; fi", nil)
		})
	}
	t.Cleanup(func() {
		pids := q(c.dir + "/bg.pids")
		// A daemon a test started (#247) outlives its sessions: stop it
		// before its folder goes, or it would run on for its idle time.
		daemons := "for p in " + q(c.serverRoot()) + "/*/state/daemon.pid; do [ -f \"$p\" ] && kill $(cat \"$p\") 2>/dev/null; done; sleep 1; "
		_, _ = hostRun("if [ -f "+pids+" ]; then kill $(cat "+pids+") 2>/dev/null; fi; "+daemons+reset+"; rm -rf "+q(c.dir), nil)
	})
	return c
}

// sh runs a command on the host as the test account.
func (c *container) sh(script string) string {
	c.t.Helper()
	out, err := hostRun(script, nil)
	if err != nil {
		c.t.Fatalf("on the host: %s: %v\n%s", script, err, out)
	}
	return out
}

// ok runs a command on the host and reports whether it succeeded.
func (c *container) ok(script string) bool {
	_, err := hostRun(script, nil)
	return err == nil
}

func (c *container) exists(path string) bool { return c.ok("test -e " + q(path)) }

// writeFileAsTest writes content to path through stdin, so it is on no
// command line.
func (c *container) writeFileAsTest(path, content string) {
	c.t.Helper()
	if out, err := hostRun("umask 077 && cat > "+q(path), []byte(content)); err != nil {
		c.t.Fatalf("writing %s: %v\n%s", path, err, out)
	}
}

// install copies a program of this machine to path on the host.
func (c *container) install(local, path string) {
	c.t.Helper()
	data, err := os.ReadFile(local)
	if err != nil {
		c.t.Fatal(err)
	}
	if out, err := hostRun("umask 077 && cat > "+q(path)+" && chmod 700 "+q(path), data); err != nil {
		c.t.Fatalf("installing %s: %v\n%s", path, err, out)
	}
}

// background starts a command and leaves it running until the test ends.
func (c *container) background(script string) {
	c.t.Helper()
	c.sh("nohup /bin/sh -c " + q(script) + " > /dev/null 2>&1 < /dev/null & echo $! >> " + q(c.dir+"/bg.pids"))
}

// sampleProcesses appends the command line of every process the account
// can see to log every 50 ms: on macOS that is every process of the host,
// and at least the server and every git it starts, which run as the
// account. The log stays in the test's folder, which the cleanup deletes.
func (c *container) sampleProcesses(log string) {
	c.t.Helper()
	c.background("while :; do ps -axww -o args >> " + q(log) + "; sleep 0.05; done")
}

func (c *container) home() string    { return hostIT.home }
func (c *container) goos() string    { return hostIT.goos }
func (c *container) scratch() string { return c.dir }

// serverDir is the test's server_dir, under the home.
func (c *container) serverDir() string {
	return "~/" + strings.TrimPrefix(c.dir, hostIT.home+"/") + "/server"
}
func (c *container) serverRoot() string { return c.dir + "/server" }
func (c *container) hostBlock() string  { return hostIT.block }
func (c *container) knownHosts() string { return hostIT.known }
func (c *container) requireNoEgress() {
	c.t.Log("a real host keeps its network; the sshd lane proves the install needs no egress")
}
func terminalShells(t *testing.T) []terminalShell {
	if hostIT.goos == "darwin" && filepath.Base(hostIT.shell) != "zsh" {
		t.Fatalf("the macOS test account's login shell is %s; set it to /bin/zsh, the default", hostIT.shell)
	}
	return []terminalShell{{hostIT.shell, remote.ShellFamily(hostIT.shell)}}
}
