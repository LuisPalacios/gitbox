# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## What this repo is

A multi-component project for managing Git multi-account environments across providers (GitHub, GitLab, Gitea, Forgejo, Bitbucket).

**Go app** (`cmd/cli/`, `cmd/gui/`, `pkg/`) — CLI + TUI + Wails GUI sharing a Go library.

## Core Principles

- **Simplicity First**: Make every change as simple as possible. Impact minimal code.
- **No Laziness**: Find root causes. No temporary fixes. Senior developer standards.
- **Minimal Impact**: Changes should only touch what's necessary. Avoid introducing bugs.
- **Skills first**: Check if a skill exists before manual work
- **Self-improve**: When a skill fails, update its SKILL.md with the fix
- **Zero entropy**: Never create files outside defined structure

## Documentation style

When writing docs, READMEs, or code comments, follow these rules:

- **First person singular** ("I install", "I use"), unless explaining to others ("Install from releases page")
- **Sentence-case headings** — only capitalize the first word
- **Hyphens (`-`)** for unordered lists, never asterisks
- **Language tags** on every fenced code block
- **Active voice**, direct phrasing, no AI filler ("Let's dive in", "In conclusion")
- **Prose over tables** for concepts; tables only for CLI references or pure data
- No passive voice, no long paragraphs, no placeholder TODOs

## Repository layout

```text
cmd/
  cli/                    Go CLI + TUI binary (gitbox)
    tui/                  Bubble Tea TUI (launched when no args + terminal)
      styles/             Theme, colors, symbols
  gui/                    Wails v2 + Svelte GUI (GitboxApp)
pkg/                      Shared Go library
  config/                 Config v2 model, load/save, v1→v2 migration
  credential/             Credential verification (GCM, SSH, token)
  git/                    Git subprocess operations (os/exec)
  provider/               Provider API clients + mirror interfaces (GitHub, GitLab, Gitea, Forgejo)
  mirror/                 Push/pull mirror setup, status, guides
  status/                 Clone status checking
  update/                 Auto-update: version check, download, self-replace
docs/
  cli-guide.md            CLI quick start
  gui-guide.md            GUI guide
  reference.md            Detailed command & config reference
  developer-guide.md      Build instructions, contributing
  architecture.md         Technical design
  macos-signing.md        macOS code signing setup
  credentials.md          Credential setup guide
  testing.md              Test levels, fixture format, pre-PR and release checklists
  testing-reference.md    Test inventory, harness internals
  completion.md           Shell completion docs
  diagrams/               Architecture diagrams
assets/                   Icons, logo, screenshot, VHS tape files
scripts/
  installer.iss            Windows Inno Setup installer script
  appimage/                Linux AppImage support files (desktop, AppRun)
  dmg/                     macOS DMG installer script + README
.githooks/pre-push        Pre-push hook (go vet + unit tests)
.claude/
  context/
    feature-radar.md      Feature backlog (managed by /wish skill)
    testing-patterns.md   Test helpers, naming conventions
  skills/
    wish/                 Feature lifecycle: view, add, plan, ship
    test-plan/            Pre-PR and release verification
    fixing-markdown/      Markdown lint + format
    screenshot-prototype/ GUI screenshot generation
    preview-prototype/    Local Svelte preview server
  rules/
    skills-authoring.md   Skill creation guidelines
.github/workflows/ci.yml  CI: build, test, release (+ installers, DMGs, AppImage)
json/
  gitbox.schema.json      v2 JSON Schema
  gitbox.jsonc            v2 annotated example (Spanish comments)
go.mod / go.sum           Go module
README.md                 Project overview
```

## Go app

**Language:** Go. **GUI:** Wails v2 + Svelte. **TUI:** Charm stack (Bubble Tea, Lip Gloss, Bubbles). **Three modes** from shared `pkg/`.

### Binary naming

The CLI/TUI binary is `gitbox`, the GUI binary is `GitboxApp`. This avoids a Windows NTFS collision (case-insensitive filesystem).

| Platform | CLI/TUI | GUI |
| --- | --- | --- |
| Windows | `gitbox.exe` | `GitboxApp.exe` |
| macOS | `gitbox` | `GitboxApp.app` |
| Linux | `gitbox` | `GitboxApp` |

### Build commands

```bash
# Build CLI/TUI
go build -o build/gitbox ./cmd/cli

# Build GUI (requires Wails CLI)
# ALWAYS copy icons before building — they are not checked in under cmd/gui/build/
cp assets/appicon.png cmd/gui/build/appicon.png
cp assets/icon.ico    cmd/gui/build/windows/icon.ico
cd cmd/gui && wails build
# Output: cmd/gui/build/bin/GitboxApp[.exe]

# Run tests
go test ./pkg/...              # shared library
go test ./cmd/cli/tui/         # TUI unit + integration (teatest)
go test ./cmd/cli/             # CLI unit + integration + scenario
go test ./...                  # everything
```

