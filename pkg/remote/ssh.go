package remote

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// Options configure every operation against one remote.
type Options struct {
	// ServerDir is the server root on the host: absolute, or starting with
	// "~/". Empty means DefaultServerDir.
	ServerDir string
	// ClientID names this client process on the host: its in-use marker
	// and the owner of the GC lock. 1 to 64 of [A-Za-z0-9._-].
	ClientID string
	// Version and Commit are this build's stamp; the server installed must
	// be the same build.
	Version, Commit string
	// Target returns the server binary for an operating system and
	// architecture as Go spells them, with its pin.
	Target func(goos, goarch string) (Target, error)
	// Log receives every step. Nil discards them.
	Log *Log
	// ControlDir is where the client keeps its ssh control sockets when the
	// user's ssh_config has no master to reuse. Empty never adds one.
	ControlDir string
	// OnInstall is called when the server is missing and the install
	// starts.
	OnInstall func()

	// control holds the connection-reuse options the runner chose.
	control []string

	// Test-only fields, set through export_test.go.
	configFile   string                    // adds -F <configFile>
	sshPath      string                    // replaces the system ssh
	uploadFilter func(io.Reader) io.Reader // wraps the upload stream
}

// DefaultServerDir is the server root when a remote sets none.
const DefaultServerDir = "~/.gitbox-server"

// maxParsed caps what the client reads, after the marker, from a remote
// command whose output it parses, and maxBanner what it discards before the
// marker. Both match the handshake's limit.
const (
	maxBanner = 1 << 20
	maxParsed = 1 << 20
)

// marker comes first in the output of every remote command whose output is
// parsed: a login shell may print a banner before it.
var marker = []byte("\x00GBX\x00")

var aliasRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
var clientIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// baseOptions are the options of every connection. Production code never
// adds StrictHostKeyChecking, UserKnownHostsFile or -F: the user's
// configuration decides.
var baseOptions = []string{
	"-T",
	"-o", "BatchMode=yes",
	"-o", "ServerAliveInterval=15",
	"-o", "ServerAliveCountMax=3",
	"-o", "ConnectTimeout=10",
	"-e", "none",
}

// Argv returns the arguments of the system ssh that runs remoteCommand on
// alias. Every remote command of this package is built here.
func Argv(alias string, o Options, remoteCommand string) []string {
	args := append([]string(nil), baseOptions...)
	args = append(args, o.configArgs()...)
	args = append(args, o.control...)
	return append(args, alias, remoteCommand)
}

func (o Options) configArgs() []string {
	if o.configFile == "" {
		return nil
	}
	return []string{"-F", o.configFile}
}

