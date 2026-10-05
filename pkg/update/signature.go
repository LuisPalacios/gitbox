package update

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	_ "embed"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// signatureAsset is the release asset holding the maintainer's SSH
// signature over the release's checksums file.
const signatureAsset = "checksums.sha256.sig"

// signatureNamespace is the ssh-keygen -Y namespace releases are signed
// under, so a signature made with the same key for anything else (commits,
// files) never verifies as a release signature.
const signatureNamespace = "gitbox-release"

// releaseSigningKeyFile is the public half of the release signing key. The
// private half lives in the maintainer's SSH agent and never reaches CI.
// scripts/sign-release.sh signs with this same file, and allowed_signers at
// the repo root must list the same key (a test enforces it).
//
//go:embed release-signing-key.pub
var releaseSigningKeyFile []byte

// releaseSigningKey is the key every release signature must come from.
// Tests swap it for a test key.
var releaseSigningKey = mustParsePublicKey(releaseSigningKeyFile)

func mustParsePublicKey(line []byte) ssh.PublicKey {
	key, _, _, _, err := ssh.ParseAuthorizedKey(line)
	if err != nil {
		panic(fmt.Sprintf("update: invalid release signing key: %v", err))
	}
	return key
}

// signedMessage is what the maintainer signs for a release: a header that
// binds the signature to one tag, followed by the checksums file verbatim.
func signedMessage(tag string, checksums []byte) []byte {
	return append([]byte("gitbox "+tag+"\n"), checksums...)
}

// sshsigBlob is the body of an armored "SSH SIGNATURE" after the magic
// preamble (PROTOCOL.sshsig in the OpenSSH sources).
type sshsigBlob struct {
	Version   uint32
	PublicKey []byte
	Namespace string
	Reserved  []byte
	HashAlg   string
	Signature []byte
}

const sshsigMagic = "SSHSIG"

// verifyReleaseSignature checks that armoredSig is an ssh-keygen -Y sign
// signature by key, in the release namespace, over the release message for
// tag and checksums.
func verifyReleaseSignature(tag string, checksums, armoredSig []byte, key ssh.PublicKey) error {
	block, _ := pem.Decode(armoredSig)
	if block == nil || block.Type != "SSH SIGNATURE" {
		return fmt.Errorf("not an SSH signature")
	}
	raw, ok := bytes.CutPrefix(block.Bytes, []byte(sshsigMagic))
	if !ok {
		return fmt.Errorf("not an SSH signature")
	}
	var sig sshsigBlob
	if err := ssh.Unmarshal(raw, &sig); err != nil {
		return fmt.Errorf("malformed SSH signature: %w", err)
	}
	if sig.Version != 1 {
		return fmt.Errorf("unsupported SSH signature version %d", sig.Version)
	}
	if sig.Namespace != signatureNamespace {
		return fmt.Errorf("signature namespace is %q, want %q", sig.Namespace, signatureNamespace)
	}
	if !bytes.Equal(sig.PublicKey, key.Marshal()) {
		return fmt.Errorf("signed by an unknown key")
	}

	var digest []byte
	switch sig.HashAlg {
	case "sha512":
		h := sha512.Sum512(signedMessage(tag, checksums))
		digest = h[:]
	case "sha256":
		h := sha256.Sum256(signedMessage(tag, checksums))
		digest = h[:]
	default:
		return fmt.Errorf("unsupported signature hash %q", sig.HashAlg)
	}

	var inner ssh.Signature
	if err := ssh.Unmarshal(sig.Signature, &inner); err != nil {
		return fmt.Errorf("malformed SSH signature: %w", err)
	}
	signed := append([]byte(sshsigMagic), ssh.Marshal(struct {
		Namespace string
		Reserved  []byte
		HashAlg   string
		Digest    []byte
	}{sig.Namespace, sig.Reserved, sig.HashAlg, digest})...)
	if err := key.Verify(signed, &inner); err != nil {
		return fmt.Errorf("signature does not match release %s", tag)
	}
	return nil
}
