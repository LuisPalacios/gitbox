<p align="center">
  <img src="../assets/screenshot-gui.png" alt="Gitbox" width="800" />
</p>

# Gitbox desktop — user guide

Gitbox is a desktop app that helps you keep all your Git projects organized and up to date, even when you work with multiple accounts on GitHub, GitLab, Forgejo, and other providers.

This guide walks you through everything from first launch to day-to-day use.

## Prerequisites

Download the installer for your platform from the [Releases](https://github.com/LuisPalacios/gitbox/releases) page:

- **Windows** — `gitbox-win-amd64-setup.exe` (installs `GitboxApp.exe` to Program Files with Start Menu shortcuts)
- **macOS** — `gitbox-macos-arm64.dmg` or `gitbox-macos-amd64.dmg` (macOS 13 Ventura or later; open DMG, run the install script from Terminal)
- **Linux** — `gitbox-x86_64.AppImage` (self-contained, just download and run)

Alternatively, download the ZIP archives (`gitbox-<platform>-<arch>.zip`) and extract manually. Each one contains only the app: `GitboxApp.exe`, `GitboxApp.app`, or `GitboxApp`.

> **macOS note:** The app is not signed by Apple. The DMG includes an "Install Gitbox" script that copies `GitboxApp.app` to `/Applications/` and removes quarantine flags automatically. Run `bash "/Volumes/gitbox/Install Gitbox.command"` from Terminal. For manual install, use `xattr -cr /path/to/GitboxApp.app`.

Gitbox calls tools that are already on the system: **Git** on your PATH, and [Git Credential Manager](https://github.com/git-ecosystem/git-credential-manager) for GCM accounts. **Settings → System check** lists anything missing with the install command for your OS — see [Settings panel](#settings-panel).

### Bootstrap script

On macOS, Linux, or Windows (Git Bash), the bootstrap script downloads the latest release and installs the app:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh)
```

Use `--version <tag>` for a specific release or `--prefix <dir>` to change the install directory (default `~/bin`; macOS installs `GitboxApp.app` to `/Applications/`).

On Linux the bootstrap script also registers the app in the Activities menu so I can search for "Gitbox" or drag it to the dock. Skip with `--no-desktop`; run it later on its own with `bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/register-gitbox.sh)`. Pass `--uninstall` to the same script to remove the menu entry. The `.desktop` file points at an absolute path, so in-app updates and later bootstrap runs don't need a re-register.

On Windows the setup exe and the bootstrap script are alternatives, not layers. The setup exe installs into Program Files with a Start menu entry; the bootstrap script installs into `~/bin` with none. I pick one. When the bootstrap script finds a copy from the setup exe, it warns that the Start menu keeps launching that copy and leaves it in place. It also flags a half-removed install (an uninstall entry whose files are gone) and prints the commands to clean it up.

Headless hosts can't run the app. There, `--cli-only` installs the latest 1.x `gitbox` CLI from the v1 maintenance line, and the script picks it automatically on Linux when neither `DISPLAY` nor `WAYLAND_DISPLAY` is set.

### Linux AppImage

Download the AppImage, make it executable, and run:

```bash
chmod +x gitbox-x86_64.AppImage
./gitbox-x86_64.AppImage
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

Discovery is **add-only** — it adds repos to your config but never removes them.

## Step 4: Day-to-day

I close any dialog with **Escape** or by clicking outside it. Dialogs that are busy or that need an explicit choice (a running clone, a confirmation in progress) stay open until they finish. Account names, repo rows and organization badges also respond to **Enter** and **Space** when focused with **Tab**.

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

#### Automatic checking

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

#### Periodic fetch

In Settings, you can enable automatic fetch every 5, 15, or 30 minutes. Gitbox checks all remotes and re-checks credential health in the background.

#### Viewing details

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

