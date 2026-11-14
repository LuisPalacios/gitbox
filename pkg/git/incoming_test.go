package git

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func incomingTestRepo(t *testing.T) (string, func(...string) string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "global.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	run := func(args ...string) string {
		t.Helper()
		out, err := output(dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(out)
	}
	run("init", "--initial-branch=main")
	run("config", "user.name", "Test Author")
	run("config", "user.email", "test@example.invalid")
	run("commit", "--allow-empty", "-m", "Shared base")
	run("remote", "add", "origin", "https://example.invalid/repository.git")
	run("update-ref", "refs/remotes/origin/release", "HEAD")
	run("branch", "--set-upstream-to=origin/release", "main")
	return dir, run
}

func TestIncomingCommits_HistoryAndReadOnly(t *testing.T) {
	dir, run := incomingTestRepo(t)
	run("switch", "-c", "remote-work")
	run("commit", "--allow-empty", "--author=Ana García <ana@example.invalid>", "-m", "First change | café")
	first := run("rev-parse", "HEAD")
	run("commit", "--allow-empty", "-m", "Second change")
	second := run("rev-parse", "HEAD")
	run("update-ref", "refs/remotes/origin/release", "HEAD")
	run("switch", "main")
	writeFile(t, filepath.Join(dir, "pending.txt"), "local work\n")
	run("add", "pending.txt")
	indexBefore, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	headBefore := run("rev-parse", "HEAD")
	refsBefore := run("show-ref")
	got, err := IncomingCommits(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []IncomingCommit{
		{Hash: second, Author: "Test Author", Subject: "Second change"},
		{Hash: first, Author: "Ana García", Subject: "First change | café"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("incoming = %#v, want %#v", got, want)
	}
	indexAfter, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if string(indexAfter) != string(indexBefore) || run("rev-parse", "HEAD") != headBefore || run("show-ref") != refsBefore {
		t.Fatal("preview changed the index, HEAD or refs")
	}
	if contents, err := os.ReadFile(filepath.Join(dir, "pending.txt")); err != nil || string(contents) != "local work\n" {
		t.Fatalf("preview changed working files: %q, %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "FETCH_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("preview unexpectedly fetched: %v", err)
	}
	run("commit", "-m", "Local-only change")
	got, err = IncomingCommits(dir)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("diverged incoming = %#v, %v, want %#v", got, err, want)
	}
}

func TestIncomingCommits_EmptyAndMissingUpstream(t *testing.T) {
	dir, run := incomingTestRepo(t)
	got, err := IncomingCommits(dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("up-to-date incoming = %#v, %v", got, err)
	}
	run("commit", "--allow-empty", "-m", "Local only")
	got, err = IncomingCommits(dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("ahead-only incoming = %#v, %v", got, err)
	}
	run("branch", "--unset-upstream")
	if _, err := IncomingCommits(dir); err == nil {
		t.Fatal("missing upstream should report an error")
	}
}

func TestIncomingCommits_AllCommits(t *testing.T) {
	dir, run := incomingTestRepo(t)
	run("switch", "-c", "remote-work")
	const count = 105
	for i := range count {
		run("commit", "--allow-empty", "-m", fmt.Sprintf("Change %03d", i))
	}
	run("update-ref", "refs/remotes/origin/release", "HEAD")
	run("switch", "main")
	got, err := IncomingCommits(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != count {
		t.Fatalf("incoming count = %d, want %d", len(got), count)
	}
	for i, commit := range got {
		if want := fmt.Sprintf("Change %03d", count-i-1); commit.Subject != want {
			t.Fatalf("incoming[%d] = %q, want %q", i, commit.Subject, want)
		}
	}
	run("merge", "--ff-only", "origin/release")
	got, err = IncomingCommits(dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("after update incoming = %#v, %v", got, err)
	}
}

func TestParseIncomingCommits_Records(t *testing.T) {
	for _, malformed := range []string{"hash\x00author\x00subject", "hash\x00author\x00"} {
		if _, err := parseIncomingCommits(malformed); err == nil {
			t.Errorf("accepted incomplete record %q", malformed)
		}
	}
	got, err := parseIncomingCommits("hash\x00Author\x00\x00")
	if err != nil || len(got) != 1 || got[0].Subject != "" {
		t.Fatalf("empty subject = %#v, %v", got, err)
	}
}
