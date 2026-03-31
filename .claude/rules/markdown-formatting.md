---
paths:
  - "**/*.md"
  - "doc/**"
---

# Markdown Standards

Adhere to `davidanson.vscode-markdownlint` rules when generating or editing Markdown.

## Structure

- **MD001:** Headings increment by one (`#` → `##` → `###`). No skipping.
- **MD002/MD041:** First line is H1 or frontmatter. No leading blank lines.
- **MD003:** ATX style only (`## Heading`). No Setext (underlines).
- **MD025:** One H1 per file (the title).
- **MD036:** Add colon (`:`) when creating emphasis instead of a heading. Example: **Emphasis line**:
- **MD060:** Table pipes must have space after and before the pipe character (compact style)

## Spacing

- **MD009:** No trailing whitespace.
- **MD010:** Spaces only (2-space indent). No hard tabs.
- **MD012:** Max one consecutive blank line.
- **MD022:** Blank line before and after headings.
- **MD031:** Blank line before and after fenced code blocks.
- **MD032:** Blank line before and after lists.
- **MD047:** File ends with single newline.

## Lists & Code

- **MD004:** Dashes (`-`) for unordered lists. Consistent throughout.
- **MD024:** No duplicate heading text in same section.
- **MD040:** Fenced code blocks MUST have language tag (` ```ts `, ` ```bash `).
- **MD046:** Fenced style only (` ``` `). Never indented code blocks.

## Relaxed

- **MD013:** Line length — IGNORE. Let IDE soft-wrap.
- **MD033:** Inline HTML — Avoid, but OK for `<br>` in tables or `<details>`.

## Pre-Output Checklist

Before finalizing any Markdown output, verify:

1. All code blocks have language identifiers?
2. No double blank lines anywhere?
3. H1 is first non-frontmatter line?
4. File ends with exactly one newline?
