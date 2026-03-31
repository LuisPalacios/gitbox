Feature: Credential Management
  As a gitbox user
  I need to set up, verify, and remove credentials for my accounts
  So that gitbox can authenticate against provider APIs and clone repositories

  Background:
    Given a valid gitbox config exists
    And the OS keyring service is available

  # --- Token resolution chain ---

  Scenario Outline: Token resolution chain: env var GITBOX_TOKEN_<KEY> then GIT_TOKEN then OS keyring
    Given an account "my-acct" exists with default_credential_type "token"
    And <env_state>
    And <keyring_state>
    When the token is resolved for account "my-acct"
    Then the token value is "<expected_token>"
    And the source is "<expected_source>"

    Examples:
      | env_state                                  | keyring_state                           | expected_token | expected_source                            |
      | GITBOX_TOKEN_MY_ACCT is set to "env-tok"   | keyring has "keyring-tok" for "my-acct" | env-tok        | environment variable GITBOX_TOKEN_MY_ACCT  |
      | GITBOX_TOKEN_MY_ACCT is not set            | keyring has "keyring-tok" for "my-acct" | keyring-tok    | OS keyring                                 |
      | GITBOX_TOKEN_MY_ACCT is not set            | keyring has no token for "my-acct"      |                | error: token not found                     |

  Scenario: GIT_TOKEN generic fallback used when specific env var is missing
    Given an account "my-acct" exists with default_credential_type "token"
    And GITBOX_TOKEN_MY_ACCT is not set
    And GIT_TOKEN is set to "generic-tok"
    And keyring has no token for "my-acct"
    When the token is resolved for account "my-acct"
    Then the token value is "generic-tok"
    And the source is "environment variable GIT_TOKEN"

  # --- Store and delete token in OS keyring ---

  Scenario: Store token in OS keyring
    Given an account "my-gitea" exists with default_credential_type "token"
    When I run "gitbox account credential setup my-gitea"
    And I enter PAT "ghp_abc123xyz" at the prompt
    Then the PAT is stored in the OS keyring under service "gitbox" and key "my-gitea"
    And the output shows "Token stored for my-gitea in OS keyring"

  Scenario: Delete token from OS keyring
    Given an account "my-gitea" exists with default_credential_type "token"
    And a PAT is stored in the OS keyring for "my-gitea"
    When I run "gitbox account credential del my-gitea"
    Then the PAT is removed from the OS keyring
    And the credential store file is removed if present
    And the output shows "Token removed for my-gitea"

  # --- GCM setup ---

  Scenario: GCM setup triggers git credential fill (browser OAuth) then approve
    Given an account "my-github" exists with default_credential_type "gcm"
    And GCM has no stored credential for "LuisPalacios@github.com"
    When I run "gitbox account credential setup my-github"
    Then GCM opens the browser for OAuth login
    And "git credential fill" is invoked with protocol "https", host "github.com", username "LuisPalacios"
    And "git credential approve" is called to persist the credential
    And the output shows "GCM credential stored"

  Scenario: GCM resolves token via git credential fill with non-interactive env
    Given an account "my-github" with URL "https://github.com" and username "LuisPalacios"
    And GCM has a stored credential
    When GCM token is resolved
    Then "git credential fill" is invoked with protocol "https", host "github.com", username "LuisPalacios"
    And interactive prompts are suppressed via GIT_TERMINAL_PROMPT=0 and GCM_INTERACTIVE=never
    And the password line from the output is returned as the token

  # --- SSH setup ---

  Scenario: SSH setup generates ed25519 key, writes ~/.ssh/config entry, tests connection
    Given an account "my-gitlab" exists with default_credential_type "ssh"
    And the account has SSH host "gl-myuser" and hostname "gitlab.com"
    And no SSH key exists for "my-gitlab"
    And no SSH config entry exists for Host "gl-myuser"
    When I run "gitbox account credential setup my-gitlab"
    Then directory "~/.ssh" is created if missing
    And an ed25519 SSH key pair is generated at "~/.ssh/gitbox-my-gitlab-sshkey"
    And SSH config entry for Host "gl-myuser" is written to "~/.ssh/config"
    And the public key is displayed for the user to add to the provider
    And "ssh -T gl-myuser" is attempted to test the connection

  Scenario: SSH regenerate deletes old key+config, creates new
    Given an account "my-gitlab" exists with default_credential_type "ssh"
    And SSH key exists at "~/.ssh/gitbox-my-gitlab-sshkey"
    And SSH config has Host "gl-myuser"
    When I run "gitbox account credential setup my-gitlab --regenerate"
    Then the old SSH private and public key files are deleted
    And the old SSH config Host entry is removed
    And a new ed25519 key pair is generated
    And a new SSH config entry is written

  # --- Credential verify ---

  Scenario: Credential verify checks authentication against provider API
    Given an account "my-gitea" exists with default_credential_type "token"
    And a valid PAT is stored in the OS keyring
    When I run "gitbox account credential verify my-gitea"
    Then the output shows "Token: OK (source: OS keyring)"
    And the output shows "API access: OK"
    And the provider API user endpoint is called to confirm authentication

  Scenario: Credential verify for SSH checks config, key, connection, and API
    Given an account "my-gitlab" exists with default_credential_type "ssh"
    And SSH is fully configured and connection succeeds
    And a PAT is stored in the keyring
    When I run "gitbox account credential verify my-gitlab"
    Then the output shows "SSH config: Host gitbox-my-gitlab"
    And the output shows "SSH key: ~/.ssh/gitbox-my-gitlab-sshkey"
    And the output shows "SSH connection: OK"
    And the output shows "API access: OK"

  # --- Credential delete ---

  Scenario: Credential delete removes artifacts (keyring, credential files, SSH keys)
    Given an account "my-gitlab" exists with default_credential_type "ssh"
    And SSH key files exist at "~/.ssh/gitbox-my-gitlab-sshkey"
    And SSH config has Host "gitbox-my-gitlab"
    And a PAT is stored in the keyring for "my-gitlab"
    When I run "gitbox account credential del my-gitlab"
    Then the SSH private key file is deleted
    And the SSH public key file is deleted
    And the SSH config Host entry is removed
    And the PAT is removed from the keyring
    And the user is reminded to remove the public key from the provider

  # --- Mirror token resolution ---

  Scenario: Mirror token resolution rejects GCM OAuth tokens (requires separate PAT)
    Given an account "my-gcm" exists with default_credential_type "gcm"
    And no PAT is stored in env vars or keyring for "my-gcm"
    When the mirror token is resolved for account "my-gcm"
    Then the resolution fails with "mirrors require a PAT stored via 'gitbox account credential setup'"

  Scenario: Mirror token uses keyring PAT for GCM accounts
    Given an account "my-gcm" exists with default_credential_type "gcm"
    And a PAT is stored in the keyring for "my-gcm"
    When the mirror token is resolved for account "my-gcm"
    Then the PAT from the keyring is returned
    And GCM OAuth tokens are NOT used

  # --- API token dispatch ---

  Scenario Outline: API token dispatch routes by credential type
    Given an account "<account>" exists with default_credential_type "<cred_type>"
    And <token_state>
    When the API token is dispatched for "<account>"
    Then the token is resolved via <resolution_method>

    Examples:
      | account    | cred_type | token_state                              | resolution_method                        |
      | my-gitea   | token     | keyring has PAT for "my-gitea"           | keyring lookup under service "gitbox"    |
      | my-github  | gcm       | GCM has stored credential                | git credential fill with non-interactive |
      | my-gitlab  | ssh       | keyring has PAT for "my-gitlab"          | keyring fallback lookup                  |

  # --- Credential type switching ---

  Scenario: Credential type switching cleans up old artifacts before configuring new
    Given an account "my-github" exists with default_credential_type "ssh"
    And SSH key files and config exist for "my-github"
    When the account credential type is changed to "token"
    Then old SSH key files are removed
    And old SSH config entry is removed
    And the account is configured for token credentials

  # --- Per-repo credential isolation ---

  Scenario: Per-repo credential isolation: empty credential.helper cancels global, type-specific helper set
    Given an account "my-github" with default_credential_type "gcm"
    When a repo is cloned for this account
    Then the repo's .git/config has an empty credential.helper line to cancel global helpers
    And the repo's .git/config has the type-specific credential helper configured
    And each repo's credentials are isolated from other accounts

  # --- Reconfigure existing clones ---

  Scenario: Reconfigure existing clones after credential type change
    Given an account "my-github" exists with default_credential_type "gcm"
    And 3 repos are cloned for that account
    When I run "gitbox account update my-github --default-credential-type token"
    Then the account has default_credential_type "token"
    And 3 cloned repos are reconfigured for token credentials
    And the output shows "Reconfigured 3 cloned repo(s)"
