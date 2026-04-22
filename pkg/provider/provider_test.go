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
			if !contains(guide, "gitbox account credential setup test-account") {
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

// --- GitHub RepoCreator tests ---

func TestGitHubCreateRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v3/user/repos" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("missing auth")
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"full_name":"user/new-repo"}`))
	}))
	defer srv.Close()

	gh := &GitHub{}
	if err := gh.CreateRepo(context.Background(), srv.URL, "tok", "", "", "new-repo", "", true); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
}

func TestGitHubRepoExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/repos/user/exists" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"full_name":"user/exists"}`))
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gh := &GitHub{}
	exists, err := gh.RepoExists(context.Background(), srv.URL, "tok", "", "user", "exists")
	if err != nil {
		t.Fatalf("RepoExists: %v", err)
	}
	if !exists {
		t.Error("expected exists=true")
	}

	exists, err = gh.RepoExists(context.Background(), srv.URL, "tok", "", "user", "nope")
	if err != nil {
		t.Fatalf("RepoExists: %v", err)
	}
	if exists {
		t.Error("expected exists=false")
	}
}

// giteaTestHandler wraps a handler to also respond to the auth probe request
// that resolveAuth sends (GET /api/v1/user/repos?limit=1&page=1).
func giteaTestHandler(actual http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth probe — return empty array to confirm token works.
		if r.Method == "GET" && r.URL.Path == "/api/v1/user/repos" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
			return
		}
		actual(w, r)
	}
}

// --- Gitea RepoCreator tests ---

func TestGiteaCreateRepo(t *testing.T) {
	srv := httptest.NewServer(giteaTestHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/user/repos" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"full_name":"user/new-repo"}`))
	}))
	defer srv.Close()

	gt := &Gitea{}
	if err := gt.CreateRepo(context.Background(), srv.URL, "tok", "user", "", "new-repo", "", true); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
}

func TestGiteaRepoExists(t *testing.T) {
	srv := httptest.NewServer(giteaTestHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/repos/user/exists" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"full_name":"user/exists"}`))
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gt := &Gitea{}
	exists, err := gt.RepoExists(context.Background(), srv.URL, "tok", "user", "user", "exists")
	if err != nil {
		t.Fatalf("RepoExists: %v", err)
	}
	if !exists {
		t.Error("expected exists=true")
	}

	exists, err = gt.RepoExists(context.Background(), srv.URL, "tok", "user", "user", "nope")
	if err != nil {
		t.Fatalf("RepoExists: %v", err)
	}
	if exists {
		t.Error("expected exists=false")
	}
}

// --- Gitea PushMirrorProvider tests ---

func TestGiteaCreatePushMirror(t *testing.T) {
	srv := httptest.NewServer(giteaTestHandler(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/repos/luis/project/push_mirrors":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":1}`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/repos/luis/project/push_mirrors-sync":
			w.WriteHeader(http.StatusOK) // sync trigger — best effort
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gt := &Gitea{}
	if err := gt.CreatePushMirror(context.Background(), srv.URL, "tok", "luis", "luis", "project", "https://github.com/luis/project.git", "gh-tok"); err != nil {
		t.Fatalf("CreatePushMirror: %v", err)
	}
}

func TestGiteaListPushMirrors(t *testing.T) {
	mirrors := []giteaPushMirror{
		{ID: 1, RemoteAddr: "https://github.com/luis/project.git", Interval: "8h", SyncOnCommit: true},
		{ID: 2, RemoteAddr: "https://gitlab.com/luis/project.git", Interval: "12h"},
	}
	srv := httptest.NewServer(giteaTestHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mirrors)
	}))
	defer srv.Close()

	gt := &Gitea{}
	result, err := gt.ListPushMirrors(context.Background(), srv.URL, "tok", "luis", "luis", "project")
	if err != nil {
		t.Fatalf("ListPushMirrors: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 mirrors, got %d", len(result))
	}
	if result[0].ID != 1 || result[0].SyncOnCommit != true {
		t.Errorf("mirror[0] = %+v", result[0])
	}
}

func TestGiteaDeletePushMirror(t *testing.T) {
	srv := httptest.NewServer(giteaTestHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/repos/luis/project/push_mirrors/42" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	gt := &Gitea{}
	if err := gt.DeletePushMirror(context.Background(), srv.URL, "tok", "luis", "luis", "project", 42); err != nil {
		t.Fatalf("DeletePushMirror: %v", err)
	}
}

// --- Gitea PullMirrorProvider tests ---

func TestGiteaCreatePullMirror(t *testing.T) {
	srv := httptest.NewServer(giteaTestHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/repos/migrate" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"full_name":"luis/dotfiles"}`))
	}))
	defer srv.Close()

	gt := &Gitea{}
	if err := gt.CreatePullMirror(context.Background(), srv.URL, "tok", "luis", "dotfiles", "https://github.com/user/dotfiles.git", "src-tok", true); err != nil {
		t.Fatalf("CreatePullMirror: %v", err)
	}
}

