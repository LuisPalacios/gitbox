// Package serverbin holds the gitbox-server binaries that GitboxApp carries.
//
// Remote mode (#233) installs gitbox-server on the other host from inside
// GitboxApp, with no download. scripts/build-server.sh compiles the server
// for each target from the same commit as the app, and the gen command
// compresses each binary and writes zz_generated.go, which pins the SHA-256
// of the uncompressed binary. The release signature covers GitboxApp, and
// with it these pins.
//
// The binaries are only there in a build with the serverbin tag, made after
// build-server.sh ran. Every other build compiles this package with no
// targets and needs none of the generated files.
package serverbin

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
)

// Target is one embedded gitbox-server binary.
type Target struct {
	OS   string
	Arch string
	// Size and SHA256 describe the binary as it is installed: uncompressed.
	Size   int64
	SHA256 string
}

// Name is the target the way Go spells it: "linux/arm64".
func (t Target) Name() string { return t.OS + "/" + t.Arch }

// Supported lists every target a release embeds, in the order they are
// reported. Remote mode runs on Linux and on Macs with Apple silicon (#350);
// Windows hosts come later (#246).
var Supported = []string{"linux/amd64", "linux/arm64", "darwin/arm64"}

// FileName is the name of a target's compressed binary inside this package.
func FileName(goos, goarch string) string {
	name := "gitbox-server-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name + ".gz"
}

// Targets returns the embedded targets. It is empty in a build without the
// serverbin tag.
func Targets() []Target { return slices.Clone(table) }

// Lookup returns the embedded target for an operating system and
// architecture.
func Lookup(goos, goarch string) (Target, bool) {
	for _, t := range table {
		if t.OS == goos && t.Arch == goarch {
			return t, true
		}
	}
	return Target{}, false
}

// ErrCorrupt reports an embedded binary that does not match its pin.
var ErrCorrupt = errors.New("serverbin: embedded binary does not match its pin")

// Binary returns the gitbox-server binary of a target, uncompressed. It
// returns nothing unless the bytes have the pinned size and SHA-256.
func Binary(t Target) ([]byte, error) {
	pinned, ok := Lookup(t.OS, t.Arch)
	if !ok {
		return nil, fmt.Errorf("serverbin: no embedded binary for %s", t.Name())
	}
	// The pin comes from the table, never from the caller.
	return read(files, pinned)
}

func read(fsys fs.FS, t Target) ([]byte, error) {
	f, err := fsys.Open(FileName(t.OS, t.Arch))
	if err != nil {
		return nil, fmt.Errorf("serverbin: %s: %w", t.Name(), err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCorrupt, t.Name(), err)
	}
	// One byte past the pinned size: a longer stream then fails the hash,
	// and a corrupt one cannot grow without bound.
	var out bytes.Buffer
	if _, err := io.Copy(&out, io.LimitReader(zr, t.Size+1)); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCorrupt, t.Name(), err)
	}
	if sum := sha256.Sum256(out.Bytes()); hex.EncodeToString(sum[:]) != t.SHA256 {
		return nil, fmt.Errorf("%w: %s: SHA-256 differs", ErrCorrupt, t.Name())
	}
	return out.Bytes(), nil
}
