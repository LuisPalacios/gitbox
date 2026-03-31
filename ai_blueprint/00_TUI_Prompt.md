# TUI Implementation Prompt

> Gold-standard example of an implementation prompt for a new `pkg/` consumer.
> This prompt was used to build the Bubble Tea TUI embedded in the CLI binary.
> Use its structure as a template when creating prompts for other consumers
> (web UI, Slack bot, etc.) — see REFERENCE.md for the required sections.

````text
## Execution Instructions

Enter plan mode immediately. Complete the entire plan without asking
questions — you have full authorization to read any file in the repo.
Do NOT exit plan mode or begin implementation. Your deliverable is a
comprehensive, step-by-step implementation plan covering all phases.
If any requirement is ambiguous, state your assumption and continue.
Do NOT ask the user to confirm — just document the assumption in the plan.

---

Act as a senior Go developer and TUI architect. Your objective is to add a
full-featured TUI (Terminal User Interface) mode to the `gitbox` binary using
the Charm stack: Bubble Tea, Lip Gloss, Bubbles, and Huh.

## Context

gitbox is a Go monorepo that manages multi-account Git environments across
providers (GitHub, GitLab, Gitea, Forgejo, Bitbucket). It currently has:

- `cmd/cli/` — CLI binary (Cobra), named `gitbox`
- `cmd/gui/` — GUI binary (Wails v2 + Svelte), named `GitboxApp`
- `pkg/` — Shared Go library (config, credential, provider, git, status,
  mirror, identity)

Both binaries consume `pkg/` with zero presentation coupling. The TUI will be
embedded in the CLI binary, making it the third consumer of the shared library.

## Target behavior

After this change the CLI binary supports two modes:

```text
gitbox                        → TUI mode (interactive terminal dashboard)
gitbox <any arguments>        → CLI mode (current behavior, non-interactive)
gitbox status                 → CLI mode
gitbox mirror discover        → CLI mode
gitbox --help                 → CLI mode (shows help)
gitbox --version              → CLI mode (shows version)
```

**Detection logic:** If `len(os.Args) == 1` and stdin is a terminal → launch
TUI. Otherwise → run CLI commands via Cobra as today. No `--tui` or `--no-tui`
flags needed. The presence of any argument means CLI mode.

```go
func main() {
    if len(os.Args) == 1 && isTerminal() {
        if err := tui.Run(); err != nil {
            fmt.Fprintf(os.Stderr, "error: %v\n", err)
            os.Exit(1)
        }
        return
    }
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}

func isTerminal() bool {
    fi, err := os.Stdin.Stat()
    if err != nil {
        return false
    }
    return fi.Mode()&os.ModeCharDevice != 0
}
```

## Blueprint reference

Before writing any code, read the entire `ai_blueprint/` directory:

| File | What to extract |
| --- | --- |
| `01_Architecture.md` | System context, container relationships, which `pkg/` packages the TUI will call |
| `02_Database.dbml` | Every persisted and runtime type. The **API Surface** table group documents function signatures, error semantics, and calling sequences — follow these exactly |
| `03_API_Contract.yaml` | Section B (Wails bindings) defines every operation the GUI supports. The TUI must offer equivalent functionality. The `x-events` extensions document async event schemas — translate these to `tea.Cmd` + `tea.Msg` patterns |
| `04_Features/*.feature` | Gherkin scenarios are your acceptance criteria. Pay attention to `@error` and `@empty` scenarios — implement these paths, not just the happy path |
| `05_UI_Design.json` | Design tokens (colors, status palettes, typography, symbols). Apply via Lip Gloss styles |

Also read `docs/architecture.md` and `docs/developer-guide.md` for build
conventions and platform requirements.

## Phased implementation

Do NOT implement everything in one pass. Break the work into phases with
mandatory verification gates between them.

### Phase 1: Scaffold + dashboard (MVP)

**Scope:** Entry point detection, root model (screen router), dashboard screen
with account list showing repo counts and status summary. One working screen
that loads real data from `pkg/config` and `pkg/status`.

**Package structure:**

```text
cmd/cli/tui/
  tui.go           — Run() entry point, config loading, program launch
  model.go         — Root model (router between screens)
  dashboard.go     — Main screen: account cards, repo list with status
  keys.go          — Shared key bindings
  styles/
    colors.go      — Lip Gloss styles from 05_UI_Design.json tokens
```

**Verification gate (mandatory before proceeding):**

```bash
go build -o build/gitbox ./cmd/cli        # Must compile
go vet ./cmd/cli/...                       # Must pass
# Manual: run build/gitbox with no args — dashboard must render with real data
# Manual: run build/gitbox status — CLI mode must still work
```

