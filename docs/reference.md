# Reference guide

Complete reference for all gitbox commands, configuration format, folder structure, and troubleshooting.

For getting started, see the [CLI guide](cli-guide.md) or [GUI guide](gui-guide.md). For installation, see the [README](../README.md).

---

## Account management

An account represents WHO you are on a git server — one unique `(hostname, username)` pair.

### Adding accounts

```bash
# GitHub personal
gitbox account add github-personal \
  --provider github \
  --url https://github.com \
  --username MyGitHubUser \
  --name "My Name" \
  --email "myuser@example.com" \
  --default-credential-type gcm

# Forgejo homelab (SSH — host and key-type are mandatory)
gitbox account add forgejo-homelab \
  --provider forgejo \
  --url https://forge.mylab.lan \
  --username myuser \
  --name "My Name" \
  --email "myuser@mylab.lan" \
  --default-credential-type ssh \
  --ssh-host gt-myuser \
  --ssh-key-type ed25519

# GitLab
gitbox account add gitlab-work \
  --provider gitlab \
  --url https://gitlab.com \
  --username youruser \
  --name "Your Name" \
  --email "you@company.com"
```

### Listing and inspecting accounts

```bash
gitbox account list
gitbox account show github-personal
gitbox account show github-personal --json
```

### Updating an account

Only the flags you specify are changed:

```bash
gitbox account update github-personal --name "New Name" --email "new@email.com"
```

### Deleting an account

An account can only be deleted if no sources reference it:

```bash
gitbox account delete github-personal
# Error: cannot delete — referenced by source "github-personal"

gitbox source delete github-personal
gitbox account delete github-personal  # now succeeds
```

---

## Source management

A source represents WHAT you clone from an account. Each source references one account and contains a list of repos.

### Adding a source

```bash
gitbox source add github-personal --account github-personal
gitbox source add forgejo-homelab --account forgejo-homelab
```

By default, the source key is used as the first-level clone folder. To override:

```bash
gitbox source add my-server --account forgejo-homelab --folder "server-repos"
```

### Listing sources

```bash
gitbox source list
gitbox source list --account github-personal
```

---

## Repo management

Repos use `org/repo` format. The org part becomes the second-level folder, the repo part becomes the third-level (clone) folder.

### Adding repos

```bash
# Simple — inherits credential type from account default
gitbox repo add github-personal "MyGitHubUser/gitbox"
gitbox repo add github-personal "MyGitHubUser/dotfiles"

# Cross-org — access another org's repo with your credentials
gitbox repo add github-personal "other-org/their-repo"

# Multiple orgs on same server (Forgejo/Gitea)
gitbox repo add forgejo-homelab "infra/homelab"
gitbox repo add forgejo-homelab "infra/migration"
gitbox repo add forgejo-homelab "personal/my-project"

# Override credential type for a specific repo
gitbox repo add github-personal "MyGitHubUser/private-repo" --credential-type ssh
```

### Folder overrides

```bash
# Override the 2nd level folder (org → custom name)
gitbox repo add github-work "MyOrg/myorg.github.io" --id-folder "myorg-rest"
# Clones to: ~/git/github-work/myorg-rest/myorg.github.io/

# Override the 3rd level folder (clone name)
gitbox repo add github-work "MyOrg/myorg.web" --clone-folder "website"
# Clones to: ~/git/github-work/MyOrg/website/

# Absolute path — replaces everything
gitbox repo add forgejo-homelab "myuser/my-config" --clone-folder "~/.config/my-config"
# Clones to: ~/.config/my-config/
```

### Listing and inspecting repos

```bash
gitbox repo list
gitbox repo list --source github-personal
gitbox repo show github-personal "MyGitHubUser/gitbox"
```

### Updating a repo

```bash
gitbox repo update github-personal "MyGitHubUser/private-repo" --credential-type gcm
gitbox repo update forgejo-homelab "infra/homelab" --id-folder "infra-prod"
```

### Deleting a repo

```bash
gitbox repo delete github-personal "MyGitHubUser/old-project"
```

---

## Folder structure

Repos are cloned into a three-level directory structure:

```text
~/00.git/                           ← global.folder
  <source-key>/                     ← 1st level (or source.folder if set)
    <org>/                          ← 2nd level (from repo key, or id_folder override)
      <repo>/                      ← 3rd level (from repo key, or clone_folder override)
```

Example with real data:

```text
~/00.git/
  git-example/                      ← source key
    personal/my-project/            ← org/repo
    infra/homelab/
    infra/migration/
  github-MyGitHubUser/             ← source key
    MyGitHubUser/gitbox/       ← org/repo
    external-org/ext-project/       ← cross-org
  github-myorg/
    MyOrg/myorg.browser/
    myorg-rest/myorg.github.io/     ← id_folder override
```

---

## Authentication

See [credentials.md](credentials.md) for credential types, required permissions, and storage.

```bash
# Set up credentials (idempotent — detects type and guides you)
gitbox account credential setup <account-key>

# Verify credentials work
gitbox account credential verify <account-key>

# Remove credentials
gitbox account credential del <account-key>
```

**CI/CD token override (environment variables):**

```bash
# Convention: GITBOX_TOKEN_<ACCOUNT_KEY> (uppercase, hyphens → underscores)
export GITBOX_TOKEN_MY_GITEA="ghp_your_token_here"
gitbox clone
```

