package update

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// releaseServer serves one fake release: the platform artifact at /artifact
// and, when checksums is non-empty, a checksums file at /checksums.
// checksumsStatus lets a test make the checksums download fail.
type releaseServer struct {
	artifact        string
	checksums       string
	checksumsStatus int
}

func (rs releaseServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/artifact":
			fmt.Fprint(w, rs.artifact)
		case "/checksums":
			if rs.checksumsStatus != 0 {
				w.WriteHeader(rs.checksumsStatus)
				return
			}
			fmt.Fprint(w, rs.checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// release builds a ReleaseInfo for srv; withChecksums controls whether the
// release lists a checksums.sha256 asset at all.
func release(t *testing.T, srv *httptest.Server, withChecksums bool) *ReleaseInfo {
	t.Helper()
	assets := []map[string]string{{"name": ArtifactName(), "browser_download_url": srv.URL + "/artifact"}}
	if withChecksums {
		assets = append(assets, map[string]string{"name": checksumsAsset, "browser_download_url": srv.URL + "/checksums"})
	}
	raw, err := json.Marshal(map[string]any{"tag_name": "v9.9.9", "assets": assets})
	if err != nil {
		t.Fatal(err)
	}
	var rel ReleaseInfo
	if err := json.Unmarshal(raw, &rel); err != nil {
		t.Fatal(err)
	}
	return &rel
}

// isolateTemp points os.TempDir at a fresh directory and returns it, so a
// test can check that a refused update leaves nothing behind.
func isolateTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, dir)
	}
	return dir
}

func sha(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }

func TestDownloadRelease_VerifiedArtifact(t *testing.T) {
	if ArtifactName() == "" {
		t.Skip("no release artifact for this platform")
	}
	isolateTemp(t)
	const body = "artifact bytes"
	srv := releaseServer{artifact: body, checksums: sha(body) + "  " + ArtifactName() + "\n"}.start(t)

	path, err := DownloadRelease(context.Background(), release(t, srv, true), Options{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("DownloadRelease: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != body {
		t.Fatalf("downloaded artifact = %q, %v; want %q", got, err, body)
	}
}

// Every way the checksum can't be verified must refuse the update and leave
// no temp files behind.
func TestDownloadRelease_FailsClosed(t *testing.T) {
	if ArtifactName() == "" {
		t.Skip("no release artifact for this platform")
	}
	const body = "artifact bytes"
	cases := []struct {
		name          string
		server        releaseServer
		withChecksums bool
		wantErr       string
	}{
		{"no checksums asset", releaseServer{artifact: body}, false, "refusing to install an unverified update"},
		{"checksums download fails", releaseServer{artifact: body, checksumsStatus: http.StatusNotFound}, true, "refusing to install an unverified update"},
		{"artifact not listed", releaseServer{artifact: body, checksums: sha(body) + "  some-other.zip\n"}, true, "not found in checksums file"},
		{"checksum mismatch", releaseServer{artifact: body, checksums: sha("tampered") + "  " + ArtifactName() + "\n"}, true, "checksum mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolateTemp(t)
			srv := tc.server.start(t)

			path, err := DownloadRelease(context.Background(), release(t, srv, tc.withChecksums), Options{HTTPClient: srv.Client()})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("DownloadRelease error = %v, want it to contain %q", err, tc.wantErr)
			}
			if path != "" {
				t.Errorf("DownloadRelease returned path %q on failure", path)
			}
			if left, _ := os.ReadDir(tmp); len(left) != 0 {
				t.Errorf("refused update left %d entries in the temp dir", len(left))
			}
		})
	}
}
