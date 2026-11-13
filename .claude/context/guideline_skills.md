---
read-when: Before creating or changing a skill.
---

# Skill authoring guidelines

I keep shared skill-authoring guidance here. Skills live under `ai/skills/`; `ai/scripts/sync.py` renders them into `.claude/skills/` and `.agents/skills/`, so I edit only the master and follow `.claude/rules/ai-harness.md` for the parts that differ per tool.

**Sources:**

- [Skill authoring best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices)
- [Claude Code Skills docs](https://code.claude.com/docs/en/skills)

---

## Core Principles

### 1. Conciseness is Key

Context window is shared. Only add what the agent doesn't already know.

**Challenge each line:**

- "Does the agent need this explanation?"
- "Does this paragraph justify its token cost?"

**Good:** ~50 tokens with code example
**Bad:** ~150 tokens explaining what a PDF is

### 2. Degrees of Freedom

| Freedom                        | When                      | Example                      |
| ------------------------------ | ------------------------- | ---------------------------- |
| **High** (text instructions)   | Multiple approaches valid | Code review guidelines       |
| **Medium** (pseudocode/params) | Preferred pattern exists  | Report template with options |
| **Low** (exact script)         | Operations are fragile    | Database migrations          |

### 3. Verify in both harnesses

I check skill discovery in fresh Claude Code and Codex sessions. I verify the description, invocation, repository paths, available tool equivalents, and approval gates. I do not execute a publishing workflow merely to test discovery.

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

| Field         | Rules                                            |
| ------------- | ------------------------------------------------ |
| `name`        | Max 64 chars, lowercase + numbers + hyphens only |
| `description` | Max 1024 chars, non-empty, third person          |

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
ai/skills/lib-verify/SKILL.md

# WRONG - will NOT work
ai/skills/lib-verify.md
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

1. **Explicit invocation:** Claude Code uses `/skill-name arg1 arg2`; Codex uses `$skill-name arg1 arg2`.
2. **Natural language (auto-detected):** Claude Code or Codex matches the request against the skill's `description` field

I keep shared `name` and `description` fields concise, front-load the use case, and check the actual discovery output. Clients can shorten descriptions to fit their discovery budget. Claude-specific invocation settings do not configure Codex. I preserve explicit publication gates in the workflow body.

### Codex metadata

I put optional Codex UI metadata (`interface.display_name`, `interface.short_description`, `interface.default_prompt`) in `ai/skills/<name>/agents/openai.yaml`, not in the `SKILL.md` frontmatter. I reserve these files with `only.codex` in `ai/harness.yaml`; the renderer copies YAML byte for byte, so I use literal `$skill-name` invocations instead of harness tokens. Claude receives neither the file nor an instruction to read it.

I preserve implicit invocation unless the user requests an explicit-only skill. Invocation policy does not authorize publication. I add tool dependencies only when the skill actually needs them. Tool-specific frontmatter keys belong under `claude:` or `codex:` only when the corresponding tool supports those keys.

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

```text

**Step 1: Configure**
[Instructions...]
```

### Implement Feedback Loops

Run validator -> fix errors -> repeat

```markdown
1. Make changes
2. **Validate immediately**: `uv run .claude/skills/skill-name/scripts/validate.py`
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

````markdown
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
````

### Examples Pattern

````markdown
## Examples

**Input:** Added user authentication
**Output:**

```text
feat(auth): implement JWT-based authentication
```
````

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

# BAD - punts to the agent
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

Run `uv run .claude/skills/skill-name/scripts/build.py` to compile.

# Read as reference

See `scripts/build.py` for the build algorithm.
```

---

## Anti-Patterns

| Don't                             | Do Instead                                                             |
| --------------------------------- | ---------------------------------------------------------------------- |
| Windows paths `scripts\helper.py` | Unix paths `scripts/helper.py`                                         |
| Multiple options without default  | One recommended approach + alternatives                                |
| Deeply nested references          | One level deep from SKILL.md                                           |
| Time-sensitive dates              | "Old patterns" section                                                 |
| Assuming packages installed       | Declare prerequisites and respect installation and network permissions |
| Magic numbers                     | Document all constants                                                 |

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

| Element                | Limit                               |
| ---------------------- | ----------------------------------- |
| `name`                 | 64 chars, lowercase/numbers/hyphens |
| `description`          | 1024 chars, third person            |
| SKILL.md body          | < 500 lines                         |
| References             | 1 level deep                        |
| Token cost at scan     | ~100 tokens (metadata only)         |
| Token cost when active | < 5k tokens                         |