Token resolution: env var `GITBOX_TOKEN_<KEY>` → `GIT_TOKEN` → OS keyring.

---

## Status monitoring

```bash
# All repos
gitbox status

# Filter by source
gitbox status --source github-personal

# JSON output (for scripting)
gitbox status --json
```

Status indicators:

| Symbol | Color  | State       | Meaning                 |
| ------ | ------ | ----------- | ----------------------- |
| `+`    | Green  | clean       | Up to date              |
| `!`    | Orange | dirty       | Uncommitted changes     |
| `<`    | Purple | behind      | Needs pull              |
| `>`    | Blue   | ahead       | Needs push              |
| `!`    | Red    | diverged    | Both ahead and behind   |
| `!`    | Red    | conflict    | Merge conflicts         |
| `o`    | Purple | not cloned  | Directory doesn't exist |
| `~`    | Orange | no upstream | No tracking branch      |
| `x`    | Red    | error       | Git error               |

When a repo is checked out on a non-default branch, a `[branch-name]` badge appears after the repo name. Repos on the default branch show no badge. If a repo is on a local branch with no upstream tracking, the detail shows "local branch" instead of "no upstream" — this is normal for feature branches and is not counted as an issue in account summaries.

The `--json` output includes `branch` (current branch name) and `is_default` (boolean) fields for each repo.

---

## Cloning

```bash
# Clone all configured repos (default — no flags needed)
gitbox clone

# Clone repos from a specific source only
gitbox clone --source github-personal

# Clone a specific repo only
gitbox clone --source github-personal --repo "MyGitHubUser/gitbox"
```

---

## Pulling

```bash
# Pull all repos that are behind (fast-forward only)
gitbox pull

# Pull from a specific source only
gitbox pull --source github-personal
```

Dirty or conflicted repos are skipped with a warning.

---

## Browsing

Open a repository's remote web page in the default browser:

```bash
# Open a specific repo
gitbox browse --repo alice/hello-world

# Narrow to a specific source
gitbox browse --source github-personal --repo alice/hello-world

# Output URL as JSON (without opening browser)
gitbox browse --repo alice/hello-world --json
```

| Flag       | Description                          |
| ---------- | ------------------------------------ |
| `--repo`   | Repository to open (required)        |
| `--source` | Restrict search to a specific source |

---

## Sweeping

Remove stale local branches across all configured repos:

```bash
# Preview what would be deleted (no changes)
gitbox sweep --dry-run

# Sweep all repos
gitbox sweep

# Sweep a specific source or repo
gitbox sweep --source github-personal
gitbox sweep --repo alice/hello-world
```

Three types of stale branches are detected:

| Type     | Meaning                                      | Delete mode             |
| -------- | -------------------------------------------- | ----------------------- |
| Gone     | Remote tracking branch was deleted           | `git branch -D` (force) |
| Merged   | Fully merged into the default branch         | `git branch -d` (safe)  |
| Squashed | Squash-merged or rebase-merged on the server | `git branch -D` (force) |

| Flag        | Description                          |
| ----------- | ------------------------------------ |
| `--dry-run` | List stale branches without deleting |
| `--source`  | Restrict to a specific source        |
| `--repo`    | Restrict to a specific repo          |

---

## Scanning

Scan walks the filesystem (no config required) and reports the sync status of every git repo it finds:

```bash
# Scan from current directory
gitbox scan

# Scan a specific directory
gitbox scan --dir ~/projects

# Scan and pull repos that are behind (fast-forward only)
gitbox scan --pull
```

Output uses colored one-liners with symbols: `+` ok, `!` dirty, `<` behind, `>` ahead, `x` error.

Unlike `status`, `scan` does not require a gitbox configuration — it works on any directory tree.

When a gitbox configuration exists and scanning inside the parent folder, repos are annotated as `[tracked]` or `[ORPHAN]` with account-matching hints and a summary count.

Orphan tags:

- `[ORPHAN → <account>]` — scan matched the orphan to a configured account.
- `[ORPHAN — local only]` — repo has no `origin` remote.
- `[ORPHAN — unknown account]` — no host-matching account is configured.
- `[ORPHAN — ambiguous: a | b]` — two or more accounts on the same host tie on every identity signal. Move the folder under the right source subtree, edit `gitbox.json`, or set `credential.<url>.username` in the repo to disambiguate, then re-scan.

To pick an account for an orphan, gitbox scores each host-matching account using the signals below (all values are additive, higher wins):

| Signal                                                                    | Score | Source                                                                |
| ------------------------------------------------------------------------- | ----- | --------------------------------------------------------------------- |
| Host match (required baseline)                                            | 1     | account URL hostname or SSH alias vs the parsed remote host           |
| Owner equals `account.username`                                           | +3    | owner segment of the remote URL path                                  |
| Repo lives under the account's source folder                              | +5    | first path component of the repo relative to the gitbox parent folder |
| HTTPS URL embeds `user@` where user equals `account.username`             | +10   | `url.User.Username()` of the origin remote                            |
| `.git/config` has `credential.<url>.username` equal to `account.username` | +10   | `git config --get-regexp '^credential\..*\.username$'` in the repo    |

If the top score is shared by two or more accounts (including a bare host-only tie) the match is marked ambiguous: no account is picked, no files are moved, and the scan and GUI surface the candidate list.

---

## Adopting

Adopt discovers orphan repos (not in `gitbox.json`) under the parent folder and brings them into the gitbox world:

