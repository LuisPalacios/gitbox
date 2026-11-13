# Testing reference

The inventory of tests and how the test harness works. For test levels, the fixture setup and the checklists, see [testing.md](testing.md).

## Test inventory

Counts are top-level Go test functions (subtests from `t.Run` and `TestMain` are not counted) and top-level `test(...)` calls in the frontend suites. I refresh the Go counts with:

```bash
rg -c '^func Test\w*\(t \*testing\.T\)' -g '*_test.go' pkg cmd
```

The current total is 593 Go tests (516 in `pkg/`, 77 in `cmd/gui/`) and 42 frontend tests. Windows-only files (`*_windows_test.go`) are included in the counts but run only on Windows.

### Shared library (`pkg/`)

- **`pkg/adopt`** (16) — orphan discovery and account scoring.
  - `adopt_test.go`: SSH host aliases, in-place discovery, account matching by embedded URL user, credential username and parent folder, ambiguous ties.
  - `nested_test.go`: nested clones under a container, extra and explicit nested roots, in-place adoption under a second clone root, confidence that needs more than the host.
- **`pkg/config`** (112) — the config file.
  - `config_test.go`, `load_test.go`: parsing, validation, wrong versions, mirrors, round trips, `LoadWithRepair` for dangling mirrors and sources, global gitignore and PR badge defaults.
  - `migrate_v3_test.go`, `roots_test.go`, `migrate_v5_test.go`, `migrate_terminals_test.go`: migration v2 → v3 → v4 → v5 (v4 turns `global.folder` into `global.roots`, v5 drops the `workspaces` section), the nested scan depth default, legacy terminal lists.
  - `roots_test.go`: clone roots (add, make default, remove and forget, place a repo) with every clone path pinned.
  - `crud_test.go`, `clones_test.go`, `ai_harness_test.go`: account, source, repo and mirror CRUD, ignored clones, the deep copy used by unlocked scans, AI harness entries.
  - `collapsed_test.go`, `save_test.go`: layout-state pruning (folds, sidebar, the view of a deleted account, the retired `workspaces` view key), the backup ring, no backup for layout-only and theme-only saves, and a backup always kept for a file written by an older config version.
