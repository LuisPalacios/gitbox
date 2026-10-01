package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// appWithCreatedRepo seeds an App the way CreateNewRepo leaves config after
// "clone after create": the source is keyed by the account key, carries
// Account, and holds the new repo under its owner/name key with an empty
// Repo. This is the exact shape the #79 row has when its kebab is opened.
func appWithCreatedRepo(accountURL string) *App {
	cfg := &config.Config{
		Version: 2,
		Global:  config.GlobalConfig{Folder: "~/x"},
		Accounts: map[string]config.Account{
			"github-alice": {Provider: "github", URL: accountURL,
				Username: "alice", Name: "Alice", Email: "a@e"},
		},
		Sources: map[string]config.Source{
			"github-alice": {
				Account: "github-alice",
				Repos:   map[string]config.Repo{"alice/newrepo": {}},
			},
		},
	}
	return &App{cfg: cfg, cfgPath: "", mu: sync.Mutex{}}
}

func TestRepoWebURL_PostCreateShape(t *testing.T) {
	cases := map[string]string{
		"https://github.com":            "https://github.com/alice/newrepo",
		"https://github.com/":           "https://github.com/alice/newrepo",
		"https://git.example.com/gitea": "https://git.example.com/gitea/alice/newrepo",
	}
	for accountURL, want := range cases {
		a := appWithCreatedRepo(accountURL)
		got, err := a.repoWebURL("github-alice", "alice/newrepo")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", accountURL, err)
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", accountURL, got, want)
		}
	}
}

func TestOpenRepoInBrowser_ErrorPaths(t *testing.T) {
	// Each case must fail at the config lookup — before anything could
	// launch a browser — with a "not found" error the UI can display.
	t.Run("unknown source", func(t *testing.T) {
		a := appWithCreatedRepo("https://github.com")
		err := a.OpenRepoInBrowser("nope", "alice/newrepo")
		if err == nil || !strings.Contains(err.Error(), `source "nope" not found`) {
			t.Errorf("expected source-not-found error, got %v", err)
		}
	})

	t.Run("unknown repo in source", func(t *testing.T) {
		a := appWithCreatedRepo("https://github.com")
		err := a.OpenRepoInBrowser("github-alice", "alice/other")
		if err == nil || !strings.Contains(err.Error(), `repo "alice/other" not found`) {
			t.Errorf("expected repo-not-found error, got %v", err)
		}
	})

	t.Run("source whose account is missing", func(t *testing.T) {
		a := appWithCreatedRepo("https://github.com")
		delete(a.cfg.Accounts, "github-alice")
		err := a.OpenRepoInBrowser("github-alice", "alice/newrepo")
		if err == nil || !strings.Contains(err.Error(), `account "github-alice" not found`) {
			t.Errorf("expected account-not-found error, got %v", err)
		}
	})
}