// --- GitLab RepoCreator tests ---

func TestGitLabCreateRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v4/projects" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			t.Error("missing auth")
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":123}`))
	}))
	defer srv.Close()

	gl := &GitLab{}
	if err := gl.CreateRepo(context.Background(), srv.URL, "tok", "", "", "new-project", "", true); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
}

func TestGitLabRepoExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Go's http server decodes %2F in path, so we see /api/v4/projects/user/exists
		if r.URL.Path == "/api/v4/projects/user/exists" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"path_with_namespace":"user/exists"}`))
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gl := &GitLab{}
	exists, err := gl.RepoExists(context.Background(), srv.URL, "tok", "", "user", "exists")
	if err != nil {
		t.Fatalf("RepoExists: %v", err)
	}
	if !exists {
		t.Error("expected exists=true")
	}

	exists, err = gl.RepoExists(context.Background(), srv.URL, "tok", "", "user", "nope")
	if err != nil {
		t.Fatalf("RepoExists: %v", err)
	}
	if exists {
		t.Error("expected exists=false")
	}
}

// --- GitLab PushMirrorProvider tests ---

func TestGitLabCreatePushMirror(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v4/projects/owner/repo":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":99}`))
		case r.Method == "POST" && r.URL.Path == "/api/v4/projects/99/remote_mirrors":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":1}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gl := &GitLab{}
	if err := gl.CreatePushMirror(context.Background(), srv.URL, "tok", "", "owner", "repo", "https://github.com/owner/repo.git", "gh-tok"); err != nil {
		t.Fatalf("CreatePushMirror: %v", err)
	}
}

func TestGitLabListPushMirrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v4/projects/owner/repo":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":99}`))
		case r.Method == "GET" && r.URL.Path == "/api/v4/projects/99/remote_mirrors":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"id":1,"url":"https://github.com/x.git","enabled":true}]`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gl := &GitLab{}
	mirrors, err := gl.ListPushMirrors(context.Background(), srv.URL, "tok", "", "owner", "repo")
	if err != nil {
		t.Fatalf("ListPushMirrors: %v", err)
	}
	if len(mirrors) != 1 || mirrors[0].ID != 1 {
		t.Errorf("mirrors = %+v", mirrors)
	}
}

