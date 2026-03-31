# Test authoring patterns

How to write new tests for gitbox. Reference this when adding or modifying tests.

## Test infrastructure files

- `cmd/cli/tui/testhelpers_test.go` — TUI helpers: `TestEnv`, config builders, assertions, TUI message helpers
- `cmd/cli/cli_test.go` — CLI helpers: `cliTestEnv`, binary build cache, subprocess runner, assertions

## TUI tests (`cmd/cli/tui/`)

```go
func TestSomething(t *testing.T) {
    cfg := newDummyConfig(t, "/tmp/test-git")  // 2 dummy accounts + sources
    env := setupTestEnvWithConfig(t, cfg)       // XDG_CONFIG_HOME → temp, writes config
    m := newTestModel(t, env.CfgPath)           // model at 80x24
    m = initModel(t, m)                         // run Init + dispatch configLoadedMsg

    // Navigate
    m = sendMsg(m, switchScreenMsg{screen: screenAccountAdd})
    m = sendKey(m, "a")                         // single key press
    m = sendSpecialKey(m, tea.KeyEnter)          // special keys
    m = sendWindowSize(m, 120, 40)              // resize

    // Assert screen state
    if m.screen != screenAccountAdd { t.Error(...) }

    // Assert rendered output
    view := m.View()
    if !strings.Contains(view, "expected text") { t.Error(...) }

    // Assert config file on disk (external verification)
    assertConfigHasAccount(t, env.CfgPath, "key")
    assertConfigGlobalFolder(t, env.CfgPath, "/expected/path")
}
```

### Config builders

- `newTestConfig(t, gitFolder)` — minimal v2 config, empty accounts/sources/mirrors, ssh_folder in temp
- `newDummyConfig(t, gitFolder)` — 2 dummy accounts (github-alice, forgejo-bob) + sources with repos

### TUI message helpers

- `sendMsg(m, msg) model` — dispatch any tea.Msg through Update
- `sendKey(m, "a") model` — send a rune key press
- `sendSpecialKey(m, tea.KeyEnter) model` — send Enter, Tab, Escape, etc.
- `sendWindowSize(m, w, h) model` — send WindowSizeMsg
- `initModel(t, m) model` — run Init() command and dispatch the result

## CLI tests (`cmd/cli/`)

```go
func TestCLI_Something(t *testing.T) {
    cfg := newCLITestConfig("/tmp/test-git")
    cfg.Accounts["test-acct"] = config.Account{...}
    env := setupCLIEnvWithConfig(t, cfg)        // XDG_CONFIG_HOME → temp, writes config, builds binary

    // Run a command
    result := env.run(t, "account", "list", "--json")
    if result.ExitCode != 0 { t.Fatal(result.Stderr) }

    // Parse JSON output
    var out map[string]any
    result := env.runJSON(t, &out, "account", "show", "test-acct")

    // Assert config on disk
    cliAssertConfigHasAccount(t, env.CfgPath, "test-acct")
    cliAssertConfigNoAccount(t, env.CfgPath, "deleted-acct")
    cliAssertConfigHasSource(t, env.CfgPath, "src-key")
    cliAssertConfigHasRepo(t, env.CfgPath, "src-key", "org/repo")
    cliAssertConfigHasMirror(t, env.CfgPath, "mirror-key")
    cliAssertConfigHasMirrorRepo(t, env.CfgPath, "mirror-key", "org/repo")
}
```

### CLI runner

- `env.run(t, args...) cliResult` — runs `gitbox --config <cfgPath> <args>` with NO_COLOR=1
- `env.runJSON(t, &target, args...) cliResult` — same + appends `--json`, unmarshals stdout
- `cliResult` has `.Stdout`, `.Stderr`, `.ExitCode`
- Binary is compiled once per test run (cached via `sync.Once`)

## Integration tests

```go
func TestIntegration_Something(t *testing.T) {
    fixture := requireCLIIntegration(t)  // loads test-gitbox.json, sets GITBOX_TOKEN_* env vars, skips if missing

    // Find an account with repos
    ghKey, srcKey, repo, ok := fixture.firstAccountWithRepos()
    if !ok { t.Skip("no account with repos and token") }
    acct := fixture.Config.Accounts[ghKey]

    // Build throwaway config with real account
    env := setupCLIEnv(t)
    cfg := newCLITestConfig(env.GitFolder)
    cfg.Accounts[ghKey] = acct
    cfg.Sources[srcKey] = config.Source{Account: ghKey, Repos: map[string]config.Repo{repo: {}}}
    config.Save(cfg, env.CfgPath)

    // Test real operations
    env.run(t, "clone")
    // ... verify on disk
}
```

### Fixture helpers

- `fixture.FirstAccountWithRepos() (accountKey, sourceKey, repoKey string, ok bool)` — first account with sources+repos+token
- `fixture.HasToken(accountKey) bool` — check if account has a test token
- `fixture.FirstSourceForAccount(accountKey) (string, bool)` — first source key for account
- `fixture.FirstRepoForSource(sourceKey) (string, bool)` — first repo key from source
- `fixture.Config` — the parsed gitbox config from test-gitbox.json
- `fixture.Secrets` — map of account_key → `{Token, SSHKey}`
- `firstTokenAccount(fixture) (string, bool)` — first account with `default_credential_type: "token"` and a test token
- `accountCardIndex(fixture, accountKey) int` — sorted index for card navigation

## Credential screen tests (`cmd/cli/tui/screen_credential_test.go`)

Tests for the GCM browser auth flow use a dedicated config builder and navigation helper:

