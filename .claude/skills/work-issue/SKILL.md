---
name: work-issue
description: Drive an issue from worktree creation through push and PR, with explicit user gates. Use when the user says "work on issue N", "let's tackle #N", or invokes /work-issue. Accepts an optional plan source (local path, URL, or the literal "issue") to adopt an existing plan instead of entering plan mode from scratch. Stops before merge; companion /merge-issue skill handles that.
---

# /work-issue — Drive an issue in an isolated worktree

**IMPORTANT:** Before starting, tell the user "I'm executing `/work-issue <N>`" and create a todo list with the phases below. Use the todo list to track progress and to survive context compaction.

## Usage

```text
/work-issue <N>                      Work on issue N, plan from scratch
/work-issue <N> issue                Adopt the plan written inside the issue body
/work-issue <N> <path-or-url>        Adopt the plan from a local file or URL
```

`<N>` is the GitHub issue number. The plan source is optional.

## Golden rules (never skip)

- **Never push, PR, merge, or comment on an issue/PR without explicit user go-ahead.** Local commits are free; publishing is not.
- **Never hardcode a GitHub username.** Derive the expected owner from this clone's `remote.origin.url` — the skill must work on any fork.
- **Stay in the worktree** after Phase 2. Never edit files outside it, never check out a different branch in it.
- **Anonymise anything that lands on GitHub** (PR title/body, commit messages that will be pushed). No local paths, no private account keys, no org names unrelated to this repository. See "Anonymize before posting to GitHub" in `.claude/CLAUDE.md`.
- **Build both binaries** after any change under `cmd/` or `pkg/` — the CLI and GUI share `pkg/` but diverge on build tags and the `git.HideWindow` Windows rule.

## Resumability

Each phase begins by reading current git state (`git rev-parse --abbrev-ref HEAD`, `git worktree list`, `git log -1`, `gh pr view` if a PR exists). If `/work-issue <N>` is re-invoked mid-flow, jump to the right phase instead of restarting.

## Universal gh-user detection

Run this before every `gh` command. It is portable across Git Bash (Windows), macOS, and Linux.

```bash
# Derive expected owner from this clone's origin remote.
# Handles HTTPS (https://github.com/OWNER/REPO.git)
# and SSH   (git@github.com:OWNER/REPO.git).
origin_url=$(git config --get remote.origin.url)
expected_owner=$(echo "$origin_url" | sed -E 's|.*[:/]([^/]+)/[^/]+$|\1|' | sed 's|\.git$||')

# What user does gh currently consider active?
active_user=$(gh auth status 2>&1 | grep -oE 'account [A-Za-z0-9-]+' | head -1 | awk '{print $2}')

echo "expected=$expected_owner active=$active_user"

if [ "$active_user" != "$expected_owner" ]; then
  gh auth switch --user "$expected_owner" || {
    echo "gh does not know user '$expected_owner'. Run:"
    echo "  gh auth login --hostname github.com --web"
    exit 1
  }
fi
```

If `gh auth switch` fails (the owner is not in `gh auth status`), stop and ask the user to log in manually. Never attempt non-interactive auth.

---

## Step 1: Preflight

- Confirm we are in the main clone, not already inside a worktree: `git rev-parse --show-toplevel` and `git rev-parse --git-dir`. If the git dir is under `.git/worktrees/`, we're in a worktree — re-invoke in the main clone.
- Run the gh-user detection snippet above.
- `git fetch origin`.
- Read the issue: `gh issue view <N> --json number,title,body,labels`.
- Derive slug from title: lowercase, replace non-`[a-z0-9]+` with `-`, collapse repeats, trim to 40 chars.
- Derive branch name `<type>/<N>-<slug>`:
  - `fix` if the issue has label `bug`.
  - `feat` if label `enhancement`.
  - `chore` otherwise.
- Propose worktree path `../gitbox-<N>-<slug>` (sibling of main clone).

**Gate:** show the user the branch name, worktree path, and a one-line issue summary. Wait for confirmation before touching the filesystem.

## Step 2: Create worktree

- **Orphan heads-up:** if `~/.config/gitbox/gitbox.json` exists, read it and check whether the proposed worktree absolute path falls under `folder` (top-level) or any account's `folder`. If it does, warn once: "Heads-up — this worktree will sit inside a gitbox-managed folder, so `gitbox status` and discovery will list it as an orphan. Not actually orphaned — just unrecognised by the scanner. Safe to ignore." Proceed unless the user says otherwise.
- Create: `git worktree add ../gitbox-<N>-<slug> -b <branch> origin/main`.
- `cd` into the worktree for all subsequent steps.
- Tell the user: "Worktree ready at `<path>` on branch `<branch>`. You can open a separate Claude session in that directory and continue there — I'll resume from Step 3 either way."

## Step 3: Understand

- Re-read the issue body and labels.
- Explore only the files the issue clearly touches (use Grep/Glob, not broad scans).
- Summarise in ≤5 lines: what the problem is, where it lives, what success looks like.

**Gate:** ask the user "Ready to plan?" before moving on.

## Step 4: Plan — or adopt an existing one

Resolve the plan source:

- **Explicit second argument** — treat as a path or URL if it's not the literal `issue`. Read it (use WebFetch for URLs, Read for local paths).
- **Second argument is `issue`** — extract any section from the issue body titled `Plan`, `Implementation`, `Approach`, or `Proposed solution` (case-insensitive).
- **No second argument** — scan the issue body for the same section headings anyway (cheap), but do not assume one exists.

**Completeness check** — a plan is complete enough to adopt only if it covers all three:

1. Which files will change and why.
2. Test / verification steps.
3. Any architectural decisions or non-obvious trade-offs called out.

- **Complete:** summarise the adopted plan in 5 lines for the user, confirm, skip to Step 5. Do not call `ExitPlanMode` — plan mode was never entered.
- **Partial:** enter plan mode, seed it with the user's plan, call out the gaps, draft additions, finish with `ExitPlanMode`.
- **Absent:** enter plan mode fresh, finish with `ExitPlanMode`.

## Step 5: Implement

- Edit + commit cycles. Keep commits focused and logical. Reference the issue in the body: `Closes #<N>` on the final commit (or the commit that completes the fix).
- **Never** include `Co-Authored-By: Claude` or any Claude attribution. Follow the repo's git-commits rule in `.claude/CLAUDE.md`.
- All changes stay local. No pushes in this step.

## Step 6: Auto-verify

Run in this order, stop on first failure and fix before continuing:

1. `go vet ./...`
2. The focused test command from the table in `.claude/CLAUDE.md` under "Testing" (match what changed: `pkg/`, `cmd/cli/tui/`, `cmd/cli/`, etc.). When unsure, run `go test ./...`.
3. Build both binaries (quick compile-check during iteration, full build before reporting done):
   ```bash
   # Quick iterative check
   go build -o /dev/null ./cmd/cli ./cmd/gui

   # Full build (needed before push)
   go build -o build/gitbox ./cmd/cli
   cp assets/appicon.png cmd/gui/build/appicon.png
   cp assets/icon.ico    cmd/gui/build/windows/icon.ico
   cd cmd/gui && wails build
   cd ..
   ```

Doc-only changes (`.md`, `docs/`, comments, `.claude/`, `.gitignore`, `go.mod` tidy) skip the builds but still run `go vet` and relevant tests.

## Step 7: User smoke-test gate

This is a hard pause. Follow the "Never push without asking first" protocol from `.claude/CLAUDE.md` verbatim.

**Runtime-affecting changes** (anything under `cmd/` or `pkg/`, build config, UI behaviour):

1. State the commit SHA(s) on the branch.
2. List the binary paths the user should launch:
   - Windows: `build/gitbox.exe`, `cmd/gui/build/bin/GitboxApp.exe`
   - macOS / Linux: equivalent paths.
3. Name a concrete check: what to click/type, expected output, regression to rule out. Be specific — "open Preferences → Open In Editor, confirm no cmd.exe flash" is good; "test it" is not.
4. Show the exact push command you intend to run next (`git push -u origin <branch>`). Describe it, do not execute it.

**Trivial or doc-only changes:**

One line — "Commit `<sha>` ready. Push directly to main / open a PR / hold?" — and wait.

**Wait for explicit go-ahead** ("ok, push it", "go ahead", "ship it"). If the user reports a regression, fix it and re-offer the build. Never ship over an unresolved failure.

## Step 8: Sync with main

After go-ahead and before pushing:

```bash
git fetch origin
git log --oneline HEAD..origin/main
```

- If `origin/main` has not advanced, skip to Step 9.
- If it has advanced, offer `git rebase origin/main`. On conflicts, surface each file and ask for guidance — do not auto-resolve non-trivial conflicts. After rebase: re-run Step 6 (auto-verify) and re-offer Step 7 (smoke test). The user's earlier go-ahead applied to the pre-rebase state; a rebase invalidates it.

## Step 9: Push gate

After re-confirmed go-ahead:

```bash
git push -u origin <branch>
```

If Step 8 rebased commits that were already pushed, use `--force-with-lease` instead of a plain push.

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

**Anonymise before showing:** no local paths (`~/00.git/...`, `C:\Users\...`), no private account keys (`github-<realname>`), no unrelated org/repo names. See "Anonymize before posting to GitHub" in `.claude/CLAUDE.md` for the full rule.

Show the title and body. **Wait for explicit go-ahead.** Then:

```bash
gh pr create --title "<title>" --body "$(cat <<'EOF'
<body>
EOF
)"
```

## Step 11: Stop

- Report the PR URL from the `gh pr create` output.
- Do **not** merge, do not auto-enable merge, do not comment further.
- Tell the user: "PR up at `<url>`. Run `/merge-issue <PR#>` when you're ready to merge."

## Behavior notes

- The skill is resumable — re-invoking it mid-flow picks up from the right phase based on git state, not from Step 1.
- If the user interrupts between gates, stay put. Never advance a gate without explicit instruction.
- If any automated step in Step 6 fails, stop and fix. Do not skip to Step 7 with a broken build.
- The plan mode decision is made once in Step 4 and does not re-run. If the user wants to re-plan later, they can invoke plan mode manually.
