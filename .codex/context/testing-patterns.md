---
read-when: Before running, writing or changing tests; it holds the per-package test table, test levels, helpers and naming.
---

# Test authoring patterns

How to write new tests for gitbox. Reference this when adding or modifying tests.

Application business logic lives in `pkg/`, so its tests live there too: plain `go test` packages with no UI driver. `cmd/gui` keeps a few tests for the Wails-bound `App` methods.

## Which tests to run

After any code change, always run the relevant tests:

| What changed                                | Command                                            |
| ------------------------------------------- | -------------------------------------------------- |
| `pkg/` (shared library)                     | `go test ./pkg/...`                                |
| Account/credential/clone flows (`pkg/ops/`) | `go test ./pkg/ops/`                               |
| Credential logic (`pkg/credential/`)        | `go test ./pkg/credential/ ./pkg/ops/ ./cmd/gui/`  |
| Config logic (`pkg/config/`)                | `go test ./pkg/config/ ./pkg/ops/ ./cmd/gui/`      |
| Disk reconcile (`pkg/reconcile/`)           | `go test ./pkg/reconcile/ ./pkg/adopt/ ./cmd/gui/` |
| GUI bindings (`cmd/gui/`)                   | `go test ./cmd/gui/`                               |
| Update logic (`pkg/update/`)                | `go test ./pkg/update/`                            |
| Doctor / tool detection (`pkg/doctor/`)     | `go test ./pkg/doctor/`                            |
| Unsure what's affected                      | `go test ./...`                                    |

`cmd/gui` tests (and `./...`) need `cmd/gui/frontend/dist` — build the frontend once if it is missing.

## Harness checks

For changes limited to `ai/` and its rendered trees, I run `uv run ai/scripts/test_sync.py`, `uv run ai/scripts/test_fixing_markdown.py`, `uv run ai/scripts/sync.py verify` and `./scripts/health.sh harness markdown shellcheck actionlint`. These changes do not require Go tests or a GUI build; the full health check still runs before a PR. I test formatter selection against temporary fixtures, including rendered targets and symlinks, without formatting the real repository. The harness health gate runs both Python suites. On POSIX hosts, the formatter suite runs real pinned tools through `pnpm dlx` with global formatters absent from `PATH`, checks harness markers, and compares complete fixture snapshots for unexpected dependency files or check-mode writes. It also runs the Markdown health gate without a frontend build to check clean input, lint findings and malformed configuration. The isolated PATH invokes bootstrap tools at their original paths so package-manager launchers can resolve relative payloads; a fixture covers that behavior. The real-tool checks need pnpm and Node and may populate their cache; Windows skips the restricted-PATH fixtures because they use POSIX shell launchers.

I also assert that the health gate and formatter pin the same markdownlint package. A separate POSIX fixture runs the real `health.sh harness` without `frontend/dist`, records its three uv calls with a stub to avoid recursive tests, and rejects any npm invocation. Its checkout snapshot must remain unchanged.

## Test levels

- **Unit tests** — always run, no credentials needed. Package-level tests in `pkg/`, isolated with `t.TempDir()` and `t.Setenv()`; `cmd/gui` tests call `App` methods directly, without the Wails runtime.
- **Integration tests** — require `test-gitbox.json` at repo root with real provider credentials, through `requireIntegration(t)`. They fail with a clear message when the file is missing and skip with `-short`. Today the only one is the scenario test, `TestScenario_FullLifecycle` in `pkg/ops`: the full account → clone → mirror → rename → delete lifecycle against a real provider. Name new ones `TestIntegration_*`.

## When adding features or fixing bugs

- Add or update tests that cover the change. Check the helpers below before writing new ones.
- `pkg/` changes: add package-level tests. Account, credential, clone and discovery flows go in `pkg/ops` (helpers: `isolate`, `makeClone`, `gitRun`).
- Flows that need real credentials or API calls: gate them with `requireIntegration(t)`, or extend `TestScenario_FullLifecycle` with a new step.
- GUI binding changes: add `cmd/gui` tests that build an `App` and call the method.

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

