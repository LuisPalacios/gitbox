package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/spf13/cobra"
)

var (
	adoptDryRun bool
	adoptAll    bool
)

var adoptCmd = &cobra.Command{
	Use:   "adopt",
	Short: "Adopt orphan repos into gitbox",
	Long: `Discovers git repositories under the gitbox parent folder that are not
tracked in gitbox.json and offers to adopt them.

For each orphan with a matching account, gitbox will:
  - Add it to the config under the correct source
  - Set up per-repo credential isolation
  - Configure user.name and user.email
  - Rewrite the remote URL to match the credential type
  - Optionally relocate it to the standard folder structure

Use --dry-run to preview without making changes.`,
	RunE: runAdopt,
}

func init() {
	adoptCmd.Flags().BoolVar(&adoptDryRun, "dry-run", false, "show what would happen without making changes")
	adoptCmd.Flags().BoolVar(&adoptAll, "all", false, "adopt all matched orphans without prompting")
}

func runAdopt(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	orphans, err := adopt.FindOrphans(cfg)
	if err != nil {
		return fmt.Errorf("scanning for orphans: %w", err)
	}

	if len(orphans) == 0 {
		fmt.Println("No orphan repos found.")
		return nil
	}

	// Group orphans by category.
	var matched, unknown, local []adopt.OrphanRepo
	for _, o := range orphans {
		switch {
		case o.LocalOnly:
			local = append(local, o)
		case o.MatchedAccount != "":
			matched = append(matched, o)
		default:
			unknown = append(unknown, o)
		}
	}

	parentFolder := config.ExpandTilde(cfg.Global.Folder)
	home, _ := os.UserHomeDir()

	// Display report.
	fmt.Printf("Found %d orphan repos under %s:\n", len(orphans), tildePrefix(parentFolder, home))

	if len(matched) > 0 {
		fmt.Printf("\n%s\n", colorize("Ready to adopt (account matched):", colorCyan))
		for _, o := range matched {
			action := "in place"
			if o.NeedsRelocate {
				action = "will relocate"
			}
			fmt.Printf("  %-50s → %s / %s  [%s]\n",
				tildePrefix(o.Path, home), o.MatchedSource, o.RepoKey, action)
		}
	}

	if len(unknown) > 0 {
		fmt.Printf("\n%s\n", colorize("Unknown account (manual setup needed):", colorRed))
		for _, o := range unknown {
			fmt.Printf("  %-50s → remote: %s\n", tildePrefix(o.Path, home), o.RemoteURL)
		}
		fmt.Println("  Tip: run 'gitbox account add' to create the account, then re-run adopt.")
	}

	if len(local) > 0 {
		fmt.Printf("\n%s\n", colorize("Local only (skipped — no remote):", colorYellow))
		for _, o := range local {
			fmt.Printf("  %s\n", tildePrefix(o.Path, home))
		}
	}

	if len(matched) == 0 {
		fmt.Println("\nNo matched orphans to adopt.")
		return nil
	}

	if adoptDryRun {
		fmt.Printf("\n%s\n", colorize("Dry run — no changes made.", colorBlue))
		return nil
	}

	// Interactive adoption.
	reader := bufio.NewReader(os.Stdin)
	adopted := 0
	relocated := 0
	skipped := 0

	for _, o := range matched {
		if !adoptAll {
			fmt.Printf("\nAdopt %s into source %s? [Y/n] ", colorize(o.RepoKey, colorWhite), o.MatchedSource)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer == "n" || answer == "no" {
				skipped++
				continue
			}
		}

		repoPath := o.Path

		// Relocate if needed.
		if o.NeedsRelocate {
			if !adoptAll {
				fmt.Printf("  Move from %s\n    to %s? [Y/n] ",
					tildePrefix(o.Path, home), tildePrefix(o.ExpectedPath, home))
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer == "n" || answer == "no" {
					fmt.Println("  Adopting in place (setting clone_folder override).")
				} else {
					if err := relocateRepo(o.Path, o.ExpectedPath); err != nil {
						printStatusLine("x", "error", o.RepoKey, "relocate: "+err.Error(), colorRed)
						skipped++
						continue
					}
					repoPath = o.ExpectedPath
					relocated++
				}
			} else {
				// --all: adopt in place with override, don't auto-relocate.
				fmt.Printf("  %s: adopting in place (use interactive mode to relocate)\n", o.RepoKey)
			}
		}

		// Add repo to config.
		repo := config.Repo{}

		// If we didn't relocate but the path doesn't match the convention,
		// set CloneFolder to the actual path so gitbox can find it.
		if repoPath == o.Path && o.NeedsRelocate {
			repo.CloneFolder = repoPath
		}

		if err := cfg.AddRepo(o.MatchedSource, o.RepoKey, repo); err != nil {
			printStatusLine("x", "error", o.RepoKey, "config: "+err.Error(), colorRed)
			skipped++
			continue
		}

		// Sanitize .git/config.
		acct := cfg.Accounts[o.MatchedAccount]
		credType := repo.EffectiveCredentialType(&acct)

		// Rewrite remote URL.
		newURL := plainRemoteURL(acct, o.RepoKey, credType)
		if err := git.SetRemoteURL(repoPath, "origin", newURL); err != nil {
			printStatusLine("!", "warn", o.RepoKey, "set-url: "+err.Error(), colorOrange)
		}

		// Configure credential isolation.
		if err := credential.ConfigureRepoCredential(repoPath, acct, o.MatchedAccount, credType, cfg.Global); err != nil {
			printStatusLine("!", "warn", o.RepoKey, "credential: "+err.Error(), colorOrange)
		}

		// Set identity.
		name := repo.Name
		if name == "" {
			name = acct.Name
		}
		email := repo.Email
		if email == "" {
			email = acct.Email
		}
		_ = git.ConfigSet(repoPath, "user.name", name)
		_ = git.ConfigSet(repoPath, "user.email", email)

		printStatusLine("+", "adopted", o.RepoKey, credType, colorGreen)
		adopted++
	}

	// Save config.
	if adopted > 0 {
		if err := saveConfig(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}

	fmt.Printf("\nAdopted: %d, Relocated: %d, Skipped: %d\n", adopted, relocated, skipped)
	return nil
}

// relocateRepo moves a repo from src to dst, creating parent directories.
func relocateRepo(src, dst string) error {
	// Check destination doesn't already exist.
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("destination already exists: %s", dst)
	}

	// Create parent directory.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Move.
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("moving repo: %w", err)
	}

	return nil
}

// tildePrefix replaces the home directory prefix with ~ for display.
func tildePrefix(path, home string) string {
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
