package ops

import (
	"fmt"
	"net/url"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// ClonePlan is everything needed to clone one configured repo. Building the
// plan reads the config; running it doesn't, so the caller can release its
// config lock while git transfers data.
type ClonePlan struct {
	SourceKey string
	RepoKey   string
	Dest      string // absolute clone destination
	URL       string // URL handed to git clone (may embed a token)
	Opts      git.CloneOpts
}

// PlanClone resolves the destination, URL and git options for cloning a
// configured repo. For token accounts the token is embedded in the clone URL
// and the global credential helper is cancelled, so GCM can't store a ghost
// credential; heal.Repo strips the token from the stored origin afterwards.
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

	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	credType := repo.EffectiveCredentialType(&acct)
	plan := ClonePlan{
		SourceKey: sourceKey,
		RepoKey:   repoKey,
		Dest:      status.ResolveRepoPath(globalFolder, src.EffectiveFolder(sourceKey), repoKey, repo),
		URL:       heal.ExpectedOriginURL(acct, repoKey, credType),
		Opts:      git.CloneOpts{Quiet: true},
	}

	if credType == "token" {
		if tok, _, err := credential.ResolveToken(acct, src.Account); err == nil && tok != "" {
			if u, err := url.Parse(plan.URL); err == nil {
				u.User = url.UserPassword(acct.Username, tok)
				plan.URL = u.String()
			}
		}
		plan.Opts.ConfigArgs = []string{"credential.helper="}
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
