---
name: test-plan
description: Run the gitbox test plan. Use when the user wants to verify changes before pushing, run pre-PR checks, or do a full release verification. Accepts an optional mode argument.
---

# /test-plan — Run test plan

**IMPORTANT:** Before starting, inform the user: "I'm executing `/test-plan`"

## Usage

```text
/test-plan              Run pre-PR checks (default)
/test-plan pre-pr       Same as above — quick automated checks
/test-plan full         Full release verification (automated + interactive)
```

## Checklist source

Read the checklists from `docs/testing.md` (sections "Pre-PR checklist" and "Full release checklist"). This is the single source of truth — always re-read it at execution time to pick up any updates.

## Pre-PR mode (default)

Run all automated checks. Track progress with the todo list.

### Step 1: Static analysis

```bash
go vet ./...
```

Fail fast if `go vet` reports issues. Fix before continuing.

### Step 2: Unit tests

```bash
go test -short ./...
```

All tests must pass. If any fail, report and stop.

### Step 3: Cross-compile

Build all 3 platform binaries in parallel:

```bash
go build -o build/gitbox.exe ./cmd/cli
GOOS=darwin GOARCH=arm64 go build -o build/gitbox-darwin-arm64 ./cmd/cli
GOOS=linux  GOARCH=amd64 go build -o build/gitbox-linux-amd64  ./cmd/cli
```

### Step 4: Deploy and smoke test

Source `.env` for SSH host variables, then deploy and test:

```bash
source .env
[[ -n "$SSH_MAC_ARM_HOST"   ]] && scp build/gitbox-darwin-arm64 "$SSH_MAC_ARM_HOST":/tmp/gitbox   && ssh "$SSH_MAC_ARM_HOST"   "chmod +x /tmp/gitbox"
[[ -n "$SSH_MAC_INTEL_HOST" ]] && scp build/gitbox-darwin-amd64 "$SSH_MAC_INTEL_HOST":/tmp/gitbox && ssh "$SSH_MAC_INTEL_HOST" "chmod +x /tmp/gitbox"
[[ -n "$SSH_LINUX_HOST"     ]] && scp build/gitbox-linux-amd64  "$SSH_LINUX_HOST":/tmp/gitbox     && ssh "$SSH_LINUX_HOST"     "chmod +x /tmp/gitbox"
```

Run smoke tests on all configured platforms in parallel:

```bash
# Windows
build/gitbox.exe version
build/gitbox.exe help
# macOS Apple Silicon
[[ -n "$SSH_MAC_ARM_HOST"   ]] && ssh "$SSH_MAC_ARM_HOST"   "/tmp/gitbox version && /tmp/gitbox help"
# macOS Intel
[[ -n "$SSH_MAC_INTEL_HOST" ]] && ssh "$SSH_MAC_INTEL_HOST" "/tmp/gitbox version && /tmp/gitbox help"
# Linux
[[ -n "$SSH_LINUX_HOST"     ]] && ssh "$SSH_LINUX_HOST"     "/tmp/gitbox version && /tmp/gitbox help"
```

### Step 5: Report

Present results from all 3 platforms side by side. Format:

```text
Platform    | version        | help   | tests
------------|----------------|--------|------
Windows     | v1.x.x (hash) | OK     | 182 passed
macOS       | v1.x.x (hash) | OK     | (binary only)
Linux       | v1.x.x (hash) | OK     | (binary only)
```

If the change touches a specific area, remind the user about the relevant manual check from the checklist (e.g., "You changed the credential screen — launch the TUI and verify GCM browser auth renders correctly").

## Full mode

Run everything from pre-PR mode, plus:

### Step 6: Integration tests

```bash
go test -v ./...
```

This requires `test-gitbox.json` at repo root. If missing, warn and skip.

### Step 7: Extended CLI smoke (all platforms)

Run on all 3 platforms via SSH:

```bash
gitbox global show --json
gitbox account list --json
gitbox status --json
```

### Step 8: Interactive verification

Read the "Full release checklist" sections from `docs/testing.md` and present them as interactive instructions. For each section:

1. Show the exact commands to run on each platform
2. Use this format for interactive steps:

```text
Please run on each platform:
  Windows:  build\gitbox.exe <command>
  macOS:    ssh <mac-host> "/tmp/gitbox <command>"
  Linux:    ssh <linux-host> "/tmp/gitbox <command>"
```

3. Ask the user to confirm each section passes before moving to the next

### Step 9: Final report

Summarize all results: automated test counts, platform smoke results, and which interactive sections the user confirmed.

## Behavior notes

- Always use the todo list to track progress through the steps
- If any automated step fails, stop and report — do not continue blindly
- For SSH commands, always `source .env` first to get host variables
- Run independent commands in parallel where possible (cross-compile, deploy, smoke tests)
- The checklist file may have been updated since the last run — always re-read it
