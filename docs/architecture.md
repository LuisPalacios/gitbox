# Gitbox — Architecture & Design

> Product Requirements + Technical Design Document

---

## 1. Product Vision

### Problem

Developers who work across multiple Git identities (personal, corporate, open-source, self-hosted) face a recurring operational burden: configuring credentials, cloning repos with the right identity, and keeping everything in sync across machines. Each new machine means re-doing the setup manually.

### Solution

**gitbox** is a single tool that manages the full lifecycle of multi-account Git environments:

1. **Define** your accounts and credentials (once)
2. **Discover** repositories from each provider's API
3. **Clone** everything with the correct identity and folder structure
4. **Monitor** sync status across all repos
5. **Pull** updates safely (fast-forward only)

### Target Users

- Developers with 2+ Git identities (personal + work, multiple GitHub orgs, self-hosted Forgejo/Gitea)
- DevOps engineers managing repos across multiple providers
- Teams that need reproducible dev environment setup across machines

### Success Criteria

- A developer can go from a fresh machine to a fully configured multi-account Git environment in under 10 minutes
- All operations work identically on Windows, macOS, and Linux
- Non-technical users can follow the CLI workflow without knowing Git internals

### Platform Support

| Platform      | Status      | Notes                      |
| ------------- | ----------- | -------------------------- |
| Windows 11    | Primary dev | Git Bash, Windows Terminal |
| macOS (arm64) | Tested      | Native terminal            |
| Linux (amd64) | Tested      | Headless and desktop       |

---

## 2. Architecture Overview

gitbox is a Go monorepo producing two independent binaries from a shared library:

![Architecture Overview](diagrams/architecture-overview.png)

```text
                 ┌─────────────┐    ┌─────────────────┐
                 │  gitboxcmd   │    │    gitbox        │
                 │    (CLI)     │    │   (Wails GUI)    │
                 └──────┬──────┘    └────────┬─────────┘
                        │                    │
                        └────────┬───────────┘
                                 │
                 ┌───────────────▼───────────────┐
                 │           pkg/                │
                 │      (shared Go library)      │
                 ├───────────────────────────────┤
                 │ config/     Config model,     │
                 │             CRUD, migration   │
                 │ credential/ OS keyring, SSH,  │
                 │             GCM integration   │
                 │ provider/   GitHub, GitLab,   │
                 │             Gitea, Forgejo,   │
                 │             Bitbucket APIs    │
                 │ git/        Git subprocess    │
                 │             wrapper           │
                 │ status/     Clone status      │
                 │             checking          │
                 │ mirror/     Push mirrors      │
                 │             (planned)         │
                 └───────────────┬───────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                  ▼
         system git        Provider APIs      OS credential
         (os/exec)         (net/http)           store
              │                  │                  │
              ▼                  ▼                  ▼
         local repos        remote repos     keychain /
                                            wincredman /
                                           secretservice
```

| Binary          | Purpose                                 | Technology             | Auth            |
| --------------- | --------------------------------------- | ---------------------- | --------------- |
| **`gitboxcmd`** | CLI — power users, headless servers, CI | Go + Cobra             | GCM, SSH, Token |
| **`gitbox`**    | GUI — desktop users                     | Go + Wails v2 + Svelte | GCM (guided)    |

Both binaries share the exact same `pkg/` library. The GUI never reimplements logic that the CLI already has — it calls the same Go functions.

### Repository Layout

```text
cmd/
  cli/                    Go CLI binary (gitboxcmd)
  gui/                    Wails v2 + Svelte GUI (gitbox)
pkg/
  config/                 Config v2 model, load/save, v1→v2 migration
  credential/             OS keyring, SSH key management, GCM integration
  provider/               Provider API clients
  git/                    Git subprocess operations (os/exec)
  status/                 Clone status checking
  mirror/                 Repo migration + push mirrors (planned)
docs/                     Documentation
legacy/                   Shell scripts (UNTOUCHED, production)
```

---

## 3. Core Concepts

### Accounts, Sources, and Repos

![Config Model](diagrams/config-model.png)

The config separates **WHO you are** from **WHAT you clone**:

```text
Account (WHO)                    Source (WHAT)
├─ provider: "github"            ├─ account: "github-personal"  ← references an Account
├─ url: "https://github.com"     ├─ folder: (optional override)
├─ username: "myuser"            └─ repos:
├─ name: "My Name"                  ├─ "MyOrg/project-a": {}
├─ email: "me@example.com"          ├─ "MyOrg/project-b": {}
├─ default_credential_type: "gcm"   └─ "other-org/tools": {}
└─ gcm: { provider: "github" }
```

