# New purpose: from Git wrapper to AI project hub

Date: 2026-04-09

Sources: [radar-meeting-raw.md](radar-meeting-raw.md) (conversation transcript), [radar-meeting-ai.md](radar-meeting-ai.md) (AI summary)

---

## The shift

Gitbox started as a tool to manage Git multi-account environments — accounts, credentials, cloning, sync status, mirroring. That problem is solved. The codebase is mature: five providers, three interfaces (CLI/TUI/GUI), cross-platform, comprehensive tests, clean architecture.

The new direction is bigger: **a native desktop application that manages the full lifecycle of AI-assisted projects.** Not another IDE, not another LLM, not another knowledge base — a wrapper that connects all of them. The tool that sits between the person and their growing constellation of AI projects.

The core insight: **as AI coding tools proliferate, the meta-problem shifts from "how do I use Git?" to "how do I manage dozens of projects, each wired to different AI harnesses, knowledge bases, and workflows?"** The people who need this most are not Git experts — they are professionals and beginners alike who are building more projects than ever because AI makes it possible, and drowning in the organizational overhead.

## What exists today (and what carries forward)

The current gitbox is not thrown away — it becomes the foundation layer. Everything below maps directly to the new vision:

| Current capability | Role in the new vision |
| --- | --- |
| Multi-account Git management | Still the backbone — every project is a Git repo |
| Provider API integrations (5 providers) | Repo creation, discovery, and sync checking remain essential |
| 3-level folder hierarchy with overrides | Becomes the project organization structure (market/client/project) |
| Credential management (GCM, SSH, Token) | Unchanged — projects still need Git auth |
| Status monitoring (9 states) | Expands to include harness health, not just Git sync |
| Cross-provider mirroring | Backup strategy carries forward as-is |
| Three interfaces sharing `pkg/` | Architecture stays — CLI for power users, TUI for terminals, GUI for everyone |
| Auto-update, installers, bootstrap | Distribution model is ready |
| Config v2 with referential integrity | Extends to include harness and template metadata |

The "fleet observer" philosophy holds: the tool never modifies your repos, never commits, never pushes. It observes, organizes, launches, and reports.

## The new vision

### One-sentence pitch

**A native desktop app that organizes, scaffolds, and launches AI-assisted projects — connecting Git, AI coding agents, and knowledge bases into a single coherent workspace.**

### What it is

- A **project organizer** — hierarchical view of all your projects, grouped by market, client, type, or custom taxonomy
- A **scaffolding engine** — create a new project from a template and get a ready-to-work folder with Git repo, AI harness config, knowledge base structure, and pre-built skills
- A **launchpad** — one click to open VS Code, Claude Code terminal, Obsidian, or any configured tool in the context of a specific project
- A **health dashboard** — see which projects are synced, which have uncommitted work, which harnesses are configured, which need attention
- A **harness manager** — detect installed AI tools, configure them per project, migrate configurations between tools

### What it is NOT

