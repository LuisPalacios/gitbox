# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## What this repo is

A multi-component project for managing Git multi-account environments across providers (GitHub, GitLab, Gitea, Forgejo, Bitbucket).

**Go app** (`cmd/gui/`, `pkg/`) — the Wails GUI `GitboxApp` on top of a shared Go library. Since v2 the GUI is the only interface; the v1 CLI + TUI live on the `release/v1` branch (1.x tags).

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

## Documentation language and translations

English is the main documentation language and the source of truth for this project. The root `README.md` and files under `docs/` in English define the canonical structure, paragraphs, examples, warnings, and level of detail.

When translating documentation into any supported language, preserve the English source's content and structure:

- Treat root-level translations such as `README.es.md` as full counterparts of the root `README.md`, not as language-specific summaries.
- Every canonical English Markdown document in the root or under `docs/` must have a counterpart in each supported language, such as `README.es.md` or `docs/es/<name>.md`, unless the document is explicitly marked private or noncanonical.
- Translate the same headings, paragraphs, lists, examples, warnings, and links in the same order.
- Keep commands, flags, config keys, provider names, status values, file paths, and JSON fields unchanged unless the English source itself changes them.
- Do not create lower-quality summaries, shorter rewrites, expanded rewrites, or improved variants in translated docs. Translation quality must match the English source, neither worse nor better.
- If the English source changes, update each translated counterpart from that English source instead of inventing a separate structure.
- If a translated doc cannot stay in parity during a change, leave a clear note in the work summary and do not present it as complete.

## Repository layout

