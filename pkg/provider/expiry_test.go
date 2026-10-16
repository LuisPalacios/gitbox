package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseGitHubExpiry_Layouts(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
	}{
		{"2025-09-10 02:30:13 +0200", time.Date(2025, 9, 10, 0, 30, 13, 0, time.UTC)},
		{"2027-09-06 12:00:00 UTC", time.Date(2027, 9, 6, 12, 0, 0, 0, time.UTC)},
		{" 2027-09-06 12:00:00 UTC ", time.Date(2027, 9, 6, 12, 0, 0, 0, time.UTC)},
		{"", time.Time{}},
		{"never", time.Time{}},
		{"2027-09-06", time.Time{}},
	}
	for _, c := range cases {
		if got := parseGitHubExpiry(c.in); !got.Equal(c.want) {
			t.Errorf("parseGitHubExpiry(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// githubExpiryServer answers /user/repos with an empty list, sending header as
// the token's expiry when it isn't empty, and counts the requests.
func githubExpiryServer(t *testing.T, header string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/user/repos") {
			http.NotFound(w, r)
			return
		}
		if header != "" {
			w.Header().Set("GitHub-Authentication-Token-Expiration", header)
		}
		w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestCheckAuth_GitHubReadsExpiryWithoutExtraRequest(t *testing.T) {
	expiry := time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Second)
	srv, hits := githubExpiryServer(t, expiry.Format("2006-01-02 15:04:05 -0700"))
	got, err := CheckAuth(context.Background(), "github", srv.URL, "tok", "user")
	if err != nil {
		t.Fatalf("CheckAuth: %v", err)
	}
	if !got.Equal(expiry) {
		t.Errorf("expiry = %v, want %v", got, expiry)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
}

func TestCheckAuth_GitHubWithoutExpiry(t *testing.T) {
	srv, _ := githubExpiryServer(t, "")
	got, err := CheckAuth(context.Background(), "github", srv.URL, "tok", "user")
	if err != nil || !got.IsZero() {
		t.Fatalf("CheckAuth = %v, %v; want zero, nil", got, err)
	}
}

func TestCheckAuth_IgnoresPastExpiry(t *testing.T) {
	srv, _ := githubExpiryServer(t, time.Now().Add(-time.Minute).UTC().Format("2006-01-02 15:04:05 MST"))
	got, err := CheckAuth(context.Background(), "github", srv.URL, "tok", "user")
	if err != nil || !got.IsZero() {
		t.Fatalf("CheckAuth = %v, %v; want zero, nil", got, err)
	}
}

func TestCheckAuth_GitHubRejectedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad credentials", http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := CheckAuth(context.Background(), "github", srv.URL, "tok", "user"); err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("CheckAuth error = %v, want HTTP 401", err)
	}
}

// gitlabExpiryServer answers the project list with projectsStatus and the
// token's self endpoint with selfStatus and selfBody, and counts self hits.
func gitlabExpiryServer(t *testing.T, projectsStatus, selfStatus int, selfBody string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var selfHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects":
			w.WriteHeader(projectsStatus)
			w.Write([]byte("[]"))
		case "/api/v4/personal_access_tokens/self":
			selfHits.Add(1)
			w.WriteHeader(selfStatus)
			w.Write([]byte(selfBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &selfHits
}

func TestCheckAuth_GitLabReadsTokenExpiry(t *testing.T) {
	day := time.Now().AddDate(0, 0, 10).UTC().Format("2006-01-02")
	srv, _ := gitlabExpiryServer(t, http.StatusOK, http.StatusOK, `{"id":1,"active":true,"expires_at":"`+day+`"}`)
	got, err := CheckAuth(context.Background(), "gitlab", srv.URL, "tok", "user")
	if err != nil {
		t.Fatalf("CheckAuth: %v", err)
	}
	if want, _ := time.Parse("2006-01-02", day); !got.Equal(want) {
		t.Errorf("expiry = %v, want %v", got, want)
	}
}

func TestCheckAuth_GitLabUnknownExpiryStillPasses(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"no expiry":        {http.StatusOK, `{"id":1,"expires_at":null}`},
		"oauth token":      {http.StatusUnauthorized, `{"message":"401 Unauthorized"}`},
		"older gitlab":     {http.StatusNotFound, `{"message":"404 Not Found"}`},
		"unparsable value": {http.StatusOK, `{"expires_at":"soon"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			srv, _ := gitlabExpiryServer(t, http.StatusOK, c.status, c.body)
			got, err := CheckAuth(context.Background(), "gitlab", srv.URL, "tok", "user")
			if err != nil || !got.IsZero() {
				t.Fatalf("CheckAuth = %v, %v; want zero, nil", got, err)
			}
		})
	}
}

func TestCheckAuth_GitLabSkipsExpiryWhenListFails(t *testing.T) {
	srv, selfHits := gitlabExpiryServer(t, http.StatusUnauthorized, http.StatusOK, `{"expires_at":"2099-01-01"}`)
	if _, err := CheckAuth(context.Background(), "gitlab", srv.URL, "tok", "user"); err == nil {
		t.Fatal("CheckAuth passed with a rejected token")
	}
	if n := selfHits.Load(); n != 0 {
		t.Errorf("self endpoint hit %d times after a failed check", n)
	}
}

func TestCheckAuth_GiteaHasNoExpiry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	got, err := CheckAuth(context.Background(), "forgejo", srv.URL, "tok", "user")
	if err != nil || !got.IsZero() {
		t.Fatalf("CheckAuth = %v, %v; want zero, nil", got, err)
	}
}
