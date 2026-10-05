# Developer guide

## Prerequisites

- **Go** 1.26+ — [install](https://go.dev/doc/install)
- **Node.js** 20.19+ or 22.12+ — [install](https://nodejs.org/) (Svelte 5 + Vite 8 frontend; CI builds on Node 24)
- **Git** 2.39+
- **Wails CLI** v2 — `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- **Platform-specific:** Windows needs Git for Windows; macOS needs Xcode CLI Tools (`xcode-select --install`); Linux needs `libwebkit2gtk-4.1-dev` and `libgtk-3-dev`

For multiplatform testing via SSH, see [multiplatform.md](multiplatform.md).

---

## Building from source

gitbox v2 builds one binary, the `GitboxApp` desktop app. The CLI and TUI live only on the `release/v1` branch.

```bash
# Build-time version stamp. Without it the app falls back to `git describe` at runtime.
LDFLAGS="-X main.version=$(git describe --tags --always)-dev -X main.commit=$(git rev-parse --short HEAD)"

# Copy app icons from assets/ into the Wails build directory (they are not checked in there)
cp assets/appicon.png cmd/gui/build/appicon.png
cp assets/icon.ico    cmd/gui/build/windows/icon.ico   # Windows only

# Development mode (hot reload)
cd cmd/gui
wails dev

# Production build
# macOS only: target macOS 13, the Go runtime's minimum (Wails defaults to 10.13)
export CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0
wails build -ldflags "$LDFLAGS"
# Output: cmd/gui/build/bin/GitboxApp[.exe]
```

`wails build` cannot cross-compile the GUI: each target needs its host's native webview (WebView2 on Windows, WebKit on macOS, WebKitGTK on Linux). To build for another platform, build on that platform — [multiplatform.md](multiplatform.md) shows how `scripts/ship.sh` does it over SSH.

The Go code embeds the compiled frontend from `cmd/gui/frontend/dist`. Plain Go commands such as `go vet ./...`, `go test ./...`, or a quick compile check (`go build -o /dev/null ./cmd/gui`) need that folder to exist. `wails build` creates it; to create it without a full build:

```bash
cd cmd/gui/frontend
npm ci
npm run build
```

Once the app is built, `GitboxApp --version` prints the version and exits.

### Key design decisions

- **`pkg/` is the heart** — all business logic lives in `pkg/`. `cmd/gui` only holds Wails bindings, locking, events, and GUI-only concerns.
- **`pkg/ops` is the service layer** — operations that span several packages (account lifecycle, credential switching, clone planning, discovery) live there, so the GUI bindings stay thin and the logic stays testable without Wails.
- **GUI calls Go directly** — Wails bindings expose Go methods to Svelte. No subprocess spawning of gitbox itself.
- **Git operations use `os/exec`** — we shell out to the system `git` binary, not libgit2.
- **Provider APIs use `net/http`** — standard Go, no external HTTP client dependencies.
- **Accounts (WHO) + Sources (WHAT)** — accounts define identity on a server (hostname, username, credentials); sources reference an account and contain the list of repos to manage. This separation allows multiple sources to share the same account.
- **Account uniqueness** — an account is unique by `(hostname, username)`.
- **Repo keys use `org/repo` format** — this produces a 3-level folder structure: `<source>/<org>/<repo>`. The `id_folder` field overrides the 2nd level (org), and `clone_folder` overrides the 3rd level (or replaces the entire path when absolute).
- **Credential inheritance** — accounts have a `default_credential_type`; repos inherit it unless they set their own `credential_type`.
- **No console flash on Windows** — every `exec.Command` in `cmd/gui/` calls `git.HideWindow(cmd)` before it runs.
- **Version auto-detection** — local builds run `git describe --tags --always` at runtime; CI builds inject version and commit via ldflags.

---

## Adding a new provider

> Providers are implemented in `pkg/provider/`. GitHub, GitLab, Gitea/Forgejo, and Bitbucket are all functional. To add a new provider:

1. Create `pkg/provider/newprovider.go`:

```go
package provider

import "context"

type NewProvider struct{}

// Required: Provider interface
func (p *NewProvider) ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error) {
    // Implement paginated API call to list repositories
}

// Optional: RepoCreator interface — enables repo creation from the GUI
func (p *NewProvider) CreateRepo(ctx context.Context, baseURL, token, username, owner, repoName, description string, private bool) error {
    // If owner is empty, create under the user's personal namespace.
    // If owner is non-empty, create under that organization.
}

func (p *NewProvider) RepoExists(ctx context.Context, baseURL, token, username, owner, repoName string) (bool, error) {
    // Check if a repo exists (used by mirror setup)
}

// Optional: OrgLister interface — enables the owner dropdown in "Create repo"
func (p *NewProvider) ListUserOrgs(ctx context.Context, baseURL, token, username string) ([]string, error) {
    // Return organization names the user belongs to
}

// Optional: PushMirrorProvider, PullMirrorProvider, RepoInfoProvider
// See existing implementations for examples.
```

1. Register the provider in `pkg/provider/provider.go` (once the interface and factory are defined):

```go
func NewFromConfig(acct *config.Account) (Provider, error) {
    switch acct.Provider {
    case "github":
        return &GitHub{...}, nil
    case "newprovider":
        return &NewProvider{...}, nil
    // ...
    }
}
```

1. Add `"newprovider"` to the `provider` enum in `json/gitbox.schema.json`.

2. Write tests in `pkg/provider/newprovider_test.go`.

---

## Adding an operation to pkg/ops

When a new GUI action touches more than one package — config plus clones on disk, credentials plus every clone of an account — I put the logic in `pkg/ops` and keep the Wails binding thin:

1. Add the function to the matching file in `pkg/ops/` (`account.go`, `credential.go`, `clone.go`, or `discover.go`). It takes a `*config.Config` and works on the files it owns on disk.
2. Don't save or lock inside `pkg/ops`. The caller (`cmd/gui/app.go`) takes the config lock, calls the operation, saves the config, and emits events.
3. Keep slow per-clone work (like `ReconfigureClones`) in a separate function, so the caller can run it after a successful save.
4. Add a unit test in `pkg/ops/ops_test.go`. The tests isolate everything: a temp `XDG_CONFIG_HOME`, a temp `GIT_CONFIG_GLOBAL`, and a temp SSH folder.
5. Add the Wails binding in `cmd/gui/app.go` and call it from the frontend through `cmd/gui/frontend/src/lib/bridge.ts`.

---

## Testing

Quick start:

```bash
go test -short ./...    # unit tests (no setup needed beyond the frontend dist folder)
go test ./...           # everything (needs test-gitbox.json for the pkg/ops scenario)
```

Activate the pre-push hook once per clone: `git config core.hooksPath .githooks` — it runs a `gofmt -s` check, `go vet` and unit tests before every push.

### Health check

Before opening a PR I run every code checker in one pass:

```bash
./scripts/health.sh              # every check, one summary line each
./scripts/health.sh go svelte    # only some checks ("go" = all Go checks)
./scripts/health.sh -v           # every finding instead of the first 15
```

It is read-only and exits non-zero when any check reports a finding. Go checks: `gofmt -s`, `go vet`, staticcheck, modernize, govulncheck, deadcode (with an allowlist for functions kept on purpose) and `go test -short`. Frontend checks: `svelte-check` (unused locals and parameters fail through `noUnusedLocals`/`noUnusedParameters`), warnings printed by `npm run build` (the same Svelte diagnostics CI shows in its build step), install warnings from a clean `npm ci` in a scratch copy (deprecated packages, install scripts not covered by `allowScripts`), and `npm audit`. The `npm ci` check needs npm 11.19 or newer. Repo checks: shellcheck on the shell scripts, actionlint on the workflows, and markdownlint with the `fixing-markdown` skill config forced read-only.

The Go analyzers are installed into a temporary folder at versions pinned at the top of the script, so they need no install; vet, staticcheck and modernize analyze linux, darwin and windows on every run, so platform-specific files are checked from any host. The other three tools must be on `PATH`:

```bash
scoop install shellcheck actionlint     # Windows (brew install … on macOS, apt/dnf on Linux)
npm install -g markdownlint-cli2
```

The PR workflow runs the same script as its gate: any finding fails the job, including a new advisory reported by govulncheck or `npm audit`. CI pins shellcheck 0.11.0, actionlint 1.7.12 and markdownlint-cli2 0.23.3; use those versions locally so both runs agree.

For the full testing workflow (fixture setup, integration tests, pre-PR and release checklists), see [testing.md](testing.md). For multiplatform testing via SSH, see [multiplatform.md](multiplatform.md). If you use Claude Code, `/test-plan` automates the pre-PR checks.

---

## Config schema evolution

When adding new fields to the configuration:

1. Add the field to the appropriate Go struct in `pkg/config/config.go` — use `json:"fieldName,omitempty"` with the correct casing (e.g., `useHttpPath` is camelCase to match GCM conventions)
2. Add the field to `json/gitbox.schema.json` with a clear description
3. Update `json/gitbox.jsonc` with an example
4. If the field belongs to an account vs a source, ensure it's in the right struct (`Account` for identity/credentials, `Source` for what to clone, `Repo` for per-repo overrides)
5. If there are CRUD implications, update `pkg/config/crud.go`
6. Update the config reference tables in `docs/architecture.md` and `docs/es/architecture.md`
7. Add tests for the new field in `pkg/config/config_test.go`

**Never bump the version number for additive changes.** Version 3 can grow with optional fields. Only bump to version 4 if breaking changes are needed (renames, removals, type changes).

---

## Release process

### Versioning

Version is **auto-detected from git tags** at runtime for local builds. CI builds inject explicit values via ldflags:

```bash
# CI build with explicit version (full SHA is truncated to 7 chars at runtime)
cd cmd/gui && wails build -ldflags "-X main.version=v2.0.0 -X main.commit=$(git rev-parse HEAD)"

# Local builds auto-detect by running:
#   git describe --tags --always   → version (e.g., "v1.2.11")
#   git rev-parse --short HEAD     → commit SHA (e.g., "a99cf17")
# Display format:
#   CI:    "v2.0.0 (abc1234)"
#   Local: "v1.2.11-dev (a99cf17)"
#   No tags: "dev-a99cf17"
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

### Continuous integration

Two GitHub Actions workflows run:

- `.github/workflows/pr.yml` runs on pull requests and on pushes to `main`: `scripts/health.sh` as the gate (every checker; any finding fails the job), then a Linux `wails build` and a `--version` smoke test.
- `.github/workflows/ci.yml` runs on version tags: it builds the app on each platform, packages the installers, and creates the GitHub Release as a draft for `scripts/sign-release.sh` to sign and publish. Before tagging a change that only the release path exercises, I run it as a dry run with `gh workflow run ci.yml -f version=v0.0.0`: every build and installer job runs, and the release job is skipped.

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

The asset names match v1, so the updater and the bootstrap script find them the same way. v2 drops `gitbox-win-arm64.zip`, which only carried the CLI.

The Windows installer is built with Inno Setup (`scripts/installer.iss`). It installs only `GitboxApp.exe` and adds nothing to PATH; when it upgrades a v1 install, it removes the old `gitbox.exe` and its PATH entry. macOS DMGs are built with `create-dmg` and include a bundled `Install Gitbox.command` script (`scripts/dmg/`) that copies `GitboxApp.app` to `/Applications/` and removes quarantine flags. The Linux AppImage is built by `scripts/appimage/build-appimage.sh`, which uses linuxdeploy and its GTK plugin to bundle GTK 3, WebKitGTK and the WebKit helper processes, so the AppImage runs on systems without those libraries installed. The Linux GUI and the AppImage build on the `ubuntu-22.04` runner on purpose: the bundled libraries then need no newer glibc than 2.35, which keeps the AppImage working on older distributions.

### macOS code signing

macOS DMGs are currently **unsigned**. Code signing and notarization steps are present in the CI workflow but gated on the `APPLE_CERTIFICATE` secret. See [macos-signing.md](macos-signing.md) for setup instructions. Until signing is configured, the DMG includes a bundled "Install Gitbox" script that handles quarantine removal automatically. Users can also use the bootstrap script or ZIP downloads.

### Auto-update

The `pkg/update/` package provides version checking and self-update capabilities. The GUI runs a background check once per day and shows an update pill in the footer. The updater checks that the release's `checksums.sha256` carries a valid signature by the release key compiled into the binary, then downloads the platform-specific artifact, verifies its SHA256 checksum, and replaces the app in place.

The GUI and the v1 CLI update differently:

- The GUI follows the release GitHub marks as latest, including the jump from 1.x to 2.x. It replaces only what is already installed next to it; on macOS it replaces the whole `GitboxApp.app` bundle in the folder that holds it.
- The v1 CLI, maintained on `release/v1`, stays on the 1.x line (`MaxMajor: 1`) and replaces only its own binary, so it never downgrades a v2 GUI installed next to it. It keeps its own throttle file (`.update-check-cli`).

### Release lines

v2 is GUI-only. The CLI and TUI live on in 1.x, on the `release/v1` branch:

- `main` carries v2 and later. Tags look like `v2.y.z`.
- `release/v1` carries v1 maintenance. It takes critical and security fixes only, tagged `v1.7.z`.
- `scripts/sign-release.sh` publishes a release as GitHub's "latest" only when no release with a higher major version exists. A `v1.7.z` tag pushed after `v2.0.0` is therefore published with `--latest=false`, and v2 GUIs never see it as an update.

---

## Feature lifecycle

I track the backlog on GitHub at [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues). Features use the `enhancement` label (plus `priority:P1` for next-ups); bugs use the `bug` label. Size and severity live in the issue body so the label set stays minimal.

The workflow:

1. **Capture** — open an issue with a short title and a body describing the concept and any codebase notes I'd want a future Claude session to have.
2. **Plan** — discuss in comments, then enter plan mode in Claude Code to design a file-by-file implementation.
3. **Build** — implement the plan, then verify with `/test-plan`.
4. **Ship** — reference the issue in the commit message (e.g. `Closes #22`) so it auto-closes on push.

Use `gh issue list --label enhancement` or `gh issue view <n>` to review the radar from the terminal (run `gh auth switch --user LuisPalacios` first).

### Push to main vs branch + PR

I pick per task. Default to branch + PR when in doubt — the cost of a PR is trivial, the cost of a bad push to main is a revert.

**Push directly to main** for one-file, mechanically-obvious changes: a typo, a one-line bug fix, a doc tweak. `go vet ./...` and focused tests must pass locally. Reference the issue with `Closes #N` in the commit message so GitHub auto-closes on push.

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

1. Open `assets/logo.svg` in [Boxy SVG](https://boxy-svg.com/) (available as a desktop app for Windows and macOS)
2. Edit the design
3. Export to PNG 1024x1024 — Boxy SVG has this configured in the SVG's `<bx:export>` metadata. Save as `assets/appicon.png`
4. Convert PNG to ICO using [icoconverter.com](https://www.icoconverter.com/) — select all 6 sizes (256, 128, 64, 48, 32, 16). Save as `assets/icon.ico`
5. Run `wails build` from `cmd/gui/` — the build copies icons from `assets/` automatically

### Build-time icon flow

The Wails build reads icons from `cmd/gui/build/`:

- `cmd/gui/build/appicon.png` — used by Wails for all platforms
- `cmd/gui/build/windows/icon.ico` — embedded in the Windows `.exe`

These are **not checked in** (gitignored under `cmd/gui/build/`). Instead, the CI workflow and local builds copy them from `assets/` before running `wails build`.

---

## README screenshots

I take the README screenshots from the real app running on a fake fleet, so they never show my own accounts or paths. The kit lives in `scripts/demo-fleet/`:

- `build.sh` creates three demo accounts (Forgejo, personal GitHub, corporate GitHub) with real local clones in chosen states (synced, behind, ahead, dirty, not cloned), a `gitbox.json`, token files, and an isolated global git config under `$TEMP/gbdemo`.
- `mock.py` fakes the provider APIs on `127.0.0.1:3001-3003`, so credentials verify, PR and review badges appear, and fetches work against the demo upstreams. It needs [uv](https://docs.astral.sh/uv/).
- `run.sh` starts the mock and launches `GitboxApp` with `XDG_CONFIG_HOME` and `GIT_CONFIG_GLOBAL` pointing at the demo, so my real config, `~/.gitconfig`, and credential store stay untouched. Close any running GitboxApp first.
- `shot.ps1` (Windows) resizes the window, clicks points inside it, and captures it to a PNG.

```bash
./scripts/demo-fleet/build.sh
./scripts/demo-fleet/run.sh &
pwsh scripts/demo-fleet/shot.ps1 -Out assets/screenshot-gui.png -Width 1360 -Height 1300
```

The README uses `assets/screenshot-gui.png` (light), `assets/screenshot-dark.png`, `assets/screenshot-menu.png`, and `assets/screenshot-compact.png`. `screenshot-gui.png` is also the AppStream screenshot for the Linux AppImage. Settings → Terminals lists the host's real terminals and paths, so keep that screen out of shots.

---

## Code style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use `golangci-lint` if available
- Error messages should be lowercase, no trailing punctuation
- Exported functions need doc comments
- Use `context.Context` for operations that may be cancelled (GUI async ops)
