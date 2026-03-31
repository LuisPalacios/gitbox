# Testing

This guide covers running and writing tests for gitbox. For the full test inventory (every test name and what it covers), see [testing-reference.md](testing-reference.md). Current count: 208 tests across all packages.

## Pre-push hook

The repo includes a safety net: a pre-push hook that runs static analysis and all unit tests before every `git push`.

Git does not pick up custom hooks automatically, so after cloning the repo I run this once:

```bash
git config core.hooksPath .githooks
```

From now on every `git push` runs the checks. To bypass temporarily (not recommended): `git push --no-verify`.

## Test levels

There are three levels of tests, each requiring a bit more setup than the previous one:

- **Unit tests** — run instantly, no setup needed. They test logic in isolation without touching the network or any provider.
- **Integration tests** — connect to real providers (GitHub, GitLab, Gitea, etc.) using actual credentials. These need a small configuration file that I prepare once.
- **Scenario tests** — run the full gitbox lifecycle end-to-end: create accounts, clone repos, check status, set up mirrors, and tear everything down. Same configuration file as integration tests.

## Before you start: the test fixture

Integration and scenario tests need to talk to real Git providers. The project uses a file called `test-gitbox.json` at the repo root — a normal gitbox configuration with one extra field per account: a `_test` key that holds the token for that provider. The test runner reads it, injects the tokens as environment variables, and runs everything in throwaway temp directories so the real machine is never touched.

**Unit tests work without this file.** If you try to run integration tests without it, they fail with a clear message telling you to create it (or use `go test -short` to run only unit tests).

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

Before running integration tests, **run the setup script at least once** to verify tokens and generate SSH keys:

```bash
./scripts/setup-credentials.sh
```

This verifies API tokens, generates per-host SSH key pairs, and tests SSH connections. If a token is wrong or expired, you'll see it here — much faster than debugging a failing test. The script is idempotent and safe to run multiple times. When everything shows green `ok`, you're ready for integration tests.

To run credential setup on remote machines too: `./scripts/setup-credentials.sh all`. See [multiplatform.md](multiplatform.md) for the full cross-platform workflow.

### GCM accounts

GCM credentials live in the OS keyring and come from an interactive browser login — there's no token to put in a file. The test runner checks if `git credential fill` works at runtime and skips tests if not. Don't add a `_test` key to GCM accounts — just make sure GCM is configured on the machine.

### Mirror testing (optional)

To test mirror operations, add a `mirrors` section with a real account pair:

```json
"mirrors": {
  "gh-to-forgejo": {
    "account_src": "github-personal",
    "account_dst": "forgejo-testuser",
    "repos": {}
  }
}
```

Both accounts need tokens in their `_test` keys. The destination token needs write access because mirror tests create repos there.

### Safety

The test runner **always overrides** `global.folder` with a throwaway temp directory — all clones and config files go there and are deleted after each test.

For `credential_ssh.ssh_folder`, integration tests read the path from `test-gitbox.json` so they can find real SSH keys. This path **must not be `~/.ssh`** — point it at an isolated location like `~/.gitbox-test/ssh`.

The test runner enforces this: if the fixture points `ssh_folder` at `~/.ssh` or `global.folder` at `~/.config/gitbox`, the tests fail immediately.

## Run the tests

I recommend running tests incrementally, building confidence as you go.

### Step 1: Unit tests (no credentials needed)

Confirms the code compiles and basic logic works. No network, no providers, no `test-gitbox.json` needed.

```bash
go test -short ./...
```

### Step 2: Credential verification

Checks that provider tokens in `test-gitbox.json` actually work — connects to each provider's API and confirms authentication succeeds.

```bash
go test -v -run TestIntegration_CLI_CredentialVerify ./cmd/cli/
```

### Step 3: Discovery

Uses accounts and tokens from `test-gitbox.json` to call each provider's API and list repositories.

```bash
go test -v -run TestIntegration_CLI_Discover ./cmd/cli/
```

### Step 4: Clone, status, pull, fetch

Picks the first account with sources and repos, clones one into a temporary folder, checks sync status, pulls, and fetches. The clone is deleted automatically.