Git operations shell out to system `git` via `os/exec`. Provider APIs use `net/http`.

### TUI architecture

The TUI is embedded in the CLI binary (`cmd/cli/tui/`). It launches automatically when `gitbox` is run with no arguments and stdin is a terminal. Otherwise, Cobra CLI commands execute.

**Stack:** Bubble Tea (MVU framework), Lip Gloss (styling), Bubbles (text inputs, keys).

**Layout (mirrors GUI):** Two tabs — Accounts and Mirrors. Each tab shows cards at the top (account/mirror group summaries) with a detail list below (repos grouped by source, or mirror repos grouped by group). This matches the GUI's visual hierarchy.

**Screen structure:**

```text
model (root router)
├── dashboardModel          2 tabs: Accounts (cards + repo list), Mirrors (cards + mirror list)
├── onboardingModel         First-run setup
├── accountModel            Account detail/edit/rename (direct from dashboard card)
├── accountAddModel         Add new account form
├── credentialModel         Unified credential management (menu/type-select/setup)
├── discoveryModel          Repo discovery with multi-select
├── reposModel              Single repo detail + clone/pull/fetch/delete
├── mirrorsModel            Mirror detail + setup/discover/CRUD
├── settingsModel           Global folder, periodic sync, open in editor
└── identityModel           Global git identity check/removal
```

Helper files:

- `components.go` — reusable form/select components (formModel, selectField)
- `helpers.go` — shared credential/clone business logic (renameAccount, changeCredentialType, deleteCredential, reconfigureClones, etc.)

**Key pattern:** Every screen is a Bubble Tea `Model` with `Init()`, `Update(msg)`, `View()`. The root model routes between screens via `switchScreenMsg`. All business logic calls `pkg/` — never duplicate logic in the TUI.

### Windows console flash rule

Every `exec.Command` in the GUI binary (`cmd/gui/`) **MUST** call `git.HideWindow(cmd)` before `.Run()`, `.Output()`, or `.Start()`. This sets `SysProcAttr.HideWindow = true` on Windows, preventing a console window from flashing. The CLI binary does not need this. Always check for bare `exec.Command` calls in `cmd/gui/` after any change.

### TUI demo recordings

Use VHS (`charmbracelet/vhs`) to record terminal demo GIFs. Tape files live in `assets/`. See [developer-guide.md](docs/developer-guide.md) for details.

## Config format

Config file: `~/.config/gitbox/gitbox.json` — `accounts` + `sources`, real booleans, `gcm`/`ssh`/`token` credential types, nested SSH/GCM objects, `provider` field, `version: 2`. Optional `mirrors` section for push/pull mirror configuration between providers.

## Workflow orchestration

**Priority order when rules conflict**: Correctness > Simplicity > Elegance.

### Feature lifecycle

New features follow a structured pipeline managed by the `/wish` skill:

1. **Capture** — `/wish add <title>` records the idea in `.claude/context/feature-radar.md` with priority, size, and codebase notes
2. **Plan** — `/wish plan <id>` explores the codebase, designs the approach, and enters plan mode with a concrete implementation plan
3. **Build** — implement in plan mode, then verify with `/test-plan`
4. **Ship** — `/wish done <id>` marks the feature as shipped

Run `/wish` (no arguments) to see the current radar and get a suggestion on what to build next.

### 1. Plan first

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions)
- If something goes sideways, STOP and re-plan immediately
- Write detailed specs upfront to reduce ambiguity

### 2. Subagent strategy

- Use subagents liberally to keep main context window clean
- Offload research, exploration, and parallel analysis to subagents
- One task per subagent for focused execution

### 3. Verify before done

- Never mark a task complete without proving it works
- Test scripts with `bash -n` (syntax check) and `shellcheck` when available
- Go: `go vet ./...` + run the relevant test commands (see Testing section below)
- Validate config templates render correctly before committing
- After any command that writes config files, read the actual file on disk — never trust command output alone

### Testing

The project has a comprehensive test suite. Read `.claude/context/testing-patterns.md` for patterns, helpers, and naming conventions before writing new tests.

**After any code change, always run the relevant tests:**

| What changed | Command |
| --- | --- |
| `pkg/` (shared library) | `go test ./pkg/...` |
| `cmd/cli/tui/` (TUI screens, components) | `go test ./cmd/cli/tui/` |
| `cmd/cli/` (CLI commands) | `go test ./cmd/cli/` |
| Credential logic (`pkg/credential/`) | `go test ./pkg/credential/ ./cmd/cli/tui/ ./cmd/cli/` |
| Config logic (`pkg/config/`) | `go test ./pkg/config/ ./cmd/cli/tui/ ./cmd/cli/` |
| Update logic (`pkg/update/`) | `go test ./pkg/update/` |
| Unsure what's affected | `go test ./...` |

