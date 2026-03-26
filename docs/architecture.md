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

| Binary          | Purpose                                 | Technology             | Auth            |
| --------------- | --------------------------------------- | ---------------------- | --------------- |
| **`gitboxcmd`** | CLI — power users, headless servers, CI | Go + Cobra             | GCM, SSH, Token |
| **`gitbox`**    | GUI — desktop users                     | Go + Wails v2 + Svelte | GCM (guided)    |

Both binaries share the exact same `pkg/` library. The GUI never reimplements logic that the CLI already has — it calls the same Go functions.

## 3. Core Concepts

### Accounts, Sources, and Repos

![Config Model](diagrams/config-model.png)

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
~/00.git/                          <- global.folder
  github-personal/                 <- source key (1st level)
    MyOrg/                         <- org from "MyOrg/project-a" (2nd level)
      project-a/                   <- repo name (3rd level)
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

Handles the v2 configuration file. Core types: `Config`, `Account`, `Source`, `Repo`. See `pkg/config/config.go` for struct definitions.

**Key design decisions:**

- **Auto-detection:** `Load()` detects v1 vs v2 format (v1 has `accounts` but no `sources`)
- **JSON order preservation:** `SourceOrder` and `RepoOrder` ensure iteration follows the user's config file order
- **Credential inheritance:** Repos inherit `default_credential_type` from their account unless they override it
- **CRUD with referential integrity:** `DeleteAccount` fails if any source references it; `DeleteSource` cascades to its repos
- **v1 to v2 migration:** Deduplicates accounts by `(hostname, username)`, converts string booleans to native booleans, nests flat SSH/GCM fields into objects, and reformats repo names to `org/repo`

### pkg/credential — Credential Management

Manages tokens, SSH keys, and GCM integration across three OS credential stores. See `pkg/credential/credential.go` and `pkg/credential/validate.go`.

**Token resolution chain:** Environment variable (`GITBOX_TOKEN_<KEY>`) -> `GIT_TOKEN` fallback -> OS keyring.

**API token dispatch:** Routes by credential type — `token` uses the keyring, `gcm` runs `git credential fill`, `ssh` falls back to keyring (optional PAT).

**SSH key management:** Generates key pairs, writes `~/.ssh/config` entries, tests connections. Naming convention: host alias `gitbox-<account-key>`, key file `gitbox-<account-key>-sshkey`.

### pkg/provider — Repository Discovery

Abstraction layer for Git hosting provider APIs. Each provider implements `ListRepos()` returning `RemoteRepo` structs. See `pkg/provider/provider.go` for the interface.

| Provider      | API          | Auth                        | Notes                         |
| ------------- | ------------ | --------------------------- | ----------------------------- |
| GitHub        | REST v3      | Bearer token                | Supports GitHub Enterprise    |
| GitLab        | REST v4      | PRIVATE-TOKEN header        | Self-hosted compatible        |
| Gitea/Forgejo | REST /api/v1 | Token + Basic auth fallback | Same API, same implementation |
| Bitbucket     | REST v2      | HTTP Basic (app password)   | Cloud only                    |

Helpers include `TestAuth()` for credential validation and `TokenSetupGuide()` for per-provider PAT creation instructions.

### pkg/git — Git Operations

Thin wrapper around `os/exec` for all Git operations — no libgit2 dependency. Provides `Clone`, `CloneWithProgress`, `Pull`, `Status`, `Fetch`, `ConfigSet`, and more. See `pkg/git/git.go`.

On macOS, `GitBin()` probes Homebrew paths (`/opt/homebrew/bin/git`, `/usr/local/bin/git`) before falling back to PATH, ensuring GUI apps find GCM-enabled git even with the minimal PATH that macOS GUI apps inherit.

### pkg/status — Sync Status Checking

Determines the sync state of local clones relative to their upstream. States: Clean, Dirty, Behind, Ahead, Diverged, Conflict, NotCloned, NoUpstream, Error. Priority: Conflicts > Dirty > Diverged > Behind > Ahead > NoUpstream > Clean. See `pkg/status/status.go`.

### pkg/mirror — Push Mirrors (Planned)

Not yet implemented. Will support `git clone --mirror` + `git push --mirror` for migration and Gitea/Forgejo API for server-side push mirror configuration.

---

## 5. Config Format (v2)

See the [JSON annotated example](../gitbox.jsonc) for a complete config with comments, and the [JSON Schema](../gitbox.schema.json) for editor validation and autocompletion.

**Credential type inheritance:** Repos inherit `default_credential_type` from their account unless they set their own `credential_type`.

**Folder resolution:** `globalFolder / sourceFolder / idFolder / cloneFolder`, with overrides possible at each level. If `clone_folder` is an absolute path, it replaces the entire hierarchy.

