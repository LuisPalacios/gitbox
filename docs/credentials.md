# Credential setup

Each gitbox account needs credentials to do two things (and optionally a third):

- **Operations** — git operations like clone, fetch, and pull.
- **Discovery** — calling your provider's REST API to list your repositories, create new ones, and check their sync status.
- **Mirrors** (optional) — a portable PAT that remote servers use to push/pull on your behalf.

You choose one credential type per account. Depending on the type — and the provider — gitbox may need one or two secrets to cover all three.

<p align="center">
  <img src="diagrams/credential-types.png" alt="Credential Types" width="850" />
</p>

## Which type should I use?

| Type      | Secrets                                                           | Operations | Discovery / API                      | Mirrors     | Best for                                       |
| --------- | ----------------------------------------------------------------- | ---------- | ------------------------------------ | ----------- | ---------------------------------------------- |
| **GCM**   | 1 (credential managed by GCM, cached in the OS keyring)           | Yes        | Depends on provider — see note below | Needs a PAT | Desktop users (Windows, macOS, Linux with GUI) |
| **Token** | 1 (PAT in `~/.config/gitbox/credentials/`)                        | Yes        | Yes                                  | Same PAT    | All platforms, CI/CD, Gitea/Forgejo            |
| **SSH**   | 2 (SSH key in `~/.ssh/` + PAT in `~/.config/gitbox/credentials/`) | Yes        | Needs the companion PAT              | Same PAT    | Users who prefer SSH keys                      |

### When do I need a PAT alongside GCM?

The honest answer is "it depends on the provider":

- **GitHub, GitLab:** GCM stores an **OAuth token** that doubles as a valid API bearer. GCM alone is enough for everything — discovery, repo creation, the lot. A PAT is only needed if you want push/pull mirrors, where the token has to leave your machine.
- **Gitea, Forgejo, Bitbucket (basic auth):** GCM prompts you for a **username and password** and stores whatever you paste. Those providers refuse passwords at their REST API — only PATs work there. Two recovery paths, pick one:
  1. **Paste a PAT into GCM's password prompt** during setup. GCM will cache it and git + API both work with a single credential. Easiest.
  2. **Keep your password in GCM** (works for `git push`/`pull`) and store a **separate PAT** in the gitbox keyring for API operations. The credential status panel in the GUI has a *Setup API token* button exactly for this case.

Gitbox's credential verification will tell you which of the two situations you're in. If the card badge is green, you're done. If it's "Warning" and the Current-status panel says *"GCM token found but API check failed"*, your GCM credential is a password that the API refuses — use one of the two recovery paths above.

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

GCM handles everything through a single login. One credential is stored by GCM itself and used for git operations; whether it also works for the API depends on what GCM cached (see note above).

### Prerequisites

