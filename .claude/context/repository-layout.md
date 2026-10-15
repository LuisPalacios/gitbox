# Repository layout

```text
cmd/
  gui/                    Wails v2 + Svelte GUI (GitboxApp), the only binary
    frontend/src/         Svelte frontend: App.svelte composes the shell; style.css holds the theme tokens
      lib/                Shared modules (bridge, stores, status, theme, i18n, repo-tree + *.test.mjs) and shared pieces (Card, Logo, LauncherMenu, CredentialResult, PrecheckBanner, …)
      lib/components/     Dashboard parts split out of App.svelte (top bar, banners, settings, tabs, cards, repo list/row/detail, footer, compact view)
      lib/modals/         One component per modal + modals.css (the shared dialog kit)
pkg/                      Shared Go library
  ops/                    Account, credential, clone and discovery service layer the GUI calls (+ TestScenario_FullLifecycle)
  config/                 Config v3 model, load/save, v1→v2→v3 migration
  credential/             Credential verification (GCM, SSH, token); global GCM helper check accepts any helper resolving to git-credential-manager (short name or absolute path)
  git/                    Git subprocess operations (os/exec); global config get/get-all/set/unset
  provider/               Provider API clients + mirror interfaces (GitHub, GitLab, Gitea, Forgejo)
  mirror/                 Push/pull mirror setup, status, guides
  workspace/              Read-only VS Code .code-workspace discovery + cache (RefreshCache); no generation, no tmuxinator
  adopt/                  Orphan repo discovery + adoption (multi-root scan, container nested-clone discovery, in-place absolute clone_folder)
  status/                 Clone status checking
  update/                 Auto-update: version check, signature + checksum verified download, self-replace (AppImage build is notify-only); release-signing-key.pub is the pinned release key
  doctor/                 External-tool detection (git, GCM, ssh, …): point-of-use precheck + the GUI's tool check; `LookupIn` is the Setenv-free resolver (extra dirs, ~/.local/bin, Windows shims) shared by harness detection
  harness/                Embedded AI-tool catalog (tools-directory.md: alternates + well-known locations) + pure `Sync` that flags uninstalled harnesses `missing` instead of deleting them
  identity/               Global `~/.gitconfig` user.name/user.email detection + removal
  gitignore/              Global `~/.gitignore_global` managed-block install with sentinel dedup + timestamped backups
docs/
  README.md               Documentation index
  gui-guide.md            GUI guide
  developer-guide.md      Build instructions, contributing
  architecture.md         Technical design
  credentials.md          Credential setup guide
  testing.md              Test levels, fixture format, pre-PR and release checklists
  testing-reference.md    Test inventory, harness internals
  multiplatform.md        Remote hosts (.env) for cross-platform testing
  macos-signing.md        macOS code signing setup
  release-signing.md      Release signing: draft → sign-release.sh → publish, agent setup, fork key swap
  worktree-workflow.md    Parallel worktree sessions (/work-issue, /merge-pr)
  agentic-tools-directory.md  AI harness / agentic tools directory
  es/                     Spanish counterparts of every doc above
  diagrams/               Architecture diagrams
assets/                   Icons, logo, README screenshots (captured with scripts/demo-fleet)
  svg/                    Tool-logo (brand/) and top-bar (ui/) SVG originals; vite-plugin-icons.ts turns them into the app's icons at build time
scripts/
  bootstrap.sh             Cross-platform installer (downloads a release, places GitboxApp, registers Linux menu entry, warns about a Windows installer copy; --cli-only installs the latest 1.x CLI)
  register-gitbox.sh       Linux-only desktop registrar (.desktop + icon in ~/.local/share, idempotent, supports --uninstall)
  installer.iss            Windows Inno Setup installer script (GUI only; cleans up v1's CLI and PATH entry on upgrade)
  appimage/                Linux AppImage build (build-appimage.sh + linuxdeploy, AppRun, desktop, AppStream metainfo, icon)
  dmg/                     macOS DMG installer script + README
  _common.sh               Shared helpers for the dev scripts (.env, SSH, GitboxApp paths)
  ship.sh                  Build GitboxApp on each remote host in .env and stage it
  smoke.sh                 Run `GitboxApp --version` locally and on staged remotes
  health.sh                Read-only run of every checker (gofmt, vet, staticcheck, modernize, govulncheck, deadcode, tests, svelte-check, frontend build warnings, npm ci warnings, npm audit, shellcheck, actionlint, markdownlint)
  test-commands.sh         Print GitboxApp --test-mode launch commands per platform
  run-commands.sh          Print GitboxApp launch commands per platform
  setup-credentials.sh     Run test-setup-credentials.sh locally or on remotes
  test-setup-credentials.sh  Provision test credentials from test-gitbox.json
  send-my-production-config.sh  Copy the local gitbox.json to one remote (with diff + confirm)
  sign-release.sh          Sign a draft release's checksums through the local SSH agent and publish it (--check, --dry-run); standalone, no .env
  demo-fleet/              Fake fleet + mock provider API + window capture for README screenshots (build.sh, run.sh, mock.py, shot.ps1)
.githooks/pre-push        Pre-push hook (gofmt -s check + go vet + unit tests; builds the frontend if dist is missing)
.claude/
  CLAUDE.md               Canonical agent guidance (this file)
  context/
    repository-layout.md  Detailed repository map
    guideline_codex.md    Dual Claude/Codex wiring and capability mapping
    multiplatform-testing.md  Agent recipes for remote builds and desktop testing
    guideline_js.md       JS/TS script conventions
    guideline_python.md   Python script conventions
    guideline_skills.md   Skill-authoring quick reference
    testing-patterns.md   Test helpers, naming conventions
  skills/
    fixing-markdown/      Markdown lint + format
    merge-pr/             Merge a PR (post-/work-issue), clean up worktree + branch
    preparing-brand-logos/  Find tool logos, fit them into the 150x150 brand template (tmp/), draft -dark versions
    ship-builds/          Build + stage GitboxApp on remote hosts in .env
    test-plan/            Pre-PR and release verification
    work-issue/           Worktree → plan → code → push → PR (stops before merge)
  rules/
    skills-authoring.md   Skill creation guidelines
AGENTS.md                 → .claude/CLAUDE.md (symlink — Codex reads project guidance here)
.agents/skills            → ../.claude/skills (symlink — Codex sees skills here)
.github/workflows/ci.yml  Release CI on v* tags: test, build, draft release (+ installers, DMGs, AppImage); scripts/sign-release.sh signs and publishes it; workflow_dispatch = dry run without publishing
.github/workflows/pr.yml  PR + main CI: frontend build, scripts/health.sh gate (every checker, fails on any finding), Linux GUI build smoke
allowed_signers           Release public key for manual ssh-keygen -Y verify (must match pkg/update/release-signing-key.pub)
json/
  gitbox.schema.json      v3 JSON Schema
  gitbox.jsonc            v3 annotated example (Spanish comments)
go.mod / go.sum           Go module
README.md                 Project overview
```
