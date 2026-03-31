# Testing reference

Detailed test inventory and harness internals. For running tests, checklists, and fixture setup, see [testing.md](testing.md).

## Test inventory

### TUI unit tests — 31 tests (`cmd/cli/tui/`)

Model lifecycle and routing:

- `TestInit_FirstRun` — no config → onboarding screen
- `TestInit_ExistingConfig` — config exists → dashboard
- `TestInit_ExistingConfig_NoFolder` — config without folder → onboarding
- `TestInit_InvalidConfig` — bad JSON → error, onboarding
- `TestQuit_CtrlC` — Ctrl+C quits
- `TestQuit_Esc` — Esc quits from dashboard
- `TestWindowResize` — resize propagates to sub-models
- `TestNavigation_SwitchScreens` — 5 subtests: screen routing via messages
- `TestView_NotEmpty` — View() returns non-empty string

Dashboard:

- `TestDashboard_Empty` — no accounts renders empty state
- `TestDashboard_WithAccounts` — accounts appear in cards
- `TestDashboard_TabSwitch` — Tab toggles Accounts/Mirrors
- `TestDashboard_CardNavigation` — arrow keys move card cursor
- `TestDashboard_AddAccountShortcut` — 'a' key triggers add account
- `TestDashboard_SettingsShortcut` — 's' key triggers settings

Account screens:

- `TestAccount_Render` — detail view shows account fields
- `TestAccount_BackToDashboard` — Esc returns to dashboard
- `TestAccountAdd_Render` — form shows all fields
- `TestAccountAdd_SaveAccount` — fill form → save → verify config on disk
- `TestAccountAdd_DuplicateKey` — rejects duplicate account key
- `TestAccountAdd_InvalidKey` — rejects invalid key format

Credential screens:

- `TestCredential_GCM_MenuRender` — menu shows account key, type, browser auth hint
- `TestCredential_GCM_SetupView` — desktop shows "authenticate", headless shows "desktop session"
- `TestCredential_GCM_SetupViewBusy` — shows "Opening browser" while waiting
- `TestCredential_GCM_SetupDoneSuccess` — successful auth sets resultOK
- `TestCredential_GCM_SetupDoneNeedsPAT` — transitions to PAT input when API needs separate token
- `TestCredential_GCM_SetupDoneError` — displays error message
- `TestCredential_GCM_TypeSelectTriggersSetup` — selecting GCM type starts browser auth on desktop
- `TestCredential_GCM_BackFromSetup` — navigation back works

Onboarding:

- `TestOnboarding_Render` — first-run screen renders
- `TestOnboarding_SubmitFolder` — enter folder path → saves to config

### TUI helper unit tests — 7 tests (`cmd/cli/tui/`)

- `TestCloneURL_Token` — HTTPS URL with username
- `TestCloneURL_SSH_WithHost` — SSH URL with custom host alias
- `TestCloneURL_SSH_NoHost` — SSH URL with hostname from URL
- `TestCloneURL_GCM` — HTTPS URL (same as token)
- `TestStripScheme` — removes https://, http://, passes through bare hostnames
- `TestReconfigureClones` — updates remote URL on a real git clone
- `TestCountClonedRepos` — counts only repos that exist on disk

### TUI integration tests — 7 tests (`cmd/cli/tui/`)

These drive the real Bubble Tea event loop via **teatest** with credentials from `test-gitbox.json`:

- `TestIntegration_TUI_DashboardLoadsAccounts` — async config load → dashboard renders all account section headings
- `TestIntegration_TUI_CredentialStatus` — async credential check updates badge from "···" to credential type label
- `TestIntegration_TUI_NavigateToAccount` — Enter → account detail → Esc → back to dashboard
- `TestIntegration_TUI_AccountCredentialVerified` — token credential check completes with OK status
- `TestIntegration_TUI_Discovery` — navigate to account → 'd' → provider API returns repo list
- `TestIntegration_TUI_Settings` — 's' → settings screen renders → Esc → back to dashboard
- `TestIntegration_TUI_MirrorsTab` — Tab → mirrors tab renders → Tab → back to accounts

