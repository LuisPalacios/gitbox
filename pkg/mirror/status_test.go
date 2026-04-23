package mirror

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
)

// TestCheckStatus_404_NeedsSetup_vs_Error covers issue #59: a 404 from the
// backup's GetRepoInfo should render as neutral "needs setup" on rows that
// have never been set up (LastSync == ""), and as the red "target repo does
// not exist" error on rows that were set up previously (LastSync populated).
func TestCheckStatus_404_NeedsSetup_vs_Error(t *testing.T) {
	// Origin server: responds OK for GetRepoInfo so we only exercise the
	// backup 404 branching.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/repos/org/repo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"default_branch":"main","private":true}`))
		case "/api/v3/repos/org/repo/branches/main":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"commit":{"sha":"abc123","commit":{"author":{"date":"2025-01-01T00:00:00Z"}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()

	// Backup server: always 404 on the repo endpoint — simulating "target
	// does not exist on backup provider".
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer backup.Close()

	// Token env vars so credential.ResolveToken / ResolveAPIToken find PATs
	// without hitting the keyring.
	t.Setenv(credential.EnvVarName("origin-acct"), "tok-origin")
	t.Setenv(credential.EnvVarName("backup-acct"), "tok-backup")

	makeCfg := func(lastSync string) *config.Config {
		return &config.Config{
			Version: 2,
			Accounts: map[string]config.Account{
				"origin-acct": {
					Provider: "github", URL: origin.URL, Username: "alice",
					DefaultCredentialType: "token",
				},
				"backup-acct": {
					Provider: "github", URL: backup.URL, Username: "bob",
					DefaultCredentialType: "token",
				},
			},
			Mirrors: map[string]config.Mirror{
				"grp": {
					AccountSrc: "origin-acct",
					AccountDst: "backup-acct",
					Repos: map[string]config.MirrorRepo{
						"org/repo": {
							Direction: "push",
							Origin:    "src",
							LastSync:  lastSync,
						},
					},
				},
			},
		}
	}

	ctx := context.Background()

	// Branch 1: never set up → NeedsSetup true, no Error.
	t.Run("never set up, target 404 → NeedsSetup", func(t *testing.T) {
		cfg := makeCfg("")
		r := CheckStatus(ctx, cfg, "grp", "org/repo")
		if !r.NeedsSetup {
			t.Errorf("NeedsSetup = false, want true")
		}
		if r.Error != "" {
			t.Errorf("Error = %q, want empty (new row 404 must not surface as error)", r.Error)
		}
	})

	// Branch 2: previously set up → Error, NeedsSetup false.
	t.Run("previously set up, target 404 → Error", func(t *testing.T) {
		cfg := makeCfg("2025-01-01T00:00:00Z")
		r := CheckStatus(ctx, cfg, "grp", "org/repo")
		if r.NeedsSetup {
			t.Errorf("NeedsSetup = true, want false (previously-set-up row must surface the missing target)")
		}
		if r.Error != "target repo does not exist on backup-acct" {
			t.Errorf("Error = %q, want %q", r.Error, "target repo does not exist on backup-acct")
		}
	})
}
