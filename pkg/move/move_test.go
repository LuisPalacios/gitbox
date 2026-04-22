package move

import (
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// TestSplitRepoKey checks the helper's behavior for the usual shapes.
func TestSplitRepoKey(t *testing.T) {
	owner, name := splitRepoKey("acme/widget")
	if owner != "acme" || name != "widget" {
		t.Errorf("got %q/%q", owner, name)
	}
	owner, name = splitRepoKey("widget")
	if owner != "" || name != "widget" {
		t.Errorf("bare name: got %q/%q", owner, name)
	}
}

// TestBuildHTTPSCloneURL exercises the two common URL shapes.
func TestBuildHTTPSCloneURL(t *testing.T) {
	acct := config.Account{URL: "https://github.com/"}
	got := buildHTTPSCloneURL(acct, "acme", "widget")
	if got != "https://github.com/acme/widget.git" {
		t.Errorf("https url: %q", got)
	}
}

// TestPreflightRejectsIdenticalDestination ensures a same-account,
// same-repo move is blocked up front. This is cheap to check and
// avoids a partial API call on a clear no-op.
func TestPreflightRejectsIdenticalDestination(t *testing.T) {
	cfg := &config.Config{
		Accounts: map[string]config.Account{
			"acme": {URL: "https://github.com", Username: "acme", Provider: "github"},
		},
		Sources: map[string]config.Source{},
	}
	req := Request{
		SourceAccountKey: "acme",
		SourceSourceKey:  "acme",
		SourceRepoKey:    "acme/widget",
		SourceRepoPath:   "/tmp/does-not-matter",
		DestAccountKey:   "acme",
		DestOwner:        "acme",
		DestRepoName:     "widget",
	}
	if _, err := Preflight(nil, cfg, req); err == nil || !strings.Contains(err.Error(), "identical") {
		t.Errorf("expected 'identical' error, got %v", err)
	}
}

// TestPreflightRejectsUnknownAccounts surfaces the friendly error when
// the caller passes bad account keys.
func TestPreflightRejectsUnknownAccounts(t *testing.T) {
	cfg := &config.Config{
		Accounts: map[string]config.Account{
			"src": {URL: "https://github.com", Username: "u", Provider: "github"},
		},
	}
	req := Request{
		SourceAccountKey: "src",
		SourceSourceKey:  "src",
		SourceRepoKey:    "u/r",
		SourceRepoPath:   "/tmp/whatever",
		DestAccountKey:   "does-not-exist",
		DestOwner:        "u",
		DestRepoName:     "r",
	}
	_, err := Preflight(nil, cfg, req)
	if err == nil || !strings.Contains(err.Error(), "destination account") {
		t.Errorf("expected destination-account error, got %v", err)
	}
}

// TestPreflightRejectsMissingFields checks the cheap guard clauses.
func TestPreflightRejectsMissingFields(t *testing.T) {
	cfg := &config.Config{Accounts: map[string]config.Account{}}
	cases := []Request{
		{},
		{SourceAccountKey: "a"},
		{SourceAccountKey: "a", DestAccountKey: "b"},
		{SourceAccountKey: "a", DestAccountKey: "b", SourceSourceKey: "a", SourceRepoKey: "x/y"},
		{SourceAccountKey: "a", DestAccountKey: "b", SourceSourceKey: "a", SourceRepoKey: "x/y", DestOwner: "b", DestRepoName: "y"},
	}
	for i, req := range cases {
		if _, err := Preflight(nil, cfg, req); err == nil {
			t.Errorf("case %d: expected error on missing fields", i)
		}
	}
}
