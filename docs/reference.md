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

| Flag | Description |
| --- | --- |
| `--repo` | Repository to open (required) |
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

| Type | Meaning | Delete mode |
| --- | --- | --- |
| Gone | Remote tracking branch was deleted | `git branch -D` (force) |
| Merged | Fully merged into the default branch | `git branch -d` (safe) |
| Squashed | Squash-merged or rebase-merged on the server | `git branch -D` (force) |

| Flag | Description |
| --- | --- |
| `--dry-run` | List stale branches without deleting |
| `--source` | Restrict to a specific source |
| `--repo` | Restrict to a specific repo |

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

| Signal | Score | Source |
| --- | --- | --- |
| Host match (required baseline) | 1 | account URL hostname or SSH alias vs the parsed remote host |
| Owner equals `account.username` | +3 | owner segment of the remote URL path |
| Repo lives under the account's source folder | +5 | first path component of the repo relative to the gitbox parent folder |
| HTTPS URL embeds `user@` where user equals `account.username` | +10 | `url.User.Username()` of the origin remote |
| `.git/config` has `credential.<url>.username` equal to `account.username` | +10 | `git config --get-regexp '^credential\..*\.username$'` in the repo |

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

| Flag | Description |
| --- | --- |
| `--dry-run` | Show the adoption plan without making changes |
| `--all` | Adopt all matched orphans without prompting (adopts in place, does not auto-relocate) |

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

| Symbol | Color  | State  | Meaning                           |
| ------ | ------ | ------ | --------------------------------- |
| `+`    | Green  | synced | HEAD commits match on both sides  |
| `<`    | Purple | behind | Backup is behind origin           |
| `+`    | Green  | active | Mirror exists but can't compare   |
| `x`    | Red    | error  | API error or missing repo         |

A `⚠ backup repo is PUBLIC` warning appears if the backup repo is not private.

### Mirror credentials

Remote servers need portable PATs (not machine-local GCM tokens). See [credentials.md](credentials.md#mirror-credentials) for setup instructions.

### Automation matrix

| Scenario | Automatable | Configured on |
| -------- | ----------- | ------------- |
| Forgejo/Gitea push → any | Yes (push mirror API) | Forgejo/Gitea |
| Forgejo/Gitea pull ← any | Yes (migrate API) | Forgejo/Gitea |
| GitLab push → any | Yes (remote mirror API) | GitLab |
| GitHub push → any | No (guide only) | N/A |
| Bitbucket push → any | No (guide only) | N/A |

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

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `account_src` | string | Yes | Source account key |
| `account_dst` | string | Yes | Destination account key (must differ from src) |
| `repos.<key>.direction` | string | Yes | `"push"` or `"pull"` |
| `repos.<key>.origin` | string | Yes | `"src"` or `"dst"` — which account is the source of truth |
| `repos.<key>.target_repo` | string | No | Override target repo name (default: same as key) |
| `repos.<key>.method` | string | No | `"api"` or `"manual"` |
| `repos.<key>.status` | string | No | `"active"`, `"pending"`, `"error"`, `"paused"` |
| `repos.<key>.last_sync` | string | No | RFC3339 timestamp of last known sync |
| `repos.<key>.error` | string | No | Last error message |

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

**Automatic backups:** Every time the config is saved, a dated backup is created in the same directory (e.g., `gitbox-2026-04-01.json`). A rolling 5-day history is maintained — older backups are pruned automatically.

### Global

| Field                             | Type   | Required | Description                                                              |
| --------------------------------- | ------ | -------- | ------------------------------------------------------------------------ |
| `folder`                          | string | Yes      | Root directory for all clones. Supports `~`.                             |
| `credential_ssh`                  | object | No       | SSH platform defaults. Presence indicates SSH is available.              |
| `credential_ssh.ssh_folder`       | string | No       | SSH config directory. Default `~/.ssh`.                                  |
| `credential_gcm`                  | object | No       | GCM platform defaults. Presence indicates GCM is available.              |
| `credential_gcm.helper`           | string | No       | Credential helper. Typically `"manager"`.                                |
| `credential_gcm.credential_store` | string | No       | `"wincredman"`, `"keychain"`, or `"secretservice"`.                      |
| `credential_token`                | object | No       | Token/PAT platform defaults. Presence indicates token auth is available. |
| `editors`                         | array  | No       | Code editors for the "Open in" menu. Auto-populated on first launch.     |
| `editors[].name`                  | string | Yes      | Display name (e.g. `"VS Code"`).                                         |
| `editors[].command`               | string | Yes      | Full path or command name (e.g. `"C:\\...\\code.cmd"`).                   |
| `terminals`                       | array  | No       | Terminal emulators for the "Open in" menu. Auto-populated on first launch. |
| `terminals[].name`                | string | Yes      | Display name (e.g. `"Windows Terminal"`).                                 |
| `terminals[].command`             | string | Yes      | Full path or on-PATH launcher (e.g. `"wt.exe"`, `"gnome-terminal"`).      |
| `terminals[].args`                | array  | No       | Arguments passed before the path. Use `"{path}"` as the path placeholder; if absent, path is appended. Use `"{command}"` to mark where an AI harness argv is spliced (expands to zero items for terminal-only launches). |
| `ai_harnesses`                    | array  | No       | AI CLI harnesses for the "Open in" menu. Auto-populated on first launch (claude, codex, gemini, aider, cursor-agent, opencode). Launched inside `global.terminals[0]`, which must contain `"{command}"` in its args. |
| `ai_harnesses[].name`             | string | Yes      | Display name (e.g. `"Claude Code"`). |
| `ai_harnesses[].command`          | string | Yes      | Absolute path or on-PATH binary (e.g. `"claude"`). |
| `ai_harnesses[].args`             | array  | No       | Optional extra args for the harness. Usually empty. |

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

| Field             | Type   | Required | Description                                                |
| ----------------- | ------ | -------- | ---------------------------------------------------------- |
| `credential_type` | string | No       | Override auth method. Inherits from account.               |
| `name`            | string | No       | Override `git user.name`.                                  |
| `email`           | string | No       | Override `git user.email`.                                 |
| `id_folder`       | string | No       | Override 2nd level dir (org folder).                       |
| `clone_folder`    | string | No       | Override 3rd level dir. If absolute, replaces entire path. |

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
