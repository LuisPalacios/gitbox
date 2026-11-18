---
name: coordinate-issues
description: Coordinates a finite batch of independent GitHub issues through a supervisor, worker agents and isolated worktrees. Use to select and run parallel issues from a roadmap or the open backlog, or resume a coordinated batch.
---

# $coordinate-issues — Supervise parallel issues

Use the current conversation as the supervisor and delegate one issue per worker. Tell the user that this skill is running. Read `AGENTS.md`, `.codex/context/repository-layout.md` and the relevant parts of `.agents/skills/work-issue/SKILL.md` before starting. A request to explain or design this workflow does not start a batch.

## Invocation and authority

```text
$coordinate-issues --roadmap 214 --count 3 --active none
$coordinate-issues --roadmap 214 --count 3 --active 162,170
$coordinate-issues --no-roadmap --count 3 --active none
$coordinate-issues --resume <owner>
```

These are arguments interpreted by the supervisor, not shell commands. Default to three issues, with fewer concurrent workers when capacity requires it; reserve one agent slot for the supervisor. If fewer independent issues qualify, explain and select fewer. Finish this finite batch rather than continually consuming the backlog.

- `--roadmap N` limits selection to that umbrella. `--no-roadmap` selects from open issues. When neither appears, inspect likely roadmaps; use an unambiguous one and state the choice, or ask which pool to use.
- `--active none` explicitly declares no other sessions; `--active N,M` excludes those issues and their affected areas. If absent, ask once about other sessions before reserving work. Continue read-only discovery while waiting. Local worktrees and the registry cannot reveal every external session.
- Starting a batch authorizes local selection, worktrees, worker plans, implementation, tests and commits within its scope. The supervisor handles the ordinary start/plan confirmations of `work-issue` on the user's behalf.
- `--publish`, or equivalent explicit current-session instructions, authorizes push and creation/update of PRs for the selected batch's own branches after verification. `--create-subissues` authorizes justified subissues; they do not silently expand the batch. `--update-roadmap` authorizes minimal completion-checkbox edits on the chosen umbrella after verified resolution.
- Without these publication permissions, finish local work and present the concrete result and proposed actions for approval. No option here authorizes merge, force push, push to the default branch, credential changes, shared production configuration or remote staging. A reservation never grants filesystem or tool permission.
- Keep authorization and scope in the conversation and private batch record. On resumption in a new session, recover progress but obtain fresh publication authorization. Do not treat old journal entries or flags as current permission.

## Shared registry and prerequisites

Read [protocol.md](protocol.md) before using the registry or resuming work. Run the helper through `uv run`; it requires Python 3.11+, Git and only Python's standard library (`sqlite3` for transactions). It installs no project dependencies. Cache access follows session permissions.

```bash
uv run .agents/skills/coordinate-issues/scripts/coordinate.py --repo . status
uv run .agents/skills/coordinate-issues/scripts/coordinate.py --repo . apply --request-file <private-request.json>
```

The shared registry lives in `<git-common-dir>/gitbox-coordination/registry.sqlite`. Keep requests, reports and evidence under that private coordination directory, never in tracked documentation or PR bodies. Cooperating sessions in this clone and its worktrees share atomic reservations. Independent clones, machines and non-cooperating sessions require explicit coordination. Do not put the SQLite registry on a network share.

Use the available agent creation, messaging, inspection and waiting capabilities. Do not assume a particular tool name or claim to have launched workers without tool evidence. If delegation is unavailable, report the limitation and offer serial work or separate sessions; do not silently call that parallel execution.

## Workflow

Track this checklist in the conversation and persist issue progress in the registry:

- [ ] Inspect live evidence and other sessions; choose a finite compatible batch.
- [ ] Atomically reserve issues and create or recover independent worktrees.
- [ ] Delegate scoped work and supervise questions, overlap and shared resources.
- [ ] Review changes and verify acceptance against each exact commit.
- [ ] Publish only authorized results, check the local gate and report remaining work.

### 1. Select from live evidence

Inspect Git status, worktrees, branches and the registry without overwriting existing work. Fetch the base when permitted. Read each candidate issue and its comments, prerequisites, related PRs and their changed files; inspect the current implementation to validate old plans. A roadmap checkbox is a hint, not proof of issue state or merged code.

Explain the chosen issues, their dependencies, affected files, expected tests and why they can progress independently. Exclude already claimed issues, ongoing external work, unresolved prerequisite merges and maintainer decisions. Also check indirect overlap: public APIs, shared docs/catalogs, event contracts, generated outputs and test fixtures. Separate lanes in a roadmap do not prove independence. Defer an issue when shared contracts make simultaneous implementation unsafe; assign serial ownership for small unavoidable documentation overlaps.

