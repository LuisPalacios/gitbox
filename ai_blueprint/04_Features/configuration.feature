Feature: Configuration Management
  As a gitbox user
  I need to initialize, load, save, validate, and migrate my configuration
  So that my multi-account Git environment is properly set up and persisted

  Background:
    Given the default config path is "~/.config/gitbox/gitbox.json"
    And the v1 config path is "~/.config/git-config-repos/git-config-repos.json"

  # --- Initialization ---

  Scenario: Init creates config with global folder
    Given no config file exists at the default path
    When I run "gitbox init"
    And I enter "~/00.git" as the root folder
    Then a v2 config file is created at "~/.config/gitbox/gitbox.json"
    And the config has version 2
    And the config has global.folder set to "~/00.git"
    And the config has global.credential_ssh.ssh_folder set to "~/.ssh"
    And the config has global.credential_gcm.helper set to "manager"
    And the config has global.credential_gcm.credential_store set to the platform default
    And the config has global.credential_token present
    And the config has empty accounts and sources maps

  # --- Loading ---

  Scenario: Load valid v2 config
    Given a valid v2 config file exists with 2 accounts and 3 sources
    When I run "gitbox status"
    Then the config is loaded without errors
    And source key order matches the JSON file order
    And repo key order within each source matches the JSON file order

  Scenario: Load fails gracefully when config is malformed
    Given the config file contains invalid JSON syntax
    When I run "gitbox status"
    Then the command fails with "invalid JSON"
    And the error message includes the file path
    And no panic or stack trace is shown

  Scenario: Load non-existent file returns os.ErrNotExist
    Given no config file exists at "~/.config/gitbox/gitbox.json"
    And no config file exists at "~/.config/git-config-repos/git-config-repos.json"
    When the config loader reads "~/.config/gitbox/gitbox.json"
    Then it returns an error wrapping os.ErrNotExist
    And errors.Is(err, os.ErrNotExist) returns true

  # --- Saving ---

  Scenario: Save creates dated backup before overwriting (5-day rolling history)
    Given a config file exists at "~/.config/gitbox/gitbox.json"
    And 6 previous backup files exist matching "gitbox-????????-??????.json"
    When a config save operation runs
    Then a new timestamped backup is created like "gitbox-20260401-143025.json"
    And only the 5 most recent backups are retained
    And the oldest backup is deleted
    And the config file is written with 4-space indentation and trailing newline

  # --- Migration ---

  Scenario: Migrate v1 to v2 deduplicates accounts and converts format
    Given a v1 config exists with two accounts sharing hostname "github.com" and username "LuisPalacios"
    And the first account has URL "https://github.com/LuisPalacios" with 3 repos
    And the second account has URL "https://github.com/MyOrg" with 2 repos
    And the first account uses string boolean "true" for ssh
    When I run "gitbox migrate"
    Then a v2 config is created at "~/.config/gitbox/gitbox.json"
    And the two v1 accounts are merged into one v2 account
    And the v2 account has provider "github"
    And string booleans are converted to native JSON booleans
    And SSH config is nested under credential_ssh with host and key_type fields
    And GCM config is nested under credential_gcm with helper and provider fields
    And all 5 repos are in one source with "org/repo" naming
    And the v1 file is NOT modified

  Scenario: Dry-run migration shows preview without modifying files
    Given a valid v1 config exists
    When I run "gitbox migrate --dry-run"
    Then the converted v2 JSON is printed to stdout
    And stderr shows "(dry run -- would write to ~/.config/gitbox/gitbox.json)"
    And no v2 config file is created on disk
    And the v1 config file is NOT modified

  # --- Key order preservation ---

  Scenario: Config preserves JSON key order via SourceOrder and RepoOrder
    Given a v2 config with sources in order "my-github", "my-gitlab", "my-gitea"
    And source "my-github" has repos in order "LuisPalacios/gitbox", "LuisPalacios/dotfiles", "MyOrg/infra"
    When the config is saved and reloaded
    Then OrderedSourceKeys returns ["my-github", "my-gitlab", "my-gitea"]
    And OrderedRepoKeys for "my-github" returns ["LuisPalacios/gitbox", "LuisPalacios/dotfiles", "MyOrg/infra"]

  # --- Tilde expansion ---

  Scenario: ExpandTilde resolves ~ to home directory
    Given the user's home directory is "/home/luis"
    When ExpandTilde is called with "~/00.git/repos"
    Then the result is "/home/luis/00.git/repos"

  Scenario: ExpandTilde passes through absolute paths unchanged
    Given an absolute path "/opt/repos"
    When ExpandTilde is called with "/opt/repos"
    Then the result is "/opt/repos"

  # --- Platform-specific credential store ---

  Scenario Outline: Platform credential store detection on init
    Given I am running on "<platform>"
    When I run "gitbox init"
    Then the GCM credential_store is set to "<store>"

    Examples:
      | platform | store          |
      | Windows  | wincredman     |
      | macOS    | keychain       |
      | Linux    | secretservice  |
