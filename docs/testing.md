# Testing

This guide covers running and writing tests for gitbox. For the test inventory (what each package covers) and harness internals, see [testing-reference.md](testing-reference.md). Current count: 459 top-level test functions — 431 in `pkg/` and 28 in `cmd/gui/`.

## Pre-push hook

The repo includes a safety net: a pre-push hook that runs a `gofmt -s` check, static analysis and all unit tests before every `git push`. It builds the GUI frontend first when `cmd/gui/frontend/dist` is missing.

Git does not pick up custom hooks automatically, so after cloning the repo I run this once:

```bash
git config core.hooksPath .githooks
```

From now on every `git push` runs the checks. To bypass temporarily (not recommended): `git push --no-verify`.

## Test levels

There are three levels of tests, each requiring a bit more setup than the previous one:

- **Package unit tests** — the tests under `pkg/`. They run instantly with no setup and test logic in isolation without touching the network or any provider. `pkg/ops/ops_test.go` covers the application operations against a temp `XDG_CONFIG_HOME`, a temp `GIT_CONFIG_GLOBAL`, and a temp SSH folder, so nothing on the real host changes.
- **GUI tests** — the Go tests under `cmd/gui/`. They cover the Wails-side logic that doesn't need a window: terminal and AI harness launching, browser and folder actions, workspaces, and multi-repo containers. They need the compiled frontend in `cmd/gui/frontend/dist`, because the package embeds it.
- **Scenario test** — `TestScenario_FullLifecycle` in `pkg/ops/scenario_test.go` drives the whole lifecycle against a real provider: add account → credential check → discover → clone → status → pull and fetch → account edit + clone reconfigure → mirror CRUD → re-clone → rename → delete. It needs the test fixture described below.

## Before you start: the frontend build

`cmd/gui` embeds `cmd/gui/frontend/dist`, so `go vet ./...` and `go test ./...` fail to compile that package until the folder exists. A `wails build` creates it. To create it without a full build:

```bash
cd cmd/gui/frontend
npm ci
npm run build
```

Package tests alone (`go test ./pkg/...`) don't need it.

## Before you start: the test fixture

The scenario test needs to talk to a real Git provider. The project uses a file called `test-gitbox.json` at the repo root — a normal gitbox configuration with one extra field per account: a `_test` key that holds the token for that provider. The test runner reads it, injects the tokens as environment variables, and runs everything in throwaway temp directories so the real machine is never touched.

**Unit tests work without this file.** If you run the scenario test without it, it fails with a clear message telling you to create it (or use `go test -short` to skip it).

### Setting it up

Copy the template:

```bash
cp json/test-gitbox.json.example test-gitbox.json
```

The file is gitignored — it contains real secrets and should never be committed.

Edit the `accounts` section with real provider accounts. For each one, add a `_test` key with a Personal Access Token:

```json
"github-personal": {
   :
   "_test": {
     "token": "ghp_xxxxxxxxxxxxxxxxxxxx"
   }
}
```

You can add as many accounts as you want. The test runner picks the first one that has sources with repos and a valid token. Accounts without a `_test` key are display-only — they show up for UI testing but no API calls are made against them.

### Creating a token

You create tokens on your provider's website, the same way you would for gitbox itself:

| Provider | Where to create | Required permissions |
| --- | --- | --- |
| **GitHub** | Settings → Developer settings → Personal access tokens | `repo` (full), `read:user` |
| **Gitea / Forgejo** | Settings → Applications → Manage Access Tokens | Repository: Read+Write, User: Read, Organization: Read |
| **GitLab** | Preferences → Access Tokens | `api` scope |
| **Bitbucket** | Personal settings → App passwords | Repositories: Read+Write |

### Verifying your setup

Before running the scenario test, **run the setup script at least once** to verify tokens and generate SSH keys:

```bash
./scripts/setup-credentials.sh
```

This verifies API tokens, generates per-host SSH key pairs, and tests SSH connections. If a token is wrong or expired, you'll see it here — much faster than debugging a failing test. The script is idempotent and safe to run multiple times. When everything shows green `ok`, you're ready for the scenario test.

To run credential setup on remote machines too: `./scripts/setup-credentials.sh all`. See [multiplatform.md](multiplatform.md) for the full cross-platform workflow.

### GCM accounts