- **Windows:** already installed with Git for Windows.
- **macOS:** `brew install git-credential-manager`
- **Linux:** see [GCM install docs](https://github.com/git-ecosystem/git-credential-manager/blob/release/docs/install.md)

### How it works

1. gitbox triggers the GCM login flow.
2. For GitHub and GitLab, GCM opens your browser for OAuth authentication.
3. For Gitea and Forgejo, GCM prompts for a username and password. If you want *just one credential* to cover git and the API, paste a PAT into the password field — GCM will cache it as the password.
4. GCM stores the credential automatically in its own storage.
5. All discovery and git operations use that stored credential.

**Storage:** GCM manages its own credential storage (OS keyring). gitbox extracts the cached secret via `git credential fill` for API calls.

### Global gitconfig requirements

GCM dispatches per-host through the global `credential.helper` key in `~/.gitconfig`. Without a top-level `credential.helper = manager` and `credential.credentialStore = <keychain|wincredman|secretservice>`, `git credential fill` falls through to a TTY prompt — and in a GUI process that surfaces as the cryptic `fatal: could not read Password ... Device not configured` (errno ENXIO on `/dev/tty`).

gitbox detects this at startup whenever at least one account uses GCM. When the global `~/.gitconfig` is missing or wrong, an orange warning banner appears in the GUI (and a section on the TUI "Global Gitconfig" screen) with a **Configure** button that:

1. Writes `credential.helper = manager` + `credential.credentialStore = <os default>` to `~/.gitconfig`.
2. Backfills the same OS defaults into `gitbox.json` so the check stays green even if `~/.gitconfig` is edited later.

The OS defaults come from `pkg/credential.DefaultCredentialHelper()` and `pkg/credential.DefaultCredentialStore()`. Per-host overrides elsewhere in `~/.gitconfig` (for example `[credential "https://github.com"]` pinning `gh auth git-credential`) still take precedence over the global helper, so fixing the global entry is always safe.

### Browser detection

When you set up GCM credentials, gitbox needs to open a browser for OAuth authentication (GitHub, GitLab). Whether a browser can open depends on your environment:

- **Windows:** always works — the system browser opens directly.
- **macOS:** always works, even via SSH — macOS's `open` command forwards to the desktop session.
- **Linux desktop:** works when a display server is available (X11 or Wayland).
- **Linux SSH / headless:** no browser available. The TUI shows "GCM browser authentication requires a desktop session" and suggests running the credential setup from a desktop terminal instead. GCM will still prompt interactively on the next `git clone` or `git fetch` if you proceed without browser auth.

This detection is handled by `credential.CanOpenBrowser()` in `pkg/credential/credential.go`. It checks `SSH_CLIENT`, `SSH_TTY`, `DISPLAY`, and `WAYLAND_DISPLAY` environment variables.

### GCM in the TUI

The TUI credential screen supports interactive GCM browser authentication on desktop sessions:

1. Navigate to an account → credential setup → GCM is selected.
2. On desktop: press Enter → the browser opens for OAuth → return to the TUI when done.
3. gitbox verifies the credential was stored and tests API access.
4. If the GCM-stored credential doesn't have API scope (common with Forgejo/Gitea when a password was cached), gitbox prompts for a separate PAT.

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

1. Click the link gitbox gives you (it opens your provider's token page).
2. Name the token something like `gitbox-<your-account>`.
3. Select the permissions gitbox recommends.
4. Copy the generated token.
5. Paste it into gitbox.

Here's what each provider needs:

| Provider            | Required permissions                                   |
| ------------------- | ------------------------------------------------------ |
| **GitHub**          | `repo` (full), `read:user`                             |
| **GitLab**          | `api`                                                  |
| **Gitea / Forgejo** | Repository: Read+Write, User: Read, Organization: Read |
| **Bitbucket**       | Repositories: Read+Write                               |

**Storage:** 1 secret → `~/.config/gitbox/credentials/<account>` (git-credential-store format, used by both gitbox and git CLI).

## SSH

SSH uses a cryptographic key pair for git operations. gitbox generates the key pair and configures everything for you.

However, SSH keys **cannot** call your provider's API, so discovery and repo creation require a separate PAT. This makes SSH a two-secret setup.

### Setup flow

1. gitbox creates an ed25519 key pair in `~/.ssh/`.
2. It writes the necessary `~/.ssh/config` entry.
3. It shows you the public key and a direct link to your provider's SSH key page.
4. You paste the public key there and click save.
5. gitbox verifies the SSH connection works.
6. You store a PAT for API access (discovery, repo creation, mirrors).

### SSH companion PAT

The PAT stored for SSH accounts is used for:

- **Discovery** — listing repos from the provider API.
- **Repo creation** — creating new repos via the provider API.
- **Mirrors** — remote servers pushing/pulling on your behalf.

It needs broader permissions than a read-only token:

| Provider            | Required permissions                                   |
| ------------------- | ------------------------------------------------------ |
| **GitHub**          | `repo` (read/write), `read:user`, `read:org`           |
| **GitLab**          | `api` (full API access)                                |
| **Gitea / Forgejo** | Repository: Read+Write, User: Read, Organization: Read |
| **Bitbucket**       | Repositories: Read+Write, Account: Read                |

**Storage:** 2 secrets:

- **Operations** → SSH key pair in `~/.ssh/` (private key + public key + config entry).
- **API access** → PAT in `~/.config/gitbox/credentials/<account>` (raw token).

Without the PAT, the account works for git operations but cannot discover repos, create repos, or set up mirrors.

## Changing credentials

You can switch an account's credential type at any time. When you change the type:

- gitbox removes the old credential and its artifacts (tokens, keys, config entries).
- It sets up the new credential type.
- All existing clones are automatically reconfigured to use the new credential.

No need to re-clone anything.

## Verifying credentials

You can verify that credentials are working at any time:

- **GUI:** the credential badge on each account card shows green (working), orange (limited), or red (broken). Click the badge to open the Change Credential modal — the Current-status panel at the top shows the real status with the underlying error message (if any).
- **TUI:** same badge colours on dashboard cards; account detail shows status with a message.
- **CLI:** `gitbox account credential verify <account-key>` prints the primary credential state and the API-access state, including the raw error when either fails.

## Missing tools on the host

Credential types rely on external binaries that may not be installed on the current machine: GCM needs `git-credential-manager`, SSH needs `ssh` / `ssh-keygen` / `ssh-add`. Gitbox detects these before it starts a setup:

- **GUI** — opening the add-account or change-credential modal runs a pre-flight check. If a required tool is missing, you see a yellow banner naming it and the exact install command for your OS. The setup will not auto-run until you address it (you can still click through manually if you know what you're doing).
- **TUI** — `Credential → change type → <new type>` refuses to proceed when a tool is missing and prints the install hint as an error message.
- **CLI** — run `gitbox doctor` for a full inventory at any time (see [reference.md](reference.md#system-check-doctor)). Exit code is `1` when any tool required for your current config is missing, making it scriptable.

Typical fix on each OS:

- macOS: `brew install --cask git-credential-manager` (ssh/ssh-keygen are preinstalled).
- Linux: install your distro's GCM package (see the [GCM install docs](https://github.com/git-ecosystem/git-credential-manager/blob/main/docs/install.md)); `sudo apt install openssh-client` for SSH.
- Windows: GCM is bundled with Git for Windows; OpenSSH client is a built-in optional feature.

## Troubleshooting

### Reading the GUI Current-status panel

Click the credential badge on any account card. The Current-status panel inside the Change Credential modal shows:

- **Primary (GCM/SSH/TOKEN):** the real status of the primary credential (OK / Warning / Offline / Error / Not configured) and, below the row, the underlying error as reported by gitbox's network/HTTP layer. That error text is the first place to look when something is wrong — it names the failing URL, the HTTP code, and whatever the kernel / TLS stack reported.
- **API token (PAT):** status of the companion PAT (only relevant for GCM and SSH). When the primary credential already covers the API, the PAT message says *"not needed — GCM covers the API"*. When the primary was rejected by the API, the PAT message says *"needed for discovery and repo creation"* and a **Setup API token** button appears next to it.
- A **Re-check** button re-runs the verification without leaving the modal.
- A blue advisory banner appears under the panel when gitbox detects the error looks like an OS-level network-permission denial (see macOS / Windows / Linux sections below).

The same information is available from the shell with `gitbox account credential verify <account>` — useful when you want to copy-paste an error into a bug report.

### macOS: local network permission

**Symptom.** The GUI shows *Offline* for an account whose server lives on your LAN (`192.168.x.x`, `10.x.x.x`, or a `.local` hostname). The detail line contains `dial tcp <ip>:<port>: connect: no route to host`. The CLI at the same moment — run from Terminal — reaches the same server without any trouble.

**Root cause.** macOS Sonoma and later gate outbound connections to local-network IPs behind the **Local Network** TCC privacy permission. Each GUI app is tracked separately by code-signing identity + bundle path. Terminal was granted access the first time you used it. Your gitbox bundle has either not been approved yet, was silently denied (common for ad-hoc built binaries), or has been moved/renamed so that TCC no longer recognises it.

**Fix — move the app to `/Applications/` and re-prompt.**

```bash
# Close the running GitboxApp first (⌘Q), then as the user who'll run it:
rm -rf /Applications/GitboxApp.app
cp -R /path/to/GitboxApp.app /Applications/GitboxApp.app
xattr -dr com.apple.quarantine /Applications/GitboxApp.app
codesign --force --deep --sign - /Applications/GitboxApp.app   # ad-hoc signature
tccutil reset LocalNetwork                                     # forget existing decisions
open /Applications/GitboxApp.app
```

The ad-hoc `codesign --sign -` does not require an Apple Developer account. What it buys you is a **stable code-designated requirement** — a hash that TCC can track across launches — so the permission sticks after you grant it.

On first LAN access the OS should pop *"GitboxApp would like to find and connect to devices on your local network"*. Click **Allow**. After that, `GitboxApp` appears in *System Settings → Privacy & Security → Local Network* with a green toggle.

#### When `tccutil reset` fails

The `tccutil` call is best-effort, and in recent macOS versions it often prints `tccutil: Failed to reset LocalNetwork` — sometimes even with `sudo` and even when Terminal has Full Disk Access. What's happening under the hood:

- `tccutil` modifies per-user TCC records. `sudo tccutil` edits **root's** TCC database, which is not the one your GUI session reads — so the error is partly a sign you ran it with the wrong identity. Run it as your normal user, not with `sudo`.
- The per-user TCC database lives under `~/Library/Application Support/com.apple.TCC/TCC.db`. SIP blocks direct writes and routes everything through `tccutil`; if Terminal (or your shell emulator) does not have **Full Disk Access** granted in *System Settings → Privacy & Security → Full Disk Access*, the call fails.
- Even when `tccutil` returns `Failed`, the OS may still re-prompt on the next LAN connect, because the internal decision cache is separate from the on-disk DB on newer releases. You saw this: the reset reported a failure, but GitboxApp still popped the "Allow Local Network" dialog when it reached the server.

Passing a bundle identifier (`tccutil reset LocalNetwork com.wails.GitboxApp`) narrows the reset to one app, but returns the same error whenever the broader reset already can't write. The bundle identifier for a stock gitbox build is `com.wails.GitboxApp` — `wails build` inherits that from its default `Info.plist` template since the repo does not override it. After an ad-hoc `codesign --sign -`, the identifier is preserved (you can confirm with `defaults read /Applications/GitboxApp.app/Contents/Info.plist CFBundleIdentifier`).

**If `tccutil` fails but the prompt never appears either** — skip it. Open *System Settings → Privacy & Security → Local Network* directly and flip the GitboxApp toggle on if it's listed. If it isn't listed at all, the app truly never tried a LAN connect yet (or TCC recorded it under a different identity from an earlier ship); relaunch, trigger a credential verification on a LAN account, and the OS will add it.

### macOS: "Device not configured" during GCM setup

**Symptom.** Setting up a GCM credential in the GUI returns `fatal: could not read Password ... Device not configured`.

**Root cause.** `git credential fill` tried to open `/dev/tty` for an interactive password prompt because `credential.helper` and `credential.credentialStore` were missing or wrong in `~/.gitconfig`. GUI processes have no controlling tty; the `open()` returns `ENXIO` and git surfaces the cryptic message.

**Fix.** Click the **Configure global gitconfig** banner in the GUI (or the equivalent item on the TUI Global Gitconfig screen). Gitbox writes the right entries and verifies. Redo the credential setup.

### Windows: firewall

**Symptom.** GUI shows *Offline* for an account on your LAN. Detail line contains `A connection attempt failed` or `No route to host` (Windows translates `WSAEHOSTUNREACH`).

**Root cause.** Windows Defender Firewall — or a third-party endpoint product — is blocking outbound connections from `GitboxApp.exe` to the LAN subnet. The block is per executable, so Chrome / Edge / git.exe being allowed says nothing about the Wails app.

**Fix.**

1. Open *Windows Security → Firewall & network protection → Allow an app through firewall*.
2. Click *Change settings* → *Allow another app* → browse to the installed `GitboxApp.exe`.
3. Check both the **Private** and **Public** columns for the network you actually use.
4. If corporate endpoint software is in place, it usually needs an admin-side rule — escalate to IT with the executable path and the target server.

Re-run the verification in the GUI once the rule is live.

### Linux: network troubleshooting

**Symptom.** GUI shows *Offline* for an account on your LAN or a private IP. Detail line contains `connect: no route to host`, `connect: network is unreachable`, or `connect: connection refused`.

**Root cause.** Linux desktops usually don't have an OS-level network-permission mechanism. Either the network itself is down (VPN off, wrong interface, wrong subnet) or a host-level firewall (`ufw`, `firewalld`, nftables) is blocking the outbound connection.

**Fix.**

1. Try the same request from a shell: `curl -sSI https://<host>/` — if that also fails, the issue is the network, not the app.
2. Confirm the VPN is up (if needed) and the correct routes exist: `ip route get <server-ip>`.
3. Inspect your firewall: `sudo ufw status verbose`, `sudo firewall-cmd --list-all`, or `sudo nft list ruleset`. Allow outbound TCP to the target server.
4. If you reach the server from shell but not from the GUI, the Wails binary may have been started in a restricted namespace — rare, typically only seen under Flatpak/Snap sandboxing. Launch from a plain Terminal to confirm.

### HTTP 401 from Forgejo or Gitea

**Symptom.** The Current-status panel shows *Primary (GCM): Warning* with `gitea: authentication failed (HTTP 401) — token is invalid or expired`.

**Root cause.** Forgejo and Gitea refuse username/password authentication at the REST API. Your GCM-stored credential is probably a real account password — it works for `git push` / `pull` but not for `/api/v1/*`.

**Fix.** One of:

- Delete the current GCM entry and redo credential setup, pasting a **PAT** into GCM's password prompt. The PAT caches the same way but works for the API too.
- Leave GCM as-is and click **Setup API token** inside the Current-status panel to store a companion PAT in the gitbox keyring. The API will use the PAT, git operations keep using GCM.

### "Connection refused" (any OS)

**Symptom.** Detail line contains `connect: connection refused`.

**Root cause.** The server is reachable but nothing is listening on the target port — either the service is down, or you're hitting the wrong host/port.

**Fix.** Confirm the server URL in the account config (`gitbox account list --json`), then check service status on the server side. This is never an app-side problem.

### DNS errors

**Symptom.** Detail line contains `no such host`, `server misbehaving`, or `i/o timeout` during DNS lookup.

**Root cause.** The hostname in the account URL can't be resolved from this machine. Possible reasons: typo in the account URL, VPN / split-horizon DNS is required but isn't up, private DNS resolver isn't configured.

**Fix.** Try `dig +short <host>` or `nslookup <host>` from a shell. Fix your DNS before re-verifying in gitbox.

### TLS / certificate errors

**Symptom.** Detail line contains `x509:`, `certificate signed by unknown authority`, or `tls: failed to verify certificate`.

**Root cause.** The server is presenting a certificate that the Go HTTP client can't validate — usually a self-signed cert, a cert signed by an internal CA that isn't in the system trust store, or a hostname mismatch.

**Fix.** Add the internal CA certificate to your OS trust store (macOS Keychain, Windows cert store, or Linux `/usr/local/share/ca-certificates/` + `update-ca-certificates`). Gitbox uses the system trust store via `net/http`; it has no per-app bypass and intentionally won't offer one.

## Token scopes by capability

Different gitbox operations need different PAT scopes. The baseline (list + clone + fetch + pull) is the minimum every account needs; create, delete, and mirror each add one or more scopes on top. Give each account only the scopes it needs — add the destructive ones (delete) only when you plan to use them.

| Provider         | Discovery + clone + fetch         | Create repo (GUI "Create repo" / move dest) | Delete repo (move with "delete source")  | Mirror (push/pull between accounts)      |
| ---------------- | --------------------------------- | ------------------------------------------- | ---------------------------------------- | ---------------------------------------- |
| GitHub           | `repo`, `read:org`                | `repo`                                      | `delete_repo` (sensitive — add only if needed) | `repo` plus the destination's scopes |
| GitLab           | `api`                             | `api`                                       | `api`                                    | `api`                                    |
| Gitea / Forgejo  | `read:repository`, `read:organization` | `write:repository`                     | `write:repository` (or admin on target)  | `write:repository` on destination        |
| Bitbucket Cloud  | `repository`                      | `repository:admin`                          | `repository:delete`                      | `repository:admin` on destination        |

When a scope is missing, gitbox surfaces a warning naming the exact scope required plus the provider URL to regenerate the PAT — e.g. _"Source repo delete refused: your github PAT is missing the `delete_repo` scope. Regenerate it at https://github.com/settings/tokens (keep existing scopes, add `delete_repo`), then re-run `gitbox account credential setup <account>`."_

The TUI + GUI account setup wizard shows the same per-capability table when you store a PAT, so you can decide which scopes to include up front.

### When the error says something unusual

Gitbox surfaces the Go-level error verbatim — it does not translate or summarise it. Copy the exact string into an issue at [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues) along with:

- The OS and version (e.g. "macOS 14.5 Sonoma").
- The credential type in use (GCM / SSH / Token) and the provider (GitHub / Forgejo / ...).
- The output of `gitbox account credential verify <account-key>`.
- Whether the same request works from `curl` in a shell on the same machine.

That's usually enough to tell network issues apart from app-level issues in a single round-trip.