- Not an IDE (VS Code, Cursor, Zed do that)
- Not an AI agent (Claude Code, Gemini CLI, Codex do that)
- Not a knowledge base (Obsidian, Notion do that)
- Not a Git client (the user's tools handle branching, merging, committing)
- Not a project management tool in the Jira/Linear sense (no tickets, sprints, boards)

It is the **shell** that makes all those tools work together without the person having to manually wire up every project.

## Architecture evolution

### Config model expansion

The current config has `accounts`, `sources`, and `mirrors`. The new model adds two concepts:

```text
Config v3
├── Global (existing + new fields)
│   ├── folder: root project directory
│   ├── harnesses: detected AI tools and their versions
│   ├── editors: detected code editors (already exists)
│   └── knowledge_bases: detected KB tools (Obsidian, etc.)
├── Accounts (unchanged — Git provider auth)
├── Sources (unchanged — clone groups)
├── Mirrors (unchanged — backup mirrors)
├── Templates (NEW)
│   ├── built-in: bundled with the binary
│   └── custom: user-defined or community templates
└── Projects (NEW — optional metadata layer)
    ├── template_used: which template created this project
    ├── harnesses: which AI tools are configured here
    ├── knowledge_base: which KB tool is connected
    └── tags: user-defined labels for organization
```

The `Projects` section is metadata only — the actual project files live in the folder hierarchy already managed by the sources/repos system. This avoids duplicating state.

### New packages

| Package | Purpose |
| --- | --- |
| `pkg/harness/` | Detect, configure, and manage AI coding harnesses |
| `pkg/template/` | Template engine for project scaffolding |
| `pkg/knowledge/` | Knowledge base integration (Obsidian vault structure, MCP config) |
| `pkg/launcher/` | Launch external tools in project context |

### Harness abstraction

Each AI harness has a consistent interface:

```go
type Harness interface {
    Name() string                          // "claude-code", "gemini-cli", "codex", "cursor"
    Detect() (installed bool, version string)
    ConfigFiles() []string                 // e.g., [".claude/", "CLAUDE.md"]
    Init(projectDir string) error          // scaffold harness config for a project
    Migrate(from Harness) error            // convert another harness's config to this one
    Health(projectDir string) HarnessStatus
}
```

This is the "multi-harness" concept from the conversation. When a user switches from Claude Code to Codex, the tool knows which files to transform.

## Feature roadmap (phased)

### Phase 1 — Harness awareness (builds on what exists)

**Goal:** The dashboard shows which AI tools are configured in each project, and the user can launch them directly.

- Detect installed harnesses on the system (Claude Code, Gemini CLI, Codex, Cursor, Windsurf)
- For each repo/project, scan for harness config files (`.claude/`, `CLAUDE.md`, `.gemini/`, `.codex/`, `.cursor/`, etc.)
- Show harness badges in the dashboard alongside Git status
- Add launch buttons: "Open in VS Code", "Open Claude Code session", "Open in Cursor"
- Settings screen: configure which harnesses to detect, default editor

**Why first:** This is pure observation — no writes, no scaffolding. It extends the fleet observer pattern the project already follows. Low risk, immediate value.

**Size:** M — new `pkg/harness/` package, dashboard changes in TUI+GUI, settings additions.

### Phase 2 — Project scaffolding

**Goal:** Create a new project from a template with one command/click.

The scaffolding flow:
1. User clicks "New project" (or runs `<tool> new`)
2. Selects a template (or describes the project for AI-assisted template selection)
3. Provides: project name, description, Git account, harness preference
4. The tool:
   - Creates the Git repo (local + remote via provider API, already implemented)
   - Scaffolds the folder structure from the template
   - Initializes the AI harness config (CLAUDE.md, rules, context files)
   - Sets up Obsidian vault structure if selected
   - Configures MCP server connections between tools
   - Commits the initial scaffold and pushes

**Template anatomy:**

```text
templates/
  general/
    scaffold/              # files to copy into the project
      .claude/
        CLAUDE.md.tmpl     # Go template with project variables
      .obsidian/
        ...
    template.json          # metadata: name, description, variables, harnesses
  consulting/
    scaffold/
      meetings/
        _template-pre.md
        _template-post.md
      .claude/
        skills/
          pre-meeting/
          post-meeting/
          transcription/
    template.json
  web-app/
    ...
```

**Pre-built templates (initial set):**
- **General** — minimal: Git repo + harness config + knowledge base skeleton
- **Consulting** — meetings folder with pre/post templates, transcription skill, client deliverables structure
- **Web application** — standard web project structure with CI templates
- **Research/Analysis** — data-oriented structure with notebooks, reports, references

**Why second:** Scaffolding depends on harness awareness (Phase 1) to know what to configure. This is where the real value kicks in for the "new developer creating lots of projects" persona from the conversation.

**Size:** L — new `pkg/template/` package, template files, new screens in TUI+GUI, CLI `new` command.

### Phase 3 — Multi-harness management

**Goal:** Configure multiple AI harnesses per project and migrate between them.

- Per-project harness configuration: "this project uses Claude Code + Cursor"
- Migration assistant: switch a project from Claude Code to Codex (or vice versa)
  - Map config files between harness formats
  - Preserve custom instructions, context, and rules
  - Handle MCP server configuration differences
- Bulk operations: "migrate all projects from harness X to harness Y"
- Harness health monitoring: is the harness config valid? Are MCP connections working?

**Reference:** The conversation mentions an open-source CLI tool that does multi-harness management. Research and evaluate whether to integrate it or build natively. The key insight is that harness config formats are converging (AGENTS.md, CLAUDE.md, .cursorrules, etc.) — the migration logic is mostly file mapping and content transformation.

**Why third:** Requires both harness awareness (Phase 1) and scaffolding (Phase 2) to be useful. Migration is complex but high value for teams switching tools or using multiple tools.

**Size:** L — harness migration engine, config format parsers, bulk operation support.

### Phase 4 — Knowledge base integration

**Goal:** Obsidian (and future KB tools) are first-class citizens in the project lifecycle.

- Detect Obsidian installation and vault locations
- Per-project Obsidian vault: scaffold a vault structure as part of project creation
- MCP bridge: configure the Obsidian MCP server to connect with AI harnesses
- Cross-project knowledge: a global vault that references all project vaults (the conversation debates whether Obsidian should be per-project or global — support both, let the user choose)
- Meeting workflow: templates for pre-meeting, post-meeting, transcription processing, action items

**Why fourth:** KB integration is the most opinionated feature. Starting with Obsidian keeps scope manageable. The meeting workflow templates from the conversation fit here.

**Size:** L — Obsidian vault scaffolding, MCP configuration, meeting workflow skills.

### Phase 5 — Intelligence layer

**Goal:** The tool itself becomes smarter about your project portfolio.

- Project health scoring: combine Git sync status, harness config health, last activity date, knowledge base completeness
- Stale project detection: "you haven't touched these 5 projects in 3 months — archive?"
- Cross-project search: find content across all your projects without opening each one
- Activity timeline: what changed across all projects today/this week
- Recommendations: "project X has Claude Code but not Obsidian — add knowledge base?"

**Why last:** This is the "millones de funcionalidades dentro" (millions of features inside) that the colleague mentioned. It only makes sense when the foundation (Phases 1-4) is solid.

**Size:** XL — this is multiple features, each individually scoped.

## Name candidates

The current name "gitbox" signals Git-centricity. The new vision is bigger. Here are candidates evaluated on: memorability, CLI ergonomics (short, easy to type), domain availability likelihood, clarity of purpose, and uniqueness.

### Option 1: **devpilot**

- *Pitch:* Your AI development copilot for project management
- *CLI:* `devpilot new`, `devpilot status`, `devpilot launch`
- *Pros:* Clear purpose, "pilot" implies navigation and control, recognizable
- *Cons:* "copilot" is overused in AI branding, 8 characters, GitHub Copilot association
- *Verdict:* Safe but derivative

### Option 2: **hangar**

- *Pitch:* Where your AI projects are housed, maintained, and launched
- *CLI:* `hangar new`, `hangar status`, `hangar launch`
- *Pros:* Strong metaphor (aircraft hangar = maintenance + launch), 6 characters, unique in dev tooling, works in Spanish ("hangar" is the same word)
- *Cons:* No direct AI/code connotation, might confuse people looking for a Git tool
- *Verdict:* Bold, distinctive, memorable

### Option 3: **forja**

- *Pitch:* The forge where AI projects are shaped
- *CLI:* `forja new`, `forja status`, `forja launch`
- *Pros:* "Forge" in Spanish, works as a brand in English, 5 characters, unique, implies creation and craftsmanship
- *Cons:* Non-Spanish speakers might mispronounce it, competes with "Forgejo" in mental space
- *Verdict:* Cultural, distinctive, but niche

### Option 4: **aibox**

- *Pitch:* Your AI project box — direct evolution from gitbox
- *CLI:* `aibox new`, `aibox status`, `aibox launch`
- *Pros:* Directly maps to the conversation's "caja de IA", obvious evolution from gitbox, 5 characters, self-explanatory
- *Cons:* Generic (could mean AI hardware, AI sandbox), "AI" prefix is trendy and may age poorly
- *Verdict:* Obvious choice, low risk, low distinctiveness

### Option 5: **taller**

- *Pitch:* Your AI workshop (taller = workshop in Spanish)
- *CLI:* `taller new`, `taller status`, `taller launch`
- *Pros:* "Workshop" perfectly describes the concept, 6 characters, culturally rooted, unique in tooling
- *Cons:* English speakers will read it as "more tall", pronunciation confusion
- *Verdict:* Works if the audience is bilingual, risky otherwise

### Option 6: **loom**

- *Pitch:* Where threads of AI, code, and knowledge are woven together
- *CLI:* `loom new`, `loom status`, `loom launch`
- *Pros:* 4 characters (the shortest), beautiful metaphor (weaving tools together), easy to type, easy to say in any language
- *Cons:* Loom.com exists (video tool), might cause confusion
- *Verdict:* Elegant, poetic, potentially confusing

### My recommendation: **hangar**

"Hangar" captures the essence perfectly — a place where complex machines (projects) are stored, maintained, inspected, and launched. It implies professionalism without being corporate. It works identically in English and Spanish. The CLI reads naturally: `hangar new consulting-project`, `hangar status`, `hangar launch my-project vscode`. And it is completely unoccupied in the dev tooling space.

The secondary choice is **aibox** for its simplicity and direct lineage from gitbox.

## Recommendations and additional ideas

### Ideas that enrich the vision

**1. Project archetypes with AI-generated refinement**

Beyond static templates, the scaffolding engine could use the configured AI harness to refine the template. Flow: user picks "consulting" template → provides a 2-sentence project description → the tool calls the AI harness to customize the folder structure, generate a tailored CLAUDE.md, and pre-populate meeting agendas. The template is the skeleton; the AI adds the muscle.

**2. Credential wallet for AI services**

Just as gitbox manages Git credentials (GCM, SSH, Token) per account, the new tool should manage AI service credentials: Anthropic API keys, Google AI keys, OpenAI keys. Store them in the OS keyring (same pattern as current token storage), associate them with harness configs, and inject them into project environments. This eliminates the `.env` file sprawl across dozens of projects.

**3. Project health heartbeat**

A lightweight background process (or periodic check, like the current status refresh) that monitors all projects and surfaces a notification when something needs attention: "3 projects have uncommitted changes", "Project X's harness config references a deleted MCP server", "Project Y hasn't been synced in 30 days". The TUI/GUI already has the infrastructure for periodic refresh — extend it.

**4. Workspace snapshots**

Before migrating a harness or restructuring a project, take a snapshot: a lightweight backup of the harness config, knowledge base index, and folder structure (not the full Git repo — that's what Git is for). This gives users confidence to experiment with migrations.

**5. Community template registry**

A future extension: publish and discover templates via a registry (a Git repo with a known structure, or a simple HTTP API). "Install a template for Django projects with Claude Code" → `hangar template install django-claude`. This turns the tool into a platform.

**6. Onboarding wizard for AI newcomers**

The conversation identifies beginners as a key audience. A first-run wizard that: detects which AI tools are already installed → offers to set up accounts for those that aren't → creates a "My first AI project" from a tutorial template → walks through the first launch. This is the "facilitador" (facilitator) concept from the conversation, made concrete.

**7. Project-to-project references**

Some projects depend on others (a shared library used by multiple apps, a design system consumed by several frontends). The tool could track these relationships and surface them: "Project A depends on Library B — Library B has 3 commits you haven't pulled."

**8. Session logging**

Every time the user opens a project in an AI harness, log the session start/end time and a brief summary (if the harness provides one). Over time, this builds a timeline: "I worked on Project X for 45 minutes on Tuesday using Claude Code." Useful for freelancers tracking billable hours and for anyone wanting to understand where their time goes.

### Risk assessment

| Risk | Mitigation |
| --- | --- |
| Scope explosion — the vision is huge | Phased roadmap with clear gates; Phase 1 is pure observation, shippable alone |
| Harness config formats are unstable | Abstract behind interfaces; the migration engine handles format changes |
| Template maintenance burden | Start with 2-3 templates; community registry defers the long tail |
| Name confusion during transition | Keep "gitbox" as the underlying engine name; the new name is the product brand |
| Multi-harness competition | The open-source CLI mentioned in the conversation could be a dependency, not a competitor |
| Obsidian lock-in | Knowledge base integration is interface-driven; Obsidian is the first implementation, not the only one |

### What NOT to build

- **An AI chat interface** — harnesses do this, the tool just launches them
- **A Git GUI** — the tool shows status but never manages branches/merges/commits
- **A ticket/issue tracker** — this is project organization, not project management in the PM sense
- **An Obsidian replacement** — scaffold vaults, launch Obsidian, but never edit knowledge base content
- **A billing/invoicing system** — session logging gives data, but the tool stays in the development domain

## Migration path

The transition from gitbox to the new identity does not have to be abrupt:

1. **Now:** Continue shipping gitbox features (W7 branch-aware, W8 orphan adoption) — these remain valuable under the new vision
2. **Phase 1 (harness awareness):** Ship as a gitbox update. The tool gains new capabilities without changing identity
3. **Phase 2 (scaffolding):** This is the inflection point. The `new` command and template system fundamentally change what the tool is. This is when the name changes
4. **Rename:** The binary becomes `hangar` (or chosen name). `gitbox` becomes an alias that prints a deprecation notice. Config file stays `gitbox.json` initially with a migration path to `hangar.json`
5. **Phase 3+:** The tool is fully branded under the new name

## Next steps

1. **Decide on the name** — this affects branding, domain, repo rename, everything downstream
2. **Ship W7 and W8** — these features are valuable regardless of the pivot and keep momentum
3. **Build Phase 1 (harness awareness)** — the first tangible step toward the new vision, low risk, shippable as a gitbox update
4. **Research the multi-harness CLI tool** mentioned in the conversation — evaluate integration vs. build
5. **Design the template format** — the template.json schema and scaffold directory structure need careful design before implementation
6. **Prototype the "New project" flow** — mock up the TUI/GUI experience for creating a project from a template

## Similar ideas

- https://github.com/AbdallaAliDev/HangarAI