```bash
# Interactive adoption — prompts for each orphan
gitbox adopt

# Preview what would happen
gitbox adopt --dry-run

# Adopt all matched orphans without prompting (relocations require interactive mode)
gitbox adopt --all
```

| Flag        | Description                                                                           |
| ----------- | ------------------------------------------------------------------------------------- |
| `--dry-run` | Show the adoption plan without making changes                                         |
| `--all`     | Adopt all matched orphans without prompting (adopts in place, does not auto-relocate) |

For each adopted repo, gitbox:

- Adds it to `gitbox.json` under the matched source
- Configures per-repo credential isolation (GCM, SSH, or token)
- Sets `user.name` and `user.email` from the account config
- Rewrites the remote URL to match the credential type (SSH or HTTPS)
- Optionally relocates the repo to the standard folder structure

Orphans with no matching account are listed with their remote URL and a suggestion to create the account first.

---

## Discovery

Discover queries a provider's API to find all repos visible to an account:

```bash
# Interactive — shows numbered list, you pick which to add
gitbox account discover my-forgejo

# Add all repos without prompting
gitbox account discover my-forgejo --all

# Exclude forks and archived repos
gitbox account discover my-forgejo --skip-forks --skip-archived

# JSON output (for scripting)
gitbox account discover my-forgejo --json
```

Discovery is **add-only** — it adds repos to your config but never removes them. If repos in your config are no longer found upstream, they're flagged as stale with a warning.

**Mirror discovery** scans all account pairs to detect existing mirror relationships:

```bash
# Scan and show results
gitbox mirror discover

# Scan and apply results to config
gitbox mirror discover --apply
```

Detection methods (in decreasing confidence): push mirror API (confirmed), pull mirror flag (likely), name match (possible). In the GUI, discovery shows a per-account progress bar and marks repos already in your config.

---

## Credential management

Set up, verify, and remove credentials for accounts:

```bash
# Set up credentials (idempotent — safe to re-run)
gitbox account credential setup <account-key>

# Verify credentials work
gitbox account credential verify <account-key>

# Remove credentials
gitbox account credential del <account-key>
```

`credential setup` is the recommended entry point — it detects the credential type and guides you through the setup. See [credentials.md](credentials.md) for details on each type, required permissions, and storage.

---

## System check (doctor)

Probe the host for every external tool gitbox may call and report what is installed, where, and which version.

```bash
# Human-readable table
gitbox doctor

# Machine-readable JSON (for scripts, bug reports)
gitbox doctor --json
```

Each row is marked `ok`, `missing` (required for your config), or `optional` (not needed by any account/workspace you have). When a tool is missing, doctor prints an install command for the current OS.

**Tools probed:** `git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add`, `tmux`, `tmuxinator`, `wsl` (on Windows).

**Exit codes:** `0` when every required tool is present, `1` when at least one required tool is missing.

The GUI exposes the same report via **Settings → System check → Run**. Both the GUI add-account flow and the TUI credential-type change refuse to start a credential setup if a required tool is missing — you see the install command first, instead of hitting a cryptic runtime error.

---

## Global gitignore

Install a curated set of OS-junk patterns (`.DS_Store`, `Thumbs.db`, `*~`, …) into `~/.gitignore_global` and point `core.excludesfile` at it, so per-project `.gitignore` files don't have to repeat them.

```bash
# Show whether the recommended block is installed and core.excludesfile is set.
gitbox gitignore check

# Install or refresh the recommended block (idempotent; backs up any existing
# file to ~/.gitignore_global.bak-YYYYMMDD-HHMMSS, rolling window of 3).
gitbox gitignore install

# Machine-readable output for both subcommands
gitbox gitignore check --json
gitbox gitignore install --json

# Verbose check: list every managed pattern that also lives outside the
# sentinel markers (duplicates that `install` will sanitise away).
gitbox gitignore check --verbose
```

The installed block is wrapped in sentinels (`# >>> gitbox:global-gitignore >>>` / `# <<< gitbox:global-gitignore <<<`); user-added patterns and comments outside the sentinels are preserved across re-runs. Negation patterns (`!.DS_Store`) survive sanitisation.

**Opt-out:** the automatic startup check is gated by `global.check_global_gitignore` in `gitbox.json` (defaults to `true`). Toggle from the GUI gear panel or the TUI settings screen. Explicit commands — `gitbox gitignore check|install`, pressing `G` in the TUI dashboard, clicking **Install** on the GUI banner — always run regardless of the preference.

The GUI shows a yellow banner whenever the file is missing, the block is stale, patterns are duplicated outside the sentinels, or `core.excludesfile` is unset. The TUI dashboard footer gets a bold red `G gitignore!` hint in the same states.

---

## Mirroring

Mirrors keep backup copies of repos on another provider. Repos are mirrored server-side via provider APIs — they are NOT cloned locally.

### Mirror groups

A mirror group pairs two accounts. Each repo in the group specifies direction (push/pull) and origin (src/dst):

```bash
# Create a mirror group
gitbox mirror add forgejo-github \
  --account-src my-forgejo \
  --account-dst github-personal

# List all mirror groups
gitbox mirror list

# Show details as JSON
gitbox mirror show forgejo-github

# Delete a mirror group
gitbox mirror delete forgejo-github
```

### Mirror repos

