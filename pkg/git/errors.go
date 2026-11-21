package git

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// GitCode classifies why a git command failed.
type GitCode string

const (
	CodeAuthFailed     GitCode = "auth_failed"
	CodeNotFastForward GitCode = "not_fast_forward"
	CodeNoUpstream     GitCode = "no_upstream"
	CodeOffline        GitCode = "offline"
	CodeTimeout        GitCode = "timeout"
	CodeCancelled      GitCode = "cancelled"
	CodeNotARepo       GitCode = "not_a_repo"
	CodeLockHeld       GitCode = "lock_held"
	CodeRepoNotFound   GitCode = "repo_not_found"
	CodeUnknown        GitCode = "unknown"
)

// maxStderr bounds the raw output a GitError keeps for diagnostics.
const maxStderr = 4 << 10

// GitError is the error of every git command this package runs. Its text is
// built only from allowlisted parts (the subcommand, the code and the exit
// status), so it is safe to show, log and store: it never holds the command
// arguments, the output of git or a URL, any of which can carry a token.
type GitError struct {
	Op     string  // the git subcommand, such as "fetch"
	Code   GitCode // why it failed
	Hint   string  // catalog key of the explanation for Code
	Detail string  // allowlisted extra, such as "exit 128"; may be empty
	Err    error   // the underlying error, kept for errors.Is and errors.As

	// stderr is what git printed, bounded to maxStderr. It never leaves the
	// package through Error: only the classifiers here read it.
	stderr string
}

func (e *GitError) Error() string {
	msg := "git " + e.Op + ": " + string(e.Code)
	if e.Detail != "" {
		msg += " (" + e.Detail + ")"
	}
	return msg
}

func (e *GitError) Unwrap() error { return e.Err }

// newGitError builds the error for a failed git command. ctx may be nil; when
// set, a cancelled or expired context decides the code. output is whatever
// git printed (stderr, or stdout and stderr combined).
func newGitError(ctx context.Context, op string, err error, output []byte) *GitError {
	if len(output) == 0 {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			output = exit.Stderr
		}
	}
	if len(output) > maxStderr {
		output = output[:maxStderr]
	}
	e := &GitError{Op: op, Err: err, stderr: string(output)}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() >= 0 {
		e.Detail = fmt.Sprintf("exit %d", exit.ExitCode())
	}
	e.Code = classify(ctx, err, strings.ToLower(e.stderr))
	e.Hint = "error.git." + string(e.Code)
	return e
}

// codePatterns maps fragments of git's output to a code, in priority order.
// Matching runs on lower-cased output. The fragments only pick the code; they
// are never copied into the error.
var codePatterns = []struct {
	code      GitCode
	fragments []string
}{
	{CodeAuthFailed, []string{"authentication failed", "could not read username", "could not read password",
		"terminal prompts disabled", "permission denied (publickey", "invalid credentials",
		"http basic: access denied", "error: 401", "error: 403"}},
	{CodeNotFastForward, []string{"not possible to fast-forward", "non-fast-forward", "diverging branches"}},
	{CodeNoUpstream, []string{"no tracking information", "no upstream configured", "has no upstream branch"}},
	{CodeLockHeld, []string{"index.lock", ".lock': file exists"}},
	{CodeNotARepo, []string{"not a git repository"}},
	{CodeTimeout, []string{"timed out", "timeout"}},
	{CodeOffline, []string{"could not resolve host", "failed to connect to", "connection refused",
		"network is unreachable", "no route to host", "connection reset"}},
}

func classify(ctx context.Context, err error, lowerOutput string) GitCode {
	switch {
	case errors.Is(err, context.Canceled), ctx != nil && errors.Is(ctx.Err(), context.Canceled):
		return CodeCancelled
	case errors.Is(err, context.DeadlineExceeded), ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return CodeTimeout
	}
	if upstreamGoneText(lowerOutput) {
		return CodeRepoNotFound
	}
	for _, p := range codePatterns {
		for _, fragment := range p.fragments {
			if strings.Contains(lowerOutput, fragment) {
				return p.code
			}
		}
	}
	return CodeUnknown
}

// upstreamGoneText reports whether lower-cased git output says the remote
// repository does not exist (or the caller may no longer see it):
//
//   - GitHub and generic: "remote: Repository not found."
//   - GitLab, Gitea, Forgejo: "fatal: repository '...' not found", or HTTP 404
//   - Bitbucket: "fatal: repository access denied" once deleted, plus 404
//   - Any provider over HTTPS with a hard 404: "The requested URL returned
//     error: 404"
func upstreamGoneText(msg string) bool {
	return strings.Contains(msg, "repository not found") ||
		strings.Contains(msg, "repository '") && strings.Contains(msg, "' not found") ||
		strings.Contains(msg, "error: 404") ||
		strings.Contains(msg, "http 404") ||
		strings.Contains(msg, "repository access denied")
}

// opName returns the git subcommand in args, skipping global options such as
// "-c key=value" and "--no-optional-locks".
func opName(args []string) string {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-c" || a == "-C":
			i++ // skip its value
		case strings.HasPrefix(a, "-"):
		default:
			return a
		}
	}
	return "command"
}

// diagnosticText is the text the classifiers in this package match on: what
// git printed for a GitError, the message for any other error.
func diagnosticText(err error) string {
	if e, ok := errors.AsType[*GitError](err); ok {
		return e.stderr
	}
	return err.Error()
}

// RedactURL returns raw without its credentials: the password of an http(s)
// URL becomes "***". Anything that is not such a URL comes back unchanged.
func RedactURL(raw string) string {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.User != nil {
		if _, hasPW := u.User.Password(); hasPW {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
	}
	return u.String()
}

// tailBuffer is an io.Writer that keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tailBuffer) Bytes() []byte { return t.buf }
