# Developer guide

## Prerequisites

- **Go** 1.26+ — [install](https://go.dev/doc/install)
- **Node.js** 20+ — [install](https://nodejs.org/) (for Svelte frontend)
- **Git** 2.39+
- **Wails CLI** v2 — `go install github.com/wailsapp/wails/v2/cmd/wails@latest` (GUI builds only)
- **Platform-specific:** Windows needs Git for Windows; macOS needs Xcode CLI Tools (`xcode-select --install`); Linux needs `libwebkit2gtk-4.1-dev` and `libgtk-3-dev`

For multiplatform testing via SSH, see [multiplatform.md](multiplatform.md).

---

## Building from source

### CLI only

```bash
# From the repository root
go build -o build/gitbox ./cmd/cli

# Cross-compile for other platforms
GOOS=linux  GOARCH=amd64 go build -o build/gitbox-linux-amd64  ./cmd/cli
GOOS=darwin GOARCH=arm64 go build -o build/gitbox-darwin-arm64 ./cmd/cli
GOOS=windows GOARCH=amd64 go build -o build/gitbox.exe         ./cmd/cli
```

### GUI (Wails)

```bash
# Copy app icons from assets/ into the Wails build directory
cp assets/appicon.png cmd/gui/build/appicon.png
cp assets/icon.ico    cmd/gui/build/windows/icon.ico   # Windows only

# Development mode (hot reload)
cd cmd/gui
wails dev

# Production build
wails build
# Output: cmd/gui/build/bin/GitboxApp[.exe]
```

### Key design decisions

- **`pkg/` is the heart** — both CLI and GUI import from here. All business logic lives in `pkg/`.
- **CLI is a thin wrapper** — `cmd/cli/main.go` wires subcommands to `pkg/` functions.
- **GUI calls Go directly** — Wails bindings expose `pkg/` functions to Svelte. No subprocess spawning.
- **Git operations use `os/exec`** — we shell out to the system `git` binary, not libgit2.
- **Provider APIs use `net/http`** — standard Go, no external HTTP client dependencies.
- **Accounts (WHO) + Sources (WHAT)** — accounts define identity on a server (hostname, username, credentials); sources reference an account and contain the list of repos to manage. This separation allows multiple sources to share the same account.
- **Account uniqueness** — an account is unique by `(hostname, username)`.
- **Repo keys use `org/repo` format** — this produces a 3-level folder structure: `<source>/<org>/<repo>`. The `id_folder` field overrides the 2nd level (org), and `clone_folder` overrides the 3rd level (or replaces the entire path when absolute).
- **Credential inheritance** — accounts have a `default_credential_type`; repos inherit it unless they set their own `credential_type`.
- **CLI uses Cobra** — each subcommand lives in its own `*_cmd.go` file, registered in `main.go`'s `init()`.
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

// Optional: RepoCreator interface — enables repo creation from GUI/CLI
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

1. Write tests in `pkg/provider/newprovider_test.go`.

---

## Adding a new CLI subcommand

Each command lives in its own file following the `*_cmd.go` naming convention. Here's the pattern used throughout the codebase:

1. Create `cmd/cli/newcommand_cmd.go`:

```go
package main

import (
    "fmt"

    "github.com/LuisPalacios/gitbox/pkg/config"
    "github.com/spf13/cobra"
)

var newcommandCmd = &cobra.Command{
    Use:   "newcommand",
    Short: "Description of the new command",
}

var newcommandListCmd = &cobra.Command{
    Use:   "list",
    Short: "List something",
    RunE: func(cmd *cobra.Command, args []string) error {
        cfg, err := loadConfig()
        if err != nil {
            return err
        }
        // Call pkg/ functions using cfg
        fmt.Println("done")
        return nil
    },
}

func init() {
    newcommandCmd.AddCommand(newcommandListCmd)
}
```

1. Register the parent command in `main.go`'s `init()`:

```go
rootCmd.AddCommand(newcommandCmd)
```

---

## Testing

Quick start:

```bash
go test -short ./...    # unit tests (no setup needed)
go test ./...           # everything (needs test-gitbox.json for integration tests)
```

Activate the pre-push hook once per clone: `git config core.hooksPath .githooks` — it runs `go vet` + unit tests before every push.

For the full testing workflow (fixture setup, integration tests, pre-PR and release checklists), see [testing.md](testing.md). For multiplatform testing via SSH, see [multiplatform.md](multiplatform.md). If you use Claude Code, `/test-plan` automates the pre-PR checks.

---

## Config schema evolution

When adding new fields to the configuration:

1. Add the field to the appropriate Go struct in `pkg/config/config.go` — use `json:"fieldName,omitempty"` with the correct casing (e.g., `useHttpPath` is camelCase to match GCM conventions)
2. Add the field to `json/gitbox.schema.json` with a clear description
3. Update `json/gitbox.jsonc` with an example
4. If the field belongs to an account vs a source, ensure it's in the right struct (`Account` for identity/credentials, `Source` for what to clone, `Repo` for per-repo overrides)
5. If there are CRUD implications, update `pkg/config/crud.go`
6. Update `docs/reference.md` config reference table
7. Add tests for the new field in `pkg/config/config_test.go`

**Never bump the version number for additive changes.** Version 2 can grow with optional fields. Only bump to version 3 if breaking changes are needed (renames, removals, type changes).

---

## Release process

### Versioning

Version is **auto-detected from git tags** at runtime for local builds. CI builds inject explicit values via ldflags:

```bash
# CI build with explicit version (full SHA is truncated to 7 chars at runtime)
go build -ldflags "-X main.version=v0.2.0 -X main.commit=$(git rev-parse HEAD)" -o build/gitbox ./cmd/cli

# Local builds auto-detect by running:
#   git describe --tags --always   → version (e.g., "v1.2.11")
#   git rev-parse --short HEAD     → commit SHA (e.g., "a99cf17")
# Display format:
#   CI:    "v0.2.0 (abc1234)"
#   Local: "v1.2.11-dev (a99cf17)"
#   No tags: "dev-a99cf17"
```

### Creating a release

Releases are fully automated via CI. Push a version tag and GitHub Actions builds all binaries, creates a GitHub Release, and attaches the assets:

```bash
git tag v1.0.0
git push origin v1.0.0
```

CI injects `-ldflags "-X main.version=<tag> -X main.commit=<sha>"` into both CLI and GUI builds.

### Release assets

Each release produces the following artifacts:

| Asset | Contents |
| --- | --- |
| `gitbox-win-amd64.zip` | `gitbox.exe` + `GitboxApp.exe` |
| `gitbox-win-amd64-setup.exe` | Windows Inno Setup installer (PATH, Start Menu) |
| `gitbox-macos-arm64.zip` | `gitbox` + `GitboxApp.app` |
| `gitbox-macos-arm64.dmg` | macOS disk image with bundled installer |
| `gitbox-macos-amd64.zip` | `gitbox` + `GitboxApp.app` |
| `gitbox-macos-amd64.dmg` | macOS disk image with bundled installer |
| `gitbox-linux-amd64.zip` | `gitbox` + `GitboxApp` |
| `gitbox-linux-amd64.AppImage` | Self-contained Linux app (CLI + GUI) |
| `checksums.sha256` | SHA256 hashes for all artifacts |

The Windows installer is built with Inno Setup (`scripts/installer.iss`). macOS DMGs are built with `create-dmg` and include a bundled `Install Gitbox.command` script (`scripts/dmg/`) that copies binaries and removes quarantine flags. The Linux AppImage is built with `appimagetool` using the support files in `scripts/appimage/`.

### macOS code signing

macOS DMGs are currently **unsigned**. Code signing and notarization steps are present in the CI workflow but gated on the `APPLE_CERTIFICATE` secret. See [macos-signing.md](macos-signing.md) for setup instructions. Until signing is configured, the DMG includes a bundled "Install Gitbox" script that handles quarantine removal automatically. Users can also use the bootstrap script or ZIP downloads.

### Auto-update

The `pkg/update/` package provides version checking and self-update capabilities. Both the CLI (`gitbox update`) and GUI (background check + banner) use it. The updater downloads the platform-specific artifact from GitHub Releases, verifies the SHA256 checksum, and replaces the binaries in place.

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

| File                 | Format                    | Purpose                                                                          |
| -------------------- | ------------------------- | -------------------------------------------------------------------------------- |
| `assets/logo.svg`    | SVG                       | Source file, editable in [Boxy SVG](https://boxy-svg.com/) (Windows/macOS app)   |
| `assets/appicon.png` | 1024x1024 PNG             | macOS `.app` bundle icon, Linux desktop icon                                     |
| `assets/icon.ico`    | ICO (256/128/64/48/32/16) | Windows executable icon                                                          |

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

## TUI demo recordings (VHS)

Use [VHS](https://github.com/charmbracelet/vhs) (by the Charm team) to record terminal demo GIFs for the README and docs. VHS reads declarative `.tape` files and renders GIF/MP4/WebM output.

### Install VHS

```bash
# macOS
brew install charmbracelet/tap/vhs

# Windows (scoop)
scoop install charmbracelet/vhs/vhs

# Go install
go install github.com/charmbracelet/vhs@latest
```

VHS requires `ffmpeg` and `ttyd`. On first run it will prompt to install them.

### Recording a demo

1. Create a `.tape` file under `assets/` (e.g., `assets/demo-tui.tape`):

   ```text
   Output assets/demo-tui.gif

   Set Shell "bash"
   Set FontSize 14
   Set Width 1200
   Set Height 600
   Set Theme "Catppuccin Mocha"

   Type "gitbox"
   Enter
   Sleep 2s
   Type "j"
   Sleep 0.5s
   Type "j"
   Sleep 0.5s
   Enter
   Sleep 2s
   ```

2. Run it:

   ```bash
   vhs assets/demo-tui.tape
   ```

3. The output GIF is written to the path specified in the `Output` directive.

### Conventions

- Tape files live in `assets/` alongside the GUI prototypes
- Output GIFs also go in `assets/` (e.g., `assets/demo-tui.gif`)
- Use `Catppuccin Mocha` theme to match the TUI dark theme
- Keep recordings under 15 seconds for README embeds
- Add tape files to git, but `.gif`/`.mp4` outputs should be gitignored (regenerate on demand)

---

## Code style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use `golangci-lint` if available
- Error messages should be lowercase, no trailing punctuation
- Exported functions need doc comments
- Use `context.Context` for operations that may be cancelled (GUI async ops)
