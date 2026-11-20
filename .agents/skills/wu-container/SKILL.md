---
name: wu-container
description: Create a work-unit container with purpose, problem, high-level requirements and acceptance criteria, or a standalone issue with no children. Use to define a new unit of work before splitting tasks.
---

# $wu-container — Define a unit of work

Tell the user this skill is running. Read `AGENTS.md` and [issue-rules.md](issue-rules.md) before interacting with GitHub. This skill defines the work; it does not implement it or create child tasks.

## Invocation

```text
$wu-container <purpose or source>
$wu-container --independent <purpose or source>
$wu-container help
```

With no purpose or source, ask what problem the work should solve. Use the conversation and supplied documents before asking for missing details. Default to a container; `--independent` creates a single issue with no parent or children. Help explains usage without tools or mutations.

Gitbox has two work modes: an independent leaf issue, or a top-level work unit with direct task sub-issues. There is no umbrella above a work unit and no task below another task. The portable `work-unit` label identifies containers; do not depend on organization-specific issue types or IDs.

## Workflow

- [ ] Read the source and search for existing work.
- [ ] Draft the complete scope, requirements and acceptance criteria.
- [ ] Check traceability and show the concrete issue draft.
- [ ] Publish only with authority for this issue; read back and report.

### 1. Understand the work

Read the supplied plan, discussion or documents and inspect relevant repository context. Search open and closed issues and recent PRs for duplicates or completed portions. Use the origin-derived repository, not a copied account or repository name.

Establish the intended user, current problem, desired outcome, boundaries and constraints. Ask only for unresolved product decisions that prevent a useful specification. Do not invent milestone, assignee, delivery dates or requirements. Existing documents may supply context; a separate PRD, feature contract or approval role is not required.

For a single independently deliverable change, offer the independent mode when appropriate. Keep a requested container as a container. If an existing issue already represents this work, show it rather than creating a duplicate; converting it to a container requires explicit scope and publication authority.

### 2. Draft the issue

Use a concise outcome title, prefixed `WU:` for a container. Write an English body with these sentence-case sections, in order:

- **Purpose:** who benefits and what outcome I intend to deliver.
- **Problem:** current behavior, evidence and the concrete gap.
- **In scope:** the behaviors and surfaces this unit commits to cover.
- **Out of scope:** explicit exclusions and relevant deferred work.
- **High-level requirements:** stable `HLR1`, `HLR2`, … identifiers, each describing an outcome understandable without implementation knowledge.
- **Acceptance criteria:** populated `AC1`, `AC2`, … entries, each citing its HLR IDs and an observable pass condition, including relevant failures and regressions.
- **References:** only sources and related issues that explain this work.

Example requirement: `HLR1: I can distinguish unavailable clones from clones that no longer exist.` Example criterion: `AC1 (HLR1): When access to a tracked clone fails, refresh retains its tracking entry and reports the access problem.`

Keep class names, file lists and implementation steps out of HLR. Criteria describe proof, not a task breakdown. Every HLR needs at least one AC; every AC must trace to an HLR. Do not leave the AC section empty, use placeholder criteria or insert generic completion checks in place of requirements. For an independent issue use the same content, scaled to the task, without the `WU:` prefix or `work-unit` label.

### 3. Review and file

Show the proposed mode, title, full body and metadata. Validate that the scope fits one coherent unit, all requirements have coverage, no material decision is hidden in an assumption, and the text contains no private local details. Do not split an oversized unit into a new hierarchy; propose narrower peer work units.

Apply [issue-rules.md](issue-rules.md) for authorization, label creation if necessary, durable drafts, creation retries and readback. An instruction to implement these skills is not authorization to file example issues. Preserve any earlier current-session authorization for the specific publication actions and scope.

Report the actual URL and mode. For a container, the next step is `$wu-tasks <N>`; for an independent issue it is `$work-issue <N>`. Do not start either automatically.
