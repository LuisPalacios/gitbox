package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeReply is what a scripted git command does.
type fakeReply struct {
	Stdout, Stderr string
	Exit           int
	// Hang makes the command run until its context is cancelled.
	Hang bool
}

// fakeRunner is a Runner that answers from a script instead of running git.
// It follows the same contract as the real one (TestRunnerContract runs both
// through one table), so a test written against it holds for real git too.
type fakeRunner struct {
	// Script answers a command. Nil answers every command with success.
	// stdin reads the command's input; a command that hangs never calls it.
	Script func(req Request, stdin func() []byte) fakeReply

	mu    sync.Mutex
	calls []Request
}

// fakeExit is the error of a scripted command that exits non-zero. It is
// deliberately not an *exec.ExitError: code must read the status from the
// Result, not from the error type.
type fakeExit int

func (e fakeExit) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func (f *fakeRunner) Run(ctx context.Context, req Request) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{ExitCode: -1}, err
	}
	stdin := func() []byte {
		if req.Stdin == nil {
			return nil
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(req.Stdin)
		return buf.Bytes()
	}
	var reply fakeReply
	if f.Script != nil {
		reply = f.Script(req, stdin)
	}
	if reply.Hang {
		<-ctx.Done()
		return Result{ExitCode: -1}, ctx.Err()
	}
	limit := req.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	stdout, stderr := &boundedBuffer{max: limit}, &boundedBuffer{max: limit}
	_, _ = stdout.Write([]byte(reply.Stdout))
	_, _ = stderr.Write([]byte(reply.Stderr))
	if req.Stderr != nil {
		_, _ = req.Stderr.Write([]byte(reply.Stderr))
	}
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: reply.Exit, Truncated: stdout.truncated || stderr.truncated}
	if reply.Exit != 0 {
		return res, fakeExit(reply.Exit)
	}
	return res, nil
}

// Calls returns the requests received so far.
func (f *fakeRunner) Calls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// contractObject is the object name git gives the input "contract\n".
const contractObject = "4f20aab5d23c15070b8be7029c5f28031c4e898a"

// contractScript answers the commands of TestRunnerContract the way git does.
func contractScript(repo string) func(Request, func() []byte) fakeReply {
	return func(req Request, stdin func() []byte) fakeReply {
		switch strings.Join(req.Args, " ") {
		case "--version":
			return fakeReply{Stdout: "git version 2.99.0\n"}
		case "rev-parse --show-toplevel":
			if req.Dir != repo {
				return fakeReply{Stderr: "fatal: not a git repository (or any of the parent directories): .git\n", Exit: 128}
			}
			return fakeReply{Stdout: repo + "\n"}
		case "hash-object --stdin":
			if string(stdin()) != "contract\n" {
				return fakeReply{Stderr: "unexpected input\n", Exit: 1}
			}
			return fakeReply{Stdout: contractObject + "\n"}
		case "config --get contract.missing":
			return fakeReply{Exit: 1}
		case "var GIT_TERMINAL_PROMPT_PROBE":
			return fakeReply{Stdout: fmt.Sprint(slices.Contains(req.environ(), "GIT_TERMINAL_PROMPT=0")) + "\n"}
		case "help -a":
			return fakeReply{Stdout: strings.Repeat("git command list line\n", 200)}
		case "cat-file --batch":
			return fakeReply{Hang: true} // waits for input that never comes
		}
		return fakeReply{Stderr: "git: unknown command\n", Exit: 1}
	}
}

