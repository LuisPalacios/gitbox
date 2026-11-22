---
name: wu-coordinate-tasks
description: Coordinates up to three pending task sub-issues of one explicit work unit through a supervisor and isolated workers. Requires the container issue number, including on resume; never selects from the global backlog.
claude:
  argument-hint: "<work-unit> [--count 1-3] [--active none|N,M] [--resume <owner>] [--publish] [--update-work-unit]"
---

# {{INVOKE}}wu-coordinate-tasks — Supervise tasks within one work unit

Use the current conversation as the supervisor and delegate one issue per worker. Tell the user that this skill is running. Read [issue-rules.md](../wu-container/issue-rules.md), `{{ENTRY}}`, `{{CONTEXT}}/repository-layout.md` and the relevant parts of `{{SKILLS}}/work-issue/SKILL.md` before starting. A request to explain or design this workflow does not start a batch.

## Invocation and authority

```text
{{INVOKE}}wu-coordinate-tasks 214 --count 3 --active none
{{INVOKE}}wu-coordinate-tasks 214 --count 3 --active 162,170
{{INVOKE}}wu-coordinate-tasks 214 --resume <owner>
{{INVOKE}}wu-coordinate-tasks help
```

These are supervisor arguments, not shell commands. The positional container number is mandatory, including on resume. If absent, ask which work unit to use and wait before selection or reservation; never infer it from a roadmap, registry or recent issue. Help explains usage without operations. `--roadmap` and `--no-roadmap` are retired; an old invocation needs an explicit work-unit number and validation, not a backlog fallback.

`--count` accepts 1 to 3 tasks. Default to three tasks, with fewer workers when capacity or independence requires it; reserve one agent slot for the supervisor. Finish this finite batch rather than continually consuming all children. Distinct work units can have separate supervisor sessions, but their files, contracts and global resources still need conflict checks.

- `--active none` declares no other sessions; `--active N,M` excludes those issues and their affected areas. If absent, ask once before reserving work; continue read-only discovery while waiting. Local evidence cannot reveal every external session. Investigate contradictory live worktree evidence even after a declaration.
- Starting a batch authorizes local selection, worktrees, worker plans, implementation, tests and commits for the chosen work unit's direct pending tasks. The supervisor handles ordinary `work-issue` start/plan confirmations.
- `--publish`, or equivalent current-session instructions, authorizes push and creation/update of the selected tasks' own PRs after verification. `--update-work-unit` authorizes only minimal optional task-completion checkbox edits after verified resolution; it does not change HLR/AC or close the parent.
- Task creation belongs to `wu-tasks`; `--create-subissues` is retired. New scope does not silently expand this batch. Report any necessary follow-up and its parent AC, then use `wu-tasks` with separate authority when requested.
- Without publication authority, finish local work and present concrete results and proposed actions. No option authorizes merge, force push, push to the default branch, credential changes, production configuration or remote staging.
- Keep scope and authority in the conversation and private batch record. On resume, recover progress but obtain fresh publication authority for the new session. Old journal entries are not permission.

## Shared registry and prerequisites

Read [protocol.md](protocol.md) before using the registry or resuming work. Run the helper through `uv run`; it requires Python 3.11+, Git and only Python's standard library (`sqlite3` for transactions). It installs no project dependencies. Cache access follows session permissions.

```bash
uv run {{SKILLS}}/wu-coordinate-tasks/scripts/coordinate.py --repo . status
uv run {{SKILLS}}/wu-coordinate-tasks/scripts/coordinate.py --repo . apply --request-file <private-request.json>
```

The shared registry lives in `<git-common-dir>/gitbox-coordination/registry.sqlite`. Keep requests, reports and evidence under that private coordination directory, never in tracked documentation or PR bodies. Cooperating sessions in this clone and its worktrees share atomic reservations. Independent clones, machines and non-cooperating sessions require explicit coordination. Do not put the SQLite registry on a network share.

Use the available agent creation, messaging, inspection and waiting capabilities. Do not assume a particular tool name or claim to have launched workers without tool evidence. If delegation is unavailable, report the limitation and offer serial work or separate sessions; do not silently call that parallel execution.

<!-- harness:claude -->
### Claude Code mechanics