```text
cmd/
  gui/                    Wails v2 + Svelte GUI (GitboxApp), the only binary
pkg/                      Shared Go library
  ops/                    Account, credential, clone and discovery service layer the GUI calls (+ TestScenario_FullLifecycle)
  config/                 Config v3 model, load/save, v1→v2→v3 migration
  credential/             Credential verification (GCM, SSH, token); global GCM helper check accepts any helper resolving to git-credential-manager (short name or absolute path)
  git/                    Git subprocess operations (os/exec); global config get/get-all/set/unset
  provider/               Provider API clients + mirror interfaces (GitHub, GitLab, Gitea, Forgejo)
  mirror/                 Push/pull mirror setup, status, guides
  workspace/              Read-only VS Code .code-workspace discovery + cache (RefreshCache); no generation, no tmuxinator
  adopt/                  Orphan repo discovery + adoption (multi-root scan, container nested-clone discovery, in-place absolute clone_folder)
  status/                 Clone status checking
  update/                 Auto-update: version check, download, self-replace (AppImage build is notify-only)
  doctor/                 External-tool detection (git, GCM, ssh, …): point-of-use precheck + the GUI's tool check; `LookupIn` is the Setenv-free resolver (extra dirs, ~/.local/bin, Windows shims) shared by harness detection
  harness/                Embedded AI-tool catalog (tools-directory.md: alternates + well-known locations) + pure `Sync` that flags uninstalled harnesses `missing` instead of deleting them
  identity/               Global `~/.gitconfig` user.name/user.email detection + removal
  gitignore/              Global `~/.gitignore_global` managed-block install with sentinel dedup + timestamped backups
docs/
  README.md               Documentation index
  gui-guide.md            GUI guide
  developer-guide.md      Build instructions, contributing
  architecture.md         Technical design
  credentials.md          Credential setup guide
  testing.md              Test levels, fixture format, pre-PR and release checklists
  testing-reference.md    Test inventory, harness internals
  multiplatform.md        Remote hosts (.env) for cross-platform testing
  macos-signing.md        macOS code signing setup
  worktree-workflow.md    Parallel worktree sessions (/work-issue, /merge-pr)
  agentic-tools-directory.md  AI harness / agentic tools directory
  es/                     Spanish counterparts of every doc above
  diagrams/               Architecture diagrams
assets/                   Icons, logo, README screenshots (captured with scripts/demo-fleet)
scripts/
  bootstrap.sh             Cross-platform installer (downloads a release, places GitboxApp, registers Linux menu entry; --cli-only installs the latest 1.x CLI)
  register-gitbox.sh       Linux-only desktop registrar (.desktop + icon in ~/.local/share, idempotent, supports --uninstall)
  installer.iss            Windows Inno Setup installer script (GUI only; cleans up v1's CLI and PATH entry on upgrade)
  appimage/                Linux AppImage build (build-appimage.sh + linuxdeploy, AppRun, desktop, AppStream metainfo, icon)
  dmg/                     macOS DMG installer script + README
  _common.sh               Shared helpers for the dev scripts (.env, SSH, GitboxApp paths)
  ship.sh                  Build GitboxApp on each remote host in .env and stage it
  smoke.sh                 Run `GitboxApp --version` locally and on staged remotes
  health.sh                Read-only run of every checker (gofmt, vet, staticcheck, modernize, govulncheck, deadcode, tests, svelte-check, frontend build warnings, npm ci warnings, npm audit, shellcheck, actionlint, markdownlint)
  test-commands.sh         Print GitboxApp --test-mode launch commands per platform
  run-commands.sh          Print GitboxApp launch commands per platform
  setup-credentials.sh     Run test-setup-credentials.sh locally or on remotes
  test-setup-credentials.sh  Provision test credentials from test-gitbox.json
  send-my-production-config.sh  Copy the local gitbox.json to one remote (with diff + confirm)
  demo-fleet/              Fake fleet + mock provider API + window capture for README screenshots (build.sh, run.sh, mock.py, shot.ps1)
.githooks/pre-push        Pre-push hook (gofmt -s check + go vet + unit tests; builds the frontend if dist is missing)
.claude/
  CLAUDE.md               Canonical agent guidance (this file)
  context/
    guideline_codex.md    Dual Claude/Codex wiring (AGENTS.md + .agents/skills symlinks)
    guideline_js.md       JS/TS script conventions
    guideline_python.md   Python script conventions
    guideline_skills.md   Skill-authoring quick reference
    testing-patterns.md   Test helpers, naming conventions
  skills/
    fixing-markdown/      Markdown lint + format
    merge-pr/             Merge a PR (post-/work-issue), clean up worktree + branch
    preview-prototype/    Local Svelte preview server
    screenshot-prototype/ GUI screenshot generation
    ship-builds/          Build + stage GitboxApp on remote hosts in .env
    test-plan/            Pre-PR and release verification
    work-issue/           Worktree → plan → code → push → PR (stops before merge)
  rules/
    skills-authoring.md   Skill creation guidelines
AGENTS.md                 → .claude/CLAUDE.md (symlink — Codex reads project guidance here)
.agents/skills            → ../.claude/skills (symlink — Codex sees skills here)
.github/workflows/ci.yml  Release CI on v* tags: test, build, release (+ installers, DMGs, AppImage); workflow_dispatch = dry run without publishing
.github/workflows/pr.yml  PR + main CI: frontend build, scripts/health.sh gate (every checker, fails on any finding), Linux GUI build smoke
json/
  gitbox.schema.json      v3 JSON Schema
  gitbox.jsonc            v3 annotated example (Spanish comments)
go.mod / go.sum           Go module
README.md                 Project overview
```

## Dual Claude / Codex setup

This repo is wired so Claude Code and Codex share one source of truth without duplication. `.claude/CLAUDE.md` is the canonical guidance file — Claude Code reads it directly, and Codex reads `AGENTS.md` at the repo root, which is a symlink back to it. Skills live under `.claude/skills/` and are exposed to Codex through `.agents/skills` (symlink to `../.claude/skills`).

If the symlinks ever go missing or get checked out as plain files, follow the repair procedure in [`.claude/context/guideline_codex.md`](context/guideline_codex.md). On Windows, create them with `mklink` from cmd.exe — Git Bash's `ln -s` produces file copies, not symlinks.

## Go app