**Why separate?** One account can have multiple sources (e.g., different GitHub orgs under the same login). Sources group repos logically. Repos use `org/repo` naming — the org part becomes the folder structure.

### Credential Model

Each credential type is **self-sufficient** for the account — no mixing required:

| Type      | Git Auth                   | API Access                                 | Storage                 |
| --------- | -------------------------- | ------------------------------------------ | ----------------------- |
| **Token** | PAT embedded in clone URL  | Same PAT                                   | OS keyring (go-keyring) |
| **GCM**   | Browser OAuth via GCM      | `git credential fill` extracts OAuth token | GCM's own store         |
| **SSH**   | Key pair + `~/.ssh/config` | Optional PAT (for discovery only)          | SSH key files + keyring |

### Config as Local Database

The JSON config file (`~/.config/gitbox/gitbox.json`) is the **desired state** — a local database of what accounts, sources, and repos should exist.

**Discovery** is a northbound query — it asks the provider API "what repos exist?" and lets you add them to the config. Discovery is add-only on demand; it never auto-removes repos.

### Folder Structure

Repos are cloned into a 3-level hierarchy:

```text
~/00.git/                          ← global.folder
  github-personal/                 ← source key (1st level)
    MyOrg/                         ← org from "MyOrg/project-a" (2nd level)
      project-a/                   ← repo name (3rd level)
      project-b/
    other-org/
      tools/
  forgejo-work/
    infra/
      homelab-ops/
```

Each level can be overridden:

- **1st level**: `source.folder` overrides the source key
- **2nd level**: `repo.id_folder` overrides the org part
- **3rd level**: `repo.clone_folder` overrides the repo name (if absolute path, replaces everything)

---

## 4. Component Design

### pkg/config — Configuration Management

**Files:** `config.go`, `load.go`, `save.go`, `crud.go`, `migrate.go`, `path.go`

Handles the v2 configuration file at `~/.config/gitbox/gitbox.json`.

**Core types:**

```go
type Config struct {
    Schema      string              `json:"$schema,omitempty"`
    Version     int                 `json:"version"`           // always 2
    Global      GlobalConfig        `json:"global"`
    Accounts    map[string]Account  `json:"accounts"`
    Sources     map[string]Source   `json:"sources"`
    SourceOrder []string            `json:"-"`  // preserves JSON key order
}

type Account struct {
    Provider              string       `json:"provider"`              // github, gitlab, gitea, forgejo, bitbucket
    URL                   string       `json:"url"`                   // provider base URL
    Username              string       `json:"username"`
    Name                  string       `json:"name"`                  // git user.name
    Email                 string       `json:"email"`                 // git user.email
    DefaultCredentialType string       `json:"default_credential_type"`
    SSH                   *SSHConfig   `json:"ssh,omitempty"`
    GCM                   *GCMConfig   `json:"gcm,omitempty"`
    Token                 *TokenConfig `json:"token,omitempty"`
}

type Source struct {
    Account   string            `json:"account"`           // references an Account key
    Folder    string            `json:"folder,omitempty"`  // override 1st level dir
    Repos     map[string]Repo   `json:"repos"`
    RepoOrder []string          `json:"-"`  // preserves JSON key order
}

type Repo struct {
    CredentialType string `json:"credential_type,omitempty"` // override account default
    Name           string `json:"name,omitempty"`            // override git user.name
    Email          string `json:"email,omitempty"`            // override git user.email
    IdFolder       string `json:"id_folder,omitempty"`       // override 2nd level dir
    CloneFolder    string `json:"clone_folder,omitempty"`    // override 3rd level dir
}
```

**Key design decisions:**

- **Auto-detection:** `Load()` detects v1 vs v2 format (v1 has `accounts` but no `sources`)
- **JSON order preservation:** `SourceOrder` and `RepoOrder` are extracted during parsing via `json.Decoder` tokenizer, ensuring iteration follows the user's config file order
- **Credential inheritance:** Repos inherit `default_credential_type` from their account unless they override it
- **CRUD with referential integrity:** `DeleteAccount` fails if any source references it; `DeleteSource` cascades to its repos

**v1→v2 migration:**