func TestGitLabDeletePushMirror(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v4/projects/owner/repo":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":99}`))
		case r.Method == "DELETE" && r.URL.Path == "/api/v4/projects/99/remote_mirrors/7":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gl := &GitLab{}
	if err := gl.DeletePushMirror(context.Background(), srv.URL, "tok", "", "owner", "repo", 7); err != nil {
		t.Fatalf("DeletePushMirror: %v", err)
	}
}

// --- InjectTokenInURL test ---

func TestInjectTokenInURL(t *testing.T) {
	result := InjectTokenInURL("https://github.com/user/repo.git", "my-token")
	expected := "https://token:my-token@github.com/user/repo.git"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

// --- RepoDeleter tests ---

func TestGitHubDeleteRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v3/repos/user/goner" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("missing auth")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	gh := &GitHub{}
	if err := gh.DeleteRepo(context.Background(), srv.URL, "tok", "", "user", "goner"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
}

func TestGitHubDeleteRepoValidatesArgs(t *testing.T) {
	gh := &GitHub{}
	if err := gh.DeleteRepo(context.Background(), "https://github.com", "tok", "", "", "goner"); err == nil {
		t.Error("expected error when owner empty")
	}
	if err := gh.DeleteRepo(context.Background(), "https://github.com", "tok", "", "user", ""); err == nil {
		t.Error("expected error when repo name empty")
	}
}

func TestGitLabDeleteRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.EscapedPath() != "/api/v4/projects/group%2Fgoner" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.EscapedPath())
		}
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			t.Error("missing auth")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	gl := &GitLab{}
	if err := gl.DeleteRepo(context.Background(), srv.URL, "tok", "", "group", "goner"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
}

func TestGiteaDeleteRepo(t *testing.T) {
	srv := httptest.NewServer(giteaTestHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/repos/luis/goner" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	gt := &Gitea{}
	if err := gt.DeleteRepo(context.Background(), srv.URL, "tok", "luis", "luis", "goner"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
}

// --- Interface assertions ---

func TestGiteaImplementsMirrorInterfaces(t *testing.T) {
	var _ RepoCreator = (*Gitea)(nil)
	var _ RepoDeleter = (*Gitea)(nil)
	var _ PushMirrorProvider = (*Gitea)(nil)
	var _ PullMirrorProvider = (*Gitea)(nil)
}

func TestGitHubImplementsRepoCreator(t *testing.T) {
	var _ RepoCreator = (*GitHub)(nil)
	var _ RepoDeleter = (*GitHub)(nil)
}

func TestGitLabImplementsMirrorInterfaces(t *testing.T) {
	var _ RepoCreator = (*GitLab)(nil)
	var _ RepoDeleter = (*GitLab)(nil)
	var _ PushMirrorProvider = (*GitLab)(nil)
}

func TestBitbucketImplementsRepoDeleter(t *testing.T) {
	var _ RepoDeleter = (*Bitbucket)(nil)
}

// --- PRLister tests ---

func TestGitHubListAccountPRs(t *testing.T) {
	// Two search queries expected: authored + review-requested. Mux on q= contents.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/search/issues" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		q := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		if containsSub(q, "author:me") {
			w.Write([]byte(`{"items":[
				{"number":11,"title":"Feature A","html_url":"https://github.com/me/repo/pull/11","repository_url":"https://api.github.com/repos/me/repo","updated_at":"2026-04-19T10:00:00Z","draft":false,"user":{"login":"me"},"pull_request":{}},
				{"number":12,"title":"Draft WIP","html_url":"https://github.com/me/repo/pull/12","repository_url":"https://api.github.com/repos/me/repo","updated_at":"2026-04-20T10:00:00Z","draft":true,"user":{"login":"me"},"pull_request":{}}
			]}`))
			return
		}
		if containsSub(q, "review-requested:me") {
			w.Write([]byte(`{"items":[
				{"number":44,"title":"Please review","html_url":"https://github.com/org/api/pull/44","repository_url":"https://api.github.com/repos/org/api","updated_at":"2026-04-18T10:00:00Z","draft":false,"user":{"login":"teammate"},"pull_request":{}}
			]}`))
			return
		}
		t.Errorf("unexpected query: %s", q)
		http.Error(w, "unexpected", http.StatusBadRequest)
	}))
	defer srv.Close()

	gh := &GitHub{}
	res, err := gh.ListAccountPRs(context.Background(), srv.URL, "tok", "me", true)
	if err != nil {
		t.Fatalf("ListAccountPRs: %v", err)
	}

	mine := res.ByRepo["me/repo"]
	if got := len(mine.Authored); got != 2 {
		t.Errorf("authored in me/repo: got %d, want 2", got)
	}
	if !mine.Authored[1].IsDraft {
		t.Error("expected PR #12 to be marked draft")
	}
	other := res.ByRepo["org/api"]
	if got := len(other.ReviewRequested); got != 1 {
		t.Errorf("review-requested in org/api: got %d, want 1", got)
	}
	if other.ReviewRequested[0].Number != 44 {
		t.Errorf("review PR number: got %d", other.ReviewRequested[0].Number)
	}

	// includeDrafts=false should strip the draft: clause into the query.
	var caughtQ string
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if containsSub(q, "author:me") {
			caughtQ = q
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv2.Close()
	if _, err := gh.ListAccountPRs(context.Background(), srv2.URL, "tok", "me", false); err != nil {
		t.Fatalf("ListAccountPRs (no drafts): %v", err)
	}
	if !containsSub(caughtQ, "draft:false") {
		t.Errorf("expected draft:false in query, got %q", caughtQ)
	}
}

func TestGitLabListAccountPRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/merge_requests" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		scope := r.URL.Query().Get("scope")
		reviewer := r.URL.Query().Get("reviewer_username")
		switch {
		case scope == "created_by_me":
			w.Write([]byte(`[
				{"iid":7,"title":"MR seven","web_url":"https://gitlab.com/group/proj/-/merge_requests/7","updated_at":"2026-04-20T09:00:00Z","draft":false,"author":{"username":"me"},"references":{"full":"group/proj!7"}}
			]`))
		case reviewer == "me":
			w.Write([]byte(`[
				{"iid":8,"title":"Review me","web_url":"https://gitlab.com/other/thing/-/merge_requests/8","updated_at":"2026-04-20T10:00:00Z","draft":false,"author":{"username":"teammate"},"references":{"full":"other/thing!8"}}
			]`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	gl := &GitLab{}
	res, err := gl.ListAccountPRs(context.Background(), srv.URL, "tok", "me", true)
	if err != nil {
		t.Fatalf("GitLab ListAccountPRs: %v", err)
	}
	if got := len(res.ByRepo["group/proj"].Authored); got != 1 {
		t.Errorf("authored group/proj: got %d, want 1", got)
	}
	if got := len(res.ByRepo["other/thing"].ReviewRequested); got != 1 {
		t.Errorf("review-requested other/thing: got %d, want 1", got)
	}
}

func TestGiteaListAccountPRs(t *testing.T) {
	srv := httptest.NewServer(giteaTestHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/issues/search" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("created_by") == "me" {
			w.Write([]byte(`[
				{"number":3,"title":"Mine","html_url":"https://git.example.com/me/project/pulls/3","updated_at":"2026-04-20T09:00:00Z","draft":false,"user":{"login":"me"},"repository":{"full_name":"me/project"},"pull_request":{}}
			]`))
			return
		}
		if r.URL.Query().Get("review_requested") == "true" {
			w.Write([]byte(`[
				{"number":4,"title":"Review","html_url":"https://git.example.com/other/lib/pulls/4","updated_at":"2026-04-20T10:00:00Z","draft":false,"user":{"login":"teammate"},"repository":{"full_name":"other/lib"},"pull_request":{}}
			]`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	gt := &Gitea{}
	res, err := gt.ListAccountPRs(context.Background(), srv.URL, "tok", "me", true)
	if err != nil {
		t.Fatalf("Gitea ListAccountPRs: %v", err)
	}
	if len(res.ByRepo["me/project"].Authored) != 1 {
		t.Errorf("authored me/project: %d", len(res.ByRepo["me/project"].Authored))
	}
	if len(res.ByRepo["other/lib"].ReviewRequested) != 1 {
		t.Errorf("review other/lib: %d", len(res.ByRepo["other/lib"].ReviewRequested))
	}
}

func TestPRListerInterface(t *testing.T) {
	var _ PRLister = (*GitHub)(nil)
	var _ PRLister = (*GitLab)(nil)
	var _ PRLister = (*Gitea)(nil)
}

func containsSub(s, sub string) bool { return contains(s, sub) }

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
