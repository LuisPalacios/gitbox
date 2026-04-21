# Credential setup

Each gitbox account needs credentials to do two things (and optionally a third):

- **Discovery** — calling your provider's API to list your repositories and check their sync status.
- **Operations** — git operations like clone, fetch, and pull.
- **Mirrors** (optional) — a portable PAT that remote servers use to push/pull on your behalf.

You choose one credential type per account. Depending on the type, gitbox needs one or two secrets to cover both tasks.

<p align="center">
  <img src="diagrams/credential-types.png" alt="Credential Types" width="850" />
</p>

## Which type should I use?

| Type | Secrets | Discovery | Operations | Mirrors | Best for |
| --- | --- | --- | --- | --- | --- |
| **GCM** | 1 (OAuth token managed by GCM) | Yes | Yes | Needs separate PAT | Desktop users (Windows, macOS, Linux with GUI) |
| **Token** | 1 (PAT in `~/.config/gitbox/credentials/`) | Yes | Yes | Same PAT | All platforms, CI/CD, Gitea/Forgejo |
| **SSH** | 2 (SSH key in `~/.ssh/` + PAT in `~/.config/gitbox/credentials/`) | Optional (needs PAT) | Yes | Same PAT | Advanced users who prefer SSH keys |

> **Tip:** On desktop, try **GCM** first — one login and you're done. GitHub and GitLab use browser-based OAuth; Gitea and Forgejo use basic authentication (username/password). All work seamlessly.

## PAT storage

All Personal Access Tokens (PATs) are stored in a single file per account at `~/.config/gitbox/credentials/<accountKey>` (file mode `0600`, readable only by the owner).

The file format depends on who needs to read it:

- **Token accounts** use **git-credential-store format** (`https://user:token@host`). The reason: git CLI reads this file directly through `credential.helper = store --file <path>`, and that's the format git expects. gitbox also reads it for API calls — same file, two consumers.
- **SSH and GCM accounts** store the **raw token** (just the PAT string, one line). Git CLI never touches this file — SSH authenticates through keys in `~/.ssh/`, and GCM manages its own OAuth tokens in the OS keyring. Only gitbox reads it, for API calls (discovery, repo creation) and mirror operations.

gitbox reads tokens transparently from both formats — it tries parsing as a URL first, then falls back to raw token. You never need to worry about which format a file uses.

The resolution chain when gitbox needs a token:

1. Account-specific environment variable (`GITBOX_TOKEN_<ACCOUNT_KEY>`)
2. Generic `GIT_TOKEN` env var (CI fallback)
3. Credential file (`~/.config/gitbox/credentials/<accountKey>`)

## GCM (Git Credential Manager)

GCM handles everything through a single login. One credential is stored by GCM itself and used for both discovery and git operations.

### Prerequisites

