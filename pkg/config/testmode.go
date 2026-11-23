package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetupTestMode reads test-gitbox.json from the repo root, creates a throwaway
// config in a temp directory with global.roots overridden, and validates that
// ssh_folder does not point at ~/.ssh. Returns the temp config path and a
// cleanup function that removes the temp directory.
//
// This entry point keeps the original contract for library callers.
func SetupTestMode() (cfgPath string, cleanup func(), err error) {
	cfgPath, _, cleanup, err = PrepareTestFixture()
	return cfgPath, cleanup, err
}

// PrepareTestFixture also reports whether the fixture requests first launch.
// The saved config is valid and uses the temporary root in both cases.
func PrepareTestFixture() (cfgPath string, firstRun bool, cleanup func(), err error) {
	// Find test-gitbox.json by walking up from cwd.
	fixturePath, err := findFixture()
	if err != nil {
		return "", false, nil, err
	}

	// Parse the fixture.
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		return "", false, nil, fmt.Errorf("reading %s: %w", fixturePath, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return "", false, nil, fmt.Errorf("parsing %s: %w", fixturePath, err)
	}

	// Create temp directory.
	tmpDir, err := os.MkdirTemp("", "gitbox-test-*")
	if err != nil {
		return "", false, nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanupFn := func() { os.RemoveAll(tmpDir) }

	// Safety: validate ssh_folder is not ~/.ssh.
	home, _ := os.UserHomeDir()
	if cfg.Global.CredentialSSH != nil {
		sshFolder := ExpandTilde(cfg.Global.CredentialSSH.SSHFolder)
		realSSH := filepath.Join(home, ".ssh")
		if filepath.Clean(sshFolder) == filepath.Clean(realSSH) {
			cleanupFn()
			return "", false, nil, fmt.Errorf("test-gitbox.json has ssh_folder pointing at ~/.ssh — use an isolated path like ~/.gitbox-test/ssh")
		}
	}

	// Safety: validate no root is ~/.config/gitbox.
	gitboxConfig := filepath.Join(ConfigRoot(), V2ConfigDir)
	for _, root := range cfg.Global.Roots {
		if SameRoot(root, gitboxConfig) {
			cleanupFn()
			return "", false, nil, fmt.Errorf("test-gitbox.json has a root pointing at ~/.config/gitbox — use an isolated path like ~/.gitbox-test/git")
		}
	}

	// Override the roots with one temp dir for clone isolation; every
	// source and repo then resolves under it.
	gitDir := filepath.Join(tmpDir, "git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		cleanupFn()
		return "", false, nil, fmt.Errorf("creating git dir: %w", err)
	}
	var fixture struct {
		Test struct {
			FirstRun bool `json:"firstRun"`
		} `json:"_test"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		cleanupFn()
		return "", false, nil, err
	}
	if fixture.Test.FirstRun && (len(cfg.Accounts) != 0 || len(cfg.Sources) != 0 || len(cfg.Mirrors) != 0) {
		cleanupFn()
		return "", false, nil, fmt.Errorf("a first-run fixture must have no accounts, sources or mirrors")
	}
	cfg.Global.Roots = []string{gitDir}
	for sk, src := range cfg.Sources {
		src.Root = ""
		for rk, repo := range src.Repos {
			repo.Root = ""
			src.Repos[rk] = repo
		}
		cfg.Sources[sk] = src
	}

	// Write modified config to temp.
	cfgDir := filepath.Join(tmpDir, "gitbox")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		cleanupFn()
		return "", false, nil, fmt.Errorf("creating config dir: %w", err)
	}
	tempCfgPath := filepath.Join(cfgDir, V2ConfigFile)
	if err := Save(cfg, tempCfgPath); err != nil {
		cleanupFn()
		return "", false, nil, fmt.Errorf("writing temp config: %w", err)
	}

	// Set GIT_SSH_COMMAND so git clone/fetch/pull use the isolated SSH config
	// instead of ~/.ssh/config. Without this, SSH-based accounts fail in test mode.
	if cfg.Global.CredentialSSH != nil && cfg.Global.CredentialSSH.SSHFolder != "" {
		sshFolder := ExpandTilde(cfg.Global.CredentialSSH.SSHFolder)
		sshConfig := filepath.ToSlash(filepath.Join(sshFolder, "config"))
		os.Setenv("GIT_SSH_COMMAND", fmt.Sprintf("ssh -F %s", sshConfig))
	}

	// Extract _test.token from each account and set as env vars.
	// This mirrors what the test harness does (credential.EnvVarName convention).
	if err := injectTestTokens(data); err != nil {
		cleanupFn()
		return "", false, nil, fmt.Errorf("injecting test tokens: %w", err)
	}

	return tempCfgPath, fixture.Test.FirstRun, cleanupFn, nil
}

// injectTestTokens reads the raw JSON to extract _test.token from each account
// and sets GITBOX_TOKEN_<KEY> env vars so credential.ResolveToken() finds them.
func injectTestTokens(data []byte) error {
	var raw struct {
		Accounts map[string]struct {
			Test struct {
				Token string `json:"token"`
			} `json:"_test"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing _test sections: %w", err)
	}
	for accountKey, acct := range raw.Accounts {
		if acct.Test.Token != "" {
			envName := testTokenEnvName(accountKey)
			os.Setenv(envName, acct.Test.Token)
		}
	}
	return nil
}

// testTokenEnvName returns GITBOX_TOKEN_<KEY> (same convention as credential.EnvVarName).
func testTokenEnvName(accountKey string) string {
	key := strings.ToUpper(accountKey)
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, ".", "_")
	return "GITBOX_TOKEN_" + key
}

// findFixture walks up from cwd looking for test-gitbox.json.
func findFixture() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting cwd: %w", err)
	}
	for {
		path := filepath.Join(dir, "test-gitbox.json")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("test-gitbox.json not found (walk up from cwd to root).\n" +
				"  Create it: cp json/test-gitbox.json.example test-gitbox.json\n" +
				"  See docs/testing.md for setup instructions")
		}
		dir = parent
	}
}
