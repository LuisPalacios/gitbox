package mirror

import (
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		url          string
		wantHost     string
		wantOwner    string
		wantRepo     string
		wantErr      bool
	}{
		{"https://github.com/LuisPalacios/migra-forgejo.git", "github.com", "LuisPalacios", "migra-forgejo", false},
		{"https://github.com/LuisPalacios/migra-forgejo", "github.com", "LuisPalacios", "migra-forgejo", false},
		{"https://git.parchis.org/infra/homelab.git", "git.parchis.org", "infra", "homelab", false},
		{"https://gitlab.com/group/subgroup/repo.git", "gitlab.com", "group", "subgroup/repo", false},
		{"https://example.com/noslash", "", "", "", true},
		{"not-a-url", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			host, owner, repo, err := parseRemoteURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
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

func TestExtractHost(t *testing.T) {
	if h := extractHost("https://github.com"); h != "github.com" {
		t.Errorf("got %q", h)
	}
	if h := extractHost("https://git.parchis.org"); h != "git.parchis.org" {
		t.Errorf("got %q", h)
	}
}

func TestApplyDiscovery(t *testing.T) {
	cfg := &config.Config{
		Version:  2,
		Global:   config.GlobalConfig{Folder: "~/test"},
		Accounts: map[string]config.Account{
			"forgejo": {Provider: "forgejo", URL: "https://git.example.com", Username: "user", Name: "U", Email: "u@e"},
			"github":  {Provider: "github", URL: "https://github.com", Username: "user", Name: "U", Email: "u@e"},
		},
		Sources: make(map[string]config.Source),
	}

	results := []DiscoveryResult{
		{
			MirrorKey:  "forgejo-github",
			AccountSrc: "forgejo",
			AccountDst: "github",
			Discovered: []DiscoveredMirror{
				{RepoKey: "infra/homelab", Direction: "push", Origin: "src", Confidence: "confirmed"},
				{RepoKey: "user/dotfiles", Direction: "pull", Origin: "dst", Confidence: "likely"},
			},
		},
	}

	added, _ := ApplyDiscovery(cfg, results)
	if added != 2 {
		t.Errorf("added = %d, want 2", added)
	}

	m, ok := cfg.Mirrors["forgejo-github"]
	if !ok {
		t.Fatal("mirror group not created")
	}
	if m.AccountSrc != "forgejo" || m.AccountDst != "github" {
		t.Errorf("accounts = %s/%s", m.AccountSrc, m.AccountDst)
	}
	if len(m.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(m.Repos))
	}
	if m.Repos["infra/homelab"].Direction != "push" || m.Repos["infra/homelab"].Origin != "src" {
		t.Errorf("homelab = %+v", m.Repos["infra/homelab"])
	}
	if m.Repos["user/dotfiles"].Direction != "pull" || m.Repos["user/dotfiles"].Origin != "dst" {
		t.Errorf("dotfiles = %+v", m.Repos["user/dotfiles"])
	}
}

func TestApplyIdempotent(t *testing.T) {
	cfg := &config.Config{
		Version:  2,
		Global:   config.GlobalConfig{Folder: "~/test"},
		Accounts: map[string]config.Account{
			"forgejo": {Provider: "forgejo", URL: "https://git.example.com", Username: "user", Name: "U", Email: "u@e"},
			"github":  {Provider: "github", URL: "https://github.com", Username: "user", Name: "U", Email: "u@e"},
		},
		Sources: make(map[string]config.Source),
	}

	results := []DiscoveryResult{
		{
			MirrorKey:  "forgejo-github",
			AccountSrc: "forgejo",
			AccountDst: "github",
			Discovered: []DiscoveredMirror{
				{RepoKey: "infra/homelab", Direction: "push", Origin: "src", Confidence: "confirmed"},
			},
		},
	}

	// Apply twice.
	ApplyDiscovery(cfg, results)
	added2, _ := ApplyDiscovery(cfg, results)

	if added2 != 0 {
		t.Errorf("second apply added %d, want 0 (idempotent)", added2)
	}
	if len(cfg.Mirrors["forgejo-github"].Repos) != 1 {
		t.Error("repos should not be duplicated")
	}
}

func TestFindExistingMirrorKey(t *testing.T) {
	cfg := &config.Config{
		Mirrors: map[string]config.Mirror{
			"my-mirror": {AccountSrc: "a", AccountDst: "b"},
		},
	}

	if k := findExistingMirrorKey(cfg, "a", "b"); k != "my-mirror" {
		t.Errorf("got %q, want my-mirror", k)
	}
	// Reversed order should also match.
	if k := findExistingMirrorKey(cfg, "b", "a"); k != "my-mirror" {
		t.Errorf("reversed: got %q, want my-mirror", k)
	}
	if k := findExistingMirrorKey(cfg, "a", "c"); k != "" {
		t.Errorf("no match: got %q, want empty", k)
	}
}