```bash
# Push from source to destination (source account is the origin)
gitbox mirror add-repo forgejo-github infra/homelab \
  --origin src --direction push

# Pull from destination into source (destination account is the origin)
gitbox mirror add-repo forgejo-github MyUser/dotfiles \
  --origin dst --direction pull

# Add and immediately set up via API
gitbox mirror add-repo forgejo-github infra/tools \
  --origin src --direction push --setup

# Remove a repo from a mirror group
gitbox mirror delete-repo forgejo-github infra/homelab
```

### Mirror setup

Run API setup for pending mirrors (creates target repos, configures push/pull mirrors):

```bash
# Set up all pending mirrors across all groups
gitbox mirror setup

# Set up all pending in one group
gitbox mirror setup forgejo-github

# Set up a specific repo
gitbox mirror setup forgejo-github --repo infra/homelab
```

### Mirror status

Check live sync state by comparing HEAD commits on both sides:

```bash
# All mirrors
gitbox mirror status

# Specific group
gitbox mirror status forgejo-github

# JSON output
gitbox mirror status --json
```

Status indicators:

| Symbol | Color  | State  | Meaning                          |
| ------ | ------ | ------ | -------------------------------- |
| `+`    | Green  | synced | HEAD commits match on both sides |
| `<`    | Purple | behind | Backup is behind origin          |
| `+`    | Green  | active | Mirror exists but can't compare  |
| `x`    | Red    | error  | API error or missing repo        |

A `⚠ backup repo is PUBLIC` warning appears if the backup repo is not private.

### Mirror credentials

