package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// canaryURL carries a token the way a remote URL can. Port 1 on loopback
// refuses the connection at once, so the commands fail without a network.
const (
	canaryToken = "CANARY-TOKEN-165"
	canaryURL   = "https://user:" + canaryToken + "@127.0.0.1:1/owner/x.git"
)

// assertSecretFree fails when any text an error exposes holds the token, the
// URL or its host.
func assertSecretFree(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	texts := map[string]string{"Error()": err.Error(), "%v": fmt.Sprintf("%v", err), "%+v": fmt.Sprintf("%+v", err)}
	// Every error in the chain, too: a caller may print what it unwraps.
	for e, i := errors.Unwrap(err), 1; e != nil; e, i = errors.Unwrap(e), i+1 {
		texts[fmt.Sprintf("unwrapped %d", i)] = e.Error()
	}
	if ge, ok := errors.AsType[*GitError](err); ok {
		texts["Detail"], texts["Hint"], texts["Op"] = ge.Detail, ge.Hint, ge.Op
	}
	for name, text := range texts {
		for _, secret := range []string{canaryToken, "127.0.0.1", "user:", "x.git"} {
			if strings.Contains(text, secret) {
				t.Errorf("%s leaks %q: %s", name, secret, text)
			}
		}
	}
}

func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestGitErrorsCarryNoSecrets(t *testing.T) {
	isolateGit(t)

	t.Run("clone", func(t *testing.T) {
		err := CloneWithProgress(canaryURL, filepath.Join(t.TempDir(), "dest"), CloneOpts{}, func(CloneProgress) {})
		assertSecretFree(t, err)
		ge, ok := errors.AsType[*GitError](err)
		if !ok || ge.Op != "clone" {
			t.Fatalf("clone error = %#v, want a *GitError for clone", err)
		}
		if ge.Code != CodeOffline {
			t.Errorf("code = %q, want %q (git said: %s)", ge.Code, CodeOffline, ge.stderr)
		}
		if !strings.Contains(ge.stderr, "127.0.0.1") {
			t.Errorf("the raw output must stay available to the classifiers, got %q", ge.stderr)
		}
	})

	t.Run("fetch", func(t *testing.T) {
		repo := t.TempDir()
		if err := run(repo, "init", "--quiet"); err != nil {
			t.Fatal(err)
		}
		if err := run(repo, "remote", "add", "origin", canaryURL); err != nil {
			t.Fatal(err)
		}
		_, err := FetchCapturedContext(context.Background(), repo)
		assertSecretFree(t, err)
		if ge, ok := errors.AsType[*GitError](err); !ok || ge.Op != "fetch" {
			t.Errorf("fetch error = %#v, want a *GitError for fetch", err)
		}
		assertSecretFree(t, run(repo, "fetch", "--prune", "--tags"))
	})

	t.Run("arguments", func(t *testing.T) {
		repo := t.TempDir()
		assertSecretFree(t, run(repo, "-c", "http.extraHeader=Authorization: Bearer "+canaryToken, "ls-remote", canaryURL))
		_, err := output(repo, "ls-remote", canaryURL)
		assertSecretFree(t, err)
		_, err = PushMirror(repo, canaryURL, []string{"credential.helper="})
		assertSecretFree(t, err)
	})

	t.Run("cancelled fetch", func(t *testing.T) {
		repo := t.TempDir()
		if err := run(repo, "init", "--quiet"); err != nil {
			t.Fatal(err)
		}
		if err := run(repo, "remote", "add", "origin", canaryURL); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := FetchCapturedContext(ctx, repo)
		assertSecretFree(t, err)
		if ge, ok := errors.AsType[*GitError](err); !ok || ge.Code != CodeCancelled {
			t.Errorf("cancelled fetch = %#v, want code %q", err, CodeCancelled)
		}
	})
}

func TestParseRemoteURLErrorsCarryNoSecrets(t *testing.T) {
	for _, raw := range []string{
		"https://user:" + canaryToken + "@/owner/x.git",           // no hostname
		"https://user:" + canaryToken + "@127.0.0.1:1/x.git",      // no owner
		"https://user:" + canaryToken + "@127.0.0.1:1/ow ner/%zz", // malformed
		"user:" + canaryToken + "@127.0.0.1",                      // SSH form, no colon after host
		canaryToken + "@host.example:x.git",                       // SSH form, no owner
	} {
		_, _, _, err := ParseRemoteURL(raw)
		if err == nil {
			t.Errorf("ParseRemoteURL(%q) accepted a malformed URL", raw)
			continue
		}
		for _, secret := range []string{canaryToken, "user:", "x.git"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error leaks %q: %s", secret, err)
			}
		}
	}
}

