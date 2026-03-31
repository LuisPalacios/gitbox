Feature: GUI Application
  As a gitbox user
  I need a desktop GUI to manage my Git repositories visually
  So that I can clone, pull, fetch, and monitor repos without the command line

  Background:
    Given the gitbox GUI application is running via Wails v2

  # --- Onboarding ---

  Scenario: First run shows onboarding (IsFirstRun returns true when no global folder)
    Given no gitbox config file exists at "~/.config/gitbox/gitbox.json"
    When the application starts
    Then an empty config is created in memory with version 2
    And IsFirstRun returns true
    And the frontend shows the onboarding view with a folder picker

  Scenario: Config load error displays message to user
    Given a config file exists but contains invalid JSON
    When the application starts
    Then GetConfigLoadError returns the error message with the file path
    And the frontend displays the error to the user
    And an empty config is used for the session

  Scenario: Set global folder creates directory and saves config
    Given the first-run onboarding is showing
    When the user picks folder "~/00.git" via the native directory dialog
    Then global.folder is set to "~/00.git"
    And the directory "~/00.git" is created if it does not exist
    And the config is saved to "~/.config/gitbox/gitbox.json"
    And cfgLoaded is set to true for subsequent saves

  # --- Account and repo views ---

  Scenario: Account card shows: provider, URL, username, credential badge, sync ring
    Given account "my-github" exists with provider "github", URL "https://github.com", username "LuisPalacios", default_credential_type "gcm"
    When the frontend renders the account card for "my-github"
    Then the card displays the GitHub provider icon
    And the card shows URL "https://github.com"
    And the card shows username "LuisPalacios"
    And the card shows a "GCM" credential badge
    And the card includes a sync ring indicating last-fetched status

  Scenario: Repo list shows per-repo status with colored symbols
    Given source "my-github" has 5 repos: 2 clean, 1 dirty, 1 behind, 1 not cloned
    When the frontend renders the repo list
    Then clean repos show green "+" symbol
    And dirty repos show red "!" symbol
    And behind repos show blue "<" symbol
    And not-cloned repos show no symbol

  # --- Clone progress ---

  Scenario: Clone progress emitted via clone:progress event
    Given a repo "LuisPalacios/gitbox" needs to be cloned
    When the user initiates CloneRepo for source "my-github" repo "LuisPalacios/gitbox"
    Then the operation runs in a background goroutine
    And "clone:progress" events are emitted with source, repo, phase, and percent
    And phases include "Receiving objects", "Resolving deltas"
    And a "clone:done" event is emitted when complete with source and repo

  # --- Pull ---

  Scenario: Pull emits pull:done event
    Given a cloned repo "LuisPalacios/gitbox" is behind upstream
    When the user initiates PullRepo for source "my-github" repo "LuisPalacios/gitbox"
    Then git pull --ff-only runs in a background goroutine
    And a "pull:done" event is emitted with source and repo
    And the pull:done event has no "error" key on success

  # --- Fetch ---

  Scenario: Fetch single repo emits fetch:done and refreshes status
    Given a cloned repo exists for source "my-github" repo "LuisPalacios/gitbox"
    When the user initiates FetchRepo
    Then git fetch --all --prune runs in a background goroutine
    And per-repo identity is verified after fetch completes
    And a "fetch:done" event is emitted
    And RefreshStatus is called to update the UI

  Scenario: Fetch all repos emits fetch:start per repo then fetch:alldone
    Given 10 repos are cloned
    When the user initiates FetchAllRepos
    Then each repo emits a "fetch:start" event before fetching
    And each repo emits a "fetch:done" event after fetching
    And a "fetch:alldone" event is emitted when all repos are done
    And RefreshStatus is called to update all repo states

  # --- Compact view mode ---

  Scenario: Compact view mode persists window position separately
    Given the user has used full mode at position (100, 200, 1200, 800)
    And the user has used compact mode at position (50, 50, 400, 300)
    When the application starts in "compact" mode
    Then the window is positioned at (50, 50, 400, 300)

  Scenario: View mode toggle saves current position to leaving mode's slot
    Given the application is in "full" view mode at position (100, 200, 1200, 800)
    When the user switches to "compact" mode
    Then the current window state (100, 200, 1200, 800) is saved to the full-mode slot
    And the view mode is set to "compact" in config
    And the compact mode position is restored
    And the config is saved to disk

  # --- Periodic sync ---

  Scenario Outline: Periodic sync interval: off, 5m, 15m, 30m
    When the user sets periodic sync to "<interval>"
    Then the config has global.periodic_sync set to "<stored>"
    And the config is saved

    Examples:
      | interval | stored |
      | off      |        |
      | 5m       | 5m     |
      | 15m      | 15m    |
      | 30m      | 30m    |

  # --- Theme ---

  Scenario: Theme switching: dark/light with CSS custom properties
    Given the application is using "dark" theme
    When the user toggles the theme to "light"
    Then the root element's CSS custom properties switch to light-mode values
    And the theme preference is saved to config

  # --- Repo detail ---

  Scenario: Repo detail shows branch, ahead/behind, changed files, untracked
    Given a cloned repo "LuisPalacios/gitbox" is on branch "main"
    And the repo is 1 ahead, 2 behind, has 3 changed files and 1 untracked file
    When the frontend displays the repo detail view
    Then the branch "main" is shown
    And the ahead count is 1 and behind count is 2
    And 3 changed files are listed
    And 1 untracked file is listed

  # --- Discover repos ---

  Scenario: Discover repos: fetch from API, display filterable list, add selected
    Given account "my-github" has valid API credentials
    When the user opens the discovery modal for "my-github"
    Then repos are fetched from the GitHub API
    And already-configured repos are marked
    And the user can filter the list by name
    And selected repos are added to the source on confirmation

  # --- Create new repo on provider ---

  Scenario: Create new repo on provider via GUI (owner dropdown from account orgs, private/public, optional clone after)
    Given account "my-github" has valid API credentials
    And the account has access to organizations "MyOrg" and "TeamProject"
    When the user opens the create-repo dialog for "my-github"
    Then the owner dropdown shows "LuisPalacios", "MyOrg", and "TeamProject"
    And the default visibility is "private"
    When the user creates repo "new-tool" under owner "MyOrg" with visibility "private"
    Then a private repo "MyOrg/new-tool" is created on GitHub
    And the repo is added to source "my-github"
    And the user is offered to clone the newly created repo

  # --- Mirror tab ---

  Scenario: Mirror tab: group cards with sync rings, status dots
    Given mirror "forgejo-github" exists with 3 repos: 2 synced, 1 behind
    When the user views the mirrors tab
    Then the mirror group card shows "forgejo-github" with accounts "my-forgejo" and "my-github"
    And each repo shows a colored status dot: green for synced, yellow for behind
    And the group card includes a sync ring reflecting overall health

  Scenario: Mirror status check via GetMirrorStatus emits mirror:status events
    Given mirror "forgejo-github" has 2 repos with active status
    When the user triggers mirror status check
    Then GetMirrorStatus runs asynchronously
    And "mirror:status" events are emitted per repo with sync state (synced/behind/ahead)

  Scenario: Mirror setup via SetupMirrorRepo emits mirror:setup events
    Given mirror "forgejo-github" has a pending repo "infra/myapp"
    When the user triggers mirror setup for "infra/myapp"
    Then SetupMirrorRepo runs asynchronously
    And a "mirror:setup" event is emitted with created, mirrored, method, and any error

  Scenario: Mirror discover scans all account pairs, shows progress, allows apply
    Given accounts "my-forgejo" and "my-github" exist
    When the user triggers mirror discovery from the GUI
    Then progress events are emitted for listing and analyzing phases
    And discovered mirrors are displayed with confidence levels (confirmed, likely, possible)
    And the user can select which discoveries to apply to config

  # --- Mirror CRUD from GUI ---

  Scenario: Add/delete mirror group and mirror repo from GUI
    When the user creates a mirror group "forgejo-github" with src "my-forgejo" and dst "my-github"
    Then the group is added to config and saved
    When the user adds repo "infra/myapp" with direction "push" and origin "src"
    Then the repo is added to the group
    When the user deletes repo "infra/myapp" from the group
    Then the repo is removed from the group
    When the user deletes the mirror group "forgejo-github"
    Then the group is removed from config and saved

  Scenario: Check mirror credentials for accounts
    Given account "my-github" uses GCM credential type
    When the mirror credential check runs for "my-github"
    Then a MirrorCredentialCheck is returned
    And it reports whether the account has a portable mirror token (PAT in keyring)
    And it indicates if a PAT needs to be stored separately from GCM

  # --- Credential setup ---

  Scenario: Credential setup: GCM (browser OAuth), Token (paste + store), SSH (generate key + display public key)
    Given account "my-github" exists with default_credential_type "gcm"
    When the user triggers credential setup for "my-github" from the GUI
    Then GCM triggers browser OAuth flow
    Given account "my-gitea" exists with default_credential_type "token"
    When the user triggers credential setup for "my-gitea"
    Then a text field is shown for pasting the PAT
    And the PAT is stored in the OS keyring
    Given account "my-gitlab" exists with default_credential_type "ssh"
    When the user triggers credential setup for "my-gitlab"
    Then an ed25519 key pair is generated
    And the public key is displayed for copying

  # --- Account management ---

  Scenario: Rename account migrates all artifacts
    Given account "old-key" exists with SSH credentials
    When the user renames the account to "new-key" via the GUI
    Then the config references are updated (sources, mirrors)
    And SSH keys are renamed from "gitbox-old-key-sshkey" to "gitbox-new-key-sshkey"
    And SSH config entries are updated
    And keyring tokens are migrated

  Scenario: Delete account removes account + source
    Given account "my-old" exists with a matching source "my-old"
    When the user deletes "my-old" via the GUI
    Then both the account and the source are removed from config
    And the config is saved

  # --- Open config file ---

  Scenario: Open config file in default editor
    When the user clicks "Open config"
    Then "~/.config/gitbox/gitbox.json" is opened in the OS default editor

  # --- Autostart ---

  Scenario: Autostart (run at OS login) toggle
    When the user enables the autostart toggle
    Then the application is registered to start on OS login for the current platform
    When the user disables the autostart toggle
    Then the autostart registration is removed

  # --- Global identity warning ---

  Scenario: Global identity warning and removal
    Given ~/.gitconfig has user.name "Global Name" and user.email "global@example.com"
    When the GUI checks for global identity
    Then a warning banner is shown: "Global git identity detected -- NOT RECOMMENDED"
    When the user clicks "Remove global identity"
    Then user.name and user.email are unset from ~/.gitconfig
    And the warning banner disappears

  # --- Config auto-backup ---

  Scenario: Config auto-backup on save (rolling 5-day history)
    Given a config file exists at "~/.config/gitbox/gitbox.json"
    When the GUI triggers a config save
    Then a timestamped backup is created before writing the new config
    And only the 5 most recent backups are retained

  # --- Window position ---

  Scenario: Window position restored on startup, centered if off-screen
    Given saved window position is at (100, 200, 1200, 800)
    And the primary screen is 1920x1080
    When the application starts
    Then the window is positioned at (100, 200, 1200, 800)

  Scenario: Off-screen window position falls back to center
    Given saved window position is at (5000, 5000, 1200, 800)
    And the primary screen is 1920x1080
    When the application starts
    Then the window is centered instead of placed off-screen

  Scenario: ShowWindow called by frontend after compact layout measured
    Given the application starts in "compact" mode
    When the DOM is ready and the compact layout height is measured
    Then the frontend calls ShowWindow with the measured height
    And the window becomes visible without flickering
