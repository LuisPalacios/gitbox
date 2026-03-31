// Package identity manages per-repo git identity (user.name, user.email).
package identity

import (
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// GlobalIdentityStatus reports whether ~/.gitconfig has user identity set.
type GlobalIdentityStatus struct {
	HasName  bool   `json:"hasName"`
	HasEmail bool   `json:"hasEmail"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

// ResolveIdentity returns the (name, email) for a repo, applying
// repo-level overrides falling back to account-level values.
func ResolveIdentity(repo config.Repo, acct config.Account) (name, email string) {
	name = repo.Name
	if name == "" {
		name = acct.Name
	}
	email = repo.Email
	if email == "" {
		email = acct.Email
	}
	return name, email
}

// EnsureRepoIdentity checks the repo's local git config and sets
// user.name/user.email if they differ from the expected values.
func EnsureRepoIdentity(repoPath, wantName, wantEmail string) (fixedName, fixedEmail bool, err error) {
	curName, _ := git.ConfigGet(repoPath, "user.name")
	if curName != wantName {
		if err := git.ConfigSet(repoPath, "user.name", wantName); err != nil {
			return false, false, err
		}
		fixedName = true
	}

	curEmail, _ := git.ConfigGet(repoPath, "user.email")
	if curEmail != wantEmail {
		if err := git.ConfigSet(repoPath, "user.email", wantEmail); err != nil {
			return fixedName, false, err
		}
		fixedEmail = true
	}

	return fixedName, fixedEmail, nil
}

// CheckGlobalIdentity checks if global ~/.gitconfig has user.name/user.email.
func CheckGlobalIdentity() GlobalIdentityStatus {
	var s GlobalIdentityStatus
	if name, err := git.GlobalConfigGet("user.name"); err == nil && name != "" {
		s.HasName = true
		s.Name = name
	}
	if email, err := git.GlobalConfigGet("user.email"); err == nil && email != "" {
		s.HasEmail = true
		s.Email = email
	}
	return s
}

// RemoveGlobalIdentity unsets user.name and user.email from ~/.gitconfig.
func RemoveGlobalIdentity() error {
	if err := git.GlobalConfigUnset("user.name"); err != nil {
		return err
	}
	return git.GlobalConfigUnset("user.email")
}
