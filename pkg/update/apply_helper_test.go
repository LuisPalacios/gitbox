package update

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

const windowsTestArtifact = "gitbox-win-amd64.zip"

func TestVerifiedWindowsExecutable_AuthenticatesThePackage(t *testing.T) {
	signer := newTestSigner(t)
	signer.useAsReleaseKey(t)
	genuine := windowsUpdateArchive(t, windowsExecutableName, "signed executable", 0o755)
	changed := windowsUpdateArchive(t, windowsExecutableName, "different bytes", 0o755)
	checksums := windowsUpdateChecksums(genuine)
	sig := signer.sign(t, testTag, checksums, signatureNamespace, "sha512")

	cases := []struct {
		name, current, tag, want, wantErr string
		checksums, signature, archive     []byte
	}{
		{"valid", "v9.9.8", testTag, "signed executable", "", checksums, sig, genuine},
		{"newer build already installed", "v10.0.0", testTag, "", "not newer", checksums, sig, genuine},
		{"same build already installed", testTag, testTag, "", "not newer", checksums, sig, genuine},
		{"signature for a different tag", "v9.9.8", "v9.9.10", "", "signature", checksums, sig, genuine},
		{"archive changed", "v9.9.8", testTag, "", "checksum", checksums, sig, changed},
		{"archive and unsigned checksum changed", "v9.9.8", testTag, "", "signature", windowsUpdateChecksums(changed), sig, changed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := verifiedWindowsExecutable(tc.current, tc.tag, windowsTestArtifact, tc.checksums, tc.signature, tc.archive)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || got != nil {
					t.Fatalf("verifiedWindowsExecutable = %q, %v, want %q and no bytes", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || string(got) != tc.want {
				t.Fatalf("verifiedWindowsExecutable = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestVerifiedWindowsExecutable_RejectsUnexpectedSignedEntries(t *testing.T) {
	signer := newTestSigner(t)
	signer.useAsReleaseKey(t)
	cases := []struct {
		name, entry, data string
		mode              os.FileMode
	}{
		{"parent path", "../GitboxApp.exe", "data", 0o755},
		{"absolute path", "/GitboxApp.exe", "data", 0o755},
		{"drive relative path", "C:GitboxApp.exe", "data", 0o755},
		{"nested path", "other/GitboxApp.exe", "data", 0o755},
		{"alternate stream", "GitboxApp.exe:other", "data", 0o755},
		{"symlink", windowsExecutableName, "other.exe", os.ModeSymlink | 0o777},
		{"empty", windowsExecutableName, "", 0o755},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archive := windowsUpdateArchive(t, tc.entry, tc.data, tc.mode)
			checksums := windowsUpdateChecksums(archive)
			sig := signer.sign(t, testTag, checksums, signatureNamespace, "sha512")
			got, err := verifiedWindowsExecutable("v9.9.8", testTag, windowsTestArtifact, checksums, sig, archive)
			if err == nil || got != nil {
				t.Fatalf("accepted unexpected signed entry %q (%v)", tc.entry, err)
			}
		})
	}
}

func TestReadUpdateBytes_RefusesPartialRead(t *testing.T) {
	if data, err := readUpdateBytes(strings.NewReader("payload"), 6); err == nil || data != nil {
		t.Fatalf("readUpdateBytes = %q, %v, want rejection without a prefix", data, err)
	}
	if data, err := readUpdateBytes(strings.NewReader("payload"), 7); err != nil || string(data) != "payload" {
		t.Fatalf("readUpdateBytes at the limit = %q, %v", data, err)
	}
}

func windowsUpdateArchive(t *testing.T, name, content string, mode os.FileMode) []byte {
	t.Helper()
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	file, err := archive.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(file, content); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func windowsUpdateChecksums(archive []byte) []byte {
	return []byte(sha(string(archive)) + "  " + windowsTestArtifact + "\n")
}