For each adopted clone, Gitbox adds it to `gitbox.json` under the matched source, configures per-repo credential isolation, sets `user.name` and `user.email` from the account, and rewrites the remote URL to match the credential type. The scoring rules behind the account match are in [Architecture › pkg/adopt](architecture.md#pkgadopt--orphan-repo-discovery).

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

#### Changing credential type

Use the dropdown to switch between GCM, Token, and SSH. Click **Setup** to apply the change. gitbox removes the old credential and its artifacts, sets up the new one, and reconfigures all existing clones automatically.

#### Deleting a credential

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

- **Discover** — scans all account pairs to detect existing mirror relationships, with decreasing confidence: push mirror API (confirmed), pull mirror flag (likely), and name match (possible). During scanning, a progress bar shows per-account progress (indeterminate during repo listing, determinate during analysis). When results appear, repos already in your config are marked as **"configured"** and dimmed. Each unconfigured result has an individual **+ Add** button to add it to your config one by one, or use **Apply to config** to add all at once.
- **Check all** — checks sync status for every mirror group.

### Mirror detail list

Below the mirror cards, each group expands into a detail list showing individual mirrored repos with:

- Direction label (e.g., `origin → backup (mirror)`)
- Sync status (Synced OK, Backup is behind origin, etc.)
- Warning icon if the backup repo is public
- **Setup** button for pending repos that haven't been configured yet via API
- **+ Repo** button to add new repos to the group

## Step 6: Workspaces (read-only)

The **Workspaces** tab next to Accounts and Mirrors lists discovered VS Code `.code-workspace` files. Workspaces are read-only: the GUI discovers existing files, lists them with their resolved member clones, and opens one in my editor. It never creates, edits, generates, or deletes them — I own those files. Each entry has an **Open** button that opens the `.code-workspace` in the first editor in `global.editors`; the tab's **Discover** button rescans on demand.

### Auto-discovery on startup

Whenever I drop a `*.code-workspace` file under the gitbox-managed folder (or a configured extra folder) — or carry one over from another machine — the GUI picks it up: the cached list shows instantly at launch, then a background pass refreshes it and the tab updates if anything changed. Discovery walks `global.folder` and every `global.extra_folders` root for `*.code-workspace` files. Each file's folder references are resolved back to known clones by a deepest-prefix path match, and the cache in `gitbox.json` is only rewritten when something changed.

### Non-standard clones & multi-repo containers

A **multi-repo container** is a managed clone that holds other clones nested in its working tree (for example, a project repo whose `.code-workspace` ties together several sibling clones). When a clone looks like one — it has a `.code-workspace` at its root but isn't flagged yet — its row shows an inline **onboard nested clones** hint. Clicking it flags the clone as a container, scans its working tree, and opens the adopt modal with the nested clones it finds; you confirm which to onboard. Nested clones are adopted in place under their real account/org (stored with an absolute `clone_folder` inside the container, never relocated), and the hint is replaced by a **container** badge.

You can also manage this from the repo row's kebab menu (⋮) — **Mark as multi-repo container**, **Unmark as multi-repo container**, and **Re-scan for nested clones** (shown once a clone is a container) — or with the **Multi-repo container** checkbox in the repo detail panel. The **Change root folder** dialog manages **extra scan folders** (additional roots scanned for clones and `.code-workspace` files) and the **nested scan depth** (how many levels below a container Gitbox descends, default 1 — the container's immediate children; raise it to reach clones nested deeper).

The standard layout is `global.folder / <account> / <org|user> / repo`. Clones found in an extra scan folder show up in the orphans modal and are onboarded **in place** with an absolute `clone_folder` — Gitbox never moves them.

## Dashboard views

### Full view

The full dashboard shows the top bar with health rings, the tab bar (Accounts/Mirrors), cards, repo or mirror detail lists, and the summary footer. Action buttons in the top bar include Pull All, Fetch All, Delete mode, and Compact view.

### Collapse and expand

The repo list folds at two levels:

- **Account groups** — click an account's header bar or its name to fold its clone list. A folded header shows how many clones it holds and how many need attention, so a problem never hides behind a fold. The ⋮ menu on the header keeps working and never folds the group.
- **Clones with clones below them** — any clone that has nested clones under it gets a ▾ chevron before its status dot. Click it to fold everything below, at any depth. A folded row shows how many clones it hides and how many of them need attention. The chevron is its own control: it never opens the repo detail.

