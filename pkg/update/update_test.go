package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Semver tests ──

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input   string
		want    Version
		wantErr bool
	}{
		{"v1.2.3", Version{1, 2, 3}, false},
		{"1.2.3", Version{1, 2, 3}, false},
		{"v0.0.1", Version{0, 0, 1}, false},
		{"v1.2.3-dev", Version{1, 2, 3}, false},
		{"v1.2.3 (abc1234)", Version{1, 2, 3}, false},
		{"v10.20.30", Version{10, 20, 30}, false},
		{"dev", Version{}, true},
		{"", Version{}, true},
		{"v1.2", Version{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseVersion(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseVersion(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("ParseVersion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"v1.0.1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v2.0.0", "v1.9.9", false},
		// Dev builds: ParseVersion strips the "-N-gSHA-dev" suffix, so the
		// comparison is against the base tag. A dev build ahead of the tag
		// is NOT newer than the tag; a dev build behind the tag IS.
		{"v1.3.0-3-gabcdef-dev", "v1.3.0", false},
		{"v1.2.9-3-gabcdef-dev", "v1.3.0", true},
		{"v1.3.0-dev", "v1.3.0", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s→%s", tt.current, tt.latest), func(t *testing.T) {
			got, err := IsNewer(tt.current, tt.latest)
			if err != nil {
				t.Fatalf("IsNewer(%q, %q) error: %v", tt.current, tt.latest, err)
			}
			if got != tt.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	// Production / ldflags-set versions pass through unchanged.
	passthrough := []string{"v1.2.3", "v1.2.3-dev", "v0.0.1", "custom"}
	for _, v := range passthrough {
		t.Run("passthrough/"+v, func(t *testing.T) {
			if got := ResolveVersion(v); got != v {
				t.Errorf("ResolveVersion(%q) = %q, want %q", v, got, v)
			}
		})
	}

	// The "dev" placeholder triggers a git describe lookup. When the test
	// runs from inside the gitbox repo (the normal case), the result must
	// contain "-dev" and parse to a valid semver. When it can't reach git
	// (e.g. GIT_DIR pointed at a non-repo), it falls back to "dev".
	t.Run("dev/git-describe", func(t *testing.T) {
		got := ResolveVersion("dev")
		if got == "dev" {
			t.Skip("git describe unavailable in this environment")
		}
		if !strings.HasSuffix(got, "-dev") {
			t.Errorf("ResolveVersion(\"dev\") = %q, expected a *-dev suffix", got)
		}
		if _, err := ParseVersion(got); err != nil {
			t.Errorf("ResolveVersion(\"dev\") = %q, not a parseable semver: %v", got, err)
		}
	})
}

// ── Check tests ──

func TestCheckLatest_UpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"tag_name": "v2.0.0",
			"html_url": "https://github.com/test/test/releases/tag/v2.0.0",
			"published_at": "2026-01-01T00:00:00Z",
			"assets": [
				{"name": "gitbox-win-amd64.zip", "browser_download_url": "https://example.com/gitbox-win-amd64.zip"},
				{"name": "checksums.sha256", "browser_download_url": "https://example.com/checksums.sha256"}
			]
		}`)
	}))
	defer srv.Close()

	opts := Options{
		CurrentVersion: "v1.0.0",
		Repo:           "test/test",
		HTTPClient:     srv.Client(),
	}

	// Override the API URL by using a custom transport.
	origURL := srv.URL
	opts.HTTPClient.Transport = rewriteTransport{base: http.DefaultTransport, target: origURL}

	result, err := CheckLatestForce(context.Background(), opts)
	if err != nil {
		t.Fatalf("CheckLatestForce error: %v", err)
	}
	if !result.Available {
		t.Error("expected update to be available")
	}
	if result.Latest != "v2.0.0" {
		t.Errorf("latest = %q, want v2.0.0", result.Latest)
	}
}

func TestCheckLatest_AlreadyUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name": "v1.0.0", "html_url": "", "assets": []}`)
	}))
	defer srv.Close()

	opts := Options{
		CurrentVersion: "v1.0.0",
		Repo:           "test/test",
		HTTPClient:     srv.Client(),
	}
	opts.HTTPClient.Transport = rewriteTransport{base: http.DefaultTransport, target: srv.URL}

	result, err := CheckLatestForce(context.Background(), opts)
	if err != nil {
		t.Fatalf("CheckLatestForce error: %v", err)
	}
	if result.Available {
		t.Error("expected no update available")
	}
}

