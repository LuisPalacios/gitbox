package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// The testdata signatures were made by real ssh-keygen with a throwaway key
// (private half discarded), over signedMessage("v9.9.9", checksums.sha256):
//
//	valid.sig            ssh-keygen -Y sign -n gitbox-release, test key
//	wrong-namespace.sig  ssh-keygen -Y sign -n file, test key
//	foreign-key.sig      ssh-keygen -Y sign -n gitbox-release, another key
const fixtureTag = "v9.9.9"

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestVerifyReleaseSignature_SSHKeygenFixtures(t *testing.T) {
	key := mustParsePublicKey(readTestdata(t, "test-signing-key.pub"))
	checksums := readTestdata(t, "checksums.sha256")
	valid := readTestdata(t, "valid.sig")

	if err := verifyReleaseSignature(fixtureTag, checksums, valid, key); err != nil {
		t.Fatalf("valid ssh-keygen signature rejected: %v", err)
	}

	tampered := bytes.Replace(checksums, []byte("aaaa1111"), []byte("ffff9999"), 1)
	cases := []struct {
		name      string
		tag       string
		checksums []byte
		sig       []byte
		wantErr   string
	}{
		{"another tag", "v9.9.8", checksums, valid, "does not match release v9.9.8"},
		{"tampered checksums", fixtureTag, tampered, valid, "does not match release"},
		{"wrong namespace", fixtureTag, checksums, readTestdata(t, "wrong-namespace.sig"), "namespace"},
		{"foreign key", fixtureTag, checksums, readTestdata(t, "foreign-key.sig"), "unknown key"},
		{"empty signature", fixtureTag, checksums, nil, "not an SSH signature"},
		{"not armored", fixtureTag, checksums, []byte("garbage"), "not an SSH signature"},
		{"truncated", fixtureTag, checksums, truncateArmored(t, valid), "malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyReleaseSignature(tc.tag, tc.checksums, tc.sig, key)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// The release signing key the binary ships with must be the one
// allowed_signers publishes for manual verification.
func TestReleaseSigningKey_MatchesAllowedSigners(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "allowed_signers"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			entries = append(entries, line)
		}
	}
	if len(entries) != 1 {
		t.Fatalf("allowed_signers has %d entries, want exactly 1", len(entries))
	}
	fields := strings.Fields(entries[0])
	want := []string{"gitbox-release", `namespaces="` + signatureNamespace + `"`}
	if len(fields) < 4 || fields[0] != want[0] || fields[1] != want[1] {
		t.Fatalf("allowed_signers entry = %q, want it to start with %q", entries[0], strings.Join(want, " "))
	}
	key := mustParsePublicKey([]byte(strings.Join(fields[2:], " ")))
	if !bytes.Equal(key.Marshal(), releaseSigningKey.Marshal()) {
		t.Fatal("allowed_signers and pkg/update/release-signing-key.pub list different keys")
	}
}

// A signature over an unsupported hash must be refused even when the key
// and namespace are right.
func TestVerifyReleaseSignature_UnsupportedHash(t *testing.T) {
	signer := newTestSigner(t)
	checksums := []byte("aaaa1111  x.zip\n")
	sig := signer.sign(t, fixtureTag, checksums, signatureNamespace, "md5")
	if err := verifyReleaseSignature(fixtureTag, checksums, sig, signer.pub); err == nil || !strings.Contains(err.Error(), "unsupported signature hash") {
		t.Fatalf("error = %v, want unsupported signature hash", err)
	}
}

// truncateArmored re-armors the first half of a signature blob, so it is
// valid PEM holding an incomplete sshsig structure.
func truncateArmored(t *testing.T, armored []byte) []byte {
	t.Helper()
	block, _ := pem.Decode(armored)
	if block == nil {
		t.Fatal("fixture is not PEM")
	}
	block.Bytes = block.Bytes[:len(block.Bytes)/2]
	return pem.EncodeToMemory(block)
}

// testSigner produces ssh-keygen -Y sign compatible signatures in-process,
// so download tests can sign whatever release they serve.
type testSigner struct {
	pub ssh.PublicKey
	ssh ssh.Signer
}

func newTestSigner(t *testing.T) testSigner {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return testSigner{pub: signer.PublicKey(), ssh: signer}
}

// useAsReleaseKey makes s the key DownloadRelease trusts for this test.
func (s testSigner) useAsReleaseKey(t *testing.T) {
	t.Helper()
	old := releaseSigningKey
	releaseSigningKey = s.pub
	t.Cleanup(func() { releaseSigningKey = old })
}

func (s testSigner) sign(t *testing.T, tag string, checksums []byte, namespace, hashAlg string) []byte {
	t.Helper()
	digest := sha512.Sum512(signedMessage(tag, checksums))
	signed := append([]byte(sshsigMagic), ssh.Marshal(struct {
		Namespace string
		Reserved  []byte
		HashAlg   string
		Digest    []byte
	}{namespace, nil, hashAlg, digest[:]})...)
	sig, err := s.ssh.Sign(rand.Reader, signed)
	if err != nil {
		t.Fatal(err)
	}
	blob := append([]byte(sshsigMagic), ssh.Marshal(sshsigBlob{
		Version:   1,
		PublicKey: s.pub.Marshal(),
		Namespace: namespace,
		HashAlg:   hashAlg,
		Signature: ssh.Marshal(sig),
	})...)
	return pem.EncodeToMemory(&pem.Block{Type: "SSH SIGNATURE", Bytes: blob})
}