Remote servers need portable PATs (not machine-local GCM tokens). See [credentials.md](credentials.md#mirror-credentials) for setup instructions.

### Automation matrix

| Scenario                 | Automatable             | Configured on |
| ------------------------ | ----------------------- | ------------- |
| Forgejo/Gitea push → any | Yes (push mirror API)   | Forgejo/Gitea |
| Forgejo/Gitea pull ← any | Yes (migrate API)       | Forgejo/Gitea |
| GitLab push → any        | Yes (remote mirror API) | GitLab        |
| GitHub push → any        | No (guide only)         | N/A           |
| Bitbucket push → any     | No (guide only)         | N/A           |

### Mirror config format

```json
{
  "mirrors": {
    "forgejo-github": {
      "account_src": "my-forgejo",
      "account_dst": "github-personal",
      "repos": {
        "infra/homelab": {
          "direction": "push",
          "origin": "src",
          "method": "api",
          "status": "active"
        },
        "MyUser/dotfiles": {
          "direction": "pull",
          "origin": "dst",
          "method": "api",
          "status": "active"
        }
      }
    }
  }
}
```

| Field                     | Type   | Required | Description                                               |
| ------------------------- | ------ | -------- | --------------------------------------------------------- |
| `account_src`             | string | Yes      | Source account key                                        |
| `account_dst`             | string | Yes      | Destination account key (must differ from src)            |
| `repos.<key>.direction`   | string | Yes      | `"push"` or `"pull"`                                      |
| `repos.<key>.origin`      | string | Yes      | `"src"` or `"dst"` — which account is the source of truth |
| `repos.<key>.target_repo` | string | No       | Override target repo name (default: same as key)          |
| `repos.<key>.method`      | string | No       | `"api"` or `"manual"`                                     |
| `repos.<key>.status`      | string | No       | `"active"`, `"pending"`, `"error"`, `"paused"`            |
| `repos.<key>.last_sync`   | string | No       | RFC3339 timestamp of last known sync                      |
| `repos.<key>.error`       | string | No       | Last error message                                        |

---

## Workspaces (read-only)

Workspaces are **read-only** in gitbox. It discovers existing VS Code `.code-workspace` files under the configured folders, caches them, lists them, and opens one in your editor. gitbox never creates, edits, generates, or deletes workspace files — you own them. (Tmuxinator support and all CRUD/generation were removed in v3.)

### Commands

```bash
gitbox workspace list                 # discovered workspaces (summary table)
gitbox workspace list --json          # machine-readable
gitbox workspace show <key>           # detail: file path + resolved members
gitbox workspace open <key>           # open the .code-workspace in the first global.editors entry
gitbox workspace discover             # rescan disk and refresh the cache
```

### Discover behaviour

`discover` walks `global.folder` and every `global.extra_folders` root for `*.code-workspace` files, resolves each file's folder references back to known clones (deepest-prefix match against resolved repo paths), and refreshes the cache in `gitbox.json`. It persists only when the set changed. The GUI runs it in a background goroutine at startup (the cached list shows instantly, then updates if anything changed); the TUI runs it on launch and on each periodic-sync tick.

### Workspace cache format

The `workspaces` section is a regenerable cache — it can be deleted safely and rediscovered. Entries are always discovered VS Code workspaces (no `type`/`layout`).

```json
{
  "workspaces": {
    "sumwall": {
      "name": "sumwall",
      "file": "/home/me/00.git/.../sumwall.project/sumwall.code-workspace",
      "members": [
        { "source": "github-org", "repo": "Org/browser" },
        { "source": "github-org", "repo": "Org/services" }
      ],
      "discovered": true
    }
  }
}
```

| Field        | Type   | Description                                                        |
| ------------ | ------ | ------------------------------------------------------------------ |
| `name`       | string | Display name (the `.code-workspace` filename stem)                 |
| `file`       | string | Absolute path to the discovered `.code-workspace` file             |
| `members`    | array  | Member clones resolved from the file's folders (`source` + `repo`) |
| `discovered` | bool   | Always `true` — entries are discovered, never authored             |

---

## Non-standard clone locations & multi-repo containers

The standard clone layout is `global.folder / <account> / <org|user> / repo`. gitbox also supports clones that live elsewhere and a "container" pattern where a main repo holds a dynamic set of nested clones inside its own working tree.

### Extra scan folders

`global.extra_folders` is a list of additional root directories scanned for clones and `.code-workspace` files. Use them to onboard repos outside the standard tree.

```bash
gitbox global update --add-folder ~/work/clients
gitbox global update --remove-folder ~/work/clients
gitbox adopt --path ~/some/other/tree     # one-off scan of an arbitrary folder
```

Clones found outside the standard layout are onboarded **in place** with an absolute `clone_folder` (gitbox never moves them).

### Multi-repo containers

A managed clone can be flagged a **container** — gitbox then descends into its working tree to discover and onboard nested clones you provisioned there with your own tooling (a clone-script, not submodules).

```bash
gitbox container <source-key> <repo-key>          # flag as container
gitbox container <source-key> <repo-key> --off    # clear the flag
gitbox global update --nested-depth 2             # how deep to descend (default 1)
gitbox adopt                                       # discover + onboard nested clones
```

Nested clones onboard as plain repos under their **real** account/org (matched by remote URL), each with an absolute `clone_folder` pointing inside the container — they are never relocated out of it. `nested_scan_depth` defaults to 1 (the container's immediate children); raise it to reach clones nested one or more levels deeper.

In the GUI, the same controls live in the repo detail panel (a "Multi-repo container" checkbox) and the change-root-folder dialog (extra folders + nested depth).

### Cloning into a custom folder

`gitbox clone --source <s> --repo <r> --clone-folder <dir>` stores an absolute `clone_folder` on that repo and clones it there. (Editing a repo's `clone_folder` directly, or `gitbox repo add --clone-folder`, achieves the same.)

---

## Shell completion

Generate tab-completion scripts for your shell:

```bash
# Bash
gitbox completion bash > /etc/bash_completion.d/gitbox

# Zsh
gitbox completion zsh > "${fpath[1]}/_gitbox"

# Fish
gitbox completion fish > ~/.config/fish/completions/gitbox.fish

# PowerShell
gitbox completion powershell > gitbox.ps1
```

See [completion.md](completion.md) for detailed setup instructions.

---

## Auto-update

Gitbox can check for and apply updates from GitHub releases.

```bash
gitbox update           # check and install interactively
gitbox update --check   # just check, no install (exit code 0 = up to date)
```

The updater downloads the platform-specific artifact, verifies the SHA256 checksum (if `checksums.sha256` is present in the release), and replaces the binaries in place. On Windows, the running binary is renamed to `.old` and cleaned up on the next startup.

The GUI checks for updates automatically once per 24 hours and shows a banner when a newer version is available.

---

## Configuration file reference

The config lives at `~/.config/gitbox/gitbox.json`. See [gitbox.jsonc](../json/gitbox.jsonc) for a fully annotated example.

**Automatic backups:** Every meaningful save creates a dated backup in the same directory (e.g., `gitbox-20260401-143025.json`). The 10 most recent are kept — older ones are pruned automatically. The GUI's corruption-recovery screen can restore from any of them in one click. Window-position-only saves (moving or resizing the GUI) skip the backup step, so real pre-corruption copies are not rotated out by cosmetic churn.

### Global

| Field                             | Type   | Required | Description                                                                                                                                                                                                                                                                                                                                                                                                                |
| --------------------------------- | ------ | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `folder`                          | string | Yes      | Root directory for all clones. Supports `~`.                                                                                                                                                                                                                                                                                                                                                                               |
| `extra_folders`                   | array  | No       | Additional root directories scanned for clones and `.code-workspace` files, on top of `folder`.                                                                                                                                                                                                                                                                                                                            |
| `nested_scan_depth`               | int    | No       | Levels gitbox descends below a container repo to find nested clones. Default `1` (immediate children).                                                                                                                                                                                                                                                                                                                     |
| `credential_ssh`                  | object | No       | SSH platform defaults. Presence indicates SSH is available.                                                                                                                                                                                                                                                                                                                                                                |
| `credential_ssh.ssh_folder`       | string | No       | SSH config directory. Default `~/.ssh`.                                                                                                                                                                                                                                                                                                                                                                                    |
| `credential_gcm`                  | object | No       | GCM platform defaults. Presence indicates GCM is available.                                                                                                                                                                                                                                                                                                                                                                |
| `credential_gcm.helper`           | string | No       | Credential helper. Typically `"manager"`.                                                                                                                                                                                                                                                                                                                                                                                  |
| `credential_gcm.credential_store` | string | No       | `"wincredman"`, `"keychain"`, or `"secretservice"`.                                                                                                                                                                                                                                                                                                                                                                        |
| `credential_token`                | object | No       | Token/PAT platform defaults. Presence indicates token auth is available.                                                                                                                                                                                                                                                                                                                                                   |
| `editors`                         | array  | No       | Code editors for the "Open in" menu. Auto-populated on first launch.                                                                                                                                                                                                                                                                                                                                                       |
| `editors[].name`                  | string | Yes      | Display name (e.g. `"VS Code"`).                                                                                                                                                                                                                                                                                                                                                                                           |
| `editors[].command`               | string | Yes      | Full path or command name (e.g. `"C:\\...\\code.cmd"`).                                                                                                                                                                                                                                                                                                                                                                    |
| `terminals`                       | array  | No       | Legacy v2.0 terminal-emulator list. Still read by the launcher when `terminal_profiles` is empty (v2.0→v2.1 transition). New configs prefer the trio below.                                                                                                                                                                                                                                                                |
| `terminals[].name`                | string | Yes      | Display name (e.g. `"Windows Terminal"`).                                                                                                                                                                                                                                                                                                                                                                                  |
| `terminals[].command`             | string | Yes      | Full path or on-PATH launcher (e.g. `"wt.exe"`, `"gnome-terminal"`).                                                                                                                                                                                                                                                                                                                                                       |
| `terminals[].args`                | array  | No       | Arguments passed before the path. Use `"{path}"` as the path placeholder; if absent, path is appended. Use `"{command}"` to mark where an AI harness argv is spliced (expands to zero items for terminal-only launches).                                                                                                                                                                                                   |
| `terminal_apps`                   | array  | No       | v2.1 detected terminal emulators. Populated by the catalog probe in `pkg/terminals` (Windows: Windows Terminal, WezTerm, Alacritty, Tabby, ConEmu, Hyper, Mintty, ZOC. macOS: iTerm2, Terminal.app, Warp, Kitty, Ghostty, WezTerm, Alacritty. Linux: GNOME Terminal, Konsole, Terminator, Foot, Alacritty, Kitty, Tilda, Guake, xterm). Both the GUI Manager and the TUI's `Settings → Terminal profiles…` probe directly. |
| `terminal_apps[].id`              | string | Yes      | Stable id (`"wt"`, `"wezterm"`, `"gnome-terminal"`, `"iterm"`, …). Used as the cross-reference target from `terminal_profiles[].terminal`.                                                                                                                                                                                                                                                                                 |
| `terminal_apps[].name`            | string | Yes      | Display name shown in the Manager and the per-row launcher.                                                                                                                                                                                                                                                                                                                                                                |
| `terminal_apps[].command`         | string | Yes      | Resolved absolute path (filled in at detect time).                                                                                                                                                                                                                                                                                                                                                                         |
| `terminal_apps[].args_template`   | array  | No       | Argv template with `{path}`, `{shell_command}`, `{shell_args}`, `{command}` tokens. The launcher expands these per Profile via `pkg/launch.ResolveArgs`. Same token rules as `terminals[].args` above, plus `{shell_command}` (the resolved shell binary) and `{shell_args}` (a splice point for the shell's default args).                                                                                                |
| `shells`                          | array  | No       | v2.1 detected shells. Catalog scope per OS — Windows: PowerShell 7, PowerShell 5, CMD, Git Bash, plus per-distro `wsl-<name>` rows when WSL is installed. macOS: Zsh, Bash, Fish, Dash. Linux: Bash, Zsh, Fish, Ksh, Dash.                                                                                                                                                                                                 |
| `shells[].id`                     | string | Yes      | Stable id (`"cmd"`, `"pwsh"`, `"git-bash"`, `"wsl-ubuntu"`, …). Cross-referenced by `terminal_profiles[].shell`.                                                                                                                                                                                                                                                                                                           |
| `shells[].name`                   | string | Yes      | Display name.                                                                                                                                                                                                                                                                                                                                                                                                              |
| `shells[].command`                | string | Yes      | Resolved absolute path.                                                                                                                                                                                                                                                                                                                                                                                                    |
| `shells[].args`                   | array  | No       | Default args spliced where the terminal's `args_template` references `{shell_args}`.                                                                                                                                                                                                                                                                                                                                       |
| `terminal_profiles`               | array  | No       | v2.1 launchable Profiles. The per-row launcher and the `Open in…` menu read this list when populated. **OS-aware composition** (issue #71): on Windows each auto-derived Profile pairs a Terminal × Shell; on macOS / Linux each is Terminal-only with the host's login shell as the implicit shell.                                                                                                                       |
| `terminal_profiles[].id`          | string | Yes      | Stable id (`"wt+pwsh"`, `"wezterm+launchmenu-mybash"`, `"user-1"`, …).                                                                                                                                                                                                                                                                                                                                                     |
| `terminal_profiles[].name`        | string | Yes      | Display name (e.g. `"Windows Terminal — pwsh"`).                                                                                                                                                                                                                                                                                                                                                                           |
| `terminal_profiles[].terminal`    | string | Yes      | `terminal_apps[].id` to launch. Empty only for the bare-shell fallback Profiles emitted on Windows when no modern Terminal is installed.                                                                                                                                                                                                                                                                                   |
| `terminal_profiles[].shell`       | string | No       | `shells[].id` to run inside the terminal. Empty means "use the terminal's default" — on macOS / Linux that is the host's login shell, displayed in the Manager as a dim badge next to the Terminal name.                                                                                                                                                                                                                   |
| `terminal_profiles[].args`        | array  | No       | Override argv. When empty the launcher uses `terminal_apps[].args_template`. WezTerm `launch_menu` rows store the full `start --cwd {path} -- <argv>` shape here.                                                                                                                                                                                                                                                          |
| `terminal_profiles[].default`     | bool   | No       | Marks the Profile invoked by the per-row launcher's primary action. Mutually exclusive across the list.                                                                                                                                                                                                                                                                                                                    |
| `terminal_profiles[].preferred`   | bool   | No       | Promotes the Profile to the kebab menu's quick list.                                                                                                                                                                                                                                                                                                                                                                       |
| `terminal_profiles[].hidden`      | bool   | No       | Suppresses the Profile from menus without deleting it. The only way to suppress an auto-detected / WT-imported / WezTerm-imported / migrated Profile (those reappear on the next detect cycle if removed).                                                                                                                                                                                                                 |
| `terminal_profiles[].source`      | string | No       | **Internal field** — never displayed in the Manager. Origin tag used by the engine to gate delete-vs-hide: only `"user"` Profiles are deletable; `"detected"` / `"wt-profile"` / `"wezterm-launchmenu"` / `"migrated"` rows can only be Hidden.                                                                                                                                                                            |
| `ai_harnesses`                    | array  | No       | AI CLI harnesses for the "Open in" menu. Auto-populated on first launch (claude, codex, gemini, aider, cursor-agent, opencode). Launched inside `global.terminals[0]`, which must contain `"{command}"` in its args.                                                                                                                                                                                                       |
| `ai_harnesses[].name`             | string | Yes      | Display name (e.g. `"Claude Code"`).                                                                                                                                                                                                                                                                                                                                                                                       |
| `ai_harnesses[].command`          | string | Yes      | Absolute path or on-PATH binary (e.g. `"claude"`).                                                                                                                                                                                                                                                                                                                                                                         |
| `ai_harnesses[].args`             | array  | No       | Optional extra args for the harness. Usually empty.                                                                                                                                                                                                                                                                                                                                                                        |

### Terminal Profiles

The v2.1 `terminal_apps[]` + `shells[]` + `terminal_profiles[]` trio is owned by `pkg/terminals`. The package ships a compiled-in catalog of supported Terminals + Shells per OS — that's the vocabulary gitbox knows how to detect and launch. Adding a new terminal-emulator entry is a code change in `pkg/terminals/catalog.go`.

On every launch the catalog probes the host and reconciles the result with what's already in `gitbox.json`:

- Catalog entries the host has installed are added to `terminal_apps[]` / `shells[]` (only if missing — existing rows survive across re-detect).
- Hidden flags survive across re-detect — hiding Mintty in this session keeps it hidden after upgrades that grow the catalog.
- User-added Profiles (`source: "user"`) and migrated v2.0 Profiles (`source: "migrated"`) are preserved verbatim, even when not in the freshly-detected set.
- Catalog-but-not-installed entries are skipped silently — they reappear automatically once the user installs the binary.

#### OS-aware composition

The auto-derived Profile set follows different rules per platform:

- **Windows** — Each Profile pairs a Terminal × Shell. Bare-shell auto-Profiles (a row whose Terminal is itself the shell) are not emitted when at least one modern Terminal is installed. When no modern Terminal is installed, gitbox falls back to one bare-shell Profile per shell so the user isn't stranded — and surfaces a banner in the Manager: "Install Windows Terminal for the best experience."
- **macOS / Linux** — Each Profile is Terminal-only (`terminal_profiles[].shell == ""`). The host's login shell is implicit — `pkg/launch.ResolveArgs` collapses the empty shell tokens to zero items, and the Manager renders the login shell as a dim metadata badge next to the Terminal name. Power users can still pair a Terminal with a non-login Shell via the Manager's `+ Add profile` form; the resulting row is stamped `source: "user"`.

The Add-Profile form mirrors these rules: on macOS / Linux the shell selector includes a `(login shell)` virtual entry as the default; on Windows the shell pick is mandatory.

#### How launch matching works

When I click a `WezTerm — PowerShell 7` or `Windows Terminal — PowerShell 7` Profile, gitbox does NOT just run the generic per-Terminal template. It first consults my own terminal config for a matching entry, and only falls back to the generic template when none is found.

The lookup runs at every launch (with an mtime-invalidated in-process cache, so re-edits to `wezterm.lua` / `settings.json` are picked up without restart):

- **WezTerm** — gitbox parses `wezterm.lua` (`$WEZTERM_CONFIG_FILE`, then `$XDG_CONFIG_HOME/wezterm/wezterm.lua`, then `~/.config/wezterm/wezterm.lua`, then `~/.wezterm.lua`) and looks up an entry of `config.launch_menu` whose label matches the gitbox shell. On hit, gitbox launches `wezterm-gui.exe start --cwd <path> -- <entry args>` and splices the entry's `set_environment_variables` on top of the parent env. The parser binds specifically to the documented `config.launch_menu` table — if my config stores entries in a custom `local profiles = { … }` variable driving a custom keybinding picker, gitbox cannot discover them and falls back to the generic template. To make custom-picker entries visible to gitbox, alias them with `config.launch_menu = profiles` at the end of `wezterm.lua` (one line, no behavioural impact on the existing keybinding). What gitbox does NOT reproduce is any Lua picker callback wired in `wezterm.lua` (per-entry `color_scheme`, `mux.spawn_window` overrides, `window-focus-changed` handlers, etc.) — those only fire when an entry is picked from WezTerm's own launcher menu, never when a pane is spawned externally.
- **Windows Terminal** — gitbox parses `settings.json` (Store install, Preview install, then unpackaged install under `%LOCALAPPDATA%`) and looks up a profile in `profiles.list` whose `name` matches the gitbox shell. On hit, gitbox runs `wt.exe -w 0 nt --profile "<name>" -d <path>` — `wt.exe` itself reads the profile's `commandline`, font, colors, and starting flags from `settings.json`. The `-w 0 nt` prefix pins the new tab to the most-recent existing WT window (or creates one if none exists) so a `firstWindowPreference: persistedWindowLayout` setting in `settings.json` doesn't spawn a second window beside ours when WT was closed with saved tabs.
- **No match / no config / terminal not installed** — gitbox falls back to the generic argv template (`wezterm-gui.exe start --cwd <path> -- <shell> <args>`, `wt.exe -d <path> <shell> <args>`, etc.). That's the right behaviour for shells I haven't wired into my terminal config.

Bare-shell DIRECT Profiles (the four hidden-by-default `pwsh / powershell / cmd / wsl` shortcuts on Windows) skip the lookup — they have no terminal config to consult, so the generic "run the shell directly" template is correct.

The shell-name matcher is forgiving:

- Direct match — the entry's normalised name equals the gitbox shell's display name (`"PowerShell 7"` ≡ `"PowerShell 7"`, `"WSL — Ubuntu-24.04"` ≡ `"WSL — Ubuntu-24.04"`).
- Em-dash suffix — for gitbox names like `"WSL — Ubuntu-24.04"`, an entry labelled just `"Ubuntu-24.04"` matches too.
- Pattern fallback — `pwsh` matches entries containing `"powershell 7"`, `"powershell core"`, or `"pwsh"`; `powershell` matches `"powershell 5"` or `"windows powershell"`; `cmd` matches `"command prompt"` or `"cmd exe"`; `git-bash` matches `"git bash"`; `wsl-<distro>` matches the bare distro slug (`"ubuntu 24 04"`).

### Account

| Field                     | Type    | Required    | Description                                                                 |
| ------------------------- | ------- | ----------- | --------------------------------------------------------------------------- |
| `provider`                | string  | Yes         | `"github"`, `"gitlab"`, `"gitea"`, `"forgejo"`, `"bitbucket"`, `"generic"`  |
| `url`                     | string  | Yes         | Server URL (scheme+host, no path).                                          |
| `username`                | string  | Yes         | Account username.                                                           |
| `name`                    | string  | Yes         | Default `git user.name`.                                                    |
| `email`                   | string  | Yes         | Default `git user.email`.                                                   |
| `default_credential_type` | string  | No          | Default auth: `"gcm"`, `"ssh"`, or `"token"`.                               |
| `ssh.host`                | string  | Conditional | SSH Host alias (e.g., `"gt-myuser"`). **Mandatory** when SSH is configured. |
| `ssh.hostname`            | string  | No          | Real SSH hostname. Auto-derived from URL if omitted.                        |
| `ssh.key_type`            | string  | Conditional | `"ed25519"` or `"rsa"`. **Mandatory** when SSH is configured.               |
| `gcm.provider`            | string  | No          | GCM provider hint.                                                          |
| `gcm.useHttpPath`         | boolean | No          | Scope credentials by HTTP path.                                             |

### Source

| Field     | Type   | Required | Description                                             |
| --------- | ------ | -------- | ------------------------------------------------------- |
| `account` | string | Yes      | References an account key.                              |
| `folder`  | string | No       | Override first-level clone folder. Default: source key. |

### Repo (within source.repos)

| Field             | Type   | Required | Description                                                               |
| ----------------- | ------ | -------- | ------------------------------------------------------------------------- |
| `credential_type` | string | No       | Override auth method. Inherits from account.                              |
| `name`            | string | No       | Override `git user.name`.                                                 |
| `email`           | string | No       | Override `git user.email`.                                                |
| `id_folder`       | string | No       | Override 2nd level dir (org folder).                                      |
| `clone_folder`    | string | No       | Override 3rd level dir. If absolute, replaces entire path.                |
| `container`       | bool   | No       | Mark as a multi-repo container; gitbox scans inside it for nested clones. |

---

## Troubleshooting

### Config not found

```bash
# Check where gitbox looks for the config
gitbox global config path

# Create a new config
gitbox init

# Or specify a custom path
gitbox status --config /path/to/my-config.json
```

### GCM opens the wrong browser account

Clear cached credentials:

- **Windows:** Control Panel > Credential Manager > remove `git:https://github.com` entries
- **macOS:** Keychain Access > search `github.com` > delete
- **Linux:** `secret-tool clear protocol https host github.com`

### SSH connection refused

```bash
ssh -T git@gh-YourAlias -v
# Check: key added to provider, correct IdentityFile, ssh-agent running
```

### GCM browser auth on headless/SSH

If GCM credential setup doesn't open a browser:

- **Check your environment:** On Linux, GCM browser auth needs a display server. Run `echo $DISPLAY` — if empty, you're in a headless session.
- **Run from a desktop terminal:** SSH into the machine from a desktop session with X forwarding (`ssh -X`) or run the command directly on the desktop.
- **Let GCM handle it later:** Skip browser setup. GCM will prompt interactively on the next `git clone` or `git fetch` if the credential isn't stored yet.
- **Use a token instead:** If browser auth isn't practical, switch the account to token credential type: `gitbox account credential setup <key> --token`.

### "Repository not found" on clone

- Verify the `org/repo` name matches the actual repo
- Verify your credentials: test with `git ls-remote <url>`
- For cross-org repos, make sure your account has access
