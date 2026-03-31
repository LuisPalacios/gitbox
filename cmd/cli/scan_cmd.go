package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/spf13/cobra"
)

var (
	scanDir  string
	scanPull bool
)

// ANSI true-color codes (matching Oh-My-Posh palette).
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[38;2;97;253;95m"   // #61fd5f — clean/ok
	colorOrange = "\033[38;2;240;118;35m"  // #F07623 — dirty
	colorBlue   = "\033[38;2;75;149;233m"  // #4B95E9 — ahead
	colorPurple = "\033[38;2;217;28;154m"  // #D91C9A — behind
	colorRed    = "\033[38;2;216;30;91m"   // #D81E5B — diverged/conflict/error
	colorCyan   = "\033[38;2;97;253;255m"  // #61fdff — section headers
	colorWhite  = "\033[1m"                // bold white — titles
)

func useColor() bool {
	// Disable color if NO_COLOR is set or output is not a terminal.
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	// On Windows, modern terminals (Windows Terminal, VS Code) support ANSI.
	// Git Bash (MSYS) always does.
	return true
}

func colorize(text, color string) string {
	if !useColor() {
		return text
	}
	return color + text + colorReset
}

var colorYellow = "\033[38;2;255;215;0m" // #FFD700 — orphan tags

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan filesystem for git repos and show their status",
	Long: `Walks the directory tree from the current directory (or --dir) finding all
git repositories and reports their sync status.

When a gitbox configuration exists, each repo is annotated as [tracked] or
[ORPHAN]. Orphan repos can be adopted with 'gitbox adopt'.

Use --pull to also pull repos that are behind (fast-forward only).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := scanDir
		if dir == "" {
			var err error
			dir, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}
		}
		dir = filepath.Clean(dir)

		// Find all git repos.
		repos, err := git.FindRepos(dir)
		if err != nil {
			return fmt.Errorf("scanning %s: %w", dir, err)
		}

		if len(repos) == 0 {
			fmt.Printf("No git repositories found under %s\n", dir)
			return nil
		}

		sort.Strings(repos)

		// Try to load config for orphan annotation.
		var orphanSet map[string]*adopt.OrphanRepo
		cfg, cfgErr := loadConfig()
		if cfgErr == nil {
			parentFolder := config.ExpandTilde(cfg.Global.Folder)
			absDir, _ := filepath.Abs(dir)
			absParent, _ := filepath.Abs(parentFolder)
			// Only annotate if scanning within the gitbox parent folder.
			if strings.HasPrefix(strings.ToLower(filepath.ToSlash(absDir)), strings.ToLower(filepath.ToSlash(absParent))) {
				orphans, _ := adopt.FindOrphans(cfg)
				orphanSet = make(map[string]*adopt.OrphanRepo, len(orphans))
				for i := range orphans {
					key := strings.ToLower(filepath.ToSlash(filepath.Clean(orphans[i].Path)))
					orphanSet[key] = &orphans[i]
				}
			}
		}

		home, _ := os.UserHomeDir()
		tracked := 0
		orphanCount := 0
		total := 0

		for _, repoPath := range repos {
			displayPath := makeDisplayPath(repoPath, dir, home)

			s, err := git.Status(repoPath)
			if err != nil {
				printStatusLine("x", "error", displayPath, err.Error(), colorRed)
				total++
				continue
			}

			symbol, state, details := classifyStatus(s)

			// Pull if requested and repo is behind + clean.
			if scanPull && s.Behind > 0 && s.Modified == 0 && s.Untracked == 0 && s.Conflicts == 0 {
				if pullErr := git.Pull(repoPath); pullErr != nil {
					details = fmt.Sprintf("pull failed: %v", pullErr)
					symbol, state = "!", "error"
					printStatusLine(symbol, state, displayPath, details, colorRed)
				} else {
					printStatusLine("+", "pulled", displayPath, fmt.Sprintf("%d behind → ok", s.Behind), colorGreen)
				}
				total++
				continue
			}

			color := colorGreen
			switch state {
			case "dirty":
				color = colorOrange
			case "behind":
				color = colorPurple
			case "ahead":
				color = colorBlue
			case "diverged", "conflict", "error":
				color = colorRed
			}

			// Annotate with tracking status if config is available.
			tag := ""
			if orphanSet != nil {
				key := strings.ToLower(filepath.ToSlash(filepath.Clean(repoPath)))
				if o, isOrphan := orphanSet[key]; isOrphan {
					orphanCount++
					if o.LocalOnly {
						tag = colorize(" [ORPHAN — local only]", colorYellow)
					} else if o.MatchedAccount != "" {
						tag = colorize(fmt.Sprintf(" [ORPHAN → %s]", o.MatchedAccount), colorYellow)
					} else {
						tag = colorize(" [ORPHAN — unknown account]", colorRed)
					}
				} else {
					tracked++
					tag = colorize(" [tracked]", "\033[2m") // dim
				}
			}

			if details != "" {
				displayPath = fmt.Sprintf("%-50s  %s", displayPath, details)
			}
			fmt.Printf("%s  %s%s\n", colorize(fmt.Sprintf("%s %-8s", symbol, state), color), displayPath, tag)
			total++
		}

		// Summary.
		if orphanSet != nil {
			fmt.Printf("\nScanned: %d repos (%d tracked, %d orphans)\n", total, tracked, orphanCount)
		} else {
			fmt.Printf("\nScanned: %d repos\n", total)
		}
		return nil
	},
}

func init() {
	scanCmd.Flags().StringVar(&scanDir, "dir", "", "directory to scan (default: current directory)")
	scanCmd.Flags().BoolVar(&scanPull, "pull", false, "also pull repos that are behind (fast-forward only)")
}

func makeDisplayPath(repoPath, scanDir, home string) string {
	displayPath := repoPath
	if rel, err := filepath.Rel(scanDir, repoPath); err == nil {
		displayPath = rel
	}
	if home != "" && strings.HasPrefix(repoPath, home) {
		tilded := "~" + repoPath[len(home):]
		if len(tilded) < len(displayPath) {
			displayPath = tilded
		}
	}
	if runtime.GOOS == "windows" {
		displayPath = filepath.ToSlash(displayPath)
	}
	return displayPath
}


// classifyStatus returns symbol, state name, and details for a repo status.
func classifyStatus(s git.RepoStatus) (symbol, state, details string) {
	switch {
	case s.Conflicts > 0:
		return "!", "conflict", fmt.Sprintf("%d conflicts", s.Conflicts)
	case s.Modified > 0 || s.Added > 0 || s.Deleted > 0 || s.Untracked > 0:
		parts := []string{}
		if s.Modified > 0 {
			parts = append(parts, fmt.Sprintf("%d modified", s.Modified))
		}
		if s.Added > 0 {
			parts = append(parts, fmt.Sprintf("%d staged", s.Added))
		}
		if s.Deleted > 0 {
			parts = append(parts, fmt.Sprintf("%d deleted", s.Deleted))
		}
		if s.Untracked > 0 {
			parts = append(parts, fmt.Sprintf("%d untracked", s.Untracked))
		}
		return "!", "dirty", strings.Join(parts, ", ")
	case s.Ahead > 0 && s.Behind > 0:
		return "!", "diverged", fmt.Sprintf("%d ahead, %d behind", s.Ahead, s.Behind)
	case s.Behind > 0:
		return "<", "behind", fmt.Sprintf("%d behind", s.Behind)
	case s.Ahead > 0:
		return ">", "ahead", fmt.Sprintf("%d ahead", s.Ahead)
	default:
		return "+", "ok", ""
	}
}
