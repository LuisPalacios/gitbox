# Developer Guide

## Prerequisites

- **Go** 1.26+ ([install](https://go.dev/doc/install))
- **Node.js** 20+ ([install](https://nodejs.org/))
- **Git** 2.39+
- **Wails CLI** v2 (for GUI development):

  ```bash
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  ```

Check:

```bash
go version            # Go compiler
wails version         # Wails CLI (go install github.com/wailsapp/wails/v2/cmd/wails@latest)
node --version        # Node.js (for Svelte frontend)
npm --version         # npm
git --version         # Git
```


### Platform-Specific

- **Windows:** Git for Windows (provides Git Bash)
- **macOS:** Xcode Command Line Tools
- **Linux:** `libwebkit2gtk-4.1-dev` and `libgtk-3-dev` (for GUI builds)

---

## Building from Source

### CLI Only

```bash
# From the repository root
go build -o build/gitboxcmd ./cmd/cli

# Cross-compile for other platforms
GOOS=linux  GOARCH=amd64 go build -o build/gitboxcmd-linux-amd64  ./cmd/cli
GOOS=darwin GOARCH=arm64 go build -o build/gitboxcmd-darwin-arm64 ./cmd/cli
GOOS=windows GOARCH=amd64 go build -o build/gitboxcmd.exe         ./cmd/cli
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
# Output: cmd/gui/build/bin/Gitbox[.exe]
```

---

## Project Structure

```text
gitbox/                    (repo root)
├── assets/
│   ├── logo.svg                Project logo (SVG source, editable in Boxy SVG)
│   ├── appicon.png             App icon 1024x1024 PNG (exported from logo.svg)
│   └── icon.ico                Windows icon (256/128/64/48/32/16, converted from appicon.png)
├── cmd/
│   ├── cli/                    CLI binary (Cobra subcommands, one file per command)
│   │   ├── main.go             Root command, flag parsing, shared output helpers
│   │   ├── version_cmd.go      version — print version
│   │   ├── init_cmd.go         init — generate a starter config file
│   │   ├── global_cmd.go       global — show/edit global settings
│   │   ├── account_cmd.go      account — CRUD for accounts
│   │   ├── source_cmd.go       source — CRUD for sources
│   │   ├── repo_cmd.go         repo — CRUD for repos within sources
│   │   ├── credential_cmd.go   credential — setup/verify/del credentials
│   │   ├── discover_cmd.go     discover — find repos from provider API
│   │   ├── clone_cmd.go        clone — clone repos with progress bar
│   │   ├── pull_cmd.go         pull — pull repos that are behind
│   │   ├── status_cmd.go       status — colored sync status display
│   │   ├── scan_cmd.go         scan — filesystem walk for git repos
│   │   ├── migrate_cmd.go      migrate — v1→v2 config migration
│   │   └── token_cmd.go        token — deprecated shim
│   └── gui/                    Wails GUI app (Svelte frontend)
│       ├── main.go             Wails app initialization, build-time version vars
│       ├── app.go              Backend bindings exposed to the frontend
│       └── frontend/           Svelte SPA
├── pkg/                        Shared Go library (used by BOTH cli and gui)
│   ├── config/                 Config v2 model, load/save, v1→v2 migration
│   │   ├── config.go           Struct definitions (Config, Account, Source, Repo)
│   │   ├── load.go             JSON parsing with v1/v2 auto-detection, key order
│   │   ├── save.go             JSON serialization
│   │   ├── migrate.go          v1 → v2 conversion
│   │   ├── path.go             Config file paths, tilde expansion
│   │   ├── crud.go             CRUD operations on accounts, sources, repos
│   │   ├── config_test.go      Tests for load/save/migrate
│   │   └── crud_test.go        Tests for CRUD operations
│   ├── credential/             Credential management
│   │   ├── credential.go       Token storage (OS keyring), resolution chain
│   │   ├── validate.go         SSH key management, config parsing
│   │   ├── credential_test.go
│   │   └── validate_test.go
│   ├── git/                    Git subprocess operations
│   │   ├── git.go              Clone, CloneWithProgress, pull, status
│   │   ├── hidewindow_windows.go  Hide console window on Windows (SysProcAttr)
│   │   ├── hidewindow_other.go    No-op for non-Windows platforms
│   │   └── git_test.go
│   ├── provider/               Provider API clients
│   │   ├── provider.go         Interface, factory, TestAuth
│   │   ├── github.go           GitHub REST v3
│   │   ├── gitlab.go           GitLab REST v4
│   │   ├── gitea.go            Gitea/Forgejo REST API
│   │   ├── bitbucket.go        Bitbucket REST v2
│   │   ├── http.go             Shared HTTP helper
│   │   ├── guide.go            PAT creation URLs and scope guides
│   │   └── provider_test.go
│   ├── mirror/                 Push mirrors (planned)
│   │   └── mirror.go
│   └── status/                 Clone status checking
│       ├── status.go
│       └── status_test.go
├── docs/                       Documentation
├── legacy/
│   ├── git-config-repos/       Legacy bash script (UNTOUCHED)
│   ├── git-status-pull/        Legacy bash script (UNTOUCHED)
│   └── README.md               Legacy scripts documentation
├── gitbox.schema.json     v2 JSON Schema
├── gitbox.jsonc           Annotated example config
├── go.mod
├── go.sum
└── README.md
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

import "github.com/LuisPalacios/gitbox/pkg/config"

type NewProvider struct {
    baseURL  string
    username string
    token    string
}

func NewFromAccount(acct *config.Account) *NewProvider {
    return &NewProvider{
        baseURL:  acct.URL,
        username: acct.Username,
    }
}

func (p *NewProvider) ListRepos() ([]RepoInfo, error) {
    // Implement API call to list repositories
}

func (p *NewProvider) CreateRepo(name string, private bool) error {
    // Implement API call to create a repository
}

func (p *NewProvider) SetupMirror(repo string, targetURL string) error {
    // Implement mirror API if the provider supports it
}
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

2. Write tests in `pkg/provider/newprovider_test.go`.

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
# Run all tests
go test ./pkg/...

# Run tests with verbose output
go test -v ./pkg/...

# Run tests for a specific package
go test -v ./pkg/config/

# Run a specific test
go test -v -run TestLoadV2Config ./pkg/config/
```

### Test Conventions

- Unit tests live alongside the code they test (`foo_test.go` next to `foo.go`)
- Use table-driven tests for functions with multiple input/output cases
- Mock external calls (git subprocess, HTTP APIs) using interfaces
- Integration tests that need real git repos should create temp directories

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
# CI build with explicit version
go build -ldflags "-X main.version=v0.2.0 -X main.commit=abc1234" -o build/gitboxcmd ./cmd/cli

# Local builds auto-detect by running:
#   git describe --tags --always   → version (e.g., "v0.1.0-3-ga99cf17")
#   git rev-parse --short HEAD     → commit SHA
# Display format:
#   CI:    "v0.2.0 (abc1234)"
#   Local: "v0.1.0-3-ga99cf17-dev (a99cf17)"
```

### Creating a Release

Releases are fully automated via CI. Push a version tag and GitHub Actions builds all binaries, creates a GitHub Release, and attaches the assets:

```bash
git tag v1.0.0
git push origin v1.0.0
```

CI injects `-ldflags "-X main.version=<tag> -X main.commit=<sha>"` into both CLI and GUI builds. The release will contain one ZIP per platform with both CLI and GUI binaries:

- `gitbox-win-amd64.zip` — `gitboxcmd.exe` + `Gitbox.exe`
- `gitbox-macos-arm64.zip` — `gitboxcmd` + `Gitbox.app`
- `gitbox-linux-amd64.zip` — `gitboxcmd` + `Gitbox`

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

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use `golangci-lint` if available
- Error messages should be lowercase, no trailing punctuation
- Exported functions need doc comments
- Use `context.Context` for operations that may be cancelled (GUI async ops)
