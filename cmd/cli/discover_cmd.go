package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/spf13/cobra"
)

var (
	discoverAll          bool
	discoverSkipForks    bool
	discoverSkipArchived bool
	discoverSource       string
)

var accountDiscoverCmd = &cobra.Command{
	Use:   "discover <account-key>",
	Short: "Discover repos from a provider and add them to config",
	Long: `Fetches the list of repositories visible to the authenticated user on the
provider, displays them, and lets you choose which to add to your config.

Uses the account's credential (token or GCM) for API access. SSH accounts
need a PAT or GCM credential configured for discovery to work.

Examples:
  gitbox account discover github-personal
  gitbox account discover github-personal --all
  gitbox account discover github-personal --json
  gitbox account discover github-personal --skip-forks --skip-archived`,
	Args: cobra.ExactArgs(1),
	RunE: runDiscover,
}

func init() {
	accountDiscoverCmd.Flags().BoolVar(&discoverAll, "all", false, "add all discovered repos without prompting")
	accountDiscoverCmd.Flags().BoolVar(&discoverSkipForks, "skip-forks", false, "exclude forked repos")
	accountDiscoverCmd.Flags().BoolVar(&discoverSkipArchived, "skip-archived", false, "exclude archived repos")
	accountDiscoverCmd.Flags().StringVar(&discoverSource, "source", "", "target source key (default: account key)")
}

// discoverJSON is the structure for --json output.
type discoverJSON struct {
	Discovered        []provider.RemoteRepo `json:"discovered"`
	AlreadyConfigured []string              `json:"already_configured"`
	Stale             []staleRepo           `json:"stale"`
}

type staleRepo struct {
	RepoKey   string `json:"repo"`
	SourceKey string `json:"source"`
}

func runDiscover(cmd *cobra.Command, args []string) error {
	accountKey := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	acct, ok := cfg.GetAccountByKey(accountKey)
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	// Resolve API token based on credential type.
	token, _, err := credential.ResolveAPIToken(acct, accountKey)
	if err != nil {
		if acct.DefaultCredentialType == "ssh" {
			fmt.Fprintf(os.Stderr, "SSH credentials cannot access provider APIs.\n")
			fmt.Fprintf(os.Stderr, "Discovery requires a PAT or GCM credential for account %q.\n", accountKey)
			fmt.Fprintf(os.Stderr, "Store a PAT with: gitbox account credential setup %s --token\n", accountKey)
		} else {
			fmt.Fprintf(os.Stderr, "No credentials available for account %q.\n", accountKey)
			fmt.Fprintf(os.Stderr, "Configure credentials with: gitbox account credential setup %s\n", accountKey)
		}
		return fmt.Errorf("cannot discover without API credentials")
	}

	// Get provider.
	prov, err := provider.ByName(acct.Provider)
	if err != nil {
		return err
	}

	// Fetch repos.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Fprintf(os.Stderr, "Fetching repos for %q from %s...\n", accountKey, acct.URL)
	repos, err := prov.ListRepos(ctx, acct.URL, token, acct.Username)
	if err != nil {
		if strings.Contains(err.Error(), "authentication failed") {
			fmt.Fprintf(os.Stderr, "\nCredentials rejected. Reconfigure with: gitbox account credential setup %s\n", accountKey)
		}
		return err
	}

	// Sort by full name.
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].FullName < repos[j].FullName
	})

	// Apply filters.
	repos = filterRepos(repos)

	// Build set of already-configured repos for this account.
	configured := configuredRepos(cfg, accountKey)

	// Detect stale repos (in config but not upstream).
	upstream := make(map[string]bool, len(repos))
	for _, r := range repos {
		upstream[r.FullName] = true
	}
	staleRepos := findStaleRepos(cfg, accountKey, upstream)

	// --json mode: output and exit.
	if jsonOutput {
		var alreadyList []string
		for k := range configured {
			alreadyList = append(alreadyList, k)
		}
		sort.Strings(alreadyList)
		out := discoverJSON{
			Discovered:        repos,
			AlreadyConfigured: alreadyList,
			Stale:             staleRepos,
		}
		data, _ := json.MarshalIndent(out, "", "    ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	// Print discovered list.
	newRepos := printDiscoveredList(repos, configured)
	printStaleWarnings(staleRepos)

	if len(newRepos) == 0 {
		fmt.Println("\nAll discovered repos are already in your config.")
		return nil
	}

	// Select repos to add.
	var selected []provider.RemoteRepo
	if discoverAll {
		selected = newRepos
	} else {
		selected, err = interactiveSelect(newRepos)
		if err != nil {
			return err
		}
	}

	if len(selected) == 0 {
		fmt.Println("No repos selected.")
		return nil
	}

	// Resolve target source.
	sourceKey, err := resolveSource(cfg, accountKey)
	if err != nil {
		return err
	}

	// Add selected repos.
	added := 0
	for _, r := range selected {
		if err := cfg.AddRepo(sourceKey, r.FullName, config.Repo{}); err != nil {
			fmt.Fprintf(os.Stderr, "  skipped %s: %v\n", r.FullName, err)
			continue
		}
		added++
	}

	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nAdded: %d, Skipped: %d (already configured), Stale: %d (not found upstream), Total discovered: %d\n",
		added, len(configured), len(staleRepos), len(repos))
	return nil
}

