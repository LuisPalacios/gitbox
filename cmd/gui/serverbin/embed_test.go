//go:build serverbin

package serverbin

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"testing"
)

// Every embedded target decompresses to bytes whose SHA-256 is its pin, and
// the package carries the identity of the build they come from.
func TestEmbeddedHashesMatch(t *testing.T) {
	if !regexp.MustCompile(`^[0-9a-f]{40}(-dirty)?$`).MatchString(Commit) {
		t.Errorf("Commit = %q, want the full SHA", Commit)
	}
	if Version == "" {
		t.Error("Version is empty")
	}

	targets := Targets()
	if len(targets) == 0 {
		t.Fatal("a serverbin build has no targets")
	}
	embedded, err := fs.Glob(files, "*.gz")
	if err != nil {
		t.Fatal(err)
	}
	if len(embedded) != len(targets) {
		t.Errorf("%d files are embedded for %d targets: %v", len(embedded), len(targets), embedded)
	}

	seen := make(map[string]bool)
	for _, target := range targets {
		if !slices.Contains(Supported, target.Name()) || seen[target.Name()] {
			t.Errorf("%s is unknown or listed twice", target.Name())
		}
		seen[target.Name()] = true
		if found, ok := Lookup(target.OS, target.Arch); !ok || found != target {
			t.Errorf("Lookup(%s) = %+v, %v", target.Name(), found, ok)
		}

		// Checked here on its own, not through Binary.
		raw, err := files.ReadFile(FileName(target.OS, target.Arch))
		if err != nil {
			t.Errorf("%s: %v", target.Name(), err)
			continue
		}
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Errorf("%s: %v", target.Name(), err)
			continue
		}
		binary, err := io.ReadAll(zr)
		if err != nil {
			t.Errorf("%s: %v", target.Name(), err)
			continue
		}
		sum := sha256.Sum256(binary)
		if int64(len(binary)) != target.Size || hex.EncodeToString(sum[:]) != target.SHA256 {
			t.Errorf("%s: %d bytes with SHA-256 %x; pinned %d bytes with %s", target.Name(), len(binary), sum, target.Size, target.SHA256)
		}

		got, err := Binary(target)
		if err != nil || !bytes.Equal(got, binary) {
			t.Errorf("Binary(%s) = %d bytes, %v", target.Name(), len(got), err)
		}
		// The pin Binary checks is the table's, whatever the caller passes.
		forged := target
		forged.SHA256, forged.Size = "00", 1
		if got, err := Binary(forged); err != nil || !bytes.Equal(got, binary) {
			t.Errorf("Binary with a forged pin = %d bytes, %v; want the binary by the table's pin", len(got), err)
		}
	}
	if _, ok := Lookup("plan9", "amd64"); ok {
		t.Error("Lookup found a target nobody built")
	}
}
