---
name: wish
description: Manage the gitbox feature radar — view priorities, capture new ideas, plan implementations, delete entries, and mark features shipped. Use when the user wants to see the backlog, list pending wishes, add a feature idea, plan a feature, delete a wish, or mark one done.
---

# /wish — Feature radar manager

**IMPORTANT:** Before starting, inform the user: "I'm executing `/wish`"

## Usage

```text
/wish                  Show the radar overview
/wish list             One-line summary of all pending wishes
/wish add <title>      Capture a new feature idea
/wish plan <id>        Deep-dive and create implementation plan
/wish done <id>        Mark a wish as shipped
/wish delete <id>      Remove a wish from the radar entirely
```

## Data file

The single source of truth is `.claude/context/feature-radar.md`. Always re-read it at execution time — it may have been edited manually.

## `/wish` (no arguments) — Radar overview

1. Read `.claude/context/feature-radar.md`
2. Parse all wishes from both `## Radar` and `## Shipped` sections
3. Display a summary table grouped by priority:

```text
Feature Radar — gitbox
======================

P1 (next up):
  W6  Open in browser              radar   S
  W2  Bulk branch cleanup          radar   M

P2 (important):
  W1  Config sync                  radar   L
  W3  Dynamic workspaces           radar   M

P3 (nice to have):
  W4  Smart archiving              radar   L
  W5  Unified dashboard            radar   L

Shipped: (none)
```

4. End with a concrete suggestion: which wish to tackle next, based on priority, status, and codebase readiness

## `/wish list` — Quick pending summary

A compact, flat list of all non-shipped wishes — no grouping, no suggestion, just the facts.

1. Read `.claude/context/feature-radar.md`
2. Collect every wish under `## Radar` (skip `## Shipped`)
3. Print one line per wish, sorted by priority then ID:

```text
Pending wishes:
  W2  P1  M  radar     Bulk branch cleanup
  W1  P2  L  radar     Config sync
  W3  P2  M  radar     Dynamic workspaces
  W4  P3  L  radar     Smart archiving
  W5  P3  L  radar     Unified dashboard
```

4. End with the total count (e.g., "5 pending")

## `/wish add <title>` — Capture a new idea

1. Read the radar file to determine the next available ID (`W<max+1>`)
2. Ask the user 3-5 clarifying questions (one round):
   - What problem does this solve? (one sentence)
   - Who benefits — you on desktop, you on server, or both?
   - Rough scope — does it touch CLI only, TUI+CLI, or all three modes?
   - Any existing gitbox features it depends on or extends?
   - Urgency — is this blocking something or a quality-of-life improvement?
3. Based on answers, draft the wish entry:
   - Assign priority (P1/P2/P3) and size (S/M/L) with brief justification
   - Write the one-paragraph concept
   - Explore the codebase (2-3 targeted searches) to populate the Notes field with relevant files, existing patterns, and reuse opportunities
4. Show the draft to the user for approval
5. On approval, append the new wish to `## Radar` in `.claude/context/feature-radar.md`, sorted by priority (P1 first, then P2, then P3)

## `/wish plan <id>` — Deep-dive into implementation

This is the most valuable subcommand — it turns a vague wish into a concrete implementation plan.

1. Read the radar file and find the wish by ID (e.g., `W6`)
2. If status is already `shipped`, inform the user and stop
3. **Codebase exploration phase** (use Explore subagents where possible):
   - Read the wish's existing Notes for leads
   - Search for related code patterns (similar features, relevant `pkg/` functions, existing tests)
   - Identify all files that would need changes
   - Check for existing test infrastructure that covers the area
   - Look at how similar features were implemented
4. **Design phase** — produce a structured plan:
   - **Approach:** 2-3 sentences on the overall strategy
   - **Changes:** ordered list of files to create/modify, with what changes in each
   - **New code:** any new functions, types, or packages needed
   - **Reuse:** existing functions and patterns to reuse (with file paths)
   - **Test plan:** which test files to update, what test cases to add
   - **Docs:** which docs need updating (per the CLAUDE.md stakeholder table)
   - **Risks:** anything tricky or uncertain
   - **Estimate:** number of commits, rough session count
5. Update the wish's status to `planning` and enrich the Notes with the plan summary
6. Enter plan mode with the full implementation plan

## `/wish done <id>` — Mark as shipped

1. Read the radar file and find the wish by ID
2. If status is already `shipped`, inform the user and stop
3. Update the wish:
   - Set status to `shipped`
   - Add a shipped date: `- **Shipped:** YYYY-MM-DD`
4. Move the wish entry from `## Radar` to `## Shipped`
5. Confirm to the user

## `/wish delete <id>` — Remove a wish

1. Read the radar file and find the wish by ID (e.g., `W3`)
2. If the wish does not exist, inform the user and stop
3. Show the wish title, status, and priority — ask the user to confirm deletion
4. On confirmation, remove the entire wish entry (the `### W<id>: ...` heading and all lines until the next heading or end of section) from whichever section it lives in (`## Radar` or `## Shipped`)
5. Confirm to the user: "Deleted W<id>: <title>"

**Note:** Deleted IDs are never reused — the next `/wish add` still uses `max+1` across all existing and deleted IDs.

## Behavior notes

- Always re-read the radar file before any operation
- When updating the file, preserve the exact markdown format — do not reformat other entries
- IDs are never reused — if W3 is shipped, the next new wish is still W7 (or whatever max+1 is)
- `/wish plan` flows naturally into plan mode — after producing the plan, the user can start implementing
- Keep the overview output concise — this is a solo developer's dashboard, not a sprint board
- Follow the project's documentation style: first person singular, sentence-case headings, hyphens for lists, active voice
