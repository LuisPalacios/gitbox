package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// checksumsAsset is the release asset listing the SHA256 of every artifact.
const checksumsAsset = "checksums.sha256"

// DownloadRelease downloads the platform artifact to a temp directory and
// verifies it in two steps: the release's checksums file must carry a valid
// signature by the release signing key for this tag, and the artifact's
// SHA256 must match that file. Any problem (missing, unreadable, unsigned,
// signed by another key or for another tag, artifact not listed, mismatch)
// is an error. Returns the path to the downloaded file.
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

	// Fail closed: an update is only installed after its SHA256 matches a
	// checksums file signed by the release key, so a release missing either
	// file is refused before the artifact is even downloaded.
	checksumURL := FindAssetURL(release, checksumsAsset)
	if checksumURL == "" {
		return "", fmt.Errorf("release %s has no %s; refusing to install an unverified update", release.TagName, checksumsAsset)
	}
	signatureURL := FindAssetURL(release, signatureAsset)
	if signatureURL == "" {
		return "", fmt.Errorf("release %s has no %s; refusing to install an unverified update", release.TagName, signatureAsset)
	}

	// Create temp directory for download.
	tmpDir, err := os.MkdirTemp("", "gitbox-update-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	checksumPath := filepath.Join(tmpDir, checksumsAsset)
	if err := downloadFile(ctx, opts.HTTPClient, checksumURL, checksumPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("downloading %s: %w; refusing to install an unverified update", checksumsAsset, err)
	}
	signaturePath := filepath.Join(tmpDir, signatureAsset)
	if err := downloadFile(ctx, opts.HTTPClient, signatureURL, signaturePath); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("downloading %s: %w; refusing to install an unverified update", signatureAsset, err)
	}
	if err := verifySignatureFiles(release.TagName, checksumPath, signaturePath); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("signature verification failed: %w; refusing to install an unverified update", err)
	}

	destPath := filepath.Join(tmpDir, artifact)
	if err := downloadFile(ctx, opts.HTTPClient, downloadURL, destPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("downloading %s: %w", artifact, err)
	}
	if err := VerifyChecksum(destPath, checksumPath, artifact); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("checksum verification failed: %w", err)
	}

	return destPath, nil
}

// verifySignatureFiles checks the downloaded signature against the
// downloaded checksums file for tag.
func verifySignatureFiles(tag, checksumPath, signaturePath string) error {
	checksums, err := os.ReadFile(checksumPath)
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(signaturePath)
	if err != nil {
		return err
	}
	return verifyReleaseSignature(tag, checksums, sig, releaseSigningKey)
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