```bash
go test -v -run "TestIntegration_CLI_(Clone|Status|Pull|Fetch)" ./cmd/cli/
```

### Step 5: TUI integration tests

Runs the TUI programmatically (no visible UI). Loads accounts from `test-gitbox.json`, simulates the Bubble Tea event loop, sends keystrokes, and checks rendered output.

```bash
go test -v -run TestIntegration_TUI ./cmd/cli/tui/
```

### Step 6: Full lifecycle scenario

The big one. Runs the entire gitbox CLI workflow: creates accounts, adds sources and repos, clones, checks status, pulls, sets up mirrors, deletes a clone, re-clones, then tears everything down in reverse.

```bash
go test -v -run TestScenario ./cmd/cli/
```

### Step 7: All integration tests at once

```bash
go test -v -run Integration ./cmd/cli/... ./cmd/cli/tui/
```

### Step 8: Everything

Runs verbose, ignores cache:

```bash
go test -v -p 1 -count=1 ./...
```

## Pre-PR checklist

Run these before every push or PR. All automated — or let the pre-push hook handle vet + unit tests.

```text
- [ ] go vet ./...
- [ ] go test -short ./...
- [ ] ./scripts/deploy.sh                  (cross-compile; deploys to remotes if configured)
- [ ] ./scripts/smoke.sh                  (smoke tests on all configured platforms)
```

If the change touches a specific area, verify on at least the dev machine:

```text
- [ ] Config changes → gitbox global show --json parses correctly
- [ ] CLI command changes → run with --help and one real invocation
- [ ] TUI changes → launch gitbox (no args), navigate to the changed screen
- [ ] Credential changes → verify credential status badges on dashboard
- [ ] GUI changes → launch GitboxApp, verify the changed screen renders
```

## Full release checklist

Run before tagging a release. Combines automated + interactive steps across all platforms.

### Automated

```text
- [ ] go vet ./...
- [ ] go test -short ./...              (unit tests)
- [ ] go test ./...                     (integration + scenario, requires test-gitbox.json)
- [ ] ./scripts/deploy.sh                  (cross-compile + deploy to remotes)
```

### CLI smoke (all platforms)

Run `./scripts/smoke.sh all` or manually on each platform:

```text
- [ ] gitbox version
- [ ] gitbox help
- [ ] gitbox global show --json
- [ ] gitbox account list --json
- [ ] gitbox status --json
```

### TUI verification (interactive, all platforms)

Launch `gitbox` (no args) on each platform:

```text
- [ ] Dashboard loads with account cards and credential badges
- [ ] Tab switch (Accounts ↔ Mirrors) works
- [ ] Account detail via Enter on card
- [ ] Credential screen for each type (token/gcm/ssh)
- [ ] Discovery: account → discover → multi-select → save
- [ ] Repo detail: status, clone path
- [ ] Settings: change folder, verify persistence
- [ ] Keyboard hints render, Esc navigates back, Ctrl+C quits
```

### CLI credential flows (interactive, per platform)

```text
- [ ] Windows: token, gcm (browser), ssh (key gen)
- [ ] macOS: token, gcm (browser via `open`), ssh
- [ ] Linux: token, ssh, gcm-over-SSH ("desktop session" message)
```

### Clone and sync (at least 1 platform)

```text
- [ ] gitbox clone → clones missing repos
- [ ] gitbox status → shows clean/dirty/ahead/behind
- [ ] gitbox pull → pulls repos that are behind
- [ ] gitbox fetch → fetches without merging
```

### GUI verification (at least Windows)

```text
- [ ] App opens without console flash
- [ ] Dashboard shows accounts and repos
- [ ] Credential setup works
- [ ] Discovery and clone/pull/fetch flows work
- [ ] Mirror tab shows groups and status
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

1. Add the relevant automated test to the [test inventory](testing-reference.md)
2. Add a manual verification step to the appropriate section above (TUI, CLI, GUI)
3. If the feature is platform-sensitive, add a note to the platform-specific table

If using Claude Code, the `/test-plan` skill automates the pre-PR checks and guides through interactive steps.
