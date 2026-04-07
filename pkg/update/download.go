package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// DownloadRelease downloads the platform artifact to a temp directory.
// Returns the path to the downloaded file.
func DownloadRelease(ctx context.Context, release *ReleaseInfo, opts Options) (string, error) {
	opts.defaults()

	artifact := ArtifactName()
	if artifact == "" {
		return "", fmt.Errorf("unsupported platform")
	}

	downloadURL := FindAssetURL(release, artifact)
	if downloadURL == "" {
		return "", fmt.Errorf("artifact %s not found in release %s", artifact, release.TagName)
	}

	// Create temp directory for download.
	tmpDir, err := os.MkdirTemp("", "gitbox-update-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	destPath := filepath.Join(tmpDir, artifact)

	if err := downloadFile(ctx, opts.HTTPClient, downloadURL, destPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("downloading %s: %w", artifact, err)
	}

	// Verify checksum if available.
	checksumURL := FindAssetURL(release, "checksums.sha256")
	if checksumURL != "" {
		checksumPath := filepath.Join(tmpDir, "checksums.sha256")
		if err := downloadFile(ctx, opts.HTTPClient, checksumURL, checksumPath); err == nil {
			if err := VerifyChecksum(destPath, checksumPath, artifact); err != nil {
				os.RemoveAll(tmpDir)
				return "", fmt.Errorf("checksum verification failed: %w", err)
			}
		}
		// If checksum download fails, continue without verification.
	}

	return destPath, nil
}

func downloadFile(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "gitbox-updater")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}

	return f.Close()
}
