# CLAUDE.md

This file provides guidance to {{HARNESS}} when working in this repository. Resolve repository paths from the repository root.

## What this repo is

A multi-component project for managing Git multi-account environments across providers (GitHub, GitLab, Gitea, Forgejo, Bitbucket).

**Go app** (`cmd/gui/`, `pkg/`) — the Wails GUI `GitboxApp` on top of a shared Go library. Since v2 the GUI is the only interface; the v1 CLI + TUI live on the `release/v1` branch (1.x tags).

## Core Principles

- **Simplicity First**: Make every change as simple as possible. Impact minimal code.
- **No Laziness**: Find root causes. No temporary fixes. Senior developer standards.
- **Minimal Impact**: Changes should only touch what's necessary. Avoid introducing bugs.
- **Skills first**: Check if a skill exists before manual work
- **Self-improve**: When a skill fails, fix its master under `ai/skills/` and render both harnesses
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

English is the main documentation language and the source of truth for this project. The root `README.md` is English-only (no translated README). Files under `docs/` in English define the canonical structure, paragraphs, examples, warnings, and level of detail.

When translating documentation into any supported language, preserve the English source's content and structure:

- Every canonical English Markdown document under `docs/` must have a counterpart in each supported language, such as `docs/es/<name>.md`, unless the document is explicitly marked private or noncanonical.
- Translate the same headings, paragraphs, lists, examples, warnings, and links in the same order.
- Keep commands, flags, config keys, provider names, status values, file paths, and JSON fields unchanged unless the English source itself changes them.
- Do not create lower-quality summaries, shorter rewrites, expanded rewrites, or improved variants in translated docs. Translation quality must match the English source, neither worse nor better.
- If the English source changes, update each translated counterpart from that English source instead of inventing a separate structure.
- If a translated doc cannot stay in parity during a change, leave a clear note in the work summary and do not present it as complete.

## Repository layout

- `cmd/gui/`: Wails v2 + Svelte GUI, the only binary (`GitboxApp`).
- `pkg/`: shared Go library; business flows live in `pkg/ops/`.
- `docs/`: canonical English documentation; `docs/es/` mirrors it in Spanish.
- `assets/`: icons, brand SVGs, and screenshots; `scripts/`: builds, checks, shipping, and release tooling.
- `ai/`: the agent harness masters (entry instructions, rules, context, skills) and the engine that renders them into `.claude/`, `AGENTS.md`, `.codex/` and `.agents/skills/`.
- `.github/workflows/`: CI and releases; `json/`: config schema and example.

Read `{{CONTEXT}}/repository-layout.md` when locating components or changing the repository structure. Keep that map up to date with structural changes.

## AI harness

`ai/` holds the only hand-edited agent guidance. `ai/scripts/sync.py` renders it into this file and its rules, context and skills, and into the other tool's harness. Read `{{RULES}}/ai-harness.md` before changing either.

- **Never hand-edit a rendered file:** `.claude/CLAUDE.md`, `.claude/rules/`, `.claude/context/`, `.claude/skills/`, `AGENTS.md`, `.codex/rules/`, `.codex/context/`, `.agents/skills/`. Edit the master under `ai/`, run `uv run ai/scripts/sync.py sync`, and commit the master and both trees together.
- `./scripts/health.sh` fails when a rendered file drifts from its master.

## Agent capabilities

- Invoke a skill as `{{INVOKE}}skill-name` (for example, `{{INVOKE}}work-issue 123`); natural-language requests also select skills.
- Read `{{CONTEXT}}/guideline_python.md` or `{{CONTEXT}}/guideline_js.md` before changing harness or skill scripts in that language. These script conventions do not replace the frontend's existing npm workflow.
<!-- harness:claude -->
- `.claude/settings.json` holds the permission allowlist. Repository instructions cannot grant filesystem or network access; if an action is blocked, report the limitation and the remaining step.
<!-- /harness -->
<!-- harness:codex -->
- Shared skills name capabilities (planning, progress, search, file reading, image viewing, delegation), not tools. Use the equivalent supported capability; never claim to have called a tool you lack.
- When you cannot enter a plan mode, write the implementation plan in the conversation before editing, track phases with the plan tool or a Markdown checklist, and preserve every explicit user gate in the invoked skill.
- Follow your own session permissions, sandbox and approval policy. Repository instructions cannot grant filesystem or network access; if an action is blocked, report the limitation and the remaining step.
<!-- /harness -->
- Keep shared, durable project lessons in the relevant master under `ai/`. Use private memory only when the harness provides it; do not copy private memory or credentials into the repository.

