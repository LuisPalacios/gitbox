package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/mirror"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/spf13/cobra"
)

var (
	statusSource string
	statusRepo   string
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sync status of all repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := resolveConfigPath()
		if cfgPath == "" {
			cfgPath = config.DefaultV2Path()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if jsonOutput {
			results := status.CheckAll(cfg)
			return printStatusJSON(results)
		}

		// Section 1: Global info.
		printGlobalInfo(cfgPath, cfg)

		// Section 2: Account credential status.
		printAccountStatus(cfg)

		// Section 3: Repo status.
		printRepoStatus(cfg)

		// Section 4: Mirror summary (only if mirrors exist).
		if len(cfg.Mirrors) > 0 {
			printMirrorSummary(cfg)
		}

		return nil
	},
}

func init() {
	statusCmd.Flags().StringVar(&statusSource, "source", "", "filter by source name")
	statusCmd.Flags().StringVar(&statusRepo, "repo", "", "filter by repo name")
}

// --- Section 1: Global info ---

func printGlobalInfo(cfgPath string, cfg *config.Config) {
	fmt.Printf("%s\n", colorize("Configuration", colorWhite))
	fmt.Printf("  Config:  %s\n", cfgPath)
	fmt.Printf("  Folder:  %s\n", cfg.Global.Folder)

	gs := identity.CheckGlobalIdentity()
	if gs.HasName || gs.HasEmail {
		parts := []string{}
		if gs.HasName {
			parts = append(parts, fmt.Sprintf("user.name=%q", gs.Name))
		}
		if gs.HasEmail {
			parts = append(parts, fmt.Sprintf("user.email=%q", gs.Email))
		}
		fmt.Printf("  %s  Global ~/.gitconfig has %s (NOT RECOMMENDED)\n",
			colorize("!", colorOrange), strings.Join(parts, ", "))
		fmt.Printf("       Run '%s' to remove global identity.\n", "gitbox identity fix")
	}
	fmt.Println()
}

// --- Section 2: Account credential status ---

func printAccountStatus(cfg *config.Config) {
	fmt.Printf("%s\n", colorize("Accounts", colorWhite))

	// Sort account keys for stable output.
	keys := make([]string, 0, len(cfg.Accounts))
	for k := range cfg.Accounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		acct := cfg.Accounts[key]
		credType := acct.DefaultCredentialType
		if credType == "" {
			credType = "none"
		}

		// Quick credential check (non-interactive).
		credStatus := checkCredentialQuick(cfg, acct, key)

		symbol := "+"
		color := colorGreen
		if credStatus != "ok" {
			symbol = "x"
			color = colorRed
		}

		fmt.Printf("  %s  %-30s  %-8s  %-8s  %s\n",
			colorize(symbol, color),
			key,
			credType,
			colorize(credStatus, color),
			acct.URL)
	}
	fmt.Println()
}

// checkCredentialQuick does a fast, non-interactive credential check.
func checkCredentialQuick(cfg *config.Config, acct config.Account, accountKey string) string {
	switch acct.DefaultCredentialType {
	case "token":
		_, _, err := credential.ResolveToken(acct, accountKey)
		if err != nil {
			return "missing"
		}
		return "ok"
	case "gcm":
		_, _, err := credential.ResolveAPIToken(acct, accountKey)
		if err != nil {
			return "missing"
		}
		return "ok"
	case "ssh":
		hostAlias := credential.SSHHostAlias(accountKey)
		sshFolder := credential.SSHFolder(cfg)
		_, err := credential.TestSSHConnection(sshFolder, hostAlias)
		if err != nil {
			// SSH connection failed, but maybe config+key exist.
			if found, _ := credential.FindSSHConfigEntry(sshFolder, hostAlias); found {
				if _, keyErr := credential.FindSSHKey(sshFolder, hostAlias, "ed25519"); keyErr == nil {
					return "key ok"
				}
			}
			return "missing"
		}
		return "ok"
	default:
		return "none"
	}
}

// --- Section 3: Repo status ---

