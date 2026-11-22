package move

import (
	"context"
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
	if _, err := Preflight(context.Background(), cfg, req); err == nil || !strings.Contains(err.Error(), "identical") {
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
	_, err := Preflight(context.Background(), cfg, req)
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
		if _, err := Preflight(context.Background(), cfg, req); err == nil {
			t.Errorf("case %d: expected error on missing fields", i)
		}
	}
}

// moveCommitFixture is a config with one repo under the source account and
// an empty destination account.
func moveCommitFixture() *config.Config {
	return &config.Config{
		Accounts: map[string]config.Account{"src": {}, "dst": {}},
		Sources: map[string]config.Source{
			"src": {Account: "src", Repos: map[string]config.Repo{"acme/widget": {}}},
		},
	}
}

// TestCommitMove: the config change of a move runs on the owner's config
// through Commit and saves nothing itself (#173). The owner's copy comes
// back for heal; a change that no longer applies leaves the owner's config
// as it was and is reported, not retried.
func TestCommitMove(t *testing.T) {
	req := Request{SourceAccountKey: "src", SourceSourceKey: "src", SourceRepoKey: "acme/widget", DestAccountKey: "dst", DestOwner: "newco", DestRepoName: "widget"}
	owner := moveCommitFixture()
	calls := 0
	commit := func(change func(*config.Config) error) (*config.Config, error) {
		calls++
		work, err := owner.Clone()
		if err != nil {
			return nil, err
		}
		if err := change(work); err != nil {
			return nil, err
		}
		owner = work
		return owner.Clone()
	}

	updated, destSource, newKey, err := commitMove(commit, req)
	if err != nil {
		t.Fatal(err)
	}
	if destSource != "dst" || newKey != "newco/widget" || calls != 1 {
		t.Fatalf("commitMove = %q, %q after %d commits", destSource, newKey, calls)
	}
	if _, ok := owner.Sources["dst"].Repos["newco/widget"]; !ok {
		t.Errorf("the owner's config did not get the move: %+v", owner.Sources)
	}
	if _, ok := owner.Sources["src"].Repos["acme/widget"]; ok {
		t.Error("the source entry is still in the owner's config")
	}
	if updated == owner {
		t.Error("commitMove returned the owner's live config instead of a copy")
	}

	// The entry moved away meanwhile: the change fails once, the owner's
	// config is untouched, and nothing retries.
	before, _ := owner.Clone()
	calls = 0
	if _, _, _, err := commitMove(commit, req); err == nil || calls != 1 {
		t.Fatalf("a stale move = %v after %d commits, want one failed commit", err, calls)
	}
	if !config.EqualIgnoringUIState(before, owner) {
		t.Error("a failed change altered the owner's config")
	}

	if _, _, _, err := commitMove(nil, req); err == nil {
		t.Error("a move without an owner was accepted")
	}
}
