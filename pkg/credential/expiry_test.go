package credential

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestCheckToken_ReportsExpiry(t *testing.T) {
	expiry := time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("GitHub-Authentication-Token-Expiration", expiry.Format("2006-01-02 15:04:05 MST"))
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	t.Setenv(EnvVarName("test-expiry"), "test-token")
	acct := config.Account{Provider: "github", URL: srv.URL, Username: "account-A", DefaultCredentialType: "token"}

	r := CheckContext(context.Background(), acct, "test-expiry", &config.Config{})
	if r.Overall != StatusOK {
		t.Fatalf("overall = %v (%s), want ok", r.Overall, r.PrimaryDetail)
	}
	if !r.ExpiresAt.Equal(expiry) {
		t.Errorf("ExpiresAt = %v, want %v", r.ExpiresAt, expiry)
	}
}

func TestCheckToken_RejectedTokenFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "token expired", http.StatusUnauthorized)
	}))
	defer srv.Close()
	t.Setenv(EnvVarName("test-expiry"), "test-token")
	acct := config.Account{Provider: "github", URL: srv.URL, Username: "account-A", DefaultCredentialType: "token"}

	r := CheckContext(context.Background(), acct, "test-expiry", &config.Config{})
	if r.Overall != StatusError || r.Primary != StatusError || r.PAT != StatusError {
		t.Fatalf("rejected token = %+v, want error everywhere", r)
	}
	if !r.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v on a rejected token", r.ExpiresAt)
	}
}

func TestCheckToken_ForbiddenStaysWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing scope", http.StatusForbidden)
	}))
	defer srv.Close()
	t.Setenv(EnvVarName("test-expiry"), "test-token")
	acct := config.Account{Provider: "github", URL: srv.URL, Username: "account-A", DefaultCredentialType: "token"}

	if r := CheckContext(context.Background(), acct, "test-expiry", &config.Config{}); r.Overall != StatusWarning {
		t.Fatalf("forbidden token = %v (%s), want warning", r.Overall, r.PrimaryDetail)
	}
}
