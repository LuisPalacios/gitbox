package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var containerOff bool

// containerCmd flags (or clears) a managed repo as a multi-repo container, so
// gitbox descends into its working tree to discover nested clones the user
// provisioned there with their own tooling.
var containerCmd = &cobra.Command{
	Use:   "container <source-key> <repo-key>",
	Short: "Mark (or clear) a repo as a multi-repo container",
	Long: `Marks a managed repo as a multi-repo container. gitbox then descends into
its working tree (up to global.nested_scan_depth) during 'gitbox adopt' to
discover and onboard nested clones provisioned there by your own tooling.

Use --off to clear the flag.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceKey, repoKey := args[0], args[1]
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		src, ok := cfg.Sources[sourceKey]
		if !ok {
			return fmt.Errorf("unknown source %q", sourceKey)
		}
		repo, ok := src.Repos[repoKey]
		if !ok {
			return fmt.Errorf("unknown repo %q in source %q", repoKey, sourceKey)
		}
		repo.Container = !containerOff
		src.Repos[repoKey] = repo
		cfg.Sources[sourceKey] = src
		if err := saveConfig(cfg); err != nil {
			return err
		}
		if repo.Container {
			fmt.Printf("Marked %s/%s as a container. Run 'gitbox adopt' to discover nested clones.\n", sourceKey, repoKey)
		} else {
			fmt.Printf("Cleared container flag on %s/%s.\n", sourceKey, repoKey)
		}
		return nil
	},
}

func init() {
	containerCmd.Flags().BoolVar(&containerOff, "off", false, "clear the container flag instead of setting it")
}
