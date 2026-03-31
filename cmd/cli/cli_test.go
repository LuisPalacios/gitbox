package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
)

// ---------------------------------------------------------------------------
// Test environment for CLI subprocess tests
// ---------------------------------------------------------------------------

type cliTestEnv struct {
	CfgPath   string // path to gitbox.json
	GitFolder string // throwaway clone folder
	TmpDir    string // root temp dir
	BinPath   string // compiled gitbox binary
}

type cliResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Binary build cache: compile once per test run.
var (
	cachedBinPath string
	buildOnce     sync.Once
	buildErr      error
)

func ensureBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		// Find repo root.
		dir, _ := os.Getwd()
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				break
			}
			dir = filepath.Dir(dir)
		}
		// Use os.MkdirTemp instead of t.TempDir so the binary outlives any single test.
		tmp, err := os.MkdirTemp("", "gitbox-test-bin-*")
		if err != nil {
			buildErr = err
			return
		}
		binName := "gitbox-test"
		if os.PathSeparator == '\\' {
			binName = "gitbox-test.exe"
		}
		cachedBinPath = filepath.Join(tmp, binName)
		cmd := exec.Command("go", "build", "-o", cachedBinPath, "./cmd/cli")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = err
			t.Logf("build output: %s", out)
		}
	})
	if buildErr != nil {
		t.Fatalf("building test binary: %v", buildErr)
	}
	return cachedBinPath
}

func setupCLIEnv(t *testing.T) cliTestEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfgDir := filepath.Join(tmp, "gitbox")
	os.MkdirAll(cfgDir, 0o755)
	gitDir := filepath.Join(tmp, "git")
	os.MkdirAll(gitDir, 0o755)

	return cliTestEnv{
		CfgPath:   filepath.Join(cfgDir, "gitbox.json"),
		GitFolder: gitDir,
		TmpDir:    tmp,
		BinPath:   ensureBinary(t),
	}
}

func setupCLIEnvWithConfig(t *testing.T, cfg *config.Config) cliTestEnv {
	t.Helper()
	env := setupCLIEnv(t)
	if err := config.Save(cfg, env.CfgPath); err != nil {
		t.Fatalf("saving test config: %v", err)
	}
	return env
}

func newCLITestConfig(gitFolder string) *config.Config {
	sshFolder := filepath.Join(filepath.Dir(gitFolder), "ssh")
	os.MkdirAll(sshFolder, 0o755)
	return &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder:        gitFolder,
			CredentialSSH: &config.SSHGlobal{SSHFolder: sshFolder},
		},
		Accounts: map[string]config.Account{},
		Sources:  map[string]config.Source{},
		Mirrors:  map[string]config.Mirror{},
	}
}

// newCLIIntegrationConfig builds a test config for integration tests.
// Uses the fixture's ssh_folder so SSH operations find keys and config there.
func newCLIIntegrationConfig(gitFolder string, fixture cliTestFixture) *config.Config {
	cfg := newCLITestConfig(gitFolder)
	if fixture.Config.Global.CredentialSSH != nil {
		cfg.Global.CredentialSSH = fixture.Config.Global.CredentialSSH
	}
	return cfg
}

// run executes the gitbox binary with --config pointing to the test config.
func (env cliTestEnv) run(t *testing.T, args ...string) cliResult {
	t.Helper()
	fullArgs := append([]string{"--config", env.CfgPath}, args...)
	cmd := exec.Command(env.BinPath, fullArgs...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("running CLI: %v", err)
		}
	}

	return cliResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// runJSON executes with --json and unmarshals stdout.
func (env cliTestEnv) runJSON(t *testing.T, target any, args ...string) cliResult {
	t.Helper()
	args = append(args, "--json")
	result := env.run(t, args...)
	if result.ExitCode != 0 {
		return result
	}
	if err := json.Unmarshal([]byte(result.Stdout), target); err != nil {
		t.Fatalf("unmarshaling JSON: %v\nstdout: %s", err, result.Stdout)
	}
	return result
}

