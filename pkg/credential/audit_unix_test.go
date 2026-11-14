//go:build !windows

package credential

import (
	"os"
	"path/filepath"
	"testing"
)

func auditSecureTestPath(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0o600)
	if info.IsDir() {
		mode = 0o700
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestAuditCredentialDir_UnixPermissions(t *testing.T) {
	dir := auditTestDir(t)
	file := filepath.Join(dir, "example")
	if err := os.WriteFile(file, []byte("fixture-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := AuditCredentialDir(); len(got) != 0 {
		t.Fatalf("private directory and file: %+v", got)
	}
	for _, tc := range []struct {
		name    string
		path    string
		mode    os.FileMode
		problem string
		exposed bool
	}{
		{"file-readable", file, 0o644, "file_mode", true},
		{"file-unreadable", file, 0, "file_mode", false},
		{"directory-readable", dir, 0o755, "directory_mode", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.Chmod(tc.path, tc.mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { auditSecureTestPath(t, tc.path) })
			got := AuditCredentialDir()
			if len(got) != 1 || got[0].Path != tc.path || got[0].Problem != tc.problem || got[0].Exposed != tc.exposed {
				t.Fatalf("permission findings: %+v", got)
			}
			info, err := os.Lstat(tc.path)
			if err != nil || info.Mode().Perm() != tc.mode {
				t.Fatalf("audit changed permissions: %v, %v", info, err)
			}
		})
	}
}

func TestAuditCredentialDir_UnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read directories without read permission")
	}
	dir := auditTestDir(t)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { auditSecureTestPath(t, dir) })
	got := AuditCredentialDir()
	if len(got) != 2 || got[1].Problem != "unverifiable" || got[1].Exposed {
		t.Fatalf("unreadable directory findings: %+v", got)
	}
}