### Phase 2: Core screens

**Scope:** Add remaining screens one at a time. After adding each screen,
verify that it builds and the dashboard still works.

Screens in priority order:

1. **Onboarding** — First-run setup (Huh form wizard)
2. **Account management** — Add, edit, rename, delete (Huh forms)
3. **Credential setup** — Token/GCM/SSH wizard with verification
4. **Repository discovery** — Fetch from provider, filterable multi-select
5. **Repo detail** — Branch, ahead/behind, changed files, clone/pull/fetch
6. **Mirror management** — Groups, status, setup, discover
7. **Settings** — Global folder, periodic sync
8. **Identity** — Global identity warning and fix

**Verification gate (after each screen):**

```bash
go build -o build/gitbox ./cmd/cli
go vet ./cmd/cli/...
# Manual: navigate to the new screen, test happy path + one error case
```

### Phase 3: Polish

**Scope:** Keyboard shortcuts help overlay, dark/light theme toggle, compact
view mode, auto-refresh timer, status bar with sync summary.

**Verification gate:**

```bash
go build -o build/gitbox ./cmd/cli
go vet ./cmd/cli/...
# Manual: full walkthrough of all screens
# Manual: test on Windows Terminal, macOS Terminal, Linux terminal
```

## pkg/ usage rules (critical)

These rules prevent the most common AI-generation bugs. Violating any of them
is a build-blocking issue.

### Never duplicate pkg/ logic

Before writing any business logic, search `pkg/` for existing helpers.
Common functions you MUST use (not reimplement):

- `credential.SSHHostAlias(accountKey)` — not `"gitbox-" + accountKey`
- `credential.VerifyToken(...)` — not manual HTTP calls
- `config.Save(cfg, path)` — not manual JSON marshaling
- `identity.EnsureRepoIdentity(...)` — not manual git config writes

### Always check error returns

Every function that returns `error` must have its error checked. No blank
identifier `_` for error values. If an error is truly ignorable, add a
comment explaining why:

```go
// Good
if err := credential.DeleteToken(key); err != nil {
    return fmt.Errorf("delete token: %w", err)
}

// Bad
_ = credential.DeleteToken(key)

// Acceptable only with justification
// Token may not exist yet; deletion is best-effort cleanup.
if err := credential.DeleteToken(key); err != nil {
    log.Printf("cleanup: %v", err)
}
```

### Use the API Surface from 02_Database.dbml

The DBML file's "API Surface" table group documents function signatures,
error semantics, and calling sequences. Follow them. If the DBML says
"Must call A before B", call A before B.

### Type-safe field access

When a struct field can hold different kinds (union-like), always check the
kind before accessing kind-specific fields:

```go
// Good — check before access
if field.Kind == fieldText {
    field.TextInput.Focus()
}

// Bad — crashes if field is a select, not text
field.TextInput.Focus()
```

## Async pattern (Bubble Tea)

All long-running operations (clone, pull, fetch, discover, mirror setup) MUST
use this pattern. Do NOT invent a different async approach.

```go
// 1. Define a message type for the result
type cloneResultMsg struct {
    repoKey string
    err     error
}

// 2. Return a tea.Cmd that runs the operation in a goroutine
func cloneRepoCmd(cfg *config.Config, accountKey, repoKey string) tea.Cmd {
    return func() tea.Msg {
        err := git.Clone(/* params from cfg */)
        return cloneResultMsg{repoKey: repoKey, err: err}
    }
}

// 3. Handle the message in Update()
case cloneResultMsg:
    if msg.err != nil {
        m.status = fmt.Sprintf("Clone failed: %v", msg.err)
    } else {
        m.status = fmt.Sprintf("Cloned %s", msg.repoKey)
    }
```

For operations with progress (clone, fetch-all), use a channel-based pattern:

```go
type progressMsg struct {
    current int
    total   int
    label   string
}

type doneMsg struct {
    results []string
    err     error
}

func fetchAllCmd(repos []config.Repo) tea.Cmd {
    return func() tea.Msg {
        // This is simplified — in practice, use tea.Batch or
        // a subscription (tea.Sub) for streaming progress.
        var results []string
        for i, r := range repos {
            err := git.Fetch(r.Path)
            results = append(results, fmt.Sprintf("%s: %v", r.Key, err))
        }
        return doneMsg{results: results}
    }
}
```

## State management rules

### Screen initialization

Every screen's `Init()` must set up a clean initial state. Never carry state
from a previous visit to the same screen.

### Config reload after mutation

