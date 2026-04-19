package git

import "testing"

func TestRepoWebURL(t *testing.T) {
	tests := []struct {
		name       string
		accountURL string
		repoKey    string
		want       string
	}{
		{"github", "https://github.com", "LuisPalacios/gitbox", "https://github.com/LuisPalacios/gitbox"},
		{"gitlab", "https://gitlab.com", "org/project", "https://gitlab.com/org/project"},
		{"gitea", "https://gitea.example.com", "alice/repo", "https://gitea.example.com/alice/repo"},
		{"forgejo", "https://codeberg.org", "user/cool-project", "https://codeberg.org/user/cool-project"},
		{"bitbucket", "https://bitbucket.org", "workspace/repo", "https://bitbucket.org/workspace/repo"},
		{"self-hosted with port", "https://git.internal:3000", "team/service", "https://git.internal:3000/team/service"},
		{"trailing slash", "https://github.com/", "org/repo", "https://github.com/org/repo"},
		{"multiple trailing slashes", "https://github.com///", "org/repo", "https://github.com/org/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RepoWebURL(tt.accountURL, tt.repoKey)
			if got != tt.want {
				t.Errorf("RepoWebURL(%q, %q) = %q, want %q", tt.accountURL, tt.repoKey, got, tt.want)
			}
		})
	}
}

func TestAccountProfileURL(t *testing.T) {
	tests := []struct {
		name       string
		accountURL string
		username   string
		want       string
	}{
		{"github user", "https://github.com", "LuisPalacios", "https://github.com/LuisPalacios"},
		{"github org", "https://github.com", "Sumwall", "https://github.com/Sumwall"},
		{"gitlab user", "https://gitlab.com", "alice", "https://gitlab.com/alice"},
		{"gitea", "https://gitea.example.com", "bob", "https://gitea.example.com/bob"},
		{"forgejo", "https://codeberg.org", "team", "https://codeberg.org/team"},
		{"bitbucket", "https://bitbucket.org", "workspace", "https://bitbucket.org/workspace"},
		{"self-hosted with port", "https://git.internal:3000", "team", "https://git.internal:3000/team"},
		{"trailing slash trimmed", "https://github.com/", "user", "https://github.com/user"},
		{"multiple trailing slashes", "https://github.com///", "user", "https://github.com/user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AccountProfileURL(tt.accountURL, tt.username)
			if got != tt.want {
				t.Errorf("AccountProfileURL(%q, %q) = %q, want %q", tt.accountURL, tt.username, got, tt.want)
			}
		})
	}
}