func printRepoStatus(cfg *config.Config) {
	results := status.CheckAll(cfg)

	// Filter by source/repo if specified.
	if statusSource != "" || statusRepo != "" {
		var filtered []status.RepoStatus
		for _, r := range results {
			if statusSource != "" && r.Source != statusSource {
				continue
			}
			if statusRepo != "" && r.Repo != statusRepo {
				continue
			}
			filtered = append(filtered, r)
		}
		results = filtered
	}

	// Sort by source, then repo.
	sort.Slice(results, func(i, j int) bool {
		if results[i].Source != results[j].Source {
			return results[i].Source < results[j].Source
		}
		return results[i].Repo < results[j].Repo
	})

	fmt.Printf("%s\n", colorize("Repositories", colorWhite))

	if len(results) == 0 {
		fmt.Println("  No repositories configured.")
		return
	}

	// Group by source for cleaner output.
	currentSource := ""
	for _, r := range results {
		if r.Source != currentSource {
			currentSource = r.Source
			fmt.Printf("  %s\n", colorize(currentSource, colorCyan))
		}

		symbol, color := stateToSymbolColor(r.State)
		details := formatDetails(r)

		// Show symbol + state label + repo + details.
		stateLabel := r.State.String()
		if details != "" {
			fmt.Printf("    %s  %-50s  %s\n",
				colorize(fmt.Sprintf("%s %-10s", symbol, stateLabel), color),
				r.Repo,
				details)
		} else {
			fmt.Printf("    %s  %s\n",
				colorize(fmt.Sprintf("%s %-10s", symbol, stateLabel), color),
				r.Repo)
		}
	}
	fmt.Println()
}

func stateToSymbolColor(s status.State) (string, string) {
	switch s {
	case status.Clean:
		return "+", colorGreen
	case status.Dirty:
		return "!", colorOrange
	case status.Behind:
		return "<", colorPurple
	case status.Ahead:
		return ">", colorBlue
	case status.Diverged:
		return "!", colorRed
	case status.Conflict:
		return "!", colorRed
	case status.NotCloned:
		return "o", colorPurple
	case status.NoUpstream:
		return "~", colorOrange
	case status.Error:
		return "x", colorRed
	default:
		return "?", colorWhite
	}
}

func formatDetails(r status.RepoStatus) string {
	switch r.State {
	case status.Behind:
		return fmt.Sprintf("%d behind", r.Behind)
	case status.Ahead:
		return fmt.Sprintf("%d ahead", r.Ahead)
	case status.Diverged:
		return fmt.Sprintf("%d ahead, %d behind", r.Ahead, r.Behind)
	case status.Dirty:
		parts := []string{}
		if r.Modified > 0 {
			parts = append(parts, fmt.Sprintf("%d modified", r.Modified))
		}
		if r.Untracked > 0 {
			parts = append(parts, fmt.Sprintf("%d untracked", r.Untracked))
		}
		if len(parts) == 0 {
			return "has changes"
		}
		return strings.Join(parts, ", ")
	case status.Conflict:
		return fmt.Sprintf("%d conflicts", r.Conflicts)
	case status.NotCloned:
		return "not cloned"
	case status.NoUpstream:
		return "no upstream"
	case status.Error:
		return r.ErrorMsg
	default:
		return ""
	}
}

// --- Section 4: Mirror summary ---

func printMirrorSummary(cfg *config.Config) {
	summaries := mirror.Summarize(cfg, nil)
	if len(summaries) == 0 {
		return
	}

	fmt.Printf("%s\n", colorize("Mirrors", colorWhite))
	for _, s := range summaries {
		symbol := "+"
		color := colorGreen
		if s.Error > 0 {
			symbol = "x"
			color = colorRed
		} else if s.Unchecked > 0 {
			symbol = "~"
			color = colorOrange
		}

		details := fmt.Sprintf("%d repos", s.Total)
		if s.Active > 0 {
			details += fmt.Sprintf(", %d active", s.Active)
		}
		if s.Unchecked > 0 {
			details += fmt.Sprintf(", %d unchecked", s.Unchecked)
		}
		if s.Error > 0 {
			details += fmt.Sprintf(", %d error", s.Error)
		}

		fmt.Printf("  %s  %-30s  %s ↔ %s  %s\n",
			colorize(symbol, color),
			s.MirrorKey,
			s.AccountSrc,
			s.AccountDst,
			details)
	}
	fmt.Println()
}

func printStatusJSON(results []status.RepoStatus) error {
	data, err := json.MarshalIndent(results, "", "    ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}
