---
read-when: Before changing how the AI harness is structured or rendered, or when Claude Code or Codex seems to miss an instruction, rule or skill.
---

# Guideline: dual AI harness

Claude Code and Codex read this repository's agent guidance from neutral masters in `ai/`. This note explains the model; `{{RULES}}/ai-harness.md` has the authoring rules.

## Model

Claude Code and Codex agree on what to do here (policy, builds, gates, workflows) but not on how they load instructions. Claude Code reads `.claude/CLAUDE.md`, loads a rule when it touches a matching file and discovers skills itself. Codex reads one flat `AGENTS.md` at the root, follows no imports, ignores text past 32 KiB and loads no rules by itself.

So I write each instruction once and let an engine compile it into both native layouts, like source and build output:

```text
ai/main.md       ->  .claude/CLAUDE.md         AGENTS.md
ai/rules/        ->  .claude/rules/            .codex/rules/
ai/context/      ->  .claude/context/          .codex/context/
ai/skills/       ->  .claude/skills/           .agents/skills/
ai/harness.yaml      render map: destinations, `only:` globs, budgets
ai/scripts/          sync.py (the engine) and its tests
```

Every rendered file is committed, so a fresh clone works in both tools on every OS without a render step or symlinks.

## Three levels of sharing

- **Shared:** most text. It reaches both tools unchanged, with tokens expanding to each tool's paths and invocation prefix.
- **Shared with divergent parts:** harness blocks inside one master. I use them for mechanism only: tool names, plan workflow, permissions, image viewing, documentation links.
- **One tool only:** an `only:` glob for a whole master, `claude:` or `codex:` frontmatter keys for metadata, and native configuration (`.claude/settings.json`, `.codex/config.toml`) that the engine never touches.

Each tool also gets the indexes it needs: both get the on-demand context table; only Codex gets the rules and skills tables, because Claude Code loads rules and lists skills on its own.

## Working from either tool

I can ask Claude Code or Codex to change the harness. Both load `ai-harness.md` when they touch `ai/` or a rendered file, both run the same `uv run ai/scripts/sync.py` commands, and `./scripts/health.sh harness` fails the PR gate when a rendered file drifts from its master. A drift message names the master to edit.

## Size budget

Codex reads at most 32 KiB of project instructions by default. The engine refuses an `AGENTS.md` above that and warns above the budget in `ai/harness.yaml`, which leaves room for a personal `~/.codex/AGENTS.md`. When `ai/main.md` grows, I move task-specific detail into a context file with a `read-when:` line instead of raising the cap.

I keep skill descriptions concise and free of embedded quotation marks so discovery clients show them whole. Markdown links in skills resolve from the skill directory, which sits at the same depth in `ai/skills/`, `.claude/skills/` and `.agents/skills/`.

## Fresh-session verification

After changing the harness, I run the sync, then start a fresh session at the repository root in each tool and ask:

```text
Summarize this repository's instructions and list its local skills.
Explain the publication gate, the GUI build rule, and where to edit a skill.
Do not modify files or run the skills.
```

I expect every skill under `ai/skills/` with a complete description, approval before publishing, a Wails build after Go changes, and `ai/skills/` as the place to edit.

## Troubleshooting

- **`verify` reports drift I did not cause:** someone edited a rendered file. Port the edit into the master it names, then sync.
- **A tool still shows old instructions:** start a fresh session; both read their entry file once per session.
- **`AGENTS.md` or `.agents/skills` is a symlink:** an old checkout. Run the sync; it replaces them with real files.

## References

<!-- harness:claude -->
- [Claude Code memory and CLAUDE.md](https://code.claude.com/docs/en/memory)
- [Claude Code skills](https://code.claude.com/docs/en/skills)
<!-- /harness -->
<!-- harness:codex -->
- [Codex instruction discovery and its size limit](https://developers.openai.com/codex/guides/agents-md)
- [Codex skills](https://developers.openai.com/codex/skills)
<!-- /harness -->
