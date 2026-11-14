package update

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestAllowTokenForHost_ExactHosts(t *testing.T) {
	for _, host := range []string{"api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "uploads.github.com"} {
		t.Run(host, func(t *testing.T) {
			if !allowTokenForHost(host) {
				t.Errorf("host %q must allow the token", host)
			}
		})
	}
	for _, host := range []string{"", "GITHUB.COM", "GitHub.com", "github.com.evil.example", "evil-github.com", "evil.github.com", "api.github.com.", "localhost", "127.0.0.1"} {
		t.Run(host, func(t *testing.T) {
			if allowTokenForHost(host) {
				t.Errorf("host %q must not allow the token", host)
			}
		})
	}
}

func TestDownloadFile_LocalhostWithoutToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("localhost received Authorization")
		}
		fmt.Fprint(w, "download")
	}))
	defer srv.Close()
	if err := downloadFile(context.Background(), srv.Client(), srv.URL, filepath.Join(t.TempDir(), "artifact")); err != nil {
		t.Fatal(err)
	}
}

// updateTransport lets http.Client handle real redirects without DNS or network.
type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestUpdater_TokenRedirectPolicy(t *testing.T) {
	const token = "token test-token"
	type redirectCase struct {
		name   string
		urls   []string
		want   []string
		inject bool
	}
	cases := []redirectCase{
		{"same host", []string{"https://github.com/start", "https://github.com/end"}, []string{token, token}, false},
		{"allowed subdomain", []string{"https://github.com/start", "https://api.github.com/end"}, []string{token, token}, false},
		{"cross domain stays stripped", []string{"https://github.com/start", "https://release-assets.githubusercontent.com/end"}, []string{token, ""}, false},
		{"unlisted subdomain", []string{"https://github.com/start", "https://evil.github.com/end"}, []string{token, ""}, false},
		{"unlisted host", []string{"https://github.com/start", "https://evil.example/end"}, []string{token, ""}, false},
		{"downgrade", []string{"https://github.com/start", "http://github.com/end"}, []string{token, ""}, false},
		{"return from unlisted host", []string{"https://github.com/start", "https://evil.github.com/hop", "https://github.com/end"}, []string{token, "", ""}, false},
		{"return from downgrade", []string{"https://github.com/start", "http://github.com/hop", "https://github.com/end"}, []string{token, "", ""}, false},
		{"callback cannot restore token", []string{"https://github.com/start", "https://evil.github.com/hop", "https://github.com/end"}, []string{token, "", ""}, true},
		{"initial unlisted host", []string{"https://evil.example/start", "https://github.com/end"}, []string{"", ""}, false},
		{"initial HTTP", []string{"http://github.com/start", "https://github.com/end"}, []string{"", ""}, false},
		{"initial uppercase", []string{"https://GITHUB.COM/start"}, []string{""}, false},
	}
	for _, host := range []string{"api.github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "uploads.github.com"} {
		cases = append(cases, redirectCase{host, []string{"https://" + host + "/start"}, []string{token}, false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", "test-token")
			hits, callbacks := 0, 0
			client := &http.Client{Transport: updateTransport(func(req *http.Request) (*http.Response, error) {
				if hits >= len(tc.urls) {
					t.Fatal("unexpected extra request")
				}
				if req.URL.String() != tc.urls[hits] {
					t.Errorf("request %d URL = %q, want %q", hits, req.URL, tc.urls[hits])
				}
				if got := req.Header.Get("Authorization"); got != tc.want[hits] {
					t.Errorf("request %d Authorization = %q, want %q", hits, got, tc.want[hits])
				}
				hits++
				resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("download")), Request: req}
				if hits < len(tc.urls) {
					resp.StatusCode = http.StatusFound
					resp.Header.Set("Location", tc.urls[hits])
				}
				return resp, nil
			})}
			if tc.inject {
				client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
					callbacks++
					req.Header.Set("Authorization", token)
					return nil
				}
			}
			if err := downloadFile(context.Background(), client, tc.urls[0], filepath.Join(t.TempDir(), "artifact")); err != nil {
				t.Fatal(err)
			}
			if hits != len(tc.urls) {
				t.Errorf("requests = %d, want %d", hits, len(tc.urls))
			}
			if tc.inject && callbacks != len(tc.urls)-1 {
				t.Errorf("redirect callbacks = %d, want %d", callbacks, len(tc.urls)-1)
			}
			if !tc.inject && client.CheckRedirect != nil {
				t.Error("download mutated the caller's redirect policy")
			}
		})
	}
}

func TestUpdater_RedirectLimits(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	stop := errors.New("caller stopped redirect")
	for _, tc := range []struct {
		name     string
		policy   func(*http.Request, []*http.Request) error
		wantErr  error
		wantHits int
	}{
		{"default limit", nil, nil, 10},
		{"caller error", func(*http.Request, []*http.Request) error { return stop }, stop, 1},
		{"caller last response", func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			client := &http.Client{CheckRedirect: tc.policy, Transport: updateTransport(func(req *http.Request) (*http.Response, error) {
				hits++
				if hits > 10 {
					t.Fatal("default redirect limit was lost")
				}
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://github.com/again"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			})}
			err := downloadFile(context.Background(), client, "https://github.com/start", filepath.Join(t.TempDir(), "artifact"))
			if err == nil {
				t.Fatal("expected redirect error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
			if tc.name == "default limit" && !strings.Contains(err.Error(), "stopped after 10 redirects") {
				t.Errorf("error = %v, want default redirect limit", err)
			}
			if tc.name == "caller last response" && err.Error() != "HTTP 302" {
				t.Errorf("error = %v, want HTTP 302", err)
			}
			if hits != tc.wantHits {
				t.Errorf("requests = %d, want %d", hits, tc.wantHits)
			}
		})
	}
}

func TestCheckLatest_RedirectWithoutToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	hits := 0
	client := &http.Client{Transport: updateTransport(func(req *http.Request) (*http.Response, error) {
		hits++
		resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"tag_name":"v9.9.9"}`)), Request: req}
		switch hits {
		case 1:
			if req.Header.Get("Authorization") != "token test-token" {
				t.Error("initial API request missing token")
			}
			resp.StatusCode = http.StatusFound
			resp.Header.Set("Location", "https://evil.api.github.com/release")
		case 2:
			if req.Header.Get("Authorization") != "" {
				t.Error("API redirect sent token to unlisted subdomain")
			}
		default:
			t.Fatal("unexpected extra request")
		}
		return resp, nil
	})}
	result, err := CheckLatest(context.Background(), Options{HTTPClient: client, CurrentVersion: "v1.0.0"})
	if err != nil || !result.Available || hits != 2 {
		t.Fatalf("CheckLatest = %v, %v, requests = %d; want available update after 2 requests", result, err, hits)
	}
}
