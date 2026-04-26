# Skill Authoring Guidelines

Quick reference for creating and reviewing Claude Code skills.

**Sources:**

- [Skill authoring best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices)
- [Claude Code Skills docs](https://code.claude.com/docs/en/skills)

---

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
|---------|------|---------|
| **High** (text instructions) | Multiple approaches valid | Code review guidelines |
| **Medium** (pseudocode/params) | Preferred pattern exists | Report template with options |
| **Low** (exact script) | Operations are fragile | Database migrations |

### 3. Test With All Models

- **Haiku:** Needs more guidance
- **Sonnet:** Balanced
- **Opus:** Avoid over-explaining

---

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

### Frontmatter Rules

| Field | Rules |
|-------|-------|
| `name` | Max 64 chars, lowercase + numbers + hyphens only |
| `description` | Max 1024 chars, non-empty, third person |

**Reserved words (avoid):** anthropic, claude

### Naming Conventions

**Prefer gerund form:** `processing-pdfs`, `analyzing-data`, `testing-code`

**Acceptable:** `pdf-processing`, `process-pdfs`

**Avoid:** `helper`, `utils`, `tools`, vague names

### Effective Descriptions

```yaml
# GOOD - specific, includes triggers
description: Build and test libparchis with Clang+Ninja. Use after pulling changes or when user asks to verify builds.

# BAD - vague
description: Helps with building
```

---

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

---

## Workflows

### Use Checklists for Complex Tasks

```markdown
## Workflow

Copy this checklist:

```

- [ ] Step 1: Configure
- [ ] Step 2: Build
- [ ] Step 3: Test
- [ ] Step 4: Verify

```

**Step 1: Configure**
[Instructions...]
```

### Implement Feedback Loops

Run validator -> fix errors -> repeat

```markdown
1. Make changes
2. **Validate immediately**: `python validate.py`
3. If validation fails, fix and re-validate
4. **Only proceed when validation passes**
```

---

## Content Guidelines

### Avoid Time-Sensitive Info

```markdown
# BAD
If doing this before August 2025, use old API.

# GOOD
## Current method
Use v2 API...

## Old patterns (deprecated)
<details>Legacy v1 API...</details>
```

### Consistent Terminology

Pick ONE term and use it everywhere:

- Always "API endpoint" (not mix with "URL", "route", "path")
- Always "field" (not mix with "box", "element")

---

## Common Patterns

### Template Pattern

```markdown
## Output format

ALWAYS use this structure:

```markdown
# [Title]
## Summary
[Overview]
## Findings
- Finding 1
- Finding 2
```

```

### Examples Pattern

```markdown
## Examples

**Input:** Added user authentication
**Output:**
```

feat(auth): implement JWT-based authentication

```
```

### Conditional Workflow

```markdown
1. Determine type:
   - **Creating new?** -> Follow "Creation workflow"
   - **Editing existing?** -> Follow "Editing workflow"
```

---

## Scripts & Code

### Solve, Don't Punt

```python
# GOOD - handles errors
def process_file(path):
    try:
        return open(path).read()
    except FileNotFoundError:
        print(f"Creating {path}")
        return ""

# BAD - punts to Claude
def process_file(path):
    return open(path).read()  # Just fails
```

### Document Constants

```python
# GOOD
REQUEST_TIMEOUT = 30  # HTTP requests typically complete within 30s

# BAD
TIMEOUT = 47  # Magic number, no explanation
```

### Make Execution Intent Clear

```markdown
# Execute the script
Run `python scripts/build.py` to compile.

# Read as reference
See `scripts/build.py` for the build algorithm.
```

---

## Anti-Patterns

| Don't | Do Instead |
|-------|-----------|
| Windows paths `scripts\helper.py` | Unix paths `scripts/helper.py` |
| Multiple options without default | One recommended approach + alternatives |
| Deeply nested references | One level deep from SKILL.md |
| Time-sensitive dates | "Old patterns" section |
| Assuming packages installed | Explicit `pip install` or `npm install` |
| Magic numbers | Document all constants |

---

## Checklist

### Before Publishing

- [ ] Description is specific and includes triggers
- [ ] SKILL.md < 500 lines
- [ ] Additional details in separate files
- [ ] No time-sensitive info
- [ ] Consistent terminology
- [ ] File references one level deep
- [ ] Workflows have clear steps
- [ ] Tested with real scenarios

### For Skills with Code

- [ ] Scripts handle errors explicitly
- [ ] No magic constants
- [ ] Required packages listed
- [ ] Forward slashes in all paths
- [ ] Validation steps for critical operations

---

## Quick Reference

| Element | Limit |
|---------|-------|
| `name` | 64 chars, lowercase/numbers/hyphens |
| `description` | 1024 chars, third person |
| SKILL.md body | < 500 lines |
| References | 1 level deep |
| Token cost at scan | ~100 tokens (metadata only) |
| Token cost when active | < 5k tokens |
