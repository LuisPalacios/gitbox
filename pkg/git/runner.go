package git

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

// Runner runs one git command. Every git process this program starts goes
// through the package's Runner, so tests can replace git with a script and
// the rules that apply to every command (hidden window, environment, output
// bound, how a cancelled command stops) live in one place.
type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
}

// EnvPolicy selects the environment of a git command.
type EnvPolicy int

const (
	// NonInteractive is Environ() plus the settings that keep git and its
	// credential helpers from prompting. It is the zero value: a command
	// must ask for anything else.
	NonInteractive EnvPolicy = iota
	// Inherit is Environ() as it is.
	Inherit
)

// Request describes one git command.
type Request struct {
	// Dir is the working directory; empty means the current one.
	Dir string
	// Args are the arguments after "git".
	Args []string
	// Env picks the environment, unless EnvExact is set.
	Env EnvPolicy
	// EnvExact, when not nil, is the whole environment of the command.
	EnvExact []string
	// Stdin feeds the command; nil means no input.
	Stdin io.Reader
	// Stderr, when set, also receives what the command prints to stderr as
	// it is printed. Result.Stderr holds it either way.
	Stderr io.Writer
	// MaxOutput bounds Result.Stdout and Result.Stderr, each. Zero means
	// DefaultMaxOutput.
	MaxOutput int
}

// Result is what a git command produced. It is filled in also when the
// command fails.
type Result struct {
	Stdout, Stderr []byte
	// ExitCode is the exit status, or -1 when the command did not exit by
	// itself (it could not start, or it was killed).
	ExitCode int
	// Truncated reports that Stdout or Stderr reached MaxOutput and the rest
	// was dropped.
	Truncated bool
}

// combined returns what the command printed to stderr followed by what it
// printed to stdout, for callers that only classify or pass on the text.
func (r Result) combined() []byte {
	return append(append([]byte{}, r.Stderr...), r.Stdout...)
}

const (
	// DefaultMaxOutput bounds each output stream of a command.
	DefaultMaxOutput = 8 << 20
	// runnerWaitDelay is how long a cancelled command gets to exit after
	// the interrupt, and how long a finished command's helpers may keep its
	// output open, before the runner stops waiting.
	runnerWaitDelay = 2 * time.Second
)

// Default is the Runner every git command uses. Tests replace it with
// SetRunnerForTest.
var Default Runner = execRunner{}

// testCleanup is the part of *testing.T that the test setters need. Taking
// it as an interface keeps the testing package out of the program.
type testCleanup interface {
	Helper()
	Cleanup(func())
}

// SetRunnerForTest replaces Default until the test ends.
func SetRunnerForTest(t testCleanup, r Runner) {
	t.Helper()
	previous := Default
	Default = r
	t.Cleanup(func() { Default = previous })
}

// Run runs one git command through Default. Code outside this package that
// needs a git command the package has no function for uses it.
func Run(ctx context.Context, req Request) (Result, error) {
	return Default.Run(ctx, req)
}

// environ returns the environment the request asks for.
func (r Request) environ() []string {
	switch {
	case r.EnvExact != nil:
		return r.EnvExact
	case r.Env == Inherit:
		return Environ()
	default:
		return nonInteractiveEnv()
	}
}

// execRunner runs the system git. It is the only place that starts a git
// process.
type execRunner struct {
	// bin is the program to run; empty means GitBin(). Tests set it to run
	// a stand-in for git.
	bin string
}

func (r execRunner) Run(ctx context.Context, req Request) (Result, error) {
	bin := r.bin
	if bin == "" {
		bin = GitBin()
	}
	limit := req.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	stdout, stderr := &boundedBuffer{max: limit}, &boundedBuffer{max: limit}

	cmd := exec.CommandContext(ctx, bin, req.Args...)
	cmd.Dir = req.Dir
	cmd.Env = req.environ()
	// The input goes through a pipe that the runner feeds itself. Handing
	// req.Stdin to os/exec would make Wait block until that reader returns,
	// so a cancelled command could not come back while its input stayed
	// open.
	var stdinRead, stdinWrite *os.File
	if req.Stdin != nil {
		var err error
		if stdinRead, stdinWrite, err = os.Pipe(); err != nil {
			return Result{ExitCode: -1}, err
		}
		cmd.Stdin = stdinRead
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if req.Stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, req.Stderr)
	}
	// Every exec.Command in a GUI binary must hide its console window on
	// Windows. For git this is the one place that does it.
	HideWindow(cmd)

	// A cancelled command is stopped together with everything it started
	// (credential helpers, ssh, hooks). The tree is first asked to stop, so
	// git removes its lock files where the platform allows it, and whatever
	// is still alive after runnerWaitDelay is killed.
	var tree procTree
	tree.prepare(cmd)
	cmd.Cancel = func() error { return tree.terminate(cmd) }
	cmd.WaitDelay = runnerWaitDelay

	if err := cmd.Start(); err != nil {
		if stdinRead != nil {
			_ = stdinRead.Close()
			_ = stdinWrite.Close()
		}
		return Result{ExitCode: -1}, err
	}
	tree.attach(cmd)
	if stdinRead != nil {
		_ = stdinRead.Close() // the command holds its own end
		go func() {
			_, _ = io.Copy(stdinWrite, req.Stdin)
			_ = stdinWrite.Close()
		}()
	}
	err := cmd.Wait()
	if stdinWrite != nil {
		_ = stdinWrite.Close() // stops the feeder if it is still writing
	}
	if ctx.Err() != nil {
		// The command was cancelled: nothing it started may outlive it.
		tree.kill(cmd)
	}
	tree.release()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: -1, Truncated: stdout.truncated || stderr.truncated}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	return res, err
}

// boundedBuffer keeps the first max bytes written to it and drops the rest.
type boundedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); len(p) > room {
		b.truncated = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil // keep the command running; the surplus is dropped
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) Bytes() []byte { return b.buf.Bytes() }
