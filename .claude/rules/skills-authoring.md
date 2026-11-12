---
paths:
  - ".claude/skills/**/SKILL.md"
  - ".claude/skills/**/*.md"
---

# Shared skill authoring rules

Before creating or changing a repository skill, read and follow [guideline_skills.md](../context/guideline_skills.md). I keep the authoring guidance there so Claude Code and Codex use one source of truth. Codex reads this rule through the rules table in `AGENTS.md`. After changing a skill, run the `mirroring-harness` sync so `.agents/skills/` matches.
