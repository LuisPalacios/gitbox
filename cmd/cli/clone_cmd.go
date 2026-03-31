package main

import (
	"fmt"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/spf13/cobra"
)

var (
	cloneSource string
	cloneRepo   string
	cloneAll    bool
)

var cloneCmd = &cobra.Command{
	Use:   "clone",
	Short: "Clone repositories",
	Long:  "Clone configured repositories that haven't been cloned yet.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := resolveConfigPath()
		if cfgPath == "" {
			cfgPath = config.DefaultV2Path()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Note: per-repo credential isolation replaces global credential config.
		// Each clone gets its own credential.helper in .git/config.

		globalFolder := config.ExpandTilde(cfg.Global.Folder)
		fmt.Printf("Cloning into %s\n", globalFolder)
		cloned := 0
		skipped := 0
		errors := 0

		for _, sourceName := range cfg.OrderedSourceKeys() {
			source := cfg.Sources[sourceName]
			if cloneSource != "" && sourceName != cloneSource {
				continue
			}

			acct, ok := cfg.Accounts[source.Account]
			if !ok {
				printStatusLine("x", "error", sourceName+"/*", "unknown account: "+source.Account, colorRed)
				errors++
				continue
			}

			for _, repoName := range source.OrderedRepoKeys() {
				repo := source.Repos[repoName]
				if cloneRepo != "" && repoName != cloneRepo {
					continue
				}

				label := sourceName + "/" + repoName

				sourceFolder := source.EffectiveFolder(sourceName)
				dest := status.ResolveRepoPath(globalFolder, sourceFolder, repoName, repo)

				if git.IsRepo(dest) {
					printStatusLine("~", "exists", label, "", colorCyan)
					skipped++
					continue
				}

				credType := repo.EffectiveCredentialType(&acct)

				// Validate that the account has the necessary credential config.
				switch credType {
				case "ssh":
					if acct.SSH == nil || acct.SSH.Host == "" {
						printStatusLine("x", "error", label, "ssh.host required in account "+source.Account, colorRed)
						errors++
						continue
					}
				}

				cloneURL, err := buildCloneURL(acct, sourceName, repoName, repo)
				if err != nil {
					printStatusLine("x", "error", label, err.Error(), colorRed)
					errors++
					continue
				}

				// Show progress bar while cloning.
				// For token clones, cancel the global credential helper during clone
				// to prevent GCM from storing a ghost credential via "credential approve".
				var cloneOpts git.CloneOpts
				if credType == "token" {
					cloneOpts.ConfigArgs = []string{"credential.helper="}
				}
				printCloneProgress(label, 0, "starting")
				cloneErr := git.CloneWithProgress(cloneURL, dest, cloneOpts, func(p git.CloneProgress) {
					printCloneProgress(label, p.Percent, p.Phase)
				})
				if cloneErr != nil {
					printStatusLineFinish("x", "error", label, cloneErr.Error(), colorRed)
					errors++
					continue
				}

				// For token clones, sanitize the remote URL to remove the embedded token.
				if credType == "token" {
					plainURL := fmt.Sprintf("%s/%s.git", strings.TrimSuffix(acct.URL, "/"), repoName)
					if err := git.SetRemoteURL(dest, "origin", plainURL); err != nil {
						printStatusLine("!", "warn", label, "sanitize URL: "+err.Error(), colorOrange)
					}
				}

				// Set per-repo identity (user.name, user.email).
				wantName, wantEmail := identity.ResolveIdentity(repo, acct)
				_ = git.ConfigSet(dest, "user.name", wantName)
				_ = git.ConfigSet(dest, "user.email", wantEmail)

				// Configure per-repo credential isolation (cancels global helper,
				// sets type-specific helper: store for token, manager for GCM, empty for SSH).
				_ = credential.ConfigureRepoCredential(dest, acct, source.Account, credType, cfg.Global)

				printStatusLineFinish("+", "cloned", label, "", colorGreen)
				cloned++
			}
		}

		fmt.Printf("\nCloned: %d, Skipped: %d, Errors: %d\n", cloned, skipped, errors)
		return nil
	},
}

func init() {
	cloneCmd.Flags().StringVar(&cloneSource, "source", "", "clone repos from a specific source only")
	cloneCmd.Flags().StringVar(&cloneRepo, "repo", "", "clone a specific repo only")
	cloneCmd.Flags().BoolVar(&cloneAll, "all", false, "clone all configured repos")
}

// buildCloneURL constructs the clone URL from account + repo info.
// Repo names already contain the org prefix (e.g., "MyOrg/myorg.browser"),
// so the URL is simply: baseURL/repoName.git or git@host:repoName.git
func buildCloneURL(acct config.Account, accountKey, repoName string, repo config.Repo) (string, error) {
	credType := repo.EffectiveCredentialType(&acct)

	switch credType {
	case "ssh":
		if acct.SSH != nil && acct.SSH.Host != "" {
			return fmt.Sprintf("git@%s:%s.git", acct.SSH.Host, repoName), nil
		}
		hostname := extractHostname(acct.URL)
		return fmt.Sprintf("git@%s:%s.git", hostname, repoName), nil

	case "token":
		token, _, err := credential.ResolveToken(acct, accountKey)
		if err != nil {
			return "", err
		}
		hostname := extractHostname(acct.URL)
		// GitLab uses "oauth2" as username for token auth.
		username := acct.Username
		if acct.Provider == "gitlab" {
			username = "oauth2"
		}
		return fmt.Sprintf("https://%s:%s@%s/%s.git", username, token, hostname, repoName), nil

	default: // gcm — HTTPS with username so GCM picks the right credential.
		hostname := extractHostname(acct.URL)
		return fmt.Sprintf("https://%s@%s/%s.git", acct.Username, hostname, repoName), nil
	}
}

func extractHostname(rawURL string) string {
	// Simple extraction: strip scheme.
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return s
}

// printCloneProgress renders an in-place progress bar for a clone operation.
// Example: + cloning  git-parchis-luis/familia/ines-denia  ████████░░░░░░░░  52% Receiving
func printCloneProgress(label string, pct int, phase string) {
	const barWidth = 20
	filled := barWidth * pct / 100
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	// Shorten phase name for display.
	shortPhase := phase
	switch {
	case strings.Contains(phase, "Receiving"):
		shortPhase = "Receiving"
	case strings.Contains(phase, "Resolving"):
		shortPhase = "Resolving"
	case strings.Contains(phase, "Counting"):
		shortPhase = "Counting"
	case strings.Contains(phase, "Compressing"):
		shortPhase = "Compressing"
	case strings.Contains(phase, "Enumerating"):
		shortPhase = "Enumerating"
	}

	status := colorize(fmt.Sprintf("+ %-8s", "cloning"), colorOrange)
	progress := colorize(bar, colorOrange)
	fmt.Printf("\r%s  %-55s  %s %3d%% %-12s", status, label, progress, pct, shortPhase)
}
