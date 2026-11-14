package credential

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreToken_FormatsAndPermissions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_TOKEN", "environment-token-must-not-replace-input")
	const key = "account-a"
	t.Setenv(EnvVarName(key), "another-environment-token")
	const token = "fake-token /?#%123"
	path := CredentialFilePath(key)
	if err := StoreToken(key, " \n"+token+"\r\n\n"); err != nil {
		t.Fatal(err)
	}
	assertToken := func(wantLine string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil || string(data) != wantLine || bytes.Count(data, []byte("\n")) != 1 {
			t.Fatalf("credential contents = %q, error = %v", data, err)
		}
		got, err := GetToken(key)
		if err != nil || got != token {
			t.Fatalf("token = %q, error = %v", got, err)
		}
		if runtime.GOOS != "windows" {
			for p, mode := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
				info, err := os.Stat(p)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != mode {
					t.Fatalf("permissions = %o, want %o", info.Mode().Perm(), mode)
				}
			}
		}
	}
	assertToken(token + "\n")
	acct := credentialAccount()
	if err := StoreTokenWithAccount(key, "\n"+token+"\n", acct); err != nil {
		t.Fatal(err)
	}
	const storeLine = "https://account-a:fake-token%20%2F%3F%23%25123@example.org\n"
	assertToken(storeLine)
	// Both writers produce the same store representation from the same token.
	t.Setenv("GIT_TOKEN", "")
	t.Setenv(EnvVarName(key), "")
	if err := StoreToken(key, token); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredentialFile(path, acct, key); err != nil {
		t.Fatal(err)
	}
	assertToken(storeLine)
}

func TestStoreToken_UnchangedPreservesFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const key = "account-a"
	if err := StoreToken(key, "fake-token"); err != nil {
		t.Fatal(err)
	}
	path := CredentialFilePath(key)
	old := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := StoreToken(key, "fake-token\n"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatal("unchanged raw credential was rewritten")
	}
}

func TestCredentialWriter_FailedReplacementPreservesPrevious(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const key = "account-a"
	if err := StoreToken(key, "previous-token"); err != nil {
		t.Fatal(err)
	}
	path := CredentialFilePath(key)
	wantErr := errors.New("replacement interrupted")
	called := false
	err := writeCredentialFileWithReplace(path, []byte("next-token\n"), func(temp, dest string) error {
		called = true
		if dest != path || filepath.Dir(temp) != filepath.Dir(path) {
			t.Fatal("replacement did not use a sibling temporary file")
		}
		data, err := os.ReadFile(temp)
		if err != nil || string(data) != "next-token\n" {
			t.Fatalf("temporary contents = %q, error = %v", data, err)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(temp)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("temporary permissions are not private: %v", err)
			}
		}
		return wantErr
	})
	if !called || !errors.Is(err, wantErr) {
		t.Fatalf("replacement called = %v, error = %v", called, err)
	}
	got, err := GetToken(key)
	if err != nil || got != "previous-token" {
		t.Fatalf("previous token changed after failure: %q, %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != key {
		t.Fatal("failed replacement left a temporary credential behind")
	}
}

func TestStoreToken_ConcurrentReadersDuringRotation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const key = "account-a"
	first, second := strings.Repeat("a", 32768), strings.Repeat("b", 32768)
	if err := StoreToken(key, first); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
					token, err := GetToken(key)
					if err != nil || (token != first && token != second) {
						t.Errorf("reader saw an incomplete credential: length %d, error %v", len(token), err)
						return
					}
				}
			}
		})
	}
	for i := range 20 {
		next := first
		if i%2 == 0 {
			next = second
		}
		if err := StoreToken(key, next); err != nil {
			t.Error(err)
			break
		}
	}
	close(done)
	readers.Wait()
}
