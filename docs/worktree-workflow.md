# Worktree-based parallel workflow

I use this workflow to work on several GitHub issues at once, each in its own Claude Code or Codex session and its own branch, without the sessions stepping on each other. Two skills drive it: `/work-issue <N>` creates a sibling worktree and takes the issue through plan, code, verification, push and PR; `/merge-pr <PR#>` merges the PR and cleans up.

The skills enforce my publishing rule: nothing reaches GitHub until I say so. In Codex I run `$work-issue <N>` and `$merge-pr <PR#>` with the same arguments and gates. Both tools load the shared instructions and skills described in [developer-guide.md](developer-guide.md#working-with-claude-code-and-codex).

## What this workflow gives me

- One session per issue, each in its own worktree and branch.
- An automatic `gh auth switch` to the account that owns this clone's `origin`, so I never push from the wrong account. The skills derive it from `remote.origin.url` and never hardcode a user, so they work in a fork too.
- An explicit pause before every publish step: push, PR and merge.
- A clean exit: when the PR merges, the worktree and the local branch go away.

## Opening a session per issue

From the main clone:

1. Open a fresh Claude Code or Codex window.
2. Type `/work-issue 42`.
3. The skill reads the issue and shows its title, labels, branch name and worktree path. The branch is `<type>/<N>-<slug>`: `fix` for a `bug` issue, `feat` for an `enhancement`, `chore` otherwise. The slug comes from the title, and the worktree goes to `../gitbox-<N>-<slug>`.
4. Once I confirm it is the right issue, the skill creates the worktree from `origin/main` and continues there.

To work on issue 43 at the same time, I open a second window and run `/work-issue 43` there. I can also open a new session directly inside a worktree; the skill resumes from the right phase.

## The gates

The skill stops at each gate and tells me what comes next:

1. **Preflight**: it checks that it runs in the main clone, switches the `gh` account, fetches `origin`, reads the issue, and proposes the branch and worktree path.
2. **Create worktree**: after my confirmation it runs `git worktree add ../gitbox-<N>-<slug> -b <branch> origin/main`.
3. **Understand**: it re-reads the issue, summarises it in five lines and asks whether I'm ready to plan.
4. **Plan**: it adopts my plan or writes one with me. See [Providing my own plan](#providing-my-own-plan).
5. **Implement**: edit and commit cycles. The final commit carries `Closes #<N>`. Everything stays local.
6. **Auto-verify**: `go vet ./...`, the focused tests for what changed, and the GUI build with a `GitboxApp --version` smoke check. Doc-only changes skip the build.
7. **Smoke-test gate**: it gives me the commit, the binary path to launch and one concrete check, plus the push command it intends to run. I test. If something is wrong, it fixes it and offers a new build. When it's good, I say "push it".
8. **Sync with main**: it fetches and, if `origin/main` moved, offers `git rebase origin/main`.
9. **Push gate**: it runs `git push -u origin <branch>`.
10. **PR gate**: it drafts an anonymised title and body (`Closes #<N>`, what changed, test plan), waits for my approval and runs `gh pr create`.
11. **Stop**: it reports the PR URL and tells me to run `/merge-pr` when I'm ready.

The skill never merges. Merging is a separate, deliberate action.

## Providing my own plan

When I already have a plan, I pass its source as the second argument:

```text
/work-issue 42 ./plans/42.md      # local file
/work-issue 42 https://.../42     # URL
/work-issue 42 issue              # a section of the issue body
```

With `issue`, the skill reads the section of the issue body titled `Plan`, `Implementation`, `Approach` or `Proposed solution`. Without a second argument it still looks for those sections.

A plan is complete when it covers three things:

- Which files change and why.
- Test or verification steps.
- Architectural decisions or non-obvious trade-offs.

A complete plan is adopted as is: the skill summarises it, asks me to confirm and skips plan mode. A partial plan seeds plan mode, where the skill calls out the gaps and we fill them. Without a plan, the skill plans from scratch. Either way, I approve the plan before it implements anything.

## Running two sessions in parallel

The source trees are isolated, but some state is shared across worktrees:

- `~/.config/gitbox/gitbox.json` is one file. I never run credential setup or first-run flows in two sessions at once; they fight over the same file.
- The SSH agent, Git Credential Manager and the system git config are global. Pushes are fine; two credential setups at once are not.
- `go test -short ./...` in parallel is fine. The scenario test in `go test ./...` works against the same provider fixture, so I run it in one session at a time.
- Two worktrees can't have the same branch checked out; git refuses.
- I never push the same remote branch from two worktrees.

## Worktrees and Gitbox's own clone detection

When the main clone lives under one of Gitbox's clone roots, a sibling worktree sits inside that root too. The reconcile pass finds it and lists it under **Unmatched clones** as "a git worktree of another clone". A worktree isn't a clone of its own: its `.git` is a pointer file to the main clone, so Gitbox never offers to adopt it.

Three ways to live with it:

1. Ignore it. The row disappears when `/merge-pr` removes the worktree.
2. Click **Ignore** on its row in Unmatched clones to hide it.
3. Create the worktree outside every clone root with `git worktree add` by hand. The skill always uses the sibling path.

## When main advances under me

When a PR lands while I'm still working on mine, usually from my other session, `origin/main` moves. The Sync-with-main gate fetches, lists the new commits and offers `git rebase origin/main`. The skill shows me each conflicting file and doesn't resolve non-trivial conflicts on its own.

After a rebase, auto-verify and the smoke-test gate run again. My earlier "push it" doesn't carry over, because the rebase changed what I tested. If the rebased commits were already pushed, the skill pushes with `--force-with-lease`.

## Merging

When I'm ready:

```text
/merge-pr <PR#>
```

It runs in any session, the one that created the worktree or a fresh one. The skill:

1. Reads the PR and finds its worktree by branch name.
2. Checks that the worktree has no uncommitted or unpushed changes, and stops if it has, unless I override. This check runs before the merge because `gh pr merge --delete-branch` also deletes the local branch and its worktree.
3. Fetches and, if the base branch moved, offers a rebase and re-pushes with `--force-with-lease`.
4. Gates on CI: every check must pass. It stops on a failed check and reports its log; it stops too while checks are still running, and I re-run it later.
5. Shows me `gh pr merge <PR#> --squash --delete-branch` and waits for my go-ahead.
6. After the merge, returns to the main clone, fast-forwards `main`, removes any worktree and local branch `gh` left behind, and prunes stale worktree metadata.

If the PR body says `Closes #N`, GitHub closes the issue on merge.

## Cleanup reference

To clean up by hand (abandoned work, a crashed session):

```bash
# Remove a worktree. Refuses if dirty.
git worktree remove ../gitbox-42-my-slug

# Force-remove even if dirty.
git worktree remove --force ../gitbox-42-my-slug

# Prune metadata for worktrees deleted from disk without git knowing.
git worktree prune

# Delete the local branch.
git branch -D fix/42-my-slug

# List the registered worktrees.
git worktree list
```

## FAQ

**Can I work on a branch without an issue?** Not with these skills: `/work-issue` requires an issue number. For exploratory work I open a short `chore:` issue first.

**What if I abandon the work mid-flow?** I leave the worktree where it is. When I run `/work-issue <N>` again, the skill reads the git state (branch, worktrees, last commit, open PR) and resumes at the right phase. To drop the work, I run `git worktree remove --force` and `git branch -D`.

**How do I continue a worktree on another machine?** I push the branch and check it out there normally. The worktree layout doesn't need to be replicated.

**Can I skip the plan phase?** I pass a complete plan as the second argument. The skill adopts it and only asks me to confirm before implementing.

**What if `/merge-pr` fails after the merge but before cleanup?** I run it again from the main clone. It sees the PR is already merged and goes straight to cleanup.
