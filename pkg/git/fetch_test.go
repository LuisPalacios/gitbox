package git

import (
	"errors"
	"testing"
)

func TestIsRefusedFast(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"refused in ms", errors.New("git fetch: exit status 128: fatal: unable to access 'https://git.example.lan/a.git/': Failed to connect to git.example.lan port 443 after 5 ms: Couldn't connect to server"), true},
		{"localized git text", errors.New("fatal: no es posible acceder a 'https://h/a.git/': Failed to connect to h port 443 after 999 ms: Couldn't connect to server"), true},
		{"timeout", errors.New("Failed to connect to h port 443 after 75003 ms: Couldn't connect to server"), false},
		{"auth failure", errors.New("git fetch: exit status 128: fatal: Authentication failed"), false},
		{"dns failure", errors.New("fatal: unable to access 'https://h/a.git/': Could not resolve host: h"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRefusedFast(tc.err); got != tc.want {
				t.Fatalf("IsRefusedFast() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFetchUsesSelectedRemote(t *testing.T) {
	clone, origin := initTestRepo(t)
	other := t.TempDir()
	runGit(t, other, "init", "--bare")
	runGit(t, clone, "remote", "add", "other", other)
	runGit(t, clone, "push", "other", "HEAD:refs/heads/other-only")
	runGit(t, clone, "update-ref", "-d", "refs/remotes/other/other-only")
	runGit(t, origin, "update-ref", "refs/heads/origin-only", "HEAD")
	if _, err := FetchCapturedContext(t.Context(), clone, "origin"); err != nil {
		t.Fatal(err)
	}
	runGit(t, clone, "show-ref", "--verify", "refs/remotes/origin/origin-only")
	if err := run(clone, "show-ref", "--verify", "--quiet", "refs/remotes/other/other-only"); exitCode(err) != 1 {
		t.Fatalf("second remote was fetched or the ref check failed: %v", err)
	}
	if _, err := FetchCapturedContext(t.Context(), clone, "--all"); err != nil {
		t.Fatal(err)
	}
	runGit(t, clone, "show-ref", "--verify", "refs/remotes/other/other-only")
}