**Language:** Go. **GUI:** Wails v2 + Svelte. One binary, `GitboxApp`, on top of the shared `pkg/` library. Business logic lives in `pkg/` (account, credential, clone and discovery flows in `pkg/ops`); `cmd/gui` is Wails glue.

### Binary naming

The GUI binary is `GitboxApp`. The v1 CLI/TUI binary was `gitbox` — a different name, so the two never collided on the case-insensitive NTFS filesystem when shipped side by side. The CLI/TUI is maintained on the `release/v1` branch and installed with `scripts/bootstrap.sh --cli-only` (latest 1.x).

| Platform | GUI             |
| -------- | --------------- |
| Windows  | `GitboxApp.exe` |
| macOS    | `GitboxApp.app` |
| Linux    | `GitboxApp`     |

`GitboxApp --version` prints `GitboxApp <version> (<sha>)` and exits without opening a window, so scripts and CI use it as a smoke check.

### Build commands

```bash
# Build-time version stamp. Without -ldflags the GUI launched from Finder
# shows "dev-none" (runtime git fallback can't find .git when the app's CWD
# is outside the repo). Matches the values CI and scripts/ship.sh inject.
LDFLAGS="-X main.version=$(git describe --tags --always)-dev -X main.commit=$(git rev-parse --short HEAD)"

# cmd/gui embeds frontend/dist (go:embed), so `go build ./...`, `go vet ./...`
# and `go test ./...` need the frontend built once (wails build also does it).
(cd cmd/gui/frontend && npm ci && npm run build)

# Build GUI (requires Wails CLI)
# ALWAYS copy icons before building — they are not checked in under cmd/gui/build/
cp assets/appicon.png cmd/gui/build/appicon.png
cp assets/icon.ico    cmd/gui/build/windows/icon.ico
(cd cmd/gui && wails build -ldflags "$LDFLAGS")   # Linux: add -tags webkit2_41
# macOS: export CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0
# first (Go's runtime minimum; cmd/gui/build/darwin/Info.plist declares the same 13.0)
# Output: cmd/gui/build/bin/GitboxApp[.exe] (GitboxApp.app on macOS)

# Smoke check
cmd/gui/build/bin/GitboxApp --version        # macOS: GitboxApp.app/Contents/MacOS/GitboxApp

# Run tests
go test ./pkg/...              # shared library (pkg/ops holds the lifecycle scenario)
go test ./cmd/gui/             # Wails-bound App methods
go test ./...                  # everything
```

Git operations shell out to system `git` via `os/exec`. Provider APIs use `net/http`.

### Windows console flash rule

Every `exec.Command` in the GUI binary (`cmd/gui/`) **MUST** call `git.HideWindow(cmd)` before `.Run()`, `.Output()`, or `.Start()`. This sets `SysProcAttr.HideWindow = true` on Windows, preventing a console window from flashing. Always check for bare `exec.Command` calls in `cmd/gui/` after any change.

**Launching a visible terminal** (e.g. `OpenInTerminal`) is a special case: a GUI parent has no console, and Go's exec inherits null stdio to the child with `STARTF_USESTDHANDLES` set, so plain console apps (`cmd.exe`, `pwsh.exe`, etc.) see closed stdin and exit. Solution: wrap the launch in `cmd.exe /C start "" /D <path> <command> <args...>`. `start` creates a fresh console for the terminal; `HideWindow` hides the intermediate `cmd.exe` wrapper so the rule above still holds.

## Config format

Config file: `~/.config/gitbox/gitbox.json` — `accounts` + `sources`, real booleans, `gcm`/`ssh`/`token` credential types, nested SSH/GCM objects, `provider` field, `version: 3`. Optional `mirrors` section for push/pull mirror configuration between providers.

**Versioning & migration:** `config.CurrentVersion` is 3. `config.Parse` loads v3 strictly and migrates v2 transparently on load (in-memory; persisted on next save). The v2→v3 migration bumps the version and **discards the `workspaces` section wholesale** (it is now a regenerable discovered cache); accounts, sources, mirrors, and global settings are preserved; legacy `global.terminals` is converted by the existing terminal migrator. Any other version is an error.

