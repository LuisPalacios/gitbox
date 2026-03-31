package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

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

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan filesystem for git repos and show their status",
	Long: `Walks the directory tree from the current directory (or --dir) finding all
git repositories and reports their sync status. Unlike 'status', this command
does not require a gitbox configuration — it works on any directory.

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
		repos, err := findGitRepos(dir)
		if err != nil {
			return fmt.Errorf("scanning %s: %w", dir, err)
		}

		if len(repos) == 0 {
			fmt.Printf("No git repositories found under %s\n", dir)
			return nil
		}

		sort.Strings(repos)

		home, _ := os.UserHomeDir()
		count := 0

		// Process and print each repo as we go (streaming).
		for _, repoPath := range repos {
			displayPath := makeDisplayPath(repoPath, dir, home)

			s, err := git.Status(repoPath)
			if err != nil {
				printStatusLine("x", "error", displayPath, err.Error(), colorRed)
				count++
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
				count++
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

			printStatusLine(symbol, state, displayPath, details, color)
			count++
		}

		fmt.Printf("\nScanned: %d repos\n", count)
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

// findGitRepos walks the directory tree and returns paths to all git repositories.
func findGitRepos(root string) ([]string, error) {
	var repos []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible directories.
		}
		if !d.IsDir() {
			return nil
		}

		// Skip hidden directories (except .git itself).
		name := d.Name()
		if strings.HasPrefix(name, ".") && name != ".git" {
			return filepath.SkipDir
		}

		// If this directory contains a .git subdirectory, it's a repo.
		if name == ".git" {
			repos = append(repos, filepath.Dir(path))
			return filepath.SkipDir // Don't descend into .git.
		}

		return nil
	})

	return repos, err
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
