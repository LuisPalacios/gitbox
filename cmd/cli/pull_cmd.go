package main

import (
	"fmt"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/spf13/cobra"
)

var (
	pullSource string
	pullRepo   string
	pullAll    bool
)

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull repositories that are behind upstream",
	Long:  "Pull (fast-forward only) repositories that are behind their upstream.",
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
		pulled := 0
		skipped := 0
		errors := 0

		for _, sourceName := range cfg.OrderedSourceKeys() {
			source := cfg.Sources[sourceName]
			if pullSource != "" && sourceName != pullSource {
				continue
			}

			sourceFolder := source.EffectiveFolder(sourceName)
			for _, repoName := range source.OrderedRepoKeys() {
				repo := source.Repos[repoName]
				if pullRepo != "" && repoName != pullRepo {
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

				// Check status first — only pull if behind.
				rs := status.Check(dest)
				if rs.State == status.Clean || rs.State == status.Ahead || rs.State == status.NoUpstream {
					if verbose {
						printStatusLine("~", "skip", label, rs.State.String(), colorCyan)
					}
					skipped++
					continue
				}

				if rs.State == status.Dirty || rs.State == status.Conflict {
					printStatusLine("!", "dirty", label, rs.State.String()+" — resolve manually", colorOrange)
					skipped++
					continue
				}

				// Behind or Diverged — attempt pull.
				if err := git.PullQuiet(dest); err != nil {
					printStatusLine("x", "error", label, err.Error(), colorRed)
					errors++
					continue
				}
				printStatusLine("+", "pulled", label, fmt.Sprintf("%d behind → ok", rs.Behind), colorGreen)
				pulled++
			}
		}

		fmt.Printf("\nPulled: %d, Skipped: %d, Errors: %d\n", pulled, skipped, errors)
		return nil
	},
}

func init() {
	pullCmd.Flags().StringVar(&pullSource, "source", "", "pull repos from a specific source only")
	pullCmd.Flags().StringVar(&pullRepo, "repo", "", "pull a specific repo only")
	pullCmd.Flags().BoolVar(&pullAll, "all", false, "pull all configured repos")
}