**v3 keys:**

- `global.extra_folders` (`[]string`) — additional root directories scanned for clones and `.code-workspace` files, on top of `global.folder`. Onboard repos outside the standard `folder/account/org/repo` tree.
- `global.nested_scan_depth` (`int`, default 1 via `NestedScanDepthOrDefault`) — how many levels gitbox descends below a container repo to find nested clones.
- `repo.container` (`bool`) — marks a clone as a multi-repo parent; gitbox descends into its working tree to discover and onboard nested clones the user provisioned there. Nested clones onboard as plain repos under their real account/org with an absolute `clone_folder`, never relocated. Parent→child nesting is derived from paths by `status.ComputeNesting`.
- `repo.clone_folder` (existing) — an absolute value (`/`, `~`, `../`) places a clone anywhere on disk; this is how non-standard and nested clones are stored.

**Workspaces are read-only** (`version: 3`): gitbox discovers existing VS Code `.code-workspace` files, caches them under `workspaces` (all `discovered: true`, no `type`/`layout`), lists them, and opens them. It never creates, edits, generates, or deletes them, and tmuxinator support is removed. `pkg/workspace.RefreshCache` rebuilds the cache; `Discover` is pure.

## Workflow orchestration

**Priority order when rules conflict**: Correctness > Simplicity > Elegance.

### Feature lifecycle

