---
paths:
  - "ai/**"
  - ".claude/CLAUDE.md"
  - ".claude/rules/**"
  - ".claude/context/**"
  - ".claude/skills/**"
  - "AGENTS.md"
  - ".codex/rules/**"
  - ".codex/context/**"
  - ".agents/skills/**"
---

# AI harness authoring rules

`ai/` is the only hand-edited source of agent guidance. `ai/scripts/sync.py` renders every master into both the Claude Code tree (`.claude/CLAUDE.md`, `.claude/rules/`, `.claude/context/`, `.claude/skills/`) and the Codex tree (`AGENTS.md`, `.codex/rules/`, `.codex/context/`, `.agents/skills/`).

- **Rendered files are read-only.** To change one, edit its master: `ai/main.md` for the entry files, `ai/<rules|context|skills>/<same path>` for the rest. Then run `uv run ai/scripts/sync.py sync` and commit the master with both trees.
- **Write once, shared by default.** Text outside a harness block reaches both tools. Keep policy, build rules, gates and workflows shared; diverge only on mechanism (tool names, plan workflow, permissions, invocation).
- **Harness blocks** hold the parts that differ. Put each marker alone on its own line, never nested:

  ```text
  <!-- harness:claude -->
  Claude Code only.
  <!-- /harness -->
  <!-- harness:codex -->
  Codex only.
  <!-- /harness -->
  ```

- **Tokens** expand per harness; use them instead of hard-coding one tool's paths: `{{!HARNESS}}` (tool name), `{{!ENTRY}}` (entry file), `{{!RULES}}`, `{{!CONTEXT}}`, `{{!SKILLS}}` (rendered folders), `{{!INVOKE}}` (`/` or `$`, for example `{{!INVOKE}}work-issue`). Write `{{!!NAME}}` to show a token literally. Only Markdown files expand tokens and blocks; scripts and configs are copied byte for byte.
- **Whole files for one tool:** add an `only:` glob in `ai/harness.yaml` (relative to `ai/`). **Frontmatter for one tool:** nest the keys under `claude:` or `codex:`; the renderer merges them into that tool's copy only.
- **Codex skill sidecars:** keep optional UI metadata in `ai/skills/<name>/agents/openai.yaml` and reserve `skills/*/agents/openai.yaml` for Codex with `only:`. The engine copies YAML literally; do not put harness tokens in it or move sidecar fields into the `codex:` frontmatter.
- **Native tool configuration is not rendered:** `.claude/settings.json` and `.codex/config.toml` stay hand-edited in place.
- **Indexes are generated** from frontmatter: a rule needs `paths:`, a context file needs a one-sentence `read-when:`, a skill needs `name` (its folder) and `description`. `ai/main.md` places the index markers; Codex reads the Markdown rules through its rules table. It discovers `.agents/skills/` natively; the skills table provides a compact repository index.
- **Mind the budget.** `AGENTS.md` must stay under 32 KiB. Move task-specific detail into a context file with `read-when:` rather than growing `ai/main.md`.
