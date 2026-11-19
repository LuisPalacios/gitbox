package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type repoExistsTransport func(*http.Request) (*http.Response, error)

func (f repoExistsTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRepoExists_HTTPStatus(t *testing.T) {
	for _, name := range []string{"github", "gitlab", "gitea", "forgejo", "bitbucket"} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name, repo, body string
				status           int
				wantExists       bool
				wantError        string
			}{
				{name: "success", repo: "repo404", status: http.StatusOK, body: `{}`, wantExists: true},
				{name: "not found", repo: "repo", status: http.StatusNotFound, body: "missing"},
				{name: "500 with 404 in URL", repo: "repo404", status: http.StatusInternalServerError, body: "server failed", wantError: "HTTP 500"},
				{name: "500 with 404 in body", repo: "repo", status: http.StatusInternalServerError, body: "lookup 404 failed", wantError: "HTTP 500"},
				{name: "unauthorized", repo: "repo404", status: http.StatusUnauthorized, body: "404", wantError: "authentication failed (HTTP 401) — token is invalid or expired"},
				{name: "forbidden", repo: "repo404", status: http.StatusForbidden, body: "404", wantError: "insufficient permissions (HTTP 403) — token needs broader scopes"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					previous := http.DefaultClient
					t.Cleanup(func() { http.DefaultClient = previous })
					// I intercept every request, including Bitbucket's fixed public API URL.
					var requestURL string
					http.DefaultClient = &http.Client{Transport: repoExistsTransport(func(req *http.Request) (*http.Response, error) {
						w := httptest.NewRecorder()
						w.Header().Set("Content-Type", "application/json")
						if strings.HasSuffix(req.URL.Path, "/api/v1/user/repos") {
							_, _ = w.Write([]byte(`[]`))
						} else {
							requestURL = req.URL.String()
							w.WriteHeader(tc.status)
							_, _ = w.Write([]byte(tc.body))
						}
						return w.Result(), nil
					})}
					p, err := ByName(name)
					if err != nil {
						t.Fatal(err)
					}
					exists, err := p.(RepoCreator).RepoExists(context.Background(), "https://example.invalid", "test-token", "user", "org", tc.repo)
					if requestURL == "" {
						t.Fatal("repository endpoint was not requested")
					}
					if exists != tc.wantExists || (err != nil) != (tc.wantError != "") {
						t.Fatalf("RepoExists = (%v, %v), want exists=%v error=%q", exists, err, tc.wantExists, tc.wantError)
					}
					if err != nil {
						prefix := name
						if name == "forgejo" {
							prefix = "gitea"
						}
						message := tc.wantError
						if tc.status == http.StatusInternalServerError {
							message = fmt.Sprintf("HTTP %d from %s: %s", tc.status, requestURL, tc.body)
						}
						if want := prefix + " repo exists: " + message; err.Error() != want {
							t.Fatalf("error = %q, want %q", err, want)
						}
					}
				})
			}
		})
	}
}
