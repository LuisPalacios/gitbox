
<p align="center">
  <img src="../assets/screenshot-cli.png" alt="Gitbox" width="800" />
</p>

# Getting Started with gitbox CLI

This guide walks you through the complete workflow — from a fresh install to a fully managed multi-account Git environment.

<p align="center">
  <img src="diagrams/cli-workflow.png" alt="CLI Workflow" width="700" />
</p>

## Prerequisites

- **Git** installed and on your PATH
- **gitbox** binary — install with the one-liner or [download manually](https://github.com/LuisPalacios/gitbox/releases) or [build from source](developer-guide.md)
- For GCM accounts: [Git Credential Manager](https://github.com/git-ecosystem/git-credential-manager) installed. On Linux, GCM browser-based OAuth also needs a display server (X11 or Wayland) — see [credentials.md](credentials.md) for headless alternatives.

### Installing (macOS, Linux, Git Bash)

One command handles download, extraction, quarantine flags, and PATH setup:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/bootstrap.sh)
```

Use `--cli-only` to skip the GUI, `--version <tag>` for a specific release, or `--prefix <dir>` to change the install directory (default `~/bin`). See the [README](../README.md) for more examples.

## Step 1: Initialize

Create your configuration file:

```bash
gitbox init
```

This creates `~/.config/gitbox/gitbox.json` with sensible defaults for your platform. It auto-detects your credential store (Windows Credential Manager, macOS Keychain, etc.).

For tab-completion of commands and flags, see [Shell Completion](completion.md).

## Step 2: Add Accounts

An **account** defines WHO you are on a Git provider — your identity, not your repos.

### Forgejo / Gitea (self-hosted, GCM auth)

```bash
gitbox account add my-forgejo \
  --provider forgejo \
  --url https://git.example.org \
  --username myuser \
  --name "My Name" \
  --email "me@example.com" \
  --default-credential-type gcm \
  --gcm-provider generic
```

### GitHub (GCM auth)

```bash
gitbox account add github-personal \
  --provider github \
  --url https://github.com \
  --username MyGitHubUser \
  --name "My Name" \
  --email "me@example.com" \
  --default-credential-type gcm
```

### GitHub (SSH auth)

```bash
gitbox account add github-ssh \
  --provider github \
  --url https://github.com \
  --username SSHUser \
  --name "SSH User" \
  --email "sshuser@example.com" \
  --default-credential-type ssh
```

### GitHub (Token auth)

```bash
gitbox account add github-token \
  --provider github \
  --url https://github.com \
  --username TokenUser \
  --name "Token User" \
  --email "tokenuser@example.com" \
  --default-credential-type token
```

### Verify your accounts

```bash
gitbox account list
```

## Step 3: Set Up Credentials

Run `credential setup` for each account. It detects the credential type and does the right thing:

```bash
gitbox account credential setup my-forgejo
gitbox account credential setup github-personal
gitbox account credential setup github-ssh
```

The command is **idempotent** — run it again anytime to check or fix your setup.

For GCM accounts, the setup opens your browser for OAuth authentication (GitHub, GitLab) or prompts for username/password (Gitea, Forgejo). On headless or SSH sessions where no browser is available, gitbox tells you and suggests running from a desktop terminal instead. See [credentials.md](credentials.md) for details on each credential type, browser detection, and what permissions to select.

### Verify credentials

```bash
gitbox account credential verify my-forgejo
gitbox account credential verify github-personal
gitbox account credential verify github-ssh
```

## Step 4: Discover Repos

Discover fetches all repos visible to your account from the provider's API and lets you choose which ones to manage:

```bash
gitbox account discover my-forgejo
```

You'll see a numbered list:

```text
Discovered 12 repos:

  #     REPO                                                STATUS
  1     personal/my-project                                 (new)
  2     infra/homelab                                       (new)
  -     training/old-course                                 (already in source "my-forgejo")

Enter repos to add (e.g. 1,3,5-10 or "all", empty to cancel):
```

Type `all` to add everything, or pick specific numbers.

### Discover options

```bash
gitbox account discover my-forgejo --all            # Add all without prompting
gitbox account discover my-forgejo --skip-forks     # Exclude forks
gitbox account discover my-forgejo --skip-archived  # Exclude archived repos
gitbox account discover my-forgejo --json           # JSON output (for scripting)
```

### Discover all accounts

```bash
gitbox account discover github-personal
gitbox account discover github-ssh
```

## Step 5: Clone Everything

```bash
gitbox clone
```

You'll see colored, one-line-per-repo output with a progress bar for each clone:

```text
Cloning into ~/00.git
+ cloned    my-forgejo/personal/my-project
+ cloned    my-forgejo/infra/homelab
~ exists    github-personal/MyOrg/project-a
~ exists    github-personal/MyOrg/project-b

Cloned: 2, Skipped: 2, Errors: 0
```

### Clone options

```bash
gitbox clone --source my-forgejo    # Clone from one source only
gitbox clone --repo MyOrg/tools     # Clone a specific repo only
gitbox clone --verbose              # Show all repos including skipped
```

## Step 6: Day-to-Day

### Check status

```bash
gitbox status
```

Shows config info, account credential health, and per-repo sync state grouped by source.

### Pull updates

```bash
gitbox pull
```

Pulls repos that are behind (fast-forward only). Dirty or conflicted repos are skipped with a warning.

```bash
gitbox pull --verbose    # Show all repos including clean ones
gitbox pull --source my-forgejo  # Pull from one source only
```

### Scan any directory

```bash
gitbox scan
```

Walks the filesystem from the current directory, finds all git repos, and shows their sync status. Unlike `status`, this doesn't need a gitbox config — it works on any directory.

```bash
gitbox scan --dir ~/projects    # Scan a specific directory
gitbox scan --pull              # Also pull repos that are behind
```

## Step 7: Set Up Mirrors (Optional)

Mirrors let you keep backup copies of repos on another provider — for example, pushing from a homelab Forgejo to GitHub, or pulling GitHub repos into Forgejo.

### Create a mirror group

A mirror group pairs two accounts:

```bash
gitbox mirror add forgejo-github \
  --account-src my-forgejo \
  --account-dst github-personal
```

### Add repos to mirror

Each repo specifies which account is the source of truth (`--origin`) and the direction (`--direction`):

```bash
# Push from Forgejo to GitHub (Forgejo is the source)
gitbox mirror add-repo forgejo-github infra/homelab \
  --origin src --direction push --setup

# Pull from GitHub into Forgejo (GitHub is the source)
gitbox mirror add-repo forgejo-github MyUser/dotfiles \
  --origin dst --direction pull --setup
```

The `--setup` flag immediately creates the target repo and configures the mirror via API.

### Discover existing mirrors

If you already have mirror relationships set up on your servers, gitbox can detect them:

```bash
# Show discovered mirrors
gitbox mirror discover

# Discover and apply to config
gitbox mirror discover --apply
```

Detection uses three methods with decreasing confidence: push mirror API queries (confirmed), pull mirror flags (likely), and repo name matching (possible).

### Check mirror status

```bash
gitbox mirror status
```

Shows sync state (comparing HEAD commits on both sides) and warns if backup repos are not private.

### Mirror credentials

If your account uses GCM, mirrors need a separate PAT (GCM OAuth tokens are machine-local). Store one with:

```bash
gitbox account credential setup github-personal --token
```

Token and SSH accounts already have a portable PAT — no extra setup needed. See [credentials.md](credentials.md) for details.

## Quick Reference

| Command                                       | What it does                  |
| --------------------------------------------- | ----------------------------- |
| `gitbox init`                              | Create config file            |
| `gitbox account list`                      | List all accounts             |
| `gitbox account add <key> ...`             | Add an account                |
| `gitbox account credential setup <key>`    | Set up credentials            |
| `gitbox account credential verify <key>`   | Verify credentials            |
| `gitbox account discover <key>`            | Discover repos from provider  |
| `gitbox source list`                       | List all sources              |
| `gitbox repo list`                         | List all repos                |
| `gitbox clone`                             | Clone configured repos        |
| `gitbox status`                            | Show sync status              |
| `gitbox pull`                              | Pull repos that are behind    |
| `gitbox scan`                              | Scan filesystem for git repos |
| `gitbox migrate --source ... --target ...` | Migrate v1 config to v2       |
| `gitbox mirror list`                       | List all mirror groups        |
| `gitbox mirror add <key> ...`              | Create a mirror group         |
| `gitbox mirror add-repo <key> <repo> ...`  | Add a repo to mirror          |
| `gitbox mirror setup [<key>]`              | Run API setup for mirrors     |
| `gitbox mirror status [<key>]`             | Check live mirror sync status |
| `gitbox mirror discover [--apply]`         | Detect existing mirrors       |
| `gitbox completion <shell>`                | Generate shell completion     |

## What's Next

- See the [Reference Guide](reference.md) for all commands, config format, and troubleshooting
- See [Credentials](credentials.md) for detailed PAT creation instructions per provider
- See the [Architecture](architecture.md) for technical design and component details
- See the [Migration Guide](migration.md) if coming from `git-config-repos.sh`
