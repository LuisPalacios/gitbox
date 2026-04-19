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

func TestCredentialUsernames(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	// No credential.*.username keys set → expect nil.
	if names := CredentialUsernames(clonePath); len(names) != 0 {
		t.Errorf("expected no usernames, got %v", names)
	}

	// Set one credential.<url>.username and one unrelated credential.<url>.* key.
	if err := ConfigSet(clonePath, "credential.https://example.com.username", "user-a"); err != nil {
		t.Fatalf("ConfigSet: %v", err)
	}
	if err := ConfigSet(clonePath, "credential.https://example.com.helper", "manager"); err != nil {
		t.Fatalf("ConfigSet: %v", err)
	}
	if err := ConfigSet(clonePath, "credential.https://other.example.com.username", "user-b"); err != nil {
		t.Fatalf("ConfigSet: %v", err)
	}

	names := CredentialUsernames(clonePath)
	if len(names) != 2 {
		t.Fatalf("expected 2 usernames, got %d: %v", len(names), names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	if !seen["user-a"] || !seen["user-b"] {
		t.Errorf("expected user-a and user-b, got %v", names)
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

// ─── Sweep tests ──────────────────────────────────────────────

func TestDefaultBranch(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	// initTestRepo uses "master" as default.
	branch, err := DefaultBranch(clonePath)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if branch != "master" {
		t.Errorf("DefaultBranch = %q, want master", branch)
	}
}

func TestSweepBranches_Merged(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	// Create a feature branch, add a commit, merge it back to master.
	runGit(t, clonePath, "checkout", "-b", "feature-x")
	writeFile(t, filepath.Join(clonePath, "feature.txt"), "feat\n")
	runGit(t, clonePath, "add", "feature.txt")
	runGit(t, clonePath, "commit", "-m", "feature work")
	runGit(t, clonePath, "checkout", "master")
	runGit(t, clonePath, "merge", "feature-x", "--no-ff", "-m", "merge feature-x")

	result, err := SweepBranches(clonePath)
	if err != nil {
		t.Fatalf("SweepBranches: %v", err)
	}
	if len(result.Merged) != 1 || result.Merged[0] != "feature-x" {
		t.Errorf("Merged = %v, want [feature-x]", result.Merged)
	}
	if len(result.Gone) != 0 {
		t.Errorf("Gone = %v, want []", result.Gone)
	}
}

func TestSweepBranches_Gone(t *testing.T) {
	clonePath, barePath := initTestRepo(t)

	// Create a branch, push it, then delete it on the remote.
	runGit(t, clonePath, "checkout", "-b", "old-branch")
	writeFile(t, filepath.Join(clonePath, "old.txt"), "old\n")
	runGit(t, clonePath, "add", "old.txt")
	runGit(t, clonePath, "commit", "-m", "old branch work")
	runGit(t, clonePath, "push", "-u", "origin", "old-branch")
	runGit(t, clonePath, "checkout", "master")

	// Delete the branch on the bare remote.
	runGit(t, barePath, "branch", "-D", "old-branch")

	// Fetch with prune so the tracking ref is gone.
	runGit(t, clonePath, "fetch", "--prune")

	result, err := SweepBranches(clonePath)
	if err != nil {
		t.Fatalf("SweepBranches: %v", err)
	}
	if len(result.Gone) != 1 || result.Gone[0] != "old-branch" {
		t.Errorf("Gone = %v, want [old-branch]", result.Gone)
	}
}

func TestSweepBranches_ProtectsCurrentAndDefault(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	// We're on master (which is also default). It should never appear.
	result, err := SweepBranches(clonePath)
	if err != nil {
		t.Fatalf("SweepBranches: %v", err)
	}
	for _, b := range result.Merged {
		if b == "master" {
			t.Error("master should not be in Merged")
		}
	}
	for _, b := range result.Gone {
		if b == "master" {
			t.Error("master should not be in Gone")
		}
	}
}

func TestDeleteStaleBranches(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	// Create and merge two feature branches.
	for _, name := range []string{"feat-a", "feat-b"} {
		runGit(t, clonePath, "checkout", "-b", name)
		writeFile(t, filepath.Join(clonePath, name+".txt"), name+"\n")
		runGit(t, clonePath, "add", name+".txt")
		runGit(t, clonePath, "commit", "-m", "work on "+name)
		runGit(t, clonePath, "checkout", "master")
		runGit(t, clonePath, "merge", name, "--no-ff", "-m", "merge "+name)
	}

	result, err := SweepBranches(clonePath)
	if err != nil {
		t.Fatalf("SweepBranches: %v", err)
	}
	if len(result.Merged) != 2 {
		t.Fatalf("expected 2 merged, got %d: %v", len(result.Merged), result.Merged)
	}

	deleted, errs := DeleteStaleBranches(clonePath, result)
	if len(errs) > 0 {
		t.Fatalf("DeleteStaleBranches errors: %v", errs)
	}
	if len(deleted) != 2 {
		t.Errorf("deleted = %v, want 2 branches", deleted)
	}

	// Verify branches no longer exist.
	for _, name := range []string{"feat-a", "feat-b"} {
		cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+name)
		cmd.Dir = clonePath
		if err := cmd.Run(); err == nil {
			t.Errorf("branch %s should have been deleted", name)
		}
	}
}

func TestSweepBranches_SquashMerged(t *testing.T) {
	clonePath, barePath := initTestRepo(t)

	// Create a feature branch with a commit.
	runGit(t, clonePath, "checkout", "-b", "feature-squashed")
	writeFile(t, filepath.Join(clonePath, "squash.txt"), "squashed work\n")
	runGit(t, clonePath, "add", "squash.txt")
	runGit(t, clonePath, "commit", "-m", "feature: squashed work")
	runGit(t, clonePath, "push", "-u", "origin", "feature-squashed")

	// Simulate a squash-merge on the server: create a NEW commit on master
	// with the same changes but a different SHA (as GitHub "Squash and merge" does).
	runGit(t, clonePath, "checkout", "master")
	// Apply the same file change as a new commit (not a merge).
	writeFile(t, filepath.Join(clonePath, "squash.txt"), "squashed work\n")
	runGit(t, clonePath, "add", "squash.txt")
	runGit(t, clonePath, "commit", "-m", "feature: squashed work (#1)")
	runGit(t, clonePath, "push", "origin", "master")

	// Delete the remote branch (as GitHub does after squash-merge).
	runGit(t, barePath, "branch", "-D", "feature-squashed")
	runGit(t, clonePath, "fetch", "--prune")

	result, err := SweepBranches(clonePath)
	if err != nil {
		t.Fatalf("SweepBranches: %v", err)
	}

	// The branch should be detected as either gone or squashed (or both).
	// Since remote was deleted, it will be "gone". But it also qualifies as squashed.
	// Our de-dup puts gone branches into Gone, skipping them from Squashed.
	found := false
	for _, b := range result.Gone {
		if b == "feature-squashed" {
			found = true
		}
	}
	for _, b := range result.Squashed {
		if b == "feature-squashed" {
			found = true
		}
	}
	if !found {
		t.Errorf("feature-squashed not found in Gone or Squashed.\nGone=%v\nSquashed=%v\nMerged=%v",
			result.Gone, result.Squashed, result.Merged)
	}
}

func TestSweepBranches_SquashMergedLocalOnly(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	// Create a feature branch with a commit (never pushed).
	runGit(t, clonePath, "checkout", "-b", "local-squash")
	writeFile(t, filepath.Join(clonePath, "local-sq.txt"), "local squash\n")
	runGit(t, clonePath, "add", "local-sq.txt")
	runGit(t, clonePath, "commit", "-m", "local squash work")

	// Simulate squash-merge: apply same changes to master as a different commit.
	runGit(t, clonePath, "checkout", "master")
	writeFile(t, filepath.Join(clonePath, "local-sq.txt"), "local squash\n")
	runGit(t, clonePath, "add", "local-sq.txt")
	runGit(t, clonePath, "commit", "-m", "squashed: local squash work")

	result, err := SweepBranches(clonePath)
	if err != nil {
		t.Fatalf("SweepBranches: %v", err)
	}

	// local-squash was never pushed (no upstream), not git-merged, but its
	// changes are in master via the squash commit → should be in Squashed.
	found := false
	for _, b := range result.Squashed {
		if b == "local-squash" {
			found = true
		}
	}
	if !found {
		t.Errorf("local-squash not found in Squashed. Squashed=%v, Merged=%v, Gone=%v",
			result.Squashed, result.Merged, result.Gone)
	}
}

func TestSweepBranches_NothingToSweep(t *testing.T) {
	clonePath, _ := initTestRepo(t)

	result, err := SweepBranches(clonePath)
	if err != nil {
		t.Fatalf("SweepBranches: %v", err)
	}
	if len(result.Merged) != 0 {
		t.Errorf("Merged = %v, want []", result.Merged)
	}
	if len(result.Gone) != 0 {
		t.Errorf("Gone = %v, want []", result.Gone)
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