GCM credentials live in the OS keyring and come from an interactive browser login — there's no token to put in a file. Don't add a `_test` key to GCM accounts — the scenario test only picks accounts with a token, and GCM accounts stay available for manual checks in `--test-mode`.

### Mirror testing (optional)

To check mirror operations by hand in `--test-mode`, add a `mirrors` section with a real account pair:

```json
"mirrors": {
  "gh-to-forgejo": {
    "account_src": "github-personal",
    "account_dst": "forgejo-testuser",
    "repos": {}
  }
}
```

Both accounts need tokens in their `_test` keys. The destination token needs write access because setting up a mirror creates the target repo there. The scenario test doesn't need this section — its mirror step only creates and deletes a mirror group in the config.

### Safety

The test runner **always overrides** `global.folder` with a throwaway temp directory — all clones and config files go there and are deleted after each test.

For `credential_ssh.ssh_folder`, the scenario test reads the path from `test-gitbox.json` so it can find real SSH keys. This path **must not be `~/.ssh`** — point it at an isolated location like `~/.gitbox-test/ssh`.

The test runner enforces this: if the fixture points `ssh_folder` at `~/.ssh` or `global.folder` at `~/.config/gitbox`, the tests fail immediately.

## Run the tests

I recommend running tests incrementally, building confidence as you go.

### Step 1: Unit tests (no credentials needed)

Confirms the code compiles and basic logic works. No network, no providers, no `test-gitbox.json` needed.

```bash
go test -short ./...
```

The WSL probing tests in `pkg/git` are skipped by default on Windows. To exercise them, set `GITBOX_TEST_WSL=1` and re-run `go test ./pkg/git/`. The probe runs `wsl.exe --status` and skips cleanly if WSL is not installed; on non-Windows the tests assert the helpers return false / error.

### Step 2: Frontend type check

Checks the Svelte frontend with `svelte-check`, the same check the PR workflow runs.

```bash
cd cmd/gui/frontend
npm run check
```

I also run `npm test` from `cmd/gui/frontend` for pure clone-tree regressions: cross-source ownership, duplicate prevention, nested collapse state, and recovery from stale parent references. The health check runs these tests as `frontend-test`.

### Step 3: Full lifecycle scenario

The big one. Runs the whole account lifecycle through `pkg/ops` against the first fixture account with a token: creates the account, checks credentials, discovers repos, clones one, checks status, pulls, fetches, edits the account and reconfigures the clone, creates and deletes a mirror group, re-clones, renames the account, and deletes everything. Every step saves and reloads the config, the same way the GUI persists after each action.

```bash
go test -v -run TestScenario ./pkg/ops/
```

### Step 4: Everything

Runs verbose, ignores cache:

```bash
go test -v -p 1 -count=1 ./...
```

### Step 5: Interactive test mode

Runs the app against the fixture instead of my real config:

```bash
GitboxApp --test-mode
```

The `--test-mode` flag reads `test-gitbox.json` (searching upwards from the current directory), builds a throwaway config in a temp directory, overrides `global.folder`, and injects the fixture tokens as environment variables. Nothing touches my real `~/.config/gitbox/` or existing clones, and the temp directory is deleted when the app exits.

### Startup regression checks

I run the startup snapshot and workspace discovery tests in `cmd/gui` to verify nesting before clones exist, detached response data, config access during a blocked scan, preservation of concurrent edits, and retries after discovery inputs change. I measure snapshot construction separately with `go test ./cmd/gui/ -run '^$' -bench BenchmarkStartupSnapshot -benchmem`. This benchmark measures backend work, not visible startup time.

For interactive startup measurements, I record process launch, window visibility, first configured rows, and complete status separately, along with the platform, clone count, container layout, and run count. I distinguish native production launches from browser navigation against an already-running `wails dev` backend. I need an interactive Windows desktop to measure WebView2 rendering; SSH backend timings alone cannot establish the first-row target. I keep unmeasured targets open until that desktop check completes.

### Refresh regression checks

I run `go test -race ./cmd/gui/ ./pkg/credential/ ./pkg/heal/ ./pkg/git/` for refresh coordination, cancellation, stale results, and credential maintenance. I use channels to block injected work and overlap triggers without a Wails runtime. Provider tests use local HTTP servers to verify bounded concurrency, shared probes, deadlines, and preservation of upstream state after errors or credential rotation.

