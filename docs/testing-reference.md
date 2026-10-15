# Testing reference

Test inventory and harness internals. For running tests, checklists, and fixture setup, see [testing.md](testing.md).

## Test inventory

Counts are top-level `func Test…` functions per package, from `grep -rc "^func Test" --include=*_test.go pkg cmd`. Subtests (`t.Run`) are not counted.

### Package tests — 466 tests (`pkg/`)

- `pkg/adopt/` — 14 tests: orphan discovery, account scoring (embedded URL user, credential username, parent folder, ambiguous ties), nested clones under containers
- `pkg/config/` — 105 tests: config parsing, v1/v2 → v3 migration, CRUD operations, save/load, backups, test-mode setup, layout-state pruning (folds, sidebar, the view of a deleted account), a rename that keeps its view, and backup skip for layout and theme saves
- `pkg/credential/` — 32 tests: token resolution, validation, OS-default helpers, `Check`/`FixGlobalGCMConfig` (global gitconfig health for GCM), cancellation, compare-first maintenance, atomic token-file updates
- `pkg/doctor/` — 24 tests: tool table shape, install hints, lookups, per-credential-type prechecks, tool output decoding
- `pkg/git/` — 34 tests: git subprocess operations, repo and profile URLs, nested repo discovery, cancellable fetches, connects refused within milliseconds, config snapshots with includes and linked worktrees
- `pkg/gitignore/` — 23 tests: managed block round-trip, merge with user content, idempotent install, backups, duplicate sanitising
- `pkg/harness/` — 28 tests: embedded tools directory parsing, retired tools, WezTerm `launch_menu` parsing
- `pkg/heal/` — 11 tests: expected origin URL per credential type, identity repair, stripping embedded tokens, two-process unchanged maintenance, shadowed repairs, linked worktrees
- `pkg/i18n/` — 1 test: language normalisation
- `pkg/identity/` — 4 tests: `ResolveIdentity`, `CheckGlobalIdentity`
- `pkg/launch/` — 13 tests: argv expansion, shell quoting, AI harness wrapping per shell, macOS AppleScript
- `pkg/mirror/` — 6 tests: remote URL parsing, mirror discovery, status error classification
- `pkg/move/` — 5 tests: repo key parsing, clone URLs, preflight validation
- `pkg/ops/` — 17 tests: 16 isolated unit tests (add, rename, delete account; suggest an account key; discard a just-added account; credential type change and delete; delete repo; clone planning; reconfigure clones; add discovered repos) plus the `TestScenario_FullLifecycle` scenario
- `pkg/provider/` — 43 tests: HTTP client, provider API parsing
- `pkg/status/` — 16 tests: clone status checking, branch detection, nesting computation (including container inside container)
- `pkg/terminals/` — 55 tests: catalog shape, OS-aware Profile composition, WezTerm and Windows Terminal lookups, merge rules
- `pkg/update/` — 28 tests: semver parsing, version comparison, update check (mock API, a pending update reported on every check), major-version cap, artifact names, AppImage notify-only detection, install targets, relaunch command per platform and waiting for the old process to exit (plus the elevated Windows script, on Windows), checksum verification, release signature verification against signatures made by real `ssh-keygen` (wrong tag, tampered checksums, wrong namespace, foreign key, malformed), `allowed_signers` in sync with the embedded key, fail-closed download (missing, unreadable or invalid checksums or signature refuse the update, and a bad signature stops it before the artifact download)
- `pkg/workspace/` — 7 tests: workspace discovery, cache refresh, extra folders, tentative containers

### GUI tests — 70 tests (`cmd/gui/`)

Go-side logic of the Wails app that runs without a window:

- Account and browser actions — account folder resolution, provider URLs, error paths for unknown accounts and repos
- AI harness actions — detection, ordering, dedup, retired-harness pruning, `~/.local/bin` fallback, launcher default Profile
- Experience mode and theme — `ui_mode` defaults to Advanced, set/get validation, and onboarding writes Simple only on a new install; `SetTheme` validates and persists the theme
- Self-update — the AppImage build refuses `ApplyUpdate` before any download
- Terminals — MSYS path and env sanitising for spawned terminals, and a first launch that never writes the legacy `global.terminals` list
- Workspaces and containers — cache refresh, container flag persistence, extra folders, nested scan depth, absolute `clone_folder` for onboarded clones, layout-state persistence and pruning (folds, sidebar and view)

I cover startup snapshots and nonblocking workspace discovery in `cmd/gui/startup_test.go` and `cmd/gui/workspaces_test.go`, including stale scan inputs and concurrent config edits. `BenchmarkStartupSnapshot` measures snapshot construction separately from native rendering. I run pure frontend regressions with `npm test` in `cmd/gui/frontend`: the clone tree and problems-first order (`src/lib/repo-tree.test.mjs`), status mappings, glossary labels, attention ranks and account health (`src/lib/status.test.mjs`), view resolution and the Needs attention list and counts (`src/lib/views.test.mjs`), Fetch all and Pull all runs and summaries (`src/lib/activity.test.mjs`), and the result cards (`src/lib/notices.test.mjs`); `scripts/health.sh frontend-test` runs the same suite.

I cover refresh coordination and upstream probes without a Wails window, including overlapping triggers, bounded workers, cancellation, and stale config or credential results. Credential context tests use local provider servers and a stalled helper. Config snapshot and maintenance regressions use isolated Git config, include files, linked worktrees, and Trace2 process counts; token-file checks cover unchanged modification times, concurrent readers, rotation, permissions, and account separation.

### Scenario test — 1 test, 12 steps (`pkg/ops/`)

- `TestScenario_FullLifecycle` — end-to-end through `pkg/ops`: add account → credential check → discover → add repo → clone → status → pull and fetch → account edit + reconfigure clones → mirror CRUD → re-clone → rename account → delete everything

### Total: 523 tests

## How the test harness works

The `_test` key inside each account is silently ignored by `config.Parse()` — Go's JSON unmarshaler skips unknown struct fields. The scenario harness (`requireIntegration` in `pkg/ops/fixture_test.go`):

1. Skips in `-short` mode, and fails with setup instructions when `test-gitbox.json` is missing
2. Parses the file as a standard gitbox config (accounts, sources, mirrors)
3. Extracts `_test` from each account via a separate raw JSON pass
4. Sets `GITBOX_TOKEN_<KEY>` env vars for accounts that have a `_test.token`
5. Refuses fixtures whose `credential_ssh.ssh_folder` is `~/.ssh` or whose `global.folder` is the real gitbox config directory
6. Points git's SSH at the fixture's isolated SSH config through `GIT_SSH_COMMAND`

The scenario itself then sets a temp `XDG_CONFIG_HOME`, builds a fresh config with `global.folder` in a temp directory, and saves and reloads that config after every step, the same way the GUI persists after each action. Clone directories are auto-cleaned by Go's `t.TempDir()` after the test.

`GitboxApp --test-mode` uses the same fixture through `config.SetupTestMode()`: it finds `test-gitbox.json` by walking up from the current directory, writes a throwaway config with `global.folder` overridden, applies the same path safety checks, and injects the fixture tokens as environment variables.
