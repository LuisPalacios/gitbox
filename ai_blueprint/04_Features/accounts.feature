Feature: Account Management
  As a gitbox user
  I need to add, update, rename, delete, and inspect accounts
  So that I can manage my Git provider identities across multiple services

  Background:
    Given a valid gitbox config exists

  # --- List ---

  Scenario: List all accounts in table output
    Given accounts "my-github" and "my-gitea" exist
    When I run "gitbox account list"
    Then the output shows a table with columns ACCOUNT, PROVIDER, URL, USERNAME, DEFAULT CRED
    And both accounts appear in the table

  Scenario: List all accounts in JSON output
    Given accounts "my-github" and "my-gitea" exist
    When I run "gitbox account list --json"
    Then the output is valid JSON containing both account keys
    And each entry includes provider, url, username, name, email, and default_credential_type

  # --- Add ---

  Scenario: Add account with all fields
    When I run:
      """
      gitbox account add my-github \
        --provider github \
        --url https://github.com \
        --username LuisPalacios \
        --name "Luis Palacios" \
        --email luis@example.com \
        --default-credential-type gcm
      """
    Then the account "my-github" is created
    And the account has provider "github"
    And the account has url "https://github.com"
    And the account has username "LuisPalacios"
    And the account has name "Luis Palacios"
    And the account has email "luis@example.com"
    And the account has default_credential_type "gcm"

  Scenario: Add account auto-populates GCM config when default-credential-type is gcm
    When I run:
      """
      gitbox account add my-github \
        --provider github \
        --url https://github.com \
        --username LuisPalacios \
        --name "Luis Palacios" \
        --email luis@example.com \
        --default-credential-type gcm
      """
    Then the account "my-github" has a GCM config with provider "github"
    And the GCM config has helper "manager"
    And the GCM config has credential_store set to the platform default

  Scenario: Add account requires ssh-host and ssh-key-type when SSH
    When I run:
      """
      gitbox account add my-gitlab \
        --provider gitlab \
        --url https://gitlab.com \
        --username myuser \
        --name "My Name" \
        --email myuser@example.com \
        --default-credential-type ssh
      """
    Then the command fails with "--ssh-host is required for SSH accounts"

  Scenario: Add account with SSH provides all required SSH fields
    When I run:
      """
      gitbox account add my-gitlab \
        --provider gitlab \
        --url https://gitlab.com \
        --username myuser \
        --name "My Name" \
        --email myuser@example.com \
        --default-credential-type ssh \
        --ssh-host gl-myuser \
        --ssh-key-type ed25519
      """
    Then the account "my-gitlab" is created
    And the account has default_credential_type "ssh"
    And the account has SSH config with host "gl-myuser"
    And the account has SSH config with hostname "gitlab.com"
    And the account has SSH config with key_type "ed25519"

  # --- Update ---

  Scenario: Update account partial fields (only changed flags applied)
    Given an account "my-github" exists with name "Old Name" and email "old@example.com"
    When I run "gitbox account update my-github --name 'New Name' --email new@example.com"
    Then the account "my-github" has name "New Name"
    And the account "my-github" has email "new@example.com"
    And the account provider and URL remain unchanged

  # --- Delete ---

  Scenario: Delete account fails if source references it
    Given an account "my-github" exists
    And a source "my-github" references account "my-github"
    When I run "gitbox account delete my-github"
    Then the command fails with 'cannot delete account "my-github": referenced by source "my-github"'

  Scenario: Delete account succeeds after source removed
    Given an account "orphan" exists with no sources or mirrors referencing it
    When I run "gitbox account delete orphan"
    Then the account "orphan" is removed from config
    And the config is saved

  # --- Show ---

  Scenario: Show account as JSON
    Given an account "my-github" exists with provider "github" and username "LuisPalacios"
    When I run "gitbox account show my-github"
    Then the output is valid JSON with the full account structure
    And it includes provider, url, username, name, email, and credential configs

  # --- Orgs ---

  Scenario Outline: Account orgs lists organizations/groups from provider
    Given an account "<account>" exists with provider "<provider>" and URL "<url>"
    And the account has valid API credentials
    When I run "gitbox account orgs <account>"
    Then the output lists organization/group names accessible to the user
    And pagination is handled via <pagination_method>

    Examples:
      | account      | provider  | url                     | pagination_method             |
      | my-github    | github    | https://github.com      | Link header with per_page=100 |
      | my-gitlab    | gitlab    | https://gitlab.com      | X-Next-Page header            |
      | my-gitea     | gitea     | https://git.example.org | page parameter with limit=50  |
      | my-bitbucket | bitbucket | https://bitbucket.org   | cursor-based next URL         |

  Scenario: Account orgs JSON output
    Given an account "my-github" exists with valid API credentials
    When I run "gitbox account orgs my-github --json"
    Then the output is valid JSON containing an array of organization names

  # --- Rename ---

  Scenario: Rename account migrates config keys, source key+folder, keyring tokens, SSH keys+config
    Given an account "old-name" exists with default_credential_type "ssh"
    And a source "old-name" references account "old-name"
    And a PAT is stored in the OS keyring under key "old-name"
    And SSH key files exist at "~/.ssh/gitbox-old-name-sshkey" and "~/.ssh/gitbox-old-name-sshkey.pub"
    And SSH config has a Host entry for "gitbox-old-name"
    When the account is renamed from "old-name" to "new-name"
    Then the account key becomes "new-name"
    And the source "old-name" now references account "new-name"
    And the PAT is accessible under keyring key "new-name"
    And the PAT is removed from keyring key "old-name"
    And SSH key files are renamed to "~/.ssh/gitbox-new-name-sshkey" and "~/.ssh/gitbox-new-name-sshkey.pub"
    And SSH config Host entry is updated from "gitbox-old-name" to "gitbox-new-name"
    And the account SSH host field is updated to "gitbox-new-name"

  Scenario: Rename account fails if new key already exists
    Given accounts "alpha" and "beta" both exist
    When the account is renamed from "alpha" to "beta"
    Then the operation fails with 'account "beta" already exists'

  # --- Discover ---

  Scenario Outline: Discover repos from provider API
    Given an account "<account>" exists with provider "<provider>" and URL "<url>"
    And the account has valid API credentials
    When I run "gitbox account discover <account>"
    Then all repos visible to the authenticated user are fetched
    And the repos are returned in "org/repo" format
    And pagination is handled automatically using <pagination_method>

    Examples:
      | account      | provider  | url                     | pagination_method             |
      | my-github    | github    | https://github.com      | Link header with per_page=100 |
      | my-gitlab    | gitlab    | https://gitlab.com      | X-Next-Page header            |
      | my-gitea     | gitea     | https://git.example.org | page parameter with limit=50  |
      | my-bitbucket | bitbucket | https://bitbucket.org   | cursor-based next URL         |

  Scenario: Discover repos interactive mode prompts for selection
    Given an account "my-github" exists with 5 undiscovered repos
    When I run "gitbox account discover my-github"
    Then the user is shown a list of discovered repos
    And the user can select which repos to add

  Scenario: Discover repos with --all adds without prompting
    Given an account "my-github" exists with 5 undiscovered repos
    When I run "gitbox account discover my-github --all"
    Then all 5 repos are added to the source config
    And the config is saved

  Scenario: Discover with --skip-forks and --skip-archived filters
    Given an account "my-github" exists with 10 repos including 2 forks and 1 archived
    When I run "gitbox account discover my-github --skip-forks --skip-archived"
    Then 7 repos are shown in the discovered list
    And forked and archived repos are excluded

  # --- GCM provider auto-derivation ---

  Scenario Outline: GCM provider is auto-derived from provider type
    When I add an account with --provider "<provider>" and --default-credential-type "gcm"
    Then the GCM config provider is set to "<gcm_provider>"

    Examples:
      | provider  | gcm_provider |
      | github    | github       |
      | gitlab    | gitlab       |
      | bitbucket | bitbucket    |
      | gitea     | generic      |
      | forgejo   | generic      |
