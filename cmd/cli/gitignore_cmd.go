package main

import (
	"encoding/json"
	"fmt"

	"github.com/LuisPalacios/gitbox/pkg/gitignore"
	"github.com/spf13/cobra"
)

var gitignoreCmd = &cobra.Command{
	Use:   "gitignore",
	Short: "Manage the recommended global gitignore (~/.gitignore_global)",
}

var gitignoreCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Show whether the recommended global gitignore block is installed",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := gitignore.Check()
		if err != nil {
			return err
		}

		if jsonOutput {
			out, _ := json.MarshalIndent(s, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Printf("%s\n", colorize("Global Gitignore", colorWhite))

		if s.ExcludesfileSet {
			printStatusLine("✓", "set", "core.excludesfile", s.Excludesfile, colorGreen)
		} else {
			printStatusLine("!", "unset", "core.excludesfile", "would set to "+s.DefaultPath, colorOrange)
		}

		switch {
		case !s.FileExists:
			printStatusLine("!", "missing", s.Path, "no global gitignore file", colorOrange)
		case !s.BlockPresent:
			printStatusLine("!", "no-block", s.Path, "managed block not found", colorOrange)
		case !s.BlockUpToDate:
			printStatusLine("!", "stale", s.Path, "managed block is out of date", colorOrange)
		default:
			printStatusLine("✓", "ok", s.Path, "managed block up to date", colorGreen)
		}

		if s.HasDuplicates {
			details := fmt.Sprintf("%d managed pattern(s) duplicated outside the block", len(s.Duplicates))
			printStatusLine("!", "dup", s.Path, details, colorOrange)
			if verbose {
				for _, d := range s.Duplicates {
					fmt.Printf("    %s\n", d)
				}
			}
		}

		fmt.Println()
		if s.NeedsAction {
			fmt.Printf("Run '%s' to install or update the recommended block.\n", "gitbox gitignore install")
		} else {
			fmt.Println("Nothing to do — global gitignore is up to date.")
		}
		return nil
	},
}

var gitignoreInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install or update the recommended global gitignore block",
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := gitignore.Install()
		if err != nil {
			printStatusLine("x", "error", "gitignore", err.Error(), colorRed)
			return err
		}

		if jsonOutput {
			out, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Printf("%s\n", colorize("Global Gitignore", colorWhite))

		if res.AlreadyUpToDate {
			printStatusLine("✓", "ok", res.Path, "already up to date", colorGreen)
		} else {
			details := "managed block written"
			if res.BackupPath != "" {
				details = "managed block written, backup at " + res.BackupPath
			}
			printStatusLine("✓", "updated", res.Path, details, colorGreen)
		}
		if res.SetExcludesfile {
			printStatusLine("✓", "set", "core.excludesfile", res.Path, colorGreen)
		}
		return nil
	},
}

func init() {
	gitignoreCmd.AddCommand(gitignoreCheckCmd, gitignoreInstallCmd)
}