Gitbox remembers what I fold across restarts (`global.collapsed` in `gitbox.json`) and forgets entries for accounts or clones I delete or rename. The compact view keeps its own per-account expansion.

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
- **System check** — **Run** opens a report of every external tool gitbox uses (`git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add`, and `wsl` on Windows), where it's installed, its version, and — for anything missing that your config needs — an install command. Each tool is marked ok, missing (required by your config), or optional.
- **Terminals** — **Manager** opens the Terminal Profile editor in its own OS window. Three sections: detected Terminal apps (read-only), detected Shells (read-only), and Profiles (the launchable Terminal × Shell pairs the kebab menu offers). Toggle Default / Preferred / Hidden per row, edit a Profile's name + Terminal + Shell binding, add user-defined Profiles, or delete those you added. Re-detect re-runs the host probe to pick up new shells, fresh WezTerm `launch_menu` entries, or freshly installed terminals without restarting the GUI. The window is owned by the main app — closing the main window closes the Manager too.

  When I click a `WezTerm + <Shell>` or `Windows Terminal + <Shell>` Profile, gitbox first looks up a matching entry in my own terminal config (`wezterm.lua` `launch_menu` for WezTerm, `settings.json` `profiles.list` for Windows Terminal). On a hit, gitbox launches that entry — for WezTerm it builds `wezterm-gui.exe start --cwd <path> -- <entry argv>` and splices the entry's `set_environment_variables` on top of the parent env; for Windows Terminal it runs `wt.exe -w 0 nt --profile "<name>" -d <path>` so WT applies my profile's font, colors, and `commandline` without spawning a second window when `firstWindowPreference: persistedWindowLayout` is set. With no match (or no config / terminal not installed), gitbox falls back to its generic argv template. Note: WezTerm picker-callback logic in `wezterm.lua` (per-entry `color_scheme`, custom `mux.spawn_window` hooks, etc.) only fires when the entry is chosen from WezTerm's own launcher menu — gitbox spawning the pane externally bypasses those callbacks. See [Architecture › How launch matching works](architecture.md#how-launch-matching-works) for the matcher rules (em-dash suffix, pattern fallback for `pwsh` / `cmd` / WSL distros, etc.).

- **Version** — current app version
- **Author** — project author and link to the GitHub repository

The add-account and change-credential flows run the same check automatically: if you pick the `gcm` credential type on a machine that doesn't have Git Credential Manager installed, you get a yellow banner with the install command instead of a cryptic authentication failure later on.

### Clone actions

Each cloned repo row has a **kebab menu (⋮)** on the right side. The menu is split into three sections so the items you use most aren't buried behind scrolling:

1. **Always visible** — `🌐 Open in browser` and `📁 Open folder`. The browser entry opens `<account url>/<owner>/<name>`, resolved on the Go side from the saved config, and shows an error dialog if the row can't be found there. Every path-based entry (folder, editor, terminal, profile, AI harness) shows a "clone first" dialog instead of doing nothing when the clone hasn't finished or its status hasn't loaded yet.
2. **Defaults** — one entry per category: `>_ <default profile>` (the Terminal Profile marked Default, shown by its name), `✎ Open with <editors[0]>`, and `🤖 Open with <ai_harnesses[0]>`. An entry is hidden when that category has nothing configured.
3. **Submenus** — `Profiles ▸`, `Editors ▸`, `AI Harnesses ▸`. `Profiles ▸` lists the profiles marked Preferred, other than the default, and appears when there is at least one. `Editors ▸` and `AI Harnesses ▸` only appear when the category has **two or more** entries — with just one, the default already covers it. Click the submenu to expand (not hover), click another submenu to switch, click outside or pick an item to close everything.

Below the submenus:

- **🧹 Sweep branches** — finds and deletes stale local branches. Shows a confirmation dialog with the list of branches before deleting anything. The current branch and the default branch are never touched. Three kinds of stale branch are detected:
  - **Gone** — the remote tracking branch was deleted (e.g. a PR merged and its branch deleted on the server); deleted with `git branch -D`.
  - **Merged** — fully merged into the default branch; deleted with `git branch -d`.
  - **Squashed** — squash-merged or rebase-merged on the server (different commits, same changes); deleted with `git branch -D`.

