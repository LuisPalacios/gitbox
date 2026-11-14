package credential

import (
	"os"
	"path/filepath"
	"testing"
)

func auditTestDir(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Dir(CredentialFilePath("example"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	auditSecureTestPath(t, dir)
	return dir
}

func TestAuditCredentialDir_Missing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := AuditCredentialDir(); len(got) != 0 {
		t.Fatalf("missing credentials directory: %+v", got)
	}
	if _, err := os.Lstat(filepath.Dir(CredentialFilePath("example"))); !os.IsNotExist(err) {
		t.Fatalf("audit created its directory: %v", err)
	}
}

func TestAuditCredentialDir_UnexpectedEntry(t *testing.T) {
	dir := auditTestDir(t)
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "not-inspected"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := AuditCredentialDir()
	if len(got) != 1 || got[0].Path != nested || got[0].Problem != "unexpected_type" || got[0].Exposed {
		t.Fatalf("unexpected directory findings: %+v", got)
	}
}

func TestAuditCredentialDir_Symlinks(t *testing.T) {
	for _, rootLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[rootLink], func(t *testing.T) {
			dir := auditTestDir(t)
			target := t.TempDir()
			link := filepath.Join(dir, "linked")
			if rootLink {
				if err := os.Remove(dir); err != nil {
					t.Fatal(err)
				}
				link = dir
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			got := AuditCredentialDir()
			if len(got) != 1 || got[0].Path != link || got[0].Problem != "symlink" || got[0].Exposed {
				t.Fatalf("symlink findings: %+v", got)
			}
		})
	}
}

func TestAuditCredentialDir_PathIsFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Dir(CredentialFilePath("example"))
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := AuditCredentialDir()
	if len(got) != 1 || got[0].Path != dir || got[0].Problem != "unexpected_type" {
		t.Fatalf("non-directory findings: %+v", got)
	}
}
