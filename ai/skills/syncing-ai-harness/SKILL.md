---
name: syncing-ai-harness
description: Render the Claude Code and Codex harnesses from the neutral masters in ai/ and check them for drift. Use after editing anything under ai/, to change a rule, context file, skill or the entry instructions for either AI tool, or to check that the generated files match their masters.
---

# {{INVOKE}}syncing-ai-harness — Render both harnesses from ai/

**IMPORTANT:** Before starting, tell the user: "I'm executing `{{INVOKE}}syncing-ai-harness`".

`ai/` is the only hand-edited source. The engine renders it into each tool's native layout:

```text
ai/main.md       ->  .claude/CLAUDE.md          and  AGENTS.md
ai/rules/        ->  .claude/rules/             and  .codex/rules/
ai/context/      ->  .claude/context/           and  .codex/context/
ai/skills/       ->  .claude/skills/            and  .agents/skills/
```

**Never hand-edit a rendered file.** Edit its master under `ai/`; the next sync overwrites any direct edit. Native tool configuration (`.claude/settings.json`, `.codex/config.toml`) is not rendered and stays hand-edited.

## Commands

Requires `uv` (zero-footprint: PEP 723 inline metadata, PyYAML fetched into uv's cache). Run from the repo root, in either tool:

```bash
uv run ai/scripts/sync.py list     # what each harness receives
uv run ai/scripts/sync.py diff     # what sync would change (read-only)
uv run ai/scripts/sync.py sync     # render both harnesses, then verify
uv run ai/scripts/sync.py verify   # read-only; exit 1 on any problem
uv run ai/scripts/test_sync.py     # the engine's own tests
```

`./scripts/health.sh harness` runs the renderer and safe-formatting regression suites, then `verify --quiet`; the PR gate runs it too. Dependency downloads and cache access follow the current session permissions. Do not install prerequisites or bypass a restriction without the required authorization.

## Routine

1. Edit the master under `ai/`, following `{{RULES}}/ai-harness.md`.
2. Run `diff` and show the user the list.
3. Run `sync`. It writes nothing when a check fails, and verifies itself after writing.
4. Inspect both rendered diffs. Keep common policy consistent and verify that tool-only additions leave the other tree unchanged. Run `./scripts/health.sh harness markdown shellcheck actionlint`.
5. After changes to discovery or instructions, verify them in a fresh session of each tool; report any tool not yet checked. See `{{CONTEXT}}/guideline_harness.md` for the diagnostic prompt.
6. Commit the masters and both rendered trees together; follow the publication gate in `{{ENTRY}}`.

## When verify reports drift

The message names the master behind each drifted file. If the drift is a hand edit worth keeping, port it into that master first, then run `sync`. Never fix a rendered file in place.

## Notes

- Engine and render map: `ai/scripts/sync.py`, `ai/harness.yaml`. Design notes: `{{CONTEXT}}/guideline_harness.md`.
- `sync` deletes rendered files whose master is gone, inside the rendered rules, context and skills folders of both tools.