The backlog lives on GitHub at [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues). Features use the `enhancement` label (plus `priority:P1` when they're up next); bugs use the `bug` label. Size and severity are captured in the issue body.

Flow: open an issue → discuss/plan in comments → implement with plan mode → reference the issue in the commit message → close on merge. Use `gh issue list --label enhancement` or `gh issue view <n>` from the CLI; remember to `gh auth switch --user LuisPalacios` first.

### Push to main vs branch + PR

Pick per task. Default to branch + PR when in doubt — the cost of a PR for a solo maintainer is trivial, the cost of a bad push to main is a revert.

**Push directly to main** when ALL of these hold:

- The change is one file (or a tightly-coupled 2-file edit like config + its test).
- The fix or addition is mechanically obvious — a typo, a one-line bug fix, a doc tweak, a `HideWindow` call that was clearly missing.
- `go vet ./...` and the relevant focused tests pass locally.
- No new public API, no behavior change that another platform might surface differently, no risky refactor.

Reference the issue in the commit with `Closes #N` — GitHub auto-closes on push.

**Branch + PR to self-merge** for everything else: multi-file features, anything touching `pkg/` public surface, refactors, UI changes, anything that benefits from seeing the full diff at once or letting CI gate the merge. The PR body is where I narrate the change (and where CI runs) — I am allowed to self-approve and merge immediately. Branch names follow `<type>/<issue>-<slug>`, e.g. `fix/31-ide-flash` or `feat/22-open-in-terminal`. Always use `gh pr create --body "Closes #N\n\n..."` so the issue closes on merge.

**External contributions** (anyone who is not me): always come through PRs from forks — I review, CI must pass, then merge.

### Never push without asking first

Local commits are fine — commit freely whenever a logical unit is ready. **But any action that makes work visible on GitHub requires the user's explicit go-ahead first.** That covers `git push origin main`, `git push origin <branch>`, `gh pr create`, `gh pr merge`, and any edit to an existing issue/PR/comment. No exemptions for "just a doc fix" or "just a typo" — publication is the point of no return, and the cost of one confirming message is zero.

The protocol has two shapes depending on what was changed:

**Runtime-affecting changes** (code under `cmd/` or `pkg/`, build config, UI behaviour):

1. Finish the implementation locally: commits on the fix branch (or staged on main), `go vet ./...` clean, focused tests passing, **the GUI built** per the build rule.
2. Stop and send one short message with:
   - Which commit(s) or branch are ready and the binary path to launch (e.g. `cmd/gui/build/bin/GitboxApp.exe`).
   - A concrete check: what to click, what output to expect, what regression to rule out ("open Preferences → Open In Editor, confirm no cmd.exe flash").
   - The exact publish command I intend to run next (`git push origin main` or `gh pr merge <n> --squash`) — described, not executed.
3. Wait. Do not push, do not merge, do not `gh pr create` with auto-merge. If the user reports a regression, fix it and re-offer the build — never ship over an unresolved user-reported failure.
4. Only run the publish step after an explicit "ok, push it" / "go ahead" / "ship it".

**Trivial or doc-only changes** (`.md`, `docs/`, comments, `.claude/` metadata, `.gitignore`, `go.mod` tidy, rename-a-test):

1. Commit locally with a clear message.
2. Stop and ask: "Commit `<sha>` ready — want me to push directly / open a PR, or hold?" One sentence, no ceremony.
3. Wait for the answer before any push or PR action.

The only pre-authorisation that bypasses this is an explicit, in-session "ship on green" for a specific task ("just push it when tests pass"). A blanket "you have my approval" from a previous session does not count — authorisation applies to the scope it was granted for.

When in doubt, ask. It is never wrong to pause; it is sometimes wrong to push.

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
- **Build the GUI** after any code change to `cmd/` or `pkg/`. `go build ./...` alone skips the Wails build tags and the frontend build, so a change that compiles there can still break `wails build`. Quick compile-check during iterative edits: `go build ./...` (needs `cmd/gui/frontend/dist`, see Build commands). Full build before reporting done or pushing: `cd cmd/gui && wails build` (copy icons first — see Build commands), then `GitboxApp --version` on the result. Doc-only and non-Go changes are exempt.
- Validate config templates render correctly before committing
- After any command that writes config files, read the actual file on disk — never trust command output alone

### Testing

The project has a comprehensive test suite. Read `.claude/context/testing-patterns.md` for patterns, helpers, and naming conventions before writing new tests.

**After any code change, always run the relevant tests:**

| What changed                                | Command                                           |
| ------------------------------------------- | ------------------------------------------------- |
| `pkg/` (shared library)                     | `go test ./pkg/...`                               |
| Account/credential/clone flows (`pkg/ops/`) | `go test ./pkg/ops/`                              |
| Credential logic (`pkg/credential/`)        | `go test ./pkg/credential/ ./pkg/ops/ ./cmd/gui/` |
| Config logic (`pkg/config/`)                | `go test ./pkg/config/ ./pkg/ops/ ./cmd/gui/`     |
| GUI bindings (`cmd/gui/`)                   | `go test ./cmd/gui/`                              |
| Update logic (`pkg/update/`)                | `go test ./pkg/update/`                           |
| Doctor / tool detection (`pkg/doctor/`)     | `go test ./pkg/doctor/`                           |
| Unsure what's affected                      | `go test ./...`                                   |

`cmd/gui` tests (and `./...`) need `cmd/gui/frontend/dist` — build the frontend once if it is missing.

**Test levels:**

- **Unit tests** — always run, no credentials needed. Package-level tests in `pkg/`, isolated with `t.TempDir()` and `t.Setenv()`; `cmd/gui` tests call `App` methods directly, without the Wails runtime.
- **Integration tests** (`TestIntegration_*`) — require `test-gitbox.json` at repo root with real provider credentials. Fail with a clear message when missing; skip with `-short`.
- **Scenario test** (`TestScenario_FullLifecycle` in `pkg/ops`) — the full account → clone → mirror → rename → delete lifecycle against a real provider, also gated by the fixture.

**When adding new features or fixing bugs:**

- Add or update tests that cover the change. Check `.claude/context/testing-patterns.md` for existing helpers before writing new ones.
- `pkg/` changes: add package-level tests. Account, credential, clone and discovery flows go in `pkg/ops` (helpers: `isolate`, `makeClone`, `gitRun`).
- Flows that need real credentials or API calls: gate them with `requireIntegration(t)`, or extend `TestScenario_FullLifecycle` with a new step.
- GUI binding changes: add `cmd/gui` tests that build an `App` and call the method.

**Run `go vet ./...` before committing** — it catches issues the test suite doesn't.

**Health check:** `./scripts/health.sh` runs every checker read-only (Go analyzers at pinned versions, svelte-check with unused-code errors, frontend build and npm ci warnings, npm audit, shellcheck, actionlint, markdownlint). Run it before every PR; `pr.yml` runs it as the CI gate and fails on any finding (including new govulncheck / npm audit advisories). Never run the `fixing-markdown` markdownlint config directly for a check — it has `"fix": true` and rewrites files.

**Pre-push hook:** The repo includes `.githooks/pre-push` which runs a `gofmt -s -l` check, `go vet` and `go test -short` before every push, building the frontend first when `cmd/gui/frontend/dist` is missing. Run `gofmt -s -w` on changed Go files before committing. Activate with `git config core.hooksPath .githooks`.

**Test plan:** The full pre-PR and release verification workflow is documented in `docs/testing.md`. If using Claude Code, the `/test-plan` skill automates the automated steps and guides through interactive ones.

### Update stakeholder documents

A feature is not complete until all affected documents are updated. Review this table after every change:

| Change type                       | Documents to review                                                                   |
| --------------------------------- | ------------------------------------------------------------------------------------- |
| New feature / behavior change     | `docs/gui-guide.md`, `docs/credentials.md`, `docs/architecture.md`                    |
| New/changed tests                 | `docs/testing.md`, `docs/testing-reference.md`, `.claude/context/testing-patterns.md` |
| New tooling (skill, hook, script) | `docs/developer-guide.md`, `docs/multiplatform.md` (dev scripts)                      |
| Repo structure change             | `.claude/CLAUDE.md` (repository layout), `docs/README.md` (index)                     |
| All of the above                  | `.claude/CLAUDE.md` (relevant sections)                                               |

Every English doc under `docs/` has a counterpart under `docs/es/`; update both (see "Documentation language and translations").

### 4. Autonomous bug fixing

- When given a bug report: just fix it
- Point at logs, errors, failing tests — then resolve them

### 5. Learn from corrections

- After corrections from the user: save a `feedback` memory via the memory system

## Worktree-based parallel workflow

For multi-issue work where several Claude sessions run in parallel, use the `/work-issue <N>` skill. It creates a sibling worktree at `../gitbox-<N>-<slug>`, drives the plan → code → auto-test → user-smoke-test → sync → push → PR flow, and stops before merge. `/merge-pr <PR#>` handles rebase check, CI gate, merge, and worktree cleanup as a separate, deliberate action.

If I already have a plan written (in the issue body, a local file, or a URL), I can pass it with `/work-issue <N> <plan-source>`. The skill evaluates completeness: adopts it if it covers files-to-change, verification, and key decisions; extends it via plan mode if partial; falls back to full plan mode if absent.

Cross-cutting rules when a session is running inside a worktree:

- Stay in the worktree directory. Never edit files outside it, never check out a different branch in it.
- Global state is shared across worktrees: `~/.config/gitbox/gitbox.json`, system git config, GCM, SSH agent. Never run interactive credential or init-wizard flows in two sessions at once.
- Before any `gh` command, derive the expected user from this clone's `remote.origin.url` (not a hardcoded name) and switch with `gh auth switch` if needed. The skill does this automatically.
- If the worktree path falls under a gitbox-managed folder, expect GitboxApp's status and discovery views to list it as an orphan. It's not actually orphaned — just unrecognised by the scanner. Safe to ignore.

See [docs/worktree-workflow.md](../docs/worktree-workflow.md) for the full walkthrough.

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

## Anonymize before posting to GitHub

Anything I publish to a GitHub-visible surface — issue body, issue comment, PR title/body/comment, release notes, commit messages that get pushed — **must not contain private details from the user's local development setup**. GitHub preserves edit history on public issues and comments; a manual edit after posting does NOT remove the leak. The only way to truly delete a leak is to delete the issue or comment entirely. Prevention is the only reliable strategy.

**Always strip or replace with placeholders:**

- Local filesystem paths: `~/00.git/...`, `C:\Users\<name>\...`, `/Users/<name>/...`, `/home/<name>/...`
- Account keys as they appear in `gitbox.json`: `github-<realname>`, `gitlab-<company>`, etc. Use `github-MyAccount`, "account A", "account B"
- Real organization / user / private-repo names the user has in their config (company orgs, client names, private projects)
- Workstation hostnames, SSH host aliases, IP addresses, email addresses, personal names

**Fine to include** — public references to the `LuisPalacios/gitbox` repo itself: file paths, line numbers, function/type names, commit SHAs, PR/issue numbers, permalinks like `github.com/LuisPalacios/gitbox/blob/main/...`. Those are already on the public web.

**Recipe for bug repros:** when the user describes a problem using real names, rewrite in abstract terms before posting. "Under `~/00.git/github-Acme/Acme/` there are clones `internal-lib` and `secret-service`…" becomes "Two clones live on disk under the folder tree of account A, but their `origin` remotes point to an organization owned by a different account B also configured in gitbox." Technical content preserved; private setup not exposed.

**When in doubt, ask** before posting. The cost of one clarifying question is trivial; the cost of a leaked issue is a deleted issue, a broken `#N` cross-reference chain, and lost trust.

## Multiplatform testing

The developer workstation can be any OS. Remote machines are available via SSH for cross-platform testing. Every build-and-test cycle MUST cover all three platforms.

**Local first, then ship remotes.** The default test flow for any change is: (1) build the GUI locally per the build rule, (2) smoke-test it on the local host, (3) `./scripts/ship.sh` to fan out to the remotes configured in `.env` (or `./scripts/ship.sh <short-name>` for one). Never skip step 1 — the local host is part of the cycle, and jumping straight to ship reports "green" without verifying on the machine you actually iterated on. If `.env` is absent or empty, the local build + smoke is the entire cycle.

Connection details are in `.env` (gitignored). Copy `docs/.env.example` to `.env` and fill in your SSH hosts. See `docs/multiplatform.md` for the full setup guide.

| Platform            | Env var              | Arch  | Wails target    | Script token              |
| ------------------- | -------------------- | ----- | --------------- | ------------------------- |
| Windows amd64       | `SSH_WIN_INTEL_HOST` | amd64 | `windows/amd64` | `win-intel` (alias `win`) |
| Windows arm64       | `SSH_WIN_ARM_HOST`   | arm64 | `windows/arm64` | `win-arm`                 |
| macOS Apple Silicon | `SSH_MAC_ARM_HOST`   | arm64 | `darwin/arm64`  | `mac-arm` (alias `mac`)   |
| macOS Intel         | `SSH_MAC_INTEL_HOST` | amd64 | `darwin/amd64`  | `mac-intel`               |
| Linux               | `SSH_LINUX_HOST`     | amd64 | `linux/amd64`   | `linux`                   |

Legacy `SSH_WIN_HOST` is still honored as a fallback for `SSH_WIN_INTEL_HOST` — older `.env` files keep working without a rename.

### Build-test-ship cycle

Scripts in `scripts/` automate the full cycle:

```bash
./scripts/ship.sh            # build GitboxApp on every remote in .env and stage it (+ test-gitbox.json)
./scripts/smoke.sh all       # GitboxApp --version on the local build and every staged remote
./scripts/test-commands.sh   # print GitboxApp --test-mode launch commands
./scripts/run-commands.sh    # print GitboxApp launch commands (real config)
./scripts/setup-credentials.sh all  # set up SSH keys + tokens on all platforms
```

The scripts auto-detect the local OS. Local commands run directly, remote commands use SSH. See each script's header for usage.

### Building the GUI on a remote host

`wails build` cannot cross-compile because each target needs the host's native webview (WebView2 on Windows, WebKit on macOS, WebKitGTK on Linux), so `scripts/ship.sh` builds the GUI **on each remote** over SSH. The manual recipe below is what it does under the hood; it uses `tar | ssh` (rsync isn't available in Git Bash on Windows by default).

