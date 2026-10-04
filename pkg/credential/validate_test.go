package credential

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindSSHKey(t *testing.T) {
	dir := t.TempDir()

	// Create a key file.
	keyPath := filepath.Join(dir, "id_ed25519_gh-testuser")
	if err := os.WriteFile(keyPath, []byte("fake-key"), 0600); err != nil {
		t.Fatal(err)
	}

	// Found.
	path, err := FindSSHKey(dir, "gh-testuser", "ed25519")
	if err != nil {
		t.Fatalf("expected to find key, got: %v", err)
	}
	if path != keyPath {
		t.Errorf("expected %s, got %s", keyPath, path)
	}

	// Not found.
	_, err = FindSSHKey(dir, "gh-nobody", "ed25519")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestFindSSHConfigEntry(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	content := `Host gh-MyGitHubUser
    HostName github.com
    User git
    IdentityFile ~/.ssh/id_ed25519_gh-MyGitHubUser

Host gt-myuser
    HostName git.example.org
    User git
`
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Found.
	found, err := FindSSHConfigEntry(dir, "gh-MyGitHubUser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Error("expected to find gh-MyGitHubUser")
	}

	// Found second entry.
	found, err = FindSSHConfigEntry(dir, "gt-myuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Error("expected to find gt-myuser")
	}

	// Not found.
	found, err = FindSSHConfigEntry(dir, "gh-nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected NOT to find gh-nobody")
	}
}

func TestFindSSHConfigEntry_NoFile(t *testing.T) {
	dir := t.TempDir()
	_, err := FindSSHConfigEntry(dir, "anything")
	if err == nil {
		t.Error("expected error when config file doesn't exist")
	}
}