// shellQuote quotes s as one word for a POSIX shell, fish and csh alike.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// remoteCommand wraps a POSIX script as sh -c '<script>' with its arguments,
// so the login shell, whatever it is, hands it to sh unchanged. The script
// is one line: csh rejects a newline inside quotes.
func remoteCommand(script string, args ...string) string {
	var b strings.Builder
	b.WriteString("sh -c ")
	b.WriteString(shellQuote(oneLine(script)))
	b.WriteString(" gbx")
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

// oneLine joins the lines of a script with spaces. Each line ends where a
// shell allows a space: after ';', '&&', '||', '|', 'then', 'do', '{' or
// 'in'.
func oneLine(script string) string {
	var parts []string
	for line := range strings.SplitSeq(script, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

// runner runs commands on one remote.
type runner struct {
	ssh   string
	alias string
	o     Options
	log   *Log
}

func newRunner(alias string, o Options) (*runner, error) {
	if !ValidAlias(alias) {
		return nil, newError(CodeSSHConfig, "%q is not a valid ssh_config alias", alias)
	}
	path, err := SSHPath(o)
	if err != nil {
		return nil, err
	}
	return &runner{ssh: path, alias: alias, o: o, log: o.Log}, nil
}

// command builds one ssh process. Every process this package starts comes
// from here, with its console window hidden on Windows.
func (r *runner) command(args ...string) *exec.Cmd {
	cmd := exec.Command(r.ssh, args...)
	git.HideWindow(cmd)
	// A control master that ssh leaves in the background must not hold the
	// pipes open forever.
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// setupControl decides on connection reuse: the user's own master when one
// is running, otherwise a master of the client's under ControlDir. Windows
// clients have no ControlMaster.
func (r *runner) setupControl(ctx context.Context) {
	if runtime.GOOS == "windows" || r.o.ControlDir == "" {
		return
	}
	cfg := r.command(append(r.o.configArgs(), "-G", r.alias)...)
	out, err := outputCtx(ctx, cfg)
	if err == nil {
		for line := range strings.SplitSeq(string(out), "\n") {
			key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
			if key == "controlpath" && value != "" && value != "none" {
				check := r.command(append(r.o.configArgs(), "-o", "BatchMode=yes", "-O", "check", r.alias)...)
				if _, err := outputCtx(ctx, check); err == nil {
					r.log.Printf("ssh: reusing the control master of the ssh config")
					return
				}
			}
		}
	}
	if err := os.MkdirAll(r.o.ControlDir, 0o700); err != nil {
		r.log.Printf("ssh: no connection reuse: the control folder cannot be created")
		return
	}
	r.o.control = []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=60",
		"-o", "ControlPath=" + filepath.Join(r.o.ControlDir, "cm-%C"),
	}
}

// outputCtx runs cmd and returns its stdout, killing it when ctx ends.
func outputCtx(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = cmd.Process.Kill() })
	defer stop()
	err := cmd.Wait()
	return out.Bytes(), err
}

// CheckAlias asks the system ssh to resolve alias without connecting. It
// catches a broken ssh_config, not an unknown host: ssh resolves any name.
func CheckAlias(ctx context.Context, alias string, o Options) error {
	r, err := newRunner(alias, o)
	if err != nil {
		return err
	}
	cmd := r.command(append(o.configArgs(), "-G", alias)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if _, err := outputCtx(ctx, cmd); err != nil {
		r.log.Printf("ssh -G %s failed: %s", alias, firstLines(stderr.Bytes(), 3))
		return newError(CodeSSHConfig, "ssh cannot read the configuration of %s", alias)
	}
	return nil
}

// result is what one parsed remote command returned.
type result struct {
	fields   []string // the NUL-terminated fields after the marker
	banner   int      // bytes discarded before the marker
	exitCode int
	stderr   []byte
}

// run runs one remote command and parses its output: everything up to the
// marker is discarded, then NUL-terminated fields are read. stdin, when not
// nil, is streamed to the command.
func (r *runner) run(ctx context.Context, step, script string, stdin io.Reader, args ...string) (*result, error) {
	t0 := time.Now()
	cmd := r.command(Argv(r.alias, r.o, remoteCommand(script, args...))...)
	cmd.Stdin = stdin
	stderr := newTail(64 << 10)
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, newError(CodeSSHMissing, "ssh could not be started")
	}
	stop := context.AfterFunc(ctx, func() { _ = cmd.Process.Kill() })
	defer stop()

	res := &result{}
	br := bufio.NewReader(stdout)
	found, banner, readErr := skipMarker(br, maxBanner)
	res.banner = banner
	if found {
		res.fields, readErr = readFields(io.LimitReader(br, maxParsed))
	}
	if !found && readErr == errOverflow {
		_ = cmd.Process.Kill()
	}
	_, _ = io.Copy(io.Discard, br)
	waitErr := cmd.Wait()
	res.stderr = stderr.Bytes()
	res.exitCode = exitCode(waitErr)
	r.log.Printf("%s: exit %d in %s [banner: %d bytes]", step, res.exitCode, time.Since(t0).Round(time.Millisecond), banner)
	r.logStderr(step, res.stderr, stderr.total())

	if ctx.Err() != nil {
		return res, newError(CodeSSHTimeout, "%s was cancelled", step)
	}
	if !found {
		if readErr == errOverflow {
			return res, newError(CodeBannerOverflow, "the login shell of %s printed more than 1 MiB before the answer", r.alias)
		}
		if res.exitCode == 255 || waitErr != nil {
			return res, classifySSH(r.alias, res.exitCode, res.stderr)
		}
		return res, newError(CodeSSHFailed, "%s gave no answer on %s", step, r.alias)
	}
	return res, nil
}

// logStderr records what ssh and the remote command printed on stderr. A
// login shell can print a lot, so only the first lines are kept.
func (r *runner) logStderr(step string, b []byte, total int) {
	if total == 0 {
		return
	}
	const keep = 20
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	r.log.Printf("%s: stderr %d bytes, last %d lines:", step, total, len(lines))
	for _, l := range lines {
		r.log.Printf("  %s", l)
	}
}

var errOverflow = errors.New("banner overflow")

// skipMarker discards bytes up to and including the marker. It reports
// whether the marker was found and how much it discarded; it gives up with
// errOverflow after limit bytes.
func skipMarker(r *bufio.Reader, limit int) (bool, int, error) {
	matched, discarded := 0, 0
	for {
		c, err := r.ReadByte()
		if err != nil {
			return false, discarded + matched, err
		}
		switch {
		case c == marker[matched]:
			matched++
			if matched == len(marker) {
				return true, discarded, nil
			}
			continue
		case c == marker[0]:
			// The marker's only border is its first byte.
			discarded += matched
			matched = 1
		default:
			discarded += matched + 1
			matched = 0
		}
		if discarded > limit {
			return false, discarded, errOverflow
		}
	}
}

// readFields reads NUL-terminated fields until the stream ends. A field
// without its NUL at the end is dropped.
func readFields(r io.Reader) ([]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var fields []string
	for {
		i := bytes.IndexByte(data, 0)
		if i < 0 {
			return fields, nil
		}
		fields = append(fields, string(data[:i]))
		data = data[i+1:]
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		if code := ee.ExitCode(); code >= 0 {
			return code
		}
	}
	return -1
}

// classifySSH turns a failed ssh into its code. ssh itself exits with 255;
// its stderr says why. The text is read only to pick a code: it never goes
// into the error, only into the log.
func classifySSH(alias string, code int, stderr []byte) *Error {
	s := strings.ToLower(string(stderr))
	has := func(subs ...string) bool {
		for _, sub := range subs {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
	switch {
	case has("host key verification failed", "remote host identification has changed", "host key for", "no matching host key"):
		return newError(CodeSSHHostKey, "ssh does not trust the host key of %s; run ssh %s once in a terminal", alias, alias)
	// An agent that asks for approval, such as 1Password, fails the signature
	// when nobody approves: ssh then also prints "permission denied".
	case has("communication with agent failed", "agent refused operation"):
		return newError(CodeSSHAgent, "the ssh agent did not sign the login to %s; approve the request in the agent and connect again", alias)
	case has("permission denied", "too many authentication failures", "no supported authentication methods"):
		return newError(CodeSSHAuth, "ssh could not log in to %s without a prompt; run ssh %s once in a terminal", alias, alias)
	case has("timed out", "timeout, server", "operation timed out"):
		return newError(CodeSSHTimeout, "%s did not answer in time", alias)
	case has("bad configuration option", "terminating, ", "garbage at end of line", "no argument after keyword", "unsupported option"):
		return newError(CodeSSHConfig, "ssh cannot read the configuration of %s", alias)
	}
	return newError(CodeSSHFailed, "ssh to %s failed with exit code %d", alias, code)
}

// tail keeps the last n bytes written to it, and counts all of them.
type tail struct {
	mu  sync.Mutex
	n   int
	buf []byte
	all int
}

func newTail(n int) *tail { return &tail{n: n} }

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.all += len(p)
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.n; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tail) Bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]byte(nil), t.buf...)
}

func (t *tail) total() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.all
}

func firstLines(b []byte, n int) string {
	lines := strings.SplitN(strings.TrimSpace(string(b)), "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " | ")
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