- Launch each worker with the Agent tool as a fresh agent, not a fork, and never with `isolation: "worktree"`: that option creates a temporary worktree and branch of its own instead of the sibling worktree this workflow records. A fresh worker knows only its prompt, so give it the complete contract and tell it to read `{{SKILLS}}/work-issue/SKILL.md` and this skill first.
- Workers run in the background and a notification resumes this conversation when one stops. Waiting for that notification is supervision: do not poll, sleep or duplicate a worker's task meanwhile. The batch still ends only when every issue has a recorded outcome.
- A worker that stops to report its plan, a question or a blocker keeps its context. Answer it with SendMessage and record its agent ID as the claim's `worker`. Workers cannot ask the user; relay their questions.
- A worker can spawn agents of its own here, so the contract's ban on nested delegation is the only limit. Keep that line in every contract.
- The sibling worktrees sit outside this session's working directory, so each `cd` and edit there asks for permission, and a background worker's prompts appear in this conversation. After creating the worktrees and before launching workers, ask the user once to run `/add-dir <absolute-worktree>` for each one. Never change `.claude/settings.json` to grant that access yourself.
<!-- /harness -->

## Workflow

Track this checklist in the conversation and persist issue progress in the registry:

- [ ] Validate the explicit work unit, inspect live evidence and other sessions; choose compatible pending children.
- [ ] Atomically reserve issues and create or recover independent worktrees.
- [ ] Delegate scoped work and supervise questions, overlap and shared resources.
- [ ] Review changes and verify acceptance against each exact commit.
- [ ] Publish only authorized results, check the local gate and report remaining work.

### 1. Select from live evidence

For new selection, validate the parent using the shared issue rules: open, marked `work-unit`, same repository, no parent, with purpose, problem, scope, HLR and populated AC. Read its entire body, relevant comments and references. Fetch every page of its native children in list order, including closed tasks; verify selected children point back to it and are leaves without the `work-unit` label. An empty container needs `wu-tasks`; an unmarked legacy roadmap needs an explicitly authorized conversion. Do not convert or link anything here.

Only open direct children without existing work or an open PR qualify for new selection. On resume, retain and inspect this batch's own worktrees and PRs. If the parent or a task has closed, allow read-only reconciliation and safe cleanup of confirmed resolved work; do not begin new implementation or publish against the closed scope. Confirm prerequisite work actually merged and satisfies the dependency; closed-as-not-planned is not completed. Read task AC references against the parent contract. Inspect Git status, worktrees, branches and the registry without overwriting existing work. Fetch the base when permitted. Read each candidate issue and its comments, prerequisites, related PRs and their changed files; inspect the current implementation to validate old plans. Native relationships define membership. A checkbox is neither membership nor proof of completion. Recheck membership and pending state immediately before reservation and again on resume or scope changes; a detached/reparented task blocks further execution and publication until resolved.

Explain the chosen issues, their dependencies, affected files, expected tests and why they can progress independently. Exclude already claimed issues, ongoing external work, unresolved prerequisite merges and maintainer decisions. Also check indirect overlap: public APIs, shared docs/catalogs, event contracts, generated outputs and test fixtures. Task order and different containers do not prove independence. Defer an issue when shared contracts make simultaneous implementation unsafe; assign serial ownership for small unavoidable documentation overlaps.

### 2. Reserve and isolate

Reserve the complete selected issue set with its mandatory `work_unit` in one transaction using a unique owner ID; if any claim conflicts, select again without leaving partial reservations. Record declared external issues as explicit exclusion-only external reservations when unclaimed, and inspect existing owners rather than replacing them. Record their affected areas for selection. Keep them occupied until the user confirms that external work ended.

After reservation and before creating worktrees or launching workers, reread all current claims and inspect their issue scopes, including claims whose details are still empty. Another supervisor may have reserved different issues affecting the same files since selection. Yield and reselect an unstarted conflicting issue rather than launch overlapping work. Recheck this at scope changes; the registry's issue uniqueness alone does not enforce file/API isolation.

Create one branch and sibling worktree per issue using `work-issue` naming and base rules. Create them sequentially, recording the actual paths and base commit. Never check out a different branch in an occupied worktree. A partial setup failure retains its reservation until the supervisor accounts for created worktrees and releases only safe entries.

On resume, require the supplied container to match the recorded scope, then validate its live native relationships and AC again. A legacy `roadmap` value is only a scope hint, not proof of a valid work unit; do not silently migrate legacy batches into execution. Inspect the recorded agents, processes, worktrees, Git state and PRs. Adopt existing work instead of creating another branch. An old heartbeat does not prove the workers stopped. Use the explicit generation-checked takeover only after confirming the old supervisor and workers cannot still mutate state. Fencing protects registry writes, not running shell processes or GitHub actions.

### 3. Delegate and communicate

Give each worker this contract, filled with concrete values:

```text
Work unit, task and parent AC IDs advanced:
Task acceptance criteria:
Owner, generation and registry path:
Absolute worktree, branch and base SHA:
Owned files/contracts and exclusions (including other sessions):
Plan source and required tests/build/smoke:
Authority: local work only; no publication or nested worker delegation.
Report: plan before edits; scope expansion or blockers immediately;
then commit SHA, diff summary, commands/results and remaining criteria.
```

