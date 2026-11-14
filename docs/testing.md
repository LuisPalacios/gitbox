# Testing

This guide covers the test levels, the `test-gitbox.json` fixture, and the checklists I run before a PR and before a release. The inventory of tests and the harness internals live in [testing-reference.md](testing-reference.md). The health check is described in [developer-guide.md › Health check](developer-guide.md#health-check).

## Test levels

- **Package unit tests** — the Go tests under `pkg/`. They need no setup, no network and no provider, and isolate everything with `t.TempDir()` and `t.Setenv()`. The `pkg/ops` unit tests run against a temp `XDG_CONFIG_HOME`, a temp `GIT_CONFIG_GLOBAL` and a temp SSH folder, so nothing on the host changes.
- **GUI tests** — the Go tests under `cmd/gui/`. They call `App` methods directly, without a Wails window. They need the compiled frontend in `cmd/gui/frontend/dist`, because the package embeds it.
- **Frontend tests** — pure Node tests in `cmd/gui/frontend/src/lib/*.test.mjs`, run with `npm test`. They need neither Wails nor credentials.
- **Scenario test** — `TestScenario_FullLifecycle` in `pkg/ops/scenario_test.go` drives the whole account lifecycle through `pkg/ops` against a real provider. It needs the fixture below, and `go test -short` skips it.

`go test ./pkg/...` runs without the frontend build. Everything else under Go needs `cmd/gui/frontend/dist`; build it once:

```bash
(cd cmd/gui/frontend && npm ci && npm run build)
```

`wails build` creates it too. See [developer-guide.md › Building from source](developer-guide.md#building-from-source).

## Pre-push hook

`.githooks/pre-push` runs a `gofmt -s -l` check, `go vet ./...` and `go test -short ./...` before every push, and builds the frontend first when `cmd/gui/frontend/dist` is missing. Tag-only pushes skip it. I activate it once per clone:

```bash
git config core.hooksPath .githooks
```

## The test fixture

The scenario test and `GitboxApp --test-mode` read `test-gitbox.json` at the repo root: a normal `gitbox.json` whose accounts can carry a `_test` key with a token. Unit tests don't need it. Without it, the scenario test fails with instructions to create the file or run `go test -short`.

### Setting it up

I copy the template and fill in real accounts:

```bash
cp json/test-gitbox.json.example test-gitbox.json
```

The file is gitignored because it holds real secrets. Never commit it.

Each account that the tests may call gets a `_test` key with a personal access token:

```json
"gb-github-personal": {
  "provider": "github",
  "_test": {
    "token": "ghp_xxxxxxxxxxxxxxxxxxxx"
  }
}
```

Accounts without `_test` are display-only: they show in `--test-mode`, but no API call goes to them. The scenario test uses the first source, in key order, that has repos and whose account has a token. The token needs the same permissions as a gitbox token credential; see [credentials.md › How to create a PAT](credentials.md#how-to-create-a-pat).

### GCM accounts

GCM credentials come from an interactive browser sign-in, so there is no token to store. I leave `_test` off GCM accounts: the scenario test skips them, and they stay available for manual checks in `--test-mode`.

### Mirrors (optional)

To check mirrors by hand in `--test-mode`, I add a `mirrors` section with a real account pair:

```json
"mirrors": {
  "gh-to-forgejo": {
    "account_src": "gb-github-personal",
    "account_dst": "gb-home-personal",
    "repos": {}
  }
}
```

Both accounts need a `_test` token, and the destination token needs write access, because setting up a mirror creates the target repo. The scenario test doesn't need this section: its mirror step only creates and deletes a mirror group in the config.

### Verifying the tokens and SSH keys

Before the first scenario run, I check the fixture:

```bash
./scripts/setup-credentials.sh
```

It verifies each `_test` token against its provider, generates per-host SSH key pairs, adds their `Host` entries to the fixture's SSH config and tests the SSH connections. It is idempotent. With no argument it runs on this machine and on every remote configured in `.env`; a target such as `linux` limits it. When every line shows a green `ok`, the fixture is ready. See [multiplatform.md](multiplatform.md) for the remote setup.

### Safety

- The harness and `--test-mode` replace `global.roots` with one temp directory, so every clone and config file lands there and is deleted afterwards.
- `credential_ssh.ssh_folder` comes from the fixture, so the tests find the generated SSH keys. Point it at an isolated folder such as `~/.gitbox-test/ssh`.
- The run stops at once when `ssh_folder` is `~/.ssh` or a root in `global.roots` is the gitbox config directory (`~/.config/gitbox`).

## Running the tests

I run them in this order, from fastest to slowest.

1. Unit tests, no fixture needed:

   ```bash
   go test -short ./...
   ```

2. Frontend type check and frontend tests:

   ```bash
   cd cmd/gui/frontend
   npm run check
   npm test
   ```

3. The scenario test, against the first fixture account with a token:

   ```bash
   go test -v -run TestScenario ./pkg/ops/
   ```

4. Everything, verbose and uncached:

   ```bash
   go test -v -p 1 -count=1 ./...
   ```

5. The app against the fixture instead of my real config:

   ```bash
   GitboxApp --test-mode
   ```

   `--test-mode` finds `test-gitbox.json` by walking up from the current directory, writes a throwaway config to a temp directory, overrides `global.roots`, applies the same safety checks and injects the fixture tokens as environment variables. My real `~/.config/gitbox/` and clones stay untouched, and the temp directory goes away when the app exits. `./scripts/test-commands.sh` prints the launch command for each platform.

### Focused regression runs

- **Startup snapshot** — `go test ./cmd/gui/ -run TestStartupSnapshot` checks nesting before clones exist and detached snapshot data. `go test ./cmd/gui/ -run '^$' -bench BenchmarkStartupSnapshot -benchmem` measures snapshot construction, which is backend work, not visible startup time.
- **Refresh and credential maintenance** — `go test -race ./cmd/gui/ ./pkg/credential/ ./pkg/heal/ ./pkg/git/` covers refresh coordination, cancellation, stale results, upstream probes and pre-fetch maintenance.

For visible startup time I measure on the native desktop: process launch to window, window to the first configured rows with container nesting, and time to complete status. I record the platform, clone count, container layout and number of runs. A browser navigation against a running `wails dev` backend measures a different path, and SSH timings can't show WebView2 row paint, so a Windows startup target stays open until I measure it on an interactive Windows desktop.

### Discovery dialog layout

I check **Find projects** in native WebKit with synthetic accounts and repositories. I cover loading, error, empty, one-result and long-list states, with results arriving both during and after the opening transition. I reopen the dialog, filter the list, resize between short and tall windows, and check keyboard access, selection and **Add & Pull**. I keep provider and clone calls isolated from production credentials and repositories.

With enough viewport space, I expect the count, filter, **Select all** and one repository to fit without scrolling. Long lists must scroll while the header and action buttons remain accessible. When shared modal styles change, I also check account, credential, confirmation and mirror discovery dialogs.

I let the opening transition and result update finish before reading element sizes. Intermediate layout reads can force WebKit to recalculate sizes and hide the regression from #221. I compare the body's `clientHeight` and `scrollHeight`, inspect the rendered result, and check the row and footer bounds; searching CSS text does not test layout.

## Pre-PR checklist

I run these before every push or PR. `/test-plan` in Claude Code or `$test-plan` in Codex automates them.

- [ ] `./scripts/health.sh` passes: it includes `go vet`, `go test -short`, `svelte-check` and `npm test` (see [Health check](developer-guide.md#health-check))
- [ ] The focused tests for the changed area pass (`go test ./pkg/<area>/ ./cmd/gui/`)
- [ ] `cd cmd/gui && wails build` succeeds after copying the icons (see [Building from source](developer-guide.md#building-from-source))
- [ ] `cmd/gui/build/bin/GitboxApp --version` prints `GitboxApp <version> (<sha>)`
- [ ] `./scripts/ship.sh` and `./scripts/smoke.sh` pass on every remote in `.env` (skip when `.env` has none)

When the change touches one of these areas, I also check it on the dev machine:

- [ ] Config: GitboxApp loads the config, and the change survives a restart
- [ ] Credentials: the account's health words in the sidebar and on its card match the real credential state
- [ ] Disk reconcile or containers: a clone dropped under a root is adopted or listed in **Unmatched clones** on the next window focus
- [ ] GUI: the changed screen renders in Simple and in Advanced, in light and dark themes
- [ ] Windows: no console window flashes when the changed action runs

## Full release checklist

I run this before tagging a release. It combines automated and interactive steps across every platform.

### Automated

- [ ] `./scripts/health.sh` passes
- [ ] `go test ./...` passes with `test-gitbox.json` in place, including `TestScenario_FullLifecycle`
- [ ] `./scripts/ship.sh` builds and stages the GUI on every remote
- [ ] `./scripts/smoke.sh all` prints a version on every platform
- [ ] `./scripts/sign-release.sh --check` passes, so the release key is in the SSH agent

### GUI verification (interactive, all platforms)

I launch GitboxApp on each platform with the commands `./scripts/run-commands.sh` (real config) or `./scripts/test-commands.sh` (fixture) prints.

- [ ] The app opens; on Windows without a console flash
- [ ] First run: with `XDG_CONFIG_HOME` pointing at an empty folder, the guided setup goes folder → connect (browser sign-in, **Use a token instead**, **Change details** keeps the chosen folder) → pick projects → **Download**; the main screen opens in Simple and the picked repos clone
- [ ] Adding accounts: in Simple, **Connect an account** runs the same connect and pick steps; in Advanced, **Add account** opens the full form
- [ ] Sidebar: Needs attention, All repos, each account and Mirrors open their view; the GitBox logo and ⌘B / Ctrl+B fold it to the icon rail and back; below 780 px the rail shows and the logo opens the sidebar over the content; the view and the rail survive a restart
- [ ] Account groups: in All repos and Needs attention each account sits in a light card with a filled ▾ caret and its name in uppercase in the provider's colour; clicking the header folds it, and its counts stay visible while folded
- [ ] Account card: the account view's card shows the health mark and name, the health words with their fix button, and the counts with **Find** and **+ New** in both modes; a broken credential tints it red and puts its fix at the top of Needs attention
- [ ] Account edit and rename keep the clones, the view and the credential
- [ ] Credential setup works for token, GCM and SSH
- [ ] Discovery: **Find** → select repos → **Add & Pull** clones them into the account's root
- [ ] Clone, **Pull all** and **Fetch all** update the status dots; the activity strip shows the scope while they run, and a result card sums them up
- [ ] The repo detail panel shows branch and ahead/behind in Advanced, and changed files in both modes; a clone with incoming updates opens a scrollable list of every commit with author and message, keeps local-file details, and refreshes after an update
- [ ] Incoming preview: a long list fits a small window; switching clones or closing and reopening while loading never shows another clone's results; opening the panel alone does not fetch or pull
- [ ] Clone roots: in Settings I add a root, make it default and remove it; with two roots, a row's **Clone** asks which root and lands there, and **Pull all** doesn't ask
- [ ] Disk reconcile: a recognised clone dropped under a root is adopted in place on the next window focus; an unrecognised one appears in **Unmatched clones** under **Found on disk**, where **Adopt**, **Ignore**, **Show ignored** and **Stop ignoring** work; a moved clone is re-linked; repos under an unplugged root read **Root offline**
- [ ] Containers: a clone with nested clones shows an outlined ▾ chevron in place of its status dot; the chevron folds everything below, the name opens available details or otherwise folds the children, and folds survive a restart
- [ ] Workspaces: a `.code-workspace` dropped at a clone's root makes it a container on the next window focus, with its nested clones below and missing listed folders as dimmed **not cloned** rows; a second file asks which one to use; the editor button opens the workspace file in VS Code
- [ ] Simple / Advanced: the top-bar switch (grey Simple, red Advanced) changes mode at once; Simple rows show the status dot, name, status sentence, **Update** or **Download** when needed, the AI, editor and terminal icons and ⋯; Advanced rows keep every detail and the per-row fetch button; Simple hides Mirrors, delete mode and the compact view
- [ ] The top-bar rings show an icon, an attention badge and a hover tooltip, and clicking a pending ring opens Needs attention (the mirror ring opens Mirrors)
- [ ] Mirrors view shows its groups with **Discover** and **Check all** in the header; a mirror chip opens into its card and folds back on an outside click or Escape
- [ ] Settings: System check and the Terminals Manager open; the theme follows the OS, light or dark, and survives a restart
- [ ] Compact view and back to the full view

### Credential flows (interactive, per platform)

- [ ] Windows: token, GCM (browser), SSH (key generation)
- [ ] macOS: token, GCM (browser), SSH
- [ ] Linux: token, GCM (browser on a desktop session), SSH

### Update and upgrade (at least one platform)

- [ ] With a newer signed release published, the update pill appears in the footer; clicking it verifies the signature and checksum, applies the update, and the app restarts on the new version
- [ ] The Linux AppImage pill opens the release page instead of updating
- [ ] A v1 install upgrades to v2 and keeps its `gitbox.json`

The fail-closed paths (missing or invalid signature, missing checksum) are covered by the `pkg/update` tests, not by hand.

### Platform-specific notes

| Area | Windows | macOS | Linux |
| --- | --- | --- | --- |
| GCM store | Windows Credential Manager | macOS Keychain | `secretservice` (default) or `gpg` |
| SSH agent | OpenSSH agent or Pageant | System ssh-agent | System ssh-agent |
| Git binary | `git.exe` (Git for Windows) | `/usr/bin/git` or Homebrew | System `git` |
| Browser open (GCM) | Always works | Works even via SSH | Needs `DISPLAY` or `WAYLAND_DISPLAY` |
| GUI framework | Wails + WebView2 | Wails + WebKit | Wails + WebKitGTK |
| Config path | `~/.config/gitbox/` | `~/.config/gitbox/` | `~/.config/gitbox/` |

`XDG_CONFIG_HOME` overrides the config path on every platform.

## Adding new checks

When I add a feature, I update the test docs:

1. Add the automated test and list it in the [test inventory](testing-reference.md#test-inventory).
2. Add a manual step to the matching checklist section above.
3. If the feature behaves differently per platform, add a row or note to the platform table.

## Agent harness checks

I verify changes limited to agent guidance and maintenance scripts with `./scripts/health.sh harness markdown shellcheck actionlint`. The harness gate runs the renderer and Markdown formatter regression suites, then checks both generated trees against `ai/`. These changes do not need application tests or a GUI build. I still run the full health check before a PR and verify discovery in a fresh session of each tool.
