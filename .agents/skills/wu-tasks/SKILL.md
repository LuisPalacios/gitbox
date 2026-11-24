---
name: wu-tasks
description: Decompose an existing work unit into native GitHub task sub-issues, with acceptance-criteria coverage and explicit dependencies. Use to create or complete a work unit's task breakdown.
---

# $wu-tasks — Break a work unit into tasks

Tell the user this skill is running. Read `AGENTS.md` and [issue-rules.md](../wu-container/issue-rules.md). This skill creates the breakdown; it does not implement tasks or rewrite the parent's requirements.

## Invocation

```text
$wu-tasks <work-unit-number>
$wu-tasks help
```

Ask for the container number if absent. Never choose a recent issue or search result as an implicit parent. Help explains usage without tools or mutations.

## Workflow

- [ ] Validate the parent and read its complete specification and existing children.
- [ ] Draft tasks with AC coverage, dependencies and execution order.
- [ ] Present the concrete breakdown and resolve material gaps.
- [ ] Create, link and order only authorized tasks; verify and report.

### 1. Read the whole work unit

Require an open top-level `work-unit` in the origin-derived repository. Read its entire body: purpose, problem, in/out of scope, every HLR and AC, references and relevant comments. Inspect referenced plans and the current implementation where necessary. List all native children, open and closed, in order, and inspect their linked PRs and actual outcomes to avoid duplicating completed work.

If requirements or criteria are missing, contradictory or untestable, identify the exact gap and propose a correction through `wu-container`; do not invent criteria while decomposing. An independent issue, a task, a nested container or a legacy checklist without native relationships does not become a work unit merely because this skill receives its number.

### 2. Design the breakdown

Split by independently reviewable outcomes, AC clusters and code boundaries. Each task must fit one branch/PR and contribute to the parent's defined scope. Size tasks by their acceptance criteria. Split oversized tasks into siblings; never create grandchildren. Include integration or verification work when needed to prove criteria that cross task boundaries.

Draft each task with:

- A concise action title and a short purpose within the parent.
- `Parent: #N` and `Advances: AC1, AC2` using existing parent IDs.
- Scope, exclusions and checkable task acceptance criteria.
- Known files/contracts and implementation starting points, based on current code.
- Explicit prerequisites: existing issue numbers or draft task names until creation assigns numbers. State `None` where there are none.
- Verification commands and behavioral checks appropriate to the change, plus affected English/Spanish documentation.

Build a coverage mapping from every parent AC to one or more existing or proposed tasks, identifying already verified coverage separately from planned work. Do not equate assigning an AC to satisfying it. Resolve uncovered AC and dependency cycles before publication. Propose a complete order including existing tasks, with prerequisites first. Order is a reading aid; explicit dependencies govern execution. Identify file/API overlap and which tasks can run in parallel.

### 3. Review and publish

Show the full draft bodies, metadata, coverage and ordering, then apply the publication rules in [issue-rules.md](../wu-container/issue-rules.md). Allow the user to revise the proposed breakdown. Do not add a mandatory confirmation when current-session authorization already covers the concrete actions and scope and no material decision remains.

Create tasks sequentially in dependency order, replacing draft dependency names with returned real issue numbers before publishing dependents. Link each task to the validated parent and read back both directions. Keep existing tasks intact; adopting or editing an unparented existing issue needs explicit authority for that issue. Inherit metadata as the shared rules specify, without copying the container's `work-unit` label.

Set the agreed native list order over the complete child set and read it back. On partial failure stop new creations, report what exists and preserve the journal. On resumption inspect both linked tasks and previously created but unlinked issues, then complete only missing operations. Do not recreate the same breakdown.

### 4. Report and hand off

Report the parent URL and the actual tasks in order, with each task's URL, AC IDs and prerequisites. Distinguish created, reused, linked and pending operations. Show uncovered criteria if any remain; never call a partial breakdown complete.

Once the breakdown is complete, the next step is `$wu-coordinate-tasks <N>` for a finite batch (three tasks by default, with independent `--count` and `--concurrency` options), or `$work-issue <task-number>` for one task. Creation does not start execution or close the container.
