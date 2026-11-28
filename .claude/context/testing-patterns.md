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
| Path identity (`pkg/pathkey/`)              | `go test ./pkg/pathkey/ ./pkg/config/`             |
| Config logic (`pkg/config/`)                | `go test ./pkg/config/ ./pkg/ops/ ./cmd/gui/`      |
| Fleet recipes (`pkg/recipe/`)               | `go test ./pkg/recipe/ ./cmd/gui/`                 |
| Diagnostics export (`pkg/diag/`)            | `go test -short ./pkg/diag/ ./cmd/gui/`            |
| Disk reconcile (`pkg/reconcile/`)           | `go test ./pkg/reconcile/ ./pkg/adopt/ ./cmd/gui/` |
| GUI bindings (`cmd/gui/`)                   | `go test ./cmd/gui/`                               |
| Svelte components                           | `npm run test:dom` in `cmd/gui/frontend`           |
| Update logic (`pkg/update/`)                | `go test ./pkg/update/`                            |
| Doctor / tool detection (`pkg/doctor/`)     | `go test ./pkg/doctor/`                            |
| Server protocol (`pkg/rpc/`)                | `go test -race ./pkg/rpc/ ./cmd/gui/`              |
| Embedded server (`cmd/gui/serverbin/`)      | `go test ./cmd/gui/...`                            |
| SSH bootstrap (`pkg/remote/`)               | `go test -race ./pkg/remote/ ./cmd/gui/`           |
| Server-side code (`gitboxserver` files)     | `go test -race -tags gitboxserver ./cmd/gui/`      |
| Unsure what's affected                      | `go test ./...`                                    |

`cmd/gui` tests (and `./...`) need `cmd/gui/frontend/dist` — build the frontend once if it is missing.

The embedded binaries themselves only exist in a `serverbin` build. After `scripts/build-server.sh`, `go test -tags serverbin ./cmd/gui/serverbin/` reads each one back against its pin; the gate lanes run it.

## Remote mode tests

