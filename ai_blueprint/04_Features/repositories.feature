Feature: Repository Management
  As a gitbox user
  I need to discover, clone, pull, fetch, and check status of repositories
  So that I can manage all my Git repos across multiple providers from one tool

  Background:
    Given a valid gitbox config exists with at least one account and source
    And global folder is set to "~/00.git"

  # --- Clone with GCM credentials ---

  Scenario: Clone repo with GCM credentials (HTTPS URL with username)
    Given a source "my-github" has repo "LuisPalacios/gitbox" with credential_type "gcm"
    And the account has username "LuisPalacios" and URL "https://github.com"
    When I run "gitbox clone --source my-github --repo LuisPalacios/gitbox"
    Then the clone URL is "https://LuisPalacios@github.com/LuisPalacios/gitbox.git"
    And GCM handles authentication during clone
    And the repo is cloned to "~/00.git/my-github/LuisPalacios/gitbox"
    And per-repo identity (user.name, user.email) is set from the account
    And per-repo credential isolation is configured

  # --- Clone with SSH credentials ---

  Scenario: Clone repo with SSH credentials (git@<host-alias>:repo.git)
    Given a source "my-gitlab" has repo "team/project" with credential_type "ssh"
    And the account has SSH host "gl-myuser"
    When I run "gitbox clone --source my-gitlab --repo team/project"
    Then the repo is cloned using URL "git@gl-myuser:team/project.git"
    And the repo is cloned to "~/00.git/my-gitlab/team/project"

  # --- Clone with token credentials ---

  Scenario: Clone repo with token credentials (token embedded then sanitized)
    Given a source "my-gitea" has repo "myorg/myapp" with credential_type "token"
    And a valid PAT "tok_abc123" is available for the account
    And the account has username "myuser" and URL "https://git.example.org"
    When I run "gitbox clone --source my-gitea --repo myorg/myapp"
    Then the repo is cloned to "~/00.git/my-gitea/myorg/myapp"
    And the clone URL embeds the token as "https://myuser:tok_abc123@git.example.org/myorg/myapp.git"
    And after cloning the remote URL is sanitized to remove the embedded token
    And the remote origin URL becomes "https://myuser@git.example.org/myorg/myapp.git"

  Scenario: Token clone cancels global credential helper during clone
    Given a source "my-gitea" has repo "myorg/myapp" with credential_type "token"
    When the clone command runs
    Then git clone is invoked with "-c credential.helper=" to cancel global helpers
    And per-repo credential isolation is configured after clone

  # --- Pull ---

  Scenario: Pull repos behind upstream (fast-forward only)
    Given a cloned repo "myorg/myapp" is 3 commits behind upstream
    And the repo has no local modifications
    When I run "gitbox pull"
    Then git pull --ff-only is executed on the repo
    And the output shows "pulled" with "3 behind"

  Scenario: Pull skips dirty or conflicted repos
    Given a cloned repo "myorg/myapp" is behind but has uncommitted changes
    When I run "gitbox pull"
    Then the repo is NOT pulled
    And the output shows the repo as "dirty"

  # --- Fetch ---

  Scenario: Fetch runs git fetch --all --prune on all repos
    Given 5 repos are cloned across 2 sources
    When I run "gitbox fetch"
    Then "git fetch --all --prune" is executed on each cloned repo
    And the summary shows "Fetched: 5, Skipped: 0, Errors: 0"

  Scenario: Fetch with --source filter
    Given source "my-github" has 3 cloned repos and source "my-gitlab" has 2 cloned repos
    When I run "gitbox fetch --source my-github"
    Then only the 3 repos from "my-github" are fetched
    And repos from "my-gitlab" are not touched

  Scenario: Fetch with --repo filter
    Given source "my-github" has repos "LuisPalacios/gitbox" and "LuisPalacios/dotfiles"
    When I run "gitbox fetch --repo LuisPalacios/gitbox"
    Then only "LuisPalacios/gitbox" is fetched
    And "LuisPalacios/dotfiles" is not touched

  Scenario: Fetch shows status after: behind count, ahead count, diverged
    Given a cloned repo "LuisPalacios/gitbox" is 2 behind and 1 ahead after fetch
    When I run "gitbox fetch"
    Then the output shows "fetched" for "LuisPalacios/gitbox" with "1 ahead, 2 behind"

  # --- Status ---

  Scenario Outline: Status check returns correct state and symbol
    Given a cloned repo at "~/00.git/src/org/app"
    And the repo's git state is <state_description>
    When the status is checked
    Then the state is "<state>"
    And the symbol is "<symbol>"

    Examples:
      | state_description                 | state       | symbol |
      | clean with no changes             | clean       | +      |
      | has 2 modified files              | dirty       | !      |
      | 3 commits behind upstream         | behind      | <      |
      | 1 commit ahead of upstream        | ahead       | >      |
      | 2 ahead and 1 behind upstream     | diverged    | !      |
      | has merge conflicts               | conflict    | !      |
      | directory does not exist          | not cloned  | (none) |
      | no upstream tracking branch       | no upstream | ~      |
      | git command returns error         | error       | x      |

  Scenario: Status with --json output
    Given 3 repos are cloned with various states
    When I run "gitbox status --json"
    Then the output is valid JSON with each repo's state, ahead, behind, branch, and path

  # --- Repo CRUD ---

  Scenario: Repo list
    Given source "my-github" has 3 repos configured
    When I run "gitbox repo list"
    Then all repos across all sources are listed with source, repo key, and clone status

  Scenario: Repo show
    Given source "my-github" has repo "LuisPalacios/gitbox"
    When I run "gitbox repo show --source my-github --repo LuisPalacios/gitbox"
    Then the output shows the repo config as JSON including credential_type, id_folder, clone_folder

  Scenario: Repo add
    When I run "gitbox repo add --source my-github --repo LuisPalacios/newrepo"
    Then repo "LuisPalacios/newrepo" is added to source "my-github"
    And the config is saved

  Scenario: Repo update
    Given source "my-github" has repo "LuisPalacios/gitbox"
    When I run "gitbox repo update --source my-github --repo LuisPalacios/gitbox --clone-folder custom-name"
    Then the repo's clone_folder is set to "custom-name"

  Scenario: Repo delete
    Given source "my-github" has repo "LuisPalacios/old-repo"
    When I run "gitbox repo delete --source my-github --repo LuisPalacios/old-repo"
    Then the repo is removed from source "my-github"
    And the config is saved

  Scenario: Repo list filtered by --source
    Given source "my-github" has 3 repos and source "my-gitlab" has 2 repos
    When I run "gitbox repo list --source my-github"
    Then only the 3 repos from "my-github" are listed

  # --- Folder structure ---

  Scenario: Repo folder structure: globalFolder/sourceFolder/idFolder/cloneFolder
    Given global folder "~/00.git" and source folder "my-github"
    And repo key is "LuisPalacios/gitbox"
    When the repo path is resolved
    Then the path is "~/00.git/my-github/LuisPalacios/gitbox"

  Scenario: Repo id_folder override changes 2nd level dir
    Given global folder "~/00.git" and source folder "my-github"
    And repo key is "myorg/myapp" with id_folder "custom-org"
    When the repo path is resolved
    Then the path is "~/00.git/my-github/custom-org/myapp"

  Scenario: Repo clone_folder override changes 3rd level dir
    Given global folder "~/00.git" and source folder "my-github"
    And repo key is "myorg/myapp" with clone_folder "renamed-app"
    When the repo path is resolved
    Then the path is "~/00.git/my-github/myorg/renamed-app"

  Scenario: Absolute clone_folder replaces entire path
    Given repo key is "myorg/myapp" with clone_folder "~/special/location"
    When the repo path is resolved
    Then the path is the expanded value of "~/special/location"

  # --- Create repo on provider ---

  Scenario: Create repo on provider (private by default)
    Given an account "my-github" exists with provider "github" and valid API credentials
    When I run "gitbox repo create --account my-github --name new-project"
    Then a private repository "new-project" is created on GitHub via the API
    And the repo is added to the source config

  Scenario: Create repo with --public flag
    Given an account "my-github" exists with provider "github"
    When I run "gitbox repo create --account my-github --name public-project --public"
    Then a public repository "public-project" is created on GitHub

  Scenario: Create repo with --owner for org and --description
    Given an account "my-github" exists with provider "github"
    When I run "gitbox repo create --account my-github --name infra-tools --owner MyOrg --description 'Infrastructure automation tools'"
    Then the repository "infra-tools" is created under organization "MyOrg"
    And the repo description is "Infrastructure automation tools"

  Scenario Outline: Create repo works across providers
    Given an account exists with provider "<provider>" and URL "<url>"
    When a repo creation request is sent
    Then the <api_endpoint> is called with the correct payload

    Examples:
      | provider | url                     | api_endpoint                          |
      | github   | https://github.com      | POST /user/repos or /orgs/{org}/repos |
      | gitlab   | https://gitlab.com      | POST /api/v4/projects                 |
      | gitea    | https://git.example.org | POST /api/v1/user/repos or /orgs/{org}/repos |
      | forgejo  | https://forgejo.example  | POST /api/v1/user/repos or /orgs/{org}/repos |
      | bitbucket| https://bitbucket.org   | POST /2.0/repositories/{workspace}/{slug} |

  # --- Per-repo identity ---

  Scenario: Per-repo identity (user.name, user.email) set on clone and verified on fetch
    Given an account "my-github" has name "Luis Palacios" and email "luis@github.com"
    When a repo is cloned for source "my-github"
    Then the repo's local git config has user.name "Luis Palacios"
    And the repo's local git config has user.email "luis@github.com"
    When a fetch operation runs on the repo
    Then per-repo identity is re-verified and corrected if needed

  # --- Scan filesystem ---

  Scenario: Scan filesystem for repo status (no config required, optional --pull)
    Given directory "~/projects" contains 5 git repos in nested subdirectories
    When I run "gitbox scan --dir ~/projects"
    Then all 5 repos are found
    And each repo's sync status is displayed
    And the summary shows "Scanned: 5 repos"

  Scenario: Scan with --pull auto-pulls clean repos that are behind
    Given directory "~/projects" has a repo that is 2 behind and clean
    When I run "gitbox scan --dir ~/projects --pull"
    Then the behind repo is pulled via fast-forward
    And the output shows "pulled" with "2 behind -> ok"
