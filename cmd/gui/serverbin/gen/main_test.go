package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testVersion = "v9.9.9-3-gabcdef0-dev"
	testCommit  = "0123456789abcdef0123456789abcdef01234567"
)

func unzip(t *testing.T, data []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCompress(t *testing.T) {
	data := bytes.Repeat([]byte("the same bytes "), 10000)
	packed, err := compress(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unzip(t, packed), data) {
		t.Error("the round trip changed the bytes")
	}
	if len(packed) >= len(data)/10 {
		t.Errorf("%d bytes compressed to %d: not the highest level", len(data), len(packed))
	}
	again, _ := compress(data)
	if !bytes.Equal(packed, again) {
		t.Error("the same bytes compressed to two different files")
	}
	// No name and no time in the header: nothing of the machine or the
	// moment ends up in the file.
	zr, _ := gzip.NewReader(bytes.NewReader(packed))
	if zr.Name != "" || !zr.ModTime.IsZero() {
		t.Errorf("header carries name %q and time %v", zr.Name, zr.ModTime)
	}
}

func TestParseTargets(t *testing.T) {
	got, err := parseTargets("windows/arm64,linux/amd64,darwin/arm64")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, target := range got {
		names = append(names, target.Name())
	}
	if want := "linux/amd64 darwin/arm64 windows/arm64"; strings.Join(names, " ") != want {
		t.Errorf("targets = %v, want %s (the order package serverbin reports)", names, want)
	}
	for _, bad := range []string{"", "plan9/amd64", "linux/amd64,linux/amd64", "linux/amd64,", "linux"} {
		if _, err := parseTargets(bad); err == nil {
			t.Errorf("parseTargets(%q) accepted the list", bad)
		}
	}
}

// run writes one compressed file per target and a Go file that pins the
// uncompressed binary, and removes what an earlier run left.
func TestRun(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	linux := bytes.Repeat([]byte("linux server "), 500)
	windows := bytes.Repeat([]byte("windows server "), 700)
	for name, data := range map[string][]byte{
		"gitbox-server-linux-arm64":       linux,
		"gitbox-server-windows-amd64.exe": windows,
	} {
		if err := os.WriteFile(filepath.Join(in, name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stale := filepath.Join(out, "gitbox-server-darwin-arm64.gz")
	keep := filepath.Join(out, "embed.go")
	for _, path := range []string{stale, keep, filepath.Join(out, generatedFile)} {
		if err := os.WriteFile(path, []byte("from an earlier run"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := run(in, out, testVersion, testCommit, "windows/amd64,linux/arm64"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the binary of an earlier run is still there and would be embedded")
	}
	if data, _ := os.ReadFile(keep); string(data) != "from an earlier run" {
		t.Error("run touched a file that is not its own")
	}
	for name, want := range map[string][]byte{
		"gitbox-server-linux-arm64.gz":       linux,
		"gitbox-server-windows-amd64.exe.gz": windows,
	} {
		packed, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(unzip(t, packed), want) {
			t.Errorf("%s does not hold its binary", name)
		}
	}

	source, err := os.ReadFile(filepath.Join(out, generatedFile))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.HasPrefix(text, "//go:build serverbin\n") {
		t.Errorf("the generated file must only build with the serverbin tag:\n%s", text)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), generatedFile, source, parser.AllErrors); err != nil {
		t.Errorf("the generated file is not Go: %v", err)
	}
	sum := func(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }
	for _, want := range []string{
		`Version = "` + testVersion + `"`,
		`Commit  = "` + testCommit + `"`,
		`{OS: "linux", Arch: "arm64", Size: 6500, SHA256: "` + sum(linux) + `"}`,
		`{OS: "windows", Arch: "amd64", Size: 10500, SHA256: "` + sum(windows) + `"}`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the generated file lacks %s:\n%s", want, text)
		}
	}
	if strings.Index(text, `"linux"`) > strings.Index(text, `"windows"`) {
		t.Error("targets are not in the order package serverbin reports")
	}
}

func TestRunRefusesBadInput(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "gitbox-server-linux-amd64"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(in, "gitbox-server-linux-arm64"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][5]string{
		"a short commit":         {in, out, testVersion, "0123abc", "linux/amd64"},
		"a commit with a suffix": {in, out, testVersion, testCommit + "-dirty", "linux/amd64"},
		"a version with a quote": {in, out, `v1"; panic("x`, testCommit, "linux/amd64"},
		"no version":             {in, out, "", testCommit, "linux/amd64"},
		"an unknown target":      {in, out, testVersion, testCommit, "plan9/amd64"},
		"a binary never built":   {in, out, testVersion, testCommit, "darwin/arm64"},
		"an empty binary":        {in, out, testVersion, testCommit, "linux/arm64"},
		"no output folder":       {in, "", testVersion, testCommit, "linux/amd64"},
	} {
		if err := run(args[0], args[1], args[2], args[3], args[4]); err == nil {
			t.Errorf("%s: run accepted it", name)
		}
	}
}
