package config

import (
	"errors"
	"testing"
)

// TestKeyErrors: a duplicate add or rename unwraps to ErrExists, a missing
// entry to ErrNotFound, and the message stays the one users and tests
// already know (#294).
func TestKeyErrors(t *testing.T) {
	c := &Config{Accounts: map[string]Account{}, Sources: map[string]Source{}, Mirrors: map[string]Mirror{}}
	acct := Account{Provider: "github", URL: "https://github.com", Username: "a", Name: "A", Email: "a@e"}
	if err := c.AddAccount("a", acct); err != nil {
		t.Fatal(err)
	}
	err := c.AddAccount("a", acct)
	if !errors.Is(err, ErrExists) || err.Error() != `account "a" already exists` {
		t.Errorf("duplicate AddAccount = %v", err)
	}
	if err := c.DeleteAccount("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteAccount of a missing key = %v", err)
	}
	if err := c.RenameAccount("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RenameAccount of a missing key = %v", err)
	}
	if err := c.AddAccount("b", Account{Provider: "github", URL: "https://github.com", Username: "b", Name: "B", Email: "b@e"}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddMirror("m", Mirror{AccountSrc: "a", AccountDst: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddMirror("m", Mirror{AccountSrc: "a", AccountDst: "b"}); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate AddMirror = %v", err)
	}
	if err := c.DeleteMirrorRepo("m", "o/r"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteMirrorRepo of a missing repo = %v", err)
	}
	if err := c.DeleteMirror("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteMirror of a missing group = %v", err)
	}
}
