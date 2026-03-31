# Testing checklist

This is the living test matrix for gitbox. It covers automated tests (run locally or in CI) and human verification steps across all 3 interfaces (CLI, TUI, GUI) and 3 platforms (Windows, macOS, Linux). If you use Claude Code, the `/test-plan` skill can automate the automated steps and guide you through the interactive ones.

I use this checklist in two modes:

- **Pre-PR** — quick sanity check before pushing (~5 min)
- **Full** — thorough verification before a release (~20 min)

## Pre-PR checklist

Run these before every push or PR. All automated — run them manually or let the pre-push hook handle vet + tests.

### Automated checks

```text
- [ ] go vet ./...
- [ ] go test -short ./...
- [ ] ./scripts/deploy.sh                  (cross-compile; deploys to remotes if configured in .env)
- [ ] ./scripts/smoke.sh                  (smoke tests on all configured platforms)
```

### Quick manual checks

If the change touches a specific area, verify it works on at least the dev machine:

```text
- [ ] Config changes → gitbox global show --json parses correctly
- [ ] CLI command changes → run the changed command with --help and one real invocation
- [ ] TUI changes → launch gitbox (no args) and navigate to the changed screen
- [ ] Credential changes → verify credential status on dashboard shows correct badges
- [ ] GUI changes → launch GitboxApp and verify the changed screen renders
```

## Full release checklist

Run before tagging a release. Combines automated + interactive steps across all platforms.

### 1. Automated test suite

```text
- [ ] go vet ./...
- [ ] go test -short ./...              (unit tests, all platforms implied)
- [ ] go test ./...                     (integration + scenario, requires test-gitbox.json)
- [ ] ./scripts/deploy.sh                  (cross-compile + deploy to remotes)
```

### 2. CLI smoke tests (automated, all platforms)

Run `./scripts/smoke.sh all` or manually on each platform. Expected: clean JSON output, exit code 0.

```text
- [ ] gitbox version
- [ ] gitbox help
- [ ] gitbox global show --json
- [ ] gitbox account list --json
- [ ] gitbox status --json
```

### 3. TUI verification (interactive, all platforms)

Launch `gitbox` (no args) on each platform and verify:

```text
- [ ] Dashboard loads, shows account cards with credential badges
- [ ] Tab switch (Accounts ↔ Mirrors) works
- [ ] Navigate to account detail (Enter on card)
- [ ] Credential screen renders for each type:
      - [ ] token: shows PAT input, guide link
      - [ ] gcm: desktop shows browser auth prompt; SSH shows "desktop session" message
      - [ ] ssh: shows key generation / connection test
- [ ] Discovery: navigate to account → discover → multi-select → save
- [ ] Repo detail: navigate to a repo → see status, clone path
- [ ] Settings: change folder, verify persistence
- [ ] Keyboard hints render correctly at the bottom
- [ ] Esc navigates back at every level
- [ ] Ctrl+C quits cleanly
```

### 4. CLI credential flows (interactive, per platform)

These require real credentials. Run manually:

```text
- [ ] Windows: gitbox credential setup <token-account>   → paste PAT → verify
- [ ] Windows: gitbox credential setup <gcm-account>     → browser opens → verify
- [ ] Windows: gitbox credential setup <ssh-account>      → key generated → verify
- [ ] macOS:   gitbox credential setup <token-account>
- [ ] macOS:   gitbox credential setup <gcm-account>      → browser via `open`
- [ ] macOS:   gitbox credential setup <ssh-account>
- [ ] Linux:   gitbox credential setup <token-account>
- [ ] Linux:   gitbox credential setup <ssh-account>
- [ ] Linux SSH: gitbox credential setup <gcm-account>    → should show "desktop session" message
```

### 5. Clone and sync (interactive, at least 1 platform)

```text
- [ ] gitbox clone          → clones repos that don't exist
- [ ] gitbox status         → shows clean/dirty/ahead/behind
- [ ] gitbox pull           → pulls repos that are behind
- [ ] gitbox fetch          → fetches without merging
```

### 6. GUI verification (interactive, at least Windows)

Launch `GitboxApp` and verify:

```text
- [ ] App opens without console flash (Windows)
- [ ] Dashboard shows accounts and repos
- [ ] Credential setup works (GCM browser auth, token input)
- [ ] Discovery flow works
- [ ] Clone/pull/fetch progress bars render
- [ ] Mirror tab shows groups and status
```

### 7. Platform-specific notes

| Area | Windows | macOS | Linux |
| --- | --- | --- | --- |
| GCM credential store | Windows Credential Manager | macOS Keychain (`osxkeychain`) | `secretservice` or `gpg` |
| SSH agent | OpenSSH agent or Pageant | System ssh-agent | System ssh-agent |
| Git binary | `git.exe` (Git for Windows) | `/usr/bin/git` or Homebrew | System `git` |
| Browser open (GCM) | Always works | Works even via SSH (`open` cmd) | Needs `DISPLAY` or `WAYLAND_DISPLAY` |
| GUI framework | Wails + WebView2 | Wails + WebKit | Wails + WebKitGTK |
| Path separator | Backslash (NTFS, case-insensitive) | Forward slash (APFS) | Forward slash (ext4) |
| Config path | `%APPDATA%/gitbox/` or `~/.config/gitbox/` | `~/.config/gitbox/` | `~/.config/gitbox/` |

## Test inventory

Current automated test counts by package:

| Package | Tests | Coverage |
| --- | --- | --- |
| `pkg/config` | 61 | CRUD, parse, save, migrate, mirrors, backup |
| `pkg/credential` | 12 | Env var, SSH key, CanOpenBrowser, EnsureGlobalGCMConfig |
| `pkg/git` | 9 | Clone, status, remote, branch, config |
| `pkg/provider` | 34 | All 4 providers, HTTP helpers, mirror interfaces |
| `pkg/mirror` | 5 | URL parse, apply discovery |
| `pkg/status` | 8 | Clean, dirty, ahead, behind, not-cloned |
| `pkg/identity` | 7 | Global git identity check/removal |
| CLI unit | 24 | CRUD commands, error paths |
| CLI integration | 6 | Credential, discover, clone, status, pull, fetch |
| CLI scenario | 1 (22 steps) | Full lifecycle |
| TUI unit | 31 | Dashboard, onboarding, account, credential (GCM) |
| TUI helpers | 7 | CloneURL, StripScheme, ReconfigureClones, CountCloned |
| TUI integration | 7 | Dashboard, credential status, navigation, discovery, settings, mirrors |
| **Total** | **212** | |

## Adding new checks

When I add a new feature, I update this file:

1. Add the relevant automated test to the test inventory table
2. Add a manual verification step to the appropriate section (TUI, CLI, GUI)
3. If the feature is platform-sensitive, add a note to the platform-specific table
