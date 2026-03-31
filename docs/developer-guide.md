# Developer Guide

## Prerequisites

- **Go** 1.26+ ([install](https://go.dev/doc/install))
- **Node.js** 20+ ([install](https://nodejs.org/))
- **Git** 2.39+
- **Wails CLI** v2 (for GUI development):

  ```bash
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  ```

  After installing, ensure `$(go env GOPATH)/bin` is in your `PATH`. Add this to your shell profile (`~/.zshrc` on macOS, `~/.bashrc` on Linux) if `wails version` is not found:

  ```bash
  export PATH="$PATH:$(go env GOPATH)/bin"
  ```

Check:

```bash
go version            # Go compiler
wails version         # Wails CLI (go install github.com/wailsapp/wails/v2/cmd/wails@latest)
node --version        # Node.js (for Svelte frontend)
npm --version         # npm
git --version         # Git
```

### Platform-specific

- **Windows:** Git for Windows (provides Git Bash)
- **macOS:** Xcode Command Line Tools (`xcode-select --install`)
- **Linux:** `libwebkit2gtk-4.1-dev` and `libgtk-3-dev` (for GUI builds)

### Multiplatform testing (optional)

If you want to build and test on multiple platforms from your dev machine:

- **SSH client** with key-based auth to remote machines
- **jq** and **curl** on all machines (for credential setup scripts)

See [Multiplatform](multiplatform.md) for the full setup.

---

## Building from Source

### CLI Only

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

### Key Design Decisions

- **`pkg/` is the heart** — both CLI and GUI import from here. All business logic lives in `pkg/`.
- **CLI is a thin wrapper** — `cmd/cli/main.go` wires subcommands to `pkg/` functions.
- **GUI calls Go directly** — Wails bindings expose `pkg/` functions to Svelte. No subprocess spawning.
- **Git operations use `os/exec`** — we shell out to the system `git` binary, not libgit2.
- **Provider APIs use `net/http`** — standard Go, no external HTTP client dependencies.
- **Accounts (WHO) + Sources (WHAT)** — accounts define identity on a server (hostname, username, credentials); sources reference an account and contain the list of repos to manage. This separation allows multiple sources to share the same account.
- **Account uniqueness** — an account is unique by `(hostname, username)`. During v1→v2 migration, duplicate accounts are deduplicated automatically.
- **Repo keys use `org/repo` format** — this produces a 3-level folder structure: `<source>/<org>/<repo>`. The `id_folder` field overrides the 2nd level (org), and `clone_folder` overrides the 3rd level (or replaces the entire path when absolute).
- **Credential inheritance** — accounts have a `default_credential_type`; repos inherit it unless they set their own `credential_type`.
- **CLI uses Cobra** — each subcommand lives in its own `*_cmd.go` file, registered in `main.go`'s `init()`.
- **Version auto-detection** — local builds run `git describe --tags --always` at runtime; CI builds inject version and commit via ldflags.

---

## Adding a New Provider

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

1. Add `"newprovider"` to the `provider` enum in `gitbox.schema.json`.

1. Write tests in `pkg/provider/newprovider_test.go`.

---

## Adding a New CLI Subcommand

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

```bash
# Unit tests only (integration tests require test-gitbox.json)
go test -short ./...

# All tests including TUI and CLI
go test ./...

# Integration tests (requires test-gitbox.json)
go test -v -run Integration ./cmd/cli/...

# Full lifecycle scenario test
go test -v -run Scenario ./cmd/cli/...
```

There are three levels of testing, each building on the previous:

1. **Unit tests** (no setup needed): `go test -short ./...`
2. **Integration tests** (requires `test-gitbox.json` with real provider tokens): see [Testing](testing.md)
3. **Multiplatform tests** (requires `.env` with SSH remotes): see [Multiplatform](multiplatform.md)

### Git hooks

The repo includes a pre-push hook that runs `go vet` and unit tests before allowing a push. I activate it once per clone:

```bash
git config core.hooksPath .githooks
```

After that, every `git push` runs the checks automatically. If `go vet` or any test fails, the push is blocked until I fix it.

The hook lives in `.githooks/pre-push` (version-controlled). To bypass it temporarily (not recommended):

```bash
git push --no-verify
```

### Test plan skill (Claude Code)

If you use Claude Code, the `/test-plan` skill automates the pre-PR verification workflow — it reads `docs/testing-checklist.md` and runs the automated steps, then guides you through the interactive ones across all 3 platforms.

```text
/test-plan              Quick pre-PR checks (default)
/test-plan full         Full release verification
```

Without Claude Code, follow the [testing checklist](testing-checklist.md) manually — every step is documented with the exact commands to run.

---

## Config Schema Evolution

When adding new fields to the configuration:

1. Add the field to the appropriate Go struct in `pkg/config/config.go` — use `json:"fieldName,omitempty"` with the correct casing (e.g., `useHttpPath` is camelCase to match GCM conventions)
2. Add the field to `gitbox.schema.json` with a clear description
3. Update `gitbox.jsonc` with an example
4. If the field belongs to an account vs a source, ensure it's in the right struct (`Account` for identity/credentials, `Source` for what to clone, `Repo` for per-repo overrides)
5. If the field affects v1→v2 migration, update `pkg/config/migrate.go`
6. If there are CRUD implications, update `pkg/config/crud.go`
7. Update `docs/reference.md` config reference table
8. Add tests for the new field in `pkg/config/config_test.go`

**Never bump the version number for additive changes.** Version 2 can grow with optional fields. Only bump to version 3 if breaking changes are needed (renames, removals, type changes).

---

## Release Process

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

### Creating a Release

Releases are fully automated via CI. Push a version tag and GitHub Actions builds all binaries, creates a GitHub Release, and attaches the assets:

```bash
git tag v1.0.0
git push origin v1.0.0
```

CI injects `-ldflags "-X main.version=<tag> -X main.commit=<sha>"` into both CLI and GUI builds. The release will contain one ZIP per platform with both CLI and GUI binaries:

- `gitbox-win-amd64.zip` — `gitbox.exe` + `GitboxApp.exe`
- `gitbox-macos-arm64.zip` — `gitbox` + `GitboxApp.app`
- `gitbox-linux-amd64.zip` — `gitbox` + `GitboxApp`

> **macOS note:** The GUI app is not codesigned. Users must run `xattr -cr gitbox.app` after downloading.

---

## Logo and App Icons

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

## TUI Demo Recordings (VHS)

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

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use `golangci-lint` if available
- Error messages should be lowercase, no trailing punctuation
- Exported functions need doc comments
- Use `context.Context` for operations that may be cancelled (GUI async ops)