- **`pkg/credential`** (36) — token resolution from `GITBOX_TOKEN_<KEY>` and `GIT_TOKEN`, token checks with expiry and rejected tokens, OS-default GCM helper and store, global gitconfig health for GCM (`CheckGlobalGCMConfig`, `FixGlobalGCMConfig`), SSH key and config lookup, cancellation with a stalled helper, compare-first per-repo credential config, and atomic credential-file writes with concurrent readers.
- **`pkg/doctor`** (34) — the tool table and install hints, tool lookup (extra folders, `~/.local/bin`, stored paths, Windows shims), the GCM probe (asking git, install folders, reported missing), per-credential-type prechecks, UTF-16 tool output decoding. `wsl_windows_test.go` (Windows only) reads WSL distros from the registry and proves the WSL install stub is never run.
- **`pkg/git`** (34) — git subprocess operations (status, clone, branches, branch sweeps), remote and profile URLs, repo and nested repo discovery, connects refused within milliseconds, config snapshots with includes, multi-value keys and linked worktrees.
- **`pkg/gitignore`** (23) — the managed global gitignore block: round trip, merge with user content, idempotent install, an existing `core.excludesfile`, duplicate cleanup, backups.
- **`pkg/harness`** (28) — the embedded AI harness directory (parsing, retired tools), harness sync (detection, missing and reinstalled tools, user entries, ordering), WezTerm `launch_menu` parsing.
- **`pkg/heal`** (11) — expected origin URL per credential type, URL redaction, identity repair, stripping embedded tokens, two reads and no writes for an unchanged clone, shadowed identity, linked worktrees.
- **`pkg/i18n`** (1) — language normalisation.
- **`pkg/identity`** (4) — `ResolveIdentity` with account fallback and repo overrides, `CheckGlobalIdentity`.
- **`pkg/launch`** (13) — argv expansion, POSIX shell quoting, AI harness wrapping per shell (PowerShell, cmd, POSIX), macOS AppleScript.
- **`pkg/mirror`** (6) — remote URL parsing, mirror discovery, status error classification.
- **`pkg/move`** (5) — repo key parsing, HTTPS clone URLs, preflight validation.
- **`pkg/ops`** (18) — 17 isolated unit tests (add, discard, rename and delete accounts, also across two clone roots; suggest an account key; change and delete credentials; delete a repo; plan a clone; reconfigure clones; add discovered repos) plus `TestScenario_FullLifecycle`.
- **`pkg/provider`** (52) — the HTTP client, provider API parsing for every provider (repos, pagination, create, delete, mirrors, PR lists), and token expiry (GitHub header layouts with no extra request, GitLab `personal_access_tokens/self`, unknown expiry still passing).
- **`pkg/reconcile`** (8) — confident clones adopted in place, undecided ones left with their reason, a moved clone re-linked, a deleted clone kept, an offline root left alone, ignored clones skipped, adopting into a picked account, linked worktrees never adopted.
- **`pkg/status`** (18) — clone status (clean, dirty, ahead, behind, not cloned, detached), branch detection, root offline, repo paths through source and repo roots, nesting (including a container inside a container).
- **`pkg/terminals`** (57) — catalog shape, OS-aware profile composition, WezTerm and Windows Terminal lookups, launch resolution, merge and sync rules, WSL distros detected without running `wsl.exe`.
- **`pkg/update`** (28) — semver parsing and comparison, the update check (a pending update reported on every check, the major-version cap), artifact names, AppImage notify-only detection, install targets, relaunch and waiting for the old process (plus the elevated Windows script, Windows only), checksum verification, release signatures checked against fixtures made by real `ssh-keygen` (wrong tag, tampered checksums, wrong namespace, foreign key, malformed), the embedded key matching `allowed_signers`, and fail-closed downloads.
- **`pkg/workspace`** (12) — `.code-workspace` files at a clone root, one file making a container, several asking once and keeping the pick, a preferred file winning again when it comes back, a vanished file clearing the pick, placeholders for listed folders missing inside a container (never inside another clone), the clones a workspace lists, JSON with comments.

### GUI bindings (`cmd/gui/`)

- `account_actions_test.go`, `browser_actions_test.go` (4) — account folder resolution, provider URLs, error paths for unknown accounts and repos.
- `roots_test.go` (2) — add, make default and remove clone roots; account folders through the source root.
- `reconcile_test.go` (3) — a confident clone adopted and saved, a retry when the config changes during the scan, adopting into a picked account, ignoring a clone and adopting it from the ignored list.
- `containers_test.go` (8) — container flag persistence, nested scan depth, absolute `clone_folder` for onboarded clones, layout-state persistence and pruning (folds, sidebar, view), a workspace file making a container that adopts its nested clone and lists its missing folders in one pass, choosing among several workspace files.
- `uimode_test.go` (5) — `ui_mode` defaults to Advanced, set and get validation, onboarding writes Simple only on a new install, `SetTheme` validates and persists the theme.
- `harness_test.go` (15) — AI harness detection, ordering, dedup, retired-harness pruning, missing and reinstalled tools, the `~/.local/bin` fallback, the launcher default profile, error paths.
- `terminal_test.go`, `startup_sync_test.go` (3) — MSYS path and environment cleanup for spawned terminals, a first launch that never writes the legacy `global.terminals` list.
- `gcm_setup_test.go` (4) — GCM sign-in survives an unusable stderr, forces GCM's own windows over per-user `GCM_GUI_PROMPT` and `GCM_INTERACTIVE` settings, times out with a message naming those variables, and reports the helper's stderr.
- `refresh_test.go` (20) — shared refresh cycles, worker limits, token rotation, stale config, shutdown, maintenance failures, retries of instant connection refusals with back-off, the token expiry passed to the frontend.
- `probe_test.go` (10) — upstream probes: bounded concurrency, shared flights and throttling, cancellation, a shared deadline for credential lookup, stale config and rotated tokens, shutdown.
- `startup_test.go` (2 + `BenchmarkStartupSnapshot`) — nesting before clones exist, a snapshot detached from the live config.
- `update_test.go` (1) — the AppImage build refuses `ApplyUpdate` before any download.