- `isolate(t) (root string, cfg *config.Config)` — points `XDG_CONFIG_HOME`, `GIT_CONFIG_GLOBAL` and the SSH folder at a temp dir, sets `GIT_CONFIG_NOSYSTEM=1`, returns an empty current-version config whose only root (`global.roots`) is `<root>/git`
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

## Refresh concurrency tests

I inject refresh work through `refreshHooks` in `cmd/gui` tests and use channels to coordinate overlapping callers. I assert shared active work, bounded follow-ups, event ordering, cancellation, and rejection of stale results without starting Wails. I use local HTTP servers for provider deadlines and cancellation. I isolate Git config and token files, and use `GIT_TRACE2_EVENT` to count real pre-fetch Git processes rather than mocking the config wrappers.

## Frontend tree and status tests

I test incoming history in `pkg/git/incoming_test.go` with isolated local Git repositories, including diverged branches, differently named upstreams, long lists and unchanged working files. `cmd/gui/repo_detail_test.go` checks that incoming commits and local-file details coexist and refresh after an update. `src/lib/repo-detail.test.mjs` uses deferred promises to check out-of-order loads, cancellation, refreshes and errors without Wails or a browser.

I run `npm test` from `cmd/gui/frontend` for pure clone-tree regressions, including cross-source ownership, duplicate prevention, nested collapse state, alphabetical order, and recovery from stale parent references. `src/lib/status.test.mjs` covers the status mappings and account health in `src/lib/status.ts`, `src/lib/views.test.mjs` the view resolution and Needs attention list in `src/lib/views.ts`, `src/lib/activity.test.mjs` the Fetch all and Pull all runs in `src/lib/activity.ts`, `src/lib/theme.test.mjs` the provider colours in `src/lib/theme.ts`, and `src/lib/notices.test.mjs` the result cards in `src/lib/notices.ts` (it uses `mock.timers` for auto-dismiss and re-imports the module per test for a fresh list). `src/lib/i18n.test.mjs` reads the catalogs from the `i18n.ts` syntax tree (`i18n.ts` imports `svelte/store`, so it can't load from a data URL), checks that `en` and `es` share keys and placeholders and that every literal `$t('…')` and `<Rich key>` key exists, and scans every `.svelte` template with `svelte/compiler` for hard-coded text; when it flags a string, I move the string to `i18n.ts`, or mark verbatim content with `<code>` or `translate="no"`. Each other test file transpiles its TypeScript module with `typescript` and imports it from a data URL, so the module under test must have no runtime imports. I keep these tests independent of Wails and credentials. `scripts/health.sh frontend-test` runs the same suite.

## Integration tests and the fixture gate

Integration tests run real provider operations with credentials from `test-gitbox.json` at the repo root (copy `json/test-gitbox.json.example` and fill it in). The gate is `requireIntegration(t)`:

- `go test -short` — skips every integration test
- full run without the file — fails with instructions, so a full run never passes silently without coverage
- with the file — exports each account's `_test.token` as `GITBOX_TOKEN_<KEY>`, points `GIT_SSH_COMMAND` at the fixture's isolated SSH folder, and rejects fixtures whose `ssh_folder` is `~/.ssh` or with a root in `global.roots` that is the real gitbox config dir

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
- `global.roots` → `[<tmpDir>/git]`
- `credential_ssh.ssh_folder` → `<tmpDir>/ssh` (fixture: its own isolated folder)
- Config → `<tmpDir>/gitbox/gitbox.json`; credentials → `<tmpDir>/gitbox/credentials/<accountKey>`
- Never fall through to the real `~/.config/gitbox/gitbox.json` or `~/.ssh`
- The pre-push hook scrubs `GIT_DIR` and friends before running tests; tests that shell out to git must not depend on them either
- AI harness detection is stubbed: `cmd/gui` tests swap `harnessLookupFn` via `stubHarnessLookup(t, map)` and `pkg/harness.Sync` tests use a map-backed `fakeLookup` — never let a test probe the real host for harness binaries.

## Naming conventions

- Unit tests: `TestXxx_Yyy` (e.g., `TestRenameAccount_MigratesEverything`)
- Integration: `TestIntegration_Xxx`
- Scenario: `TestScenario_FullLifecycle`
