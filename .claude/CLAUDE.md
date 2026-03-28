# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## What this repo is

A multi-component project for managing Git multi-account environments across providers (GitHub, GitLab, Gitea, Forgejo, Bitbucket).

1. **Go app** (`cmd/cli/`, `cmd/gui/`, `pkg/`) — CLI + Wails GUI sharing a Go library. In development.
2. **`legacy/git-config-repos/`** — Bash script for automated repo configuration. Production, power users. UNTOUCHED.
3. **`legacy/git-status-pull/`** — Bash script for sync status and auto-pull. Production, power users. UNTOUCHED.

## Repository layout

```text
cmd/
  cli/                    Go CLI binary (gitboxcmd)
  gui/                    Wails v2 + Svelte GUI (gitbox)
pkg/                      Shared Go library
  config/                 Config v2 model, load/save, v1→v2 migration
  git/                    Git subprocess operations (os/exec)
  provider/               Provider API clients (GitHub, GitLab, Gitea, Forgejo)
  mirror/                 Repo migration + push mirrors
  status/                 Clone status checking
docs/
  cli-guide.md            CLI quick start
  gui-guide.md            GUI guide (placeholder)
  reference.md            Detailed command & config reference
  developer-guide.md      Build instructions, contributing
  architecture.md         Technical design
  migration.md            v1→v2 migration guide
legacy/
  git-config-repos/       Shell script sub-project (UNTOUCHED)
  git-status-pull/        Shell script sub-project (UNTOUCHED)
gitbox.schema.json   v2 JSON Schema
gitbox.jsonc         v2 annotated example (Spanish comments)
go.mod / go.sum           Go module
README.md                 Project overview
```

## Go app

**Language:** Go. **GUI:** Wails v2 + Svelte. **Two independent binaries** from shared `pkg/`.

```bash
# Build CLI
go build -o build/gitboxcmd ./cmd/cli

# Build GUI (requires Wails CLI)
# ALWAYS copy icons before building — they are not checked in under cmd/gui/build/
cp assets/appicon.png cmd/gui/build/appicon.png
cp assets/icon.ico    cmd/gui/build/windows/icon.ico
cd cmd/gui && wails build

# Run tests
go test ./pkg/...
```

Git operations shell out to system `git` via `os/exec`. Provider APIs use `net/http`.

**Windows console flash rule:** Every `exec.Command` in the GUI binary (`cmd/gui/`) **MUST** call `git.HideWindow(cmd)` before `.Run()`, `.Output()`, or `.Start()`. This sets `SysProcAttr.HideWindow = true` on Windows, preventing a console window from flashing. The CLI binary does not need this. Always check for bare `exec.Command` calls in `cmd/gui/` after any change.

## Shell scripts

### `legacy/git-config-repos/git-config-repos.sh`

Reads `~/.config/git-config-repos/git-config-repos.json` and for each configured account/repo: verifies credentials, clones repos that don't exist, and fixes configuration of existing ones. Supports GCM, SSH, and token credential types per-repo.

```bash
./legacy/git-config-repos/git-config-repos.sh
./legacy/git-config-repos/git-config-repos.sh --dry-run
```

### `legacy/git-status-pull/git-status-pull.sh`

Scans all `.git` directories from CWD and reports sync status. Can auto-pull if safe.

```bash
./legacy/git-status-pull/git-status-pull.sh
./legacy/git-status-pull/git-status-pull.sh -v pull
```

## Platform detection

Both scripts share the same detection block: `PLATFORM` (`wsl2` | `gitbash` | `macos` | `linux`) and `cmdgit` (`git.exe` on WSL2, `git` elsewhere).

## Config format

**v1** (shell scripts): `~/.config/git-config-repos/git-config-repos.json` — `accounts`, string booleans, `gcm`/`ssh` only.

**v2** (Go app): `~/.config/gitbox/gitbox.json` — `sources`, real booleans, `gcm`/`ssh`/`token`, nested SSH/GCM objects, `provider` field, `version: 2`.

## Workflow Orchestration

**Priority order when rules conflict**: Correctness > Simplicity > Elegance.

### 1. Plan First

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions)
- If something goes sideways, STOP and re-plan immediately
- Write detailed specs upfront to reduce ambiguity

### 2. Subagent Strategy