I trace pre-fetch maintenance with `GIT_TRACE2_EVENT` against isolated clones: unchanged SSH, token, and GCM clones must each spawn two read-only Git commands. I check config and credential-file modification times, token rotation, permissions, helper cancellation, type transitions, linked worktrees, and warnings for repairs that included config overrides. I record the platform, clone count, process count, and elapsed time separately from network fetch duration.

### Refresh measurements (2026-10-15)

I compared pre-fetch maintenance from `da83922` with the implementation for #131 using the same isolated fixture: 24 local clones, one account, and no network fetches. I ran one warm, sequential sweep per platform, credential type, and state. For the drifted cases, I changed both identity fields, origin, and helper before each sweep. I used Go 1.26.1 for the standalone harness; Windows used Git 2.53.0.windows.1.

I measured the whole maintenance loop in the parent process and counted Git process-start events with Trace2. The elapsed times include process creation, Git execution, file operations, and tracing overhead. These runs do not separate Git-internal time, measure GUI paint, or estimate a full eight-worker network refresh.

| Platform      | Clone state | Credential | Git processes before → after | Elapsed seconds before → after |
| ------------- | ----------- | ---------- | ---------------------------- | ------------------------------ |
| macOS arm64   | unchanged   | SSH        | 216 → 48                     | 0.902 → 0.191                  |
| macOS arm64   | unchanged   | Token      | 240 → 48                     | 1.013 → 0.202                  |
| macOS arm64   | unchanged   | GCM        | 192 → 48                     | 0.923 → 0.278                  |
| Windows amd64 | unchanged   | SSH        | 216 → 48                     | 9.831 → 2.037                  |
| Windows amd64 | unchanged   | Token      | 240 → 48                     | 12.396 → 2.382                 |
| Windows amd64 | unchanged   | GCM        | 192 → 48                     | 9.386 → 2.202                  |
| macOS arm64   | drifted     | SSH        | 288 → 264                    | 1.311 → 1.216                  |
| macOS arm64   | drifted     | Token      | 312 → 288                    | 1.457 → 1.319                  |
| macOS arm64   | drifted     | GCM        | 264 → 288                    | 1.222 → 1.402                  |
| Windows amd64 | drifted     | SSH        | 288 → 264                    | 12.774 → 11.085                |
| Windows amd64 | drifted     | Token      | 312 → 288                    | 13.576 → 12.089                |
| Windows amd64 | drifted     | GCM        | 264 → 288                    | 11.105 → 13.073                |

I leave unchanged clones at two read-only Git processes each. GCM drift repair adds effective-config validation after writes, so its drifted case costs more than the baseline; I retain those checks to prevent fetching with settings that an include or worktree config overrides.

In a separate Windows sweep with 24 unchanged SSH clones, I recorded 8.918 → 1.963 s in the parent process, versus 5.421 → 1.193 s of Git-internal time summed from top-level Trace2 session `exit.t_abs` fields. The remaining difference was 3.497 → 0.770 s. That residual includes process startup, IPC, and caller file operations; I do not attribute it solely to antivirus or `HideWindow`. This is a separate sample from the rows above.

I also ran native Windows amd64 probe regressions with Go 1.27.0 and loopback HTTP servers. Twelve upstreams shared by two sources produced twelve requests, one credential lookup, and at most four concurrent requests. With a scaled twenty-millisecond request timeout, twelve stalled requests completed in 61.34 ms. With a twenty-millisecond overall budget, the sweep stopped in 20.57 ms after four requests. These fixture checks verify scheduling and cancellation; production limits remain ten seconds per request and thirty seconds per sweep, and these figures do not predict real provider latency.

## Pre-PR checklist

Run these before every push or PR. The pre-push hook handles gofmt + vet + unit tests, and the PR workflow runs `scripts/health.sh` as its gate.

```text
- [ ] ./scripts/health.sh                  (every checker: Go, frontend, scripts, workflows, docs)
- [ ] go vet ./...
- [ ] go test -short ./...
- [ ] cd cmd/gui/frontend && npm run check
- [ ] cd cmd/gui && wails build             (local GUI build)
- [ ] ./scripts/smoke.sh                   (GitboxApp --version on all configured platforms)
```

If the change touches a specific area, verify on at least the dev machine:

