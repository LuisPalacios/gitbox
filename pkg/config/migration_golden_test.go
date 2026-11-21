package config_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

func TestLegacyConfigurationsMigrateToV5Golden(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	read := func(t *testing.T, name string) *config.Config {
		t.Helper()
		cfg, err := config.Load(filepath.Join("testdata", "migrations", name))
		if err != nil {
			t.Fatalf("Load %s: %v", name, err)
		}
		return cfg
	}
	want := read(t, "v5-golden.json")
	wantPath := filepath.Join(home, "other", "projects", "org", "custom-repo")
	for version := 2; version < config.CurrentVersion; version++ {
		name := fmt.Sprintf("v%d.json", version)
		t.Run(name, func(t *testing.T) {
			migrated := read(t, name)
			encoded, err := config.Marshal(migrated)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			actual, err := config.Parse(encoded)
			if err != nil {
				t.Fatalf("Parse saved migration: %v", err)
			}
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("saved migration differs from v5 golden:\n%s", encoded)
			}
			for _, cfg := range []*config.Config{migrated, actual} {
				if got := status.RepoPath(cfg, "sample", "org/repo", cfg.Sources["sample"].Repos["org/repo"]); got != wantPath {
					t.Errorf("clone path = %q, want %q", got, wantPath)
				}
			}
		})
	}
}