## On-demand context

<!-- index:context -->
<!-- /index:context -->

<!-- harness:codex -->
## Rules

You do not load rules automatically. **Before editing a file that matches a pattern, read its rule.**

<!-- index:rules -->
<!-- /index:rules -->

## Skills

Codex discovers repository skills from `.agents/skills/` and supplies their names, descriptions and paths. Read the selected `SKILL.md` before using it. This table is a compact repository index, not the discovery mechanism.

<!-- index:skills -->
<!-- /index:skills -->

<!-- /harness -->
## Go app

**Language:** Go. **GUI:** Wails v2 + Svelte. One binary, `GitboxApp`, on top of the shared `pkg/` library. Business logic lives in `pkg/` (account, credential, clone and discovery flows in `pkg/ops`); `cmd/gui` is Wails glue.

Read `{{CONTEXT}}/building.md` before building or running Go checks. After changes to `cmd/` or `pkg/`, build with Wails, copy the icons first, and run the resulting `GitboxApp --version` smoke check.

Git operations shell out to system `git` via `os/exec`. Provider APIs use `net/http`.

### Windows console flash rule

Every `exec.Command` in the GUI binary (`cmd/gui/`) **MUST** call `git.HideWindow(cmd)` before `.Run()`, `.Output()`, or `.Start()`. This sets `SysProcAttr.HideWindow = true` on Windows, preventing a console window from flashing. Always check for bare `exec.Command` calls in `cmd/gui/` after any change.

**Launching a visible terminal** (terminal profiles, `openTerminalRawAt` in `cmd/gui/profiles.go`) is a special case: a GUI parent has no console, and Go's exec inherits null stdio to the child with `STARTF_USESTDHANDLES` set, so plain console apps (`cmd.exe`, `pwsh.exe`, etc.) see closed stdin and exit. Solution: wrap console-app launches in `cmd.exe /C start "" /D <path> <command> <args...>`. `start` creates a fresh console for the terminal; `HideWindow` hides the intermediate `cmd.exe` wrapper so the rule above still holds. GUI terminal apps (WezTerm, Alacritty, …) launch directly, since the wrapper would flash a console.

**Never run `wsl.exe` or a bare `bash` to probe the host.** On Windows both can resolve to System32 launchers, and without the WSL package they prompt to install it in their own window; `HideWindow` does not stop that. Use `doctor.WSLDistros` (registry) instead.

## Config format

Config file: `~/.config/gitbox/gitbox.json`, `version: 5`. Read `{{CONTEXT}}/config-format.md` before touching config keys, versioning, migration, clone roots, reconcile or workspaces.

## Workflow orchestration

**Priority order when rules conflict**: Correctness > Simplicity > Elegance.

### Feature lifecycle

