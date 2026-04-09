package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ReleaseInfo holds metadata about a GitHub release.
type ReleaseInfo struct {
	TagName   string `json:"tag_name"`
	HTMLURL   string `json:"html_url"`
	Published string `json:"published_at"`
	Assets    []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// CheckResult is the outcome of an update check.
type CheckResult struct {
	Available bool
	Current   string
	Latest    string
	Release   *ReleaseInfo
}

// Options configures the updater.
type Options struct {
	CurrentVersion string        // e.g. "v1.2.0" from ldflags
	Repo           string        // e.g. "LuisPalacios/gitbox"
	HTTPClient     *http.Client  // nil = default with 10s timeout
	CacheFile      string        // path to throttle timestamp file
	ThrottleDur    time.Duration // default 24h
}

func (o *Options) defaults() {
	if o.Repo == "" {
		o.Repo = "LuisPalacios/gitbox"
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if o.ThrottleDur == 0 {
		o.ThrottleDur = 24 * time.Hour
	}
}

// CheckLatest queries GitHub for the latest release, respecting the throttle.
// Returns nil result (no error) if throttled.
func CheckLatest(ctx context.Context, opts Options) (*CheckResult, error) {
	opts.defaults()

	if opts.CacheFile != "" && isThrottled(opts.CacheFile, opts.ThrottleDur) {
		return nil, nil
	}

	result, err := checkLatestAPI(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Only throttle when an update was found — avoids locking out checks
	// for 24h after a "no update" result, which would hide a release
	// published during that window.
	if opts.CacheFile != "" && result.Available {
		writeThrottleTimestamp(opts.CacheFile)
	}

	return result, nil
}

// CheckLatestForce bypasses the throttle cache.
func CheckLatestForce(ctx context.Context, opts Options) (*CheckResult, error) {
	opts.defaults()
	result, err := checkLatestAPI(ctx, opts)
	if err != nil {
		return nil, err
	}

	if opts.CacheFile != "" {
		writeThrottleTimestamp(opts.CacheFile)
	}

	return result, nil
}

func checkLatestAPI(ctx context.Context, opts Options) (*CheckResult, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", opts.Repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "gitbox-updater")

	// Use GITHUB_TOKEN if available for higher rate limits.
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("GitHub API rate limit exceeded (HTTP 403)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var release ReleaseInfo
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parsing release JSON: %w", err)
	}

	// Parse current version — if it's "dev" or unparseable, treat as "always behind".
	newer, err := IsNewer(opts.CurrentVersion, release.TagName)
	if err != nil {
		// Dev builds can't compare — always report as available.
		if strings.Contains(opts.CurrentVersion, "dev") {
			newer = true
		} else {
			return nil, fmt.Errorf("comparing versions: %w", err)
		}
	}

	return &CheckResult{
		Available: newer,
		Current:   opts.CurrentVersion,
		Latest:    release.TagName,
		Release:   &release,
	}, nil
}

// ArtifactName returns the expected zip/artifact name for the current platform.
func ArtifactName() string {
	if os.Getenv("APPIMAGE") != "" {
		return "gitbox-linux-amd64.AppImage"
	}

	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "gitbox-win-amd64.zip"
	case "darwin/arm64":
		return "gitbox-macos-arm64.zip"
	case "darwin/amd64":
		return "gitbox-macos-amd64.zip"
	case "linux/amd64":
		return "gitbox-linux-amd64.zip"
	default:
		return ""
	}
}

// FindAssetURL finds the download URL for a specific asset in a release.
func FindAssetURL(release *ReleaseInfo, assetName string) string {
	for _, a := range release.Assets {
		if a.Name == assetName {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// ── Throttle helpers ──

func isThrottled(cacheFile string, dur time.Duration) bool {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return time.Since(t) < dur
}

func writeThrottleTimestamp(cacheFile string) {
	dir := filepath.Dir(cacheFile)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(cacheFile, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644)
}
