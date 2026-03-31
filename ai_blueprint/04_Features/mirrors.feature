Feature: Mirror Management
  As a gitbox user
  I need to set up, monitor, and discover repository mirrors between providers
  So that my repositories are backed up across multiple Git hosting services

  Background:
    Given a valid gitbox config exists
    And account "my-forgejo" exists with provider "forgejo" and URL "https://git.example.org"
    And account "my-github" exists with provider "github" and URL "https://github.com"

  # --- Mirror group CRUD ---

  Scenario: Create mirror group pairing two accounts
    When I run "gitbox mirror add forgejo-github --account-src my-forgejo --account-dst my-github"
    Then mirror group "forgejo-github" is created
    And the mirror has account_src "my-forgejo" and account_dst "my-github"
    And the mirror repos map is empty
    And the config is saved

  Scenario: Delete mirror group
    Given a mirror group "forgejo-github" exists
    When I run "gitbox mirror delete forgejo-github"
    Then mirror group "forgejo-github" is removed from config
    And the config is saved

  # --- Mirror repo CRUD ---

  Scenario: Add push mirror repo (origin=src, direction=push)
    Given a mirror group "forgejo-github" exists
    When I run "gitbox mirror add-repo forgejo-github infra/myapp --direction push --origin src"
    Then repo "infra/myapp" is added to mirror "forgejo-github"
    And the repo has direction "push", origin "src", and status "pending"

  Scenario: Add pull mirror repo (origin=dst, direction=pull)
    Given a mirror group "forgejo-github" exists
    When I run "gitbox mirror add-repo forgejo-github LuisPalacios/backup --direction pull --origin dst"
    Then repo "LuisPalacios/backup" is added to mirror "forgejo-github"
    And the repo has direction "pull" and origin "dst"

  Scenario: Delete mirror repo from group
    Given a mirror group "forgejo-github" exists with repo "infra/myapp"
    When I run "gitbox mirror delete-repo forgejo-github infra/myapp"
    Then repo "infra/myapp" is removed from mirror "forgejo-github"

  # --- Push mirror setup ---

  Scenario: Setup push mirror: create target repo on backup + create push mirror on origin
    Given a mirror group "forgejo-github" exists with repo "infra/myapp" direction "push" origin "src"
    And valid API tokens are available for both accounts
    And valid mirror tokens (PATs) are available for both accounts
    When I run "gitbox mirror setup forgejo-github --repo infra/myapp"
    Then the target repo "myapp" is created on GitHub if it does not exist
    And a push mirror is created on Forgejo pointing to "https://github.com/LuisPalacios/myapp.git"
    And the mirror repo status is updated to "active" in config
    And the output shows "repo created, mirror configured"

  Scenario: Push mirror triggers immediate sync after creation (Gitea/Forgejo)
    Given a mirror group "forgejo-github" has repo "infra/myapp" with direction "push" origin "src"
    And the origin account is "my-forgejo" with provider "forgejo"
    When the push mirror setup completes
    Then a sync request is sent via POST /api/v1/repos/{owner}/{repo}/mirror-sync
    And the mirror begins replicating immediately

  # --- Pull mirror setup ---

  Scenario: Setup pull mirror: create pull mirror via migrate API on backup
    Given a mirror group "forgejo-github" exists with repo "LuisPalacios/tools" direction "pull" origin "dst"
    And valid tokens are available
    When I run "gitbox mirror setup forgejo-github --repo LuisPalacios/tools"
    Then a pull mirror is created on Forgejo using the migrate API
    And the source URL is "https://github.com/LuisPalacios/tools.git"
    And the mirror is created as a private repository
    And the status is updated to "active"

  # --- Manual guide fallback ---

  Scenario: Setup returns manual guide when provider doesn't support API mirrors
    Given the origin account is "my-github" with provider "github"
    And GitHub does not support push mirror API
    When the push mirror setup runs
    Then the method is set to "manual"
    And step-by-step instructions are printed for manual setup
    And the output shows "requires manual setup"

  # --- Mirror status ---

  Scenario: Mirror status compares HEAD commits on origin vs backup
    Given a mirror "forgejo-github" has repo "infra/myapp" with active status
    And the HEAD commit on origin and backup are both "a1b2c3d"
    When I run "gitbox mirror status forgejo-github"
    Then the output shows "synced" for "infra/myapp" with commit "a1b2c3d"

  Scenario: Mirror status reports: synced, behind, ahead
    Given a mirror "forgejo-github" has 3 repos with active status
    And repo "infra/myapp" has matching HEAD commits
    And repo "infra/api" has origin HEAD "abc1234" newer than backup HEAD "def5678"
    And repo "infra/lib" has backup HEAD newer than origin HEAD
    When I run "gitbox mirror status forgejo-github"
    Then "infra/myapp" shows "synced"
    And "infra/api" shows "behind" with "Backup is behind origin"
    And "infra/lib" shows "ahead" with "Backup is ahead of origin"

  Scenario: Mirror status warns if backup repo is public
    Given a mirror "forgejo-github" has repo "infra/private-app"
    And the backup repo on GitHub is public
    When I run "gitbox mirror status forgejo-github"
    Then the output includes warning "backup repo is PUBLIC"

  # --- Mirror discovery ---

  Scenario: Mirror discover scans account pairs (3 methods: push mirror API confirmed, mirror flag likely, name match possible)
    Given accounts "my-forgejo" and "my-github" exist with valid tokens
    And Forgejo repo "infra/myapp" has a push mirror pointing to "https://github.com/LuisPalacios/myapp.git"
    And Forgejo repo "LuisPalacios/tools" has mirror flag true and GitHub has "LuisPalacios/tools"
    And both accounts have a repo named "shared-lib" but no mirror API evidence
    When I run "gitbox mirror discover"
    Then "infra/myapp" is found with confidence "confirmed" and direction "push"
    And "LuisPalacios/tools" is found with confidence "likely" and direction "pull"
    And "shared-lib" is found with confidence "possible"

  Scenario: Mirror discover with progress reporting (listing + analyzing phases)
    Given accounts "my-forgejo" and "my-github" exist with valid tokens
    When I run "gitbox mirror discover"
    Then progress output shows "Listing repos from my-forgejo..."
    And progress output shows "Listing repos from my-github..."
    And progress output shows "Analyzing N repo(s)..."

  Scenario: Mirror discover --apply merges results into config
    Given mirror discovery found 4 mirror relationships for "my-forgejo" and "my-github"
    When I run "gitbox mirror discover --apply"
    Then a mirror group is created for the account pair if one does not exist
    And 4 mirror repos are added with status "active"
    And the config is saved
    And the output shows "Applied: 4 mirror(s) added to config"

  # --- Mirror list ---

  Scenario: Mirror list shows summary (active/pending/error counts)
    Given mirror "forgejo-github" has 5 repos: 3 active, 1 pending, 1 error
    When I run "gitbox mirror list"
    Then the output shows "forgejo-github  my-forgejo <-> my-github"
    And the summary shows total 5, active 3, pending 1, error 1
    And a table lists each repo with direction, origin account, method, and status

  # --- Mirror credentials ---

  Scenario: Mirror credentials: GCM accounts need separate PAT for mirrors
    Given the backup account "my-github" uses default_credential_type "gcm"
    And no PAT is stored in env vars or keyring for "my-github"
    When the push mirror setup runs
    Then it fails with "mirrors require a PAT stored via 'gitbox account credential setup'"
    And the error explains that GCM OAuth tokens cannot be used for mirror authentication

  # --- Setup all pending ---

  Scenario: Setup all pending mirrors in a group
    Given a mirror group "forgejo-github" exists with 3 repos in "pending" status
    When I run "gitbox mirror setup forgejo-github"
    Then all 3 pending repos are set up via API
    And their statuses are updated to "active"
    And the config is saved once after all setups
    And the summary shows "Set up: 3, Skipped: 0, Errors: 0"