### CLI unit tests — 24 tests (`cmd/cli/`)

CRUD commands via subprocess execution against isolated config:

- `TestCLI_Version`, `TestCLI_Help` — basic binary invocation
- `TestCLI_GlobalShow`, `TestCLI_GlobalUpdate` — global config operations
- `TestCLI_AccountAdd`, `TestCLI_AccountList`, `TestCLI_AccountShow`, `TestCLI_AccountUpdate`, `TestCLI_AccountDelete` — account CRUD
- `TestCLI_SourceAdd`, `TestCLI_SourceList`, `TestCLI_SourceDelete` — source CRUD
- `TestCLI_RepoAdd`, `TestCLI_RepoList`, `TestCLI_RepoDelete` — repo CRUD
- `TestCLI_MirrorAdd`, `TestCLI_MirrorAddRepo`, `TestCLI_MirrorDeleteRepo`, `TestCLI_MirrorDelete`, `TestCLI_MirrorList` — mirror CRUD
- `TestCLI_Status` — status command output
- `TestCLI_AccountAdd_MissingFlags`, `TestCLI_AccountShow_NotFound`, `TestCLI_SourceAdd_NoAccount` — error cases

### CLI integration tests — 6 tests (`cmd/cli/`)

Real provider API calls and git operations via subprocess:

- `TestIntegration_CLI_CredentialVerify` — token resolves and API responds
- `TestIntegration_CLI_Discover` — lists repos from provider
- `TestIntegration_CLI_Clone` — clones repo to temp dir, verifies .git and git identity
- `TestIntegration_CLI_Status` — checks sync status of cloned repo
- `TestIntegration_CLI_Pull` — pulls from remote
- `TestIntegration_CLI_Fetch` — fetches from remote

### CLI scenario test — 1 test, 22 steps (`cmd/cli/`)

- `TestScenario_CLI_FullLifecycle` — end-to-end: add accounts → add sources → add repos → clone → status → pull → mirror setup → delete clone → re-clone → teardown in reverse

### Package tests — 138 tests (`pkg/`)

- `pkg/config/` — 58 tests: config parsing, CRUD operations, save/load
- `pkg/credential/` — 13 tests: token resolution, validation, CanOpenBrowser, EnsureGlobalGCMConfig
- `pkg/git/` — 9 tests: git subprocess operations
- `pkg/identity/` — 7 tests: ResolveIdentity, EnsureRepoIdentity, CheckGlobalIdentity
- `pkg/mirror/` — 5 tests: mirror discovery
- `pkg/provider/` — 35 tests: HTTP client, provider API parsing
- `pkg/status/` — 8 tests: clone status checking
- `pkg/update/` — 6 tests: semver parsing, version comparison, update check (mock API), throttle, checksum verification

### Total: ~214 tests

## How the test harness works

The `_test` key inside each account is silently ignored by `config.Parse()` — Go's JSON unmarshaler skips unknown struct fields. The test harness:

1. Parses the file as a standard gitbox config (accounts, sources, mirrors)
2. Extracts `_test` from each account via a separate raw JSON pass
3. Sets `GITBOX_TOKEN_<KEY>` env vars for accounts that have a `_test.token`
4. Overrides `global.folder` and `credential_ssh.ssh_folder` with throwaway temp directories
5. Writes the config to the throwaway path and runs CLI commands with `--config <throwaway-path>`

Clone directories are auto-cleaned by Go's `t.TempDir()` after each test. Integration tests use the `ssh_folder` from your `test-gitbox.json` so they can find real SSH keys. The test runner validates that this path is not `~/.ssh` and that `global.folder` is not `~/.config/gitbox` — if either points at a real user directory, the tests fail immediately.
