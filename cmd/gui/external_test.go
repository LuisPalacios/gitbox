package main

import (
	"strings"
	"testing"
)

// recordBrowser replaces the system browser for one test and returns the
// links it was asked to open.
func recordBrowser(t *testing.T) *[]string {
	t.Helper()
	prev := openBrowser
	var opened []string
	openBrowser = func(link string) error {
		opened = append(opened, link)
		return nil
	}
	t.Cleanup(func() { openBrowser = prev })
	return &opened
}

// The funnel opens https links, http ones only when confirmed, and refuses
// everything else before anything starts.
func TestOpenExternal(t *testing.T) {
	cases := []struct {
		link      string
		confirmed bool
		code      string // "" = opened
	}{
		{"https://host/ok", false, ""},
		{"https://github.com/LuisPalacios/gitbox", false, ""},
		{"http://example.com", false, "url.needs_confirmation"},
		{"http://example.com", true, ""},
		{"javascript:alert(1)", true, "url.rejected"},
		{"vscode://x", true, "url.rejected"},
		{"ms-settings:", true, "url.rejected"},
		{"file:///etc/passwd", true, "url.rejected"},
		{"data:text/html,<script>", true, "url.rejected"},
		{"https://user:pass@host", true, "url.rejected"},
		{"https://user@host/", true, "url.rejected"},
		{"https://host/\r\nX", true, "url.rejected"},
		{"https://host/\x1b[31m", true, "url.rejected"},
		{"https://host/a b", true, "url.rejected"},
		{"https:///no-host", true, "url.rejected"},
		{"https:opaque", true, "url.rejected"},
		{"", true, "url.rejected"},
		{"HTTPS://host/upper", false, ""},
		{"https://host/" + strings.Repeat("a", maxExternalURL), true, "url.rejected"},
	}
	for _, tc := range cases {
		opened := recordBrowser(t)
		err := OpenExternal(tc.link, tc.confirmed)
		if got := toUserError(err).Code; got != tc.code {
			t.Errorf("OpenExternal(%q, %v) = %q (%v), want %q", tc.link, tc.confirmed, got, err, tc.code)
		}
		if wantOpen := tc.code == ""; (len(*opened) == 1) != wantOpen {
			t.Errorf("OpenExternal(%q, %v) opened %v", tc.link, tc.confirmed, *opened)
		}
		if err != nil && strings.Contains(err.Error(), tc.link) && tc.link != "" {
			t.Errorf("the refusal of %q carries the link: %v", tc.link, err)
		}
		if err != nil {
			if hint := toUserError(err).Hint; hint != "error."+tc.code {
				t.Errorf("hint %q, want error.%s", hint, tc.code)
			}
		}
	}
}

// A bound method turns an unconfirmed http link into a question for the
// user, and opens it once confirmed.
func TestOpenInBrowserAsksForHTTP(t *testing.T) {
	opened := recordBrowser(t)
	a := newTestApp()
	res, err := a.OpenInBrowser("http://lan-gitea.local/alice", false)
	if err != nil || !res.NeedsConfirm || res.URL != "http://lan-gitea.local/alice" || len(*opened) != 0 {
		t.Fatalf("unconfirmed http: %+v, %v, opened %v", res, err, *opened)
	}
	res, err = a.OpenInBrowser("http://lan-gitea.local/alice", true)
	if err != nil || res.NeedsConfirm || len(*opened) != 1 {
		t.Fatalf("confirmed http: %+v, %v, opened %v", res, err, *opened)
	}
	if _, err := a.OpenInBrowser("vscode://x", true); toUserError(err).Code != "url.rejected" {
		t.Errorf("vscode link: %v", err)
	}
	if len(*opened) != 1 {
		t.Errorf("a refused link opened: %v", *opened)
	}

	// The account and repo links take the same path.
	b := appWithCreatedRepo("http://lan-gitea.local")
	res, err = b.OpenRepoInBrowser("github-alice", "alice/newrepo", false)
	if err != nil || !res.NeedsConfirm || res.URL != "http://lan-gitea.local/alice/newrepo" {
		t.Errorf("repo link: %+v, %v", res, err)
	}
	res, err = b.OpenAccountInBrowser("github-alice", true)
	if err != nil || res.NeedsConfirm || (*opened)[len(*opened)-1] != "http://lan-gitea.local/alice" {
		t.Errorf("account link: %+v, %v, opened %v", res, err, *opened)
	}
}
