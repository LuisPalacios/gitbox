package terminals

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDiscoverWSLDistros_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses a Unix shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wsl.exe"), []byte("#!/bin/sh\nexec /bin/sleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if got := discoverWSLDistros(ctx); len(got) != 0 {
		t.Fatalf("timed out discovery returned distributions: %v", got)
	}
	if ctx.Err() == nil {
		t.Fatal("fake WSL command did not reach its timeout")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timed out command remained blocked for %s", elapsed)
	}
}

func TestLookPathWithBrewPATH_PreservesEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses Unix executable permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox-test-shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := lookPathWithBrewPATH("gitbox-test-shell")
	if err != nil || got != path {
		t.Fatalf("lookup = %q, %v; want %q", got, err, path)
	}
	if os.Getenv("PATH") != dir {
		t.Fatal("lookup changed process PATH")
	}
}