To change which editor or AI harness appears as the top-level default, reorder the array in `gitbox.json` — the first entry is always the default. The terminal default is the profile marked Default in **Settings → Terminals → Manager**.

Terminal detection covers Windows Terminal, WezTerm, Alacritty, Tabby, ConEmu, Hyper, Mintty, and ZOC on Windows; iTerm2, Terminal, Warp, Kitty, Ghostty, WezTerm, and Alacritty on macOS; GNOME Terminal, Konsole, Terminator, Foot, Alacritty, Kitty, Tilda, Guake, and xterm on Linux. Shell detection covers PowerShell 7/5, Command Prompt, Git Bash, and WSL on Windows; Zsh, Bash, Fish, and Dash on macOS; Bash, Zsh, Fish, Ksh, and Dash on Linux. Editors cover VS Code, Cursor, Zed, and anything else discoverable on `PATH`. AI harnesses (Claude Code, Codex, Antigravity, Aider, Cursor Agent, OpenCode) run inside the default Terminal Profile's shell — see [AI harness actions](#ai-harness-actions) below.

### Account actions

Each source group in the repo list has a **kebab menu (⋮)** on the right side of its header (the account title above the list of clones). The account kebab uses the **same structure and icons** as the repo-row kebab — top-level defaults, per-category submenus, same hide rules — scoped to the account's parent folder (`<global.folder>/<account-key>`) rather than a single clone:

- **🌐 Open in browser** — opens the provider profile/org page for the account (e.g. `https://github.com/<username>`, the GitLab group page, the Gitea/Forgejo user page).
- **📁 Open folder** — opens the account's parent folder in the OS file manager. The folder is the natural workspace root for cross-repo greps, multi-repo edits, or shell loops. If the folder doesn't exist yet (nothing cloned under that account), the action errors silently — clone at least one repo first.
- **>\_ Open in \<terminal\>**, **✎ Open in \<editor\>**, **🤖 Open in \<AI harness\>** — same default-first entries as the repo kebab, plus the category submenus when you have multiple options configured. Sweep branches is dropped here — it's meaningful only on a specific clone.

Editors are auto-detected on startup by scanning PATH. Gitbox writes the detected editors to `global.editors` in your config file with their full paths. You can reorder entries or add custom editors by editing the config — the menu always reflects the config order.

