---
name: work-issue
description: Drive a GitHub issue through an isolated worktree, planning, implementation, verification, and PR creation. Use for work on issue N or an existing issue plan. Requires explicit approval before publishing and stops before merge.
---

# $work-issue — Drive an issue in an isolated worktree

**IMPORTANT:** Before starting, tell the user "I'm executing `$work-issue <N>`" and create a todo list with the phases below. Use the todo list to track progress and to survive context compaction.

## Harness compatibility

Follow "Agent capabilities" in `AGENTS.md`: use supported tools or a conversation plan/checklist when a native capability is unavailable. Preserve every explicit approval gate below. Resolve repository paths from the active worktree root.

## Usage

```text
$work-issue <N>                      Work on issue N, plan from scratch
$work-issue <N> issue                Adopt the plan written inside the issue body
$work-issue <N> <path-or-url>        Adopt the plan from a local file or URL
```

`<N>` is the GitHub issue number. The plan source is optional.

## Golden rules (never skip)

- **Never push, PR, merge, or comment on an issue/PR without explicit user go-ahead.** Local commits are free; publishing is not. An explicit authorization in the current session applies only to the actions and scope it names; do not ask again for an action it already covers.
- **Never hardcode a GitHub username.** Derive the expected owner from this clone's `remote.origin.url` — the skill must work on any fork.
- **Work only inside the worktree** after Step 2. Record `MAIN` (the `main` line of `wt_access.py find`) and `WT` (the worktree's absolute path with forward slashes). Never edit, build, commit or check out a branch in `MAIN`: another session may be working there. Use absolute paths inside `WT` for every file read and edit. Run path-sensitive commands one at a time, never in parallel. Never write through `git -C`. Never check out a different branch in `WT`.
- **Set the shell tool's working directory to `WT`** for every command instead of a `cd` prefix, so the allow rules match.
- **Anonymise anything that lands on GitHub** (PR title/body, commit messages that will be pushed). No local paths, no private account keys, no org names unrelated to this repository. See "Anonymize before posting to GitHub" in `AGENTS.md`.
- **Build the GUI** after any change under `cmd/` or `pkg/` — `go build ./...` alone does not exercise the Wails build tags, the embedded frontend, or the `git.HideWindow` Windows rule.

## Resumability

Each phase begins by reading current git state (`wt_access.py find --issue <N>`, `git rev-parse --abbrev-ref HEAD`, `git log -1`, `gh pr view` if a PR exists). If `$work-issue <N>` is re-invoked mid-flow, from `MAIN` or from inside `WT`, Step 1 adopts the existing worktree, Step 2b runs again, and the skill jumps to the right phase instead of restarting. Worktrees created at the old sibling path `../gitbox-<N>-<slug>` are adopted the same way; never move them.

## Access helper

`.agents/skills/work-issue/scripts/wt_access.py` holds the fragile parts. It needs only `uv` and the Python standard library (no packages, no network), following the zero-footprint rule. Run it as `uv run -q .agents/skills/work-issue/scripts/wt_access.py <command>`:

- `root --repo <repo> [--create]` prints the worktree root `~/.wt/<repo>` for this OS, with forward slashes.
- `find --issue <N>` prints `main <MAIN>` and each worktree whose branch is `<type>/<N>-…`, including one stopped in a rebase. Exit 0 means one usable match, 4 none, 5 attention (several matches, prunable or locked).
- `check --harness claude|codex` reports whether the one-time user-level grant is present (exit 0) or missing (exit 3).
- `apply --harness claude|codex --dry-run` prints the exact diff of the grant; `apply --harness codex --yes` writes it.

Exit 1 always means a condition to resolve by hand; show its message and stop.

## Delegated batch work

When a supervisor assigns a reserved issue through `$wu-coordinate-tasks`, read that skill and follow its worker contract. The user's batch-start instruction delegates local issue selection and plan approval to the supervisor: send the preflight details and plan there instead of repeating the ordinary user gates below. Resume in the assigned worktree when it already exists. Keep every command and edit in that worktree, report scope changes before editing, and return verified local commits to the supervisor. Workers stop before Steps 8–10; the supervisor owns sync, publication and CI.

Only in this coordinated mode, evidence of an equivalent automated behavioral smoke can satisfy Step 7 under `wu-coordinate-tasks` criteria. Missing coverage still requires the user's check. This mode grants no worker publication authority and does not change the standalone workflow below.

## Universal gh-user detection

Run this before every `gh` command. It is portable across Git Bash (Windows), macOS, and Linux.

```bash
origin_url=$(git config --get remote.origin.url)
expected_owner=$(printf '%s\n' "$origin_url" | sed -E 's|.*[:/]([^/]+)/[^/]+$|\1|')
gh auth switch --hostname github.com --user "$expected_owner" || {
  echo "Cannot select the account for this origin; stop and confirm the login."
  exit 1
}
```

This repository and personal forks use the origin owner as the login. If the owner is an organization or the switch fails, stop and confirm the intended account; never infer it from the first account printed by `gh auth status` or attempt non-interactive authentication.

---

## Step 1: Preflight

Read `.agents/skills/wu-container/issue-rules.md` to distinguish an independent issue from a work-unit task. Implement leaves only. A container goes through `$wu-tasks` and `$wu-coordinate-tasks <N>`; do not implement or close the container as one task. For a native child, read the parent specification and referenced AC, verify its prerequisites, and check the coordination registry for an existing claim before creating competing work.

- **Locate.** Run `wt_access.py find --issue <N>` from wherever the session started. Record `MAIN` from its `main` line.
  - Exit 0: adopt the matched worktree as `WT`, tell the user, and go to Step 2b. A `rebasing` match resumes at Step 8.
  - Exit 5: show the matches and ask which one to use. Never pick one, prune it or unlock it on your own.
  - Exit 4: continue below. If the session sits inside another worktree (`git rev-parse --show-toplevel` differs from `MAIN`), stop: the new worktree is created from `MAIN` or from a fresh session there.
- Run the gh-user detection snippet above.
- `git fetch origin`.
- Read the issue: `gh issue view <N> --json number,title,body,labels`.
- Derive slug from title: lowercase, replace non-`[a-z0-9]+` with `-`, collapse repeats, trim to 40 chars.
- Derive branch name `<type>/<N>-<slug>`:
  - `fix` if the issue has label `bug`.
  - `feat` if label `enhancement`.
  - `chore` otherwise.
- Derive `<repo>` from the last segment of `remote.origin.url` without `.git`, and get the root with `wt_access.py root --repo <repo>`. Propose `WT` = `<root>/<N>-<slug>`.
- **Grant check.** Run `wt_access.py check --harness <this harness>`. When it reports missing items, run `apply --dry-run` and show the diff with the offer below. The grant is optional: when the user declines, the per-session fallback in Step 2b applies.

**Gate:** show the user the following block verbatim so they can confirm I fetched the right issue, then wait for explicit confirmation before touching the filesystem.

```text
Issue #<N>: <exact title from gh>
Labels:    <labels>
Branch:    <type>/<N>-<slug>
Worktree:  <WT>
Grant:     granted | missing (diff above)
```

Ask: "Is this the right issue? Proceed?" and, when the grant is missing, "Add the one-time grant for `~/.wt`?" Do not create the worktree until the user answers yes. Apply the grant only after an explicit yes to it.

To apply the Codex grant, run `wt_access.py apply --harness codex --yes` and rerun `check`. Codex reads its configuration at startup: after a new grant, create the worktree (Step 2), then stop and ask the user to relaunch Codex in `MAIN` and run `$work-issue <N>` again, which adopts the worktree.

## Step 2: Create worktree

- **Collisions.** Before creating anything:
  - If `WT` already exists and `find` did not adopt it, stop. When `git rev-parse --path-format=absolute --git-common-dir` inside it differs from `MAIN`'s, it belongs to another clone: offer `<N>-<slug>-<owner>` as the folder name instead.
  - If `git rev-parse --verify --quiet refs/heads/<branch>` succeeds, or `git ls-remote --heads origin <branch>` prints a line, stop and ask: earlier work exists without a worktree.
- **Orphan heads-up:** if `~/.config/gitbox/gitbox.json` exists, read it and check whether `WT` falls under any of the clone roots in `global.roots` (a leading `~` means the home folder). If it does, warn once: "Heads-up — this worktree will sit inside a gitbox-managed folder, so GitboxApp's status and discovery will list it as an orphan. Not actually orphaned — just unrecognised by the scanner. Safe to ignore." Proceed unless the user says otherwise.
- Create the root with `wt_access.py root --repo <repo> --create`, then the worktree, without tracking `origin/main` so a bare `git push` can never target `main`: `git worktree add --no-track -b <branch> "<WT>" origin/main`.
- Tell the user: "Worktree ready at `<WT>` on branch `<branch>`. You can open a separate Claude Code or Codex session in that directory and continue there; `$work-issue <N>` adopts it from either place."

## Step 2b: Access preflight

Prove, before any implementation, that every capability Steps 3–6 need is available in `WT`. Run each probe from `WT`, one at a time, and show the result as a table of granted and missing items:

- File write: write a probe file at `<WT>/cmd/gui/build/bin/.wt-probe` (git-ignored) with the file tool, then overwrite it once.
- Git: `git status --porcelain`, `git add --dry-run .`, and the commit form with `--dry-run --allow-empty -m probe`.
- Go: `go vet ./internal/...` and `gofmt -s -l pkg/git`.
- Tools: `npm --version`, `wails version`, `uv --version`, and `test -x scripts/gate.sh`.
- Location: `git rev-parse --show-toplevel` prints `WT`.

Collect every missing item into one grouped request for the user and wait. When a probe cannot be proved, because access was denied or the result is ambiguous, stop before Step 3 and say what could not be proved. Never continue on the assumption that a capability exists. After the preflight passes, Steps 3–6 must not need a new permission.

When file probes fail, ask the user to relaunch Codex with `--add-dir <WT>`, or to accept the grant. The commit form is the `commit with:` line printed by `check --harness codex`; always commit through it, so a commit never runs hooks from the worktree. Tell the user which prompts remain expected in Codex: `npm ci` (network), `./scripts/gate.sh` (SSH and the gate marker in the common git directory) and the Step 8 rebase.

## Step 3: Understand

- Re-read the issue body and labels.
- Explore only the files the issue clearly touches, using targeted filename and content searches.
- Summarise in ≤5 lines: what the problem is, where it lives, what success looks like.

**Gate:** ask the user "Ready to plan?" before moving on.

## Step 4: Plan — or adopt an existing one

Resolve the plan source:

- **Explicit second argument** — treat as a path or URL if it's not the literal `issue`. Fetch URLs with an available web tool; read local paths with an available file-reading tool.
- **Second argument is `issue`** — extract any section from the issue body titled `Plan`, `Implementation`, `Approach`, or `Proposed solution` (case-insensitive).
- **No second argument** — scan the issue body for the same section headings anyway (cheap), but do not assume one exists.

**Completeness check** — a plan is complete enough to adopt only if it covers all three:

1. Which files will change and why.
2. Test / verification steps.
3. Any architectural decisions or non-obvious trade-offs called out.

- **Complete:** summarise the adopted plan in 5 lines for the user, confirm, skip to Step 5. No plan-mode exit is needed because plan mode was never entered.
- **Partial:** seed a plan with the user's plan, call out the gaps, draft additions, and present the plan in the conversation for approval.
- **Absent:** write a fresh plan and present it in the conversation for approval.

## Step 5: Implement

- Before the first commit, confirm that `git rev-parse --show-toplevel` prints `WT`; stop if it does not.
- Edit + commit cycles. Keep commits focused and logical. Reference the issue in the body: `Closes #<N>` on the final commit (or the commit that completes the fix).
- **Never** attribute authorship or co-authorship to any AI model, assistant, agent, or harness in commit messages or PR titles, descriptions, or comments. Remove AI `Co-Authored-By` trailers, signatures, and “generated by/with” footers before committing or publishing. Follow the shared rule in `AGENTS.md`.
- All changes stay local. No pushes in this step.

## Step 6: Auto-verify

Run in this order from `WT` (the commands below are relative to it), stop on first failure and fix before continuing:

1. `go vet ./...`
2. The focused test command from the table in `.codex/context/testing-patterns.md` under "Which tests to run" (match what changed: `pkg/`, `pkg/ops/`, `cmd/gui/`, etc.). When unsure, run `go test ./...`.
3. Build the GUI (quick compile-check during iteration, full build before reporting done). `cmd/gui` embeds `frontend/dist`, so build the frontend once if it is missing (`(cd cmd/gui/frontend && npm ci && npm run build)`):

   ```bash
   # Quick iterative check
   go build ./...      # several packages: compiles and discards the output

   # Full build (needed before push)
   cp assets/appicon.png cmd/gui/build/appicon.png
   mkdir -p cmd/gui/build/windows   # absent in a fresh clone or worktree
   cp assets/icon.ico    cmd/gui/build/windows/icon.ico
   (cd cmd/gui && wails build)

   # Smoke: prints "GitboxApp <version> (<sha>)" and exits
   cmd/gui/build/bin/GitboxApp --version   # GitboxApp.exe on Windows, GitboxApp.app/Contents/MacOS/GitboxApp on macOS
   ```

4. Once the work is committed, run the local gate on the final commit. It verifies the committed `HEAD` on macOS, Linux and Windows and replaces waiting for GitHub CI (see `docs/local-gate.md`). Doc-only changes run it too, because its health check lints Markdown and the harness mirror:

   ```bash
   ./scripts/gate.sh
   ```

   A failing lane prints its log path; fix the cause, commit, and re-run. Skip a lane (`--skip <lane>`) only with the user's agreement when its host is down; the result is a partial gate, which `$merge-pr` reports.

Doc-only changes (`.md`, `docs/`, comments, `ai/` and the harness files it renders, `.gitignore`, `go.mod` tidy) skip the builds but still run `go vet` and relevant tests.

## Step 7: User smoke-test gate

Follow the "Never push without asking first" protocol from `AGENTS.md`. Check whether current-session authorization already covers the proposed publication action and scope. Existing publication authorization does not waive the required user smoke test for runtime changes.

**Runtime-affecting changes** (anything under `cmd/` or `pkg/`, build config, UI behaviour):

1. State the commit SHA(s) on the branch.
2. List the binary path the user should launch:
   - Windows: `cmd/gui/build/bin/GitboxApp.exe`
   - macOS: `cmd/gui/build/bin/GitboxApp.app`
   - Linux: `cmd/gui/build/bin/GitboxApp`
3. Name a concrete check: what to click/type, expected output, regression to rule out. Be specific — "open Preferences → Open In Editor, confirm no cmd.exe flash" is good; "test it" is not.
4. Show the exact push command you intend to run next (`git push -u origin <branch>`). Describe it, do not execute it. Wait for the user to confirm the smoke test before proceeding.

**Trivial or doc-only changes:**

State the commit and intended publication action. If the user has not already authorized that action and scope in this session, ask whether to push the branch, open a PR, or hold, and wait.

**Require explicit go-ahead for publication** ("ok, push it", "go ahead", "ship it") unless current-session authorization already covers that action and scope. Approval of a smoke test alone does not authorize publication. If the user reports a regression, fix it and re-offer the build. Never ship over an unresolved failure.

## Step 8: Sync with main

After go-ahead and before pushing:

```bash
git fetch origin
git log --oneline HEAD..origin/main
```

- If `origin/main` has not advanced, skip to Step 9.
- If it has advanced, offer `git rebase origin/main`. On conflicts, surface each file and ask for guidance — do not auto-resolve non-trivial conflicts.
- A rebase invalidates the earlier verification results, including the gate marker of the old SHA. Re-run Step 6 on the rebased state; for runtime changes, rebuild and repeat Step 7's user smoke test before publishing.
- Reassess authorization separately from verification. If approval named a particular SHA or build, or the scope changed, show the new state and obtain renewed approval. An existing authorization to push this branch and create a PR once checks pass remains valid for the same actions and scope after those checks and any required smoke test pass; do not ask for it again.

## Step 9: Push gate

Show the exact push command and execute it only after verification and any required user smoke test pass, with explicit current-session authorization for this push and scope. If that authorization is missing or no longer applies, obtain it before running:

```bash
git push -u origin <branch>
```

After the push, publish the gate result as an informational `local-gate` commit status with `./scripts/gate.sh --report`. It is part of the authorized push, not a separate publication.

If Step 8 rebased commits that were already pushed, propose `git push --force-with-lease origin <branch>`. Obtain explicit authorization for that force push unless the current-session authorization already covers it; permission for a plain push is insufficient.

## Step 10: PR gate

Draft the PR title (≤72 chars, no leading scope prefix unless the repo convention uses one — scan recent `gh pr list --state merged --limit 10` titles to match style) and body.

PR body template:

```markdown
Closes #<N>

## What changed

<2-4 bullets, anonymised>

## Test plan

- [ ] <concrete check 1>
- [ ] <concrete check 2>
```

**Anonymise before showing:** no local paths (`~/00.git/...`, `C:\Users\...`), no private account keys (`github-<realname>`), no unrelated org/repo names. See "Anonymize before posting to GitHub" in `AGENTS.md` for the full rule.

Show the title and body. Create the PR only with explicit current-session authorization for PR creation and its scope. If the user already granted that authorization, proceed without asking again; otherwise wait for it. Push authorization alone does not authorize PR creation. Then:

```bash
gh pr create --title "<title>" --body "$(cat <<'EOF'
<body>
EOF
)"
```

## Step 11: Stop

- Report the PR URL from the `gh pr create` output.
- Do **not** merge, do not auto-enable merge, do not comment further.
- Tell the user: "PR up at `<url>`. Run `$merge-pr <PR#>` when you're ready to merge."

## Behavior notes

- The skill is resumable — re-invoking it mid-flow picks up from the right phase based on git state, not from Step 1.
- If the user interrupts between gates, stay put. Never advance a gate without explicit instruction.
- If any automated step in Step 6 fails, stop and fix. Do not skip to Step 7 with a broken build.
- Make the planning decision in Step 4. If the user requests a new plan later, present the revised plan in the conversation and preserve the applicable approval gates.