// ── Throttle tests ──

func TestThrottle(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, ".update-check")

	// Not throttled when file doesn't exist.
	if isThrottled(cacheFile, 24*time.Hour) {
		t.Error("expected not throttled when cache file missing")
	}

	// Write a recent timestamp.
	writeThrottleTimestamp(cacheFile)
	if !isThrottled(cacheFile, 24*time.Hour) {
		t.Error("expected throttled after recent write")
	}

	// Write an old timestamp.
	old := time.Now().Add(-25 * time.Hour)
	os.WriteFile(cacheFile, []byte(old.Format(time.RFC3339)+"\n"), 0o644)
	if isThrottled(cacheFile, 24*time.Hour) {
		t.Error("expected not throttled after 25h")
	}
}

func TestCheckLatest_NoUpdate_DoesNotThrottle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name": "v1.0.0", "html_url": "", "assets": []}`)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, ".update-check")

	opts := Options{
		CurrentVersion: "v1.0.0",
		Repo:           "test/test",
		HTTPClient:     srv.Client(),
		CacheFile:      cacheFile,
		ThrottleDur:    24 * time.Hour,
	}
	opts.HTTPClient.Transport = rewriteTransport{base: http.DefaultTransport, target: srv.URL}

	result, err := CheckLatest(context.Background(), opts)
	if err != nil {
		t.Fatalf("CheckLatest error: %v", err)
	}
	if result.Available {
		t.Error("expected no update available")
	}

	// Cache file should NOT exist — no-update results must not throttle.
	if _, err := os.Stat(cacheFile); err == nil {
		t.Error("throttle cache written after no-update result; should only cache when update is available")
	}
}

func TestCheckLatest_UpdateAvailable_Throttles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name": "v2.0.0", "html_url": "", "assets": []}`)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, ".update-check")

	opts := Options{
		CurrentVersion: "v1.0.0",
		Repo:           "test/test",
		HTTPClient:     srv.Client(),
		CacheFile:      cacheFile,
		ThrottleDur:    24 * time.Hour,
	}
	opts.HTTPClient.Transport = rewriteTransport{base: http.DefaultTransport, target: srv.URL}

	result, err := CheckLatest(context.Background(), opts)
	if err != nil {
		t.Fatalf("CheckLatest error: %v", err)
	}
	if !result.Available {
		t.Error("expected update available")
	}

	// Cache file SHOULD exist — positive results must throttle.
	if _, err := os.Stat(cacheFile); err != nil {
		t.Error("throttle cache not written after update-available result")
	}

	// Second call should be throttled (return nil).
	result2, err := CheckLatest(context.Background(), opts)
	if err != nil {
		t.Fatalf("second CheckLatest error: %v", err)
	}
	if result2 != nil {
		t.Error("expected nil result (throttled) on second call")
	}
}

// ── Checksum tests ──

func TestVerifyChecksum(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test file with known content.
	testFile := filepath.Join(tmpDir, "test.zip")
	content := []byte("hello world\n")
	os.WriteFile(testFile, content, 0o644)

	// Compute the actual hash of the written content.
	actualHash, _ := fileHash(testFile)

	checksumFile := filepath.Join(tmpDir, "checksums.sha256")
	os.WriteFile(checksumFile, []byte(actualHash+"  test.zip\n"), 0o644)

	if err := VerifyChecksum(testFile, checksumFile, "test.zip"); err != nil {
		t.Errorf("VerifyChecksum should pass: %v", err)
	}

	// Test with wrong hash.
	os.WriteFile(checksumFile, []byte("0000000000000000000000000000000000000000000000000000000000000000  test.zip\n"), 0o644)
	if err := VerifyChecksum(testFile, checksumFile, "test.zip"); err == nil {
		t.Error("VerifyChecksum should fail with wrong hash")
	}
}

// ── Helpers ──

// rewriteTransport redirects all requests to the test server.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.target[len("http://"):]
	return t.base.RoundTrip(req)
}
