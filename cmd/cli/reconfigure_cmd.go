package main

import (
	"fmt"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/spf13/cobra"
)

var reconfigureAccount string

var reconfigureCmd = &cobra.Command{
	Use:   "reconfigure",
	Short: "Reconfigure credential isolation for all cloned repos",
	Long: `Updates the .git/config of every cloned repo to use per-repo credential
isolation. Each repo gets a credential.helper override that cancels the global
helper and sets the correct type-specific helper (store for token, manager for
GCM, empty for SSH).

This is useful after upgrading gitbox, changing credential types, or fixing
repos that were cloned before per-repo isolation was available.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		count := 0
		errors := 0

		for accountKey := range cfg.Accounts {
			if reconfigureAccount != "" && accountKey != reconfigureAccount {
				continue
			}

			n, e := reconfigureClonesForAccount(cfg, accountKey)
			count += n
			errors += e
		}

		fmt.Printf("\nReconfigured: %d, Errors: %d\n", count, errors)
		return nil
	},
}

func init() {
	reconfigureCmd.Flags().StringVar(&reconfigureAccount, "account", "", "reconfigure repos for a specific account only")
}

// reconfigureClonesForAccount updates remote URLs and per-repo credential config
// for all cloned repos belonging to the given account. Returns (updated, errors).
func reconfigureClonesForAccount(cfg *config.Config, accountKey string) (int, int) {
	acct, ok := cfg.Accounts[accountKey]
	if !ok {
		printStatusLine("x", "error", accountKey, "account not found", colorRed)
		return 0, 1
	}

	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	count := 0
	errors := 0

	for _, sourceKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[sourceKey]
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if !git.IsRepo(path) {
				continue
			}

			label := sourceKey + "/" + repoKey
			credType := repo.EffectiveCredentialType(&acct)

			// Update remote URL (plain, no embedded token).
			newURL := adopt.PlainRemoteURL(acct, repoKey, credType)
			if err := git.SetRemoteURL(path, "origin", newURL); err != nil {
				printStatusLine("x", "error", label, "set-url: "+err.Error(), colorRed)
				errors++
				continue
			}

			// Configure per-repo credential isolation.
			if err := credential.ConfigureRepoCredential(path, acct, accountKey, credType, cfg.Global); err != nil {
				printStatusLine("x", "error", label, "credential: "+err.Error(), colorRed)
				errors++
				continue
			}

			// Update user.name and user.email (repo-level overrides take priority).
			name := repo.Name
			if name == "" {
				name = acct.Name
			}
			email := repo.Email
			if email == "" {
				email = acct.Email
			}
			_ = git.ConfigSet(path, "user.name", name)
			_ = git.ConfigSet(path, "user.email", email)

			printStatusLine("+", "reconfig", label, credType, colorGreen)
			count++
		}
	}
	return count, errors
}


