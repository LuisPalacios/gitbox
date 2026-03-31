package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// ---------------------------------------------------------------------------
// TestEnv — isolated test environment
// ---------------------------------------------------------------------------

// TestEnv holds paths for an isolated test environment.
// All paths are under a temp dir that is auto-cleaned by t.TempDir().
type TestEnv struct {
	CfgPath   string // path to gitbox.json
	GitFolder string // throwaway clone folder
	TmpDir    string // root temp dir
}

// setupTestEnv creates an isolated environment with XDG_CONFIG_HOME redirected.
// Config path: <tmpDir>/gitbox/gitbox.json
// Git folder: <tmpDir>/git
func setupTestEnv(t *testing.T) TestEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfgDir := filepath.Join(tmp, "gitbox")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	gitDir := filepath.Join(tmp, "git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("creating git dir: %v", err)
	}
	return TestEnv{
		CfgPath:   filepath.Join(cfgDir, "gitbox.json"),
		GitFolder: gitDir,
		TmpDir:    tmp,
	}
}

// setupTestEnvWithConfig creates the environment and writes a config file.
func setupTestEnvWithConfig(t *testing.T, cfg *config.Config) TestEnv {
	t.Helper()
	env := setupTestEnv(t)
	if err := config.Save(cfg, env.CfgPath); err != nil {
		t.Fatalf("saving test config: %v", err)
	}
	return env
}

// ---------------------------------------------------------------------------
// Config builders
// ---------------------------------------------------------------------------

