# Guideline: dual Claude/Codex setup

This document explains how this repository is wired so Claude Code and Codex can coexist while sharing one canonical instructions file and one shared skills directory.

## Intent

- Human source of truth for agent guidance lives at `.claude/CLAUDE.md`.
- Claude Code reads that file directly.
- Codex reads `AGENTS.md` from the repository root, so the repo exposes a symlink instead of duplicating the file.
- Repo-scoped skills remain under `.claude/skills`, and Codex sees them through a symlink at `.agents/skills`.
- The design goal is zero duplication and low-maintenance cross-platform compatibility.

## Expected repository state

These paths should exist:

```text
.claude/CLAUDE.md
.claude/skills/
AGENTS.md -> .claude/CLAUDE.md
.agents/skills -> ../.claude/skills
```

Notes:

- `AGENTS.md` must stay at the repository root because Codex instruction discovery checks root `AGENTS.md`; `.agents/AGENTS.md` is not the default project instruction location.
- Symlink targets should be relative, not absolute.
- The canonical content is `.claude/CLAUDE.md`; `AGENTS.md` is only the Codex-facing bridge.

## Quick check

Before repairing anything, verify whether repair is actually needed.

Linux, macOS, or WSL:

```bash
ls -l AGENTS.md .agents/skills
```

Expected result:

- `AGENTS.md` points to `.claude/CLAUDE.md`
- `.agents/skills` points to `../.claude/skills`

Git check:

```bash
git ls-files --stage -- AGENTS.md .agents/skills
```

Expected result after staging:

- symlinks appear with mode `120000`

If the links already match the expected targets, stop there. Do not recreate them unnecessarily.

## Repair procedure for AI agents

Use this procedure only if the wiring is missing, broken, or explicitly requested by the user.

1. Confirm the canonical files still exist:
   - `.claude/CLAUDE.md`
   - `.claude/skills/`
2. Check the current state of:
   - `AGENTS.md`
   - `.agents/skills`
3. If the expected link already exists and is correct, leave it unchanged.
4. If a wrong plain file or wrong symlink exists, pause and assess whether replacing it could overwrite user work.
5. Recreate the expected links using relative targets:
   - `AGENTS.md` -> `.claude/CLAUDE.md`
   - `.agents/skills` -> `../.claude/skills`
6. Re-verify the links after creation.
7. If Git tracking matters, ensure the symlinks are staged as symlinks, not copied file contents.
8. Remind the user to restart Codex or start a fresh Codex session so it reloads `AGENTS.md`.

Agent behavior rules:

- Do not move the canonical instructions away from `.claude/CLAUDE.md` unless the user explicitly asks.
- Do not move `AGENTS.md` into `.agents/`; that is not the default Codex project-discovery path.
- Do not duplicate the text of `CLAUDE.md` into a separate `AGENTS.md` file unless symlinks are impossible and the user explicitly accepts duplication.
- Prefer the smallest possible repair.

## How to create the links

### Linux / macOS / WSL

From the repository root:

```bash
ln -s .claude/CLAUDE.md AGENTS.md
mkdir -p .agents
ln -s ../.claude/skills .agents/skills
```

If an incorrect existing path must be replaced, inspect it first and only then remove or replace it carefully.

### Windows CMD

From the repository root:

```cmd
mklink AGENTS.md .claude\CLAUDE.md
mkdir .agents
mklink /D .agents\skills ..\.claude\skills
```

Notes for Windows:

- Developer Mode is strongly recommended so symlink creation works without extra friction.
- `mklink` must be run from `cmd.exe`, not regular PowerShell syntax.
- Git stores symlinks well, but the operating system creates them. In practice, create the link with `ln -s` or `mklink`, then commit it with Git.

## Cross-platform guidance for humans

Recommended practice:

1. Keep editing `.claude/CLAUDE.md` and `.claude/skills/` as the real files.
2. Keep `AGENTS.md` and `.agents/skills` as thin symlink bridges for Codex.
3. Commit the symlinks to Git so macOS/Linux clones preserve them naturally.
4. On Windows, enable Developer Mode and ensure Git is configured to honor symlinks when possible.
5. After changing `AGENTS.md` or its target, restart Codex or open a fresh session.

Why this setup exists:

- It avoids maintaining duplicate instruction files.
- It lets Claude Code and Codex share the same project guidance.
- It keeps Claude-specific layout preferences under `.claude/` while still satisfying Codex's root `AGENTS.md` discovery behavior.

## Troubleshooting

### Problem: `AGENTS.md` exists but is a normal text file

This usually means symlinks were not created correctly on the current machine, or Git checked out a symlink as a plain file.

Fix:

- Compare its contents and target intent.
- Replace it with a proper symlink if safe to do so.

### Problem: `.agents/skills` cannot be created on Windows

Common causes:

- Developer Mode disabled
- insufficient privileges
- wrong shell for `mklink`

Fix:

- use `cmd.exe`
- enable Developer Mode
- retry `mklink /D`

### Problem: Codex still does not see the updated instructions

Fix:

- restart the Codex extension or launch a fresh Codex session
- verify `AGENTS.md` exists at the repository root
- verify the target resolves to `.claude/CLAUDE.md`

### Problem: someone proposes `.agents/AGENTS.md`

Do not assume Codex will load that automatically. The supported default is root `AGENTS.md`, unless each machine is configured with a custom fallback filename strategy.
