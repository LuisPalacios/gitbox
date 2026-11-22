# Worktree-based parallel workflow

I use this workflow to work on several GitHub issues at once, each in its own Claude Code or Codex session and its own branch, without the sessions stepping on each other. Two skills drive it: `/work-issue <N>` creates a sibling worktree and takes the issue through plan, code, verification, push and PR; `/merge-pr <PR#>` merges the PR and cleans up.

The skills enforce my publishing rule: nothing reaches GitHub until I say so. In Codex I run `$work-issue <N>` and `$merge-pr <PR#>` with the same arguments and gates. Both tools load the shared instructions and skills described in [developer-guide.md](developer-guide.md#working-with-claude-code-and-codex).

## Independent issues and work units

I use two forms of work: an independent issue with no parent or children, or a work unit that contains tasks. A work unit carries the `work-unit` label and has no parent. Its tasks are direct GitHub sub-issues in the same repository, with no children of their own. I keep exactly these two levels; I do not add an umbrella above the work unit. A title, a body reference or a checklist does not establish a parent-child relationship.

I create the container with `$wu-container <description>` in Codex or `/wu-container <description>` in Claude Code. The skill drafts its purpose, the problem, scope and exclusions, high-level requirements with stable `HLR1` IDs, and populated acceptance criteria with `AC1` IDs linked to those requirements. It checks for existing work and presents the concrete issue before any publication that needs my approval. For one independent issue I use `wu-container --independent`; I implement that issue with `work-issue`.

I then use `wu-tasks <N>` to decompose a work unit. The skill reads the entire container and its existing children, proposes tasks that cover its acceptance criteria, and identifies dependencies and a useful execution order. It drafts the task bodies before requesting any missing publication authorization. It creates and links only the approved tasks, verifies the native relationships and list order, and resumes partial work by inspecting existing effects before retrying. It does not rewrite the container's requirements or acceptance criteria to fit the tasks.

## One supervisor for a batch

I use `wu-coordinate-tasks` when I want one conversation to select and supervise up to three compatible pending tasks from one work unit. The supervisor checks live issues, comments, dependencies, open PRs and affected files before choosing a finite batch. It selects only open direct sub-issues and excludes unresolved prerequisites. It may choose fewer tasks when overlap or available agent capacity makes three unsafe. It keeps one agent slot for itself and delegates each task to a worker in its own worktree.

In Codex I pass the work unit's issue number and declare work already underway elsewhere:

```text
$wu-coordinate-tasks <N> --count 3 --active none
$wu-coordinate-tasks <N> --count 3 --active <issue-numbers>
$wu-coordinate-tasks <N> --resume <owner>
```

In Claude Code I replace `$wu-coordinate-tasks` with `/wu-coordinate-tasks`. There the supervisor asks me once to run `/add-dir` for each new worktree before it launches workers, because sibling worktrees sit outside the session's working directory. The work unit number is mandatory, including on resume: if I omit it, the supervisor asks and never infers a container or falls back to the global backlog. If I omit the active-session information, it asks once before reserving the batch. Checkboxes provide context; native relationships and live issue and PR state determine which work belongs to this unit and remains pending. I do not need to open a conversation for each worker.

The invocation authorizes local selection, planning, implementation, verification and commits. I can explicitly authorize publication for the selected batch with `--publish`, which covers its branch pushes and PR creation or updates after checks pass. `--update-work-unit` authorizes minimal completion-checkbox updates to that container after verified resolution. I create additional tasks through `wu-tasks`; the coordinator does not create them. These options do not authorize a merge, a force push, real credential changes or remote staging. The supervisor keeps publication and questions in the main conversation; workers do not publish. Asking how the skill works does not start a batch.

`wu-coordinate-tasks` replaces `coordinate-issues`; `--roadmap`, `--no-roadmap`, `--create-subissues` and `--update-roadmap` no longer apply. Existing roadmaps such as #214 need an explicit review and authorized conversion before they qualify as work units. The skills do not silently label them or turn their references into native sub-issues.

### Reservations and recovery

Cooperating Claude and Codex sessions share an atomic SQLite registry under `<git-common-dir>/gitbox-coordination/registry.sqlite`. The registry covers a local clone and its worktrees; it does not coordinate separate clones or machines. I still declare external sessions so the supervisor can account for their issue and file ownership.

The supervisor reserves the selected tasks together before creating worktrees. A conflict leaves the whole request unreserved, so it can reconsider the batch. The registry also excludes another batch for the same work unit until the supervisor releases all its task claims. It records the owner and generation, work unit, worktree, branch, worker, progress and verification evidence. An old heartbeat does not free a reservation: the supervisor checks surviving workers, Git state and PRs before recovering it with a new generation. An open PR keeps its task occupied. On interruption, I pass the work unit number and recorded owner to resume; the supervisor checks existing effects before retrying a push or PR creation.

The helper requires `work_unit` when starting a batch. It accepts an unscoped `external: true` reservation only to exclude work from another session. The registry keeps its historical `roadmap` storage field for compatibility; that field alone never proves native membership. When resuming an older batch, the supervisor validates the chosen work unit and every pending task against GitHub before continuing. It preserves old records and unfinished work.

I can run separate supervisor sessions for different work units. Their tasks still share a repository and may overlap in files, APIs or generated output, so the supervisor checks those boundaries before delegating work.

GUI sessions, shared configuration, credential operations and staging destinations need exclusive coordination. A reservation does not grant permission to use a resource. The supervisor serializes GUI smoke tests because test-mode instances share an application identity, and checks which binary and window it is testing.

### Verification and completion

The supervisor links each required check to the tested commit and base, with PASS, FAIL or PENDING and the command or interaction evidence. A rebase or new change invalidates the old evidence. An automated smoke can satisfy an interactive criterion when it verifies the correct binary and commit, isolated fixture, window, actions and expected behavior. A version string or screenshot alone does not prove the interaction works. I perform only the required steps that the available tools cannot verify.

Failing or pending required checks block publication. Once publication has authorization and the local checks pass, the supervisor opens the PR and confirms the local gate passed on its head before calling it ready. It stops before merge; I use `merge-pr` separately. With authorization to update the work unit, the merge workflow or a resumed supervisor marks only existing checklist entries whose merged PR fully resolves the task. The optional checklist helper retains the filename `roadmap.py`; a checklist never replaces native membership. A partial PR or a successful gate or CI alone does not complete a task, and finishing a batch does not automatically close its container or prove all its acceptance criteria.

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
6. **Auto-verify**: `go vet ./...`, the focused tests for what changed, and the GUI build with a `GitboxApp --version` smoke check. Doc-only changes skip the build. Then `./scripts/gate.sh` verifies the final commit on macOS, Linux and Windows (see [Local gate](local-gate.md)).
7. **Smoke-test gate**: it gives me the commit, the binary path to launch and one concrete check, plus the push command it intends to run. I test. If something is wrong, it fixes it and offers a new build. When it's good, I say "push it".
8. **Sync with main**: it fetches and, if `origin/main` moved, offers `git rebase origin/main`.
9. **Push gate**: it runs `git push -u origin <branch>`, then `./scripts/gate.sh --report` to show the gate result on the commit.
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
4. Gates on the local gate: the head commit needs a passing gate marker or `local-gate` status, and it offers to run the gate when there is none. A failed GitHub check also stops it and reports its log; a check still running does not.
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
