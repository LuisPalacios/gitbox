package identity

import (
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestResolveIdentity_AccountFallback(t *testing.T) {
	acct := config.Account{Name: "Alice", Email: "alice@example.com"}
	repo := config.Repo{} // no overrides

	name, email := ResolveIdentity(repo, acct)
	if name != "Alice" || email != "alice@example.com" {
		t.Errorf("expected Alice/alice@example.com, got %s/%s", name, email)
	}
}

func TestResolveIdentity_RepoOverride(t *testing.T) {
	acct := config.Account{Name: "Alice", Email: "alice@example.com"}
	repo := config.Repo{Name: "Bob", Email: "bob@corp.com"}

	name, email := ResolveIdentity(repo, acct)
	if name != "Bob" || email != "bob@corp.com" {
		t.Errorf("expected Bob/bob@corp.com, got %s/%s", name, email)
	}
}

func TestResolveIdentity_PartialOverride(t *testing.T) {
	acct := config.Account{Name: "Alice", Email: "alice@example.com"}
	repo := config.Repo{Name: "Bob"} // email falls back to account

	name, email := ResolveIdentity(repo, acct)
	if name != "Bob" || email != "alice@example.com" {
		t.Errorf("expected Bob/alice@example.com, got %s/%s", name, email)
	}
}

func TestCheckGlobalIdentity(t *testing.T) {
	// Just verify it doesn't panic — result depends on the machine's ~/.gitconfig
	s := CheckGlobalIdentity()
	_ = s.HasName
	_ = s.HasEmail
}
