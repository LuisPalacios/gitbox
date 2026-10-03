package ops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
)

// testFixture is test-gitbox.json: a regular gitbox config whose accounts
// may carry a "_test" section with a real token for integration tests.
type testFixture struct {
	Config *config.Config
	Tokens map[string]string // account key → token
}

// requireIntegration loads test-gitbox.json from the repo root, exports its
// tokens as GITBOX_TOKEN_<KEY> env vars, and points git's SSH at the
// fixture's isolated ssh folder. Skips in -short mode; fails when the file
// is missing so a full run never passes silently without coverage.
func requireIntegration(t *testing.T) testFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	path := filepath.Join(repoRoot(t), "test-gitbox.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Fatal("test-gitbox.json not found. Integration tests need this file to run.\n" +
			"  Create it:  cp json/test-gitbox.json.example test-gitbox.json\n" +
			"  Or skip:    go test -short ./...")
	}
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatalf("parsing test fixture config: %v", err)
	}

	var raw struct {
		Accounts map[string]struct {
			Test struct {
				Token string `json:"token"`
			} `json:"_test"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing account _test sections: %v", err)
	}
	f := testFixture{Config: cfg, Tokens: map[string]string{}}
	for key, a := range raw.Accounts {
		if a.Test.Token != "" {
			f.Tokens[key] = a.Test.Token
			t.Setenv(credential.EnvVarName(key), a.Test.Token)
		}
	}

	rejectRealPaths(t, cfg)
	if cfg.Global.CredentialSSH != nil {
		sshConfig := filepath.ToSlash(filepath.Join(config.ExpandTilde(cfg.Global.CredentialSSH.SSHFolder), "config"))
		t.Setenv("GIT_SSH_COMMAND", fmt.Sprintf("ssh -F %s", sshConfig))
	}
	return f
}

// rejectRealPaths refuses fixtures that point at the real ~/.ssh or the real
// gitbox config directory. Integration tests must run on isolated paths.
func rejectRealPaths(t *testing.T, cfg *config.Config) {
	t.Helper()
	home, _ := os.UserHomeDir()
	if cfg.Global.CredentialSSH != nil {
		ssh := filepath.Clean(config.ExpandTilde(cfg.Global.CredentialSSH.SSHFolder))
		if ssh == filepath.Clean(filepath.Join(home, ".ssh")) {
			t.Fatal("test-gitbox.json has ssh_folder pointing at ~/.ssh — use an isolated path like ~/.gitbox-test/ssh")
		}
	}
	if cfg.Global.Folder != "" {
		folder := filepath.Clean(config.ExpandTilde(cfg.Global.Folder))
		if folder == filepath.Clean(filepath.Join(config.ConfigRoot(), "gitbox")) {
			t.Fatal("test-gitbox.json has global.folder pointing at ~/.config/gitbox — use an isolated path like ~/.gitbox-test/git")
		}
	}
}

// firstAccountWithRepos picks, deterministically, the first source that has
// repos and whose account has a token in the fixture.
func (f testFixture) firstAccountWithRepos() (accountKey, sourceKey, repoKey string, ok bool) {
	srcKeys := make([]string, 0, len(f.Config.Sources))
	for k := range f.Config.Sources {
		srcKeys = append(srcKeys, k)
	}
	sort.Strings(srcKeys)
	for _, sk := range srcKeys {
		src := f.Config.Sources[sk]
		if len(src.Repos) == 0 || f.Tokens[src.Account] == "" {
			continue
		}
		repoKeys := make([]string, 0, len(src.Repos))
		for k := range src.Repos {
			repoKeys = append(repoKeys, k)
		}
		sort.Strings(repoKeys)
		return src.Account, sk, repoKeys[0], true
	}
	return "", "", "", false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}
