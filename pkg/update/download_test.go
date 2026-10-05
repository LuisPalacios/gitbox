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
	"sync/atomic"
	"testing"
)

// releaseServer serves one fake release: the platform artifact at
// /artifact, a checksums file at /checksums and its signature at /sig.
// checksumsStatus and sigStatus let a test make those downloads fail;
// artifactHits, when set, counts artifact downloads.
type releaseServer struct {
	artifact        string
	checksums       string
	checksumsStatus int
	sig             []byte
	sigStatus       int
	artifactHits    *atomic.Int32
}

func (rs releaseServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/artifact":
			if rs.artifactHits != nil {
				rs.artifactHits.Add(1)
			}
			fmt.Fprint(w, rs.artifact)
		case "/checksums":
			if rs.checksumsStatus != 0 {
				w.WriteHeader(rs.checksumsStatus)
				return
			}
			fmt.Fprint(w, rs.checksums)
		case "/sig":
			if rs.sigStatus != 0 {
				w.WriteHeader(rs.sigStatus)
				return
			}
			_, _ = w.Write(rs.sig)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testTag is the tag every fake release is published under.
const testTag = "v9.9.9"

// release builds a ReleaseInfo for srv; withChecksums and withSig control
// whether the release lists the checksums and signature assets at all.
func release(t *testing.T, srv *httptest.Server, withChecksums, withSig bool) *ReleaseInfo {
	t.Helper()
	assets := []map[string]string{{"name": ArtifactName(), "browser_download_url": srv.URL + "/artifact"}}
	if withChecksums {
		assets = append(assets, map[string]string{"name": checksumsAsset, "browser_download_url": srv.URL + "/checksums"})
	}
	if withSig {
		assets = append(assets, map[string]string{"name": signatureAsset, "browser_download_url": srv.URL + "/sig"})
	}
	raw, err := json.Marshal(map[string]any{"tag_name": testTag, "assets": assets})
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
	signer := newTestSigner(t)
	signer.useAsReleaseKey(t)
	const body = "artifact bytes"
	checksums := sha(body) + "  " + ArtifactName() + "\n"
	srv := releaseServer{
		artifact:  body,
		checksums: checksums,
		sig:       signer.sign(t, testTag, []byte(checksums), signatureNamespace, "sha512"),
	}.start(t)

	path, err := DownloadRelease(context.Background(), release(t, srv, true, true), Options{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("DownloadRelease: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != body {
		t.Fatalf("downloaded artifact = %q, %v; want %q", got, err, body)
	}
}

// Every way the checksums or their signature can't be verified must refuse
// the update and leave no temp files behind. A bad signature is caught
// before the artifact is downloaded at all.
func TestDownloadRelease_FailsClosed(t *testing.T) {
	if ArtifactName() == "" {
		t.Skip("no release artifact for this platform")
	}
	signer := newTestSigner(t)
	signer.useAsReleaseKey(t)
	other := newTestSigner(t)

	const body = "artifact bytes"
	sign := func(s testSigner, tag, checksums string) []byte {
		return s.sign(t, tag, []byte(checksums), signatureNamespace, "sha512")
	}
	good := sha(body) + "  " + ArtifactName() + "\n"
	notListed := sha(body) + "  some-other.zip\n"
	mismatch := sha("tampered") + "  " + ArtifactName() + "\n"
	const unverified = "refusing to install an unverified update"

	cases := []struct {
		name          string
		server        releaseServer
		withChecksums bool
		withSig       bool
		wantErr       string
		wantArtifact  bool // whether the artifact may be downloaded at all
	}{
		{"no checksums asset", releaseServer{sig: sign(signer, testTag, good)}, false, true, unverified, false},
		{"checksums download fails", releaseServer{checksumsStatus: http.StatusNotFound, sig: sign(signer, testTag, good)}, true, true, unverified, false},
		{"no signature asset", releaseServer{checksums: good}, true, false, "has no " + signatureAsset, false},
		{"signature download fails", releaseServer{checksums: good, sigStatus: http.StatusNotFound}, true, true, unverified, false},
		{"signed by another key", releaseServer{checksums: good, sig: sign(other, testTag, good)}, true, true, "unknown key", false},
		{"signed for another tag", releaseServer{checksums: good, sig: sign(signer, "v9.9.8", good)}, true, true, "does not match release " + testTag, false},
		{"checksums replaced after signing", releaseServer{checksums: mismatch, sig: sign(signer, testTag, good)}, true, true, "does not match release", false},
		{"artifact not listed", releaseServer{checksums: notListed, sig: sign(signer, testTag, notListed)}, true, true, "not found in checksums file", true},
		{"checksum mismatch", releaseServer{checksums: mismatch, sig: sign(signer, testTag, mismatch)}, true, true, "checksum mismatch", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolateTemp(t)
			hits := &atomic.Int32{}
			tc.server.artifact = body
			tc.server.artifactHits = hits
			srv := tc.server.start(t)

			path, err := DownloadRelease(context.Background(), release(t, srv, tc.withChecksums, tc.withSig), Options{HTTPClient: srv.Client()})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("DownloadRelease error = %v, want it to contain %q", err, tc.wantErr)
			}
			if path != "" {
				t.Errorf("DownloadRelease returned path %q on failure", path)
			}
			if left, _ := os.ReadDir(tmp); len(left) != 0 {
				t.Errorf("refused update left %d entries in the temp dir", len(left))
			}
			if fetched := hits.Load() > 0; fetched && !tc.wantArtifact {
				t.Error("artifact was downloaded although the signature check failed")
			}
		})
	}
}