Any screen that calls `config.Save()` (account add, repo discovery, settings
change, mirror CRUD) must signal the root model to reload config when
returning to the dashboard. Use a message:

```go
type configChangedMsg struct{}
```

The root model handles this by reloading config and reinitializing the
dashboard:

```go
case configChangedMsg:
    cfg, _ := config.Load(m.configPath)
    m.cfg = cfg
    m.dashboard = newDashboardModel(cfg, m.theme, m.width, m.height)
```

### Navigation cleanup

When returning from a sub-screen (Esc or completion), the sub-screen must
not leave modal state (confirmations, spinners, error banners) that bleeds
into the parent screen.

## Styling

- Use Lip Gloss for all styling. Extract colors from `05_UI_Design.json`
- Support dark and light themes, toggled at runtime
- Use the exact status symbols from the design tokens (●, ◗, ◆, ▲, ○, ~,
  ⚠, ⚡, ✕)
- Use the same status color semantics: green=clean, magenta=behind,
  orange=dirty, blue=ahead, red=error/conflict/diverged, gray=not-cloned
- Terminal symbol safety: all symbols used are in the BMP (Basic Multilingual
  Plane) and render correctly on Windows Terminal, macOS Terminal, and common
  Linux terminals. If you add new symbols, verify BMP compatibility

## Functional parity with GUI

Map every Wails binding from Section B of `03_API_Contract.yaml` to a TUI
interaction. Reference `04_Features/gui.feature` for the specific user flows.
Key screens:

- **Dashboard**: All accounts with repo counts and sync status. Mirror
  summary. Bubbles table with status colors. Full/compact toggle
- **Onboarding**: Huh form wizard for first-run setup
- **Account management**: Huh forms for add/edit. Inline actions (edit,
  delete, credential type change). List organizations/groups
- **Credential setup**: Multi-step wizard — Token: paste + verify; GCM:
  trigger OAuth + show result; SSH: generate key + display public key
- **Repository discovery**: Provider API fetch, filterable multi-select,
  add to config
- **Repo detail**: Branch, ahead/behind, changed/untracked files. Actions:
  clone, pull, fetch, delete
- **Mirror management**: Group list, per-repo status, setup, discover with
  confidence levels, apply discovered mirrors
- **Settings**: Global folder, periodic sync interval, open config in editor
- **Identity**: Global identity warning, removal action

## Architecture patterns

- **Elm architecture**: Every screen is a Bubble Tea Model with Init, Update,
  View. The root model routes between screens via messages
- **Shared pkg/ only**: Import from `pkg/`. Never duplicate business logic
- **Config persistence**: After any mutation, call `config.Save()` immediately
- **Keyboard navigation**: j/k or arrows for lists. Enter to select. q/Esc
  to go back. ? for help overlay

## Build integration

The TUI is embedded in the CLI binary — single build target:

```bash
go build -o build/gitbox ./cmd/cli
```

Add TUI dependencies to go.mod:

```text
github.com/charmbracelet/bubbletea
github.com/charmbracelet/lipgloss
github.com/charmbracelet/bubbles
github.com/charmbracelet/huh
```

## Platform rules

- Must work on Windows (Git Bash, Windows Terminal), macOS, and Linux
- Must work over SSH sessions (no GUI dependencies)
- Test on all three platforms per the project's multiplatform testing protocol

## Testing protocol

After each phase, run this sequence:

```bash
# Build (must succeed)
go build -o build/gitbox ./cmd/cli

# Vet (must pass)
go vet ./cmd/cli/...

# Unit tests (must pass)
go test ./pkg/...

# Manual: TUI launches
build/gitbox

# Manual: CLI still works
build/gitbox --help
build/gitbox status

# Manual: piped stdin doesn't launch TUI
echo "" | build/gitbox
```

Cross-platform testing after Phase 2:

```bash
GOOS=darwin GOARCH=arm64 go build -o build/gitbox-darwin-arm64 ./cmd/cli
GOOS=linux  GOARCH=amd64 go build -o build/gitbox-linux-amd64  ./cmd/cli
```

## What NOT to do

- Do NOT modify any code in `pkg/`. If you need something `pkg/` doesn't
  provide, propose the addition separately
- Do NOT modify `cmd/gui/`. The GUI binary is unchanged
- Do NOT add `--tui` or `--no-tui` flags. Detection is implicit
- Do NOT create a separate `cmd/tui/` binary. The TUI lives inside `cmd/cli/`
- Do NOT use blank `_` for error returns without a justifying comment
- Do NOT hardcode strings that `pkg/` provides as helper functions
- Do NOT implement all screens in one pass. Follow the phased approach
- Do NOT skip verification gates between phases
````