// ---------------------------------------------------------------------------
// Config assertions (for CLI tests)
// ---------------------------------------------------------------------------

func cliAssertConfigHasAccount(t *testing.T, cfgPath, key string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Accounts[key]; !ok {
		t.Errorf("expected account %q in config", key)
	}
}

func cliAssertConfigNoAccount(t *testing.T, cfgPath, key string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Accounts[key]; ok {
		t.Errorf("expected account %q to be absent", key)
	}
}

func cliAssertConfigHasSource(t *testing.T, cfgPath, key string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Sources[key]; !ok {
		t.Errorf("expected source %q in config", key)
	}
}

func cliAssertConfigNoSource(t *testing.T, cfgPath, key string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Sources[key]; ok {
		t.Errorf("expected source %q to be absent", key)
	}
}

func cliAssertConfigHasRepo(t *testing.T, cfgPath, sourceKey, repoKey string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	src, ok := cfg.Sources[sourceKey]
	if !ok {
		t.Fatalf("source %q not found", sourceKey)
	}
	if _, ok := src.Repos[repoKey]; !ok {
		t.Errorf("expected repo %q in source %q", repoKey, sourceKey)
	}
}

func cliAssertConfigNoRepo(t *testing.T, cfgPath, sourceKey, repoKey string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	src, ok := cfg.Sources[sourceKey]
	if !ok {
		return // source gone = repo gone
	}
	if _, ok := src.Repos[repoKey]; ok {
		t.Errorf("expected repo %q to be absent from source %q", repoKey, sourceKey)
	}
}

func cliAssertConfigHasMirror(t *testing.T, cfgPath, key string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Mirrors[key]; !ok {
		t.Errorf("expected mirror %q in config", key)
	}
}

func cliAssertConfigNoMirror(t *testing.T, cfgPath, key string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Mirrors[key]; ok {
		t.Errorf("expected mirror %q to be absent", key)
	}
}

func cliAssertConfigHasMirrorRepo(t *testing.T, cfgPath, mirrorKey, repoKey string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	m, ok := cfg.Mirrors[mirrorKey]
	if !ok {
		t.Fatalf("mirror %q not found", mirrorKey)
	}
	if _, ok := m.Repos[repoKey]; !ok {
		t.Errorf("expected repo %q in mirror %q", repoKey, mirrorKey)
	}
}

func cliAssertConfigNoMirrorRepo(t *testing.T, cfgPath, mirrorKey, repoKey string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	m, ok := cfg.Mirrors[mirrorKey]
	if !ok {
		return // mirror gone = repo gone
	}
	if _, ok := m.Repos[repoKey]; ok {
		t.Errorf("expected repo %q to be absent from mirror %q", repoKey, mirrorKey)
	}
}

// ---------------------------------------------------------------------------
// test-gitbox.json fixture loader + integration gate
// ---------------------------------------------------------------------------

// cliTestFixture holds the parsed test configuration and per-account secrets.
type cliTestFixture struct {
	Config  *config.Config
	Secrets map[string]cliAccountTestSection // account_key → test secrets
}

type cliAccountTestSection struct {
	Token string `json:"token"`
}

