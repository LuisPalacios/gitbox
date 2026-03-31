package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo creates a bare git repo with one commit, then clones it.
// Returns (clonePath, barePath).
func initTestRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()

	// Create a bare repo to act as "remote".
	barePath := filepath.Join(dir, "remote.git")
	runGit(t, dir, "init", "--bare", barePath)

	// Clone it to get a working copy.
	clonePath := filepath.Join(dir, "clone")
	runGit(t, dir, "clone", barePath, clonePath)

	// Configure user for commits.
	runGit(t, clonePath, "config", "user.name", "Test")
	runGit(t, clonePath, "config", "user.email", "test@test.com")

	// Create initial commit so we have a branch.
	writeFile(t, filepath.Join(clonePath, "README.md"), "# test\n")
	runGit(t, clonePath, "add", "README.md")
	runGit(t, clonePath, "commit", "-m", "initial")
	runGit(t, clonePath, "push", "origin", "master")

	return clonePath, barePath
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsRepo(t *testing.T) {
	clonePath, barePath := initTestRepo(t)

	if !IsRepo(clonePath) {
		t.Error("clone path should be a repo")
	}
	if !IsRepo(barePath) {
		t.Error("bare path should be a repo")
	}
	if IsRepo(t.TempDir()) {
		t.Error("random dir should not be a repo")
	}
}

func TestStatusClean(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	st, err := Status(clonePath)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Branch != "master" {
		t.Errorf("branch = %q, want master", st.Branch)
	}
	if st.Modified != 0 || st.Untracked != 0 || st.Conflicts != 0 {
		t.Errorf("expected clean status, got modified=%d untracked=%d conflicts=%d",
			st.Modified, st.Untracked, st.Conflicts)
	}
	if st.Ahead != 0 || st.Behind != 0 {
		t.Errorf("expected 0 ahead/behind, got %d/%d", st.Ahead, st.Behind)
	}
}

func TestStatusDirty(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	// Modify a tracked file.
	writeFile(t, filepath.Join(clonePath, "README.md"), "# changed\n")
	// Add an untracked file.
	writeFile(t, filepath.Join(clonePath, "new.txt"), "new\n")

	st, err := Status(clonePath)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Modified == 0 {
		t.Error("expected modified > 0")
	}
	if st.Untracked == 0 {
		t.Error("expected untracked > 0")
	}
}

func TestStatusAheadBehind(t *testing.T) {
	clonePath, barePath := initTestRepo(t)

	// Create a commit locally (ahead).
	writeFile(t, filepath.Join(clonePath, "local.txt"), "local\n")
	runGit(t, clonePath, "add", "local.txt")
	runGit(t, clonePath, "commit", "-m", "local commit")

	st, err := Status(clonePath)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Ahead != 1 {
		t.Errorf("ahead = %d, want 1", st.Ahead)
	}

	// Push it, then create a commit in a second clone (behind).
	runGit(t, clonePath, "push", "origin", "master")

	dir := t.TempDir()
	clone2 := filepath.Join(dir, "clone2")
	runGit(t, dir, "clone", barePath, clone2)
	runGit(t, clone2, "config", "user.name", "Test")
	runGit(t, clone2, "config", "user.email", "test@test.com")

	// Push a new commit from clone2.
	writeFile(t, filepath.Join(clone2, "remote.txt"), "remote\n")
	runGit(t, clone2, "add", "remote.txt")
	runGit(t, clone2, "commit", "-m", "remote commit")
	runGit(t, clone2, "push", "origin", "master")

	// Fetch in original clone — now it should be behind.
	runGit(t, clonePath, "fetch", "--all")
	st2, err := Status(clonePath)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st2.Behind != 1 {
		t.Errorf("behind = %d, want 1", st2.Behind)
	}
}

func TestClone(t *testing.T) {
	_, barePath := initTestRepo(t)

	dest := filepath.Join(t.TempDir(), "new-clone")
	err := Clone(barePath, dest, CloneOpts{})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if !IsRepo(dest) {
		t.Error("cloned path should be a repo")
	}
}

func TestRemoteURL(t *testing.T) {
	clonePath, barePath := initTestRepo(t)

	url, err := RemoteURL(clonePath)
	if err != nil {
		t.Fatalf("RemoteURL: %v", err)
	}
	// The URL should match the bare repo path.
	if url != barePath {
		t.Errorf("remote url = %q, want %q", url, barePath)
	}
}

func TestConfigGetSet(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	if err := ConfigSet(clonePath, "user.name", "New Name"); err != nil {
		t.Fatalf("ConfigSet: %v", err)
	}
	val, err := ConfigGet(clonePath, "user.name")
	if err != nil {
		t.Fatalf("ConfigGet: %v", err)
	}
	if val != "New Name" {
		t.Errorf("config user.name = %q, want New Name", val)
	}
}

func TestCurrentBranch(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	branch, err := CurrentBranch(clonePath)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "master" {
		t.Errorf("branch = %q, want master", branch)
	}
}

func TestParseStatus(t *testing.T) {
	input := `# branch.oid abc123
# branch.head main
# branch.upstream origin/main
# branch.ab +2 -1
1 .M N... 100644 100644 100644 abc def file.go
? untracked.txt
? another.txt
u UU N... 100644 100644 100644 100644 abc def ghi conflict.go
`
	s := parseStatus(input)
	if s.Branch != "main" {
		t.Errorf("branch = %q, want main", s.Branch)
	}
	if s.Upstream != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", s.Upstream)
	}
	if s.Ahead != 2 {
		t.Errorf("ahead = %d, want 2", s.Ahead)
	}
	if s.Behind != 1 {
		t.Errorf("behind = %d, want 1", s.Behind)
	}
	if s.Modified != 1 {
		t.Errorf("modified = %d, want 1", s.Modified)
	}
	if s.Untracked != 2 {
		t.Errorf("untracked = %d, want 2", s.Untracked)
	}
	if s.Conflicts != 1 {
		t.Errorf("conflicts = %d, want 1", s.Conflicts)
	}
}