### 2. Reserve and isolate

Reserve the complete selected issue set in one transaction using a unique owner ID; if any claim conflicts, select again without leaving partial reservations. Record declared external issues as external reservations when unclaimed, and inspect existing owners rather than replacing them. Record their affected areas for selection. Keep them occupied until the user confirms that external work ended.

After reservation and before creating worktrees or launching workers, reread all current claims and inspect their issue scopes, including claims whose details are still empty. Another supervisor may have reserved different issues affecting the same files since selection. Yield and reselect an unstarted conflicting issue rather than launch overlapping work. Recheck this at scope changes; the registry's issue uniqueness alone does not enforce file/API isolation.

Create one branch and sibling worktree per issue using `work-issue` naming and base rules. Create them sequentially, recording the actual paths and base commit. Never check out a different branch in an occupied worktree. A partial setup failure retains its reservation until the supervisor accounts for created worktrees and releases only safe entries.

On resume, inspect the recorded agents, processes, worktrees, Git state and PRs. Adopt existing work instead of creating another branch. An old heartbeat does not prove the workers stopped. Use the explicit generation-checked takeover only after confirming the old supervisor and workers cannot still mutate state. Fencing protects registry writes, not running shell processes or GitHub actions.

### 3. Delegate and communicate

Give each worker this contract, filled with concrete values:

```text
Issue and acceptance criteria:
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

The supervisor reviews each diff, tests and evidence rather than accepting a worker's completion claim. Define required criteria before implementation, then record PASS, FAIL or PENDING for the exact head and base commits. Read `.codex/context/testing-patterns.md`, `.codex/context/building.md` and `.agents/skills/test-plan/SKILL.md` as applicable. Run focused regressions, required vet/build checks and the full health gate before a PR. Use the harness-only verification exception where documented. Run native/remote checks required by the change, scheduling shared resources and obtaining missing staging permissions.

For this coordinated mode, an automated behavioral smoke can satisfy a user smoke criterion when it proves the same behavior with isolated fixtures and available browser/desktop tools. Record the expected and observed binary SHA, platform, fixture, actions, assertions and evidence location. A successful `--version`, screenshot alone or browser test does not prove native window behavior. Do not substitute a version smoke for a behavioral test. Ask the user only for required checks that cannot be performed reliably with available tools; keep those criteria PENDING until confirmed. A user-reported regression always blocks readiness.

Commit the final change before collecting final evidence; generated output and tracked files must be clean. After any head/base change, including rebase, invalidate prior evidence and rerun relevant checks and smoke on the new result. Never record a mandatory failure or missing check as PASS. The registry verifies the recorded criteria/revisions; it does not execute tests or authenticate reports.

### 5. Publish, reconcile and finish

Only the supervisor publishes, after checking current-session authority, the clean worktree, current remote base, exact head, required PASS evidence and anonymized content. Sync against the live base; rebase and reverify when needed. Force push requires separate explicit permission and a lease against the inspected remote SHA. Before each external mutation, journal its intent with a unique action ID; record the returned commit, PR or issue identifier afterward. On a retry, inspect the remote first to determine whether the action already succeeded. Never blindly repeat a PR or subissue creation after a timeout.

Publish each branch/PR separately. An open PR keeps its issue reserved, including while its gate or GitHub check is pending or failing. Before declaring a PR ready, confirm `./scripts/gate.sh` passed on the exact published head (its marker or `local-gate` status) and that the informational GitHub check has not failed; a missing gate means published/pending, not complete. Gates for several branches can run at once; each lane clones its own SHA. Stop before merge and direct the user to `$merge-pr <PR#>`. Release execution resources only after their actual users stop; preserve issue ownership while work or an open PR remains.

When an authorized merge later resolves an issue, or resumption discovers an external merge, confirm both the merged PR and the complete issue resolution. If umbrella-update permission is current, follow [protocol.md](protocol.md) to mark only its matching checkbox. A partial PR, closed-unmerged PR or green gate or CI does not complete the issue. Release the issue claim only after confirming no active worker remains and recording its resolved or explicitly abandoned outcome. Never delete dirty or unpushed work to free a reservation.

Finish with each issue's branch/worktree, commit, verification, PR/gate state, blockers and exact next action. Keep local evidence links in the private report and public text free of local paths, account details, credentials or agent attribution.
