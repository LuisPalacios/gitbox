package git

import "testing"

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		url       string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		// HTTPS
		{"https://github.com/LuisPalacios/gitbox.git", "github.com", "LuisPalacios", "gitbox", false},
		{"https://github.com/LuisPalacios/gitbox", "github.com", "LuisPalacios", "gitbox", false},
		{"https://user@github.com/LuisPalacios/gitbox.git", "github.com", "LuisPalacios", "gitbox", false},
		{"https://user:token@github.com/LuisPalacios/gitbox.git", "github.com", "LuisPalacios", "gitbox", false},
		{"https://git.parchis.org/infra/homelab.git", "git.parchis.org", "infra", "homelab", false},

		// HTTPS — GitLab nested groups
		{"https://gitlab.com/group/subgroup/repo.git", "gitlab.com", "group/subgroup", "repo", false},
		{"https://gitlab.com/a/b/c/repo.git", "gitlab.com", "a/b/c", "repo", false},

		// SSH
		{"git@github.com:LuisPalacios/gitbox.git", "github.com", "LuisPalacios", "gitbox", false},
		{"git@github.com:LuisPalacios/gitbox", "github.com", "LuisPalacios", "gitbox", false},
		{"git@gitlab.com:group/subgroup/repo.git", "gitlab.com", "group/subgroup", "repo", false},
		{"git@git.parchis.org:infra/homelab.git", "git.parchis.org", "infra", "homelab", false},

		// SSH — custom host alias (gitbox convention)
		{"git@gitbox-github-luis:LuisPalacios/gitbox.git", "gitbox-github-luis", "LuisPalacios", "gitbox", false},

		// Errors
		{"https://example.com/noslash", "", "", "", true},
		{"not-a-url", "", "", "", true},
		{"", "", "", "", true},
		{"git@github.com:", "", "", "", true},             // SSH missing path
		{"git@host:repoonly", "", "", "", true},            // SSH no owner
		{"https://github.com/", "", "", "", true},          // HTTPS empty path
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			host, owner, repo, err := ParseRemoteURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got host=%q owner=%q repo=%q", host, owner, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestSplitOwnerRepo(t *testing.T) {
	tests := []struct {
		path      string
		wantOwner string
		wantRepo  string
	}{
		{"user/repo", "user", "repo"},
		{"group/sub/repo", "group/sub", "repo"},
		{"a/b/c/d", "a/b/c", "d"},
		{"noslash", "", "noslash"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			owner, repo := splitOwnerRepo(tt.path)
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}