- Use subagents liberally to keep main context window clean
- Offload research, exploration, and parallel analysis to subagents
- One task per subagent for focused execution

### 3. Verify Before Done

- Never mark a task complete without proving it works
- Test scripts with `bash -n` (syntax check) and `shellcheck` when available
- Go: `go vet ./...` + `go test ./...`
- Validate config templates render correctly before committing
- After any command that writes config files, read the actual file on disk (`cat` via SSH or locally) — never trust command output alone

### 4. Autonomous Bug Fixing

- When given a bug report: just fix it
- Point at logs, errors, failing tests — then resolve them

### 5. Learn from Corrections

- After corrections from the user: save a `feedback` memory via the memory system

## Git Commits

- **Never** include `Co-Authored-By: Claude` or any Claude attribution in commit messages
- **Never** modify git config for author/committer
- Commits should appear as the user's own work

## Multiplatform Testing

Development happens on Windows. Two remote boxes (macOS, Linux) are available
for cross-platform testing via SSH. Every build-and-test cycle MUST cover all
three platforms. This is not optional.

### Hosts

Connection details are read from `.env` at the repo root (gitignored, never committed).

| Platform | Env var          | Arch  | GOOS/GOARCH     |
|----------|------------------|-------|-----------------|
| Windows  | (local machine)  | amd64 | `windows/amd64` |
| macOS    | `SSH_MAC_HOST`   | arm64 | `darwin/arm64`  |
| Linux    | `SSH_LINUX_HOST` | amd64 | `linux/amd64`   |

Before using SSH/SCP, source the `.env` file:

```bash
source .env
```

SSH uses key-based auth (no passwords, no interactive prompts).

### Build-Test-Deploy cycle

After any code change, always execute this full cycle:

1. **Build all three binaries** (parallel):

   ```bash
   go build -o build/gitboxcmd.exe ./cmd/cli
   GOOS=darwin GOARCH=arm64 go build -o build/gitboxcmd-darwin-arm64 ./cmd/cli
   GOOS=linux  GOARCH=amd64 go build -o build/gitboxcmd-linux-amd64  ./cmd/cli
   ```

2. **Run Go unit tests** locally:

   ```bash
   go test ./pkg/...
   ```

3. **Deploy to remotes** (parallel):

   ```bash
   scp build/gitboxcmd-darwin-arm64 "$SSH_MAC_HOST":/tmp/gitboxcmd && ssh "$SSH_MAC_HOST" "chmod +x /tmp/gitboxcmd"
   scp build/gitboxcmd-linux-amd64  "$SSH_LINUX_HOST":/tmp/gitboxcmd && ssh "$SSH_LINUX_HOST" "chmod +x /tmp/gitboxcmd"
   ```

4. **Run non-interactive tests on all platforms** (parallel):

   ```bash
   # Windows (local)
   build/gitboxcmd.exe <subcommand> <args>
   # macOS
   ssh "$SSH_MAC_HOST" "/tmp/gitboxcmd <subcommand> <args>"
   # Linux
   ssh "$SSH_LINUX_HOST" "/tmp/gitboxcmd <subcommand> <args>"
   ```

5. **Report results** from all three platforms side by side.

### Non-interactive vs Interactive tests

- **Non-interactive** (read-only commands, JSON output, version, status, config show, etc.):
  Claude runs these directly via SSH on all platforms and reports results.
  The user can also verify artifacts (e.g. `.json` files) on remotes via their own SSH terminals.

- **Interactive** (commands requiring user input, credential prompts, `init` wizard, etc.):
  Claude shows the exact command to run on each platform. The user executes
  them manually on their own SSH terminals and reports back.
  Format for interactive instructions:

  ```text
  Please run on each platform:
    Windows:  build\gitboxcmd.exe <command>
    macOS:    ssh <mac-host> "/tmp/gitboxcmd <command>"
    Linux:    ssh <linux-host> "/tmp/gitboxcmd <command>"
  ```

## Core Principles

- **Simplicity First**: Make every change as simple as possible
- **No Laziness**: Find root causes. No temporary fixes.
- **Minimal Impact**: Changes should only touch what's necessary
- **Skills first**: Check if a skill exists before doing work manually
- **Zero entropy**: Never create files outside defined structure