```bash
# Build GUI for mac from a Windows or Linux host.
# For Apple Silicon: host=$SSH_MAC_ARM_HOST,   target=darwin/arm64
# For Intel Mac:     host=$SSH_MAC_INTEL_HOST, target=darwin/amd64
host="me@mac-host"       # from $SSH_MAC_ARM_HOST or $SSH_MAC_INTEL_HOST
target="darwin/arm64"      # or darwin/amd64 / darwin/universal

# 1. Wipe and prepare the remote scratch dir.
ssh "$host" 'rm -rf ~/gitbox-remote-build && mkdir -p ~/gitbox-remote-build'

# 2. Ship source (exclude artefacts, node_modules, .git, .env).
tar \
  --exclude='./.git' \
  --exclude='./build' \
  --exclude='./cmd/gui/build/bin' \
  --exclude='./cmd/gui/frontend/node_modules' \
  --exclude='./cmd/gui/frontend/dist' \
  --exclude='./.env' \
  -cf - . | ssh "$host" 'cd ~/gitbox-remote-build && tar -xf -'

# 3. Build on the remote. PATH needs Homebrew / $HOME/go/bin for non-login SSH shells.
#    Include both Homebrew prefixes so the recipe works on arm64 (/opt/homebrew)
#    and Intel (/usr/local) without tweaks.
ssh "$host" 'export PATH=/opt/homebrew/bin:/usr/local/bin:$HOME/go/bin:$PATH && \
  export CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 && \
  cd ~/gitbox-remote-build && \
  cp assets/appicon.png cmd/gui/build/appicon.png 2>/dev/null; \
  cd cmd/gui && wails build -platform '"$target"

# 4. Stage somewhere the user can double-click from Finder / open.
ssh "$host" 'rm -rf /tmp/GitboxApp.app && \
  cp -R ~/gitbox-remote-build/cmd/gui/build/bin/GitboxApp.app /tmp/GitboxApp.app'
```

