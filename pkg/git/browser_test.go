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