---

## 6. Credential Architecture

![Credential Flow](diagrams/credential-flow.png)

### Token Flow

User runs `credential setup` -> app shows the provider-specific PAT creation URL with required scopes -> user pastes the token -> app validates it via the provider API -> stores it in the OS keyring. Clone and API access both use the same token.

### GCM Flow

User runs `credential setup` -> app triggers `git credential fill` which opens browser OAuth -> app runs `git credential approve` to persist -> tests API access with the GCM token. Clone uses HTTPS with username (GCM provides credentials automatically). API access extracts the OAuth token via `git credential fill`.

### SSH Flow

User runs `credential setup` -> app creates `~/.ssh/config` entry and generates an ed25519 key pair -> displays the public key for the user to register at their provider -> tests the SSH connection. API access optionally uses a separately stored PAT for discovery.

---

## 7. CLI Command Structure

![CLI Workflow](diagrams/cli-workflow.png)

### Command Tree

```text
gitboxcmd
|- init                              Initialize config file
|- account
|  |- list                           List all accounts
|  |- add <key> --provider ...       Add an account
|  |- update <key> --name ...        Update account fields
|  |- delete <key>                   Delete an account
|  |- show <key>                     Show account as JSON
|  |- credential
|  |  |- setup <key>                 Set up credentials (idempotent)
|  |  |- verify <key>               Verify credentials work
|  |  +- del <key>                  Remove credentials
|  +- discover <key>                 Discover repos from provider API
|- source
|  |- list                           List all sources
|  |- add <key> --account ...        Add a source
|  |- update <key> --folder ...      Update source fields
|  |- delete <key>                   Delete source + its repos
|  +- show <key>                     Show source as JSON
|- repo
|  |- list [--source <key>]          List repos (optionally filtered)
|  |- add <source> <repo>            Add a repo to a source
|  |- update <source> <repo> ...     Update repo fields
|  |- delete <source> <repo>         Remove a repo
|  +- show <source> <repo>           Show repo as JSON
|- clone [--source] [--repo]         Clone configured repos
|- pull [--source] [--repo]          Pull repos that are behind
|- status [--source] [--repo]        Show sync status of all repos
|- scan [--dir] [--pull]             Filesystem walk for repo status
|- migrate --source ... --target ... Migrate v1 config to v2
|- completion <shell>                Generate shell completion
+- version                           Show version info
```

### Global Flags

```text
--config <path>    Custom config file path
--json             JSON output format
--verbose          Show all items (including clean/skipped)
```

---

## 8. UX Design Principles

These principles apply to both the CLI and the GUI.

### Core Philosophy

> The user should NOT need to think about or know Git internals. Commands are verbs that do what they say. Output is designed for people who don't know git well.

### Output Patterns

- **One-liner streaming:** Every operation shows one colored line per item as it happens, not in a batch after completion
- **Progress feedback always:** Long operations show animated progress bars, then snap to the final state
- **Quiet by default, verbose on demand:** Only show actionable items (errors, warnings, actual changes). Clean repos and skipped items are hidden unless `--verbose` is used

### Color System

True-color ANSI codes matching the Oh-My-Posh palette. All color output respects the `NO_COLOR` environment variable.

| Color      | Symbol | Meaning                     |
| ---------- | ------ | --------------------------- |
| Green      | `+`    | Success, clean, ok          |
| Orange     | `!`    | Warning, dirty, in-progress |
| Red        | `x`    | Error, conflict             |
| Cyan       | `~`    | Info, skip, section header  |
| Purple     | `<`    | Behind upstream             |
| Blue       | `>`    | Ahead of upstream           |
| Bold white | --     | Titles and headers          |

### Behavioral Rules

- **JSON order preserved:** Output follows the user's config file order
- **Idempotent commands:** Running `credential setup` or `clone` multiple times is safe
- **Fail fast with actionable messages:** Errors tell the user what to do, not just what went wrong
- **No secrets in output:** Tokens are never displayed, even in verbose mode

---

## 9. GUI Architecture

The GUI is a Wails v2 desktop app with a Svelte frontend. The Go backend (`cmd/gui/app.go`) exposes methods that the frontend calls via auto-generated TypeScript bindings. The frontend bridge is in `cmd/gui/frontend/src/lib/bridge.ts`.

Long-running operations (clone, status refresh, pull) run in goroutines with progress pushed to the frontend via Wails events.

See the [GUI Guide](gui-guide.md) for the user-facing walkthrough.

---

## 10. Security

- **Tokens are NEVER stored in the JSON config file.** They go in the OS credential store (Windows Credential Manager, macOS Keychain, Linux Secret Service) via go-keyring.
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
