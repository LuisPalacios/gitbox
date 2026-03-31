---
name: fixing-markdown
description: Validate and fix markdown formatting in files and folders. Use when the user wants to check formatting, validate markdown, fix lint errors, review markdown format, fix markdown format.
---

# /fixing-markdown — Validate and Fix Markdown

**IMPORTANT:** Before starting execution, inform the user: "I'm executing `/fixing-markdown`"

Run `markdownlint-cli2` + `fix_md_extra.py` + `prettier` to auto-fix markdown formatting issues.

## CRITICAL: Execution Requirements

**ALWAYS run ALL tools in sequence, regardless of exit codes.**

1. **markdownlint-cli2** may exit with code 1 if it finds errors it cannot auto-fix. This is expected behavior—**proceed anyway**.
2. **fix_md_extra.py** fixes issues markdownlint cannot auto-fix (MD040, MD025).
3. **prettier** must ALWAYS run after the previous steps.
4. After all tools complete, verify with a final markdownlint check.

**Correct flow:**

```text
markdownlint-cli2 --fix → fix_md_extra.py → prettier --write → markdownlint-cli2 (verify)
```

**Wrong flow (DO NOT DO THIS):**

```text
markdownlint-cli2 --fix → (sees exit code 1) → STOP
```

## Usage

```text
/fixing-markdown <target>
```

**Arguments:**

- `target` (Required):
  - **File path**: Single file (e.g., `src/content/posts/example.md`)
  - **Folder path**: All .md files recursively (e.g., `src/content/posts`)

**No argument = show this usage.**

## Environment Setup

This skill uses **zero-footprint dependency management**:

- **JavaScript CLI tools** (`markdownlint-cli2`, `prettier`): Executed via `pnpm dlx` — no `package.json` or `node_modules` needed
- **Python scripts**: Executed via `uv run` with PEP 723 inline metadata — no `requirements.txt` or `.venv` needed

**Prerequisites** (must be installed on the system):

- `pnpm` — [https://pnpm.io/](https://pnpm.io/)
- `uv` — [https://docs.astral.sh/uv/](https://docs.astral.sh/uv/)

No per-project setup required. Dependencies are cached globally and resolved on first run.

## Commands

Replace `<plugin-path>` with the actual path to this plugin's installation directory.

### Single File

```bash
# Step 1: Fix structural issues (ALWAYS run, ignore exit code)
pnpm dlx markdownlint-cli2 --config .claude/skills/fixing-markdown/.markdownlint-cli2.jsonc --fix "path/to/file.md"

# Step 2: Fix issues markdownlint cannot auto-fix (MD040, MD025)
uv run .claude/skills/fixing-markdown/scripts/fix_md_extra.py "path/to/file.md"

# Step 3: Format (ALWAYS run, even if previous steps had errors)
pnpm dlx prettier --config .claude/skills/fixing-markdown/.prettierrc --write "path/to/file.md"

# Step 4: Check remaining errors (if any, fix manually)
pnpm dlx markdownlint-cli2 --config .claude/skills/fixing-markdown/.markdownlint-cli2.jsonc "path/to/file.md"
```

### Folder (recursive)

```bash
# Step 1: Fix structural issues (exclude .agent/, ignore exit code)
pnpm dlx markdownlint-cli2 --config .claude/skills/fixing-markdown/.markdownlint-cli2.jsonc --fix "path/to/folder/**/*.md" "#.agent"

# Step 2: Fix issues markdownlint cannot auto-fix (MD040, MD025)
uv run .claude/skills/fixing-markdown/scripts/fix_md_extra.py "path/to/folder"

# Step 3: Format (exclude .agent/, ALWAYS run)
pnpm dlx prettier --config .claude/skills/fixing-markdown/.prettierrc --write "path/to/folder/**/*.md" --ignore-pattern ".agent/**"

# Step 4: Check remaining errors
pnpm dlx markdownlint-cli2 --config .claude/skills/fixing-markdown/.markdownlint-cli2.jsonc "path/to/folder/**/*.md" "#.agent"
```

## Examples

```text
/fixing-markdown src/content/posts/example.md
-> Fixes and formats specific file

/fixing-markdown src/content/posts
-> Fixes and formats all .md files in posts/ recursively

/fixing-markdown docs
-> Fixes and formats all .md files in docs/ recursively
```

## Output Format

### Clean File

```text
Fixing: src/content/posts/example.md

markdownlint: 0 errors
fix_md_extra: no changes
prettier: formatted

Done
```

### With Issues Fixed

```text
Fixing: src/content/posts/example.md

markdownlint: 2 errors fixed
fix_md_extra: added language to 1 code block
prettier: formatted

Done
```

## Tools

| Tool              | Purpose                                                      |
| ----------------- | ------------------------------------------------------------ |
| markdownlint-cli2 | Structural fixes (headings, lists, code blocks, blank lines) |
| fix_md_extra.py   | Fixes MD040 (code block language) and MD025 (multiple H1)    |
| prettier          | Visual formatting (table alignment, consistent spacing)      |

## Rules Enforced

### markdownlint-cli2 (`.markdownlint-cli2.jsonc`)

| Rule  | Description                     | Auto-fixable     |
| ----- | ------------------------------- | ---------------- |
| MD001 | Heading levels increment by one | No               |
| MD003 | ATX style headings (`##`)       | Yes              |
| MD004 | Dash (`-`) for unordered lists  | Yes              |
| MD009 | No trailing whitespace          | Yes              |
| MD010 | No hard tabs                    | Yes              |
| MD012 | Max 1 consecutive blank line    | Yes              |
| MD022 | Blank lines around headings     | Yes              |
| MD025 | Single H1 heading               | Yes (via script) |
| MD031 | Blank lines around code blocks  | Yes              |
| MD032 | Blank lines around lists        | Yes              |
| MD040 | Code blocks have language       | Yes (via script) |
| MD047 | File ends with newline          | Yes              |

### fix_md_extra.py (`scripts/`)

- MD040: Adds `text` as default language to code blocks without one
- MD025: Demotes duplicate H1 headings to H2

### prettier (`.prettierrc`)

- Table column alignment
- Consistent spacing
- Prose wrap preserved (no line breaking)

## Behavior

1. **Check argument**: If no target provided, show usage and exit
2. **Detect target type**: file or folder
3. **Run markdownlint-cli2 with --fix**: Fix structural issues. **IGNORE exit code—proceed regardless.**
4. **Run fix_md_extra.py**: Fix MD040 and MD025 issues that markdownlint cannot auto-fix.
5. **Run prettier**: Format visual appearance. **ALWAYS run this step.**
6. **Run markdownlint-cli2 without --fix**: Verify no remaining errors
7. **Report**: Show results from all tools

## Notes

- Config files: `.markdownlint-cli2.jsonc`, `.prettierrc` (in skill folder)
- Requires: `pnpm` and `uv` installed on system
- **markdownlint exit code 1 is normal** when there are unfixable errors—do not stop execution
- No `node_modules` or `.venv` created—dependencies cached globally by pnpm/uv
