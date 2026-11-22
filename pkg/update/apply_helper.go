package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// ApplyUpdateFlag dispatches the Windows updater before the GUI starts.
const ApplyUpdateFlag = "--apply-update"

// RestartAfterUpdateFlag dispatches the Windows restart waiter before the GUI starts.
const RestartAfterUpdateFlag = "--restart-after-update"

const (
	windowsExecutableName = "GitboxApp.exe"
	updateMetadataLimit   = 64 << 10
	updateArchiveLimit    = 256 << 20
	updateExecutableLimit = 256 << 20
)

func readUpdateBytes(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("update file is too large")
	}
	return data, nil
}

// verifiedWindowsExecutable verifies the same archive bytes it opens. The
// signed Windows release zip contains only the executable shipped by CI.
func verifiedWindowsExecutable(current, tag, artifact string, checksums, signature, archive []byte) ([]byte, error) {
	if newer, err := IsNewer(current, tag); err != nil || !newer {
		return nil, errors.New("the update is not newer than the installed version")
	}
	if err := verifyReleaseSignature(tag, checksums, signature, releaseSigningKey); err != nil {
		return nil, errors.New("the update signature could not be verified")
	}
	expected, err := expectedArtifactHash(checksums, artifact)
	if err != nil || expected != fmt.Sprintf("%x", sha256.Sum256(archive)) {
		return nil, errors.New("the update archive checksum could not be verified")
	}

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil || len(reader.File) != 1 {
		return nil, errors.New("the signed update must contain exactly one executable")
	}
	entry := reader.File[0]
	if entry.Name != windowsExecutableName || !entry.Mode().IsRegular() {
		return nil, errors.New("the signed update contains an unexpected entry")
	}
	if entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > updateExecutableLimit {
		return nil, errors.New("the signed executable has an invalid size")
	}
	file, err := entry.Open()
	if err != nil {
		return nil, errors.New("the signed executable could not be opened")
	}
	defer file.Close()
	data, err := readUpdateBytes(file, int64(entry.UncompressedSize64))
	if err != nil || uint64(len(data)) != entry.UncompressedSize64 {
		return nil, errors.New("the signed executable could not be read completely")
	}
	return data, nil
}
