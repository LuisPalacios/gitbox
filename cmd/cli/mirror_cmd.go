package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/mirror"
	"github.com/spf13/cobra"
)

var mirrorCmd = &cobra.Command{
	Use:   "mirror",
	Short: "Manage repository mirrors between providers",
}

// --- mirror list ---

var mirrorListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all mirror groups and repos",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if jsonOutput {
			data, _ := json.MarshalIndent(cfg.Mirrors, "", "    ")
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		}

		if len(cfg.Mirrors) == 0 {
			fmt.Println("No mirrors configured.")
			return nil
		}

		for _, key := range cfg.OrderedMirrorKeys() {
			m := cfg.Mirrors[key]
			fmt.Printf("%s  %s ↔ %s\n", colorize(key, colorWhite), m.AccountSrc, m.AccountDst)

			if len(m.Repos) == 0 {
				fmt.Println("  (no repos)")
				continue
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "  REPO\tDIRECTION\tORIGIN\n")
			fmt.Fprintf(w, "  ────\t─────────\t──────\n")
			for _, repoKey := range m.OrderedRepoKeys() {
				mr := m.Repos[repoKey]
				originLabel := m.AccountSrc
				if mr.Origin == "dst" {
					originLabel = m.AccountDst
				}
				fmt.Fprintf(w, "  %s\t%s\t%s\n", repoKey, mr.Direction, originLabel)
			}
			w.Flush()
			fmt.Println()
		}
		return nil
	},
}

// --- mirror show ---

var mirrorShowCmd = &cobra.Command{
	Use:   "show <mirror-key>",
	Short: "Show mirror group details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		m, ok := cfg.Mirrors[args[0]]
		if !ok {
			return fmt.Errorf("mirror %q not found", args[0])
		}
		data, _ := json.MarshalIndent(m, "", "    ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

// --- mirror add ---

var (
	mirrorAccountSrc string
	mirrorAccountDst string
)

var mirrorAddCmd = &cobra.Command{
	Use:   "add <mirror-key>",
	Short: "Create a new mirror group",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		m := config.Mirror{
			AccountSrc: mirrorAccountSrc,
			AccountDst: mirrorAccountDst,
		}
		if err := cfg.AddMirror(args[0], m); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Mirror %q created (%s ↔ %s)\n", args[0], mirrorAccountSrc, mirrorAccountDst)
		return nil
	},
}

// --- mirror delete ---

var mirrorDeleteCmd = &cobra.Command{
	Use:   "delete <mirror-key>",
	Short: "Delete a mirror group",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if err := cfg.DeleteMirror(args[0]); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Mirror %q deleted\n", args[0])
		return nil
	},
}

// --- mirror add-repo ---

var (
	mirrorRepoOrigin    string
	mirrorRepoDirection string
	mirrorRepoSetup     bool
)

var mirrorAddRepoCmd = &cobra.Command{
	Use:   "add-repo <mirror-key> <org/repo>",
	Short: "Add a repo to a mirror group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		repo := config.MirrorRepo{
			Direction: mirrorRepoDirection,
			Origin:    mirrorRepoOrigin,
		}
		if err := cfg.AddMirrorRepo(args[0], args[1], repo); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Repo %q added to mirror %q (direction=%s, origin=%s)\n", args[1], args[0], mirrorRepoDirection, mirrorRepoOrigin)

		if mirrorRepoSetup {
			// Reload to get saved state and run setup.
			cfg, err = loadConfig()
			if err != nil {
				return err
			}
			return runMirrorSetup(cfg, args[0], args[1])
		}
		return nil
	},
}

// --- mirror delete-repo ---

var mirrorDeleteRepoCmd = &cobra.Command{
	Use:   "delete-repo <mirror-key> <org/repo>",
	Short: "Remove a repo from a mirror group",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if err := cfg.DeleteMirrorRepo(args[0], args[1]); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Repo %q removed from mirror %q\n", args[1], args[0])
		return nil
	},
}

// --- mirror setup ---

var mirrorSetupRepo string

