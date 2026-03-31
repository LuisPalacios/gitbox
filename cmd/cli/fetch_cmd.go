package main

import (
	"fmt"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/spf13/cobra"
)

var (
	fetchSource string
	fetchRepo   string
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch all remotes for repositories (without merging)",
	Long:  "Runs git fetch --all --prune on configured repos. Updates remote tracking branches without touching working trees.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := resolveConfigPath()
		if cfgPath == "" {
			cfgPath = config.DefaultV2Path()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		globalFolder := config.ExpandTilde(cfg.Global.Folder)
		fetched := 0
		skipped := 0
		errors := 0

		for _, sourceName := range cfg.OrderedSourceKeys() {
			source := cfg.Sources[sourceName]
			if fetchSource != "" && sourceName != fetchSource {
				continue
			}

			sourceFolder := source.EffectiveFolder(sourceName)
			for _, repoName := range source.OrderedRepoKeys() {
				repo := source.Repos[repoName]
				if fetchRepo != "" && repoName != fetchRepo {
					continue
				}

				label := sourceName + "/" + repoName
				dest := status.ResolveRepoPath(globalFolder, sourceFolder, repoName, repo)

				if !git.IsRepo(dest) {
					if verbose {
						printStatusLine("o", "missing", label, "not cloned", colorPurple)
					}
					skipped++
					continue
				}

				if err := git.Fetch(dest); err != nil {
					printStatusLine("x", "error", label, err.Error(), colorRed)
					errors++
					continue
				}

				// Show status after fetch.
				rs := status.Check(dest)
				switch rs.State {
				case status.Behind:
					printStatusLine("+", "fetched", label, fmt.Sprintf("%d behind", rs.Behind), colorBlue)
				case status.Ahead:
					printStatusLine("+", "fetched", label, fmt.Sprintf("%d ahead", rs.Ahead), colorBlue)
				case status.Diverged:
					printStatusLine("+", "fetched", label, fmt.Sprintf("%d ahead, %d behind", rs.Ahead, rs.Behind), colorPurple)
				default:
					if verbose {
						printStatusLine("+", "fetched", label, rs.State.String(), colorGreen)
					}
				}
				fetched++
			}
		}

		fmt.Printf("\nFetched: %d, Skipped: %d, Errors: %d\n", fetched, skipped, errors)
		return nil
	},
}

func init() {
	fetchCmd.Flags().StringVar(&fetchSource, "source", "", "fetch repos from a specific source only")
	fetchCmd.Flags().StringVar(&fetchRepo, "repo", "", "fetch a specific repo only")
}