// TestRunnerContract states what every Runner must do, and holds the real
// runner and the fake to it with one table. A fake that drifts from real git
// fails here instead of hiding a bug in the tests that use it.
func TestRunnerContract(t *testing.T) {
	isolateGit(t)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := run(repo, "init", "--quiet"); err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	runners := map[string]Runner{"real": execRunner{}, "fake": &fakeRunner{Script: contractScript(repo)}}
	for name, r := range runners {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			t.Run("success returns stdout, status 0 and no error", func(t *testing.T) {
				res, err := r.Run(ctx, Request{Args: []string{"--version"}})
				if err != nil || res.ExitCode != 0 || !strings.HasPrefix(string(res.Stdout), "git version ") || res.Truncated {
					t.Errorf("res = %+v, err = %v", res, err)
				}
			})

			t.Run("the command runs in Dir", func(t *testing.T) {
				res, err := r.Run(ctx, Request{Dir: repo, Args: []string{"rev-parse", "--show-toplevel"}})
				if err != nil || filepath.ToSlash(strings.TrimSpace(string(res.Stdout))) != filepath.ToSlash(repo) {
					t.Errorf("toplevel = %q, err = %v, want %q", res.Stdout, err, repo)
				}
			})

			t.Run("a failure returns an error with the status and stderr", func(t *testing.T) {
				res, err := r.Run(ctx, Request{Dir: outside, Args: []string{"rev-parse", "--show-toplevel"}})
				if err == nil || res.ExitCode != 128 || !strings.Contains(string(res.Stderr), "not a git repository") {
					t.Errorf("res = %+v, err = %v", res, err)
				}
			})

			t.Run("a quiet failure still reports its status", func(t *testing.T) {
				res, err := r.Run(ctx, Request{Dir: repo, Args: []string{"config", "--get", "contract.missing"}})
				if err == nil || res.ExitCode != 1 {
					t.Errorf("res = %+v, err = %v, want status 1", res, err)
				}
			})

			t.Run("stdin reaches the command", func(t *testing.T) {
				res, err := r.Run(ctx, Request{Dir: repo, Args: []string{"hash-object", "--stdin"}, Stdin: strings.NewReader("contract\n")})
				if got := strings.TrimSpace(string(res.Stdout)); err != nil || got != contractObject {
					t.Errorf("object = %q, err = %v; want %s, the name of what was sent", got, err, contractObject)
				}
			})

			t.Run("stderr is also copied live", func(t *testing.T) {
				var live bytes.Buffer
				res, err := r.Run(ctx, Request{Dir: outside, Args: []string{"rev-parse", "--show-toplevel"}, Stderr: &live})
				if err == nil || live.Len() == 0 || live.String() != string(res.Stderr) {
					t.Errorf("live copy %q, result %q, err %v", live.String(), res.Stderr, err)
				}
			})

			t.Run("output past MaxOutput is dropped and flagged", func(t *testing.T) {
				res, err := r.Run(ctx, Request{Args: []string{"help", "-a"}, MaxOutput: 64})
				if err != nil || len(res.Stdout) != 64 || !res.Truncated {
					t.Errorf("kept %d bytes, truncated %v, err %v; want 64, true, nil", len(res.Stdout), res.Truncated, err)
				}
			})

			t.Run("a cancelled context stops the command", func(t *testing.T) {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				started := time.Now()
				res, err := r.Run(cancelled, Request{Dir: repo, Args: []string{"cat-file", "--batch"}})
				if err == nil || res.ExitCode != -1 {
					t.Errorf("res = %+v, err = %v; want an error and status -1", res, err)
				}
				if spent := time.Since(started); spent > 5*time.Second {
					t.Errorf("the cancelled command took %v", spent)
				}
			})

			t.Run("a command in flight stops when its context is cancelled", func(t *testing.T) {
				// git cat-file --batch reads requests from its input and
				// waits; the pipe never delivers any.
				pr, pw := io.Pipe()
				defer pw.Close()
				running, cancel := context.WithCancel(ctx)
				defer cancel()
				type outcome struct {
					res Result
					err error
				}
				done := make(chan outcome, 1)
				go func() {
					res, err := r.Run(running, Request{Dir: repo, Args: []string{"cat-file", "--batch"}, Stdin: pr})
					done <- outcome{res, err}
				}()
				select {
				case o := <-done:
					t.Fatalf("the command ended before the cancel: %+v, %v", o.res, o.err)
				case <-time.After(300 * time.Millisecond):
				}
				started := time.Now()
				cancel()
				select {
				case o := <-done:
					if o.err == nil {
						t.Errorf("a cancelled command must report an error: %+v", o.res)
					}
				case <-time.After(runnerWaitDelay + 10*time.Second):
					t.Fatal("the command did not stop after the cancel")
				}
				if spent := time.Since(started); spent > runnerWaitDelay+5*time.Second {
					t.Errorf("the command took %v to stop", spent)
				}
			})
		})
	}
}

// A translated git must not defeat the classifiers: the commands the package
// runs speak English whatever the system language is.
func TestGitSpeaksEnglishToTheClassifiers(t *testing.T) {
	isolateGit(t)
	for _, kv := range [][2]string{{"LC_ALL", "es_ES.UTF-8"}, {"LANG", "es_ES.UTF-8"}, {"LANGUAGE", "es"}} {
		t.Setenv(kv[0], kv[1])
	}
	_, err := Status(t.TempDir())
	ge, ok := errors.AsType[*GitError](err)
	if !ok || ge.Code != CodeNotARepo {
		t.Errorf("status outside a repository = %#v, want code %q whatever the language", err, CodeNotARepo)
	}
}

// The environment policy is decided by the request, in one place.
func TestRequestEnviron(t *testing.T) {
	t.Setenv("GITBOX_RUNNER_PROBE", "1")
	has := func(env []string, kv string) bool { return slices.Contains(env, kv) }

	nonInteractive := Request{}.environ()
	for _, kv := range []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_ASKPASS=", "GITBOX_RUNNER_PROBE=1"} {
		if !has(nonInteractive, kv) {
			t.Errorf("the default environment lacks %q", kv)
		}
	}
	inherit := Request{Env: Inherit}.environ()
	if has(inherit, "GIT_TERMINAL_PROMPT=0") || !has(inherit, "GITBOX_RUNNER_PROBE=1") {
		t.Errorf("Inherit must be the process environment without the prompt settings")
	}
	exact := Request{Env: Inherit, EnvExact: []string{"ONLY=1"}}.environ()
	if len(exact) != 1 || exact[0] != "ONLY=1" {
		t.Errorf("EnvExact = %v, want it used as is", exact)
	}
}