- `pkg/remote` unit tests run everywhere. `fakessh_test.go` points `remote.SetSSHPath` at a fake `ssh` that runs the real remote scripts with the local `sh` in a temp home (POSIX only); `export_test.go` holds the only hooks that add `-F` or wrap the upload, and `TestNoConfigOverrideInProduction` checks the production argv never does.
- The remote launches (#243) are tested with real interpreters, never by comparing strings alone: `TestRemoteTerminalArgv` runs each hostile folder through every shell the host has (`<shell> -c <command>`, as sshd does) with a small `sh` script as the program that prints `$PWD`, and checks for a marker file. A race-built test binary as the helper process is too slow for hundreds of runs; the Windows file uses it only for cmd and PowerShell. In `cmd/gui`, `recordRemoteSpawn(t)` replaces `remoteSpawn`, the one exit of every remote launch, and `attachSession(t, a)` gives an App from `startHostile` a session of `my-server`.
- Files tagged `gitboxserver` (marker, server log) are tested with `go test -race -short -tags gitboxserver ./cmd/gui/`. A `cmd/gui` test that calls the window shell (`startup`, `beforeClose`, the classification and DTO guards) carries `//go:build !gitboxserver`.
- `TestRemoteLifecycle` (`remote_test.go`) builds a remote window's `App` with `remoteApp(t, dial)` and `pipeServer(t, path)`, a dialer that serves a real server over in-memory pipes. Swap `a.remote.dial` for failures and `a.checkAlias` for `ssh -G`.
- Remote mode (#240, #294, #295) has its own seams. `TestHostileServer` (`hostile_test.go`) starts a fake `rpc.Server` with hostile handlers through `startHostile(t, handlers)`; `recordBrowser(t)` replaces `openBrowser`, the funnel's only exit, and `trapPath(t)` puts recording fakes of every launcher first on `PATH`. `TestRemoteWindowReadOnly` checks that a remote window's unserved mutations never reach `answerEverything`'s wire. A method added to `servedMethods` also needs a row in `contractRows` (a read), `mutationRows` (a config mutation) or `repoMutationRows` (a repository mutation, run against two hosts built by `repoHost`), each mutation with its line in `mutationIdempotence`, and a line in `readOnlyCalls` or `mutationCalls` of the sshd tests; `TestIntegrationCallsEveryServedMethod` and `TestServedMutationsHaveIdempotence` fail otherwise. In the frontend every `use:ro` names the bridge calls its control ends in; `ro-names.test.mjs` fails otherwise. `TestBackendContract` answers provider requests with `fakeProvider` and leaves `*:progress` events out, since the wire collapses them.
- The sshd integration tests (`pkg/remote/integration_test.go`, tag `sshd_integration`) run only through `scripts/remote-it.sh` on Linux with Docker: one fresh `gitbox-sshd` container per test on an `--internal` network, the server built with `e2e_sentinel` (its `ran.log` proves a file never ran). `ctOpts{netAdmin, gitDaemon}` gives a container `tc netem` and the 60 MB fixture on `git://127.0.0.1:9418` for the fault tests; `TestCancelReachesBackgroundWork` in `cmd/gui` covers the cancel itself everywhere else, with a shell transport that hangs as a real child of git. The gate runs them in its `remote-it` lane; this Mac has no Docker, so I run them on the `.env` Linux host from a scratch clone. `TestTokenCanary` (`canary_integration_test.go`) copies `test/fakeforge` into its container: a fake Gitea and git over HTTP behind a tokens file, so the token is on no command line the `ps` sampler watches.
- The daemon of #247 has its own seams. In `cmd/gui`, `newDaemonHost(t)` (`server_daemon_test.go`, Linux and macOS) gives a build folder, its state folder and a socket path under `/tmp`, short enough for macOS; `startDaemon` runs `runDaemon` in process, and `daemonConfig.prepare` reaches its `App` and adds handlers; `runProxied` runs the real `runProxy` over pipes; `env.enterErr` forces the fallback to an in-process session. No test changes the working folder: only `proxyMain` and `daemonMain` do. `remote_daemon_test.go` puts `remoteApp` on that daemon through `daemonDialer`, whose `cut()` breaks both pipes as a killed ssh does, and shortens `remoteSession.policy`; a cut during Fetch all goes through `App.RunTask`, the top bar's path. `frozenLink` stops a link without closing it, and its test runs in `testing/synctest` so the heartbeat's 10-second pings pass in virtual time. In `pkg/remote`, the test binary doubles as a daemon with `GBX_FAKE_DAEMON` (`daemon_unix_test.go`), copied as `gitbox-server` when a host script must recognize it with `ps`. In the sshd lane, `daemon_integration_test.go` turns `Options.Daemon` on with `c.daemonClient`; every other test keeps `serve --stdio`, the path of a session without the daemon. `pgrepF` keeps `pgrep -f` from matching the shell that runs it, and the e2e build accepts the one-minute fleet interval through `testFleetInterval`.
- The real-host lane (#245, tag `remote_host`) runs the same files against a disposable account through `scripts/remote-host-it.sh` (`SSH_REMOTE_MAC_HOST`, `SSH_REMOTE_MAC_KNOWN_HOSTS` in `.env`). The harnesses define `container` twice: `itdocker_test.go` (`sshd_integration`) and `ithost_test.go` (`remote_host && !sshd_integration`). A test runs on a real host only when it is in `hostTests` and its `ctOpts` need no container; port it with the portable helpers (`c.mode`, `c.age`, `c.sha256`, `q`, `c.home()`, `c.scratch()`, `s.root`), never with `stat -c`, `touch -d`, `sha256sum`, `/tmp` or `testHome`. The harness refuses an account without `~/.gitbox-it-host` or the one the tests run from; never point it at a real account. On macOS, `ps -axww` as a plain user lists every user's processes.
- Secrets (#242) have two canaries. The structural one, `internal/secretcheck`, walks every output DTO of `cmd/gui` and the wire types of `pkg/rpc`: a field named like a token fails until it is allowlisted with its exact type and JSON name. The value one, `TestTokenCanary` in `cmd/gui` and in `pkg/remote`, stores a random `gbx-canary-<hex>` and asserts it is everywhere absent but in the one request and the store file, with a positive control for each tap. A canary that passes must also fail with the leak put back (I checked it against the old token URL of a mirror push). A test that stores a token, or repairs a clone, sets `XDG_CONFIG_HOME` to a temp dir: `contractConfig` does it, so the credential files of the contract tests never reach the real home. SSH keys (#244) need no marker, because no parameter carries one: `TestPrivateKeyNeverOnWire`, in `cmd/gui` and in `pkg/remote`, taps both directions and both logs through setup, verify, agent report and regenerate, finds no line of either private key, and finds the public keys as its positive control. The `cmd/gui` `TestMain` points `HOME` and `USERPROFILE` at a temp folder, keeping the Go caches, because an SSH setup on a config without `credential_ssh.ssh_folder` writes into `~/.ssh`; in `pkg/ops` and `pkg/credential`, give the config a temp SSH folder.
- `remote_smoke_test.go` (tag `remote_smoke`) drives the client methods against a real host; it needs `scripts/build-server.sh` and `-ldflags "-X github.com/LuisPalacios/gitbox/cmd/gui.version=$VERSION -X github.com/LuisPalacios/gitbox/cmd/gui.commit=$COMMIT"`, because a test binary takes the stamp under the import path.

## Harness checks

For changes limited to `ai/` and its rendered trees, I run `uv run ai/scripts/test_sync.py`, `uv run ai/scripts/test_fixing_markdown.py`, `uv run ai/scripts/test_coordinate_issues.py`, `uv run ai/scripts/test_coordinate_roadmap.py`, `uv run ai/scripts/test_work_issue_access.py`, `uv run ai/scripts/sync.py verify` and `./scripts/health.sh harness markdown shellcheck actionlint`. These changes do not require Go tests or a GUI build; the full health check still runs before a PR. I test formatter selection against temporary fixtures, including rendered targets and symlinks, without formatting the real repository. The harness health gate runs all five Python suites. On POSIX hosts, the formatter suite runs real pinned tools through `pnpm dlx` with global formatters absent from `PATH`, checks harness markers, and compares complete fixture snapshots for unexpected dependency files or check-mode writes. It also runs the Markdown health gate without a frontend build to check clean input, lint findings and malformed configuration. The isolated PATH invokes bootstrap tools at their original paths so package-manager launchers can resolve relative payloads; a fixture covers that behavior. The real-tool checks need pnpm and Node and may populate their cache; Windows skips the restricted-PATH fixtures because they use POSIX shell launchers.

I also assert that the health gate and formatter pin the same markdownlint package. A separate POSIX fixture runs the real `health.sh harness` without `frontend/dist`, records its five uv calls with a stub to avoid recursive tests, and rejects any npm invocation. Its checkout snapshot must remain unchanged.

I test the coordination registry with temporary Git repositories and separate processes: competing claims, required work-unit scope, same-container exclusion, independent-container concurrency, legacy registry preservation, all-or-none batches, recovery fencing, resource exclusion and evidence invalidation. I simulate a finite queue with 15 and four worker slots, refill while peers run, saturation, waiting for integration, recovery without duplicates and the original three-issue batch. I separately test minimal optional work-unit checkbox edits, idempotency, ambiguous references and unrelated text preservation. The tests need no GitHub credentials or live agents. The local gate's macOS and Windows lanes run both coordination suites, and the release workflow runs them on all three platforms; I report native results only after one of them runs.

## Bootstrap verification tests

I run `bash scripts/test-bootstrap.sh` or `./scripts/health.sh bootstrap` for installer changes. The fixtures sign local checksums with a throwaway SSH key, check parity with the updater's pinned public key, and reject wrong keys, modified archives/checksums, replayed tags, missing signatures and ambiguous checksum entries. They prove verification precedes extraction, exercise both download paths with local stubs, and check bundled desktop registration and quarantine policy without installing software or touching credentials. The local gate runs the same fixtures on macOS through its health check and directly in the Linux and Windows lanes.

## Health check argument tests

I run `bash scripts/test-health-args.sh` or `./scripts/health.sh selftest` after changing the argument handling or the check list of `health.sh`. It expects exit code 2 with the valid names for an unknown check or option and for `fuzz` without `HEALTH_FUZZ=1`, exit 0 for `-h`/`--help`, and no started check in any case. It also keeps `CHECKS`, the dispatched checks and the help text in step. When I add a check, I add its name to `CHECKS`, its `want` line and the header comment together. The suite also checks that `failures_first` puts a failed `go test`'s `--- FAIL` lines and messages ahead of stderr noise (#346). `HEALTH_SH=<copy>` runs the suite against another version of the script as a negative control.

## Git hook tests

I run `bash scripts/test-githooks.sh` or `./scripts/health.sh selftest` after changing `.githooks/`. It checks the patterns of `.githooks/ai-attribution.sh` (AI co-author trailers, session trailers and "generated with" footers flagged; human co-authors and plain tool mentions allowed; the whole `HEAD` history clean), then runs `commit-msg` and `pre-push` in throwaway repositories with an isolated `HOME`. `HOOKS_DIR=<copy>` runs it against another copy of the hooks as a negative control.

## Test levels

- **Unit tests** — always run, no credentials needed. Package-level tests in `pkg/`, isolated with `t.TempDir()` and `t.Setenv()`; `cmd/gui` tests call `App` methods directly, without the Wails runtime.
- **Integration tests** — require `test-gitbox.json` at repo root with real provider credentials, through `requireIntegration(t)`. They fail with a clear message when the file is missing and skip with `-short`. Today the only one is the scenario test, `TestScenario_FullLifecycle` in `pkg/ops`: the full account → clone → mirror → rename → delete lifecycle against a real provider. Name new ones `TestIntegration_*`.

## Fuzz tests

I keep Go fuzz targets in `pkg/<package>/fuzz_test.go` and small checked-in corpora in `pkg/<package>/testdata/fuzz/<target>/`. Ordinary tests run the seeds; `HEALTH_FUZZ=1 ./scripts/health.sh fuzz` generates mutations for a fixed number of executions per target, about 10 seconds each, with two workers, all four targets at the same time; an exec count instead of `-fuzztime=10s` avoids the spurious `context deadline exceeded` of Go before 1.26.12 and 1.27.5 (#409). The Linux PR health job opts in. A longer pass selects exactly one target, for example `go test ./pkg/config/ -run='^$' -fuzz='^FuzzDecodeRoundTrip$' -fuzztime=1m -parallel=2`.

`FuzzDecodeRoundTrip` uses external `config_test` to compare repository intent through `status.RepoPath` without an import cycle, and checks migration to `CurrentVersion`. `FuzzStripJSONC` checks no panic, no growth and exact preservation of valid JSON. `FuzzDecodeFrame` in `pkg/rpc` checks that the frame reader never panics or returns a body over its cap, and that an accepted frame survives a write and a second read. I keep a minimized failure in the target's corpus after fixing the cause, retain example tests with distinct assertions, and leave the generated fuzz cache untracked.

`FuzzRepoPath` checks absolute results and lexical containment within the selected default/source/repo root for absolute roots and safe relative components. The relative-path domain allows empty and nested paths but excludes parent segments, backslashes, NUL and leading tildes. Separate branches check absolute clone overrides and one leading `../` followed by a safe relative path. Native temporary-root seeds keep the property active on Windows and POSIX. This is a test boundary, not config validation or a symlink guarantee; arbitrary accepted configs can still escape roots or resolve relatively.

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

### Replacing git and HTTP in a test

- `git.SetRunnerForTest(t, runner)` replaces the runner that starts every git process, and restores it when the test ends. A runner implements `Run(ctx, git.Request) (git.Result, error)`. Read the exit status from `Result.ExitCode` or `GitError.ExitCode`, never from the error's type. `pkg/git/runner_test.go` has a scripted `fakeRunner` and `TestRunnerContract`, the table that both the real runner and the fake must pass; extend the table when the fake learns something new.
- `provider.SetDoerForTest(t, doer)` replaces the client that sends every provider API request.
- Both setters change package state: a test that uses one must not call `t.Parallel()`.
- File operations with failure tests are passed in instead: `saveWithFiles` in `pkg/config` and `replaceOps` in `pkg/update`. Follow that style for new ones.

## GUI binding tests (`cmd/gui/`)

`TestBoundSurface` compares every exported `*App` method and both generated Wails binding files with the documented inventory in `cmd/gui/testdata/bound_methods.txt`. Update the relevant group and give each new renderer binding a one-line purpose. Regenerate the bindings with `wails build` in `cmd/gui/`.

Build the `App` struct directly and call the bound method. No Wails runtime, no window:

```go
func TestSomething(t *testing.T) {
    dir := t.TempDir()
    cfg := &config.Config{Version: config.CurrentVersion, Global: config.GlobalConfig{Folder: dir}, ...}
    a := &App{cfg: cfg, cfgPath: filepath.Join(dir, "gitbox.json"), mu: sync.Mutex{}}

    a.syncEditors()

    // Assert on a.cfg and on the config file at a.cfgPath.
}
```

For a test that needs the backend and the client apart, `newTestApp(backend, localPath)` (`prefs_test.go`) builds an `App` whose client preferences live in their own gitbox.json, `fileBackend(t, path)` gives it a backend that owns a different file, and `seedConfig` writes either one. Set `a.windowGeom` to run `beforeClose` without a Wails runtime.

For a test that needs a `gitbox-server` and its client joined, `serveRemote(t, path, extra)` (`backend_contract_test.go`) starts the server on a config and connects a `RemoteBackend` to it over in-memory pipes; `extra` adds handlers only that test needs, and `link.Sent` holds every byte the server wrote. A method added to `servedMethods` needs a row in the table of `TestBackendContract`, which runs it against the local backend and the remote one and fails when it is missing. In `pkg/rpc`, `connect(t, newServer(handlers))` (`harness_test.go`) does the same one level down.

Keep business logic out of `cmd/gui`: if a test needs more than the `App` glue, the logic belongs in `pkg/` and the test goes with it.

## Refresh concurrency tests

I inject refresh work through `refreshHooks` in `cmd/gui` tests and use channels to coordinate overlapping callers. I assert shared active work, bounded follow-ups, event ordering, cancellation, and rejection of stale results without starting Wails. I use local HTTP servers for provider deadlines and cancellation. I isolate Git config and token files, and use `GIT_TRACE2_EVENT` to count real pre-fetch Git processes rather than mocking the config wrappers.

For deterministic overlaps, `pkg/git/testutil` provides a scripted runner adapter and a channel barrier that announces entry. I use it in `cmd/gui/schedules_test.go` to pause a scan, clone or fetch while a binding changes the config, then check the persisted state. For real Git credential waits, `pkg/git/credential_schedule_test.go` uses a temporary bare repository, loopback HTTP and a helper that signals entry before blocking. I use file-operation seams in `pkg/config/save_fault_test.go` to inject errors after writes, sync and rename. On macOS and Linux the default health check runs `go test -race -short ./cmd/gui/ ./pkg/config/ ./pkg/git/...` at the same time as `go test -short` over every other package; the race pass replaces the plain run of those packages. Windows keeps one plain pass over everything.

## Frontend tree and status tests

I run `go generate ./cmd/gui` after changing an event DTO. `TestEventTypesGenerated` checks the committed TypeScript definitions, `TestCloneCompletionPublishesDirtyWorkingTree` exercises a real post-checkout hook, and `events.test.mjs` covers stale events, reconnects and correlated status results.

I test incoming history in `pkg/git/incoming_test.go` with isolated local Git repositories, including diverged branches, differently named upstreams, long lists and unchanged working files. `cmd/gui/repo_detail_test.go` checks that incoming commits and local-file details coexist and refresh after an update. `src/lib/repo-detail.test.mjs` uses deferred promises to check out-of-order loads, cancellation, refreshes and errors without Wails or a browser.

I run `npm test` from `cmd/gui/frontend` for pure clone-tree regressions, including cross-source ownership, duplicate prevention, nested collapse state, alphabetical order, and recovery from stale parent references. `src/lib/status.test.mjs` covers the status glyphs, palette and account health in `src/lib/status.ts`, `src/lib/presentation.test.mjs` checks `src/lib/presentation.ts` across all statuses, branch identities and display modes, `src/lib/views.test.mjs` the view resolution and Needs attention list in `src/lib/views.ts`, `src/lib/activity.test.mjs` the Fetch all and Pull all runs in `src/lib/activity.ts`, `src/lib/theme.test.mjs` the provider colours in `src/lib/theme.ts`, and `src/lib/notices.test.mjs` the result cards in `src/lib/notices.ts` (it uses `mock.timers` for auto-dismiss and re-imports the module per test for a fresh list). `src/lib/css-colors.test.mjs` fails on any `color-mix()` in the frontend sources, comments aside, and recomputes each mixed token of `style.css` in both themes; a new mixed token needs its line in `RECIPES`. `src/lib/i18n.test.mjs` reads the catalogs from the `i18n.ts` syntax tree, checks that `en` and `es` share keys and placeholders and that every literal `$t('…')` and `<Rich key>` key exists, and scans every `.svelte` template with `svelte/compiler` for hard-coded text; when it flags a string, I move the string to `i18n.ts`, or mark verbatim content with `<code>` or `translate="no"`. To run the translator itself, it rewrites the `svelte/store` import of the transpiled `i18n.ts` to `import.meta.resolve('svelte/store')` before it loads the data URL; that is how it checks that a remote window reads each `<key>.remote` variant with `{host}` filled in. Each other test file transpiles its TypeScript module with `typescript` and imports it from a data URL, so the module under test must have no runtime imports. I keep these tests independent of Wails and credentials. `scripts/health.sh frontend-test` runs the same suite.

## Frontend component tests

I add `*.dom.test.mjs` under `cmd/gui/frontend/src/lib/components/` for behavior that needs real mounted Svelte components. `npm test` runs both the pure Node and DOM suites. I use `testing/mount.mjs` with Node's test runner, jsdom and Svelte Testing Library. I use `testing/fakeBridge.ts` to script generated Wails binding replies and emit typed envelopes while keeping the real bridge and event router active. I use fake accounts and tokens, resolve in-flight calls explicitly, and assert on the rendered UI and cleanup. The row-width test uses the mounted text, narrow-content CSS and a monospace estimate since jsdom supplies no layout. I use the packaged Wails smoke and the native webview for platform behavior and actual rendering. `src/lib/session/controller.test.mjs` bundles the SessionController with vite, as `mount.mjs` does, and drives its handlers through the real event router and a fake bridge without a DOM.

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
