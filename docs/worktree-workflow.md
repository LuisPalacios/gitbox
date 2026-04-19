# Worktree-based parallel workflow

I use this workflow when I want to work on two or more GitHub issues at the same time, each in its own Claude Code session, each on its own branch, without the sessions stepping on each other. It's driven by two skills: `/work-issue <N>` creates a sibling worktree and walks the issue through plan, code, test, push, and PR; `/merge-pr <PR#>` handles the merge and cleanup.

The skills enforce the gates I care about — nothing lands on GitHub without me saying so — so I don't have to re-explain the rules in every session.

## What this workflow gives me

- One Claude session per issue, fully isolated at the filesystem level.
- An automatic `gh auth switch` to the right user for the clone, so I never push from the wrong account.
- An explicit pause before every publish step (push, PR, merge).
- A clean cleanup path: when the PR merges, the worktree and local branch disappear.

## Opening a session per issue

From the main clone:

1. Open a fresh Claude Code window.
2. Type `/work-issue 42` (or whatever the issue number is).
3. The skill derives a branch name like `fix/42-my-slug`, proposes a worktree at `../gitbox-42-my-slug`, and asks me to confirm.
4. On confirmation it creates the worktree, `cd`s into it, and moves through the phases.

If I want to work on issue 43 at the same time, I open a second Claude Code window, run `/work-issue 43` in *that* window, and now I have two parallel sessions on two separate worktrees.

## The gates

The skill pauses at each gate and waits for me. I don't have to memorise them — it tells me what's next — but here's the flow so I know what to expect:

- **Preflight** — the skill confirms the gh user, fetches origin, reads the issue, proposes a branch + worktree path.
- **Create worktree** — after I confirm, it runs `git worktree add` and switches into the new directory.
- **Understand** — it re-reads the issue and summarises in five lines. I say "ready" before planning.
- **Plan (or adopt an existing plan)** — if I pass a plan source, it reads and evaluates it. Otherwise it enters plan mode fresh. See [Providing my own plan](#providing-my-own-plan) below.
- **Implement** — edit and commit cycles. Everything stays local.
- **Auto-verify** — `go vet`, focused tests, both binaries built.
- **Smoke-test gate** — the skill hands me the binary paths and a concrete check. I test. If something's wrong I say so and it fixes. If it's good I say "push it".
- **Sync with main** — it checks whether `origin/main` moved and offers to rebase.
- **Push gate** — I approve, it pushes the branch.
- **PR gate** — it drafts title and body (anonymised), I approve, it runs `gh pr create`.
- **Stop** — it reports the PR URL and tells me to run `/merge-pr` when I'm ready.

At no point does the skill merge. Merging is a separate, deliberate action.

## Providing my own plan

If I've already thought through the issue and have a plan written down — in the issue body, a local markdown file, or a URL — I pass it to the skill:

```bash
/work-issue 42 ./plans/42.md      # local file
/work-issue 42 https://.../42     # URL
/work-issue 42 issue              # use a "## Plan" section inside the issue body
```

The skill reads the plan and evaluates whether it covers the three things it needs:

- Which files will change and why.
- Test or verification steps.
- Any architectural decisions or non-obvious trade-offs.

If the plan covers all three, the skill adopts it verbatim and skips plan mode. If it's a sketch, the skill enters plan mode with my plan as seed material and we fill the gaps together. If there's no plan at all, it plans from scratch.

## Running two sessions in parallel

A few things are shared across worktrees even though the source trees are isolated. I keep these in mind:

- `~/.config/gitbox/gitbox.json` is one file. I never run two interactive TUI credential or init-wizard flows simultaneously — they fight over the same file.
- The SSH agent and Git Credential Manager are global. Not a problem for pushes, but I don't run two credential-setup flows at once.
- `go test -short ./...` in parallel is fine. `go test ./...` (full integration tests) reads shared fixtures and can collide — I run those serially.
- Two worktrees can't have the same branch checked out. Git refuses. The error message is obvious.
- I don't push to the same remote branch from two worktrees. If I ever need to, one of them must force-push with lease after the other lands.

## Worktrees and gitbox's own orphan detection

If my gitbox-managed folder root is `~/00.git/github-<me>/<me>/`, then a sibling worktree at `~/00.git/github-<me>/<me>/gitbox-42-my-slug` sits *inside* that managed tree. When I run `gitbox status`, the adopt flow, or the GUI/TUI discovery screens, they list the worktree as an orphan repo — it doesn't match any account's expected layout.

It isn't actually orphaned. The worktree has a valid `.git` pointer file and git sees it correctly. gitbox scans by directory structure, not by reading `.git` contents, so it can't tell a worktree from an unregistered clone.

Three ways to live with it:

1. Ignore the noise. The worktree disappears when `/merge-pr` cleans up after the merge.
2. Place worktrees outside any gitbox-managed folder. This is not the skill's default today — it uses the sibling path because editors and file managers behave best there — but I can `git worktree add` manually to a different location if the noise bothers me on a particular branch.
3. Track it as a follow-up issue if it starts biting. There's no CLI flag for excluding worktree patterns from discovery yet.

## When main advances under me

If someone (usually me in the other Claude session) lands a PR while I'm still working on mine, the `origin/main` branch moves and I need to rebase. The skill handles this at the Sync-with-main gate — it fetches, checks for new commits, and offers `git rebase origin/main`. Conflicts are surfaced to me per-file; the skill doesn't auto-resolve non-trivial conflicts.

After a rebase, auto-verify and the smoke-test gate re-run. My earlier "ok, push it" doesn't carry over — rebase rewrites history, so the skill asks again.

## Merging

When I'm ready:

```bash
/merge-pr <PR#>
```

This runs in whichever Claude session — either the one that created the worktree, or a fresh window. The skill:

1. Verifies the PR is rebased on latest `origin/main` (re-pushes with `--force-with-lease` if not).
2. Gates on CI — all checks must be green.
3. Shows me the exact `gh pr merge --squash --delete-branch` command and waits for go-ahead.
4. After the merge, removes the worktree and local branch, and prunes stale worktree metadata.

It refuses to remove a worktree that has uncommitted or unpushed state unless I explicitly override.

## Cleanup reference

If I need to clean up manually (abandoned work, skill crash, whatever):

```bash
# Remove a worktree. Refuses if dirty.
git worktree remove ../gitbox-42-my-slug

# Force-remove even if dirty.
git worktree remove --force ../gitbox-42-my-slug

# Prune metadata for worktrees that were deleted from disk without git knowing.
git worktree prune

# Delete the local branch.
git branch -D fix/42-my-slug

# List what's currently registered.
git worktree list
```

## FAQ

**Can I work on a branch without an issue?** Not today. `/work-issue` requires an issue number. If I want to do exploratory work with no issue, I open one first — a single `chore:` issue is cheap and keeps the workflow consistent.

**What if I abandon the work mid-flow?** Leave the worktree where it is. The next time I re-invoke `/work-issue <N>`, the skill detects the existing worktree from `git worktree list` and resumes at the right phase. If I want the work gone, `git worktree remove --force` + `git branch -D`.

**How do I share a WIP worktree with another machine?** Push the branch to origin. On the other machine, clone and check out the branch normally — there's no need to replicate the worktree structure.

**Can I skip the plan phase?** Yes. Either pass an existing plan as the second argument, or tell the skill during the Understand gate that the plan is trivial. It will still confirm with me before implementing.

**What if `/merge-pr` fails after the merge but before cleanup?** Re-invoke it. It checks PR state first — if it's already `MERGED`, it jumps straight to cleanup.