var mirrorSetupCmd = &cobra.Command{
	Use:   "setup [mirror-key]",
	Short: "Run API setup for pending mirror repos",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if len(args) == 1 && mirrorSetupRepo != "" {
			// Setup a specific repo.
			return runMirrorSetup(cfg, args[0], mirrorSetupRepo)
		}

		if len(args) == 1 {
			// Setup all pending in one mirror group.
			return runMirrorSetupAll(cfg, args[0])
		}

		// Setup all pending across all mirrors.
		for _, key := range cfg.OrderedMirrorKeys() {
			if err := runMirrorSetupAll(cfg, key); err != nil {
				return err
			}
		}
		return nil
	},
}

func runMirrorSetup(cfg *config.Config, mirrorKey, repoKey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result := mirror.SetupMirror(ctx, cfg, mirrorKey, repoKey)
	printSetupResult(result)
	return nil
}

func runMirrorSetupAll(cfg *config.Config, mirrorKey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	results := mirror.SetupAll(ctx, cfg, mirrorKey)
	for _, r := range results {
		printSetupResult(r)
	}
	return nil
}

func printSetupResult(r mirror.SetupResult) {
	if r.Error != "" {
		printStatusLine("x", "error", r.RepoKey, r.Error, colorRed)
		return
	}
	details := ""
	if r.Created {
		details += "repo created"
	}
	if r.Mirrored {
		if details != "" {
			details += ", "
		}
		details += "mirror configured"
	}
	if r.Method == "manual" {
		printStatusLine("~", "manual", r.RepoKey, "requires manual setup", colorOrange)
		if r.Instructions != "" {
			fmt.Println(r.Instructions)
		}
		return
	}
	printStatusLine("+", "ok", r.RepoKey, details, colorGreen)
}

// --- mirror status ---

var mirrorStatusCmd = &cobra.Command{
	Use:   "status [mirror-key]",
	Short: "Check live mirror status",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if len(cfg.Mirrors) == 0 {
			fmt.Println("No mirrors configured.")
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		var allResults map[string][]mirror.StatusResult

		if len(args) == 1 {
			allResults = map[string][]mirror.StatusResult{
				args[0]: {},
			}
			m, ok := cfg.Mirrors[args[0]]
			if !ok {
				return fmt.Errorf("mirror %q not found", args[0])
			}
			for repoKey := range m.Repos {
				allResults[args[0]] = append(allResults[args[0]], mirror.CheckStatus(ctx, cfg, args[0], repoKey))
			}
		} else {
			allResults = mirror.CheckAllMirrors(ctx, cfg)
		}

		if jsonOutput {
			data, _ := json.MarshalIndent(allResults, "", "    ")
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		}

		for mirrorKey, results := range allResults {
			m := cfg.Mirrors[mirrorKey]
			fmt.Printf("%s  %s ↔ %s\n", colorize(mirrorKey, colorWhite), m.AccountSrc, m.AccountDst)
			for _, r := range results {
				dirLabel := formatDirectionLabel(r)
				if r.Error != "" {
					printStatusLine("x", "error", r.RepoKey, r.Error, colorRed)
				} else if r.SyncStatus == "synced" {
					details := fmt.Sprintf("%s  Synced OK", dirLabel)
					if r.Warning != "" {
						details += fmt.Sprintf("  ⚠ %s", r.Warning)
					}
					printStatusLine("+", "synced", r.RepoKey, details, colorGreen)
				} else if r.SyncStatus == "behind" {
					details := fmt.Sprintf("%s  Backup is behind origin", dirLabel)
					if r.Warning != "" {
						details += fmt.Sprintf("  ⚠ %s", r.Warning)
					}
					printStatusLine("<", "behind", r.RepoKey, details, colorPurple)
				} else if r.SyncStatus == "ahead" {
					details := fmt.Sprintf("%s  Backup is ahead of origin", dirLabel)
					if r.Warning != "" {
						details += fmt.Sprintf("  ⚠ %s", r.Warning)
					}
					printStatusLine(">", "ahead", r.RepoKey, details, colorRed)
				} else if r.Active {
					details := dirLabel
					if r.Warning != "" {
						details += fmt.Sprintf("  ⚠ %s", r.Warning)
					}
					printStatusLine("+", "active", r.RepoKey, details, colorGreen)
				} else {
					printStatusLine("?", "unknown", r.RepoKey, "", colorOrange)
				}
			}
			fmt.Println()
		}
		return nil
	},
}