**Test levels:**

- **Unit tests** — always run, no credentials needed. TUI unit tests use manual message dispatch (`sendMsg`, `sendKey`).
- **Integration tests** (`TestIntegration_*`) — require `test-gitbox.json` at repo root with real provider credentials. Fail with a clear message when missing; skip with `-short`. TUI integration tests use teatest (real Bubble Tea event loop).
- **Scenario tests** (`TestScenario_*`) — full CLI lifecycle, also require fixture.

**When adding new features or fixing bugs:**

- Add or update tests that cover the change. Check `.claude/context/testing-patterns.md` for existing helpers before writing new ones.
- TUI screen changes: add unit tests using `newTestModel`/`initModel`/`sendMsg` helpers. For flows involving real credentials or API calls, add teatest integration tests.
- CLI command changes: add subprocess tests using `env.run()`/`env.runJSON()`.
- `pkg/` changes: add package-level tests.

**Run `go vet ./...` before committing** — it catches issues the test suite doesn't.

**Pre-push hook:** The repo includes `.githooks/pre-push` which runs `go vet` + `go test -short` before every push. Activate with `git config core.hooksPath .githooks`.

**Test plan:** The full pre-PR and release verification workflow is documented in `docs/testing.md`. If using Claude Code, the `/test-plan` skill automates the automated steps and guides through interactive ones.

### Update stakeholder documents

A feature is not complete until all affected documents are updated. Review this table after every change:

| Change type | Documents to review |
| --- | --- |
| New feature / behavior change | `docs/credentials.md`, `docs/cli-guide.md`, `docs/reference.md`, `docs/gui-guide.md` |
| New/changed tests | `docs/testing.md`, `.claude/context/testing-patterns.md` |
| New tooling (skill, hook, script) | `docs/developer-guide.md` |
| Repo structure change | `.claude/CLAUDE.md` (repository layout), `docs/README.md` (index) |
| All of the above | `.claude/CLAUDE.md` (relevant sections) |

### 4. Autonomous bug fixing

- When given a bug report: just fix it
- Point at logs, errors, failing tests — then resolve them

### 5. Learn from corrections

- After corrections from the user: save a `feedback` memory via the memory system

## Git commits

- **Never** include `Co-Authored-By: Claude` or any Claude attribution in commit messages
- **Never** modify git config for author/committer
- Commits should appear as the user's own work

## GitHub CLI (`gh`) account

This repo is owned by the `LuisPalacios` GitHub account. Before running any `gh` command (PR, issue, release, etc.), **always** switch to the correct account:

```bash
gh auth switch --user LuisPalacios
```

Do this at the start of any `gh` workflow — do not assume the active account is correct.

## Multiplatform testing

The developer workstation can be any OS. Remote machines are available via SSH for cross-platform testing. Every build-and-test cycle MUST cover all three platforms.

Connection details are in `.env` (gitignored). Copy `docs/.env.example` to `.env` and fill in your SSH hosts. See `docs/multiplatform.md` for the full setup guide.

| Platform | Env var | Arch | GOOS/GOARCH |
| --- | --- | --- | --- |
| Windows | `SSH_WIN_HOST` | amd64 | `windows/amd64` |
| macOS | `SSH_MAC_HOST` | arm64 | `darwin/arm64` |
| Linux | `SSH_LINUX_HOST` | amd64 | `linux/amd64` |

### Build-test-deploy cycle

Scripts in `scripts/` automate the full cycle:

```bash
./scripts/deploy.sh          # cross-compile all 3 + deploy to remotes
./scripts/smoke.sh all       # non-interactive smoke tests on all platforms
./scripts/test-commands.sh   # print interactive test-mode commands
./scripts/run-commands.sh    # print interactive production-mode commands
./scripts/setup-credentials.sh all  # set up SSH keys + tokens on all platforms
```

The scripts auto-detect the local OS. Local commands run directly, remote commands use SSH. See each script's header for usage.

### Non-interactive vs interactive tests

- **Non-interactive** (version, status, config show, JSON output): Claude runs via `./scripts/smoke.sh` or directly via SSH.
- **Interactive** (TUI, credential prompts, init wizard): Claude uses `./scripts/test-commands.sh` or `./scripts/run-commands.sh` to print the exact commands. The user runs them in their own terminal.

## Screenshots for debugging

When the user says "I took a screenshot" or "check my screenshot", find and read the latest screenshot file:

- **Windows:** `~/Pictures/Screenshots/`
- **macOS:** `~/Desktop/`

Use file modification time (not filename) to find the most recent one. Sort by `mtime` descending and read the latest (or more if the user indicates). Example:

```bash
# Windows
ls -t "$HOME/Pictures/Screenshots/"*.png | head -1
# macOS
ls -t ~/Desktop/*.png | head -1
```

Read the file with the Read tool (it supports images). Then analyze the TUI output and report what you see.
