# Testing reference

Test inventory and harness internals. For running tests, checklists, and fixture setup, see [testing.md](testing.md).

## Test inventory

Counts are top-level `func Test…` functions per package, from `grep -rc "^func Test" --include=*_test.go pkg cmd`. Subtests (`t.Run`) are not counted.

### Package tests — 433 tests (`pkg/`)

- `pkg/adopt/` — 13 tests: orphan discovery, account scoring (embedded URL user, credential username, parent folder, ambiguous ties), nested clones under containers
- `pkg/config/` — 98 tests: config parsing, v1/v2 → v3 migration, CRUD operations, save/load, backups, test-mode setup
- `pkg/credential/` — 21 tests: token resolution, validation, OS-default helpers, `Check`/`FixGlobalGCMConfig` (global gitconfig health for GCM)
- `pkg/doctor/` — 24 tests: tool table shape, install hints, lookups, per-credential-type prechecks, tool output decoding
- `pkg/git/` — 30 tests: git subprocess operations, repo and profile URLs, nested repo discovery
- `pkg/gitignore/` — 23 tests: managed block round-trip, merge with user content, idempotent install, backups, duplicate sanitising
- `pkg/harness/` — 33 tests: embedded tools directory parsing, retired tools, WezTerm `launch_menu` parsing
- `pkg/heal/` — 7 tests: expected origin URL per credential type, identity repair, stripping embedded tokens
- `pkg/i18n/` — 1 test: language normalisation
- `pkg/identity/` — 7 tests: `ResolveIdentity`, `EnsureRepoIdentity`, `CheckGlobalIdentity`
- `pkg/launch/` — 13 tests: argv expansion, shell quoting, AI harness wrapping per shell, macOS AppleScript
- `pkg/mirror/` — 6 tests: remote URL parsing, mirror discovery, status error classification
- `pkg/move/` — 5 tests: repo key parsing, clone URLs, preflight validation
- `pkg/ops/` — 15 tests: 14 isolated unit tests (add, rename, delete account; credential type change and delete; delete repo; clone planning; reconfigure clones; add discovered repos) plus the `TestScenario_FullLifecycle` scenario
- `pkg/provider/` — 43 tests: HTTP client, provider API parsing
- `pkg/status/` — 15 tests: clone status checking, branch detection, nesting computation
- `pkg/terminals/` — 51 tests: catalog shape, OS-aware Profile composition, WezTerm and Windows Terminal lookups, merge rules
- `pkg/update/` — 23 tests: semver parsing, version comparison, update check (mock API), major-version cap, artifact names, AppImage notify-only detection, install targets, checksum verification, fail-closed download (missing or unreadable checksums refuse the update)
- `pkg/workspace/` — 5 tests: workspace discovery, cache refresh, extra folders, tentative containers

### GUI tests — 42 tests (`cmd/gui/`)

Go-side logic of the Wails app that runs without a window:

- Account and browser actions — account folder resolution, provider URLs, error paths for unknown accounts and repos
- AI harness actions — detection, ordering, dedup, retired-harness pruning, `~/.local/bin` fallback, launcher default Profile
- Self-update — the AppImage build refuses `ApplyUpdate` before any download
- Terminals — argv resolution, legacy entry upgrades, Windows Terminal profile parsing and merge, MSYS path and env sanitising
- Workspaces and containers — cache refresh, container flag persistence, extra folders, nested scan depth, absolute `clone_folder` for onboarded clones

### Scenario test — 1 test, 12 steps (`pkg/ops/`)

- `TestScenario_FullLifecycle` — end-to-end through `pkg/ops`: add account → credential check → discover → add repo → clone → status → pull and fetch → account edit + reconfigure clones → mirror CRUD → re-clone → rename account → delete everything

### Total: 475 tests

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
