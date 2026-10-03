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
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Published  string `json:"published_at"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
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
	// MaxMajor caps the major version this binary may update to (0 = no
	// cap). The v1 CLI sets 1 so it never "updates" to a v2 release, which
	// ships the GUI only. With a cap, the release list is scanned instead
	// of trusting releases/latest.
	MaxMajor int
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
	var (
		release *ReleaseInfo
		err     error
	)
	if opts.MaxMajor > 0 {
		release, err = fetchNewestWithinMajor(ctx, opts)
	} else {
		release, err = fetchLatest(ctx, opts)
	}
	if err != nil {
		return nil, err
	}
	if release == nil {
		// No release within the allowed major line: nothing to offer.
		return &CheckResult{Current: opts.CurrentVersion}, nil
	}

	// Compare versions. If the current version is unparseable (a dev build
	// where git describe failed, a shallow clone, or any other tagless
	// state), hide the banner rather than nag with a false positive.
	newer, err := IsNewer(opts.CurrentVersion, release.TagName)
	if err != nil {
		newer = false
	}

	return &CheckResult{
		Available: newer,
		Current:   opts.CurrentVersion,
		Latest:    release.TagName,
		Release:   release,
	}, nil
}

// fetchLatest returns the release GitHub marks as latest.
func fetchLatest(ctx context.Context, opts Options) (*ReleaseInfo, error) {
	var release ReleaseInfo
	if err := getJSON(ctx, opts, "releases/latest", &release); err != nil {
		return nil, err
	}
	return &release, nil
}

// fetchNewestWithinMajor returns the highest published, non-prerelease
// release whose major version is <= opts.MaxMajor, or nil if none exists.
func fetchNewestWithinMajor(ctx context.Context, opts Options) (*ReleaseInfo, error) {
	var releases []ReleaseInfo
	if err := getJSON(ctx, opts, "releases?per_page=100", &releases); err != nil {
		return nil, err
	}
	return newestWithinMajor(releases, opts.MaxMajor), nil
}

// newestWithinMajor picks the highest stable release with major <= maxMajor.
// Tags that don't parse as semver are ignored.
func newestWithinMajor(releases []ReleaseInfo, maxMajor int) *ReleaseInfo {
	var best *ReleaseInfo
	for i := range releases {
		r := &releases[i]
		if r.Draft || r.Prerelease {
			continue
		}
		v, err := ParseVersion(r.TagName)
		if err != nil || v.Major > maxMajor {
			continue
		}
		if best == nil {
			best = r
			continue
		}
		if newer, err := IsNewer(best.TagName, r.TagName); err == nil && newer {
			best = r
		}
	}
	return best
}

// getJSON GETs https://api.github.com/repos/<repo>/<path> and decodes it.
func getJSON(ctx context.Context, opts Options, path string, out any) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", opts.Repo, path)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "gitbox-updater")

	// Use GITHUB_TOKEN if available for higher rate limits.
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("GitHub API rate limit exceeded (HTTP 403)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parsing release JSON: %w", err)
	}
	return nil
}

// ArtifactName returns the expected zip/artifact name for the current platform.
func ArtifactName() string {
	// Set by the AppImage runtime to the path of the running AppImage.
	if runningFromAppImage() {
		return "gitbox-x86_64.AppImage"
	}
	return artifactNameFor(runtime.GOOS, runtime.GOARCH)
}

// artifactNameFor returns the release artifact name for a given GOOS/GOARCH
// pair, or "" when no release asset exists for that combination. Exported via
// ArtifactName() but separated so tests can cover every supported platform
// without runtime.GOOS/GOARCH.
func artifactNameFor(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "windows/amd64":
		return "gitbox-win-amd64.zip"
	case "windows/arm64":
		return "gitbox-win-arm64.zip"
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
