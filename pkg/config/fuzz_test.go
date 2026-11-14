package config_test

import (
	"maps"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

func FuzzDecodeRoundTrip(f *testing.F) {
	// The checked-in corpus includes the annotated example without comments
	// and the v2, v3 and v4 migration fixtures.
	f.Add([]byte{})
	f.Add([]byte(`{}`))
	home := f.TempDir()
	f.Setenv("HOME", home)
	f.Setenv("USERPROFILE", home)
	f.Fuzz(func(t *testing.T, data []byte) {
		before, err := config.Parse(data)
		if err != nil {
			return
		}
		if before.Version != config.CurrentVersion {
			t.Fatalf("parsed version = %d, want %d", before.Version, config.CurrentVersion)
		}
		want := configIntents(before)
		encoded, err := config.Marshal(before)
		if err != nil {
			t.Fatalf("Marshal accepted config: %v", err)
		}
		after, err := config.Parse(encoded)
		if err != nil {
			t.Fatalf("Parse marshalled config: %v", err)
		}
		if after.Version != config.CurrentVersion {
			t.Fatalf("round-trip version = %d, want %d", after.Version, config.CurrentVersion)
		}
		if got := configIntents(after); !maps.Equal(got, want) {
			t.Fatalf("round-trip changed repository intent:\nbefore: %v\nafter: %v", want, got)
		}
	})
}

type configIntent struct {
	source, repo, path, credential, workspace string
	container                                 bool
}

func configIntents(cfg *config.Config) map[configIntent]bool {
	intents := make(map[configIntent]bool)
	for sourceKey, source := range cfg.Sources {
		account := cfg.Accounts[source.Account]
		for repoKey, repo := range source.Repos {
			intents[configIntent{
				source: sourceKey, repo: repoKey,
				path:       status.RepoPath(cfg, sourceKey, repoKey, repo),
				credential: repo.EffectiveCredentialType(&account),
				container:  repo.Container, workspace: repo.Workspace,
			}] = true
		}
	}
	return intents
}