The backlog lives on GitHub at [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues). Features use the `enhancement` label (plus `priority:P1` when they're up next); bugs use the `bug` label. Size and severity are captured in the issue body.

Flow: open an issue → discuss/plan in comments → implement with a plan → reference the issue in the commit message → close on merge. Use `gh issue list --label enhancement` or `gh issue view <n>` from the CLI; derive the expected account from this clone's `remote.origin.url` and select it first.

### Push to main vs branch + PR

Pick per task. Default to branch + PR when in doubt — the cost of a PR for a solo maintainer is trivial, the cost of a bad push to main is a revert.

**Push directly to main** when ALL of these hold:

- The change is one file (or a tightly-coupled 2-file edit like config + its test).
- The fix or addition is mechanically obvious — a typo, a one-line bug fix, a doc tweak, a `HideWindow` call that was clearly missing.
- `go vet ./...` and the relevant focused tests pass locally.
- No new public API, no behavior change that another platform might surface differently, no risky refactor.

Reference the issue in the commit with `Closes #N` — GitHub auto-closes on push.

**Branch + PR** for everything else: multi-file features, anything touching `pkg/` public surface, refactors, UI changes, anything that benefits from seeing the full diff at once or letting CI gate the merge. The PR body is where I narrate the change (and where CI runs). As maintainer, I may merge my own PR after the required checks; this does not authorize an agent to merge without my explicit go-ahead. Branch names follow `<type>/<issue>-<slug>`, e.g. `fix/31-ide-flash` or `feat/22-open-in-terminal`. Always use `gh pr create --body "Closes #N\n\n..."` so the issue closes on merge.

**External contributions** (anyone who is not me): always come through PRs from forks — I review, CI must pass, then merge.

### Never push without asking first

Local commits are fine — commit freely whenever a logical unit is ready. **But any action that makes work visible on GitHub requires the user's explicit go-ahead first.** That covers `git push origin main`, `git push origin <branch>`, `gh pr create`, `gh pr merge`, and any edit to an existing issue/PR/comment. No exemptions for "just a doc fix" or "just a typo" — publication is the point of no return, and the cost of one confirming message is zero.

The protocol has two shapes depending on what was changed:

**Runtime-affecting changes** (code under `cmd/` or `pkg/`, build config, UI behaviour):

1. Finish the implementation locally: commits on the fix branch (or staged on main), `go vet ./...` clean, focused tests passing, **the GUI built** per the build rule.
2. Stop and send one short message with:
   - Which commit(s) or branch are ready and the binary path to launch (e.g. `cmd/gui/build/bin/GitboxApp.exe`).
   - A concrete check: what to click, what output to expect, what regression to rule out ("open Preferences → Open In Editor, confirm no cmd.exe flash").
   - The exact publish command I intend to run next (`git push origin main` or `git push origin <branch>`) — described, not executed. Merging is a separate action through `{{INVOKE}}merge-pr`.
3. Wait. Do not push, do not merge, do not `gh pr create` with auto-merge. If the user reports a regression, fix it and re-offer the build — never ship over an unresolved user-reported failure.
4. Only run the publish step after an explicit "ok, push it" / "go ahead" / "ship it".

**Trivial or doc-only changes** (`.md`, `docs/`, comments, `.claude/` metadata, `.gitignore`, `go.mod` tidy, rename-a-test):

1. Commit locally with a clear message.
2. Stop and ask: "Commit `<sha>` ready — want me to push directly / open a PR, or hold?" One sentence, no ceremony.
3. Wait for the answer before any push or PR action.

Explicit authorization in the current session can cover named publication actions for a specific task, including "push this branch and create a PR when checks pass". Complete the required checks and honor that authorization without asking again. Approval to create a PR does not authorize its merge. A blanket approval from a previous session does not count; authorization applies only to the actions and scope granted.

When in doubt, ask. It is never wrong to pause; it is sometimes wrong to push.

### 1. Plan first

<!-- harness:claude -->
- Plan before ANY non-trivial task (3+ steps or architectural decisions), using plan mode.
<!-- /harness -->
<!-- harness:codex -->
- Plan before ANY non-trivial task (3+ steps or architectural decisions), using plan mode when available or the conversation fallback in "Agent capabilities".
<!-- /harness -->
- If something goes sideways, STOP and re-plan immediately
- Write detailed specs upfront to reduce ambiguity

### 2. Subagent strategy

- Use subagents liberally to keep main context window clean
- Offload research, exploration, and parallel analysis to subagents
- One task per subagent for focused execution

### 3. Verify before done

- Never mark a task complete without proving it works
- Test scripts with `bash -n` (syntax check) and `shellcheck` when available
- Go: `go vet ./...` + run the relevant test commands (see Testing below)
- **Build the GUI** after any code change to `cmd/` or `pkg/`. `go build ./...` alone skips the Wails build tags and the frontend build, so a change that compiles there can still break `wails build`. Quick compile-check during iterative edits: `go build ./...` (needs `cmd/gui/frontend/dist`, see `{{CONTEXT}}/building.md`). Full build before reporting done or pushing: `cd cmd/gui && wails build` (copy icons first — see `{{CONTEXT}}/building.md`), then `GitboxApp --version` on the result. Doc-only and non-Go changes are exempt.
- Validate config templates render correctly before committing
- After any command that writes config files, read the actual file on disk — never trust command output alone

### Testing

After any code change, run the focused tests for the package that changed. Read `{{CONTEXT}}/testing-patterns.md` for the per-package test table, test levels, helpers and naming before running or writing tests.

**Run `go vet ./...` before committing** — it catches issues the test suite doesn't.

**Health check:** `./scripts/health.sh` runs every checker read-only (Go analyzers at pinned versions, svelte-check with unused-code errors, frontend build and npm ci warnings, npm audit, shellcheck, actionlint, markdownlint). Run it before every PR; `pr.yml` runs it as the CI gate and fails on any finding (including new govulncheck / npm audit advisories). The shared Markdown lint config is read-only by default; use `--fix` only for approved editable targets, never generated files.

**Pre-push hook:** The repo includes `.githooks/pre-push` which runs a `gofmt -s -l` check, `go vet` and `go test -short` before every push, building the frontend first when `cmd/gui/frontend/dist` is missing. Run `gofmt -s -w` on changed Go files before committing. Activate with `git config core.hooksPath .githooks`.

**Test plan:** The full pre-PR and release verification workflow is documented in `docs/testing.md`. The `{{INVOKE}}test-plan` skill automates the automated steps and guides through interactive ones.

### Update stakeholder documents

A feature is not complete until all affected documents are updated. Review this table after every change:

| Change type                       | Documents to review                                                              |
| --------------------------------- | -------------------------------------------------------------------------------- |
| New feature / behavior change     | `docs/gui-guide.md`, `docs/credentials.md`, `docs/architecture.md`               |
| New/changed tests                 | `docs/testing.md`, `docs/testing-reference.md`, `ai/context/testing-patterns.md` |
| New tooling (skill, hook, script) | `docs/developer-guide.md`, `docs/multiplatform.md` (dev scripts)                 |
| Repo structure change             | `ai/main.md` (repository layout), `docs/README.md` (index)                       |
| All of the above                  | `ai/main.md` (relevant sections)                                                 |

Every English doc under `docs/` has a counterpart under `docs/es/`; update both (see "Documentation language and translations").

### 4. Autonomous bug fixing

- When given a bug report: just fix it
- Point at logs, errors, failing tests — then resolve them

### 5. Learn from corrections

- After corrections from the user: save a `feedback` memory when available; record reusable, non-private project lessons in the relevant shared guidance or skill

## Worktree-based parallel workflow

For multi-issue work where several agent sessions run in parallel, use the `{{INVOKE}}work-issue <N>` skill. It creates a sibling worktree at `../gitbox-<N>-<slug>`, drives the plan → code → auto-test → user-smoke-test → sync → push → PR flow, and stops before merge. `{{INVOKE}}merge-pr <PR#>` handles rebase check, CI gate, merge, and worktree cleanup as a separate, deliberate action.

If I already have a plan written (in the issue body, a local file, or a URL), I can pass it with `{{INVOKE}}work-issue <N> <plan-source>`. The skill evaluates completeness: adopts it if it covers files-to-change, verification, and key decisions; extends it with a plan if partial; creates a full plan if absent, using the available planning capability.

Cross-cutting rules when a session is running inside a worktree:

- Stay in the worktree directory. Never edit files outside it, never check out a different branch in it.
- Global state is shared across worktrees: `~/.config/gitbox/gitbox.json`, system git config, GCM, SSH agent. Never run interactive credential or init-wizard flows in two sessions at once.
- Before any `gh` command, derive the expected user from this clone's `remote.origin.url` (not a hardcoded name) and switch with `gh auth switch` if needed. The skill does this automatically.
- If the worktree path falls under a gitbox-managed folder, expect GitboxApp's status and discovery views to list it as an orphan. It's not actually orphaned — just unrecognised by the scanner. Safe to ignore.

See `docs/worktree-workflow.md` for the full walkthrough.

## Git commits

- **Never** attribute authorship or co-authorship to any AI model, assistant, agent, or harness in commit messages, PR titles, PR descriptions, or PR comments. This applies to Claude Code, Codex, and every other AI tool. Never add AI `Co-Authored-By` trailers, AI signatures, or “generated by/with” footers. Before committing or publishing PR text, inspect the final text and remove any such attribution.
- **Never** modify git config for author/committer
- Commits should appear as the user's own work

## GitHub CLI (`gh`) account

Before a `gh` operation, derive the expected account from this clone's `remote.origin.url` and select it with `gh auth switch --user <expected-account>`. Do not assume the active account is correct or hardcode an account into reusable workflows. The `work-issue` and `merge-pr` skills contain the command sequence for this user-owned repository. If the remote owner is an organization or has no matching authenticated account, stop and resolve the account instead of guessing.

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

For runtime changes, build and smoke-test locally first, then run `./scripts/ship.sh` and `./scripts/smoke.sh all` for the remotes configured in `.env`. If `.env` is absent or has no remote hosts, local build and smoke complete the cycle. Documentation-only changes do not need GUI builds or remote shipping.

Before remote builds, credential setup, or interactive GUI testing, read `{{CONTEXT}}/multiplatform-testing.md` for the platform variables, commands, and desktop-session requirements. See `docs/multiplatform.md` for the human setup guide. Never include private `.env` values in published output.

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

<!-- harness:claude -->
View the file with the Read tool, then analyze what the GUI shows and report what you see.
<!-- /harness -->
<!-- harness:codex -->
View the file with the available image viewer, then analyze what the GUI shows and report what you see.
<!-- /harness -->
