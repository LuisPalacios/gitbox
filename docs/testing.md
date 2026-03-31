# Testing

This guide walks you through running and writing tests for gitbox. For the full test inventory (counts and what each covers), see the [testing checklist](testing-checklist.md). Current count: 212 tests across all packages.

## Pre-push hook

The repo includes a safety net: a pre-push hook that automatically checks your code before every `git push`. It runs a static analysis and all unit tests to make sure nothing is broken.

Git does not pick up custom hooks automatically, so after cloning the repo, you MUST run this once:

```bash
git config core.hooksPath .githooks
```

That's it — from now on every `git push` runs the checks. To bypass temporarily (not recommended): `git push --no-verify`.

## Test levels

There are three levels of tests, each requiring a bit more setup than the previous one:

- **Unit tests** — run instantly, no setup needed. They test logic in isolation without touching the network or any provider.
- **Integration tests** — connect to real providers (GitHub, GitLab, Gitea, etc.) using your actual credentials. These need a small configuration file that you prepare once.
- **Scenario tests** — run the full gitbox lifecycle end-to-end: create accounts, clone repos, check status, set up mirrors, and tear everything down. Same configuration file as integration tests.

## Before you start: the test fixture

Integration and scenario tests (Steps 2-8 below) need to talk to real Git providers. To make this work without hardcoding anyone's credentials, the project uses a simple trick: a file called `test-gitbox.json` at the repo root.

This file is just a normal gitbox configuration — the same format as `~/.config/gitbox/gitbox.json` — with one extra field per account: a `_test` key that holds the token for that provider. The test runner reads it, injects the tokens as environment variables, and runs everything in throwaway temp directories so your real machine is never touched.

**Step 1 is the only test that works without this file.** If you try to run integration tests without it, they will fail with a clear message telling you to create it (or use `go test -short` to run only unit tests).

### Setting it up

Copy the template:

```bash
cp test-gitbox.json.example test-gitbox.json
```

The file is gitignored — it contains real secrets and should never be committed.

Edit the `accounts` section with your real provider accounts. For each one, add a `_test` key with a Personal Access Token:

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

| Provider            | Where to create                                                                  | Required permissions                                   |
| ------------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------ |
| **GitHub**          | Settings → Developer settings → Personal access tokens → Fine-grained or Classic | `repo` (full), `read:user`                             |
| **Gitea / Forgejo** | Settings → Applications → Manage Access Tokens                                   | Repository: Read+Write, User: Read, Organization: Read |
| **GitLab**          | Preferences → Access Tokens                                                      | `api` scope                                            |
| **Bitbucket**       | Personal settings → App passwords                                                | Repositories: Read+Write                               |

### Verifying your setup

Before running integration tests, **run the setup script at least once** to verify tokens and generate SSH keys:

```bash
./scripts/setup-credentials.sh
```

This verifies API tokens, generates per-host SSH key pairs, and tests SSH connections. If a token is wrong or expired, you'll see it here — much faster than debugging a failing test. The script is idempotent and safe to run multiple times. When everything shows green `ok`, you're ready for integration tests.

To run credential setup on remote machines too, pass `all`: `./scripts/setup-credentials.sh all`. See [Multiplatform](multiplatform.md) for the full cross-platform workflow.

### GCM accounts

GCM credentials live in the OS keyring and come from an interactive browser login — there's no token to put in a file. For GCM accounts, the test runner checks if `git credential fill` works at runtime and skips tests if not. Don't add a `_test` key to GCM accounts — just make sure GCM is configured on your machine.

### Mirror testing (optional)

If you want to test mirror operations, add a `mirrors` section with a real account pair:

```json
"mirrors": {
  "gh-to-forgejo": {
    "account_src": "github-personal",
    "account_dst": "forgejo-testuser",
    "repos": {}
  }
}
```

Both accounts need tokens in their `_test` keys. Mirror tests create repos on the destination provider, so the destination token needs write access.

### Safety

The test runner **always overrides** `global.folder` with a throwaway temp directory — all clones and config files go there and are deleted after each test.

For `credential_ssh.ssh_folder`, integration tests read the path from your `test-gitbox.json` so they can find real SSH keys. This path **must not be `~/.ssh`** — point it at an isolated location like `~/.gitbox-test/ssh`.

The test runner enforces this: if your fixture points `ssh_folder` at `~/.ssh` or `global.folder` at `~/.config/gitbox`, the tests fail immediately with a clear error telling you to use isolated paths.

## Run the tests

I recommend running tests incrementally, building confidence as you go.

### Step 1: Unit tests (no credentials needed)

Confirms the code compiles and basic logic works. No network, no providers, no `test-gitbox.json` needed.

```bash
go test -short ./...
```

### Step 2: Credential verification

Checks that your provider tokens (the ones you put in `test-gitbox.json`) actually work — it connects to each provider's API and confirms authentication succeeds.

```bash
go test -v -run TestIntegration_CLI_CredentialVerify ./cmd/cli/
```

### Step 3: Discovery

Uses the accounts and tokens from your `test-gitbox.json` to call each provider's API and list your repositories. If this passes, gitbox can see what repos you have on GitHub, GitLab, Gitea, etc.

```bash
go test -v -run TestIntegration_CLI_Discover ./cmd/cli/
```

### Step 4: Clone, status, pull, fetch

Picks the first account in your `test-gitbox.json` that has sources with repos, clones one into a temporary folder, checks its sync status, pulls updates, and fetches. The temporary clone is deleted automatically after the test — your real repos are not touched.

```bash
go test -v -run "TestIntegration_CLI_(Clone|Status|Pull|Fetch)" ./cmd/cli/
```

### Step 5: TUI integration tests

Runs the TUI code behind the scenes (you won't see any terminal UI — it's all programmatic). It loads your accounts from `test-gitbox.json`, simulates the Bubble Tea event loop in memory, sends keystrokes, and checks that the rendered output contains the right account cards, credential badges, and discovery results.

```bash
go test -v -run TestIntegration_TUI ./cmd/cli/tui/
```

### Step 6: Full lifecycle scenario

The big one. Using your `test-gitbox.json` accounts, it runs the entire gitbox CLI workflow programmatically: creates accounts, adds sources and repos, clones them, checks status, pulls, sets up mirrors, deletes a clone, re-clones it, then tears everything down in reverse. If this passes, the whole stack works.

```bash
go test -v -run TestScenario ./cmd/cli/
```

### Step 7: All integration tests at once

```bash
go test -v -run Integration ./cmd/cli/... ./cmd/cli/tui/
```

### Step 8: Everything

It runs verbose, as it goes, and ignores previous caches

```bash
go test -v -p 1 -count=1 ./...
```

## Testing checklist

The [testing checklist](testing-checklist.md) is a manual verification matrix for pre-PR and pre-release checks. It covers all 3 interfaces (CLI, TUI, GUI) across all 3 platforms (Windows, macOS, Linux), with separate sections for automated checks and interactive human verification.

Two modes:

- **Pre-PR** — quick automated checks (~5 min): vet, unit tests, cross-compile, deploy, smoke tests
- **Full release** — everything above plus integration tests and interactive verification of credential flows, TUI rendering, GUI launch, and platform-specific behavior

If you use Claude Code, the `/test-plan` skill automates the pre-PR checks and guides you through interactive steps. Without Claude Code, follow the checklist manually — every step is documented with the exact commands to run.
