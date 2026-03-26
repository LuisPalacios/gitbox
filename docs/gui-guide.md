# GUI Guide

The desktop app (`gitbox`) provides a visual interface built with **Wails v2 + Svelte**, sharing the same Go library (`pkg/`) and config file as the CLI.

## Download

Grab the latest build from the [Releases](https://github.com/LuisPalacios/gitbox/releases) page.

> **macOS:** The app is not signed. Run `xattr -cr /path/to/gitbox.app` after extracting.

## First Run — Onboarding

On first launch, gitbox shows a welcome screen asking you to choose a **clone folder** (where repos will be stored). Pick a path like `~/00.git` and click **Get started**. This creates `~/.config/gitbox/gitbox.json`.

If you already have a config from the CLI, the GUI picks it up automatically — no onboarding needed.

## Main Screen

The top bar shows:

- **Sync ring** — how many repos are synced out of total
- **Sync All** — clones missing repos and pulls repos that are behind (one click)
- **Delete mode** (trash icon) — toggle to remove accounts or repos
- **Theme** — cycle between System / Light / Dark
- **Settings** (gear icon) — expand the settings panel

### Settings Panel

Click the gear to see:

| Setting | What it shows |
| --- | --- |
| Config | Path to `gitbox.json`, with an "Open in Editor" button |
| Clone folder | Current folder, with a "Change" button |
| Theme | System / Light / Dark toggle |
| Accounts | Number of configured accounts |
| Version | App version and commit |

## Account Cards

Each account appears as a card showing:

- Provider icon and account key
- Username and server URL
- Credential status (green = OK, yellow = warning, red = failed)
- Repo count (synced / total)

### Adding an Account

Click the **+ Add Account** button to open the wizard. Fill in:

1. **Account key** — a short identifier (e.g., `github-personal`)
2. **Provider** — GitHub, GitLab, Gitea, Forgejo, or Bitbucket
3. **URL** — server URL (e.g., `https://github.com`)
4. **Username, Name, Email** — your git identity
5. **Credential type** — GCM, SSH, or Token

After adding, the app guides you through credential setup.

### Credential Setup

Click the credential status badge on an account card to set up or fix credentials:

- **GCM**: Opens your browser for OAuth login
- **Token**: Shows the URL to create a PAT with the required scopes, then stores it in your OS keyring
- **SSH**: Generates a key pair and shows the public key to register at your provider

### Deleting an Account

Click the trash icon in the top bar to enter delete mode. Each account card shows an X button — click it to remove that account and its sources.

## Repo Discovery

Click the **Discover** button on an account card. The app fetches all repos visible to your account from the provider API and shows them with checkboxes. Select the repos you want to manage, then click **Add Selected**.

## Repo List

Expand an account card to see its repos grouped by source. Each repo shows:

- Sync status (cloned, not cloned, ahead, behind, dirty, conflict)
- Color-coded status indicator

Right-click or use the action menu on a repo to clone, pull, or remove it.

## Sync All

The **Sync All** button in the top bar:

1. Clones repos that haven't been cloned yet
2. Pulls repos that are behind upstream (fast-forward only)
3. Skips dirty or conflicted repos

## Same Config as the CLI

The GUI reads and writes the same `~/.config/gitbox/gitbox.json` as `gitboxcmd`. Changes made in one are immediately visible in the other. External edits to the config file are picked up automatically when the window regains focus.

## See Also

- [CLI Quick Start](cli-guide.md) — for terminal users
- [Credentials](credentials.md) — detailed credential setup per provider
- [Architecture](architecture.md) — technical design