// formatDirectionLabel returns a direction indicator like:
//   "git-parchis-luis --> github-LuisPalacios (mirror)"   (push)
//   "git-parchis-luis (mirror) <-- github-LuisPalacios"   (pull)
func formatDirectionLabel(r mirror.StatusResult) string {
	if r.Direction == "" || r.OriginAcct == "" || r.BackupAcct == "" {
		return ""
	}
	if r.Direction == "push" {
		return fmt.Sprintf("%s --> %s (mirror)", r.OriginAcct, r.BackupAcct)
	}
	return fmt.Sprintf("%s (mirror) <-- %s", r.BackupAcct, r.OriginAcct)
}

// --- mirror discover ---

var mirrorDiscoverApply bool

var mirrorDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Scan accounts to discover mirror relationships",
	Long: `Scans all account pairs to detect existing push/pull mirror configurations.

Detection methods (in order of confidence):
  confirmed — push mirror API on Forgejo/Gitea returns matching remote URL
  likely    — repo has mirror flag on Forgejo/Gitea + same name on other account
  possible  — repos with same name exist on both accounts

Use --apply to merge discovered mirrors into your config.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		fmt.Println("Scanning accounts for mirror relationships...")
		results, err := mirror.DiscoverMirrors(ctx, cfg, nil)
		if err != nil {
			return err
		}

		totalFound := 0
		for _, r := range results {
			fmt.Printf("\n%s  %s ↔ %s\n", colorize(r.MirrorKey, colorWhite), r.AccountSrc, r.AccountDst)
			for _, dm := range r.Discovered {
				totalFound++
				symbol := "+"
				color := colorGreen
				if dm.Confidence == "possible" {
					symbol = "?"
					color = colorOrange
				}
				details := fmt.Sprintf("%-5s  origin=%-3s  [%s]", dm.Direction, dm.Origin, dm.Confidence)
				printStatusLine(symbol, dm.Confidence, dm.RepoKey, details, color)
			}
		}

		if totalFound == 0 {
			fmt.Println("\nNo mirror relationships found.")
			return nil
		}

		fmt.Printf("\nFound %d mirror(s).", totalFound)
		if !mirrorDiscoverApply {
			fmt.Println(" Use --apply to update config.")
			return nil
		}

		added, _ := mirror.ApplyDiscovery(cfg, results)
		if added > 0 {
			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("\nApplied: %d mirror(s) added to config.\n", added)
		} else {
			fmt.Println("\nAll discovered mirrors already in config.")
		}
		return nil
	},
}

func init() {
	// mirror add flags.
	mirrorAddCmd.Flags().StringVar(&mirrorAccountSrc, "account-src", "", "source account in the mirror pair")
	mirrorAddCmd.Flags().StringVar(&mirrorAccountDst, "account-dst", "", "destination account in the mirror pair")
	mirrorAddCmd.MarkFlagRequired("account-src")
	mirrorAddCmd.MarkFlagRequired("account-dst")

	// mirror add-repo flags.
	mirrorAddRepoCmd.Flags().StringVar(&mirrorRepoOrigin, "origin", "", "which account is the source of truth: 'src' or 'dst'")
	mirrorAddRepoCmd.Flags().StringVar(&mirrorRepoDirection, "direction", "", "mirror direction: 'push' or 'pull'")
	mirrorAddRepoCmd.Flags().BoolVar(&mirrorRepoSetup, "setup", false, "immediately run API setup after adding")
	mirrorAddRepoCmd.MarkFlagRequired("origin")
	mirrorAddRepoCmd.MarkFlagRequired("direction")

	// mirror setup flags.
	mirrorSetupCmd.Flags().StringVar(&mirrorSetupRepo, "repo", "", "specific repo to set up")

	// mirror discover flags.
	mirrorDiscoverCmd.Flags().BoolVar(&mirrorDiscoverApply, "apply", false, "update config with discovered mirrors")

	// Register subcommands.
	mirrorCmd.AddCommand(mirrorListCmd, mirrorShowCmd, mirrorAddCmd, mirrorDeleteCmd, mirrorAddRepoCmd, mirrorDeleteRepoCmd, mirrorSetupCmd, mirrorStatusCmd, mirrorDiscoverCmd)
}
