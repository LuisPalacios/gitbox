package ops

import (
	"context"
	"fmt"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/pathkey"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

// ProviderClient resolves the API token and provider implementation for an
// account. Every provider API call starts here.
func ProviderClient(acct config.Account, accountKey string) (provider.Provider, string, error) {
	token, _, err := credential.ResolveAPIToken(acct, accountKey)
	if err != nil {
		return nil, "", fmt.Errorf("no API token available: %w", err)
	}
	prov, err := provider.ByName(acct.Provider)
	if err != nil {
		return nil, "", err
	}
	return prov, token, nil
}

// ListRemoteRepos lists every repo the account can see on its provider.
func ListRemoteRepos(ctx context.Context, acct config.Account, accountKey string) ([]provider.RemoteRepo, error) {
	prov, token, err := ProviderClient(acct, accountKey)
	if err != nil {
		return nil, err
	}
	return prov.ListRepos(provider.WithAccountCache(ctx, accountKey, 0), acct.URL, token, acct.Username)
}

// ListAccountOrgs returns the namespaces the account can own repos in: its
// username first, then any organizations the provider reports. Org listing
// is best-effort.
func ListAccountOrgs(ctx context.Context, acct config.Account, accountKey string) ([]string, error) {
	prov, token, err := ProviderClient(acct, accountKey)
	if err != nil {
		return nil, err
	}
	result := []string{acct.Username}
	if ol, ok := prov.(provider.OrgLister); ok {
		if orgs, err := ol.ListUserOrgs(provider.WithAccountCache(ctx, accountKey, 0), acct.URL, token, acct.Username); err == nil {
			result = append(result, orgs.Names...)
		}
	}
	return result, nil
}

// CreateRemoteRepo creates owner/name on the account's provider. An owner
// equal to the username means the personal namespace.
func CreateRemoteRepo(ctx context.Context, acct config.Account, accountKey, owner, name, description string, private bool) error {
	prov, token, err := ProviderClient(acct, accountKey)
	if err != nil {
		return err
	}
	rc, ok := prov.(provider.RepoCreator)
	if !ok {
		return fmt.Errorf("provider %q does not support repo creation", acct.Provider)
	}
	apiOwner := owner
	if apiOwner == acct.Username {
		apiOwner = ""
	}
	return rc.CreateRepo(ctx, acct.URL, token, acct.Username, apiOwner, name, description, private)
}

// ResolveSourceKey returns key when it names a source, otherwise the first
// source (in config order) that belongs to the account named key.
func ResolveSourceKey(cfg *config.Config, key string) (string, error) {
	if _, ok := cfg.Sources[key]; ok {
		return key, nil
	}
	for _, sk := range cfg.OrderedSourceKeys() {
		if cfg.Sources[sk].Account == key {
			return sk, nil
		}
	}
	return "", fmt.Errorf("no source found for key %q", key)
}

// AddDiscoveredRepos adds repos (by "owner/name") to the source named key,
// or to the account's source when key is an account key. Repos already in
// the source are skipped.
func AddDiscoveredRepos(cfg *config.Config, key string, repoNames []string) error {
	return AddDiscoveredReposToFolder(cfg, key, repoNames, "")
}

// AddDiscoveredReposToFolder checks all destinations before adding any repos.
func AddDiscoveredReposToFolder(cfg *config.Config, key string, repoNames []string, folder string) error {
	sourceKey, err := ResolveSourceKey(cfg, key)
	if err != nil {
		return err
	}
	claimed := map[string]bool{}
	for sk, src := range cfg.Sources {
		for rk, repo := range src.Repos {
			claimed[pathkey.Compare(config.RepoPath(cfg, sk, rk, repo))] = true
		}
	}
	entry := config.Repo{CloneFolder: folder}
	pending := make([]string, 0, len(repoNames))
	names := map[string]bool{}
	for _, name := range repoNames {
		if _, exists := cfg.Sources[sourceKey].Repos[name]; exists || names[name] {
			continue
		}
		dest := pathkey.Of(config.RepoPath(cfg, sourceKey, name, entry))
		if dest.Access == pathkey.Unknown {
			return config.ErrPathUnavailable
		}
		if claimed[dest.Cmp] {
			return config.ErrPathCollision
		}
		claimed[dest.Cmp], names[name] = true, true
		pending = append(pending, name)
	}
	for _, name := range pending {
		if err := cfg.AddRepo(sourceKey, name, entry); err != nil {
			return err
		}
	}
	return nil
}
