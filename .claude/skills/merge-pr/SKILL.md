---
name: merge-pr
description: Merge a PR (typically one created by /work-issue), then clean up its worktree and branch. Use when the user says "merge PR #M", "merge the branch for issue N", or invokes /merge-pr. Runs a rebase freshness check and CI gate, waits for explicit go-ahead before merging, then removes the sibling worktree.
---

# /merge-pr — Merge a PR and clean up

**IMPORTANT:** Before starting, tell the user "I'm executing `/merge-pr <PR#>`" and create a todo list with the phases below.

## Usage

```text
/merge-pr <PR#>
```

`<PR#>` is the pull request number.

## Golden rules

- **Never merge without explicit user go-ahead.**
- **Never force-merge a PR with failing CI.** Report which checks failed and stop.
- **Never remove a worktree with uncommitted or unpushed state** unless the user overrides.
- **Derive the gh user from this clone's `remote.origin.url`** — never hardcode.

## Universal gh-user detection

Same snippet as `/work-issue`. Run it before every `gh` call.

```bash
origin_url=$(git config --get remote.origin.url)
expected_owner=$(echo "$origin_url" | sed -E 's|.*[:/]([^/]+)/[^/]+$|\1|' | sed 's|\.git$||')
active_user=$(gh auth status 2>&1 | grep -oE 'account [A-Za-z0-9-]+' | head -1 | awk '{print $2}')
if [ "$active_user" != "$expected_owner" ]; then
  gh auth switch --user "$expected_owner"
fi
```

---

## Step 1: Preflight

- Run the gh-user detection.
- Fetch PR metadata:

  ```bash
  gh pr view <PR#> --json number,title,state,mergeable,statusCheckRollup,headRefName,baseRefName,url
  ```

- Report to the user: PR number, title, base branch, head branch, state, mergeable status.
- If `state` is not `OPEN`, stop — nothing to do.
- Locate the local worktree: `git worktree list` and match on branch name.

## Step 2: Freshness check

- `cd` into the PR's worktree.
- `git fetch origin`.
- Check whether the PR branch is rebased on the latest `origin/<baseRefName>`:

  ```bash
  git log --oneline origin/<baseRefName>..HEAD    # commits ahead of base
  git log --oneline HEAD..origin/<baseRefName>    # commits on base not in PR
  ```

- If base has advanced:
  1. Offer `git rebase origin/<baseRefName>`.
  2. Resolve conflicts (surface each to the user; do not auto-resolve non-trivial ones).
  3. Re-push with `git push --force-with-lease`.
  4. Re-check CI — the push will re-trigger it.

## Step 3: CI gate

- Re-read `statusCheckRollup` after any re-push.
- All checks must be `SUCCESS` (or neutral).
- If any are `FAILURE` / `CANCELLED` / `TIMED_OUT`, stop. Report which checks failed with their log URLs (`gh run view` for Actions). Do not propose merging.
- If any are still `IN_PROGRESS` / `QUEUED`, tell the user CI is still running and stop. They can re-invoke the skill when CI completes.

## Step 4: Merge gate

- Show the exact merge command and wait for explicit go-ahead:

  ```bash
  gh pr merge <PR#> --squash --delete-branch
  ```

- Prefer `--squash` unless the repo convention says otherwise (scan `gh pr list --state merged --limit 5 --json mergeCommit` if unsure). `--delete-branch` deletes the remote branch after merge.

**Wait for explicit go-ahead** ("merge it", "go ahead", "ship it").

## Step 5: Cleanup

After successful merge:

1. Leave the worktree: `cd` back to the main clone.
2. Update main: `git fetch origin && git checkout main && git pull --ff-only origin main`.
3. Check the worktree for uncommitted or unpushed state before removing:

   ```bash
   cd <worktree-path>
   git status --porcelain        # must be empty
   git log --oneline @{u}..HEAD  # must be empty (branch was merged, so local == remote == 0)
   ```

   If either is non-empty, stop and ask the user. Do not run `git worktree remove --force` without explicit instruction.
4. Remove the worktree and local branch:

   ```bash
   cd <main-clone>
   git worktree remove <worktree-path>
   git branch -D <branch-name>
   git worktree prune
   ```

5. Report: PR merged, worktree removed, local branch deleted. Issue `#<N>` (from the PR body's `Closes #N`) will have been closed by GitHub automatically.

## Behavior notes

- The skill is resumable — if the merge succeeded but cleanup failed, re-invoking it from the main clone will notice the PR is `MERGED` and skip to Step 5.
- If the worktree was already removed manually, skip Step 5 silently.
- Never delete the `main` branch. Never run `gh pr merge` on a PR whose `baseRefName` is not the repo's default branch without confirming with the user first.