Read `work-issue` in delegated mode: the supervisor reviews the worker's plan and gives the go-ahead. Workers use explicit working directories for every command because agents may share one filesystem and starting directory. They never edit the supervisor's checkout, another worker's files or private continuity documents. Each worker reports questions to the supervisor, not directly to the user.

Resolve ordinary technical choices within scope. Forward material product, security, acceptance or permission decisions to the user with the issue number and a recommendation; keep independent workers moving while the dependent task waits. Reassess new file/API overlap before permitting scope expansion. Exchange messages at meaningful milestones, investigate failures and continue supervision until every issue has a recorded outcome. Do not launch workers and end the turn while they still need coordination.

Use exclusive resources for the test GUI, real-config/credential operations, each remote staging destination and integration fixtures. All test-mode GUI instances may share an application identity even with separate fixtures; verify the process, binary and window before acting. Serialize heavy verification with a shared `heavy-checks` resource. Reserve `github-cli` around the entire account-selection plus `gh` operation; derive the account from origin as `work-issue` requires. This lock coordinates only this clone; cross-clone account switching still requires external coordination. Prefer sufficient connected GitHub tools when available.

### 4. Review and prove acceptance

The supervisor reviews each diff, tests and evidence rather than accepting a worker's completion claim. Define required criteria before implementation, then record PASS, FAIL or PENDING for the exact head and base commits. Read `{{CONTEXT}}/testing-patterns.md`, `{{CONTEXT}}/building.md` and `{{SKILLS}}/test-plan/SKILL.md` as applicable. Run focused regressions, required vet/build checks and the full health gate before a PR. Use the harness-only verification exception where documented. Run native/remote checks required by the change, scheduling shared resources and obtaining missing staging permissions.

For this coordinated mode, an automated behavioral smoke can satisfy a user smoke criterion when it proves the same behavior with isolated fixtures and available browser/desktop tools. Record the expected and observed binary SHA, platform, fixture, actions, assertions and evidence location. A successful `--version`, screenshot alone or browser test does not prove native window behavior. Do not substitute a version smoke for a behavioral test. Ask the user only for required checks that cannot be performed reliably with available tools; keep those criteria PENDING until confirmed. A user-reported regression always blocks readiness.

Commit the final change before collecting final evidence; generated output and tracked files must be clean. After any head/base change, including rebase, invalidate prior evidence and rerun relevant checks and smoke on the new result. Never record a mandatory failure or missing check as PASS. The registry verifies the recorded criteria/revisions; it does not execute tests or authenticate reports.

### 5. Publish, reconcile and finish

Immediately before each push or PR mutation, revalidate the live parent and task: open work unit, same native parent, leaf task, open task state and unchanged relevant HLR/AC contract. If membership, scope or criteria changed, block publication, preserve local work and review the change and authorization; reverify against any revised contract.

Only the supervisor publishes, after checking current-session authority, the clean worktree, current remote base, exact head, required PASS evidence and anonymized content. Sync against the live base; rebase and reverify when needed. Force push requires separate explicit permission and a lease against the inspected remote SHA. Before each external mutation, journal its intent with a unique action ID; record the returned commit, PR or issue identifier afterward. On a retry, inspect the remote first to determine whether the action already succeeded. Never blindly repeat a PR or subissue creation after a timeout.

Publish each branch/PR separately. An open PR keeps its issue reserved, including while its gate or GitHub check is pending or failing. Before declaring a PR ready, confirm `./scripts/gate.sh` passed on the exact published head (its marker or `local-gate` status) and that the informational GitHub check has not failed; a missing gate means published/pending, not complete. Gates for several branches can run at once; each lane clones its own SHA. Stop before merge and direct the user to `{{INVOKE}}merge-pr <PR#>`. Release execution resources only after their actual users stop; preserve issue ownership while work or an open PR remains.

Finishing this batch does not finish the work unit. Report remaining children and unproven parent AC; closing the container requires verification of every AC and separate authorization.

When an authorized merge later resolves an issue, or resumption discovers an external merge, confirm both the merged PR and the complete issue resolution. If work-unit-update permission is current, follow [protocol.md](protocol.md) to mark only its optional matching task checkbox. Native child membership remains authoritative; do not add a progress section or rewrite the parent specification. A partial PR, closed-unmerged PR or green gate or CI does not complete the issue. Release the issue claim only after confirming no active worker remains and recording its resolved or explicitly abandoned outcome. Never delete dirty or unpushed work to free a reservation.

Finish with each issue's branch/worktree, commit, verification, PR/gate state, blockers and exact next action. Keep local evidence links in the private report and public text free of local paths, account details, credentials or agent attribution.
