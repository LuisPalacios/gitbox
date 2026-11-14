package ops

import (
	"fmt"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// ClonePlan is everything needed to clone one configured repo. I resolve the
// config and prepare token credentials while building the plan, so the caller
// can release its config lock while Git transfers data.
type ClonePlan struct {
	SourceKey string
	RepoKey   string
	Dest      string // absolute clone destination
	URL       string // canonical clone URL without a token or password
	Opts      git.CloneOpts
}

// PlanClone resolves the destination, URL and git options for cloning a
// configured repo. For token accounts, I prepare the credential-store file
// before cloning and reset global helpers so Git only uses that account's file.
func PlanClone(cfg *config.Config, sourceKey, repoKey string) (ClonePlan, error) {
	src, ok := cfg.Sources[sourceKey]
	if !ok {
		return ClonePlan{}, fmt.Errorf("source %q not found", sourceKey)
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		return ClonePlan{}, fmt.Errorf("repo %q not found in source %q", repoKey, sourceKey)
	}
	acct, ok := cfg.Accounts[src.Account]
	if !ok {
		return ClonePlan{}, fmt.Errorf("account %q not found", src.Account)
	}

	credType := repo.EffectiveCredentialType(&acct)
	plan := ClonePlan{
		SourceKey: sourceKey,
		RepoKey:   repoKey,
		Dest:      status.RepoPath(cfg, sourceKey, repoKey, repo),
		URL:       heal.ExpectedOriginURL(acct, repoKey, credType),
		Opts:      git.CloneOpts{Quiet: true},
	}

	if credType == "token" {
		credFile := credential.CredentialFilePath(src.Account)
		if err := credential.WriteCredentialFile(credFile, acct, src.Account); err != nil {
			return ClonePlan{}, err
		}
		plan.Opts.ConfigArgs = []string{
			"credential.helper=",
			"credential.helper=" + credential.StoreHelper(credFile),
		}
	}
	return plan, nil
}

// Run clones the repo, reporting progress through onProgress (may be nil).
func (p ClonePlan) Run(onProgress func(git.CloneProgress)) error {
	if onProgress == nil {
		onProgress = func(git.CloneProgress) {}
	}
	return git.CloneWithProgress(p.URL, p.Dest, p.Opts, onProgress)
}