```text
- [ ] Config changes → launch GitboxApp, confirm the config loads and the change persists after a restart
- [ ] Credential changes → verify credential status badges on the account cards
- [ ] GUI changes → launch GitboxApp, verify the changed screen renders
- [ ] Windows → no console window flashes when the changed action runs
```

## Full release checklist

Run before tagging a release. Combines automated + interactive steps across all platforms.

### Automated

```text
- [ ] go vet ./...
- [ ] go test -short ./...              (unit tests)
- [ ] go test ./...                     (scenario test, requires test-gitbox.json)
- [ ] cd cmd/gui/frontend && npm run check
- [ ] ./scripts/ship.sh                 (build and stage the GUI on every remote)
- [ ] ./scripts/smoke.sh all            (GitboxApp --version on all platforms)
```

### GUI verification (interactive, all platforms)

Launch `GitboxApp` on each platform (`./scripts/run-commands.sh` prints the commands):

```text
- [ ] App opens; on Windows without a console flash
- [ ] Guided first run with a fresh config (`XDG_CONFIG_HOME=<empty temp dir>` on macOS and Linux): folder → connect (browser sign-in; **Use a token instead**; **Change details** keeps any existing folder) → pick projects → **Download**; the main screen opens in Simple and the picked repos clone. In Simple, the **+** card runs the same connect and pick steps; in Advanced it opens the full form
- [ ] Dashboard shows account cards, credential badges, and repos
- [ ] Tab switch (Accounts ↔ Mirrors ↔ Workspaces) works
- [ ] Account edit and rename
- [ ] Credential setup for each type (token/gcm/ssh)
- [ ] Discovery: Find projects → select → Add & Pull
- [ ] Clone, Pull All, Fetch All update the status indicators
- [ ] Repo detail panel shows branch, ahead/behind, changed files
- [ ] Mirrors tab shows groups and status
- [ ] Settings: change root folder, System check, Terminals Manager
- [ ] Compact view and back to full view
- [ ] Simple / Advanced: the top-bar switch (grey Simple, green Advanced) changes mode instantly; Simple rows show only dot, name, status sentence, Update or Download when needed, the AI / editor / terminal icons and ⋯; Advanced rows keep every detail and show the same three icons before the fetch button; the Advanced header rings are compact (icon, attention badge, hover tooltip, no label); a fresh config lands in Simple after onboarding
- [ ] Collapse/expand: account header and a parent clone (chevron or name) fold and unfold; folds survive a restart; the cards panel starts folded, unfolds from the active tab, and remembers its state; a chip opens into its card and folds back on an outside click or Escape
```

### Credential flows (interactive, per platform)

```text
- [ ] Windows: token, gcm (browser), ssh (key gen)
- [ ] macOS: token, gcm (browser), ssh
- [ ] Linux: token, gcm (browser on a desktop session), ssh
```

### Update and upgrade (at least 1 platform)

```text
- [ ] ./scripts/sign-release.sh --check passes (the release key is in the SSH agent)
- [ ] The update pill appears when a newer release exists, and the update applies after restart
- [ ] A v1 install upgrades to v2 and keeps the existing gitbox.json
```

### Platform-specific notes

| Area | Windows | macOS | Linux |
| --- | --- | --- | --- |
| GCM store | Windows Credential Manager | macOS Keychain | `secretservice` or `gpg` |
| SSH agent | OpenSSH agent or Pageant | System ssh-agent | System ssh-agent |
| Git binary | `git.exe` (Git for Windows) | `/usr/bin/git` or Homebrew | System `git` |
| Browser open (GCM) | Always works | Works even via SSH | Needs `DISPLAY` or `WAYLAND_DISPLAY` |
| GUI framework | Wails + WebView2 | Wails + WebKit | Wails + WebKitGTK |
| Config path | `%APPDATA%/gitbox/` or `~/.config/gitbox/` | `~/.config/gitbox/` | `~/.config/gitbox/` |

## Adding new checks

When I add a new feature, I update this file:

1. Add the relevant automated test and update the [test inventory](testing-reference.md)
2. Add a manual verification step to the appropriate GUI section above
3. If the feature is platform-sensitive, add a note to the platform-specific table

I use `/test-plan` in Claude Code or `$test-plan` in Codex to automate the pre-PR checks and guide the interactive steps.