// With the fake in place, the package's functions send the expected commands
// and read the outcome from the Result: no git process runs.
func TestPackageFunctionsUseTheRunner(t *testing.T) {
	const repo = "/work/repo"
	fake := &fakeRunner{Script: func(req Request, _ func() []byte) fakeReply {
		switch cmd := strings.Join(req.Args, " "); {
		case cmd == "rev-parse --abbrev-ref HEAD":
			return fakeReply{Stdout: "main\n"}
		case cmd == "config --unset-all remote.origin.fetch":
			return fakeReply{Exit: 5} // the key is not there
		case cmd == "config --global --get-all credential.helper":
			return fakeReply{Exit: 1} // the key is not there
		case cmd == "config user.name Ana":
			return fakeReply{Stderr: "error: could not lock config file .git/config: File exists\n", Exit: 255}
		case cmd == "pull --ff-only":
			return fakeReply{Stderr: "fatal: Not possible to fast-forward, aborting.\n", Exit: 128}
		case cmd == "fetch --all --prune":
			return fakeReply{Stderr: "fatal: unable to access 'https://user:TOKEN@git.example.org/x.git/': Could not resolve host: git.example.org\n", Exit: 128}
		case strings.HasPrefix(cmd, "clone "):
			return fakeReply{Stderr: "Receiving objects:  50% (5/10)\rReceiving objects: 100% (10/10)\n"}
		}
		return fakeReply{}
	}}
	SetRunnerForTest(t, fake)

	if branch, err := CurrentBranch(repo); err != nil || branch != "main" {
		t.Errorf("CurrentBranch = %q, %v", branch, err)
	}
	if err := ConfigUnsetAll(repo, "remote.origin.fetch"); err != nil {
		t.Errorf("ConfigUnsetAll must read status 5 as nothing to remove: %v", err)
	}
	if values, err := GlobalConfigGetAll("credential.helper"); err != nil || values != nil {
		t.Errorf("GlobalConfigGetAll must read status 1 as absent: %v, %v", values, err)
	}

	err := ConfigSet(repo, "user.name", "Ana")
	if ge, ok := errors.AsType[*GitError](err); !ok || ge.Op != "config" || ge.ExitCode != 255 || ge.Detail != "exit 255" {
		t.Errorf("ConfigSet error = %#v, want a config GitError with status 255", err)
	}

	err = PullQuietContext(context.Background(), repo)
	if ge, ok := errors.AsType[*GitError](err); !ok || ge.Code != CodeNotFastForward || ge.ExitCode != 128 {
		t.Errorf("pull error = %#v, want not_fast_forward with status 128", err)
	}

	_, err = FetchCapturedContext(context.Background(), repo)
	ge, ok := errors.AsType[*GitError](err)
	if !ok || ge.Code != CodeOffline || ge.Host != "git.example.org" || strings.Contains(err.Error(), "TOKEN") {
		t.Errorf("fetch error = %#v, want offline for git.example.org without the URL", err)
	}

	var seen []int
	if err := CloneWithProgress(context.Background(), "https://git.example.org/x.git", "/work/dest", CloneOpts{}, func(p CloneProgress) { seen = append(seen, p.Percent) }); err != nil {
		t.Errorf("clone: %v", err)
	}
	if !slices.Contains(seen, 50) || !slices.Contains(seen, 100) {
		t.Errorf("clone progress = %v, want the percentages git printed", seen)
	}

	for _, req := range fake.Calls() {
		if req.Env != NonInteractive || req.EnvExact != nil {
			t.Errorf("git %v ran with environment policy %v; the package's commands never prompt", req.Args, req.Env)
		}
	}
	if n := len(fake.Calls()); n != 7 {
		t.Errorf("%d commands ran, want 7", n)
	}
}

// SetRunnerForTest puts the previous runner back when the test ends.
func TestSetRunnerForTestRestores(t *testing.T) {
	before := Default
	t.Run("inner", func(t *testing.T) {
		SetRunnerForTest(t, &fakeRunner{})
		if _, ok := Default.(*fakeRunner); !ok {
			t.Fatal("the fake is not in place")
		}
	})
	if Default != before {
		t.Errorf("Default = %T after the test, want it restored", Default)
	}
}

// truncatingRunner answers every command with output that hit the bound.
type truncatingRunner struct{}

func (truncatingRunner) Run(context.Context, Request) (Result, error) {
	return Result{Stdout: []byte("# branch.oid abc\n# branch.head ma"), Truncated: true}, nil
}

// Output that hit the runner's bound is never parsed: half a status must not
// be taken for the whole.
func TestTruncatedOutputIsAnError(t *testing.T) {
	SetRunnerForTest(t, truncatingRunner{})
	_, err := Status("/work/repo")
	ge, ok := errors.AsType[*GitError](err)
	if !ok || !errors.Is(err, errOutputTruncated) || ge.Op != "status" {
		t.Fatalf("Status with cut output = %#v, want a status GitError for truncated output", err)
	}
	if _, err := ReadConfigSnapshot("/work/repo"); !errors.Is(err, errOutputTruncated) {
		t.Errorf("ReadConfigSnapshot with cut output = %v, want the truncation error", err)
	}
}
