---
paths:
  - "ai/skills/**"
---

# Skill authoring rules

Before creating or changing a repository skill, read and follow [guideline_skills.md](../context/guideline_skills.md). Edit the skill under `ai/skills/`, never its rendered copy, and follow [ai-harness.md](ai-harness.md) for harness blocks and tokens. After the change, run `uv run ai/scripts/sync.py sync`.

Skill dependencies use zero-footprint execution: Python scripts run through `uv run` with PEP 723 metadata; npm CLI tools run through `pnpm dlx` with exact package versions. Document each dependency and its purpose in `SKILL.md`. Cache downloads follow session permissions; a permission requirement does not justify switching to global executables or installing skill dependencies in the project. Test the workflow without globally installed package tools and check that it leaves no dependency artifacts in the repository. Application builds retain their own documented dependency workflow.
