package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type configFileAfterEffect struct {
	stagedFile
	after string
}

func (f configFileAfterEffect) Write(data []byte) (int, error) {
	n, err := f.stagedFile.Write(data)
	if err == nil && f.after == "write" {
		err = errSaveInjected
	}
	return n, err
}

func (f configFileAfterEffect) Sync() error {
	err := f.stagedFile.Sync()
	if err == nil && f.after == "sync" {
		err = errSaveInjected
	}
	return err
}

func TestSave_FailureAfterSideEffectKeepsCompleteConfig(t *testing.T) {
	for _, after := range []string{"write", "sync", "rename"} {
		t.Run(after, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "gitbox.json")
			old, next := saveTestConfig(), saveTestConfig()
			old.Global.Roots = []string{filepath.Join(dir, "old")}
			next.Global.Roots = []string{filepath.Join(dir, "next")}
			original, err := Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			replacement, err := Marshal(next)
			if err != nil {
				t.Fatal(err)
			}
			writeSaveFixture(t, path, original)

			files := fixedConfigFiles()
			createTemp := files.createTemp
			files.createTemp = func(dir, pattern string) (stagedFile, error) {
				file, err := createTemp(dir, pattern)
				if err != nil {
					return nil, err
				}
				return configFileAfterEffect{file, after}, nil
			}
			if after == "rename" {
				files.rename = func(from, to string) error {
					if err := os.Rename(from, to); err != nil {
						return err
					}
					// Simulate a write reporting failure after publication.
					return errSaveInjected
				}
			}
			if err := saveWithFiles(next, path, files); !errors.Is(err, errSaveInjected) {
				t.Fatalf("save error = %v, want injected error", err)
			}

			want := original
			wantEntries := 1
			if after == "rename" {
				want = replacement
				wantEntries = 2
			}
			assertSaveBytes(t, path, want)
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != wantEntries {
				t.Fatalf("directory entries = %v, want %d published configs", entries, wantEntries)
			}
			for _, entry := range entries {
				if entry.Name() == filepath.Base(path) {
					continue
				}
				assertSaveBytes(t, filepath.Join(dir, entry.Name()), original)
			}
		})
	}
}
