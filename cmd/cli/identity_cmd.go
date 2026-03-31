package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/spf13/cobra"
)

var identityCmd = &cobra.Command{
	Use:   "identity",
	Short: "Manage per-repo git identity (user.name, user.email)",
}

var identityCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check global and per-repo git identity configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Check global ~/.gitconfig identity.
		fmt.Printf("%s\n", colorize("Global Identity", colorWhite))
		gs := identity.CheckGlobalIdentity()
		if gs.HasName || gs.HasEmail {
			parts := []string{}
			if gs.HasName {
				parts = append(parts, fmt.Sprintf("user.name=%q", gs.Name))
			}
			if gs.HasEmail {
				parts = append(parts, fmt.Sprintf("user.email=%q", gs.Email))
			}
			printStatusLine("!", "warning", "~/.gitconfig", strings.Join(parts, ", ")+" (NOT RECOMMENDED)", colorOrange)
		} else {
			printStatusLine("✓", "ok", "~/.gitconfig", "no global identity set", colorGreen)
		}
		fmt.Println()

		// 2. Check per-repo identity.
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("%s\n", colorize("Per-Repo Identity", colorWhite))
		globalFolder := config.ExpandTilde(cfg.Global.Folder)

		type repoResult struct {
			source, repo, label string
			wantName, wantEmail string
			curName, curEmail   string
			cloned              bool
		}
		var results []repoResult

		for _, srcKey := range cfg.OrderedSourceKeys() {
			src := cfg.Sources[srcKey]
			acct := cfg.Accounts[src.Account]
			sourceFolder := src.EffectiveFolder(srcKey)
			for _, rKey := range src.OrderedRepoKeys() {
				repo := src.Repos[rKey]
				path := status.ResolveRepoPath(globalFolder, sourceFolder, rKey, repo)
				wantName, wantEmail := identity.ResolveIdentity(repo, acct)
				label := fmt.Sprintf("%s/%s", srcKey, rKey)

				if !git.IsRepo(path) {
					results = append(results, repoResult{
						source: srcKey, repo: rKey, label: label,
						wantName: wantName, wantEmail: wantEmail,
						cloned: false,
					})
					continue
				}

				curName, _ := git.ConfigGet(path, "user.name")
				curEmail, _ := git.ConfigGet(path, "user.email")
				results = append(results, repoResult{
					source: srcKey, repo: rKey, label: label,
					wantName: wantName, wantEmail: wantEmail,
					curName: curName, curEmail: curEmail,
					cloned: true,
				})
			}
		}

		sort.Slice(results, func(i, j int) bool {
			return results[i].label < results[j].label
		})

		ok, mismatch, notCloned := 0, 0, 0
		for _, r := range results {
			if !r.cloned {
				notCloned++
				continue
			}
			if r.curName == r.wantName && r.curEmail == r.wantEmail {
				ok++
				if verbose {
					printStatusLine("✓", "ok", r.label, fmt.Sprintf("%s <%s>", r.curName, r.curEmail), colorGreen)
				}
			} else {
				mismatch++
				details := []string{}
				if r.curName != r.wantName {
					details = append(details, fmt.Sprintf("name: %q → %q", r.curName, r.wantName))
				}
				if r.curEmail != r.wantEmail {
					details = append(details, fmt.Sprintf("email: %q → %q", r.curEmail, r.wantEmail))
				}
				printStatusLine("!", "mismatch", r.label, strings.Join(details, ", "), colorOrange)
			}
		}

		fmt.Printf("\nOK: %d, Mismatch: %d, Not cloned: %d\n", ok, mismatch, notCloned)
		if mismatch > 0 {
			fmt.Printf("Run '%s' to fix mismatches.\n", "gitbox identity fix")
		}
		return nil
	},
}

var identityFixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Remove global identity and fix per-repo identity",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Fix global ~/.gitconfig identity.
		fmt.Printf("%s\n", colorize("Global Identity", colorWhite))
		gs := identity.CheckGlobalIdentity()
		if gs.HasName || gs.HasEmail {
			if err := identity.RemoveGlobalIdentity(); err != nil {
				printStatusLine("x", "error", "~/.gitconfig", err.Error(), colorRed)
			} else {
				removed := []string{}
				if gs.HasName {
					removed = append(removed, "user.name")
				}
				if gs.HasEmail {
					removed = append(removed, "user.email")
				}
				printStatusLine("✓", "fixed", "~/.gitconfig", "removed "+strings.Join(removed, ", "), colorGreen)
			}
		} else {
			printStatusLine("✓", "ok", "~/.gitconfig", "no global identity to remove", colorGreen)
		}
		fmt.Println()

		// 2. Fix per-repo identity.
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("%s\n", colorize("Per-Repo Identity", colorWhite))
		globalFolder := config.ExpandTilde(cfg.Global.Folder)

		fixed, ok, skipped := 0, 0, 0
		for _, srcKey := range cfg.OrderedSourceKeys() {
			src := cfg.Sources[srcKey]
			acct := cfg.Accounts[src.Account]
			sourceFolder := src.EffectiveFolder(srcKey)
			for _, rKey := range src.OrderedRepoKeys() {
				repo := src.Repos[rKey]
				path := status.ResolveRepoPath(globalFolder, sourceFolder, rKey, repo)
				label := fmt.Sprintf("%s/%s", srcKey, rKey)

				if !git.IsRepo(path) {
					skipped++
					continue
				}

				wantName, wantEmail := identity.ResolveIdentity(repo, acct)
				fixedName, fixedEmail, err := identity.EnsureRepoIdentity(path, wantName, wantEmail)
				if err != nil {
					printStatusLine("x", "error", label, err.Error(), colorRed)
					continue
				}
				if fixedName || fixedEmail {
					fixed++
					printStatusLine("✓", "fixed", label, fmt.Sprintf("%s <%s>", wantName, wantEmail), colorGreen)
				} else {
					ok++
					if verbose {
						printStatusLine("✓", "ok", label, fmt.Sprintf("%s <%s>", wantName, wantEmail), colorGreen)
					}
				}
			}
		}

		fmt.Printf("\nFixed: %d, Already OK: %d, Skipped (not cloned): %d\n", fixed, ok, skipped)
		return nil
	},
}

func init() {
	identityCmd.AddCommand(identityCheckCmd, identityFixCmd)
}
