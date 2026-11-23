package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func snapshotGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(GitBin(), args...)
	cmd.Dir, cmd.Env = dir, Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestReadConfigSnapshot_IncludesAndMultivalue(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	global := filepath.Join(root, "global")
	if err := os.WriteFile(global, []byte("[credential]\n\thelper = inherited\n[user]\n\tname = global name\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	snapshotGit(t, root, "init")
	included := filepath.Join(root, "included")
	if err := os.WriteFile(included, []byte("[user]\n\tname = included name\n[credential]\n\thelper = included helper\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshotGit(t, root, "config", "include.path", included)
	snapshotGit(t, root, "config", "--add", "credential.helper", "")
	snapshotGit(t, root, "config", "--add", "credential.helper", "manager")
	snapshotGit(t, root, "config", "credential.https://example.org/Case.username", "line one\nline two")
	s, err := ReadConfigSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Get("user.name") != "included name" || len(s.LocalAll("user.name")) != 0 {
		t.Fatalf("include scope lost: %+v", s)
	}
	if got, want := s.All("credential.helper"), []string{"inherited", "included helper", "", "manager"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("effective helper order = %#v, want %#v", got, want)
	}
	if got, want := s.LocalAll("credential.helper"), []string{"", "manager"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("local helper order = %#v, want %#v", got, want)
	}
	if got := s.Get("Credential.https://example.org/Case.Username"); got != "line one\nline two" {
		t.Fatalf("multiline/subsection case lost: %q", got)
	}
}

func TestReadConfigSnapshot_LinkedWorktree(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "missing"))
	snapshotGit(t, root, "init")
	snapshotGit(t, root, "-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "--allow-empty", "-m", "initial")
	worktree := filepath.Join(t.TempDir(), "linked")
	snapshotGit(t, root, "worktree", "add", "-b", "linked", worktree)
	snapshotGit(t, root, "config", "credential.helper", "")
	snapshotGit(t, root, "config", "extensions.worktreeConfig", "true")
	snapshotGit(t, worktree, "config", "--worktree", "user.name", "worktree name")
	s, err := ReadConfigSnapshot(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if s.Get("user.name") != "worktree name" || !reflect.DeepEqual(s.LocalAll("credential.helper"), []string{""}) {
		t.Fatalf("linked worktree settings lost: %+v", s)
	}
}

func TestReadConfigSnapshot_PreservesInspectionOverrides(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "global"))
	snapshotGit(t, root, "init")
	snapshotGit(t, root, "config", "core.hooksPath", "ordinary-hooks")
	snapshotGit(t, root, "config", "core.fsmonitor", "false")
	s, err := ReadConfigSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.All("core.hooksPath"); !reflect.DeepEqual(got, []string{"ordinary-hooks"}) {
		t.Errorf("effective hooks path: %q", got)
	}
	if got := s.All("core.fsmonitor"); !reflect.DeepEqual(got, []string{"false"}) {
		t.Errorf("effective monitor: %q", got)
	}
	if len(s.All("protocol.file.allow")) != 0 || len(s.All("core.sshCommand")) != 0 {
		t.Fatal("inspection settings leaked into the snapshot")
	}
}

func TestHooksInsideChecksPhysicalAncestors(t *testing.T) {
	clone := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(clone, "linked")); err != nil {
		t.Skipf("creating directory symlink: %v", err)
	}
	if hooksInside(clone, filepath.Join("linked", "future-hooks")) || hooksInside(clone, outside) {
		t.Error("accepted hooks outside clone")
	}
	if !hooksInside(clone, filepath.Join(".git", "future-hooks")) {
		t.Error("rejected hooks inside clone")
	}
}
