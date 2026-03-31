Feature: Identity Management
  As a gitbox user
  I need per-repo git identity (user.name, user.email) from account config
  So that commits use the correct author identity for each provider account

  Background:
    Given a valid gitbox config exists
    And account "my-github" exists with name "Luis Palacios" and email "luis@github.com"
    And account "my-work" exists with name "L. Palacios" and email "lpalacios@company.com"

  # --- ResolveIdentity ---

  Scenario: ResolveIdentity: repo-level name/email overrides account-level
    Given account "my-github" has name "Luis Palacios" and email "luis@github.com"
    And repo "LuisPalacios/special" has name "Luis P." and email "special@example.com"
    When the identity is resolved for repo "LuisPalacios/special"
    Then the resolved name is "Luis P."
    And the resolved email is "special@example.com"

  Scenario: ResolveIdentity falls back to account-level for missing repo fields
    Given account "my-github" has name "Luis Palacios" and email "luis@github.com"
    And repo "LuisPalacios/partial" has name "Luis P." but no email override
    When the identity is resolved for repo "LuisPalacios/partial"
    Then the resolved name is "Luis P."
    And the resolved email is "luis@github.com"

  # --- EnsureRepoIdentity ---

  Scenario: EnsureRepoIdentity sets user.name/user.email if different
    Given a cloned repo has user.name "Wrong Name" and user.email "wrong@example.com"
    And the account expects name "Luis Palacios" and email "luis@github.com"
    When EnsureRepoIdentity runs on the repo
    Then the repo's user.name is set to "Luis Palacios"
    And the repo's user.email is set to "luis@github.com"

  Scenario: EnsureRepoIdentity is idempotent when values match
    Given a cloned repo already has user.name "Luis Palacios" and user.email "luis@github.com"
    And the account expects name "Luis Palacios" and email "luis@github.com"
    When EnsureRepoIdentity runs on the repo
    Then no git config commands are executed
    And the repo is counted as "Already OK"

  # --- CheckGlobalIdentity ---

  Scenario: CheckGlobalIdentity detects user.name/email in ~/.gitconfig
    Given ~/.gitconfig has user.name "Global Name" and user.email "global@example.com"
    When I run "gitbox identity check"
    Then the output shows a warning for ~/.gitconfig
    And the warning includes 'user.name="Global Name"' and 'user.email="global@example.com"'
    And the warning says "NOT RECOMMENDED"

  Scenario: CheckGlobalIdentity passes when no global identity set
    Given ~/.gitconfig has no user.name or user.email
    When I run "gitbox identity check"
    Then the output shows "no global identity set" for ~/.gitconfig with OK status

  # --- RemoveGlobalIdentity ---

  Scenario: RemoveGlobalIdentity unsets user.name/email from ~/.gitconfig
    Given ~/.gitconfig has user.name "Global Name" and user.email "global@example.com"
    When I run "gitbox identity fix"
    Then user.name is unset from ~/.gitconfig via "git config --global --unset user.name"
    And user.email is unset from ~/.gitconfig via "git config --global --unset user.email"
    And the output shows "fixed" with "removed user.name, user.email"

  Scenario: RemoveGlobalIdentity is safe when already absent
    Given ~/.gitconfig has no user.name or user.email
    When I run "gitbox identity fix"
    Then the output shows "no global identity to remove"

  # --- Clone sets per-repo identity ---

  Scenario: Clone sets per-repo identity from account
    Given account "my-github" has name "Luis Palacios" and email "luis@github.com"
    When a repo is cloned for source "my-github"
    Then the repo's local git config has user.name "Luis Palacios"
    And the repo's local git config has user.email "luis@github.com"

  # --- Fetch verifies per-repo identity ---

  Scenario: Fetch verifies per-repo identity
    Given a cloned repo for account "my-github"
    And the repo has user.name "Stale Name" but the account expects "Luis Palacios"
    When a fetch operation completes on the repo
    Then per-repo identity is re-verified
    And user.name is corrected to "Luis Palacios"

  # --- Update account reconfigures clones ---

  Scenario: Update account name/email reconfigures all cloned repos
    Given account "my-github" has name "Old Name" and 4 repos are cloned
    When the account name is updated to "New Name"
    Then all 4 cloned repos have user.name updated to "New Name"
    And the output shows "Reconfigured 4 repo(s)"
