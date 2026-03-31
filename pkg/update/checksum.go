package update

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
)

// VerifyChecksum verifies that the SHA256 of filePath matches the expected
// hash in the checksums file for the given artifact name.
func VerifyChecksum(filePath, checksumsFile, artifactName string) error {
	expected, err := findExpectedHash(checksumsFile, artifactName)
	if err != nil {
		return err
	}

	actual, err := fileHash(filePath)
	if err != nil {
		return fmt.Errorf("computing hash: %w", err)
	}

	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}

func findExpectedHash(checksumsFile, artifactName string) (string, error) {
	data, err := os.ReadFile(checksumsFile)
	if err != nil {
		return "", fmt.Errorf("reading checksums file: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "hash  filename" or "hash filename"
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == artifactName {
			return parts[0], nil
		}
	}

	return "", fmt.Errorf("artifact %s not found in checksums file", artifactName)
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