func TestClassify(t *testing.T) {
	exit := &exec.ExitError{}
	tests := []struct {
		output string
		want   GitCode
	}{
		{"fatal: Authentication failed for 'https://host/x.git/'", CodeAuthFailed},
		{"fatal: could not read Username for 'https://host': terminal prompts disabled", CodeAuthFailed},
		{"git@host: Permission denied (publickey).", CodeAuthFailed},
		{"fatal: Not possible to fast-forward, aborting.", CodeNotFastForward},
		{"There is no tracking information for the current branch.", CodeNoUpstream},
		{"fatal: unable to access 'https://host/': Could not resolve host: host", CodeOffline},
		{"fatal: unable to access 'https://host/': Failed to connect to host port 443 after 5 ms: Couldn't connect to server", CodeOffline},
		{"ssh: connect to host example port 22: Operation timed out", CodeTimeout},
		{"fatal: not a git repository (or any of the parent directories): .git", CodeNotARepo},
		{"fatal: Unable to create '/r/.git/index.lock': File exists.", CodeLockHeld},
		{"remote: Repository not found.", CodeRepoNotFound},
		{"fatal: repository 'https://host/o/r.git/' not found", CodeRepoNotFound},
		{"fatal: unable to access 'https://host/': The requested URL returned error: 404", CodeRepoNotFound},
		{"error: pathspec 'main' did not match any file(s) known to git", CodeUnknown},
		{"", CodeUnknown},
	}
	for _, tt := range tests {
		e := newGitError(nil, "fetch", exit, []byte(tt.output))
		if e.Code != tt.want {
			t.Errorf("classify(%q) = %q, want %q", tt.output, e.Code, tt.want)
		}
		if e.Hint != "error.git."+string(tt.want) {
			t.Errorf("hint = %q for code %q", e.Hint, tt.want)
		}
		if tt.output != "" && strings.Contains(e.Error(), tt.output) {
			t.Errorf("Error() repeats git's output: %s", e.Error())
		}
	}

	deadline, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-deadline.Done()
	if e := newGitError(deadline, "fetch", exit, []byte("fatal: Authentication failed")); e.Code != CodeTimeout {
		t.Errorf("an expired context = %q, want %q", e.Code, CodeTimeout)
	}
	if e := newGitError(nil, "fetch", fmt.Errorf("wrapped: %w", context.Canceled), nil); e.Code != CodeCancelled || !errors.Is(e, context.Canceled) {
		t.Errorf("a cancelled error = %q, errors.Is = %v", e.Code, errors.Is(e, context.Canceled))
	}

	long := newGitError(nil, "fetch", exit, []byte(strings.Repeat("x", maxStderr*2)))
	if len(long.stderr) != maxStderr {
		t.Errorf("kept %d bytes of output, want the %d-byte cap", len(long.stderr), maxStderr)
	}
}

func TestClassifiersReadTypedErrors(t *testing.T) {
	exit := &exec.ExitError{}
	gone := newGitError(nil, "fetch", exit, []byte("remote: Repository not found.\nfatal: repository 'https://host/o/r.git/' not found"))
	refused := newGitError(nil, "fetch", exit, []byte("fatal: unable to access 'https://host/': Failed to connect to host port 443 after 5 ms: Couldn't connect to server"))
	slow := newGitError(nil, "fetch", exit, []byte("fatal: unable to access 'https://host/': Failed to connect to host port 443 after 75002 ms: Timeout"))

	if !IsUpstreamGoneError(gone) || !IsUpstreamGoneError(fmt.Errorf("refresh: %w", gone)) {
		t.Errorf("IsUpstreamGoneError missed a typed repo-not-found error")
	}
	if IsUpstreamGoneError(refused) || IsUpstreamGoneError(nil) {
		t.Errorf("IsUpstreamGoneError matched an unrelated error")
	}
	if !IsUpstreamGoneError(errors.New("HTTP 404")) {
		t.Errorf("IsUpstreamGoneError must still read the message of an untyped error")
	}
	if !IsRefusedFast(refused) || !IsRefusedFast(fmt.Errorf("refresh: %w", refused)) {
		t.Errorf("IsRefusedFast missed a typed fast refusal")
	}
	if IsRefusedFast(slow) || IsRefusedFast(gone) || IsRefusedFast(nil) {
		t.Errorf("IsRefusedFast matched a slow failure or an unrelated error")
	}
}

func TestOpName(t *testing.T) {
	tests := map[string][]string{
		"fetch":   {"fetch", "--all", "--prune"},
		"status":  {"--no-optional-locks", "status", "--porcelain=v2"},
		"push":    {"-c", "credential.helper=", "push", "--mirror", canaryURL},
		"config":  {"config", "--global", "user.name", "x"},
		"command": {"--version"},
	}
	for want, args := range tests {
		if got := opName(args); got != want {
			t.Errorf("opName(%v) = %q, want %q", args, got, want)
		}
	}
}
