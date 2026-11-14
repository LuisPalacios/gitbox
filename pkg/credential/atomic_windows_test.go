package credential

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreToken_ExternalReaderBlocksReplacementSafely(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const key = "account-a"
	if err := StoreToken(key, "previous-token"); err != nil {
		t.Fatal(err)
	}
	path := CredentialFilePath(key)
	// os.Open on Windows does not request FILE_SHARE_DELETE, as with an
	// external reader that denies replacement while it holds the file open.
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if err := StoreToken(key, "next-token"); err == nil {
		t.Fatal("replacement succeeded despite a reader denying delete sharing")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := GetToken(key)
	if err != nil || got != "previous-token" {
		t.Fatalf("previous token changed after blocked replacement: %q, %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != key {
		t.Fatal("blocked replacement left a temporary credential behind")
	}
	if err := StoreToken(key, "next-token"); err != nil {
		t.Fatalf("replacement failed after reader closed: %v", err)
	}
	got, err = GetToken(key)
	if err != nil || got != "next-token" {
		t.Fatalf("token after successful retry = %q, %v", got, err)
	}
}
