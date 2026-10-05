# Test authoring patterns

How to write new tests for gitbox. Reference this when adding or modifying tests.

All logic lives in `pkg/`, so tests live there too: plain `go test` packages with no UI driver. `cmd/gui` keeps a few tests for the Wails-bound `App` methods.

## Test infrastructure files

- `pkg/ops/ops_test.go` — isolation and git helpers for the account, credential, clone and discovery service layer
- `pkg/ops/fixture_test.go` — `test-gitbox.json` loader and fixture helpers for integration tests
- `pkg/ops/scenario_test.go` — `TestScenario_FullLifecycle`, the end-to-end run against real providers

## Package unit tests (`pkg/...`)

Unit tests never touch the real home directory, git config, or network. Use `t.TempDir()` for every path and `t.Setenv()` for every environment override, so cleanup is automatic.

```go
func TestRenameAccount_Something(t *testing.T) {
    _, cfg := isolate(t)                            // temp XDG_CONFIG_HOME, git global config, ssh folder
    if err := AddAccount(cfg, "old", testAccount("token")); err != nil {
        t.Fatal(err)
    }
    if err := cfg.AddRepo("old", "alice/repo", config.Repo{}); err != nil {
        t.Fatal(err)
    }
    makeClone(t, cfg, "old", "alice/repo", "https://github.com/alice/repo.git")

    if err := RenameAccount(cfg, "old", "new"); err != nil {
        t.Fatal(err)
    }

    // Assert on the config struct, then on disk: the clone moved with the
    // account, so look it up again with PlanClone and inspect it with gitRun.
    if _, ok := cfg.Accounts["new"]; !ok { t.Error(...) }
}
```

### `pkg/ops` helpers

- `isolate(t) (root string, cfg *config.Config)` — points `XDG_CONFIG_HOME`, `GIT_CONFIG_GLOBAL` and the SSH folder at a temp dir, sets `GIT_CONFIG_NOSYSTEM=1`, returns an empty v3 config whose `global.folder` is `<root>/git`
- `testAccount(credType) config.Account` — a GitHub account (`alice`) with the given default credential type
- `gitRun(t, dir, args...) string` — runs git in `dir` with `git.Environ()`, fails the test on error, returns trimmed output
- `makeClone(t, cfg, sourceKey, repoKey, origin) string` — `git init` at the planned clone path with the given `origin`, returns the path

### `pkg/update` helpers

- `releaseServer{...}.start(t)` — an `httptest` server for one fake release: artifact, `checksums.sha256` and its `.sig`, each with an optional failing status; `artifactHits` counts artifact downloads
- `release(t, srv, withChecksums, withSig)` — the `ReleaseInfo` for that server, tagged `testTag`
- `isolateTemp(t)` — points the temp dir at a fresh directory, so a refused update can be checked for leftovers
- `newTestSigner(t)` — an in-process ed25519 key that writes `ssh-keygen -Y sign` compatible signatures (`sign(t, tag, checksums, namespace, hashAlg)`); `useAsReleaseKey(t)` makes `DownloadRelease` trust it for one test
- `testdata/*.sig` — signatures made by real `ssh-keygen` (private key discarded), proving the verifier reads what OpenSSH writes. To add one, sign `"gitbox v9.9.9\n" + testdata/checksums.sha256` with a throwaway key and commit only the `.pub` and the `.sig`; `.gitattributes` keeps the bytes exact

Other packages follow the same idea with their own local helpers. Prefer table-driven tests for parsers, URL handling and status logic.

## GUI binding tests (`cmd/gui/`)

Build the `App` struct directly and call the bound method. No Wails runtime, no window:

```go
func TestSomething(t *testing.T) {
    dir := t.TempDir()
    cfg := &config.Config{Version: config.CurrentVersion, Global: config.GlobalConfig{Folder: dir}, ...}
    a := &App{cfg: cfg, cfgPath: filepath.Join(dir, "gitbox.json"), mu: sync.Mutex{}}

    a.SyncEditors()

    // Assert on a.cfg and on the config file at a.cfgPath.
}
```

Keep business logic out of `cmd/gui`: if a test needs more than the `App` glue, the logic belongs in `pkg/` and the test goes with it.

## Integration tests and the fixture gate

Integration tests run real provider operations with credentials from `test-gitbox.json` at the repo root (copy `json/test-gitbox.json.example` and fill it in). The gate is `requireIntegration(t)`:

- `go test -short` — skips every integration test
- full run without the file — fails with instructions, so a full run never passes silently without coverage
- with the file — exports each account's `_test.token` as `GITBOX_TOKEN_<KEY>`, points `GIT_SSH_COMMAND` at the fixture's isolated SSH folder, and rejects fixtures whose `ssh_folder` is `~/.ssh` or whose `global.folder` is the real gitbox config dir

```go
func TestIntegration_Something(t *testing.T) {
    fixture := requireIntegration(t)
    acctKey, srcKey, repoKey, ok := fixture.firstAccountWithRepos()
    if !ok { t.Skip("no account with repos and token in test fixture") }
    acct := fixture.Config.Accounts[acctKey]

    // Build a throwaway config in t.TempDir() with the real account,
    // then exercise pkg/ops against it.
}
```

### Fixture helpers

- `fixture.Config` — the parsed gitbox config from `test-gitbox.json`
- `fixture.Tokens` — map of account key → test token
- `fixture.firstAccountWithRepos() (accountKey, sourceKey, repoKey string, ok bool)` — first source (sorted) with repos whose account has a token

## Scenario test (`TestScenario_FullLifecycle`)

`pkg/ops/scenario_test.go` drives the library through the whole lifecycle as ordered subtests: add accounts → credential check → discover → add repo → clone → status → pull and fetch → update account (reconfigures clones) → mirror CRUD → re-clone → rename account → delete everything. A `persist(t)` closure saves and reloads the config after each step, the same way the GUI persists after every action, so each step also proves the config round-trips through disk.

Add a new lifecycle step as another numbered `t.Run("NN_name", ...)` in order, not as a separate scenario.

## Test isolation

- `XDG_CONFIG_HOME` → temp dir (`isolate`, or `t.Setenv` in the scenario)
- `GIT_CONFIG_GLOBAL` → temp file, `GIT_CONFIG_NOSYSTEM=1`
- `global.folder` → `<tmpDir>/git`
- `credential_ssh.ssh_folder` → `<tmpDir>/ssh` (fixture: its own isolated folder)
- Config → `<tmpDir>/gitbox/gitbox.json`; credentials → `<tmpDir>/gitbox/credentials/<accountKey>`
- Never fall through to the real `~/.config/gitbox/gitbox.json` or `~/.ssh`
- The pre-push hook scrubs `GIT_DIR` and friends before running tests; tests that shell out to git must not depend on them either
- AI harness detection is stubbed: `cmd/gui` tests swap `harnessLookupFn` via `stubHarnessLookup(t, map)` and `pkg/harness.Sync` tests use a map-backed `fakeLookup` — never let a test probe the real host for harness binaries.

## Naming conventions

- Unit tests: `TestXxx_Yyy` (e.g., `TestRenameAccount_MigratesEverything`)
- Integration: `TestIntegration_Xxx`
- Scenario: `TestScenario_FullLifecycle`
