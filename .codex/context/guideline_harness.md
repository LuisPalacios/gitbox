---
read-when: Before changing how the AI harness is structured or rendered, or when Claude Code or Codex seems to miss an instruction, rule or skill.
---

# Guideline: dual AI harness

Claude Code and Codex read this repository's agent guidance from neutral masters in `ai/`. This note explains the model; `.codex/rules/ai-harness.md` has the authoring rules.

## Model

Claude Code and Codex share policy, build requirements, approval gates and workflows. Claude Code reads `.claude/CLAUDE.md`, loads path-scoped rules and discovers `.claude/skills/`. Codex discovers `.agents/skills/` natively and loads applicable `AGENTS.md` instructions from global and project scopes, with `AGENTS.override.md` taking precedence at each scope. Its default project-instruction budget is 32 KiB, configurable with `project_doc_max_bytes`. This repository keeps its entry instructions at the root and routes task-specific reading through the context and Markdown-rule indexes; Codex does not automatically load these Markdown rules.

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

Both tools get the on-demand context table. Codex also gets the Markdown-rule routing table and a compact skills index. Its native skill catalog supplies names, descriptions and paths; the index remains useful as a repository overview, not as a discovery mechanism.

## Working from either tool

I can ask Claude Code or Codex to change the harness. Both follow `ai-harness.md` before edits (Codex reads it explicitly through the index), both run the same `uv run ai/scripts/sync.py` commands, and `./scripts/health.sh harness` fails the PR gate when a rendered file drifts from its master. A drift message names the master to edit.

I preserve the other tool's behavior when changing a shared master. I inspect both rendered diffs, including exclusions and approval gates. For a tool-only addition, I verify that the other tree remains byte-for-byte unchanged. I use the existing tokens, blocks and `only:` filters before considering an engine extension.

Optional Codex UI metadata lives in `ai/skills/<name>/agents/openai.yaml`. The `only: codex:` glob reserves these sidecars for Codex, and the engine copies them byte for byte. I keep shared `name` and `description` in `SKILL.md`; sidecar fields do not belong in the `codex:` frontmatter mapping. Natural-language skill selection stays enabled unless I deliberately request an explicit-only workflow.

Native configuration does not replace publication approval. I leave personal profiles, credentials, trust decisions and sandbox permissions outside shared masters. A project `.codex/config.toml`, if needed, stays hand-edited and must respect the session's enforced permissions; I do not add one merely to repeat defaults.

## Size budget

Codex reads at most 32 KiB of project instructions by default. The engine refuses an `AGENTS.md` above that and warns above the budget in `ai/harness.yaml`, which leaves room for a personal `~/.codex/AGENTS.md`. When `ai/main.md` grows, I move task-specific detail into a context file with a `read-when:` line instead of raising the cap.

I front-load each skill description with its purpose and trigger. Codex can shorten descriptions or omit entries when its skill-catalog budget is exhausted; concise text helps, but does not guarantee complete discovery. I check the actual catalog rather than inferring it from YAML formatting. Markdown links in skills resolve from the skill directory, which sits at the same depth in `ai/skills/`, `.claude/skills/` and `.agents/skills/`.

## Fresh-session verification

After changing the harness, I run the sync, then start a fresh session at the repository root in each tool and ask:

```text
Summarize this repository's instructions and list its local skills.
Explain the publication gate, the GUI build rule, and where to edit a skill.
Explain how to format a rendered file safely and where build commands live.
Do not modify files or run the skills.
```

I expect the skills intended for that tool, complete descriptions when the catalog budget permits, explicit authorization for each publication action, a Wails build after Go changes, `ai/skills/` as the place to edit, and `.codex/context/building.md` for build commands. Formatter requests must select masters and regenerate both trees. I do not execute publication or formatting workflows merely to test discovery.

For local diagnostics, `codex --version` identifies the installed CLI and `codex debug prompt-input` shows its model-visible startup input without running a model turn. I compare the included `AGENTS.md` text and skill descriptions with disk. This diagnostic can contain private instructions, so I keep its raw output local. A fresh session then checks whether the agent actually follows the instructions.

## Troubleshooting

- **`verify` reports drift I did not cause:** someone edited a rendered file. Port the edit into the master it names, then sync.
- **A tool still shows old instructions:** start a fresh session; both read their entry file once per session.
- **`AGENTS.md` or `.agents/skills` is a symlink:** an old checkout. Run the sync; it replaces them with real files.

## References

- [Codex instruction discovery and its size limit](https://developers.openai.com/codex/guides/agents-md)
- [Codex skills](https://developers.openai.com/codex/skills)