- Accounts with same `(hostname, username)` are deduplicated into one Account + one Source
- `"true"/"false"` strings → native booleans
- Flat SSH/GCM fields → nested objects
- Repo names become `org/repo` format
- Most common `credential_type` becomes `default_credential_type` on the account

### pkg/credential — Credential Management

**Files:** `credential.go`, `validate.go`

Manages tokens, SSH keys, and GCM integration across three OS credential stores.

**Token management (via [go-keyring](https://github.com/zalando/go-keyring)):**

```go
StoreToken(accountKey, token string) error     // Store PAT in OS keyring
GetToken(accountKey string) (string, error)     // Retrieve from keyring
DeleteToken(accountKey string) error            // Remove from keyring
```

Keyring entries use service=`gitbox`, user=account-key.

**Token resolution chain:**

```go
ResolveToken(acct, accountKey) → (token, source, error)
```

Priority: environment variable (`GITBOX_TOKEN_<KEY>`) → `GIT_TOKEN` fallback → OS keyring.

**API token dispatch:**

```go
ResolveAPIToken(acct, accountKey) → (token, source, error)
```

Routes by credential type:

- `token` → `ResolveToken()` (keyring)
- `gcm` → `ResolveGCMToken()` (git credential fill)
- `ssh` → `ResolveToken()` (keyring, optional PAT)

**GCM integration:**

```go
ResolveGCMToken(accountURL, username string) → (token, source, error)
```

Runs `git credential fill` with the account URL + username, extracts the OAuth token that GCM stored. This allows API access without the user storing a separate PAT.

**SSH key management:**

```go
GenerateSSHKey(sshFolder, accountKey, keyType) → (keyPath, error)
WriteSSHConfigEntry(sshFolder, opts) error
RemoveSSHConfigEntry(sshFolder, host) error
TestSSHConnection(host) → (greeting, error)
FindSSHKey(sshFolder, host, keyType) → (keyPath, error)
```

**SSH naming convention:**

- Host alias: `gitbox-<account-key>`
- Key file: `gitbox-<account-key>-sshkey`
- Comment: `gitbox-<hostname>`

### pkg/provider — Repository Discovery

**Files:** `provider.go`, `github.go`, `gitlab.go`, `gitea.go`, `bitbucket.go`, `http.go`, `guide.go`

Abstraction layer for Git hosting provider APIs.

```go
type Provider interface {
    ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error)
}

type RemoteRepo struct {
    FullName    string  // "org/repo" format
    Description string
    CloneHTTPS  string
    CloneSSH    string
    Private     bool
    Fork        bool
    Archived    bool
}
```

| Provider      | API          | Auth                        | Notes                         |
| ------------- | ------------ | --------------------------- | ----------------------------- |
| GitHub        | REST v3      | Bearer token                | Supports GitHub Enterprise    |
| GitLab        | REST v4      | PRIVATE-TOKEN header        | Self-hosted compatible        |
| Gitea/Forgejo | REST /api/v1 | Token + Basic auth fallback | Same API, same implementation |
| Bitbucket     | REST v2      | HTTP Basic (app password)   | Cloud only                    |

**Factory:** `ByName("github")` returns the correct implementation. Forgejo uses the Gitea client.

**Helpers:**

- `TestAuth()` — validates credentials via a minimal ListRepos call
- `TokenSetupGuide()` — per-provider PAT creation URL + required scopes
- `TokenCreationURL()` — direct link to provider's token settings page

### pkg/git — Git Operations

**File:** `git.go`

Thin wrapper around `os/exec` for all Git operations. No libgit2 dependency.

```go
Clone(url, dest string, opts CloneOpts) error
CloneWithProgress(url, dest, opts, onProgress func(CloneProgress)) error
Pull(repoPath string) error
PullQuiet(repoPath string) error
Status(repoPath string) (RepoStatus, error)
Fetch(repoPath string) error
IsRepo(path string) bool
ConfigSet(repoPath, key, value string) error
SetRemoteURL(repoPath, remote, url string) error
```

**CloneWithProgress** runs `git clone --progress`, parses stderr in real-time (splitting on `\r`), and fires a callback with `{Phase, Percent}` for each progress update. This powers the animated progress bar in the CLI.

**RepoStatus** parsed from `git status --porcelain=v2 --branch`:

```go
type RepoStatus struct {
    Branch, Upstream string
    Ahead, Behind    int
    Modified, Added, Deleted, Untracked, Conflicts int
}
```

### pkg/status — Sync Status Checking

**File:** `status.go`

Determines the sync state of local clones relative to their upstream.

```go
type State int  // Clean, Dirty, Behind, Ahead, Diverged, Conflict, NotCloned, NoUpstream, Error

func Check(repoPath string) RepoStatus
func CheckAll(cfg *config.Config) []RepoStatus
func ResolveRepoPath(globalFolder, sourceFolder, repoName string, repo config.Repo) string
```

**State priority:** Conflicts > Dirty > Diverged > Behind > Ahead > NoUpstream > Clean

**Path resolution:** `globalFolder / sourceFolder / idFolder / cloneFolder` with overrides at each level.

### pkg/mirror — Push Mirrors (Planned)

Not yet implemented. Will support:

- `git clone --mirror` + `git push --mirror` for migration
- Gitea/Forgejo API for server-side push mirror configuration

---

## 5. Config Format (v2)

### Example

```jsonc
{
  "$schema": "https://raw.githubusercontent.com/LuisPalacios/gitbox/main/gitbox.schema.json",
  "version": 2,
  "global": {
    "folder": "~/00.git",
    "credential_ssh": { "ssh_folder": "~/.ssh" },
    "credential_gcm": { "helper": "manager", "credential_store": "wincredman" },
  },
  "accounts": {
    "github-personal": {
      "provider": "github",
      "url": "https://github.com",
      "username": "myuser",
      "name": "My Name",
      "email": "me@example.com",
      "default_credential_type": "gcm",
      "gcm": { "provider": "github" },
    },
  },
  "sources": {
    "github-personal": {
      "account": "github-personal",
      "repos": {
        "MyOrg/project-a": {},
        "MyOrg/project-b": {},
        "other-org/tools": { "credential_type": "ssh" },
      },
    },
  },
}
```

### Schema

A JSON Schema is published at [`gitbox.schema.json`](../gitbox.schema.json) for editor validation and autocompletion. An annotated example with Spanish comments is at [`gitbox.jsonc`](../gitbox.jsonc).

### Credential Type Inheritance

```text
Account.default_credential_type = "gcm"
  └─ Repo "MyOrg/project-a"  → credential_type: ""      → inherits "gcm"
  └─ Repo "other-org/tools"  → credential_type: "ssh"   → uses "ssh"
```

### Folder Resolution Algorithm

```text
Input:  globalFolder="~/00.git", sourceFolder="github-personal",
        repoKey="MyOrg/project-a", repo={id_folder:"", clone_folder:""}

Step 1: Split repoKey → org="MyOrg", name="project-a"
Step 2: idFolder = repo.id_folder || org       → "MyOrg"
Step 3: cloneFolder = repo.clone_folder || name → "project-a"
Step 4: If cloneFolder is absolute → return cloneFolder directly
Step 5: Join: ~/00.git / github-personal / MyOrg / project-a
```

---

## 6. Credential Architecture

![Credential Flow](diagrams/credential-flow.png)

### Token Flow

```text
User runs: gitboxcmd credential setup <account>
  │
  ├─ Shows provider-specific PAT creation URL + required scopes
  ├─ Prompts for token
  ├─ Validates token via provider API (TestAuth)
  └─ Stores token in OS keyring (service="gitbox", user=<account-key>)

Clone uses: ResolveToken() → env var chain → keyring → embed in clone URL
API uses:   ResolveAPIToken() → same token
```

### GCM Flow

```text
User runs: gitboxcmd credential setup <account>
  │
  ├─ Runs "git credential fill" → triggers GCM browser OAuth
  ├─ Runs "git credential approve" → GCM stores the credential
  ├─ Tests API access with the GCM token
  └─ If API fails (some providers), offers to store a separate PAT

Clone uses: HTTPS URL with username → GCM provides credentials automatically
API uses:   ResolveGCMToken() → "git credential fill" → extracts OAuth token
```

### SSH Flow

```text
User runs: gitboxcmd credential setup <account>
  │
  ├─ Creates ~/.ssh/config entry (Host gitbox-<account-key>)
  ├─ Generates ed25519 key pair (gitbox-<account-key>-sshkey)
  ├─ Displays public key → user registers at provider
  ├─ Waits for user confirmation → tests SSH connection
  └─ Optionally stores PAT for API discovery

Clone uses: git@gitbox-<account-key>:<org>/<repo>.git
API uses:   ResolveAPIToken() → keyring fallback (PAT if stored)
```

### ResolveAPIToken Dispatch

```go
func ResolveAPIToken(acct, accountKey) {
    switch acct.DefaultCredentialType {
    case "token":
        return ResolveToken(acct, accountKey)          // env var → keyring
    case "gcm":
        return ResolveGCMToken(acct.URL, acct.Username) // git credential fill
    case "ssh":
        return ResolveToken(acct, accountKey)          // keyring (optional PAT)
    }
}
```

---

## 7. CLI Command Structure

![CLI Workflow](diagrams/cli-workflow.png)

### Command Tree

```text
gitboxcmd
├─ init                              Initialize config file
├─ account
│  ├─ list                           List all accounts
│  ├─ add <key> --provider ...       Add an account
│  ├─ update <key> --name ...        Update account fields
│  ├─ delete <key>                   Delete an account
│  ├─ show <key>                     Show account as JSON
│  ├─ credential
│  │  ├─ setup <key>                 Set up credentials (idempotent)
│  │  ├─ verify <key>               Verify credentials work
│  │  └─ del <key>                  Remove credentials
│  └─ discover <key>                 Discover repos from provider API
├─ source
│  ├─ list                           List all sources
│  ├─ add <key> --account ...        Add a source
│  ├─ update <key> --folder ...      Update source fields
│  ├─ delete <key>                   Delete source + its repos
│  └─ show <key>                     Show source as JSON
├─ repo
│  ├─ list [--source <key>]          List repos (optionally filtered)
│  ├─ add <source> <repo>            Add a repo to a source
│  ├─ update <source> <repo> ...     Update repo fields
│  ├─ delete <source> <repo>         Remove a repo
│  └─ show <source> <repo>           Show repo as JSON
├─ clone [--source] [--repo]         Clone configured repos
├─ pull [--source] [--repo]          Pull repos that are behind
├─ status [--source] [--repo]        Show sync status of all repos
├─ scan [--dir] [--pull]             Filesystem walk for repo status
├─ migrate --source ... --target ... Migrate v1 config to v2
├─ completion <shell>                Generate shell completion
└─ version                           Show version info
```

### Global Flags

```text
--config <path>    Custom config file path
--json             JSON output format
--verbose          Show all items (including clean/skipped)
```

### Workflow

```text
                    ┌──────────┐
                    │   init   │  Create config file
                    └────┬─────┘
                         │
                ┌────────▼────────┐
                │  account add    │  Define your identities
                └────────┬────────┘
                         │
             ┌───────────▼───────────┐
             │  credential setup     │  Store credentials
             └───────────┬───────────┘
                         │
                ┌────────▼────────┐
                │    discover     │  Find repos from provider API
                └────────┬────────┘
                         │
                ┌────────▼────────┐
                │     clone       │  Clone everything
                └────────┬────────┘
                         │
              ┌──────────▼──────────┐
              │  status / pull /    │  Day-to-day operations
              │  scan               │
              └─────────────────────┘
```

---

## 8. UX Design Principles

These principles define the project's UX DNA. They apply to both the CLI and the future GUI.

### Core Philosophy

> The user should NOT need to think about or know Git internals. Commands are verbs that do what they say. Output is designed for people who don't know git well.

### Output Patterns

**One-liner streaming:** Every operation shows one colored line per item as it happens, not in a batch after completion.

```text
+ cloned    github-personal/MyOrg/project-a
+ cloned    github-personal/MyOrg/project-b
~ exists    github-personal/other-org/tools
x error     forgejo-work/infra/broken-repo          credential not found
```

**Progress feedback always:** Never leave the user staring at a blank screen. Long operations show animated progress:

```text
+ cloning  github-personal/MyOrg/project-a  ████████░░░░░░░░░░░░  40% Receiving
```

Then snap to the final state:

```text
+ cloned   github-personal/MyOrg/project-a
```

**Quiet by default, verbose on demand:** Only show actionable items (errors, warnings, actual changes). Clean repos and skipped items are hidden unless `--verbose` is used.

### Color System

True-color ANSI codes matching the Oh-My-Posh palette. All color output goes through `colorize()` which respects the `NO_COLOR` environment variable.

| Color      | Hex       | Symbol | Meaning                     |
| ---------- | --------- | ------ | --------------------------- |
| Green      | `#61fd5f` | `+`    | Success, clean, ok          |
| Orange     | `#F07623` | `!`    | Warning, dirty, in-progress |
| Red        | `#D81E5B` | `x`    | Error, conflict             |
| Cyan       | `#61fdff` | `~`    | Info, skip, section header  |
| Purple     | `#D91C9A` | `<`    | Behind upstream             |
| Blue       | `#4B95E9` | `>`    | Ahead of upstream           |
| Bold white | —         | —      | Titles and headers          |

### Behavioral Rules

- **JSON order preserved:** Output follows the user's config file order — predictable, not random
- **Idempotent commands:** Running `credential setup` or `clone` multiple times is safe — they detect existing state and skip or verify
- **Fail fast with actionable messages:** Errors tell the user what to do, not just what went wrong (e.g., "run `credential setup` to fix this")
- **No secrets in output:** Tokens are never displayed, even in verbose mode

---

## 9. GUI Design Blueprint

> High-level mapping only. Detailed screen design will be done when the GUI phase begins.

### Technology

- **Backend:** Go (same `pkg/` library as CLI)
- **Frontend:** Svelte
- **Bridge:** Wails v2 — Go methods on an `App` struct are exposed as TypeScript functions

### Architecture

```text
┌────────────────────────────────────────────┐
│              Wails Runtime                 │
├────────────────────┬───────────────────────┤
│   Go Backend       │   Svelte Frontend     │
│                    │                       │
│   app.go           │   App.svelte          │
│   ├─ GetConfig()   │   ├─ Sidebar          │
│   ├─ SaveConfig()  │   ├─ Dashboard        │
│   ├─ Discover()    │   ├─ AccountForm      │
│   ├─ Clone()       │   ├─ ClonePanel       │
│   ├─ GetStatus()   │   ├─ StatusGrid       │
│   ├─ Pull()        │   └─ Wizards          │
│   └─ SetupAuth()   │                       │
│                    │                       │
│   imports pkg/*    │   calls Go via        │
│                    │   wails.Call()         │
└────────────────────┴───────────────────────┘
```

### Screen Mapping

| GUI Screen       | CLI Equivalent              | Purpose                                   |
| ---------------- | --------------------------- | ----------------------------------------- |
| Dashboard        | `status`                    | Overview of all repos, at-a-glance health |
| Accounts         | `account list/add/edit/del` | Manage identities                         |
| Credential Setup | `credential setup/verify`   | Guided credential wizard                  |
| Discovery        | `account discover`          | Browse + select repos from provider       |
| Clone            | `clone`                     | Clone with progress bars                  |
| Status Grid      | `status --json`             | Detailed repo state with filters          |
| Pull             | `pull`                      | One-click pull for behind repos           |
| Settings         | `global`                    | Config file location, global folder       |

### Async Operations

Long-running operations (clone, status refresh, pull) run in goroutines. Progress is pushed to the frontend via Wails events:

```go
// Go backend
func (a *App) CloneAll() {
    go func() {
        for repo := range repos {
            runtime.EventsEmit(a.ctx, "clone:progress", repo, percent)
        }
        runtime.EventsEmit(a.ctx, "clone:done")
    }()
}
```

### GUI UX Principles

The same UX principles from section 8 apply to the GUI:

- Progress feedback for every operation
- Color-coded status indicators
- Non-technical language
- Idempotent actions (clicking "Clone" when already cloned is safe)

---

## 10. Security

- **Tokens are NEVER stored in the JSON config file.** They go in the OS credential store (Windows Credential Manager, macOS Keychain, Linux Secret Service) via [go-keyring](https://github.com/zalando/go-keyring).
- **The config file contains no secrets** — only URLs, usernames, folder paths, and preference flags.
- **Provider API calls use tokens from the credential store** at runtime, never from config.
- **SSH private keys** are standard `~/.ssh/` files with appropriate permissions (600).
- **Clone URLs are sanitized** — token-authenticated clone URLs have the token stripped from the remote after cloning.
- **No secrets in output** — tokens are never displayed, even in verbose mode.
- **The repository is public** — no real hostnames, usernames, emails, or tokens in tracked files.

---

## 11. Diagrams

Architecture diagrams are available in `docs/diagrams/` as editable `.drawio` files:

- **architecture-overview.drawio** — High-level system component diagram
- **credential-flow.drawio** — Per-type credential resolution flow
- **config-model.drawio** — Accounts / Sources / Repos data model
- **cli-workflow.drawio** — User workflow from init to day-to-day

These can be opened and edited with [draw.io](https://app.diagrams.net/) or the VS Code drawio extension.
