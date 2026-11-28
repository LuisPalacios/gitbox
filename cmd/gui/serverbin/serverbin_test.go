package serverbin

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func gz(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pin(goos, goarch string, data []byte) Target {
	sum := sha256.Sum256(data)
	return Target{OS: goos, Arch: goarch, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

// read returns the binary only when it has the pinned size and hash.
func TestReadVerifiesThePin(t *testing.T) {
	binary := bytes.Repeat([]byte("gitbox-server "), 4096)
	good := pin("linux", "arm64", binary)
	fsys := fstest.MapFS{FileName("linux", "arm64"): {Data: gz(t, binary)}}

	got, err := read(fsys, good)
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("read = %d bytes, %v; want the %d bytes of the binary", len(got), err, len(binary))
	}

	tampered := slices.Clone(binary)
	tampered[len(tampered)/2] ^= 1
	for name, tt := range map[string]struct {
		fsys fstest.MapFS
		pin  Target
	}{
		"one bit changed, same size": {fstest.MapFS{FileName("linux", "arm64"): {Data: gz(t, tampered)}}, good},
		"shorter than pinned":        {fstest.MapFS{FileName("linux", "arm64"): {Data: gz(t, binary[:len(binary)-1])}}, good},
		"longer than pinned":         {fstest.MapFS{FileName("linux", "arm64"): {Data: gz(t, append(slices.Clone(binary), 'x'))}}, good},
		"not gzip":                   {fstest.MapFS{FileName("linux", "arm64"): {Data: binary}}, good},
		"truncated stream":           {fstest.MapFS{FileName("linux", "arm64"): {Data: gz(t, binary)[:64]}}, good},
		"pin of another binary":      {fsys, pin("linux", "arm64", tampered)},
	} {
		got, err := read(tt.fsys, tt.pin)
		if !errors.Is(err, ErrCorrupt) || got != nil {
			t.Errorf("%s: read = %d bytes, %v; want ErrCorrupt and no bytes", name, len(got), err)
		}
	}

	if got, err := read(fstest.MapFS{}, good); err == nil || got != nil {
		t.Errorf("a missing file: read = %d bytes, %v", len(got), err)
	}
}

func TestFileNameAndSupported(t *testing.T) {
	if got := FileName("linux", "arm64"); got != "gitbox-server-linux-arm64.gz" {
		t.Errorf("FileName(linux, arm64) = %q", got)
	}
	if got := FileName("windows", "amd64"); got != "gitbox-server-windows-amd64.exe.gz" {
		t.Errorf("FileName(windows, amd64) = %q", got)
	}
	want := []string{"linux/amd64", "linux/arm64", "darwin/arm64"}
	if !slices.Equal(Supported, want) {
		t.Errorf("Supported = %v, want %v", Supported, want)
	}
	if got := (Target{OS: "darwin", Arch: "arm64"}).Name(); got != "darwin/arm64" {
		t.Errorf("Name = %q", got)
	}
}

// build-server.sh keeps its own copy of the list, so that a shell script can
// check a target before Go runs. It must be the same list.
func TestBuildServerScriptListsSupported(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "build-server.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^ALL_TARGETS="([^"]*)"$`).FindSubmatch(data)
	if m == nil {
		t.Fatal("build-server.sh has no ALL_TARGETS line")
	}
	if got := strings.Split(string(m[1]), ","); !slices.Equal(got, Supported) {
		t.Errorf("build-server.sh ALL_TARGETS = %v, want Supported %v", got, Supported)
	}
}