// filterRepos applies --skip-forks and --skip-archived flags.
func filterRepos(repos []provider.RemoteRepo) []provider.RemoteRepo {
	if !discoverSkipForks && !discoverSkipArchived {
		return repos
	}
	var filtered []provider.RemoteRepo
	for _, r := range repos {
		if discoverSkipForks && r.Fork {
			continue
		}
		if discoverSkipArchived && r.Archived {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// configuredRepos returns a set of repo keys already in config for sources
// that reference the given account.
func configuredRepos(cfg *config.Config, accountKey string) map[string]string {
	result := make(map[string]string)
	for srcKey, src := range cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		for repoKey := range src.Repos {
			result[repoKey] = srcKey
		}
	}
	return result
}

// findStaleRepos finds repos in config (for this account) that don't exist upstream.
func findStaleRepos(cfg *config.Config, accountKey string, upstream map[string]bool) []staleRepo {
	var stale []staleRepo
	for srcKey, src := range cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		for repoKey := range src.Repos {
			if !upstream[repoKey] {
				stale = append(stale, staleRepo{RepoKey: repoKey, SourceKey: srcKey})
			}
		}
	}
	sort.Slice(stale, func(i, j int) bool {
		return stale[i].RepoKey < stale[j].RepoKey
	})
	return stale
}

// printDiscoveredList prints the numbered repo list and returns only new repos.
func printDiscoveredList(repos []provider.RemoteRepo, configured map[string]string) []provider.RemoteRepo {
	fmt.Fprintf(os.Stderr, "\nDiscovered %d repos:\n\n", len(repos))
	fmt.Fprintf(os.Stderr, "  %-4s  %-50s  %s\n", "#", "REPO", "STATUS")

	var newRepos []provider.RemoteRepo
	newIdx := 0
	for _, r := range repos {
		if srcKey, ok := configured[r.FullName]; ok {
			fmt.Fprintf(os.Stderr, "  %-4s  %-50s  (already in source %q)\n", "-", r.FullName, srcKey)
		} else {
			newIdx++
			label := "(new)"
			if r.Fork {
				label = "(new, fork)"
			}
			if r.Archived {
				label = "(new, archived)"
			}
			if r.Fork && r.Archived {
				label = "(new, fork, archived)"
			}
			fmt.Fprintf(os.Stderr, "  %-4d  %-50s  %s\n", newIdx, r.FullName, label)
			newRepos = append(newRepos, r)
		}
	}
	fmt.Fprintln(os.Stderr)
	return newRepos
}

// printStaleWarnings prints warnings about repos in config but not found upstream.
func printStaleWarnings(stale []staleRepo) {
	if len(stale) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "  Warning: %d repo(s) in your config were not found upstream:\n", len(stale))
	for _, s := range stale {
		fmt.Fprintf(os.Stderr, "    - %-50s (in source %q)\n", s.RepoKey, s.SourceKey)
	}
	fmt.Fprintln(os.Stderr)
}

// interactiveSelect prompts the user to pick repos from the new list.
func interactiveSelect(newRepos []provider.RemoteRepo) ([]provider.RemoteRepo, error) {
	fmt.Fprintf(os.Stderr, "Enter repos to add (e.g. 1,3,5-10 or \"all\", empty to cancel): ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return nil, nil
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return nil, nil
	}

	if strings.EqualFold(input, "all") {
		return newRepos, nil
	}

	indices, err := parseSelection(input, len(newRepos))
	if err != nil {
		return nil, err
	}

	var selected []provider.RemoteRepo
	for _, idx := range indices {
		selected = append(selected, newRepos[idx-1])
	}
	return selected, nil
}

// parseSelection parses a comma-separated list of numbers and ranges (1-based).
func parseSelection(input string, max int) ([]int, error) {
	seen := make(map[int]bool)
	var result []int

	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if dashIdx := strings.Index(part, "-"); dashIdx >= 0 {
			start, err := strconv.Atoi(strings.TrimSpace(part[:dashIdx]))
			if err != nil {
				return nil, fmt.Errorf("invalid selection: %q", part)
			}
			end, err := strconv.Atoi(strings.TrimSpace(part[dashIdx+1:]))
			if err != nil {
				return nil, fmt.Errorf("invalid selection: %q", part)
			}
			if start < 1 || end > max || start > end {
				return nil, fmt.Errorf("range %d-%d is out of bounds (1-%d)", start, end, max)
			}
			for i := start; i <= end; i++ {
				if !seen[i] {
					seen[i] = true
					result = append(result, i)
				}
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid selection: %q", part)
			}
			if n < 1 || n > max {
				return nil, fmt.Errorf("selection %d is out of bounds (1-%d)", n, max)
			}
			if !seen[n] {
				seen[n] = true
				result = append(result, n)
			}
		}
	}
	return result, nil
}

// resolveSource finds or creates the target source for discovered repos.
func resolveSource(cfg *config.Config, accountKey string) (string, error) {
	// If --source was specified, use it.
	if discoverSource != "" {
		if _, ok := cfg.Sources[discoverSource]; !ok {
			return "", fmt.Errorf("source %q not found", discoverSource)
		}
		return discoverSource, nil
	}

	// Find sources that reference this account.
	var matching []string
	for key, src := range cfg.Sources {
		if src.Account == accountKey {
			matching = append(matching, key)
		}
	}

	switch len(matching) {
	case 0:
		// Auto-create source with same key as account.
		src := config.Source{
			Account: accountKey,
			Repos:   make(map[string]config.Repo),
		}
		if err := cfg.AddSource(accountKey, src); err != nil {
			return "", fmt.Errorf("auto-creating source: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Created source %q (linked to account %q)\n", accountKey, accountKey)
		return accountKey, nil
	case 1:
		return matching[0], nil
	default:
		sort.Strings(matching)
		return "", fmt.Errorf("multiple sources reference account %q: %s\nUse --source to specify which one",
			accountKey, strings.Join(matching, ", "))
	}
}
