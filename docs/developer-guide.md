# Developer guide

This guide covers building Gitbox from source, checking a change, extending providers, operations and the config schema, and cutting a release. For how the pieces fit together, see [architecture.md](architecture.md).

---

## Prerequisites

- **Go** 1.26+ — [install](https://go.dev/doc/install)
- **Node.js** 20.19+ or 22.12+ — [install](https://nodejs.org/) (Svelte 5 + Vite 8 frontend; CI builds on Node 24)
- **Git** 2.39+
- **Wails CLI** v2 — `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- **Platform-specific:** Windows needs Git for Windows; macOS needs Xcode CLI Tools (`xcode-select --install`); Linux needs `libwebkit2gtk-4.1-dev` and `libgtk-3-dev`

For multiplatform testing via SSH, see [multiplatform.md](multiplatform.md).

---

## Building from source

Gitbox v2 builds one binary, the `GitboxApp` desktop app. The v1 CLI and TUI live only on the `release/v1` branch.

```bash
# Build-time version stamp. Without it the app falls back to `git describe` at runtime.
LDFLAGS="-X main.version=$(git describe --tags --always)-dev -X main.commit=$(git rev-parse --short HEAD)"

# Copy app icons from assets/ into the Wails build directory (they are not checked in there)
cp assets/appicon.png cmd/gui/build/appicon.png
mkdir -p cmd/gui/build/windows                         # absent in a fresh clone
cp assets/icon.ico    cmd/gui/build/windows/icon.ico   # Windows only

# Development mode (hot reload)
cd cmd/gui
wails dev

# Production build
# macOS only: target macOS 13, the Go runtime's minimum (Wails defaults to 10.13)
export CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0
wails build -ldflags "$LDFLAGS"          # Linux: add -tags webkit2_41
# Output: cmd/gui/build/bin/GitboxApp[.exe] (GitboxApp.app on macOS)
```

`wails build` cannot cross-compile the GUI: each target needs its host's native webview (WebView2 on Windows, WebKit on macOS, WebKitGTK on Linux). To build for another platform, I build on that platform; [multiplatform.md](multiplatform.md) shows how `scripts/ship.sh` does it over SSH.

The Go code embeds the compiled frontend from `cmd/gui/frontend/dist`. Plain Go commands such as `go vet ./...`, `go test ./...`, or a quick compile check (`go build -o /dev/null ./cmd/gui`) need that folder to exist. `wails build` creates it; to create it without a full build:

```bash
cd cmd/gui/frontend
npm ci
npm run build
```

Once the app is built, `GitboxApp --version` prints the version and exits.

### Design rules

These rules hold for every change. [architecture.md](architecture.md) explains the rest of the design.

- **`pkg/` holds the business logic.** `cmd/gui` only holds Wails bindings, locking, events, and GUI-only concerns.
- **`pkg/ops` never saves or locks.** The caller in `cmd/gui` takes the config lock, runs the operation, saves, and emits events.
- **Git runs through `os/exec`.** Gitbox shells out to the system `git` binary, not libgit2; provider APIs use `net/http`.
- **No console flash on Windows.** Every `exec.Command` in `cmd/gui/` calls `git.HideWindow(cmd)` before it runs.

---

## Testing

Quick start:

```bash
go test -short ./...    # unit tests (no setup needed beyond the frontend dist folder)
go test ./...           # everything (needs test-gitbox.json for the pkg/ops scenario)
```

I activate the pre-push hook once per clone with `git config core.hooksPath .githooks`. [testing.md](testing.md) covers the test levels, the fixture, the frontend tests, and the pre-PR and release checklists. I use `/test-plan` in Claude Code or `$test-plan` in Codex to automate the pre-PR checks.

### Health check

Before opening a PR I run every code checker in one pass:

```bash
./scripts/health.sh              # every check, one summary line each
./scripts/health.sh go svelte    # only some checks ("go" = all Go checks)
./scripts/health.sh -v           # every finding instead of the first 15
```

It is read-only and exits non-zero when any check reports a finding. Go checks: `gofmt -s`, `go vet`, staticcheck, modernize, govulncheck, deadcode (with an allowlist for functions kept on purpose) and `go test -short`. Frontend checks: pure clone-tree regressions (`npm test`), `svelte-check` (unused locals and parameters fail through `noUnusedLocals`/`noUnusedParameters`), warnings printed by `npm run build` (the same Svelte diagnostics CI shows in its build step), install warnings from a clean `npm ci` in a scratch copy (deprecated packages, install scripts not covered by `allowScripts`), and `npm audit` (with an allowlist, `NPM_AUDIT_ALLOW`, for advisories with no fix that never reach the binary; a listed advisory that stops being reported fails the check). The `npm ci` check needs npm 11.19 or newer. Repo checks: shellcheck on the shell scripts, actionlint on the workflows, markdownlint with the read-only `fixing-markdown` skill config, and the agent harness check, which runs the engine's tests and fails when a rendered harness file drifts from its master in `ai/`.

I run Markdown tooling without global or project-local package installations: `pnpm dlx` resolves `markdownlint-cli2@0.23.3` and `prettier@3.8.0` into its cache, while `uv run` handles the skill's PEP 723 Python scripts. The Markdown health check runs markdownlint only and preserves configuration and execution failures. The `fixing-markdown` runner also checks structural fixes and Prettier formatting; it prepares both cached tools before editing. Its `--check` leaves source files unchanged, although a first run can populate the caches. A green Markdown health check does not imply that Prettier checked the selected files. Markdown/harness-only health checks do not install or build the application frontend.

The Go analyzers are installed into a temporary folder at versions pinned at the top of the script, so they need no install. Vet, staticcheck and modernize analyze linux, darwin and windows on every run, so platform-specific files are checked from any host. I need shellcheck, actionlint, uv, pnpm and Node on `PATH`; Prettier and markdownlint do not need to be there. I approve any required bootstrap installation separately:

```bash
scoop install shellcheck actionlint     # Windows (brew install … on macOS, apt/dnf on Linux)
scoop install uv                        # brew install uv on macOS
scoop install pnpm                      # brew install pnpm on macOS
```

The PR workflow runs the same script as its gate: any finding fails the job, including a new advisory reported by govulncheck or `npm audit`. CI pins shellcheck 0.11.0, actionlint 1.7.12, pnpm 12.9.1 and setup-uv 10.3.3; the Markdown commands pin their package versions. I use those versions locally so both runs agree.

---

## Development environment quirks

### Launching GitboxApp from Git Bash

When I start `GitboxApp.exe` from a Git Bash or MSYS2 shell, the app inherits Windows variables in POSIX form (`LOCALAPPDATA=/c/Users/you/AppData/Local`). Gitbox cleans the environment it hands to terminals it opens, but when Windows Terminal is the default console host, its delegation path can bypass that cleanup. Tools that read those variables as Windows paths, such as `oh-my-posh` or `$PROFILE` helpers, then fail with errors like `& '/c/Users/...' — not recognized as a cmdlet`.

End users never hit this. Either fix works:

- Launch `GitboxApp.exe` from Explorer, the Start Menu, or a pinned shortcut, which start it with a clean Windows environment.
- Use a [Windows Terminal profile](gui-guide.md#windows-terminal-and-wezterm-profiles) that matches the shell. Gitbox then launches through `wt.exe --profile`, and Windows Terminal starts the shell from its own profile context.

---

## Working with Claude Code and Codex

Claude Code and Codex share one set of project instructions and seven repository skills, each in its own native layout. I edit only the neutral masters in `ai/`: `main.md`, `rules/`, `context/` and `skills/`. The engine in `ai/scripts/sync.py` renders them into both tools as real, tracked files: `.claude/CLAUDE.md`, `.claude/rules/`, `.claude/context/` and `.claude/skills/` for Claude Code, and `AGENTS.md`, `.codex/rules/`, `.codex/context/` and `.agents/skills/` for Codex.

Most text is shared. Where the tools differ (tool names, plan workflow, permissions), a master holds both variants in harness blocks, and tokens such as `{{INVOKE}}` and `{{CONTEXT}}` expand to each tool's prefix and folders. After any change under `ai/` I render and commit the masters with both trees, from either tool:

```bash
uv run ai/scripts/sync.py diff   # what would change
uv run ai/scripts/sync.py sync   # render both harnesses, then verify
```

The [health check](#health-check) fails when a rendered file drifts from its master, so a hand edit to `AGENTS.md` or `.claude/CLAUDE.md` never reaches `main`.

I open either tool at the repository root and invoke a skill as `/work-issue 123` in Claude Code or `$work-issue 123` in Codex; natural-language requests also select skills. Both tools follow the same testing rules and require explicit approval before publishing to GitHub. Native configuration stays hand-edited: Claude permissions live in `.claude/settings.json`, and Codex uses its own session permissions.

I keep build recipes in `ai/context/building.md`; both entry files retain the build requirements and route me to their native context copy. Codex discovers `.agents/skills/` automatically, while its Markdown-rule table tells it which rules to read. Its skills table is a compact index. Optional Codex skill UI metadata lives in `ai/skills/<name>/agents/openai.yaml`, reserved for Codex through `only:` in `ai/harness.yaml`; shared descriptions stay in `SKILL.md`.

I format editable sources through the `fixing-markdown` skill. It resolves explicit rendered targets to their masters, excludes generated trees from recursive formatting, and regenerates both harnesses after changing masters. It uses installed formatters, asks before installing missing tools, and runs its final lint check without autofix. I run `./scripts/health.sh harness markdown shellcheck actionlint` for harness changes and the full health check before a PR.

After changing the harness, I start a fresh session in each tool and ask it to summarize the project rules and list the seven skills. For the model, the size budget and fresh-session checks, see [the harness guide](../ai/context/guideline_harness.md).

---

## Adding a new provider

Providers live in `pkg/provider/`. GitHub, GitLab, Gitea/Forgejo, and Bitbucket are implemented. To add one:

1. Create `pkg/provider/newprovider.go` with a type that implements `Provider` (`ListRepos`, paginated). Add the optional interfaces from `provider.go` and `deleter.go` the provider supports: `RepoCreator` (create repo, existence check), `OrgLister` (owner dropdown in Create repo), `RepoDeleter`, `PushMirrorProvider`, `PullMirrorProvider`, `RepoInfoProvider` (mirror sync checks), and `PRLister` (PR badges). Callers detect each one with a type assertion and skip or refuse the feature when it is absent.
2. Register it in `ByName` in `pkg/provider/provider.go`, the factory every caller uses to get a provider by its config name:

    ```go
    case "newprovider":
        return &NewProvider{}, nil
    ```

3. Add the provider to the per-provider switches: token guidance and scopes in `pkg/provider/guide.go` and `ScopesForAction` in `pkg/provider/errors.go`, `SSHPublicKeyURL` in `pkg/credential/validate.go`, `providerDisplayName` in `pkg/credential/status.go`, and `GCMProviderFor` in `pkg/ops/credential.go` when GCM has a native provider for it.
4. Add `"newprovider"` to the `provider` enum in `json/gitbox.schema.json`, and to the provider options in `cmd/gui/frontend/src/lib/modals/AddAccountModal.svelte` and `EditAccountModal.svelte`.
5. Write `httptest`-backed tests in `pkg/provider/`, following `provider_test.go`.

---

## Adding an operation to pkg/ops

When a new GUI action touches more than one package — config plus clones on disk, credentials plus every clone of an account — I put the logic in `pkg/ops` and keep the Wails binding thin:

1. Add the function to the matching file in `pkg/ops/` (`account.go`, `credential.go`, `clone.go`, or `discover.go`). It takes a `*config.Config` and works on the files it owns on disk.
2. Don't save or lock inside `pkg/ops`. The caller (`cmd/gui/app.go`) takes the config lock, calls the operation, saves the config, and emits events.
3. Keep slow per-clone work (like `ReconfigureClones`) in a separate function, so the caller can run it after a successful save.
4. Add a unit test in `pkg/ops/ops_test.go`. The tests isolate everything: a temp `XDG_CONFIG_HOME`, a temp `GIT_CONFIG_GLOBAL`, and a temp SSH folder.
5. Add the Wails binding in `cmd/gui/app.go` and call it from the frontend through `cmd/gui/frontend/src/lib/bridge.ts`.

---

## Config schema evolution

When adding new fields to the configuration:

1. Add the field to the right struct in `pkg/config/config.go`: `Account` for identity and credentials, `Source` for what to clone, `Repo` for per-repo overrides. Optional fields use `omitempty`. Keys are snake_case, except fields that mirror another tool's names (`useHttpPath` stays camelCase to match GCM).
2. Add the field to `json/gitbox.schema.json` with a clear description.
3. Add an example to `json/gitbox.jsonc`.
4. If there are CRUD implications, update `pkg/config/crud.go`.
5. Update the [config reference](architecture.md#4-config-format-v5) in `docs/architecture.md` and `docs/es/architecture.md`.
6. Add tests for the new field in `pkg/config/config_test.go`.

**Additive changes never bump `config.CurrentVersion`.** The current version grows with optional fields. Only a breaking change (a rename, removal, or type change) bumps it, and then:

1. Raise `CurrentVersion` in `pkg/config/config.go`.
2. Add the migration step to `decode` in `pkg/config/load.go`, gated on the version read from the file (`from < N`), and list it in `decode`'s doc comment. `decode` backs both `Parse` and `LoadWithRepair`, so every loader migrates the same way, in memory; the next save writes the new version.
3. Update the `version` constant in `json/gitbox.schema.json`, `json/gitbox.jsonc`, and `json/test-gitbox.json.example`.
4. Add a migration test (`pkg/config/migrate_v<N>_test.go`) that loads a file at the previous version.
5. Update the versioning notes in [architecture.md](architecture.md#4-config-format-v5) and `ai/context/config-format.md`.

The backup ring always keeps a copy of a file written at an older version, so a user can go back to a release that still reads it.

---

## Release process

### Versioning

Local builds detect the version from git tags at runtime. CI builds inject explicit values via ldflags:

```bash
# CI build with explicit version (full SHA is truncated to 7 chars at runtime)
cd cmd/gui && wails build -ldflags "-X main.version=v2.0.0 -X main.commit=$(git rev-parse HEAD)"

# Local builds without ldflags run:
#   git describe --tags --always   → version, with "-dev" appended
#   git rev-parse --short HEAD     → commit SHA (e.g., "a99cf17")
# Display format:
#   CI:             "v2.0.0 (abc1234)"
#   Local:          "v2.0.0-3-ga99cf17-dev (a99cf17)"
#   Outside a repo: "dev-none" (the app's working folder has no .git)
```

### Creating a release

I push a version tag and GitHub Actions builds the app for every platform and creates a draft GitHub Release with the assets attached. Then I sign the draft's checksums on my machine, which publishes it:

```bash
git tag v2.0.0
git push origin v2.0.0
# when the CI workflow finishes:
./scripts/sign-release.sh v2.0.0
```

CI injects `-ldflags "-X main.version=<tag> -X main.commit=<sha>"` into the GUI build. The signing key lives in my SSH agent and never reaches CI. See [release-signing.md](release-signing.md) for how signing works, the agent setup and how a fork uses its own key.

### Release lines

v2 is GUI-only. The CLI and TUI live on in 1.x, on the `release/v1` branch:

- `main` carries v2 and later. Tags look like `v2.y.z`.
- `release/v1` carries v1 maintenance. It takes critical and security fixes only, tagged `v1.7.z`.
- `scripts/sign-release.sh` publishes a release as GitHub's "latest" only when no release with a higher major version exists. A `v1.7.z` tag pushed after `v2.0.0` is therefore published with `--latest=false`.

The GUI updater follows the release GitHub marks as latest, so these rules decide what every installed GUI updates to. The v1 CLI caps itself at 1.x (`MaxMajor: 1`). [architecture.md](architecture.md#pkgupdate--auto-update) describes how the updater checks, verifies and installs a release.

### Continuous integration

Two GitHub Actions workflows run:

- `.github/workflows/pr.yml` runs on pull requests and on pushes to `main`: the Linux `check` job runs `scripts/health.sh` as the gate (every checker; any finding fails the job), then a `wails build` and a `--version` smoke test. In parallel, `native-tests` runs short Go tests for config, status, credential, update, Git, workspace and GUI bindings on Windows and macOS.
- `.github/workflows/ci.yml` runs on version tags: it builds the app on each platform, packages the installers, and creates the GitHub Release as a draft for `scripts/sign-release.sh` to sign and publish. Before tagging a change that only the release path exercises, I run it as a dry run with `gh workflow run ci.yml -f version=v0.0.0`: every build and installer job runs, and the release job is skipped.

The native jobs build the embedded frontend first, use Go from `go.mod`, and cache Go and npm dependencies per OS. See [Native PR tests](testing.md#native-pr-tests) for the command, Windows runner settings and the manual required-check rollout after the first green hosted run. These native jobs do not launch the GUI; I keep packaged-launch smoke tests in `scripts/smoke.sh`.

### Release assets

Each release produces the following artifacts:

| Asset                        | Contents                                                                                       |
| ---------------------------- | ---------------------------------------------------------------------------------------------- |
| `gitbox-win-amd64.zip`       | `GitboxApp.exe`                                                                                |
| `gitbox-win-amd64-setup.exe` | Windows Inno Setup installer (`GitboxApp.exe`, Start Menu, no PATH; removes a v1 `gitbox.exe`) |
| `gitbox-macos-arm64.zip`     | `GitboxApp.app`                                                                                |
| `gitbox-macos-arm64.dmg`     | macOS disk image with bundled installer                                                        |
| `gitbox-macos-amd64.zip`     | `GitboxApp.app`                                                                                |
| `gitbox-macos-amd64.dmg`     | macOS disk image with bundled installer                                                        |
| `gitbox-linux-amd64.zip`     | `GitboxApp`                                                                                    |
| `gitbox-x86_64.AppImage`     | Self-contained Linux app (bundles GTK 3 and WebKitGTK)                                         |
| `checksums.sha256`           | SHA256 hashes for all artifacts                                                                |
| `checksums.sha256.sig`       | SSH signature over the tag and `checksums.sha256`, added by `scripts/sign-release.sh`          |

The updater and the bootstrap script look these assets up by name, including from v1 installs, so I never rename them.

The Windows installer is built with Inno Setup (`scripts/installer.iss`). It installs only `GitboxApp.exe` and adds nothing to PATH; when it upgrades a v1 install, it removes the old `gitbox.exe` and its PATH entry. macOS DMGs are built with `create-dmg` and include a bundled `Install Gitbox.command` script (`scripts/dmg/`) that copies `GitboxApp.app` to `/Applications/` and removes quarantine flags. The Linux AppImage is built by `scripts/appimage/build-appimage.sh`, which uses linuxdeploy and its GTK plugin to bundle GTK 3, WebKitGTK and the WebKit helper processes, so the AppImage runs on systems without those libraries installed. The Linux GUI and the AppImage build on the `ubuntu-22.04` runner on purpose: the bundled libraries then need no newer glibc than 2.35, which keeps the AppImage working on older distributions.

### macOS code signing

macOS DMGs are **unsigned** for now. The CI workflow has code signing and notarization steps, gated on the `APPLE_CERTIFICATE` secret. See [macos-signing.md](macos-signing.md) for setup instructions. Until signing is configured, the DMG's bundled "Install Gitbox" script removes the quarantine flag, and users can also install with the bootstrap script or the ZIP downloads.

---

## Feature lifecycle

I track the backlog on GitHub at [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues). Features use the `enhancement` label (plus `priority:P1` for next-ups); bugs use the `bug` label. Size and severity live in the issue body so the label set stays minimal.

The workflow:

1. **Capture** — open an issue with a short title and a body describing the concept and any codebase notes I'd want a future agent session to have.
2. **Plan** — discuss in comments, then plan in Claude Code or Codex to design a file-by-file implementation.
3. **Build** — implement the plan, then verify with `/test-plan`.
4. **Ship** — reference the issue in the commit message (e.g. `Closes #22`) so it auto-closes on push.

I review the backlog from the terminal with `gh issue list --label enhancement` or `gh issue view <n>`, after `gh auth switch --user LuisPalacios`.

### Push to main vs branch + PR

I pick per task and default to branch + PR when in doubt: a PR costs little, a bad push to main costs a revert.

**Push directly to main** for one-file, mechanically obvious changes: a typo, a one-line bug fix, a doc tweak. `go vet ./...` and focused tests must pass locally. The commit message references the issue with `Closes #N` so GitHub closes it on push.

**Branch + PR** for everything else: multi-file features, `pkg/` public-surface changes, refactors, UI work — anything that benefits from a full diff view or from letting CI gate the merge. Branch names follow `<type>/<issue>-<slug>`, e.g. `fix/31-ide-flash` or `feat/22-open-in-terminal`. The PR body closes the issue with `Closes #N`; I self-approve and merge immediately.

External contributions always come through PRs from forks — I review, CI must pass, then merge.

---

## Logo and app icons

The source of truth for the logo is `assets/logo.svg`. The derived icon files used by the Wails build live alongside it:

| File                 | Format                    | Purpose                                                                        |
| -------------------- | ------------------------- | ------------------------------------------------------------------------------ |
| `assets/logo.svg`    | SVG                       | Source file, editable in [Boxy SVG](https://boxy-svg.com/) (Windows/macOS app) |
| `assets/appicon.png` | 1024x1024 PNG             | macOS `.app` bundle icon, Linux desktop icon                                   |
| `assets/icon.ico`    | ICO (256/128/64/48/32/16) | Windows executable icon                                                        |

### Editing the logo

1. Open `assets/logo.svg` in [Boxy SVG](https://boxy-svg.com/) (available as a desktop app for Windows and macOS).
2. Edit the design.
3. Export to PNG 1024x1024 — Boxy SVG has this configured in the SVG's `<bx:export>` metadata. Save as `assets/appicon.png`.
4. Convert PNG to ICO using [icoconverter.com](https://www.icoconverter.com/) — select all 6 sizes (256, 128, 64, 48, 32, 16). Save as `assets/icon.ico`.
5. Copy the icons into `cmd/gui/build/` and run `wails build`, as in [Building from source](#building-from-source).

### Build-time icon flow

The Wails build reads icons from `cmd/gui/build/`:

- `cmd/gui/build/appicon.png` — used by Wails for all platforms
- `cmd/gui/build/windows/icon.ico` — embedded in the Windows `.exe`

These are **not checked in** (gitignored under `cmd/gui/build/`). The CI workflow and local builds copy them from `assets/` before running `wails build`.

### Tool logos and top-bar icons

The tool logos on each repo row and the top-bar icons come from SVG originals in `assets/svg/`, not from code. I replace a file there and rebuild, and the app picks it up. Under `wails dev` the window reloads on its own.

- `assets/svg/brand/logo-<name>.svg` — colour original, shown as drawn in the light theme
- `assets/svg/brand/logo-<name>-dark.svg` — single-colour version for the dark theme, repainted with the theme's text colour
- `assets/svg/ui/<name>.svg` — single-colour top-bar icon, repainted the same way

The Vite plugin `cmd/gui/frontend/vite-plugin-icons.ts` does the conversion on every frontend build. It cleans each file with svgo (removes the top-bar icons' hidden guide box and editor metadata, drops fixed sizes) and exposes the result as the `virtual:icons` module that `BrandIcon.svelte` and `UIIcon.svelte` read. A single-colour file with a gradient, filter, mask, pattern or image fails the build with its file name. [`assets/svg/README.md`](../assets/svg/README.md) has the naming and drawing rules.

To add a new tool logo I use the `preparing-brand-logos` skill. It downloads candidate sources into the gitignored `tmp/` folder, fits each one into the 150×150 template with `scripts/fit_logo.py`, and writes `tmp/preview.html` with every logo on light and dark backgrounds at real sizes. I review and edit the files there, and only then move them into `assets/svg/brand/`.

---

## README screenshots

I take the README screenshots from the real app running on a fake fleet, so they never show my own accounts or paths. The kit lives in `scripts/demo-fleet/`:

- `build.sh` creates four demo accounts (Forgejo, personal GitHub, corporate GitHub, and a GitLab account whose credential is broken) with real local clones in chosen states (up to date, behind, ahead, dirty, diverged, not cloned), a `gitbox.json`, token files, and an isolated global git config under `$TEMP/gbdemo`.
- `mock.py` fakes the provider APIs on `127.0.0.1:3001-3004`, so credentials verify, PR and review badges appear, and fetches work against the demo upstreams. Port 3004 rejects every request, so the GitLab account shows in Needs attention with its fix action. It needs [uv](https://docs.astral.sh/uv/).
- `run.sh` starts the mock and launches `GitboxApp` with `XDG_CONFIG_HOME` and `GIT_CONFIG_GLOBAL` pointing at the demo, so my real config, `~/.gitconfig`, and credential store stay untouched. Close any running GitboxApp first.
- `shot.ps1` (Windows) resizes the window, clicks points inside it, and captures it to a PNG.

```bash
./scripts/demo-fleet/build.sh
./scripts/demo-fleet/run.sh &
pwsh scripts/demo-fleet/shot.ps1 -Out assets/screenshot-gui.png -Width 1360 -Height 1300
```

The README uses `assets/screenshot-gui.png` (Simple mode) and `assets/screenshot-advanced.png`, and the GUI guide uses `screenshot-advanced.png`, `screenshot-menu.png`, and `screenshot-compact.png`, all in the light theme. `screenshot-gui.png` is also the AppStream screenshot for the Linux AppImage. Settings → Terminals lists the host's real terminals and paths, so I keep that screen out of shots.

---

## Code style

- Go code passes the [health check](#health-check): `gofmt -s`, `go vet`, staticcheck, modernize and deadcode, with no findings. I run `gofmt -s -w` on changed files before committing.
- Frontend code passes `svelte-check` with no unused locals or parameters, and builds without Svelte warnings.
- Shell scripts pass shellcheck, workflows pass actionlint, and Markdown passes markdownlint with the `fixing-markdown` config.
- Error messages are lowercase, with no trailing punctuation.
- Exported functions have doc comments.
- Operations the GUI may cancel take a `context.Context`.
