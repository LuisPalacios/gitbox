package credential

import (
	"runtime"
	"testing"
)

func TestDefaultCredentialHelper(t *testing.T) {
	if got := DefaultCredentialHelper(); got != "manager" {
		t.Errorf("DefaultCredentialHelper() = %q, want %q", got, "manager")
	}
}

func TestDefaultCredentialStore(t *testing.T) {
	got := DefaultCredentialStore()
	var want string
	switch runtime.GOOS {
	case "windows":
		want = "wincredman"
	case "darwin":
		want = "keychain"
	default:
		want = "secretservice"
	}
	if got != want {
		t.Errorf("DefaultCredentialStore() on %s = %q, want %q", runtime.GOOS, got, want)
	}
}
