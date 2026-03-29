package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/spf13/cobra"
)

// fullVersion returns the display version string.
// CI builds:    "v0.2.0 (abc1234)" — version and commit set via ldflags
// Local builds: "dev-a99cf17"      — auto-detected from git at runtime
func fullVersion() string {
	v, c := version, commit

	// If not set by ldflags, try to detect from git at runtime.
	if v == "dev" {
		if tag := gitDescribe(); tag != "" {
			v = tag + "-dev"
		}
	}
	if c == "none" {
		if sha := gitShortSHA(); sha != "" {
			c = sha
		}
	}

	if v == "dev" {
		return fmt.Sprintf("dev-%s", c)
	}
	return fmt.Sprintf("%s (%s)", v, c)
}

func gitDescribe() string {
	cmd := exec.Command(git.GitBin(), "describe", "--tags", "--always")
	cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitShortSHA() string {
	cmd := exec.Command(git.GitBin(), "rev-parse", "--short", "HEAD")
	cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of gitboxcmd",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("gitboxcmd %s\n", fullVersion())
	},
}
