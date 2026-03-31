---
paths:
  - ".claude/skills/**/SKILL.md"
  - ".claude/skills/**/*.md"
---

# Skill Authoring Guidelines

Quick reference for creating and reviewing Claude Code skills.

**Sources:**

- [Skill authoring best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices)
- [Claude Code Skills docs](https://code.claude.com/docs/en/skills)

## Core Principles

### 1. Conciseness is Key

Context window is shared. Only add what Claude doesn't already know.

**Challenge each line:**

- "Does Claude need this explanation?"
- "Does this paragraph justify its token cost?"

**Good:** ~50 tokens with code example
**Bad:** ~150 tokens explaining what a PDF is

### 2. Degrees of Freedom

| Freedom | When | Example |
| ------- | ---- | ------- |
| **High** (text instructions) | Multiple approaches valid | Code review guidelines |
| **Medium** (pseudocode/params) | Preferred pattern exists | Report template with options |
| **Low** (exact script) | Operations are fragile | Database migrations |

### 3. Test With All Models

- **Haiku:** Needs more guidance
- **Sonnet:** Balanced
- **Opus:** Avoid over-explaining

## SKILL.md Structure

```markdown
---
name: skill-name
description: What it does and WHEN to use it. Third person only.
---

# Skill Title

## Quick start
[Minimal working example]

## Detailed instructions
[Step-by-step workflow]

## References
See [REFERENCE.md](REFERENCE.md) for details
```

### Frontmatter rules

| Field | Rules |
| ----- | ----- |
| `name` | Max 64 chars, lowercase + numbers + hyphens only |
| `description` | Max 1024 chars, non-empty, third person |

**Reserved words (avoid):** anthropic, claude

### Naming conventions

**Prefer gerund form:** `processing-pdfs`, `analyzing-data`, `testing-code`

**Acceptable:** `pdf-processing`, `process-pdfs`

**Avoid:** `helper`, `utils`, `tools`, vague names

### Effective descriptions

```yaml
# GOOD - specific, includes triggers
description: Build and test mylib with Clang+Ninja. Use after pulling changes or when user asks to verify builds.

# BAD - vague
description: Helps with building
```

## File Structure (CRITICAL)

**Each skill MUST be a directory with `SKILL.md` as the entrypoint:**

```text
# CORRECT
.claude/skills/lib-verify/SKILL.md

# WRONG - will NOT work
.claude/skills/lib-verify.md
```

Keep SKILL.md under **500 lines**. Split into separate files:

```text
skill-name/
├── SKILL.md              # Main instructions (REQUIRED entrypoint)
├── REFERENCE.md          # Detailed docs (loaded as needed)
├── EXAMPLES.md           # Usage examples
└── scripts/
    └── helper.py         # Executed, not loaded into context
```

**Keep references one level deep.** All reference files should link directly from SKILL.md.

## Invocation Methods

Skills can be invoked two ways:

1. **Slash command (explicit):** `/skill-name` or `/skill-name arg1 arg2`
2. **Natural language (auto-detected):** Claude matches your request against the skill's `description` field

Both methods work. Use `disable-model-invocation: true` in frontmatter to allow only explicit slash commands.

## Anti-Patterns

| Don't | Do Instead |
| ----- | ---------- |
| Windows paths `scripts\helper.py` | Unix paths `scripts/helper.py` |
| Multiple options without default | One recommended approach + alternatives |
| Deeply nested references | One level deep from SKILL.md |
| Time-sensitive dates | "Old patterns" section |
| Assuming packages installed | Explicit `pip install` or `npm install` |
| Magic numbers | Document all constants |

## Checklist

### Before publishing

- [ ] Description is specific and includes triggers
- [ ] SKILL.md < 500 lines
- [ ] Additional details in separate files
- [ ] No time-sensitive info
- [ ] Consistent terminology
- [ ] File references one level deep
- [ ] Workflows have clear steps
- [ ] Tested with real scenarios

### For skills with code

- [ ] Scripts handle errors explicitly
- [ ] No magic constants
- [ ] Required packages listed
- [ ] Forward slashes in all paths
- [ ] Validation steps for critical operations

## Quick Reference

| Element | Limit |
| ------- | ----- |
| `name` | 64 chars, lowercase/numbers/hyphens |
| `description` | 1024 chars, third person |
| SKILL.md body | < 500 lines |
| References | 1 level deep |
| Token cost at scan | ~100 tokens (metadata only) |
| Token cost when active | < 5k tokens |
