# Gitbox Desktop — User Guide

Gitbox is a desktop app that helps you keep all your Git projects organized and up to date, even when you work with multiple accounts on GitHub, GitLab, Forgejo, and other providers.

This guide walks you through everything from first launch to day-to-day use.

## Getting Started

### Download and Install

Download the app from the [Releases](https://github.com/LuisPalacios/gitbox/releases) page. Pick the right file for your system:

- **Windows** — `gitbox-win-amd64.zip`
- **macOS (Apple Silicon)** — `gitbox-macos-arm64.zip`
- **Linux** — `gitbox-linux-amd64.zip`

Extract the ZIP and run **Gitbox** (or `Gitbox.exe` on Windows).

> **macOS note:** The app is not signed by Apple. After extracting, open a terminal and run `xattr -cr /path/to/Gitbox.app` to allow it to launch.

### First Launch — Choosing a Folder

The first time you open Gitbox, it asks you to pick a **clone folder** — this is where all your projects will live on disk. Something like `~/00.git` or `C:\repos` works well.

Click **Get started** and you're in.

## Adding Your First Account

An account tells Gitbox who you are on a particular server. For example, your GitHub account, or your company's GitLab.

Click the **+** card to add one. You'll fill in:

1. **Account key** — a short name you choose (e.g., `github-personal`). This also becomes the folder name on disk.
2. **Provider** — pick your service (GitHub, GitLab, Gitea, Forgejo, or Bitbucket).
3. **URL** — the server address. For GitHub this is `https://github.com`.
4. **Username** — your account name on that service.
5. **Name and Email** — the identity used in your Git commits.
6. **Credential type** — how Gitbox will authenticate (see below).

### Setting Up Credentials

After creating your account, Gitbox needs a way to log in to your provider. There are three options:

- **GCM (Git Credential Manager)** — The easiest option. Gitbox opens your browser so you can log in. Best for GitHub and GitLab.
- **Token (Personal Access Token)** — You create a token on your provider's website and paste it into Gitbox. The app tells you exactly which URL to visit and what permissions to select.
- **SSH** — Gitbox generates a key pair for you. You copy the public key and add it to your provider's settings. The app gives you the direct link.

Once credentials are set up, the account card shows a **green badge** with the credential type — you're good to go.

## Understanding Account Cards

Each account appears as a card on the main screen. Here's what the elements mean:

- **Credential badge** (top right) — shows your credential type with a colored background:
  - **Green** — everything is working
  - **Orange** — there's a minor issue (e.g., limited permissions)
  - **Red** — the credential is broken or expired
  - **Blue "config"** — no credential set up yet; click it to get started
- **Sync ring** — a small circle showing how many of your projects are in sync
- **Find projects** — discovers repos from your account (disabled if credentials aren't working)

If a credential is missing or broken, the entire card turns **light red** so you notice right away.

## Finding and Adding Projects

Click **Find projects** on an account card. Gitbox contacts your provider and lists all the repositories visible to your account.

The discovery window features:

- **Search field** — type to filter the list when you have many repos
- **Alphabetical sorting** — repos are listed A to Z for easy browsing
- **Select all** — check the box to select everything visible (respects your filter)
- **Already added** — repos you've already added appear dimmed and can't be selected again

Pick the ones you want, then click **Add & Pull**. Gitbox saves them to your config and starts cloning them into your folder.

## Editing an Account

Click the account name on any card to open the edit screen. You can change:

- **Account key** — if you rename it, Gitbox takes care of everything: it renames the folder on disk, updates your SSH keys and config, migrates stored tokens, and fixes all internal references.
- **Provider** — in case you picked the wrong one originally.
- **All other fields** — URL, username, name, email, default branch.

## Managing Credentials

Click the credential badge on a card to open the credential management screen.

### Changing Credential Type

Use the dropdown to switch between GCM, Token, and SSH. Click **Change & setup** to reconfigure. The button is greyed out if the selected type matches your current working credential — there's nothing to change.

If the current credential is broken, the button stays active so you can fix it by re-running setup.

### Deleting a Credential

Click the red **Delete credential** button to remove all stored authentication data. This is useful when you need a clean start — for example, if a token expired or you want to switch to a completely different authentication method.

After deleting, the card turns red and the badge shows "config". Click it to set up a fresh credential.

## Keeping Projects in Sync

### Automatic Checking

Gitbox watches your projects and shows their status:

- **Synced** (green) — up to date with the remote
- **Behind** (magenta) — the remote has new commits you can pull
- **Local changes** (orange) — you have uncommitted work
- **Ahead** (blue) — you have commits that haven't been pushed
- **Not local** (grey) — the repo hasn't been cloned yet

### Sync All

Click the **Sync All** button in the top bar to bring everything up to date in one click. It clones missing repos and pulls repos that are safely behind (skipping anything with local changes).

### Periodic Sync

In Settings, you can enable automatic sync every 5, 15, or 30 minutes. Gitbox fetches updates and re-checks credential health in the background.

### Viewing Details

Click a repo that shows local changes, conflicts, or other issues. An expandable panel appears showing:

- The current branch and how many commits you're ahead or behind
- A list of every changed file with icons showing what happened (added, deleted, renamed, modified)
- Any untracked files

This detail view **updates automatically** when Gitbox detects new changes — you don't need to close and reopen it.

## Deleting Repos and Accounts

Click the **trash icon** in the top bar to enter delete mode. Red X buttons appear on account cards and repo rows. Click one to remove it. Account deletion also removes its source and local clone folders.

Exit delete mode by clicking the trash icon again.

## Settings

Click the **gear icon** to open the settings panel:

- **Config** — shows the path to your config file with an "Open in Editor" button
- **Clone folder** — change where projects are stored
- **Periodic sync** — automatic fetch interval (off, 5m, 15m, 30m)
- **Version** — current app version

## Tips

- **Window position** — Gitbox remembers your window size and position. If you disconnect a secondary monitor and the window would open off-screen, it automatically centers on your main display.
- **External edits** — if you edit `gitbox.json` by hand (or via the CLI), the GUI picks up changes automatically when the window regains focus.
- **Same config** — the desktop app and the CLI tool (`gitboxcmd`) share the same config file. Changes in one are visible in the other.

## See Also

- [CLI Quick Start](cli-guide.md) — for terminal users
- [Configuration Reference](reference.md) — detailed config format and commands
- [Architecture](architecture.md) — technical design
