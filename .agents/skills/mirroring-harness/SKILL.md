---
name: mirroring-harness
description: Mirror the .claude/ harness into Codex and check it for drift. Regenerates AGENTS.md, .codex/rules, .codex/context and .agents/skills from .claude/, plus the generated index blocks in CLAUDE.md. Use after editing CLAUDE.md, a rule, a context file or a skill, or to check whether the mirrors drifted.
---

# /mirroring-harness — Mirror .claude into Codex

**IMPORTANT:** Before starting, tell the user: "I'm executing `/mirroring-harness`".

`.claude/` is the only hand-edited source. Everything on the Codex side is generated:

```text
.claude/CLAUDE.md   ->  AGENTS.md          (and the index blocks inside CLAUDE.md itself)
.claude/rules/      ->  .codex/rules/      .claude/context/  ->  .codex/context/
.claude/skills/     ->  .agents/skills/
```

**Never hand-edit `AGENTS.md`, `.codex/` or `.agents/skills/`**, or the blocks between `<!-- index:… -->` markers in `CLAUDE.md`. The next sync overwrites them.

## Commands

Requires `uv` (zero-footprint: PEP 723 inline metadata, PyYAML fetched into uv's cache). Run from the repo root:

```bash
uv run .claude/skills/mirroring-harness/scripts/sync.py list     # inventory of the source tree
uv run .claude/skills/mirroring-harness/scripts/sync.py diff     # what sync would change (read-only)
uv run .claude/skills/mirroring-harness/scripts/sync.py sync     # write the mirrors, then verify
uv run .claude/skills/mirroring-harness/scripts/sync.py verify   # read-only; exit 1 on any problem
uv run .claude/skills/mirroring-harness/scripts/test_sync.py     # the script's own tests
```

`./scripts/health.sh harness` runs `verify --quiet` and the tests; the PR gate runs it too.

## Routine

1. Edit the source under `.claude/`.
2. Run `diff` and show the user the list.
3. Run `sync`. It writes nothing when a check fails, and verifies itself after writing.
4. Commit the source and the mirrors together: a fresh clone must work in Codex without a sync.

## What sync does

- **Indexes:** the rules, context and skills tables in `CLAUDE.md` come from each file's frontmatter: a rule's `paths:`, a context file's `read-when:`, a skill's `name` and the first sentence of its `description`. Every context file needs a `read-when:` sentence.
- **Entry file:** `AGENTS.md` gets its own title and a generated banner. Its tables point at the `.codex/` copies, its rules table reads "before editing X, read Y" because Codex never loads rules by itself, and skills read `$name`.
- **Copies:** byte for byte, keeping file modes. A path written inside a rule, context file or skill keeps naming the `.claude/` source, the only place to edit.
- **Orphans:** a mirrored file whose source is gone is deleted.
- **Fails** when: `AGENTS.md` is over 32 KiB; a context file lacks `read-when:`; a rule lacks `paths:`; a skill's `name` differs from its folder or its description is missing or over 1024 characters; the source holds a symlink; a relative link in a generated Markdown file misses or leaves the repo.
- **Warns** when: `AGENTS.md` or a `SKILL.md` is over the budgets in `.claude/harness.yaml`.

## Notes

- If `verify` reports drift you did not cause, someone edited a mirror by hand: run `sync` to restore it and move the change into `.claude/`.
- Settings: `.claude/harness.yaml`. Design notes: `.claude/context/guideline_codex.md`.
