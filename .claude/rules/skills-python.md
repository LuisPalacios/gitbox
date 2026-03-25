---
paths:
  - ".claude/skills/**/*.py"
---

# Python Script Guidelines

## Core Rule

**Zero-footprint dependency management**: Use `uv run` with PEP 723 inline metadata. Never create `.venv/`, `venv/`, `requirements.txt`, or run `pip install`.

## Requirements

- `uv` — [docs.astral.sh/uv](https://docs.astral.sh/uv/)

## File Structure

```text
.claude/skills/skill-name/
├── SKILL.md
└── scripts/
    └── script_name.py
```

## PEP 723 Format (Required)

```python
#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "package-name>=version",  # List external packages
# ]
# ///
"""Script description and usage."""

import sys
from pathlib import Path

def main():
    pass

if __name__ == '__main__':
    main()
```

**Critical**: Metadata header must be at top, before docstring. Use `dependencies = []` if no external packages needed.

## Execution

```bash
uv run <plugin-path>/skills/skill-name/scripts/script.py [args]
```

## Key Rules

- **Location**: Metadata header before docstring (PEP 723 requirement)
- **Syntax**: Exact format with `# ///` markers (three slashes)
- **Encoding**: Always `encoding='utf-8'` for text file operations
- **Paths**: Use `pathlib.Path` for cross-platform compatibility
- **Exit codes**: Provide clear error messages with `sys.exit(1)` on failure

## SKILL.md Documentation

Document in the skill's SKILL.md:

- **Environment Setup**: Mention `uv` requirement and zero-footprint approach
- **Dependencies**: List what each package does (from PEP 723 header)
- **Commands**: Show exact `uv run` syntax with plugin-path placeholder

## Reference

- **Working example**: `.claude/skills/fixing-markdown/scripts/fix_md_extra.py`
- **PEP 723 spec**: [peps.python.org/pep-0723](https://peps.python.org/pep-0723/)
