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

`cmd/gui` embeds `cmd/gui/frontend/dist`, so build the frontend first when `dist` is missing:

```bash
[ -d cmd/gui/frontend/dist ] || (cd cmd/gui/frontend && npm ci && npm run build)
go vet ./...
```

Fail fast if `go vet` reports issues. Fix before continuing.

### Step 2: Unit tests

```bash
go test -short ./...
```

All tests must pass. If any fail, report and stop.

### Step 3: Build the GUI locally

```bash
LDFLAGS="-X main.version=$(git describe --tags --always)-dev -X main.commit=$(git rev-parse --short HEAD)"
cp assets/appicon.png cmd/gui/build/appicon.png
cp assets/icon.ico    cmd/gui/build/windows/icon.ico
(cd cmd/gui && wails build -ldflags "$LDFLAGS")
```

On Linux add `-tags webkit2_41`. Output: `cmd/gui/build/bin/GitboxApp[.exe]` (`GitboxApp.app` on macOS).

### Step 4: Ship and smoke test

Build the GUI on every remote configured in `.env` (wails cannot cross-compile), then run `GitboxApp --version` on the local build and on every staged remote copy:

```bash
./scripts/ship.sh          # skip when .env has no remote hosts
./scripts/smoke.sh all
```

`smoke.sh` prints one line per platform with the version string, and fails if any binary does not answer `--version`.

### Step 5: Report

Present results from all platforms side by side. Format:

```text
Platform    | GitboxApp --version      | tests
------------|--------------------------|-----------
Windows     | GitboxApp v2.x.x (hash)  | all passed
macOS       | GitboxApp v2.x.x (hash)  | (binary only)
Linux       | GitboxApp v2.x.x (hash)  | (binary only)
```

If the change touches a specific area, remind the user about the relevant manual check from the checklist (e.g., "You changed the credential flow — launch GitboxApp and verify GCM browser auth completes").

## Full mode

Run everything from pre-PR mode, plus:

### Step 6: Integration tests

```bash
go test -v ./...
```

This requires `test-gitbox.json` at repo root (it drives `TestScenario_FullLifecycle` in `pkg/ops` against real providers). If missing, warn and skip.

### Step 7: Interactive verification

Read the "Full release checklist" sections from `docs/testing.md` and present them as interactive instructions. For each section:

1. Print the exact launch commands with `./scripts/test-commands.sh` (test mode) or `./scripts/run-commands.sh` (real config). The GUI needs each host's desktop session, so the user runs them there.
2. Use this format for interactive steps:

```text
Please launch on each platform and check <section>:
  Windows:  cmd/gui/build/bin/GitboxApp.exe --test-mode
  macOS:    cd ~ && /tmp/GitboxApp.app/Contents/MacOS/GitboxApp --test-mode
  Linux:    cd ~ && /tmp/GitboxApp --test-mode
```

3. Ask the user to confirm each section passes before moving to the next

### Step 8: Final report

Summarize all results: automated test counts, platform smoke results, and which interactive sections the user confirmed.

## Behavior notes

- Always use the todo list to track progress through the steps
- If any automated step fails, stop and report — do not continue blindly
- The scripts load `.env` themselves; for ad-hoc SSH commands, `source .env` first to get host variables
- Run independent commands in parallel where possible
- The checklist file may have been updated since the last run — always re-read it