- **Windows:** Already installed with Git for Windows.
- **macOS:** `brew install git-credential-manager`
- **Linux:** See [GCM install docs](https://github.com/git-ecosystem/git-credential-manager/blob/release/docs/install.md)

### How it works

1. gitbox triggers the GCM login flow
2. For GitHub and GitLab, GCM opens your browser for OAuth authentication
3. For Gitea and Forgejo, GCM prompts for username and password (basic authentication)
4. GCM stores the credential automatically in its own storage
5. All discovery and git operations use that stored credential

**Storage:** GCM manages its own credential storage (OS keyring). gitbox extracts the OAuth token via `git credential fill` for API calls.

### Global gitconfig requirements

GCM dispatches per-host through the global `credential.helper` key in `~/.gitconfig`. Without a top-level `credential.helper = manager` and `credential.credentialStore = <keychain|wincredman|secretservice>`, `git credential fill` falls through to a TTY prompt — and in a GUI process that surfaces as the cryptic `fatal: could not read Password ... Device not configured` (errno ENXIO on `/dev/tty`).

gitbox detects this at startup whenever at least one account uses GCM. When the global `~/.gitconfig` is missing or wrong, a second orange warning banner appears in the GUI (and a second section on the TUI "Global Gitconfig" screen) with a **Configure** button that:

1. Writes `credential.helper = manager` + `credential.credentialStore = <os default>` to `~/.gitconfig`.
2. Backfills the same OS defaults into `gitbox.json` so the check stays green even if `~/.gitconfig` is edited later.

The OS defaults come from `pkg/credential.DefaultCredentialHelper()` and `pkg/credential.DefaultCredentialStore()`. Per-host overrides elsewhere in `~/.gitconfig` (for example `[credential "https://github.com"]` pinning `gh auth git-credential`) still take precedence over the global helper, so fixing the global entry is always safe.

### Browser detection

When you set up GCM credentials, gitbox needs to open a browser for OAuth authentication (GitHub, GitLab). Whether a browser can open depends on your environment:

- **Windows:** Always works — the system browser opens directly.
- **macOS:** Always works, even via SSH — macOS's `open` command forwards to the desktop session.
- **Linux desktop:** Works when a display server is available (X11 or Wayland).
- **Linux SSH / headless:** No browser available. The TUI shows "GCM browser authentication requires a desktop session" and suggests running the credential setup from a desktop terminal instead. GCM will still prompt interactively on the next `git clone` or `git fetch` if you proceed without browser auth.

This detection is handled by `credential.CanOpenBrowser()` in `pkg/credential/credential.go`. It checks `SSH_CLIENT`, `SSH_TTY`, `DISPLAY`, and `WAYLAND_DISPLAY` environment variables.

### GCM in the TUI

The TUI credential screen supports interactive GCM browser authentication on desktop sessions:

1. Navigate to an account → credential setup → GCM is selected
2. On desktop: press Enter → the browser opens for OAuth → return to the TUI when done
3. gitbox verifies the credential was stored and tests API access
4. If the GCM OAuth token doesn't have API scope (common with some providers), gitbox prompts for a separate PAT

On SSH or headless sessions, the TUI skips browser auth and shows guidance instead — either run from a desktop terminal or let GCM handle it on the next git operation.

### Mirrors with GCM

GCM OAuth tokens are machine-local and cannot be used by remote servers for mirroring. If you need mirrors, store a separate PAT:

```bash
gitbox account credential setup github-personal --token
```

This stores the PAT in `~/.config/gitbox/credentials/` alongside the GCM credential. The PAT is used for mirror operations; GCM continues to handle normal git operations.

## Token (Personal Access Token)

A PAT is a password-like string you generate on your provider's website. One token handles both discovery and git operations.

### How to create a PAT

When you set up a Token credential (in the GUI or CLI), gitbox shows you the exact URL to visit and which permissions to select. The steps are:

1. Click the link gitbox gives you (it opens your provider's token page)
2. Name the token something like `gitbox-<your-account>`
3. Select the permissions gitbox recommends
4. Copy the generated token
5. Paste it into gitbox

Here's what each provider needs:

| Provider            | Required permissions                                   |
| ------------------- | ------------------------------------------------------ |
| **GitHub**          | `repo` (full), `read:user`                             |
| **GitLab**          | `api`                                                  |
| **Gitea / Forgejo** | Repository: Read+Write, User: Read, Organization: Read |
| **Bitbucket**       | Repositories: Read+Write                               |

**Storage:** 1 secret → `~/.config/gitbox/credentials/<account>` (git-credential-store format, used by both gitbox and git CLI)

## SSH

SSH uses a cryptographic key pair for git operations. gitbox generates the key pair and configures everything for you.

However, SSH keys **cannot** call your provider's API, so discovery and repo creation require a separate PAT. This makes SSH a two-secret setup.

### Setup flow

1. gitbox creates an ed25519 key pair in `~/.ssh/`
2. It writes the necessary `~/.ssh/config` entry
3. It shows you the public key and a direct link to your provider's SSH key page
4. You paste the public key there and click save
5. gitbox verifies the SSH connection works
6. You store a PAT for API access (discovery, repo creation, mirrors)

### SSH companion PAT

The PAT stored for SSH accounts is used for:

- **Discovery** — listing repos from the provider API
- **Repo creation** — creating new repos via the provider API
- **Mirrors** — remote servers pushing/pulling on your behalf

It needs broader permissions than a read-only token:

| Provider | Required permissions |
| --- | --- |
| **GitHub** | `repo` (read/write), `read:user`, `read:org` |
| **GitLab** | `api` (full API access) |
| **Gitea / Forgejo** | Repository: Read+Write, User: Read, Organization: Read |
| **Bitbucket** | Repositories: Read+Write, Account: Read |

**Storage:** 2 secrets:

- **Operations** → SSH key pair in `~/.ssh/` (private key + public key + config entry)
- **API access** → PAT in `~/.config/gitbox/credentials/<account>` (raw token)

Without the PAT, the account works for git operations but cannot discover repos, create repos, or set up mirrors.

## Changing credentials

You can switch an account's credential type at any time. When you change the type:

- gitbox removes the old credential and its artifacts (tokens, keys, config entries)
- It sets up the new credential type
- All existing clones are automatically reconfigured to use the new credential

No need to re-clone anything.

## Verifying credentials

You can verify that credentials are working at any time:

- **GUI:** The credential badge on each account card shows green (working), orange (limited), or red (broken).
- **TUI:** Same badge colors on dashboard cards; account detail shows status with a message.
- **CLI:** `gitbox account credential verify <account-key>`

## Missing tools on the host

Credential types rely on external binaries that may not be installed on the current machine: GCM needs `git-credential-manager`, SSH needs `ssh` / `ssh-keygen` / `ssh-add`. Gitbox detects these before it starts a setup:

- **GUI** — opening the add-account or change-credential modal runs a pre-flight check. If a required tool is missing, you see a yellow banner naming it and the exact install command for your OS. The setup will not auto-run until you address it (you can still click through manually if you know what you're doing).
- **TUI** — `Credential → change type → <new type>` refuses to proceed when a tool is missing and prints the install hint as an error message.
- **CLI** — run `gitbox doctor` for a full inventory at any time (see [reference.md](reference.md#system-check-doctor)). Exit code is `1` when any tool required for your current config is missing, making it scriptable.

Typical fix on each OS:

- macOS: `brew install --cask git-credential-manager` (ssh/ssh-keygen are preinstalled).
- Linux: install your distro's GCM package (see the [GCM install docs](https://github.com/git-ecosystem/git-credential-manager/blob/main/docs/install.md)); `sudo apt install openssh-client` for SSH.
- Windows: GCM is bundled with Git for Windows; OpenSSH client is a built-in optional feature.
