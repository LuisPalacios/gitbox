package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- GitHub tests ---

func TestGitHubListRepos(t *testing.T) {
	repos := []githubRepo{
		{FullName: "user/repo1", Desc: "First", CloneURL: "https://github.com/user/repo1.git", SSHURL: "git@github.com:user/repo1.git", Private: false, Fork: false},
		{FullName: "user/repo2", Desc: "Second", CloneURL: "https://github.com/user/repo2.git", SSHURL: "git@github.com:user/repo2.git", Private: true, Fork: true, Archived: true},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(repos)
	}))
	defer srv.Close()

	gh := &GitHub{}
	// Use the test server URL as a GitHub Enterprise base URL.
	result, err := gh.ListRepos(context.Background(), srv.URL, "test-token", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(result))
	}
	if result[0].FullName != "user/repo1" {
		t.Errorf("expected user/repo1, got %s", result[0].FullName)
	}
	if !result[1].Private {
		t.Error("expected repo2 to be private")
	}
	if !result[1].Fork {
		t.Error("expected repo2 to be a fork")
	}
	if !result[1].Archived {
		t.Error("expected repo2 to be archived")
	}
}

func TestGitHubPagination(t *testing.T) {
	page1 := make([]githubRepo, 100)
	for i := range page1 {
		page1[i] = githubRepo{FullName: fmt.Sprintf("user/repo%d", i)}
	}
	page2 := []githubRepo{
		{FullName: "user/repo100"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		if page == "" || page == "1" {
			json.NewEncoder(w).Encode(page1)
		} else {
			json.NewEncoder(w).Encode(page2)
		}
	}))
	defer srv.Close()

	gh := &GitHub{}
	result, err := gh.ListRepos(context.Background(), srv.URL, "tok", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 101 {
		t.Fatalf("expected 101 repos, got %d", len(result))
	}
}

func TestGitHubAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad credentials", http.StatusUnauthorized)
	}))
	defer srv.Close()

	gh := &GitHub{}
	_, err := gh.ListRepos(context.Background(), srv.URL, "bad-token", "user")
	if err == nil {
		t.Fatal("expected error for bad auth")
	}
}

// --- Gitea tests ---

func TestGiteaListRepos(t *testing.T) {
	repos := []giteaRepo{
		{FullName: "luis/project1", Desc: "Project", CloneURL: "https://git.example.com/luis/project1.git", SSHURL: "git@git.example.com:luis/project1.git"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(repos)
	}))
	defer srv.Close()

	gt := &Gitea{}
	result, err := gt.ListRepos(context.Background(), srv.URL, "test-token", "testuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(result))
	}
	if result[0].FullName != "luis/project1" {
		t.Errorf("expected luis/project1, got %s", result[0].FullName)
	}
}

// --- GitLab tests ---

func TestGitLabListRepos(t *testing.T) {
	projects := []gitlabProject{
		{PathWithNS: "group/project1", Desc: "Proj", HTTPURL: "https://gitlab.com/group/project1.git", SSHURL: "git@gitlab.com:group/project1.git", Visibility: "private"},
		{PathWithNS: "group/project2", Desc: "Fork", HTTPURL: "https://gitlab.com/group/project2.git", Visibility: "public", ForkedFrom: &struct{ ID int `json:"id"` }{ID: 42}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// No next page.
		json.NewEncoder(w).Encode(projects)
	}))
	defer srv.Close()

	gl := &GitLab{}
	result, err := gl.ListRepos(context.Background(), srv.URL, "test-token", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(result))
	}
	if !result[0].Private {
		t.Error("expected project1 to be private")
	}
	if !result[1].Fork {
		t.Error("expected project2 to be a fork")
	}
	if result[1].Private {
		t.Error("expected project2 to be public")
	}
}

// --- Bitbucket tests ---

func TestBitbucketListRepos(t *testing.T) {
	resp := bitbucketResponse{
		Values: []bitbucketRepo{
			{
				FullName: "user/repo1",
				Desc:     "First",
				IsPriv:   true,
				Links: struct {
					Clone []struct {
						Name string `json:"name"`
						Href string `json:"href"`
					} `json:"clone"`
				}{
					Clone: []struct {
						Name string `json:"name"`
						Href string `json:"href"`
					}{
						{Name: "https", Href: "https://bitbucket.org/user/repo1.git"},
						{Name: "ssh", Href: "git@bitbucket.org:user/repo1.git"},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Override the Bitbucket API URL by using the test server directly.
	// We test with a custom URL since Bitbucket hardcodes api.bitbucket.org.
	// For unit testing, we test the response parsing via a direct HTTP call.
	bb := &Bitbucket{}
	_ = bb // Bitbucket hardcodes the API URL, so we test parsing separately.

	// Test the HTTP parsing directly.
	var result bitbucketResponse
	_, err := doGet(context.Background(), srv.URL, nil, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Values) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(result.Values))
	}
	if result.Values[0].FullName != "user/repo1" {
		t.Errorf("expected user/repo1, got %s", result.Values[0].FullName)
	}
}

// --- Token guide tests ---

func TestTokenSetupGuide(t *testing.T) {
	providers := []struct {
		name     string
		url      string
		contains string
	}{
		{"github", "https://github.com", "/settings/tokens/new"},
		{"gitlab", "https://gitlab.com", "/-/user_settings/personal_access_tokens"},
		{"gitea", "https://git.example.com", "/user/settings/applications"},
		{"forgejo", "https://git.example.com", "/user/settings/applications"},
		{"bitbucket", "https://bitbucket.org", "/account/settings/app-passwords/new"},
	}

	for _, tt := range providers {
		t.Run(tt.name, func(t *testing.T) {
			guide := TokenSetupGuide(tt.name, tt.url, "test-account")
			if guide == "" {
				t.Fatal("guide is empty")
			}
			if !contains(guide, tt.contains) {
				t.Errorf("guide for %s should contain %q:\n%s", tt.name, tt.contains, guide)
			}
			if !contains(guide, "gitboxcmd account credential setup test-account") {
				t.Errorf("guide should contain store command:\n%s", guide)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- ByName factory tests ---

func TestByName(t *testing.T) {
	valid := []string{"github", "gitea", "forgejo", "gitlab", "bitbucket"}
	for _, name := range valid {
		p, err := ByName(name)
		if err != nil {
			t.Errorf("ByName(%q) returned error: %v", name, err)
		}
		if p == nil {
			t.Errorf("ByName(%q) returned nil", name)
		}
	}

	_, err := ByName("generic")
	if err == nil {
		t.Error("ByName(generic) should return error")
	}

	_, err = ByName("unknown")
	if err == nil {
		t.Error("ByName(unknown) should return error")
	}
}