// newTestConfig returns a minimal valid v2 config pointing at the given git folder.
// SSH folder is placed alongside the git folder (e.g., <tmpDir>/ssh) to avoid
// touching the real ~/.ssh.
func newTestConfig(t *testing.T, gitFolder string) *config.Config {
	t.Helper()
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

// newDummyConfig returns a config with 2 dummy accounts and sources for unit tests.
// No real credentials needed — use t.Setenv for fake tokens if needed.
func newDummyConfig(t *testing.T, gitFolder string) *config.Config {
	t.Helper()
	cfg := newTestConfig(t, gitFolder)
	cfg.Accounts = map[string]config.Account{
		"github-alice": {
			Provider:              "github",
			URL:                   "https://github.com",
			Username:              "alice",
			Name:                  "Alice Smith",
			Email:                 "alice@example.com",
			DefaultCredentialType: "token",
		},
		"forgejo-bob": {
			Provider:              "forgejo",
			URL:                   "https://forge.example.com",
			Username:              "bob",
			Name:                  "Bob Jones",
			Email:                 "bob@example.com",
			DefaultCredentialType: "token",
		},
	}
	cfg.Sources = map[string]config.Source{
		"alice-repos": {
			Account: "github-alice",
			Repos: map[string]config.Repo{
				"alice/hello-world": {},
			},
			RepoOrder: []string{"alice/hello-world"},
		},
		"bob-repos": {
			Account: "forgejo-bob",
			Repos: map[string]config.Repo{
				"bob/my-project": {},
			},
			RepoOrder: []string{"bob/my-project"},
		},
	}
	cfg.SourceOrder = []string{"alice-repos", "bob-repos"}
	return cfg
}

// ---------------------------------------------------------------------------
// test-gitbox.json fixture loader + integration gate
// ---------------------------------------------------------------------------

// TestFixture holds the parsed test configuration and per-account secrets.
type TestFixture struct {
	Config  *config.Config                // standard gitbox config
	Secrets map[string]accountTestSection // account_key → test secrets
}

// accountTestSection holds test-only secrets embedded in each account's "_test" key.
type accountTestSection struct {
	Token string `json:"token"` // PAT for token/API auth
}

// findRepoRoot walks up from cwd to find go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
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

// loadTestFixture reads test-gitbox.json from the repo root.
// The file is a valid gitbox config where each account may contain a "_test"
// key with secrets (token). These are silently ignored by config.Parse().
func loadTestFixture(t *testing.T, path string) TestFixture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading test fixture %s: %v", path, err)
	}

	// Parse as gitbox config (ignores unknown keys like _test inside accounts).
	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatalf("parsing test fixture config: %v", err)
	}

	// Extract _test from each account via raw JSON.
	var raw struct {
		Accounts map[string]struct {
			Test accountTestSection `json:"_test"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing account _test sections: %v", err)
	}

	secrets := make(map[string]accountTestSection)
	for key, acct := range raw.Accounts {
		if acct.Test.Token != "" {
			secrets[key] = acct.Test
		}
	}

	return TestFixture{
		Config:  cfg,
		Secrets: secrets,
	}
}

// requireIntegration loads test-gitbox.json from the repo root, sets token
// env vars, and returns the fixture. Skips with -short; fails if fixture missing.
func requireIntegration(t *testing.T) TestFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	root := findRepoRoot(t)
	fixturePath := filepath.Join(root, "test-gitbox.json")
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		t.Fatal("test-gitbox.json not found. Integration tests need this file to run.\n" +
			"  Create it:  cp test-gitbox.json.example test-gitbox.json\n" +
			"  Or skip:    go test -short ./...")
	}

	fixture := loadTestFixture(t, fixturePath)
	validateFixturePaths(t, fixture.Config)

	// Set token env vars so credential.ResolveToken() finds them.
	for accountKey, sec := range fixture.Secrets {
		if sec.Token != "" {
			envName := credential.EnvVarName(accountKey)
			t.Setenv(envName, sec.Token)
		}
	}

	return fixture
}

// validateFixturePaths checks that test-gitbox.json does not point at real
// user directories (~/.ssh, ~/.config/gitbox). Tests must use isolated paths.
func validateFixturePaths(t *testing.T, cfg *config.Config) {
	t.Helper()
	home, _ := os.UserHomeDir()

	if cfg.Global.CredentialSSH != nil {
		sshFolder := config.ExpandTilde(cfg.Global.CredentialSSH.SSHFolder)
		realSSH := filepath.Join(home, ".ssh")
		if filepath.Clean(sshFolder) == filepath.Clean(realSSH) {
			t.Fatal("test-gitbox.json has ssh_folder pointing at ~/.ssh — this is dangerous.\n" +
				"  Use an isolated path like ~/.gitbox-test/ssh instead.")
		}
	}

	if cfg.Global.Folder != "" {
		folder := config.ExpandTilde(cfg.Global.Folder)
		gitboxConfig := filepath.Join(config.ConfigRoot(), "gitbox")
		if filepath.Clean(folder) == filepath.Clean(gitboxConfig) {
			t.Fatal("test-gitbox.json has global.folder pointing at ~/.config/gitbox — this is dangerous.\n" +
				"  Use an isolated path like ~/.gitbox-test/git instead.")
		}
	}
}

// HasToken returns true if the account has a token in the test secrets.
func (f TestFixture) HasToken(accountKey string) bool {
	sec, ok := f.Secrets[accountKey]
	return ok && sec.Token != ""
}

// FirstAccountWithRepos returns the first account that has a source with repos
// and a token in the fixture. Returns the account key, source key, and first repo key.
func (f TestFixture) FirstAccountWithRepos() (accountKey, sourceKey, repoKey string, ok bool) {
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
		if !f.HasToken(aKey) {
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

// FirstSourceForAccount returns the first source key that references the given account.
func (f TestFixture) FirstSourceForAccount(accountKey string) (string, bool) {
	for key, src := range f.Config.Sources {
		if src.Account == accountKey {
			return key, true
		}
	}
	return "", false
}

// FirstRepoForSource returns the first repo key from the given source.
func (f TestFixture) FirstRepoForSource(sourceKey string) (string, bool) {
	if src, ok := f.Config.Sources[sourceKey]; ok {
		for key := range src.Repos {
			return key, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// TUI test helpers
// ---------------------------------------------------------------------------

// sendMsg dispatches a message through the model's Update and returns the updated model.
func sendMsg(m model, msg tea.Msg) model {
	updated, _ := m.Update(msg)
	return updated.(model)
}

// sendKey sends a single key press through the model's Update.
func sendKey(m model, key string) model {
	return sendMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

// sendSpecialKey sends a special key (Enter, Tab, Escape, etc.) through Update.
func sendSpecialKey(m model, keyType tea.KeyType) model {
	return sendMsg(m, tea.KeyMsg{Type: keyType})
}

// sendWindowSize sends a terminal resize message.
func sendWindowSize(m model, w, h int) model {
	return sendMsg(m, tea.WindowSizeMsg{Width: w, Height: h})
}

// newTestModel creates a model pointed at the given config path,
// pre-sized to 80x24 for deterministic rendering.
func newTestModel(t *testing.T, cfgPath string) model {
	t.Helper()
	m := newModel(cfgPath)
	m = sendWindowSize(m, 80, 24)
	return m
}

// initModel runs the model's Init command and dispatches the resulting message.
// This simulates what Bubble Tea does on startup: run Init(), get a Cmd, execute it,
// send the resulting Msg back through Update.
func initModel(t *testing.T, m model) model {
	t.Helper()
	cmd := m.Init()
	if cmd == nil {
		return m
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	return updated.(model)
}

// ---------------------------------------------------------------------------
// Assertion helpers — config file verification
// ---------------------------------------------------------------------------

// loadConfigFromDisk reads and parses the config file at path.
func loadConfigFromDisk(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading config from %s: %v", path, err)
	}
	return cfg
}

// assertConfigHasAccount verifies that the config file contains the given account key.
func assertConfigHasAccount(t *testing.T, cfgPath, accountKey string) {
	t.Helper()
	cfg := loadConfigFromDisk(t, cfgPath)
	if _, ok := cfg.Accounts[accountKey]; !ok {
		t.Errorf("expected account %q in config, got keys: %v", accountKey, mapKeys(cfg.Accounts))
	}
}

// assertConfigNoAccount verifies the account key is NOT in the config.
func assertConfigNoAccount(t *testing.T, cfgPath, accountKey string) {
	t.Helper()
	cfg := loadConfigFromDisk(t, cfgPath)
	if _, ok := cfg.Accounts[accountKey]; ok {
		t.Errorf("expected account %q to be absent from config", accountKey)
	}
}

// assertConfigHasSource verifies the source key exists in the config.
func assertConfigHasSource(t *testing.T, cfgPath, sourceKey string) {
	t.Helper()
	cfg := loadConfigFromDisk(t, cfgPath)
	if _, ok := cfg.Sources[sourceKey]; !ok {
		t.Errorf("expected source %q in config, got keys: %v", sourceKey, mapKeys(cfg.Sources))
	}
}

// assertConfigHasRepo verifies the repo exists within the source.
func assertConfigHasRepo(t *testing.T, cfgPath, sourceKey, repoKey string) {
	t.Helper()
	cfg := loadConfigFromDisk(t, cfgPath)
	src, ok := cfg.Sources[sourceKey]
	if !ok {
		t.Fatalf("source %q not found", sourceKey)
	}
	if _, ok := src.Repos[repoKey]; !ok {
		t.Errorf("expected repo %q in source %q, got keys: %v", repoKey, sourceKey, mapKeys(src.Repos))
	}
}

// assertConfigHasMirror verifies the mirror group exists.
func assertConfigHasMirror(t *testing.T, cfgPath, mirrorKey string) {
	t.Helper()
	cfg := loadConfigFromDisk(t, cfgPath)
	if _, ok := cfg.Mirrors[mirrorKey]; !ok {
		t.Errorf("expected mirror %q in config, got keys: %v", mirrorKey, mapKeys(cfg.Mirrors))
	}
}

// assertConfigHasMirrorRepo verifies the mirror repo exists within the group.
func assertConfigHasMirrorRepo(t *testing.T, cfgPath, mirrorKey, repoKey string) {
	t.Helper()
	cfg := loadConfigFromDisk(t, cfgPath)
	m, ok := cfg.Mirrors[mirrorKey]
	if !ok {
		t.Fatalf("mirror %q not found", mirrorKey)
	}
	if _, ok := m.Repos[repoKey]; !ok {
		t.Errorf("expected repo %q in mirror %q, got keys: %v", repoKey, mirrorKey, mapKeys(m.Repos))
	}
}

// assertConfigGlobalFolder verifies global.folder is set to the expected value.
func assertConfigGlobalFolder(t *testing.T, cfgPath, expected string) {
	t.Helper()
	cfg := loadConfigFromDisk(t, cfgPath)
	if cfg.Global.Folder != expected {
		t.Errorf("expected global.folder=%q, got %q", expected, cfg.Global.Folder)
	}
}

// ---------------------------------------------------------------------------
// Assertion helpers — filesystem verification
// ---------------------------------------------------------------------------

// assertCloneExists verifies a cloned repo directory has a .git subdirectory.
func assertCloneExists(t *testing.T, gitFolder, sourceKey, repoKey string) {
	t.Helper()
	// Repo path: <gitFolder>/<sourceKey>/<org>/<repo>
	parts := strings.SplitN(repoKey, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("repoKey %q not in org/repo format", repoKey)
	}
	repoPath := filepath.Join(gitFolder, sourceKey, parts[0], parts[1])
	gitPath := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitPath); os.IsNotExist(err) {
		t.Errorf("expected clone at %s (no .git found)", repoPath)
	}
}

// assertCloneNotExists verifies a cloned repo directory does NOT exist.
func assertCloneNotExists(t *testing.T, gitFolder, sourceKey, repoKey string) {
	t.Helper()
	parts := strings.SplitN(repoKey, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("repoKey %q not in org/repo format", repoKey)
	}
	repoPath := filepath.Join(gitFolder, sourceKey, parts[0], parts[1])
	if _, err := os.Stat(repoPath); err == nil {
		t.Errorf("expected clone at %s to NOT exist", repoPath)
	}
}

// assertGitRemote verifies the origin remote URL of a cloned repo.
func assertGitRemote(t *testing.T, repoPath, expectedURL string) {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatalf("git remote get-url origin in %s: %v", repoPath, err)
	}
	got := strings.TrimSpace(string(out))
	if got != expectedURL {
		t.Errorf("remote URL: got %q, want %q", got, expectedURL)
	}
}

// assertGitIdentity verifies per-repo user.name and user.email.
func assertGitIdentity(t *testing.T, repoPath, name, email string) {
	t.Helper()
	gotName, err := exec.Command("git", "-C", repoPath, "config", "user.name").Output()
	if err != nil {
		t.Fatalf("git config user.name in %s: %v", repoPath, err)
	}
	if strings.TrimSpace(string(gotName)) != name {
		t.Errorf("user.name: got %q, want %q", strings.TrimSpace(string(gotName)), name)
	}

	gotEmail, err := exec.Command("git", "-C", repoPath, "config", "user.email").Output()
	if err != nil {
		t.Fatalf("git config user.email in %s: %v", repoPath, err)
	}
	if strings.TrimSpace(string(gotEmail)) != email {
		t.Errorf("user.email: got %q, want %q", strings.TrimSpace(string(gotEmail)), email)
	}
}

// assertFileNotExists verifies a file or directory does not exist.
func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected %s to not exist", path)
	}
}

// assertCredentialWorks verifies that a token can be resolved for the account.
func assertCredentialWorks(t *testing.T, cfg *config.Config, accountKey string) {
	t.Helper()
	acct, ok := cfg.Accounts[accountKey]
	if !ok {
		t.Fatalf("account %q not in config", accountKey)
	}
	token, source, err := credential.ResolveToken(acct, accountKey)
	if err != nil {
		t.Errorf("credential resolve for %q failed: %v", accountKey, err)
		return
	}
	if token == "" {
		t.Errorf("credential resolve for %q returned empty token (source: %s)", accountKey, source)
	}
}

// ---------------------------------------------------------------------------
// teatest integration helpers
// ---------------------------------------------------------------------------

// newIntegrationTestModel creates a teatest.TestModel from the integration
// fixture. It builds an isolated config with temp folder paths, starts the
// real Bubble Tea event loop, and registers cleanup (Ctrl+C → quit).
func newIntegrationTestModel(t *testing.T, fixture TestFixture, opts ...teatest.TestOption) (*teatest.TestModel, TestEnv) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")

	env := setupTestEnv(t)

	// Build config from fixture with temp folder paths.
	sshFolder := filepath.Join(env.TmpDir, "ssh")
	if err := os.MkdirAll(sshFolder, 0o755); err != nil {
		t.Fatalf("creating ssh folder: %v", err)
	}
	cfg := &config.Config{
		Version:  2,
		Global:   fixture.Config.Global,
		Accounts: fixture.Config.Accounts,
		Sources:  fixture.Config.Sources,
		Mirrors:  fixture.Config.Mirrors,
	}
	cfg.Global.Folder = env.GitFolder
	if cfg.Global.CredentialSSH == nil {
		cfg.Global.CredentialSSH = &config.SSHGlobal{}
	}
	cfg.Global.CredentialSSH.SSHFolder = sshFolder
	if err := config.Save(cfg, env.CfgPath); err != nil {
		t.Fatalf("saving integration config: %v", err)
	}

	m := newModel(env.CfgPath)

	defaults := []teatest.TestOption{teatest.WithInitialTermSize(80, 24)}
	defaults = append(defaults, opts...)
	tm := teatest.NewTestModel(t, m, defaults...)

	t.Cleanup(func() {
		tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
		tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	})

	return tm, env
}

// waitForText polls the test model's output until text appears or timeout expires.
// It strips ANSI escape codes before checking, since lipgloss emits cursor
// movement and erase sequences even with NO_COLOR=1.
func waitForText(t *testing.T, tm *teatest.TestModel, text string, timeout time.Duration) {
	t.Helper()
	teatest.WaitFor(
		t, tm.Output(),
		func(bts []byte) bool {
			return bytes.Contains(stripANSI(bts), []byte(text))
		},
		teatest.WithDuration(timeout),
		teatest.WithCheckInterval(100*time.Millisecond),
	)
}

// ansiEscape matches ANSI escape sequences: CSI sequences (including private
// mode like \x1b[?25l), OSC sequences, and two-byte ESC sequences.
var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[a-zA-Z]|\][^\x07]*\x07|[()][A-Z0-9])`)

// stripANSI removes all ANSI escape sequences from the byte slice.
func stripANSI(b []byte) []byte {
	return ansiEscape.ReplaceAll(b, nil)
}

// firstTokenAccount returns the first account with default_credential_type "token"
// and a test token in the fixture. Returns accountKey or empty string.
func firstTokenAccount(fixture TestFixture) (string, bool) {
	for accountKey, acct := range fixture.Config.Accounts {
		if acct.DefaultCredentialType == "token" && fixture.HasToken(accountKey) {
			return accountKey, true
		}
	}
	return "", false
}

// accountCardIndex returns the sorted index of an account key in the fixture.
// Uses the production sortedAccountKeys(cfg) to match dashboard ordering.
func accountCardIndex(fixture TestFixture, accountKey string) int {
	keys := sortedAccountKeys(fixture.Config)
	for i, k := range keys {
		if k == accountKey {
			return i
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// CLI test helpers
// ---------------------------------------------------------------------------

// CLIResult holds the output of a CLI subprocess execution.
type CLIResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// buildTestBinary compiles the gitbox CLI binary and returns the path.
// Uses t.TempDir() for the output; cached across subtests via the parent test's env.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	binName := "gitbox"
	if os.PathSeparator == '\\' {
		binName = "gitbox.exe"
	}
	binPath := filepath.Join(binDir, binName)
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/cli")
	// Build from repo root.
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building test binary: %v\n%s", err, out)
	}
	return binPath
}

// runCLI executes the gitbox binary with --config pointing to the test config.
// NO_COLOR=1 is set for deterministic output.
func runCLI(t *testing.T, binPath, cfgPath string, args ...string) CLIResult {
	t.Helper()
	fullArgs := append([]string{"--config", cfgPath}, args...)
	cmd := exec.Command(binPath, fullArgs...)
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

	return CLIResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// runCLIJSON executes with --json and unmarshals stdout into target.
func runCLIJSON(t *testing.T, binPath, cfgPath string, target any, args ...string) CLIResult {
	t.Helper()
	args = append(args, "--json")
	result := runCLI(t, binPath, cfgPath, args...)
	if result.ExitCode != 0 {
		return result
	}
	if err := json.Unmarshal([]byte(result.Stdout), target); err != nil {
		t.Fatalf("unmarshaling JSON output: %v\nstdout: %s", err, result.Stdout)
	}
	return result
}

// ---------------------------------------------------------------------------
// Generic helpers
// ---------------------------------------------------------------------------

// mapKeys returns the keys of any map as a sorted string for error messages.
func mapKeys[K comparable, V any](m map[K]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, fmt.Sprintf("%v", k))
	}
	return keys
}
