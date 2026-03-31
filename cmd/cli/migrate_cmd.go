package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/spf13/cobra"
)

var migrateDryRun bool

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate v1 config (git-config-repos.json) to v2 format",
	Long: `Reads the v1 configuration at ~/.config/git-config-repos/git-config-repos.json
and writes a v2 configuration at ~/.config/gitbox/gitbox.json.

The original v1 file is never modified. Both tools can coexist.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		v1Path := config.DefaultV1Path()
		v2Path := config.DefaultV2Path()

		if migrateDryRun {
			cfg, err := config.MigrateDryRun(v1Path)
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(cfg, "", "    ")
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, string(data))
			fmt.Fprintf(os.Stderr, "\n(dry run — would write to %s)\n", v2Path)
			return nil
		}

		cfg, err := config.Migrate(v1Path, v2Path)
		if err != nil {
			return err
		}

		fmt.Printf("Migration complete.\n")
		fmt.Printf("  Source: %s\n", v1Path)
		fmt.Printf("  Target: %s\n", v2Path)
		fmt.Printf("  Accounts: %d\n", len(cfg.Accounts))
		fmt.Printf("  Sources:  %d\n", len(cfg.Sources))

		totalRepos := 0
		for _, s := range cfg.Sources {
			totalRepos += len(s.Repos)
		}
		fmt.Printf("  Repos:    %d\n", totalRepos)
		fmt.Println("\nThe original v1 file was NOT modified.")
		return nil
	},
}

func init() {
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "show what would be migrated without writing")
}