Notes:

- **Non-login SSH shells** don't source `.zshrc` / `.bash_profile`, so `go` and `wails` are usually off `$PATH`. Prefix every remote command with `export PATH=/opt/homebrew/bin:/usr/local/bin:$HOME/go/bin:$PATH` — Apple Silicon Homebrew uses `/opt/homebrew/bin`, Intel Homebrew uses `/usr/local/bin`; including both keeps the same recipe working for `mac-arm` and `mac-intel`.
- **Tar from the repo root.** If the shell cwd is wrong, tar will happily ship a partial tree. Anchor with `cd "$(git rev-parse --show-toplevel)"` first.
- **`.env` is gitignored** — if the worktree doesn't have one yet, copy it in from the main clone (`cp ../gitbox/.env .`) before running `scripts/ship.sh` or `scripts/smoke.sh`.
- Linux builds the same way — swap `$host` to `$SSH_LINUX_HOST`, `$target` to `linux/amd64`, adjust PATH (e.g. `/usr/local/go/bin`). The artifact lands at `cmd/gui/build/bin/GitboxApp`.
- macOS signing/notarization is a separate concern — see `docs/macos-signing.md`. A `wails build` unsigned binary launches fine locally but gets the Gatekeeper quarantine on first download; for smoke tests that's fine.

### Non-interactive vs interactive tests

- **Non-interactive** (`GitboxApp --version`, unit tests): Claude runs via `./scripts/smoke.sh` or directly via SSH.
- **Interactive** (anything in the GUI window: onboarding, credential setup, clone, mirrors): Claude uses `./scripts/test-commands.sh` or `./scripts/run-commands.sh` to print the exact launch commands. The user runs them in a terminal inside each host's desktop session.

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

Read the file with the Read tool (it supports images). Then analyze what the GUI shows and report what you see.
