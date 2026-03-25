# Reference Guide

Complete reference for all gitboxcmd commands, configuration format, folder structure, and troubleshooting.

For a quick walkthrough, see the [CLI Quick Start](cli-guide.md) first.

## Overview

gitbox helps you manage Git repositories across multiple accounts and providers (GitHub, GitLab, Gitea, Forgejo, Bitbucket) from a single configuration file and a unified interface.

Two binaries are available:

- **`gitboxcmd`** (CLI) — For power users and headless servers. Runs anywhere Git runs.
- **`gitbox`** (GUI) — For desktop users who prefer a visual interface. Modern, mouse-driven.

Both read the same configuration file at `~/.config/gitbox/gitbox.json`.

The configuration has two main sections:

- **`accounts`** — WHO you are on each server (credentials, identity)
- **`sources`** — WHAT you clone from each account (repos organized by org/repo)

---

## Installation

### Windows

Download the latest release from the [Releases page](https://github.com/LuisPalacios/gitbox/releases):

- `gitboxcmd-windows-amd64.exe` — CLI
- `gitbox-windows-amd64.exe` — GUI

Place them in a directory on your `PATH` (e.g., `C:\Users\<you>\bin`).

**Prerequisites:** [Git for Windows](https://gitforwindows.org/) must be installed.

### macOS

```bash
curl -LO https://github.com/LuisPalacios/gitbox/releases/latest/download/gitboxcmd-darwin-arm64.tar.gz
tar xzf gitboxcmd-darwin-arm64.tar.gz
sudo mv gitboxcmd gitbox /usr/local/bin/
```

**Prerequisites:** Git (via Xcode Command Line Tools or Homebrew).

### Linux

```bash
curl -LO https://github.com/LuisPalacios/gitbox/releases/latest/download/gitboxcmd-linux-amd64.tar.gz
tar xzf gitboxcmd-linux-amd64.tar.gz
sudo mv gitboxcmd /usr/local/bin/
# GUI only if you have a desktop environment:
sudo mv gitbox /usr/local/bin/
```

**Prerequisites:** Git. For the GUI, WebKitGTK is required (`sudo apt install libwebkit2gtk-4.1-dev` on Debian/Ubuntu).

---

## First-Time Setup

### Option 1: Interactive init (recommended)

```bash
gitboxcmd init
```

This creates `~/.config/gitbox/gitbox.json` with your global settings (root folder, credential store). It auto-detects your OS for the credential store.

Then add your accounts, sources, and repos:

```bash
# Add an account (who you are on a server)
gitboxcmd account add my-github \
  --provider github \
  --url https://github.com \
  --username YourUser \
  --name "Your Name" \
  --email "you@example.com" \
  --default-credential-type gcm

# Add a source (what you clone from that account)
gitboxcmd source add my-github --account my-github

# Add repos (org/repo format)
gitboxcmd repo add my-github "YourUser/my-project"
gitboxcmd repo add my-github "YourUser/dotfiles"
```

### Option 2: Migrate from git-config-repos.sh (v1)

If you already have a `~/.config/git-config-repos/git-config-repos.json`:

```bash
# Preview what the migration will produce
gitboxcmd migrate --dry-run

# Run the actual migration
gitboxcmd migrate
```

The original v1 file is never modified. Both tools can coexist.

### Option 3: Launch the GUI

```bash
gitbox
```

The setup wizard will guide you through creating accounts and discovering repos.

---

## Account Management

An account represents WHO you are on a git server — one unique `(hostname, username)` pair.

### Adding accounts

```bash
# GitHub personal
gitboxcmd account add github-personal \
  --provider github \
  --url https://github.com \
  --username MyGitHubUser \
  --name "My Name" \
  --email "myuser@example.com" \
  --default-credential-type gcm

# Forgejo homelab (SSH — host and key-type are mandatory)
gitboxcmd account add forgejo-homelab \
  --provider forgejo \
  --url https://forge.mylab.lan \
  --username myuser \
  --name "My Name" \
  --email "myuser@mylab.lan" \
  --default-credential-type ssh \
  --ssh-host gt-myuser \
  --ssh-key-type ed25519

# GitLab
gitboxcmd account add gitlab-work \
  --provider gitlab \
  --url https://gitlab.com \
  --username youruser \
  --name "Your Name" \
  --email "you@company.com"
```

### Listing and inspecting accounts

```bash
gitboxcmd account list
gitboxcmd account show github-personal
gitboxcmd account show github-personal --json
```

### Updating an account

Only the flags you specify are changed:

```bash
gitboxcmd account update github-personal --name "New Name" --email "new@email.com"
```

### Deleting an account

An account can only be deleted if no sources reference it:

```bash
gitboxcmd account delete github-personal
# Error: cannot delete — referenced by source "github-personal"

gitboxcmd source delete github-personal
gitboxcmd account delete github-personal  # now succeeds
```

---

## Source Management

A source represents WHAT you clone from an account. Each source references one account and contains a list of repos.

### Adding a source

```bash
gitboxcmd source add github-personal --account github-personal
gitboxcmd source add forgejo-homelab --account forgejo-homelab
```

By default, the source key is used as the first-level clone folder. To override:

```bash
gitboxcmd source add my-server --account forgejo-homelab --folder "server-repos"
```

### Listing sources

```bash
gitboxcmd source list
gitboxcmd source list --account github-personal
```

---

## Repo Management

Repos use `org/repo` format. The org part becomes the second-level folder, the repo part becomes the third-level (clone) folder.

### Adding repos

```bash
# Simple — inherits credential type from account default
gitboxcmd repo add github-personal "MyGitHubUser/gitbox"
gitboxcmd repo add github-personal "MyGitHubUser/dotfiles"

# Cross-org — access another org's repo with your credentials
gitboxcmd repo add github-personal "other-org/their-repo"

# Multiple orgs on same server (Forgejo/Gitea)
gitboxcmd repo add forgejo-homelab "infra/homelab"
gitboxcmd repo add forgejo-homelab "infra/migration"
gitboxcmd repo add forgejo-homelab "personal/my-project"

# Override credential type for a specific repo
gitboxcmd repo add github-personal "MyGitHubUser/private-repo" --credential-type ssh
```

### Folder overrides

```bash
# Override the 2nd level folder (org → custom name)
gitboxcmd repo add github-work "MyOrg/myorg.github.io" --id-folder "myorg-rest"
# Clones to: ~/git/github-work/myorg-rest/myorg.github.io/

# Override the 3rd level folder (clone name)
gitboxcmd repo add github-work "MyOrg/myorg.web" --clone-folder "website"
# Clones to: ~/git/github-work/MyOrg/website/

# Absolute path — replaces everything
gitboxcmd repo add forgejo-homelab "myuser/my-config" --clone-folder "~/.config/my-config"
# Clones to: ~/.config/my-config/
```

### Listing and inspecting repos

```bash
gitboxcmd repo list
gitboxcmd repo list --source github-personal
gitboxcmd repo show github-personal "MyGitHubUser/gitbox"
```

### Updating a repo

```bash
gitboxcmd repo update github-personal "MyGitHubUser/private-repo" --credential-type gcm
gitboxcmd repo update forgejo-homelab "infra/homelab" --id-folder "infra-prod"
```

### Deleting a repo

```bash
gitboxcmd repo delete github-personal "MyGitHubUser/old-project"
```

---

## Folder Structure

Repos are cloned into a three-level directory structure:

```text
~/git/                              ← global.folder
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

## Authentication Setup

gitbox supports three authentication methods. Choose based on your environment:

| Method | Best for | How it works |
| ------ | -------- | ------------ |
| **GCM** | Desktops (Windows, macOS, Linux with GUI) | Browser-based OAuth, credentials in OS keystore |
| **SSH** | Headless servers, advanced users | SSH key pairs with host aliases |
| **Token** | CI/CD, automation, Gitea/Forgejo | Personal Access Token in OS keystore |

Set the default per account:

```bash
gitboxcmd account add my-server \
  --provider forgejo \
  --url https://forge.lan \
  --username myuser \
  --name "My Name" \
  --email "myuser@lan" \
  --default-credential-type ssh    # all repos use SSH unless overridden
```

Override per repo:

```bash
gitboxcmd repo add my-server "infra/special" --credential-type token
```

### Token (PAT) setup

Tokens are stored in the OS keyring via [go-keyring](https://github.com/zalando/go-keyring) — **never in the config file**.

```bash
# 1. Create account with token auth
gitboxcmd account add my-gitea \
  --provider gitea \
  --url https://gitea.example.org \
  --username myuser \
  --name "My Name" \
  --email "myuser@example.org" \
  --default-credential-type token

# 2. Set up credentials (shows PAT creation URL + required scopes, stores in OS keyring)
gitboxcmd account credential setup my-gitea

# 3. Verify credentials work
gitboxcmd account credential verify my-gitea

# 4. Remove if needed
gitboxcmd account credential del my-gitea
```

**CI/CD usage (environment variables):**

```bash
# Convention: GITBOX_TOKEN_<ACCOUNT_KEY> (uppercase, hyphens → underscores)
export GITBOX_TOKEN_MY_GITEA="ghp_your_token_here"
gitboxcmd clone
```

**Token resolution priority:**

1. Account-specific env var (`GITBOX_TOKEN_<KEY>`)
2. Generic `GIT_TOKEN` env var (single-account CI fallback)
3. OS keyring (via go-keyring)

### Credential store by platform

- **Windows:** `wincredman` (Windows Credential Manager)
- **macOS:** `keychain` (Keychain Access)
- **Linux:** `secretservice` (GNOME Keyring / KWallet)

---

## Status Monitoring

```bash
# All repos
gitboxcmd status

# Filter by source
gitboxcmd status --source github-personal

# JSON output (for scripting)
gitboxcmd status --json
```

Status indicators:

| Symbol | Color  | State      | Meaning                 |
| ------ | ------ | ---------- | ----------------------- |
| `+`    | Green  | clean      | Up to date              |
| `!`    | Orange | dirty      | Uncommitted changes     |
| `<`    | Purple | behind     | Needs pull              |
| `>`    | Blue   | ahead      | Needs push              |
| `!`    | Orange | diverged   | Both ahead and behind   |
| `x`    | Red    | conflict   | Merge conflicts         |
| `o`    | Purple | not cloned | Directory doesn't exist |
| `~`    | Cyan   | no upstream| No tracking branch      |

---

## Cloning

```bash
# Clone all configured repos (default — no flags needed)
gitboxcmd clone

# Clone repos from a specific source only
gitboxcmd clone --source github-personal

# Clone a specific repo only
gitboxcmd clone --source github-personal --repo "MyGitHubUser/gitbox"
```

---

## Pulling

```bash
# Pull all repos that are behind (fast-forward only)
gitboxcmd pull

# Pull from a specific source only
gitboxcmd pull --source github-personal
```

Dirty or conflicted repos are skipped with a warning.

---

## Scanning

Scan walks the filesystem (no config required) and reports the sync status of every git repo it finds:

```bash
# Scan from current directory
gitboxcmd scan

# Scan a specific directory
gitboxcmd scan --dir ~/projects

# Scan and pull repos that are behind (fast-forward only)
gitboxcmd scan --pull
```

Output uses colored one-liners with symbols: `+` ok, `!` dirty, `<` behind, `>` ahead, `x` error.

Unlike `status`, `scan` does not require a gitbox configuration — it works on any directory tree.

---

## Discovery

Discover queries a provider's API to find all repos visible to an account:

```bash
# Interactive — shows numbered list, you pick which to add
gitboxcmd account discover my-forgejo

# Add all repos without prompting
gitboxcmd account discover my-forgejo --all

# Exclude forks and archived repos
gitboxcmd account discover my-forgejo --skip-forks --skip-archived

# JSON output (for scripting)
gitboxcmd account discover my-forgejo --json
```

Discovery is **add-only** — it adds repos to your config but never removes them. If repos in your config are no longer found upstream, they're flagged as stale with a warning.

---

## Credential Management

Set up, verify, and remove credentials for accounts:

```bash
# Set up credentials (idempotent — safe to re-run)
gitboxcmd account credential setup <account-key>

# Verify credentials work
gitboxcmd account credential verify <account-key>

# Remove credentials
gitboxcmd account credential del <account-key>
```

`credential setup` is the recommended entry point — it detects the credential type (token, GCM, SSH) and guides you through the setup. See [credentials.md](credentials.md) for provider-specific PAT creation instructions.

---

## Shell Completion

Generate tab-completion scripts for your shell:

```bash
# Bash
gitboxcmd completion bash > /etc/bash_completion.d/gitboxcmd

# Zsh
gitboxcmd completion zsh > "${fpath[1]}/_gitboxcmd"

# Fish
gitboxcmd completion fish > ~/.config/fish/completions/gitboxcmd.fish

# PowerShell
gitboxcmd completion powershell > gitboxcmd.ps1
```

See [completion.md](completion.md) for detailed setup instructions.

---

## Migration from v1

```bash
# Preview
gitboxcmd migrate --dry-run

# Execute
gitboxcmd migrate
```

See [migration.md](migration.md) for details on what changes and how accounts are deduplicated.

---

## Configuration File Reference

The config lives at `~/.config/gitbox/gitbox.json`. See [gitbox.jsonc](../gitbox.jsonc) for a fully annotated example.

### Global

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `folder` | string | Yes | Root directory for all clones. Supports `~`. |
| `credential_ssh` | object | No | SSH platform defaults. Presence indicates SSH is available. |
| `credential_ssh.ssh_folder` | string | No | SSH config directory. Default `~/.ssh`. |
| `credential_gcm` | object | No | GCM platform defaults. Presence indicates GCM is available. |
| `credential_gcm.helper` | string | No | Credential helper. Typically `"manager"`. |
| `credential_gcm.credential_store` | string | No | `"wincredman"`, `"keychain"`, or `"secretservice"`. |
| `credential_token` | object | No | Token/PAT platform defaults. Presence indicates token auth is available. |

### Account

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `provider` | string | Yes | `"github"`, `"gitlab"`, `"gitea"`, `"forgejo"`, `"bitbucket"`, `"generic"` |
| `url` | string | Yes | Server URL (scheme+host, no path). |
| `username` | string | Yes | Account username. |
| `name` | string | Yes | Default `git user.name`. |
| `email` | string | Yes | Default `git user.email`. |
| `default_branch` | string | No | Default branch (e.g., `"main"`). |
| `default_credential_type` | string | No | Default auth: `"gcm"`, `"ssh"`, or `"token"`. |
| `ssh.host` | string | Conditional | SSH Host alias (e.g., `"gt-myuser"`). **Mandatory** when SSH is configured. |
| `ssh.hostname` | string | No | Real SSH hostname. Auto-derived from URL if omitted. |
| `ssh.key_type` | string | Conditional | `"ed25519"` or `"rsa"`. **Mandatory** when SSH is configured. |
| `gcm.provider` | string | No | GCM provider hint. |
| `gcm.useHttpPath` | boolean | No | Scope credentials by HTTP path. |

### Source

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `account` | string | Yes | References an account key. |
| `folder` | string | No | Override first-level clone folder. Default: source key. |

### Repo (within source.repos)

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `credential_type` | string | No | Override auth method. Inherits from account. |
| `name` | string | No | Override `git user.name`. |
| `email` | string | No | Override `git user.email`. |
| `id_folder` | string | No | Override 2nd level dir (org folder). |
| `clone_folder` | string | No | Override 3rd level dir. If absolute, replaces entire path. |

---

## Troubleshooting

### Config not found

```bash
# Check where gitbox looks for the config
gitboxcmd global config path

# Create a new config
gitboxcmd init

# Or specify a custom path
gitboxcmd status --config /path/to/my-config.json
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

### "Repository not found" on clone

- Verify the `org/repo` name matches the actual repo
- Verify your credentials: test with `git ls-remote <url>`
- For cross-org repos, make sure your account has access
