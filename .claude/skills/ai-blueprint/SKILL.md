---
name: ai-blueprint
description: Create or update the ai_blueprint/ directory with machine-readable architecture documentation (C4 diagrams, DBML data model, OpenAPI contract, Gherkin features, design tokens). Use when the user wants to generate the blueprint, refresh it after codebase changes, or update specific blueprint files.
---

# /ai-blueprint -- Generate or Update AI Blueprint

**IMPORTANT:** Before starting, inform the user: "I'm executing `/ai-blueprint`"

Reverse-engineer the gitbox codebase into a structured, machine-readable blueprint inside `ai_blueprint/` at the repo root. Supports both initial creation and incremental updates.

## Usage

```text
/ai-blueprint              # Create from scratch or full update of all files
/ai-blueprint update       # Same as above (explicit)
/ai-blueprint <file>       # Update a single deliverable (e.g., "dbml", "api", "features")
```

**File shortcuts:**

| Shortcut | File |
| --- | --- |
| `arch` | `01_Architecture.md` |
| `dbml` | `02_Database.dbml` |
| `api` | `03_API_Contract.yaml` |
| `features` | `04_Features/*.feature` |
| `tokens` | `05_UI_Design.json` |
| `prompt` | `00_TUI_Prompt.md` |

## Workflow

### Step 0: Detect Mode

- If `ai_blueprint/` does **not** exist: full creation (all 6 files)
- If `ai_blueprint/` **exists**: full update (re-read codebase, regenerate all files)
- If a specific `<file>` argument is given: update only that deliverable

### Step 1: Deep Codebase Analysis

Read these source files thoroughly before generating any output:

**Core packages:**

- `pkg/config/config.go` -- all Config structs and types
- `pkg/config/crud.go` -- CRUD operations
- `pkg/config/load.go` -- load + validation logic
- `pkg/config/save.go` -- save + backup logic
- `pkg/config/migrate.go` -- v1-to-v2 migration
- `pkg/credential/credential.go` -- credential resolution and setup
- `pkg/credential/validate.go` -- credential validation helpers
- `pkg/provider/provider.go` -- Provider interfaces and shared types
- `pkg/provider/github.go` -- GitHub API endpoints
- `pkg/provider/gitlab.go` -- GitLab API endpoints
- `pkg/provider/gitea.go` -- Gitea/Forgejo API endpoints
- `pkg/provider/bitbucket.go` -- Bitbucket API endpoints
- `pkg/provider/http.go` -- HTTP utilities
- `pkg/git/git.go` -- git subprocess operations
- `pkg/status/status.go` -- status checking
- `pkg/mirror/mirror.go` -- mirror types
- `pkg/mirror/setup.go` -- mirror setup logic
- `pkg/mirror/status.go` -- mirror status checking
- `pkg/mirror/discover.go` -- mirror discovery
- `pkg/identity/identity.go` -- identity management

Also read any `*_test.go` files in the above packages -- tests document expected behavior, error conditions, and edge cases that the blueprint must capture.

**CLI:**

- `cmd/cli/main.go` -- entry point, all registered commands
- All `cmd/cli/*_cmd.go` files -- every subcommand with flags and behavior

**GUI:**

- `cmd/gui/app.go` -- all Wails binding methods (read entire file)
- `cmd/gui/frontend/src/App.svelte` -- CSS custom properties, UI structure
- `cmd/gui/frontend/src/lib/theme.ts` -- color palettes, status symbols
- `cmd/gui/frontend/src/lib/types.ts` -- TypeScript interfaces
- `cmd/gui/frontend/src/lib/bridge.ts` -- Wails event names

**Schema and docs:**

- `gitbox.schema.json` -- JSON Schema
- `docs/architecture.md` -- existing architecture docs

### Step 1.5: Extract API Surface

After reading all source files, extract the **public API surface** of `pkg/`:

- For each package, list every exported function/method with its full Go signature
- Note error return semantics: which functions return `error`, which return `(result, error)`, which silently succeed
- Identify **calling sequences** where functions must be called in order (e.g., credential verify → store → configure clone)
- Identify **union-like types** where a struct field can hold different kinds (e.g., a form field that is either TextInput or SelectField)
- Note which functions perform I/O (file, network, git subprocess) vs. pure computation

This analysis feeds into the DBML deliverable (API Surface table group) and the implementation prompt.

### Step 2: Generate Files

Use subagents for parallelism. The execution order matters for cross-referencing:

**Wave 1 (parallel):**

- `02_Database.dbml` -- config data model as DBML tables + API surface notes
- `05_UI_Design.json` -- design tokens in W3C DTCG format

**Wave 2 (parallel, after wave 1):**

- `01_Architecture.md` -- C4 Level 1 + Level 2 diagrams in Mermaid flowchart syntax
- `04_Features/` -- 7 Gherkin feature files (MUST include error and empty-state scenarios)

**Wave 3 (after wave 2):**

- `03_API_Contract.yaml` -- OpenAPI 3.0 spec (MUST include async event schemas)

**Always last:**

- `00_<Name>_Prompt.md` -- implementation prompt for a new consumer (only if it doesn't exist yet, skip on updates). Use `ai_blueprint/00_TUI_Prompt.md` as the gold-standard example of structure and depth

### Step 3: Validate

**Format checks:**

- JSON: validate `05_UI_Design.json` parses correctly
- YAML: verify `03_API_Contract.yaml` starts with `openapi: "3.0.3"`
- Mermaid: verify code blocks in `01_Architecture.md` use ` ```mermaid ` fencing
- Gherkin: verify each `.feature` file starts with `Feature:`
- DBML: verify `02_Database.dbml` contains `Table` definitions

**Content quality checks:**

- Cross-reference: entity names consistent across all files
- DBML includes an "API Surface" table group with function signatures and calling sequences
- Each `.feature` file has at least one `@error` scenario and one `@empty` scenario
- API contract documents `x-events` for async operations (clone, fetch, discover, mirror setup)
- Implementation prompt (if generated) follows phased structure with verification gates

## Deliverable Specifications

See [REFERENCE.md](REFERENCE.md) for detailed specs per deliverable.

## Notes

- All content must reflect the **current** codebase, not aspirational features
- The blueprint complements (does not replace) existing `docs/` files
- On update: overwrite all files completely -- do not attempt incremental diffs
- Use the repo's existing markdownlint rules for `.md` files
