package git

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const credentialScheduleEnv = "GITBOX_TEST_CREDENTIAL_SCHEDULE"

// This runs in the test binary only when Git invokes the fixture helper.
func runCredentialScheduleHelper(url string) int {
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		return 1
	}
	response, err := http.Get(url)
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	if _, err := io.Copy(os.Stdout, response.Body); err != nil {
		return 1
	}
	return 0
}

func installCredentialScheduleHelper(t *testing.T, clone, controlURL string) {
	t.Helper()
	t.Setenv(credentialScheduleEnv, controlURL)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shellQuote := func(s string) string {
		return "'" + strings.ReplaceAll(filepath.ToSlash(s), "'", "'\"'\"'") + "'"
	}
	script := filepath.Join(t.TempDir(), "fixture helper.sh")
	writeFile(t, script, "#!/bin/sh\n[ \"$1\" = get ] || exit 0\nexec "+shellQuote(executable)+"\n")
	runGit(t, clone, "config", "--add", "credential.helper", "")
	runGit(t, clone, "config", "--add", "credential.helper", "!sh "+shellQuote(script))
}

func TestCredentialSchedule_FetchAndPullPreserveRefs(t *testing.T) {
	for _, op := range []string{"fetch", "pull"} {
		for _, outcome := range []string{"unavailable", "cancelled", "deadline"} {
			t.Run(op+"/"+outcome, func(t *testing.T) {
				isolateGit(t)
				t.Setenv("NO_PROXY", "127.0.0.1")
				t.Setenv("no_proxy", "127.0.0.1")
				clone, bare := initTestRepo(t)
				writer := t.TempDir()
				runGit(t, writer, "clone", bare, ".")
				runGit(t, writer, "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
					"commit", "--allow-empty", "-m", "new upstream commit")
				runGit(t, writer, "push", "origin", "HEAD")
				runGit(t, bare, "update-server-info")
				repo := http.FileServer(http.Dir(bare))
				entered := make(chan struct{}, 1)
				helperStopped := make(chan struct{}, 1)
				finish := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.URL.Path == "/helper" {
						entered <- struct{}{}
						if outcome == "unavailable" {
							w.WriteHeader(http.StatusServiceUnavailable)
							return
						}
						select {
						case <-req.Context().Done():
							helperStopped <- struct{}{}
						case <-finish:
						}
						return
					}
					if _, _, ok := req.BasicAuth(); !ok {
						w.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					repo.ServeHTTP(w, req)
				}))
				defer server.Close()
				defer close(finish)
				installCredentialScheduleHelper(t, clone, server.URL+"/helper")
				if err := SetRemoteURL(clone, "origin", server.URL); err != nil {
					t.Fatal(err)
				}
				before, err := output(clone, "show-ref")
				if err != nil {
					t.Fatal(err)
				}
				timeout := 30 * time.Second
				if outcome == "deadline" {
					// Let the helper throttle this request past the command deadline.
					timeout = 5 * time.Second
				}
				ctx, cancel := context.WithTimeout(t.Context(), timeout)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					if op == "pull" {
						done <- PullQuietContext(ctx, clone)
					} else {
						_, err := FetchCapturedContext(ctx, clone, "origin")
						done <- err
					}
				}()
				select {
				case <-entered:
				case err := <-done:
					// The fast refusal and the entry notification can both be ready.
					select {
					case <-entered:
						done <- err
					default:
						t.Fatalf("git finished before invoking its credential helper: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("Git did not invoke the credential helper before the deadline")
				}
				if outcome == "cancelled" {
					cancel()
				}

				select {
				case err := <-done:
					want := CodeAuthFailed
					if outcome == "cancelled" {
						want = CodeCancelled
					}
					if outcome == "deadline" {
						want = CodeTimeout
					}
					if ge, ok := errors.AsType[*GitError](err); !ok || ge.Code != want {
						t.Fatalf("%s error = %v, want %s", op, err, want)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("Git did not return after credential failure")
				}
				if outcome != "unavailable" {
					select {
					case <-helperStopped:
					case <-time.After(10 * time.Second):
						t.Fatal("credential helper survived Git cancellation")
					}
				}
				after, err := output(clone, "show-ref")
				if err != nil || after != before {
					t.Fatalf("Git changed refs after credential failure: before=%q, after=%q, error=%v", before, after, err)
				}
				if worktree, err := output(clone, "status", "--porcelain"); err != nil || worktree != "" {
					t.Fatalf("Git changed the working tree after credential failure: %q, %v", worktree, err)
				}
			})
		}
	}
}
