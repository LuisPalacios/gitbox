---
paths:
  - ".claude/skills/**/*.{ts,js,mjs}"
---

# JavaScript/TypeScript Script Guidelines

## Core Rule

**Zero-footprint dependency management**: Use `pnpm dlx` to execute tools and scripts. Never create `node_modules/` or `package.json` in project root.

## Requirements

- `pnpm` — [pnpm.io](https://pnpm.io/)

## Two Use Cases

### 1. CLI Tools (e.g., markdownlint, prettier)

```bash
pnpm dlx tool-name [args]
```

Configuration files (`.prettierrc`, `.markdownlint-cli2.jsonc`) belong in skill directory.

### 2. Custom Scripts (TypeScript/JavaScript)

```bash
pnpm dlx tsx <plugin-path>/skills/skill-name/scripts/script.ts [args]
```

## File Structure

```text
.claude/skills/skill-name/
├── SKILL.md
└── scripts/
    ├── script_name.ts    # TypeScript (recommended)
    └── script_name.mjs   # Or ES modules
```

## Script Template

```typescript
#!/usr/bin/env node
/**
 * Brief description.
 * Usage: pnpm dlx tsx script.ts <args>
 * Dependencies: none (or list external packages)
 */

import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { exit } from 'node:process';

async function main() {
  const args = process.argv.slice(2);
  if (args.length === 0) {
    console.log('Usage: ...');
    exit(1);
  }
  // Implementation
}

main().catch((error) => {
  console.error('Error:', error.message);
  exit(1);
});
```

## External Dependencies

**Option 1**: Inline execution (simple cases)

```bash
pnpm dlx -y tsx -y package-name script.ts
```

**Option 2**: Skill-level package.json (complex cases)

Create `package.json` ONLY in `.claude/skills/skill-name/` directory:

```json
{
  "name": "skill-name-scripts",
  "version": "1.0.0",
  "private": true,
  "type": "module",
  "dependencies": {
    "package-name": "^1.0.0"
  }
}
```

Then: `cd .claude/skills/skill-name && pnpm install && pnpm dlx tsx scripts/script.ts`

**Critical**: Never create `package.json` in project root.

## Key Rules

- **File extensions**: `.ts` (TypeScript) or `.mjs` (ES modules)
- **Imports**: Use `node:` prefix for built-ins (`node:fs/promises`, `node:path`)
- **Async**: Use async/await for file operations
- **Execution**: Always via `pnpm dlx tsx`, never `node` directly
- **No project pollution**: Dependencies only in skill directory if needed

## SKILL.md Documentation

Document in the skill's SKILL.md:

- **Environment Setup**: Mention `pnpm` requirement and zero-footprint approach
- **Commands**: Show exact `pnpm dlx` syntax with plugin-path placeholder
- **Dependencies**: If using external packages, document them

## Reference

- **CLI tools example**: `.claude/skills/fixing-markdown/` (uses markdownlint-cli2, prettier)
- **pnpm dlx docs**: [pnpm.io/cli/dlx](https://pnpm.io/cli/dlx)
- **tsx docs**: [github.com/privatenumber/tsx](https://github.com/privatenumber/tsx)