### Frontend (`cmd/gui/frontend/src/lib/`)

`npm test` in `cmd/gui/frontend` runs these with `node --test`; `./scripts/health.sh frontend-test` runs the same suite.

- `repo-tree.test.mjs` (8) — the clone tree: cross-source ownership, no duplicate rows, nested collapse, recovery from stale parents and cycles, alphabetical order, attention groups split by account, a container's placeholder rows.
- `status.test.mjs` (12) — each status's shape, colour, spinner, Simple kind, count badges, detail-panel rule, credential rules, attention rank and glossary label; account health per mode; the token-expiry warning.
- `views.test.mjs` (5) — saved-view resolution, account views, the Needs attention list and its counts.
- `activity.test.mjs` (7) — Fetch all and Pull all scopes and summaries, connection failures grouped by host.
- `notices.test.mjs` (4) — result cards: showing, dismissing, auto-closing and capping them.
- `theme.test.mjs` (2) — provider colours of account names in both themes, with the text colour for an unknown provider.
- `i18n.test.mjs` (4) — matching English and Spanish keys and placeholders, no missing keys, no hard-coded template text, the inline markup of `src/lib/rich.ts`.

### Scenario (`pkg/ops/scenario_test.go`)

`TestScenario_FullLifecycle` runs 12 subtests in order:

1. `01_add_accounts`
2. `02_credential_check`
3. `03_discover`
4. `04_add_repo`
5. `05_clone`
6. `06_status`
7. `07_pull_and_fetch`
8. `08_update_account_reconfigures_clones`
9. `09_mirror_crud`
10. `10_reclone`
11. `11_rename_account`
12. `12_delete_everything`

## How the test harness works

### Unit-test helpers in `pkg/ops`

- `isolate` (`ops_test.go`) points `XDG_CONFIG_HOME`, `GIT_CONFIG_GLOBAL` and the SSH folder at a temp directory, sets `GIT_CONFIG_NOSYSTEM`, clears `GIT_TOKEN`, and returns a fresh config whose only root is in that directory.
- `makeClone` creates a git repo at the planned clone path of a source repo, with a given origin.
- `gitRun` runs git in a folder and fails the test on error.

### Fixture loading

`requireIntegration` in `pkg/ops/fixture_test.go` loads `test-gitbox.json` for the scenario test:

1. It skips in `-short` mode and fails with setup instructions when the file is missing.
2. It parses the file with `config.Parse`, which ignores the unknown `_test` keys.
3. It reads `_test.token` from each account in a separate raw JSON pass.
4. It sets `GITBOX_TOKEN_<KEY>` for every account with a token.
5. `rejectRealPaths` refuses a `credential_ssh.ssh_folder` of `~/.ssh` and any root in `global.roots` that is the gitbox config directory.
6. It points git's SSH at the fixture's SSH config through `GIT_SSH_COMMAND`.

The scenario then sets a temp `XDG_CONFIG_HOME`, builds a fresh config whose only root is a temp directory, and saves and reloads the config after every step, the way the GUI persists after each action. `firstAccountWithRepos` picks the account: the first source, in key order, with repos and a token. `t.TempDir()` removes the clones afterwards.

### Test mode

`GitboxApp --test-mode` goes through `config.SetupTestMode()` in `pkg/config/testmode.go`. It finds `test-gitbox.json` by walking up from the current directory, applies the same path checks, writes a throwaway config with `global.roots` replaced by one temp directory (clearing every `source.root` and `repo.root`), sets `GIT_SSH_COMMAND`, and injects the fixture tokens as environment variables. The GUI removes the temp directory on exit.

### Concurrency and process tests