Terminals use profiles instead. On startup gitbox detects the installed terminal apps and shells, writes them to `global.terminal_apps` and `global.shells`, and pairs them into launchable entries in `global.terminal_profiles`. On Windows a profile pairs a terminal with a shell, for example Windows Terminal + PowerShell 7. On macOS and Linux a profile is the terminal alone and runs your login shell. I manage them in **Settings → Terminals → Manager**: mark one profile Default (the kebab's top-level terminal entry), mark others Preferred (the `Profiles ▸` submenu), hide the ones I never use, or add my own. Re-detection keeps my flags, renames, hand-edited args, and the profiles I added.

On Windows, bare-shell profiles that open a shell without a terminal app (`pwsh.exe`, `powershell.exe`, `cmd.exe`, `wsl.exe`) are hidden by default, and only show up on their own when no modern terminal is installed. Un-hide one in the Manager for a direct shortcut. The launcher wraps them in `cmd.exe /C start "" /D <path>`, which gives each shell a fresh console and sets the starting directory.

#### Windows Terminal and WezTerm profiles

When a profile uses Windows Terminal, gitbox looks for a matching WT profile in `settings.json` at launch time. `Windows Terminal + PowerShell 7` matches the WT profile named `PowerShell 7`, and a per-distro WSL shell matches the WT profile for that distro. On a match gitbox runs `wt.exe -w 0 nt --profile "<name>" -d <path>`, so the shell opens as a new tab in your most recent WT window with the font, colors, and startup tweaks you tuned in WT. Hidden profiles and profiles whose `source` appears in WT's top-level `disabledProfileSources` never match. Without a match, gitbox uses the generic Windows Terminal arguments.

Locations checked, in order: `%LOCALAPPDATA%\Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json` (Store), `…\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\…` (Preview), `%LOCALAPPDATA%\Microsoft\Windows Terminal\settings.json` (unpackaged). Gitbox re-reads the file whenever it changes, so renaming or adding a WT profile is picked up on the next launch.

WezTerm works on every OS: when `wezterm.lua` defines a `config.launch_menu`, each sync adds one profile per entry, and launching it runs that entry's own `args`.

To rename a profile or change its terminal or shell, edit it in the Manager. To pass extra flags, edit its `args` in `gitbox.json` — re-detection keeps hand-edited args.

#### Launching gitbox from Git Bash (developer note)

If you launch `GitboxApp.exe` from a Git Bash / MSYS2 shell, Windows environment variables inherited by the GUI come through in posix form (e.g. `LOCALAPPDATA=/c/Users/you/AppData/Local`). Those values propagate into terminals opened from gitbox via the default `cmd.exe /C start …` path, and tools that read them as Windows paths — `oh-my-posh`, some `$PROFILE` helpers — can choke (`& '/c/Users/...' — not recognized as a cmdlet`). Gitbox sanitises the env block it hands to the spawned terminal, but when Windows Terminal is the default console host, WT's delegation path can bypass that block.

Two equally clean fixes:

- **Launch `GitboxApp.exe` from Explorer, the Start Menu, or a pinned shortcut** — anywhere Windows originates a clean env. End users never hit this, so production behaviour is unaffected.
- **Use a Windows Terminal profile** that matches the shell, as described above. Gitbox then launches through `wt.exe --profile`, and WT starts the shell from its own profile context, which has a clean Windows env regardless of how the GUI was started.

**Compact mode** shows status only. Switch to full view to reach the clone and account actions.

### AI harness actions

AI CLI harnesses (Claude Code, Codex, Antigravity, Aider, Cursor Agent, OpenCode, …) are interactive shell processes — they need a terminal to run in. Gitbox adds one **Open in \<harness\>** entry per configured harness to both the repo kebab and the source-header (account) kebab. Clicking an entry opens the **default Terminal Profile** in the target folder and runs the harness inside that profile's shell, so the same terminal and shell you get from `>_ <profile>` hosts the harness.

I pick the host by choosing the default profile in **Settings → Terminals → Manager** (the same default the launcher uses). To switch hosts, mark a different profile as default.

The harness runs through the profile's shell rather than as a bare terminal command. That keeps your shell's rc files in play — `nvm`/`npm` PATH on macOS and Linux, Git Bash PATH on Windows — which is where most harness CLIs live, and leaves you at an interactive prompt when the harness exits. PowerShell gets `-NoExit -EncodedCommand`, `cmd` gets `/K`, `fish` gets `-C`, every other POSIX shell gets `-i -c '<harness>; exec <shell>'`. On Windows, Git Bash receives npm `.cmd` shims by bare name (`claude`, resolved on its own PATH) and native binaries by their path with forward slashes; WSL always receives the bare name. Profiles without an explicit shell (macOS, Linux) use `$SHELL`.

Windows Terminal profiles matched from `settings.json` receive the wrapped shell as WT's trailing commandline, so the WT profile's font and colors still apply. WezTerm `launch_menu` entries are wrapped the same way using the shell from the entry's own `args`. macOS Terminal.app and iTerm cannot take a command on their `open -a` launch line, so gitbox drives them through AppleScript (`cd <folder> && <harness>`); the first run may prompt for Automation permission. Warp, Kitty, Ghostty, and Alacritty on macOS cannot host a command at all — with one of those as the default profile, clicking an AI harness entry shows an actionable error naming the profile; mark iTerm, Terminal, or WezTerm as default instead.

Harnesses are detected in the background, not on the startup path: a first pass runs about two seconds after the window opens, then every ten minutes, and again whenever the window regains focus (throttled to once a minute). The menu refreshes on its own when the list changes — no restart, no config reload. The probe never touches the process PATH, so it is safe to run while fetches and clones are in flight. Besides PATH, gitbox probes `~/.local/bin` on every OS (the native Claude Code, Antigravity, Codex, Goose and uv-managed installers put their binaries there, and a GUI launched from the Dock, a desktop menu or the Start menu doesn't inherit the shell's PATH), the Homebrew prefixes on macOS, and any tool-specific well-known directories listed in the catalog (for example `%LOCALAPPDATA%\agy\bin` for Antigravity on Windows). Gitbox writes detected entries to `global.ai_harnesses` with the resolved binary path and `source: "detected"`. Each entry has a `name` (display in the menu), a `command` (binary path or on-PATH name), and an optional `args` array for harness-specific flags (e.g. `["--model", "sonnet-4.6"]`). Most harnesses need no flags — `args` is usually empty.

When a detected harness is uninstalled, the next pass flags its entry `missing: true` and hides it from the menu. The entry stays in the config on purpose: reinstalling the tool (even somewhere else on disk) clears the flag, updates `command` to the new location, and brings the entry back with your `args` untouched. Entries you wrote by hand (`source: "user"`) are flagged and hidden the same way when their `command` stops resolving, but gitbox never rewrites their `command` or `args`. Entries that predate this behaviour carry no `source`; the first pass classifies them, treating a stored path whose file name matches a catalog binary as detected and anything else as user-written.

The set of harnesses gitbox tries to auto-detect is maintained as a markdown table embedded into the binary. The authoritative list lives at [`pkg/harness/tools-directory.md`](../pkg/harness/tools-directory.md) — to add or remove a detected harness, edit that file. A row is auto-detected when its `Category` is `Agentic CLI`, `AI Harness`, `Headless Harness`, `Agentic IDE`, or `Agentic IDE / CLI`, and its `Executable / CLI Command` cell contains one or more backticked identifiers (e.g. `` `claude` ``, `` `aider` ``, `` `cursor` ``). A cell with several names, such as "`` `agent` `` or `` `cursor-agent` ``" for the renamed Cursor CLI, probes them in order so either install resolves. The trailing `Well-known locations` column lists per-tool install directories outside PATH, each as its own backticked token with `~`, `$VAR` and `%VAR%` expanded at probe time. Framework, orchestrator, and cloud-platform rows are documented for reference but skipped by the detector — they don't launch from a terminal in a folder. Rows whose `Category` is `Retired CLI` (Gemini CLI, retired in favour of Antigravity CLI) are skipped too, and any `global.ai_harnesses` entry carrying a retired name is removed on the next sync so a dead binary stops showing up in the menu. Agentic IDEs (Cursor, Devin Desktop) are treated as AI tools, not editors: the "Open in Cursor" entry will therefore appear under the AI harness section of the menu, not the editor section.

In the account kebab, the same entries appear with identical ordering — the only runtime difference is that the working directory is `<global.folder>/<account-key>` (the account's parent folder) instead of a single clone. If the parent folder doesn't exist yet (nothing cloned under that account), the action errors with "account folder does not exist" — clone at least one repo first.

### Update notification

Gitbox checks for updates once per day in the background. When a newer version is available, an amber pill appears on the right side of the footer status bar showing the new version. Click it to download and apply the update in place. Gitbox checks that the release is signed by the gitbox release key and verifies its SHA256 checksum, and refuses the update when the signature is missing or invalid, the release has no checksum for it, or either file can't be downloaded; it then replaces only what is already installed next to the app — on macOS the whole `GitboxApp.app` bundle. After the update completes, click **Quit** and restart the app to use the new version.

The Linux AppImage only notifies: clicking the pill opens the release page, and I download the new AppImage and replace the old file myself. Gitbox never rewrites the AppImage, and it doesn't embed update information for external AppImage updaters.

The updater follows the release GitHub marks as latest, so a v1 GUI moves to v2 the same way.

### Deleting repos and accounts

Click the **trash icon** in the top bar to enter delete mode. Red X buttons appear on account cards, mirror group cards, and repo rows. Click one to remove it. Account deletion also removes its source and local clone folders.

Exit delete mode by clicking the trash icon again.

### Move a repository across accounts / providers

Open the kebab (⋮) on any repo row and pick **Move repository…**. The entry is disabled when the clone isn't clean and in sync — the tooltip explains why. The modal:

1. **Form** — pick the destination account + owner (personal or org, loaded asynchronously), confirm the new repo name, set visibility, and optionally opt in to deleting the source repo and/or the local clone after a successful move. Both delete toggles are unchecked by default.
2. **Confirm** — a red-bordered summary listing every destructive side effect. Type the source repo key (e.g. `acme/widget`) to unlock the **Move** button.
3. **Progress** — each phase (preflight → fetch → create destination → push mirror → rewire origin → optional deletes → update config) lands as its own line with a live status.

The move preserves every ref and tag via `git push --mirror`, rewires `origin` on the local clone to the new URL, and updates the gitbox config so the repo now lives under the destination account's source. A failed source-delete or local-clone-delete (phases 6–7) is captured as a warning — the move itself is already complete by that point.

Required token scopes on both sides are listed in [Token scopes by capability](credentials.md#token-scopes-by-capability).

### Global identity warning

If your `~/.gitconfig` has a global `user.name` or `user.email`, Gitbox shows an **orange warning banner** at the top of the dashboard. A global identity can override the per-repo identities that gitbox sets up for each account.

Click **Remove** to clear the global identity entries, or dismiss the banner with the **✕** button.

### Global credential helper warning

When at least one account uses **GCM** (Git Credential Manager), Gitbox verifies that your global `~/.gitconfig` has a `credential.helper` that resolves to Git Credential Manager — the short name `manager`, the legacy `manager-core`, or an absolute path to the `git-credential-manager` binary (the form `git-credential-manager configure` writes on macOS) — and `credential.credentialStore` set to the OS-appropriate value (`keychain` on macOS, `wincredman` on Windows, `secretservice` on Linux). If no helper resolves to GCM or the store is wrong, a second orange banner appears.

Without a GCM helper configured, GCM falls through to a TTY prompt during authentication and fails with `fatal: could not read Password ... Device not configured` in the GUI — see the banner text for the specific issue (no GCM helper, or the wrong credential store).

Click **Configure** to fix both entries in one step. Gitbox also backfills the same defaults into your `gitbox.json` so the check passes permanently, even if `~/.gitconfig` is edited later. Dismiss the banner with the **✕** button if you prefer to handle it manually.

### Global gitignore warning

Gitbox notices when `~/.gitignore_global` is missing, has an out-of-date recommended block, has managed patterns duplicated outside the sentinel markers, or when `core.excludesfile` is unset. In any of those states a banner appears with an **Install** button that does all of: writes a curated block of OS-junk patterns (`.DS_Store`, `Thumbs.db`, `*~`, …), points `core.excludesfile` at it, and saves a timestamped `.bak-YYYYMMDD-HHMMSS` backup of any existing file. Only the last 3 backups are kept.

The automatic startup check can be toggled via **Settings → Global gitignore → On/Off**, stored as `global.check_global_gitignore`. Explicit actions always run — the gear toggle and the Install button are never silenced by the preference. The managed block sits between sentinel markers, so patterns and comments I add outside them survive every reinstall. See [Architecture › pkg/gitignore](architecture.md#pkggitignore--global-gitignore-self-heal) for the managed-block format.

## Tips

- **Window position** — Gitbox remembers your window size and position. If you disconnect a secondary monitor and the window would open off-screen, it automatically centers on your main display.
- **External edits** — if you edit `gitbox.json` by hand, the GUI picks up changes automatically when the window regains focus.
- **Same config as v1** — the app reads `~/.config/gitbox/gitbox.json`, the same file v1 used. The format stays at version 3, so upgrading from v1 needs no migration.
- **Automatic backups** — every time a meaningful change is saved, Gitbox creates a dated backup (e.g., `gitbox-20260401-143025.json`) in the same directory. The 10 most recent backups are kept automatically; older ones are pruned. The GUI's corruption-recovery screen can restore from any of them in one click. Window-position-only saves (moving or resizing the app) do not create a backup — they are cosmetic churn and would rotate real pre-corruption copies out of the ring.

## See also

- [Credentials](credentials.md) — credential types, permissions, and troubleshooting
- [Config file reference](architecture.md#4-config-format-v3) — every `gitbox.json` key
- [Architecture](architecture.md) — technical design
