---
name: fixing-markdown
description: Validate and fix markdown formatting in files and folders. Use when the user wants to check formatting, validate markdown, fix lint errors, review markdown format, fix markdown format.
---

# /fixing-markdown — Check and fix Markdown

Before execution, tell the user that you are using `/fixing-markdown`.

Use the shared runner for both harnesses. It selects editable Markdown once, then gives that same list to `markdownlint-cli2`, `fix_md_extra.py` and `prettier`.

## Target selection

- Accept one or more files or directories. With no target, show usage and ask for the intended scope.
- Resolve explicitly named rendered files or subtrees to their masters: `AGENTS.md` and `.claude/CLAUDE.md` map to `ai/main.md`; rendered rules, context and skills map to the matching `ai/` subtree.
- For a repository-wide request, select the masters and other editable Markdown. Skip `.claude/`, `.codex/`, `.agents/`, dependencies, build output and private continuity files. Never pass a generated file to a formatter.
- Reject targets outside the repository, symlink targets and excluded directories. The runner does not follow symlinks during traversal.
- If a requested rendered file has no master, report the mismatch and follow `.claude/rules/ai-harness.md`; do not format it in place.

Inspect the selection without writing:

```bash
uv run .claude/skills/fixing-markdown/scripts/fix_md_extra.py --list <target>
```

## Prerequisites

Use existing installations of `uv`, `markdownlint-cli2` and `prettier`. The scripts use only Python's standard library. The runner checks both formatting executables before changing files and never downloads or installs tools. If a prerequisite is missing, request installation approval instead of running `pnpm dlx`, `npx` or an installer implicitly. Respect the session's network and cache permissions when invoking `uv`.

## Workflow

1. Choose check-only mode when the user asks to inspect or validate; choose fix mode when they ask to correct formatting.
2. Run the matching command from the repository root:

   ```bash
   # Read-only inspection.
   uv run .claude/skills/fixing-markdown/scripts/format_markdown.py --check <target>

   # Apply fixes, then run read-only verification.
   uv run .claude/skills/fixing-markdown/scripts/format_markdown.py <target>
   ```

3. Review the selected files and remaining findings. A first-pass markdownlint exit code of 1 can mean issues that later tools fix; the runner continues through the structural fixes and Prettier. Execution/configuration failures stop the run. Do not report success when the final checks fail.
4. If fix mode changed anything under `ai/`, run `uv run ai/scripts/sync.py diff`, review its destinations, then `uv run ai/scripts/sync.py sync`. Commit masters and both rendered trees together.
5. Run `./scripts/health.sh markdown harness` before reporting the harness clean. This also checks generated Markdown without editing it. Respect prerequisite permissions and report any check that could not run.

The lint configuration intentionally omits `fix`. Enable repairs only with the CLI `--fix` flag; final checks omit it, and Prettier uses `--check`. Do not put `fix` in a lint configuration: nested configurations override command-line mode selection and can make a check modify files or prevent a fix pass from repairing them.

## Examples

```bash
# Check canonical docs and translations without writing.
uv run .claude/skills/fixing-markdown/scripts/format_markdown.py --check docs

# Fix the master behind the generated entry file.
uv run .claude/skills/fixing-markdown/scripts/format_markdown.py AGENTS.md

# Fix editable Markdown throughout the repository, then render changed masters.
uv run .claude/skills/fixing-markdown/scripts/format_markdown.py .
```

## Report

Report the selected scope, files changed, final check results and any prerequisite or manual fix still needed. Distinguish changes to masters from the subsequent render. For check-only requests, report findings without modifying files or running sync.
