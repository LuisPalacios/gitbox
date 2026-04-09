package main

import (
	"fmt"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/spf13/cobra"
)

var (
	sweepSource string
	sweepRepo   string
	sweepDryRun bool
)

var sweepCmd = &cobra.Command{
	Use:   "sweep",
	Short: "Remove stale local branches (merged or gone upstream)",
	Long:  "Finds and deletes local branches that are merged into the default branch or whose upstream tracking branch has been deleted. Never touches the current branch or the default branch.",
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
		swept := 0
		totalDeleted := 0
		skipped := 0
		errors := 0

		for _, sourceName := range cfg.OrderedSourceKeys() {
			source := cfg.Sources[sourceName]
			if sweepSource != "" && sourceName != sweepSource {
				continue
			}

			sourceFolder := source.EffectiveFolder(sourceName)
			for _, repoName := range source.OrderedRepoKeys() {
				repo := source.Repos[repoName]
				if sweepRepo != "" && repoName != sweepRepo {
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

				result, err := git.SweepBranches(dest)
				if err != nil {
					printStatusLine("x", "error", label, err.Error(), colorRed)
					errors++
					continue
				}

				total := len(result.Merged) + len(result.Gone) + len(result.Squashed)
				if total == 0 {
					if verbose {
						printStatusLine("~", "clean", label, "no stale branches", colorGreen)
					}
					continue
				}

				if sweepDryRun {
					var parts []string
					for _, b := range result.Gone {
						parts = append(parts, b+" (gone)")
					}
					for _, b := range result.Merged {
						parts = append(parts, b+" (merged)")
					}
					for _, b := range result.Squashed {
						parts = append(parts, b+" (squashed)")
					}
					printStatusLine("~", "dry-run", label, "would delete: "+strings.Join(parts, ", "), colorBlue)
					continue
				}

				deleted, errs := git.DeleteStaleBranches(dest, result)
				if len(errs) > 0 {
					var errNames []string
					for _, e := range errs {
						errNames = append(errNames, e.Error())
					}
					printStatusLine("!", "partial", label, fmt.Sprintf("deleted %d, failed: %s", len(deleted), strings.Join(errNames, ", ")), colorPurple)
				}

				if len(deleted) > 0 {
					details := fmt.Sprintf("%d gone, %d merged, %d squashed: %s",
						len(result.Gone), len(result.Merged), len(result.Squashed), strings.Join(deleted, ", "))
					printStatusLine("+", "swept", label, details, colorGreen)
					swept++
					totalDeleted += len(deleted)
				}
			}
		}

		if sweepDryRun {
			fmt.Println("\nDry run — no branches were deleted.")
		} else {
			fmt.Printf("\nSwept: %d branches in %d repos, Skipped: %d, Errors: %d\n",
				totalDeleted, swept, skipped, errors)
		}
		return nil
	},
}

func init() {
	sweepCmd.Flags().StringVar(&sweepSource, "source", "", "sweep repos from a specific source only")
	sweepCmd.Flags().StringVar(&sweepRepo, "repo", "", "sweep a specific repo only")
	sweepCmd.Flags().BoolVar(&sweepDryRun, "dry-run", false, "list stale branches without deleting them")
}