- Refresh and probe tests in `cmd/gui` block injected work on channels to overlap triggers without a Wails runtime, and use local HTTP servers to check bounded concurrency, shared probes, deadlines, and upstream state kept after errors or credential rotation.
- Credential context tests use local provider servers and a stalled helper script to prove cancellation.
- Maintenance tests in `pkg/git`, `pkg/heal` and `pkg/credential` use isolated git config, include files and linked worktrees, and count git processes with `GIT_TRACE2_EVENT`: an unchanged SSH, token or GCM clone spawns two read-only git commands. Credential-file tests check unchanged modification times, concurrent readers, rotation, permissions and account separation.
- `TestMain` in `pkg/terminals/detect_test.go` and `pkg/doctor/wsl_windows_test.go` turns the test binary into a fake `wsl.exe` that writes a sentinel file (`GITBOX_TEST_FAKE_WSL_SENTINEL`) when run, so the tests prove WSL detection never executes it.
- `.githooks/pre-push` unsets `GIT_DIR`, `GIT_WORK_TREE` and the other variables git exports to hooks, so the tests' own git calls target their temp repos instead of this repository.

## Refresh maintenance baseline

I measured pre-fetch maintenance before (`da83922`) and after the compare-first rewrite on the same isolated fixture: 24 local clones, one account, no network fetches, one warm sequential sweep per platform, credential type and state. In the drifted cases I changed both identity fields, origin and helper before each sweep. The harness ran on Go 1.26.1; Windows used Git 2.53.0.windows.1.

Elapsed times cover the whole maintenance loop in the parent process, including process creation, git execution, file operations and tracing overhead. Process counts come from Trace2 start events. These runs don't separate git-internal time, measure GUI paint, or estimate a full eight-worker network refresh.

| Platform | Clone state | Credential | Git processes before → after | Elapsed seconds before → after |
| --- | --- | --- | --- | --- |
| macOS arm64 | unchanged | SSH | 216 → 48 | 0.902 → 0.191 |
| macOS arm64 | unchanged | Token | 240 → 48 | 1.013 → 0.202 |
| macOS arm64 | unchanged | GCM | 192 → 48 | 0.923 → 0.278 |
| Windows amd64 | unchanged | SSH | 216 → 48 | 9.831 → 2.037 |
| Windows amd64 | unchanged | Token | 240 → 48 | 12.396 → 2.382 |
| Windows amd64 | unchanged | GCM | 192 → 48 | 9.386 → 2.202 |
| macOS arm64 | drifted | SSH | 288 → 264 | 1.311 → 1.216 |
| macOS arm64 | drifted | Token | 312 → 288 | 1.457 → 1.319 |
| macOS arm64 | drifted | GCM | 264 → 288 | 1.222 → 1.402 |
| Windows amd64 | drifted | SSH | 288 → 264 | 12.774 → 11.085 |
| Windows amd64 | drifted | Token | 312 → 288 | 13.576 → 12.089 |
| Windows amd64 | drifted | GCM | 264 → 288 | 11.105 → 13.073 |

GCM drift repair validates the effective config after writing, so its drifted case costs more than before. I keep that check so a fetch never runs with settings an include or worktree config overrides.

- **Git-internal time** — a separate Windows sweep of 24 unchanged SSH clones took 8.918 → 1.963 s in the parent process and 5.421 → 1.193 s of git-internal time (summed from top-level Trace2 `exit.t_abs`). The 3.497 → 0.770 s residual covers process startup, IPC and caller file operations; I don't attribute it to antivirus or `HideWindow` alone.
- **Probe scheduling** — native Windows amd64 runs on Go 1.27.0 with loopback HTTP servers: twelve upstreams shared by two sources made twelve requests, one credential lookup and at most four concurrent requests. With a 20 ms per-request timeout, twelve stalled requests finished in 61.34 ms; with a 20 ms overall budget, the sweep stopped after four requests in 20.57 ms. Production limits stay at ten seconds per request and thirty per sweep, and these numbers don't predict real provider latency.

## Agent harness maintenance tests

I run `ai/scripts/test_sync.py` for rendering, drift detection, native configuration preservation, and Codex-only sidecars that leave the Claude tree unchanged. I run `ai/scripts/test_fixing_markdown.py` for safe formatter target selection and execution against temporary fixtures, including the Markdown health gate with clean files, lint findings and malformed configuration. `scripts/health.sh harness` runs both suites and verifies the committed renders; neither suite needs credentials or application binaries.