```go
func TestCredential_GCM_Something(t *testing.T) {
    cfg := newGCMConfig(t, "/tmp/test-git")           // 1 GCM account (github-gcmuser)
    env := setupTestEnvWithConfig(t, cfg)
    m := navigateToCredentialScreen(t, env.CfgPath, "github-gcmuser")

    // Manipulate view state directly for unit tests.
    m.credential.view = credViewSetup
    m.credential.busy = true

    // Send completion message to simulate async GCM auth.
    m = sendMsg(m, credSetupDoneMsg{accountKey: "github-gcmuser", gcmUsername: "gcmuser"})

    // Assert state.
    if !m.credential.resultOK { t.Error("expected success") }

    // Assert rendered output.
    view := m.View()
    if !strings.Contains(view, "expected text") { t.Error(...) }
}
```

### Credential screen helpers

- `newGCMConfig(t, gitFolder) *config.Config` — config with 1 GCM account (`github-gcmuser`)
- `navigateToCredentialScreen(t, cfgPath, accountKey) model` — creates model, inits, navigates to credential screen

### credSetupDoneMsg fields

- `err` — auth failure (displayed as error)
- `needsPAT: true` — GCM auth succeeded but API needs a separate PAT (transitions to token input)
- `gcmUsername` — actual username from GCM (may differ in casing from config)
- `sshPendingKey` — SSH key generated but connection failed (user needs to add pubkey)
- `pubKey`, `pubKeyURL` — public key content and provider URL for SSH flows

### Browser detection in tests

`credential.CanOpenBrowser()` returns a real result based on the test machine's environment. Tests that assert browser-specific UI use conditional expectations:

```go
if credential.CanOpenBrowser() {
    // Desktop: expect browser auth prompt.
} else {
    // Headless: expect "desktop session" message.
}
```

## TUI integration tests (teatest)

```go
func TestIntegration_TUI_Something(t *testing.T) {
    fixture := requireIntegration(t)  // loads test-gitbox.json, skips if missing

    tm, env := newIntegrationTestModel(t, fixture)  // teatest model with isolated env

    // Wait for text in rendered output (strips ANSI, polls every 100ms).
    waitForText(t, tm, "expected text", 5*time.Second)

    // Send keys.
    tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
    tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})

    // Wait for next screen.
    waitForText(t, tm, "next screen text", 10*time.Second)
}
```

### Teatest gotchas

- **ANSI compressor**: teatest uses `tea.WithANSICompressor()` which only sends changed cells on re-renders. Card titles in multi-card rows may not appear in subsequent frames. Assert on repo list headings or hint bar text instead.
- **Output pipe**: `tm.Output()` returns the same reader. `WaitFor` consumes bytes — data from before a WaitFor call is available, but between calls the position advances. For checking multiple strings, use a single `teatest.WaitFor` call.
- **ANSI stripping**: Use `stripANSI(bts)` before `bytes.Contains`. The regex handles CSI, OSC, and private-mode sequences (`\x1b[?25l`, etc.).
- **NO_COLOR**: `newIntegrationTestModel` sets `NO_COLOR=1` but lipgloss still emits cursor movement codes (not colors). Always use `stripANSI`.
- **Credential types**: For tests that verify credential status, use `firstTokenAccount(fixture)` to get a token-based account where env var resolution works directly.

## TUI assertion helpers (`testhelpers_test.go`)

- `assertConfigHasAccount(t, cfgPath, key)` — account exists in config file
- `assertConfigNoAccount(t, cfgPath, key)` — account absent
- `assertConfigHasSource(t, cfgPath, key)` — source exists
- `assertConfigHasRepo(t, cfgPath, srcKey, repoKey)` — repo in source
- `assertConfigHasMirror(t, cfgPath, key)` — mirror group exists
- `assertConfigHasMirrorRepo(t, cfgPath, mirrorKey, repoKey)` — mirror repo exists
- `assertConfigGlobalFolder(t, cfgPath, expected)` — global.folder matches
- `assertCloneExists(t, gitFolder, srcKey, repoKey)` — .git dir exists on disk
- `assertCloneNotExists(t, gitFolder, srcKey, repoKey)` — dir doesn't exist
- `assertGitRemote(t, repoPath, expectedURL)` — git remote get-url origin
- `assertGitIdentity(t, repoPath, name, email)` — git config user.name/email
- `assertFileNotExists(t, path)` — file/dir doesn't exist
- `assertCredentialWorks(t, cfg, accountKey)` — credential.ResolveToken succeeds

## Test isolation

- `XDG_CONFIG_HOME` → temp dir (set by `setupTestEnv` / `setupCLIEnv`)
- `global.folder` → `<tmpDir>/git`
- `credential_ssh.ssh_folder` → `<tmpDir>/ssh`
- Config → `<tmpDir>/gitbox/gitbox.json`
- Credentials → `<tmpDir>/gitbox/credentials/<accountKey>`
- All cleaned by `t.TempDir()` after each test
- CLI uses `--config <path>` flag — never falls through to DefaultV2Path
- TUI tests call `newModel(cfgPath)` directly — never call `tui.Run()`

## JSON output format

CLI `--json` output is maps, not arrays:
- `account list --json` → `map[string]any` (key → account object)
- `source list --json` → `map[string]any` (key → source object)
- `repo list --json` → `map[string]any` (key → source with repos)
- `mirror list --json` → `map[string]any` (key → mirror object)
- `status --json` → `[]map[string]any` (array of repo statuses)
- `account show <key> --json` → `map[string]any` (single account)

## Naming conventions

- Unit tests: `TestXxx_Yyy` (e.g., `TestDashboard_TabSwitch`)
- CLI tests: `TestCLI_Xxx` (e.g., `TestCLI_AccountAdd`)
- Integration: `TestIntegration_CLI_Xxx` or `TestIntegration_TUI_Xxx`
- Scenario: `TestScenario_CLI_FullLifecycle`
