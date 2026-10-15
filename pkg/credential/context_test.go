package credential

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestCheckContextCancelsInFlightProviderRequest(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	t.Setenv(EnvVarName("test-context"), "test-token")
	acct := config.Account{Provider: "forgejo", URL: server.URL, Username: "account-A", DefaultCredentialType: "token"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan StatusResult, 1)
	go func() { done <- CheckContext(ctx, acct, "test-context", &config.Config{}) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("credential verification never reached the test provider")
	}
	cancel()
	select {
	case result := <-done:
		if result.Overall != StatusOffline || result.Primary != StatusOffline || result.PAT != StatusOffline {
			t.Fatalf("cancelled verification reports broken credentials: %+v", result)
		}
		if !strings.Contains(result.PrimaryDetail, context.Canceled.Error()) {
			t.Fatalf("missing cancellation detail: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("credential verification ignored cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("provider request remained active after cancellation")
	}
}

func TestResolveTokenContextsDoNotUseCredentialsAfterCancellation(t *testing.T) {
	t.Setenv(EnvVarName("test-context"), "fallback-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, kind := range []string{"token", "ssh", "gcm"} {
		t.Run(kind, func(t *testing.T) {
			acct := config.Account{URL: "https://example.invalid", DefaultCredentialType: kind}
			token, _, err := ResolveAPITokenContext(ctx, acct, "test-context")
			if token != "" || !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled resolution returned %q, %v", token, err)
			}
		})
	}
	if _, _, err := ResolveGCMTokenContext(ctx, "https://example.invalid", "account-A"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled GCM resolution returned %v", err)
	}
}

func TestResolveGCMTokenContextCancelsStalledHelper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the isolated credential helper uses a POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	pidPath := filepath.Join(dir, "helper.pid")
	helperPath := filepath.Join(dir, "helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\nprintf '%s\\n' \"$$\" > \"$GITBOX_TEST_HELPER_PID\"\nexec /bin/sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITBOX_TEST_HELPER_PID", pidPath)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "")
	t.Setenv("GIT_CONFIG_KEY_1", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_1", "!'"+strings.ReplaceAll(helperPath, "'", "'\\''")+"'")
	defer func() {
		if data, err := os.ReadFile(pidPath); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				if process, err := os.FindProcess(pid); err == nil {
					_ = process.Kill()
				}
			}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := ResolveGCMTokenContext(ctx, "https://example.invalid", "account-A")
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(pidPath); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("GCM lookup exited before reaching the helper: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("credential helper never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled GCM lookup returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("GCM lookup remained blocked by the helper's output pipe")
	}
}

func TestSSHConnectionContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := TestSSHConnectionContext(ctx, t.TempDir(), "example.invalid"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled SSH check returned %v", err)
	}
}
