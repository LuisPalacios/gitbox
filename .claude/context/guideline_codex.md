---
read-when: Before changing how Codex sees this repo's instructions, rules, context or skills, or when Codex seems to miss them.
---

# Guideline: dual Claude/Codex setup

This document explains how Claude Code and Codex share one set of agent guidance in this repository.

## Intent

- `.claude/` is the only hand-edited source: `CLAUDE.md`, `rules/`, `context/` and `skills/`.
- Claude Code reads `.claude/` directly and loads each rule when it reads a matching file.
- Codex reads generated real files: `AGENTS.md` at the root, `.codex/rules/`, `.codex/context/` and `.agents/skills/`. The `mirroring-harness` skill renders them from `.claude/`.
- Real files, not symlinks: a fresh clone works in Codex on every OS without Developer Mode, `mklink` or `tar -h` workarounds.

## Layout

```text
.claude/CLAUDE.md      ->  AGENTS.md          (plus the index blocks inside CLAUDE.md itself)
.claude/rules/         ->  .codex/rules/
.claude/context/       ->  .codex/context/
.claude/skills/        ->  .agents/skills/
.claude/harness.yaml       mirror map and size budgets
```

`AGENTS.md` must stay at the repository root: that is where Codex discovers project instructions. `.agents/skills/` is Codex's native repository skill folder.

## Capability parity

I keep project policy, build rules, verification requirements, and publication gates in `.claude/CLAUDE.md`. Both harnesses use the same skill bodies and scripts. I use `/work-issue 123` in Claude Code and `$work-issue 123` in Codex; the arguments and approval gates match.

Codex has no rule loader, so the generated `AGENTS.md` carries a table that names the rule to read before editing a matching file. The context table works the same way in both harnesses: each context file declares its `read-when:` sentence in frontmatter, and the table routes an agent there before that task.

I use the available tools for planning, checklists, file reads, searches, images, and delegation. When Codex cannot switch into plan mode itself, I write the plan in the conversation and preserve the invoked skill's approval gates.

I keep `.claude/settings.json` as Claude-only configuration. Codex permissions come from its own session; I do not copy Claude allowlists into a permissive Codex policy.

## Size budget

Codex reads at most 32 KiB of project instructions by default. The sync refuses an `AGENTS.md` above that and warns above the budget in `.claude/harness.yaml`, which leaves room for a personal `~/.codex/AGENTS.md`. When `CLAUDE.md` grows, I move task-specific detail into a context file with a `read-when:` line instead of raising the cap.

I keep shared skill metadata concise and avoid embedded quotation marks so discovery clients expose complete descriptions. Markdown links in skill files resolve from the skill directory, which sits at the same depth in `.claude/skills/` and `.agents/skills/`.

## Fresh-session verification

After changing instructions or skill metadata, I run the sync, then start a fresh Codex session at the repository root and ask:

```text
Summarize this repository's instructions and list its local skills.
Explain the publication gate, the GUI build rule, and which rule to read before editing a SKILL.md.
Do not modify files or run the skills.
```

I expect every skill under `.claude/skills/` with a complete description, approval before publishing, a Wails build after Go changes, and `.codex/rules/skills-authoring.md` for skill edits. I repeat the check in Claude Code to confirm the shared guidance.

## Troubleshooting

- **`verify` reports drift I did not cause:** someone edited a mirror by hand. Run the sync to restore it, and move the intended change into `.claude/`.
- **`AGENTS.md` or `.agents/skills` is a symlink:** an old checkout. Run the sync; it replaces the symlinks with real files.
- **Codex still shows old instructions:** restart the Codex extension or start a fresh session; it reads `AGENTS.md` once per session.

## Official Codex references

- [Instruction discovery and its default size limit](https://developers.openai.com/codex/guides/agents-md)
- [Skill discovery and invocation](https://developers.openai.com/codex/skills)
