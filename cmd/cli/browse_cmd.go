package main

import (
	"fmt"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/spf13/cobra"
)

var (
	browseSource string
	browseRepo   string
)

var browseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Open a repository in the default browser",
	Long:  "Opens the remote web page for a repository in the default browser.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := resolveConfigPath()
		if cfgPath == "" {
			cfgPath = config.DefaultV2Path()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if browseRepo == "" {
			return fmt.Errorf("--repo is required")
		}

		for _, sourceName := range cfg.OrderedSourceKeys() {
			source := cfg.Sources[sourceName]
			if browseSource != "" && sourceName != browseSource {
				continue
			}

			for _, repoName := range source.OrderedRepoKeys() {
				if repoName != browseRepo {
					continue
				}

				acct := cfg.Accounts[source.Account]
				url := git.RepoWebURL(acct.URL, repoName)

				if jsonOutput {
					fmt.Printf("{\"url\":%q}\n", url)
					return nil
				}

				fmt.Printf("Opening %s\n", url)
				return git.OpenInBrowser(url)
			}
		}

		return fmt.Errorf("repo %q not found", browseRepo)
	},
}

func init() {
	browseCmd.Flags().StringVar(&browseSource, "source", "", "open repo from a specific source only")
	browseCmd.Flags().StringVar(&browseRepo, "repo", "", "repository to open (required)")
}
