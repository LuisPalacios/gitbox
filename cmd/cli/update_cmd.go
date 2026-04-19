package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/update"
	"github.com/spf13/cobra"
)

var checkOnly bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for updates and optionally install them",
	Long:  "Check GitHub releases for a newer version of gitbox. With no flags, prompts to download and apply the update.",
	RunE:  runUpdate,
}

func init() {
	updateCmd.Flags().BoolVar(&checkOnly, "check", false, "only check if an update is available (no install)")
}

func updateOpts() update.Options {
	cacheDir := filepath.Join(config.ConfigRoot(), config.V2ConfigDir)
	return update.Options{
		CurrentVersion: update.ResolveVersion(version),
		Repo:           "LuisPalacios/gitbox",
		CacheFile:      filepath.Join(cacheDir, ".update-check"),
		ThrottleDur:    24 * time.Hour,
	}
}

func runUpdate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	opts := updateOpts()

	fmt.Printf("gitbox %s — checking for updates...\n", fullVersion())

	result, err := update.CheckLatestForce(ctx, opts)
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}

	if !result.Available {
		fmt.Printf("You are running the latest version (%s).\n", result.Current)
		return nil
	}

	fmt.Printf("\nA new version is available: %s → %s\n", result.Current, result.Latest)
	if result.Release != nil && result.Release.HTMLURL != "" {
		fmt.Printf("Release page: %s\n", result.Release.HTMLURL)
	}

	if checkOnly {
		return nil
	}

	// Prompt for confirmation.
	fmt.Print("\nDownload and install? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Println("Update cancelled.")
		return nil
	}

	// Download.
	fmt.Printf("Downloading %s...\n", update.ArtifactName())
	zipPath, err := update.DownloadRelease(ctx, result.Release, opts)
	if err != nil {
		return fmt.Errorf("downloading update: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(zipPath))

	// Apply.
	fmt.Println("Applying update...")
	if err := update.Apply(zipPath); err != nil {
		if errors.Is(err, update.ErrNeedElevation) {
			fmt.Println("\nThe install directory requires administrator privileges.")
			fmt.Println("Please re-run from an elevated terminal:")
			fmt.Println("  Run as Administrator → gitbox update")
			return nil
		}
		return fmt.Errorf("applying update: %w", err)
	}

	fmt.Printf("\nUpdated to %s. Restart gitbox to use the new version.\n", result.Latest)
	return nil
}
