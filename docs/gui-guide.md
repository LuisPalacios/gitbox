
<p align="center">
  <img src="../assets/screenshot-gui.png" alt="Gitbox" width="800" />
</p>

# Gitbox Desktop — User Guide

Gitbox is a desktop app that helps you keep all your Git projects organized and up to date, even when you work with multiple accounts on GitHub, GitLab, Forgejo, and other providers.

This guide walks you through everything from first launch to day-to-day use.

## Prerequisites

Download the installer for your platform from the [Releases](https://github.com/LuisPalacios/gitbox/releases) page:

- **Windows** — `gitbox-win-amd64-setup.exe` (installer with PATH setup and Start Menu shortcuts)
- **macOS** — `gitbox-macos-arm64.dmg` or `gitbox-macos-amd64.dmg` (open DMG, run the install script from Terminal)
- **Linux** — `gitbox-linux-amd64.AppImage` (self-contained, just download and run)

Alternatively, download the ZIP archives (`gitbox-<platform>-<arch>.zip`) and extract manually.

> **macOS note:** The app is not signed by Apple. The DMG includes an "Install Gitbox" script that copies the binaries and removes quarantine flags automatically. Run `bash "/Volumes/gitbox/Install Gitbox.command"` from Terminal. For manual install, use `xattr -cr /path/to/GitboxApp.app` and `xattr -cr /path/to/gitbox`.

### Linux AppImage

Download the AppImage, make it executable, and run:

```bash
chmod +x gitbox-linux-amd64.AppImage
./gitbox-linux-amd64.AppImage
```

The GUI requires a desktop environment with a display server (X11 or Wayland).

## Step 1: First launch

The first time you open Gitbox, it asks you to pick a **root folder** — this is where all your projects will live on disk. Something like `~/00.git` or `C:\repos` works well.

Click **Get started** and you're in.

## Step 2: Add accounts

An account tells Gitbox who you are on a particular server. For example, your GitHub account, or your company's GitLab.

Click the **+** card to add one. You'll fill in:

1. **Account key** — a short name you choose (e.g., `github-personal`). This also becomes the folder name on disk.
2. **Provider** — pick your service (GitHub, GitLab, Gitea, Forgejo, or Bitbucket).
3. **URL** — the server address. For GitHub this is `https://github.com`.
4. **Username** — your account name on that service.
5. **Name and Email** — the identity used in your Git commits.
6. **Credential type** — how Gitbox will authenticate (see below).

### Setting up credentials

After creating your account, Gitbox needs a way to log in to your provider. There are three options:

- **GCM (Git Credential Manager)** — The easiest option. Gitbox opens your browser so you can log in. Best for GitHub and GitLab.
- **Token (Personal Access Token)** — You create a token on your provider's website and paste it into Gitbox. The app tells you exactly which URL to visit and what permissions to select.
- **SSH** — Gitbox generates a key pair for you. You copy the public key and add it to your provider's settings. The app gives you the direct link.

Once credentials are set up, the account card shows a **green badge** with the credential type — you're good to go. For more details on each type and what permissions to select, see [credentials.md](credentials.md).

## Step 3: Find and add projects

Click **Find projects** on an account card. Gitbox contacts your provider and lists all the repositories visible to your account.

The discovery window features:

- **Search field** — type to filter the list when you have many repos
- **Alphabetical sorting** — repos are listed A to Z for easy browsing
- **Select all** — check the box to select everything visible (respects your filter)
- **Already added** — repos you've already added appear dimmed and can't be selected again

Pick the ones you want, then click **Add & Pull**. Gitbox saves them to your config and starts cloning them into your folder.

## Step 4: Day-to-day

### Understanding account cards

Each account appears as a card on the **Accounts** tab. Here's what the elements mean:

- **Credential badge** (top right) — shows your credential type with a colored background:
  - **Green** — everything is working
  - **Orange** — there's a minor issue (e.g., limited permissions)
  - **Red** — the credential is broken or expired
  - **Blue "config"** — no credential set up yet; click it to get started
- **Sync ring** — a small circle showing how many of your projects are in sync
- **Find projects** — discovers repos from your account (disabled if credentials aren't working)
- **Create repo** — creates a new repository on the provider (disabled if credentials aren't working)

If a credential is missing or broken, the entire card turns **light red** so you notice right away.

### Keeping projects in sync

#### Automatic Checking

Gitbox watches your projects and shows their status:

- **Synced** (green) — up to date with the remote
- **Behind** (magenta) — the remote has new commits you can pull
- **Local changes** (orange) — you have uncommitted work
- **Ahead** (blue) — you have commits that haven't been pushed
- **Not local** (grey) — the repo hasn't been cloned yet
- **Local branch** (green) — on a feature branch with no upstream tracking (normal)
- **No upstream** (grey) — default branch has no upstream tracking (needs attention)

When a repo is checked out on a non-default branch, a small branch badge appears next to the repo name (e.g., `feature-xyz`). Repos on the default branch show no badge. Detached HEAD state shows a red `detached` badge.

#### Pull All

Click the **Pull All** button (down-arrow icon) in the top bar to bring everything up to date in one click. It clones missing repos and pulls repos that are safely behind (skipping anything with local changes).

#### Fetch All

Click the **Fetch All** button (↻ icon) to check all remotes for new commits without pulling. This updates the status indicators so you can see what's changed before deciding to pull.

#### Periodic Fetch

In Settings, you can enable automatic fetch every 5, 15, or 30 minutes. Gitbox checks all remotes and re-checks credential health in the background.

#### Viewing Details

Click a repo that shows local changes, conflicts, or other issues. An expandable panel appears showing:

- The current branch and how many commits you're ahead or behind
- A list of every changed file with icons showing what happened (added, deleted, renamed, modified)
- Any untracked files

This detail view **updates automatically** when Gitbox detects new changes — you don't need to close and reopen it.

### Adopting orphan repos

The orphans modal lists clones under your parent folder that aren't yet in `gitbox.json`, grouped by how Gitbox can handle them:

- **Ready to adopt** — Gitbox matched the clone to an account using its remote URL, the repo's `credential.<url>.username`, or the folder it lives under. Check the box and click **Adopt** to register it (and optionally relocate it to the canonical path).
- **Unknown account** — no configured account matches the remote host. Add an account first, then re-open the modal.
- **Unknown account, `ambiguous: a | b`** — two or more accounts on the same host tie on every identity signal Gitbox looks at. The checkbox is disabled so no files are moved. To disambiguate: move the clone under the correct source subtree, edit `gitbox.json` to reflect the intended account, or set `credential.<url>.username` in the clone, then re-open the modal.
- **Local only** — no `origin` remote, not adoptable.

### Creating repositories

Click **Create repo** on an account card to create a new repository directly on the provider without leaving Gitbox.

The modal asks for:

- **Owner** — a dropdown listing your personal username plus any organizations you belong to. The provider API determines which organizations are available.
- **Name** — the repository name. Invalid characters are stripped automatically (only `a-z`, `A-Z`, `0-9`, `.`, `_`, `-` allowed). Spaces are converted to hyphens as you type.
- **Description** — an optional one-line summary.
- **Private** — checked by default. Uncheck to create a public repo.
- **Clone after creating** — checked by default. When enabled, Gitbox adds the repo to your config and clones it immediately.

The button text changes based on the clone checkbox: **Create & Clone** or **Create**.

Repo creation is supported on all providers (GitHub, GitLab, Gitea, Forgejo, and Bitbucket) and works with all credential types. The same API token used for discovery is used for creation.

### Editing an account

Click the account name on any card to open the edit screen. You can change:

- **Account key** — if you rename it, Gitbox takes care of everything: it renames the folder on disk, updates your SSH keys and config, migrates stored tokens, and fixes all internal references.
- **Provider** — in case you picked the wrong one originally.
- **All other fields** — URL, username, name, email, default branch.

### Managing credentials

Click the credential badge on a card to open the credential management screen. For details on each credential type and what permissions they need, see [credentials.md](credentials.md).

#### Changing Credential Type

Use the dropdown to switch between GCM, Token, and SSH. Click **Setup** to apply the change. gitbox removes the old credential and its artifacts, sets up the new one, and reconfigures all existing clones automatically.

#### Deleting a Credential

When viewing the current credential type, click the red **Delete** button to remove all stored authentication data. This is useful when you need a clean start — for example, if a token expired or you want to start fresh.

After deleting, the card turns red and the badge shows "config". Click it to set up a fresh credential.

## Step 5: Mirrors (optional)

Mirrors keep backup copies of repos on another provider — for example, pushing from a homelab Forgejo to GitHub. Repos are mirrored server-side via provider APIs, not cloned locally.

### Accounts and mirrors tabs

The main screen uses two tabs above the cards section:

- **Accounts** (default) — shows account cards and the repo list underneath. This is where you manage accounts, discover projects, and create repos.
- **Mirrors** — shows mirror group cards and the mirror detail list underneath. Each mirror group appears as a card with a sync ring showing the active/total ratio.

Switch tabs by clicking the tab buttons. The **summary footer** at the bottom always shows both repo and mirror counts regardless of which tab is active.

### Mirror cards

Each mirror group card shows:

- **Status dot** — green if all repos are active, red if errors exist, amber otherwise
- **MIRROR label** and account pair (e.g., `forgejo ↔ github`)
- **Sync ring** — ratio of active mirrors to total mirrors in the group
- **Check status** button — verifies sync state by comparing HEAD commits on both sides
- A **+** card is always visible on the Mirrors tab to create a new mirror group

### Mirror health ring

When mirrors are configured, a second **health ring** appears in the top bar next to the repo sync ring. It shows `active/total` mirrors and turns red if any mirrors have errors.

### Mirror actions

The Mirrors tab provides two section-level buttons:

- **Discover** — scans all account pairs to detect existing mirror relationships. During scanning, a progress bar shows per-account progress (indeterminate during repo listing, determinate during analysis). When results appear, repos already in your config are marked as **"configured"** and dimmed. Each unconfigured result has an individual **+ Add** button to add it to your config one by one, or use **Apply to config** to add all at once.
- **Check all** — checks sync status for every mirror group.

### Mirror detail list

Below the mirror cards, each group expands into a detail list showing individual mirrored repos with:

- Direction label (e.g., `origin → backup (mirror)`)
- Sync status (Synced OK, Backup is behind origin, etc.)
- Warning icon if the backup repo is public
- **Setup** button for pending repos that haven't been configured yet via API
- **+ Repo** button to add new repos to the group

## Step 6: Workspaces (optional)

The **Workspaces** tab next to Accounts and Mirrors groups N clones into a single artifact (`.code-workspace` file or tmuxinator YAML) that opens them together. Create one from the tab's `+ New workspace` button or by ticking clones on the Accounts tab and using `Create workspace from selected`. See the [CLI guide](cli-guide.md#step-8-dynamic-workspaces-optional) for the backend model — the GUI uses the same config format.

### Auto-discovery on startup

Whenever I drop a `*.code-workspace` file under the gitbox-managed folder by hand — or carry one over from another machine — the GUI picks it up on the next launch and adopts it into `gitbox.json` with `discovered: true`. Same for `~/.tmuxinator/*.yml` files. The Workspaces tab shows the new entry with no extra action from me.

The tab also has a **Discover** button that re-runs the scan on demand. The scan resolves each parsed folder path back to a known clone using a deepest-prefix match. Workspaces with at least one ambiguous member (a path that ties between two clones) are flagged separately and never auto-adopted — open the tab and pick the right candidate by hand.

### Tmuxinator on Windows

Windows users with WSL installed get the same tmuxinator support as macOS / Linux: gitbox writes the YAML to the WSL-side `~/.tmuxinator/<key>.yml` (through its `\\wsl.localhost\…` UNC path) and `Open` launches the configured terminal running `wsl.exe -- tmuxinator start <key>`. Without WSL, tmuxinator workspaces remain unsupported and surface a clear error.

## Dashboard views

### Full view

The full dashboard shows the top bar with health rings, the tab bar (Accounts/Mirrors), cards, repo or mirror detail lists, and the summary footer. Action buttons in the top bar include Pull All, Fetch All, Delete mode, and Compact view.

### Compact view

Click the **◧** button in the top bar to switch to compact mode — a narrow status strip (~220px wide) that shows:

- **Global health ring** — overall sync percentage and count
- **Account pills** — one per account with a mini ring and issue count. Click to expand and see individual repos underneath
- **Mirror pill** — when mirrors are configured, shows active/total count with a colored dot
- **Theme toggle** and a **Full view** button at the bottom

This is useful when you want gitbox visible as a sidebar while working in other apps. Click **◧ Full view** to return to the full dashboard.

## Settings and maintenance

### Settings panel

Click the **gear icon** to open the settings panel:

- **Config** — shows the path to your config file with an "Open in Editor" button
- **Root folder** — where projects are stored, with a "Change" button
- **Theme** — switch between System, Light, and Dark
- **Periodic fetch** — automatic fetch interval (off, 5m, 15m, 30m)
- **Run at startup** — launch Gitbox automatically when you log in (platform dependent)
- **System check** — **Run** opens a report of every external tool gitbox uses (git, Git Credential Manager, ssh, tmux, …), where it's installed, its version, and — for anything missing that your config needs — an install command. Same data as `gitbox doctor` on the CLI.
- **Version** — current app version
- **Author** — project author and link to the GitHub repository

The add-account and change-credential flows run the same check automatically: if you pick the `gcm` credential type on a machine that doesn't have Git Credential Manager installed, you get a yellow banner with the install command instead of a cryptic authentication failure later on.

### Clone actions

Each cloned repo row has a **kebab menu (⋮)** on the right side. The menu is split into three sections so the items you use most aren't buried behind scrolling:

1. **Always visible** — `🌐 Open in browser` and `📁 Open folder`.
2. **Defaults** — one entry per category, using the first config entry as the default: `>_ Open in <terminals[0]>`, `✎ Open in <editors[0]>`, `🤖 Open in <ai_harnesses[0]>`. An entry is hidden when that category has zero configured items.
3. **Submenus** — `Terminals ▸`, `Editors ▸`, `AI Harnesses ▸`. Each submenu only appears when the category has **two or more** entries — with just one, the default already covers it. Click the submenu to expand (not hover), click another submenu to switch, click outside or pick an item to close everything.

Below the submenus:

- **🧹 Sweep branches** — finds and deletes stale local branches (gone, merged, or squash-merged). Shows a confirmation dialog with the list of branches before deleting anything.

To change which terminal/editor/harness appears as the top-level default, reorder the array in `gitbox.json` — the first entry is always the default. No separate flag required.

The list of detected terminals covers Windows Terminal, PowerShell 7/5, Git Bash, WSL, and Command Prompt on Windows; Terminal, iTerm, and Warp on macOS; gnome-terminal, Konsole, Kitty, Alacritty, Xfce Terminal, and Terminator on Linux. Editors cover VS Code, Cursor, Zed, and anything else discoverable on `PATH`. AI harnesses (Claude Code, Codex, Gemini, Aider, Cursor Agent, OpenCode) run inside the first configured terminal — see [AI harness actions](#ai-harness-actions) below.

### Account actions

Each source group in the repo list has a **kebab menu (⋮)** on the right side of its header (the account title above the list of clones). The account kebab uses the **same structure and icons** as the repo-row kebab — top-level defaults, per-category submenus, same hide rules — scoped to the account's parent folder (`<global.folder>/<account-key>`) rather than a single clone:

- **🌐 Open in browser** — opens the provider profile/org page for the account (e.g. `https://github.com/<username>`, the GitLab group page, the Gitea/Forgejo user page).
- **📁 Open folder** — opens the account's parent folder in the OS file manager. The folder is the natural workspace root for cross-repo greps, multi-repo edits, or shell loops. If the folder doesn't exist yet (nothing cloned under that account), the action errors silently — clone at least one repo first.
- **>_ Open in \<terminal\>**, **✎ Open in \<editor\>**, **🤖 Open in \<AI harness\>** — same default-first entries as the repo kebab, plus the category submenus when you have multiple options configured. Sweep branches is dropped here — it's meaningful only on a specific clone.

In compact view, hovering an account pill reveals the same folder / editor / terminal / AI harness shortcuts as small icons on the right side, matching the compact repo-row behavior.

Editors are auto-detected on startup by scanning PATH. Gitbox writes the detected editors to `global.editors` in your config file with their full paths. You can reorder entries or add custom editors by editing the config — the menu always reflects the config order.

Terminals follow the same pattern: detected on startup per platform and written to `global.terminals` with their command and argument templates. Each entry has a `name`, a `command` (absolute path or on-PATH launcher) and `args`. Use the literal token `{path}` inside `args` to mark where the repo path is injected; if the token is absent, the path is appended as the final argument. Edit or reorder freely — the order in the menu matches the order in the config.

On Windows, bare shell entries (`cmd.exe`, `powershell.exe`, `pwsh.exe`, `wsl.exe`) have empty `args` — the launcher wraps them in `cmd.exe /C start "" /D <path>`, which gives each terminal a fresh console and sets the starting directory.

#### Opening a specific Windows Terminal profile

When Windows Terminal is installed, gitbox auto-discovers your WT profiles and rewrites `global.terminals` to mirror the WT menu: one entry per visible profile, in the same order as `profiles.list`, each launching `wt.exe --profile "<name>" -d "{path}"`. The shell opens with the exact profile you tuned in WT (colors, font, starting directory, oh-my-posh, specific WSL distro) — bare-binary launches (`pwsh.exe`, `powershell.exe`, `wsl.exe`, `cmd.exe`, `git-bash.exe`) always fall back to WT's *default* profile and miss that tuning, so they're dropped from `global.terminals` whenever WT discovery succeeds.

Discovery runs at startup and on every config sync. Renaming, adding, hiding, or disabling a profile in WT is picked up on the next launch — stale entries are pruned so the menu never drifts from WT itself. A profile is excluded when its `hidden` flag is `true` or when its `source` appears in WT's top-level `disabledProfileSources` (e.g. Visual Studio dynamic profiles disabled wholesale).

Existing customizations are preserved when the entry's `name` matches a current visible profile: if you previously added `--maximized` or another flag to a profile entry, gitbox keeps your `command` and `args` intact and only restores entries it removed. Entries whose name doesn't match any visible profile are dropped, by design — that's how the legacy `Windows Terminal` / `PowerShell 7` / `WSL` / `Command Prompt` entries get cleaned up automatically on the first sync after upgrading.

Locations checked, in order: `%LOCALAPPDATA%\Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json` (Store), `…\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\…` (Preview), `%LOCALAPPDATA%\Microsoft\Windows Terminal\settings.json` (unpackaged). If none parse — file missing, malformed JSON, no `profiles.list` — gitbox falls back to the bare-binary entries so something always works.

If you want to override an auto-discovered entry (rename it, point at a different profile, add `--maximized`), edit `gitbox.json` directly. Format:

```json
{
    "name": "WSL — Ubuntu",
    "command": "C:\\Users\\<you>\\AppData\\Local\\Microsoft\\WindowsApps\\wt.exe",
    "args": [
        "--profile",
        "Ubuntu 24.04.1 LTS",
        "-d",
        "{path}"
    ]
}
```

Notes:

- `--profile "<name>"` takes the profile's display name *verbatim*, including version suffixes like `Ubuntu 24.04.1 LTS` or the exact `PowerShell 7` spelling configured in WT's settings.
- `-d "{path}"` sets the starting directory.
- Routing through `wt.exe --profile` also sidesteps the Git Bash env-leak quirk described below — WT starts the shell from its own stored profile context, so any MSYS-form env vars inherited by the GUI don't reach the shell.

#### Launching gitbox from Git Bash (developer note)

If you launch `GitboxApp.exe` from a Git Bash / MSYS2 shell, Windows environment variables inherited by the GUI come through in posix form (e.g. `LOCALAPPDATA=/c/Users/you/AppData/Local`). Those values propagate into terminals opened from gitbox via the default `cmd.exe /C start …` path, and tools that read them as Windows paths — `oh-my-posh`, some `$PROFILE` helpers — can choke (`& '/c/Users/...' — not recognized as a cmdlet`). Gitbox sanitises the env block it hands to the spawned terminal, but when Windows Terminal is the default console host, WT's delegation path can bypass that block.

Two equally clean fixes:

- **Launch `GitboxApp.exe` from Explorer, the Start Menu, or a pinned shortcut** — anywhere Windows originates a clean env. End users never hit this, so production behaviour is unaffected.
- **Switch the affected terminal entry to the `wt.exe --profile "<name>" -d "{path}"` form** shown above. WT starts the shell from its own profile context, which has clean Windows env regardless of how the GUI was started.

In **compact mode**, the clone actions appear as small icon buttons (browser, folder, editor, terminal, and AI harness) that show on hover over each repo row. Only the first configured editor, terminal, and AI harness are shown — switch to full view for the complete list.

### AI harness actions

AI CLI harnesses (Claude Code, Codex, Gemini, Aider, Cursor Agent, OpenCode, …) are interactive shell processes — they need a terminal to run in. Gitbox adds one **Open in \<harness\>** entry per configured harness to both the repo kebab and the source-header (account) kebab. Clicking an entry launches the first configured terminal in the target folder and spawns the harness inside it.

I tell gitbox which terminal to use by ordering `global.terminals`: the first entry is the host terminal. To switch hosts, move a different entry to the top of the array.

The host terminal must support launching a command. In the args template, this is marked with the literal token `"{command}"` — at launch time, gitbox splices the harness argv in place of that token. Auto-detected templates (Windows Terminal profiles, gnome-terminal, konsole, alacritty, kitty, and similar) include `{command}` by default so harness launches work without config edits. For terminal-only launches the token expands to zero items, so the same entry serves both paths.

If the first terminal can't launch a command (missing `{command}` in args), clicking an AI harness entry shows an actionable error: "*\<name\>* in global.terminals[0] doesn't support launching a command. Add `{command}` to its args, or reorder global.terminals so a compatible entry is first." Shell-only launchers like bare `pwsh.exe`, `cmd.exe`, `wsl.exe`, `git-bash.exe`, and `open -a Terminal.app` fall into this category — route harnesses through a WT profile (Windows), a GUI terminal with a command flag (Linux), or the upcoming macOS follow-up.

Harnesses are auto-detected on PATH at startup. Gitbox writes detected entries to `global.ai_harnesses` with the resolved binary path. Each entry has a `name` (display in the menu), a `command` (binary path or on-PATH name), and an optional `args` array for harness-specific flags (e.g. `["--model", "sonnet-4.6"]`). Most harnesses need no flags — `args` is usually empty.

The set of harnesses gitbox tries to auto-detect is maintained as a markdown table embedded into the binary. The authoritative list lives at [`pkg/harness/tools-directory.md`](../pkg/harness/tools-directory.md) — to add or remove a detected harness, edit that file. A row is auto-detected when its `Category` is `Agentic CLI`, `AI Harness`, `Headless Harness`, `Agentic IDE`, or `Agentic IDE / CLI`, and its `Executable / CLI Command` cell contains a single backticked identifier (e.g. `` `claude` ``, `` `aider` ``, `` `cursor` ``, `` `cursor-agent` ``). Framework, orchestrator, and cloud-platform rows are documented for reference but skipped by the detector — they don't launch from a terminal in a folder. Agentic IDEs (Cursor, Windsurf) are treated as AI tools, not editors: the "Open in Cursor" entry will therefore appear under the AI harness section of the menu, not the editor section.

In the account kebab, the same entries appear with identical ordering — the only runtime difference is that the working directory is `<global.folder>/<account-key>` (the account's parent folder) instead of a single clone. If the parent folder doesn't exist yet (nothing cloned under that account), the action errors with "account folder does not exist" — clone at least one repo first. Compact view exposes the harness as a 🤖 icon on both the repo row and the account pill, calling `global.ai_harnesses[0]`.

### Update notification

Gitbox checks for updates once per day in the background. When a newer version is available, an amber pill appears on the right side of the footer status bar showing the new version. Click it to download and apply the update in place. After the update completes, click **Quit** and restart the app to use the new version.

### Deleting repos and accounts

Click the **trash icon** in the top bar to enter delete mode. Red X buttons appear on account cards, mirror group cards, and repo rows. Click one to remove it. Account deletion also removes its source and local clone folders.

Exit delete mode by clicking the trash icon again.

### Move a repository across accounts / providers

Open the kebab (⋮) on any repo row and pick **Move repository…**. The entry is disabled when the clone isn't clean and in sync — the tooltip explains why. The modal:

1. **Form** — pick the destination account + owner (personal or org, loaded asynchronously), confirm the new repo name, set visibility, and optionally opt in to deleting the source repo and/or the local clone after a successful move. Both delete toggles are unchecked by default.
2. **Confirm** — a red-bordered summary listing every destructive side effect. Type the source repo key (e.g. `acme/widget`) to unlock the **Move** button.
3. **Progress** — each phase (preflight → fetch → create destination → push mirror → rewire origin → optional deletes → update config) lands as its own line with a live status.

The move preserves every ref and tag via `git push --mirror`, rewires `origin` on the local clone to the new URL, and updates the gitbox config so the repo now lives under the destination account's source. A failed source-delete or local-clone-delete (phases 6–7) is captured as a warning — the move itself is already complete by that point.

Required token scopes on both sides are listed in [Token scopes for destructive actions](credentials.md#token-scopes-for-destructive-actions).

### Global identity warning

If your `~/.gitconfig` has a global `user.name` or `user.email`, Gitbox shows an **orange warning banner** at the top of the dashboard. A global identity can override the per-repo identities that gitbox sets up for each account.

Click **Remove** to clear the global identity entries, or dismiss the banner with the **✕** button.

### Global credential helper warning

When at least one account uses **GCM** (Git Credential Manager), Gitbox verifies that your global `~/.gitconfig` has `credential.helper = manager` and `credential.credentialStore` set to the OS-appropriate value (`keychain` on macOS, `wincredman` on Windows, `secretservice` on Linux). If either is missing or wrong, a second orange banner appears.

Without those globals, GCM falls through to a TTY prompt during authentication and fails with `fatal: could not read Password ... Device not configured` in the GUI — see the banner text for the specific mismatch (missing, or unexpected value).

Click **Configure** to fix both entries in one step. Gitbox also backfills the same defaults into your `gitbox.json` so the check passes permanently, even if `~/.gitconfig` is edited later. Dismiss the banner with the **✕** button if you prefer to handle it manually.

### Global gitignore warning

Gitbox notices when `~/.gitignore_global` is missing, has an out-of-date recommended block, has managed patterns duplicated outside the sentinel markers, or when `core.excludesfile` is unset. In any of those states a banner appears with an **Install** button that does all of: writes a curated block of OS-junk patterns (`.DS_Store`, `Thumbs.db`, `*~`, …), points `core.excludesfile` at it, and saves a timestamped `.bak-YYYYMMDD-HHMMSS` backup of any existing file. Only the last 3 backups are kept.

The automatic startup check can be toggled via **Settings → Global gitignore → On/Off**. Explicit actions always run — the gear toggle, the Install button, and the CLI `gitbox gitignore check|install` are never silenced by the preference. See [Global gitignore in the reference](reference.md#global-gitignore) for the managed-block format and the TUI `G` shortcut.

## Tips

- **Window position** — Gitbox remembers your window size and position. If you disconnect a secondary monitor and the window would open off-screen, it automatically centers on your main display.
- **External edits** — if you edit `gitbox.json` by hand (or via the CLI), the GUI picks up changes automatically when the window regains focus.
- **Same config** — the desktop app and the CLI tool (`gitbox`) share the same config file. Changes in one are visible in the other.
- **Automatic backups** — every time a meaningful change is saved, Gitbox creates a dated backup (e.g., `gitbox-20260401-143025.json`) in the same directory. The 10 most recent backups are kept automatically; older ones are pruned. The GUI's corruption-recovery screen can restore from any of them in one click. Window-position-only saves (moving or resizing the app) do not create a backup — they are cosmetic churn and would rotate real pre-corruption copies out of the ring.

## See also

- [CLI Quick Start](cli-guide.md) — for terminal users
- [Configuration Reference](reference.md) — detailed config format and commands
- [Architecture](architecture.md) — technical design