func findCLIRepoRoot(t *testing.T) string {
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

func loadCLITestFixture(t *testing.T, path string) cliTestFixture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading test fixture %s: %v", path, err)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatalf("parsing test fixture config: %v", err)
	}

	// Extract _test from each account via raw JSON.
	var raw struct {
		Accounts map[string]struct {
			Test cliAccountTestSection `json:"_test"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing account _test sections: %v", err)
	}

	secrets := make(map[string]cliAccountTestSection)
	for key, acct := range raw.Accounts {
		if acct.Test.Token != "" {
			secrets[key] = acct.Test
		}
	}

	return cliTestFixture{
		Config:  cfg,
		Secrets: secrets,
	}
}

func requireCLIIntegration(t *testing.T) cliTestFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := findCLIRepoRoot(t)
	fixturePath := filepath.Join(root, "test-gitbox.json")
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		t.Fatal("test-gitbox.json not found. Integration tests need this file to run.\n" +
			"  Create it:  cp test-gitbox.json.example test-gitbox.json\n" +
			"  Or skip:    go test -short ./...")
	}
	fixture := loadCLITestFixture(t, fixturePath)
	validateFixturePaths(t, fixture.Config)
	for accountKey, sec := range fixture.Secrets {
		if sec.Token != "" {
			envName := credential.EnvVarName(accountKey)
			t.Setenv(envName, sec.Token)
		}
	}
	// Set GIT_SSH_COMMAND so git clone/fetch/pull use the isolated SSH config
	// instead of ~/.ssh/config. Use forward slashes for cross-platform compat.
	if fixture.Config.Global.CredentialSSH != nil {
		sshFolder := config.ExpandTilde(fixture.Config.Global.CredentialSSH.SSHFolder)
		sshConfig := filepath.ToSlash(filepath.Join(sshFolder, "config"))
		t.Setenv("GIT_SSH_COMMAND", fmt.Sprintf("ssh -F %s", sshConfig))
	}
	return fixture
}

// validateFixturePaths checks that test-gitbox.json does not point at real
// user directories (~/.ssh, ~/.config/gitbox). Tests must use isolated paths.
func validateFixturePaths(t *testing.T, cfg *config.Config) {
	t.Helper()
	home, _ := os.UserHomeDir()

	// Check ssh_folder is not ~/.ssh
	if cfg.Global.CredentialSSH != nil {
		sshFolder := config.ExpandTilde(cfg.Global.CredentialSSH.SSHFolder)
		realSSH := filepath.Join(home, ".ssh")
		if filepath.Clean(sshFolder) == filepath.Clean(realSSH) {
			t.Fatal("test-gitbox.json has ssh_folder pointing at ~/.ssh — this is dangerous.\n" +
				"  Use an isolated path like ~/.gitbox-test/ssh instead.")
		}
	}

	// Check global.folder is not a real gitbox config directory
	if cfg.Global.Folder != "" {
		folder := config.ExpandTilde(cfg.Global.Folder)
		gitboxConfig := filepath.Join(config.ConfigRoot(), "gitbox")
		if filepath.Clean(folder) == filepath.Clean(gitboxConfig) {
			t.Fatal("test-gitbox.json has global.folder pointing at ~/.config/gitbox — this is dangerous.\n" +
				"  Use an isolated path like ~/.gitbox-test/git instead.")
		}
	}
}

func (f cliTestFixture) hasToken(accountKey string) bool {
	sec, ok := f.Secrets[accountKey]
	return ok && sec.Token != ""
}

// firstAccountWithRepos returns the first account that has a source with repos
// and a token in the fixture. Uses sorted keys for deterministic selection —
// Go map iteration is random, so without sorting the test would pick a
// different repo on each run.
func (f cliTestFixture) firstAccountWithRepos() (accountKey, sourceKey, repoKey string, ok bool) {
	srcKeys := make([]string, 0, len(f.Config.Sources))
	for k := range f.Config.Sources {
		srcKeys = append(srcKeys, k)
	}
	sort.Strings(srcKeys)

	for _, sKey := range srcKeys {
		src := f.Config.Sources[sKey]
		if len(src.Repos) == 0 {
			continue
		}
		aKey := src.Account
		if !f.hasToken(aKey) {
			continue
		}
		repoKeys := make([]string, 0, len(src.Repos))
		for k := range src.Repos {
			repoKeys = append(repoKeys, k)
		}
		sort.Strings(repoKeys)
		return aKey, sKey, repoKeys[0], true
	}
	return "", "", "", false
}

