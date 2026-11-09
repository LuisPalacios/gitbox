# Credential setup

Each Gitbox account needs credentials to do two things (and optionally a third):

- **Operations** — git operations like clone, fetch, and pull.
- **Discovery** — calling your provider's REST API to list your repositories, create new ones, and check their sync status.
- **Mirrors** (optional) — a portable PAT that remote servers use to push/pull on your behalf.

You choose one credential type per account. Depending on the type — and the provider — Gitbox may need one or two secrets to cover all three.

<p align="center">
  <img src="diagrams/credential-types.png" alt="Credential Types" width="850" />
</p>

## Which type should I use?

| Type      | Secrets                                                                  | Operations | Discovery / API                      | Mirrors     | Best for                                       |
| --------- | ------------------------------------------------------------------------ | ---------- | ------------------------------------ | ----------- | ---------------------------------------------- |
| **GCM**   | 1 (credential that GCM stores in the OS credential store)                | Yes        | Depends on provider — see note below | Needs a PAT | Desktop users (Windows, macOS, Linux with GUI) |
| **Token** | 1 (PAT in `~/.config/gitbox/credentials/`)                               | Yes        | Yes                                  | Same PAT    | All platforms, CI/CD, Gitea/Forgejo            |
| **SSH**   | 2 (SSH key in `~/.ssh/` + PAT in `~/.config/gitbox/credentials/`)        | Yes        | Needs the companion PAT              | Same PAT    | Users who prefer SSH keys                      |

### When do I need a PAT alongside GCM?

It depends on the provider:

- **GitHub, GitLab:** GCM stores an **OAuth token** that doubles as a valid API bearer. GCM alone covers discovery and repo creation. You only need a PAT for push/pull mirrors, where the token has to leave your machine.
- **Gitea, Forgejo, Bitbucket (basic auth):** GCM prompts you for a **username and password** and stores whatever you paste. These providers may refuse passwords at their REST API and accept only PATs. Pick one of two recovery paths:
  1. **Paste a PAT into GCM's password prompt** during setup. GCM stores it, and git and the API both work with a single credential. This is the easiest path.
  2. **Keep your password in GCM** (it works for `git push`/`pull`) and store a **separate PAT** in the Gitbox credential file for API operations. The **Setup API token** button in the account's Current-status panel does this.

Gitbox's credential check tells you which situation you're in. If the account reads **Credential works**, you're done. If it reads **Works, with a warning** and the API token row of the Current-status panel says _API rejected the GCM credential (HTTP 401)_, the API refuses your GCM credential: use one of the two recovery paths above.

## PAT storage

Gitbox stores every Personal Access Token (PAT) in one file per account, `~/.config/gitbox/credentials/<accountKey>` (file mode `0600`, readable only by the owner).

The file format depends on who reads it:

- **Token accounts** use **git-credential-store format** (`https://user:token@host`), because git reads this file directly through `credential.helper = store --file <path>`. Gitbox reads the same file for API calls.
- **SSH and GCM accounts** store the **raw token** (the PAT string, one line). Git never reads this file: SSH authenticates with the keys in `~/.ssh/`, and GCM keeps its own credential in the OS credential store. Only Gitbox reads it, for API calls (discovery, repo creation) and mirror operations.

Gitbox reads both formats: it parses the file as a URL first and falls back to a raw token.

When Gitbox needs a token, it looks in this order:

1. Account-specific environment variable (`GITBOX_TOKEN_<ACCOUNT_KEY>`, the account key uppercased with hyphens turned into underscores)
2. Generic `GIT_TOKEN` env var (CI fallback)
3. Credential file (`~/.config/gitbox/credentials/<accountKey>`)

## GCM (Git Credential Manager)

GCM handles everything through a single login. GCM stores one credential and uses it for git operations; whether it also works for the API depends on the provider (see [When do I need a PAT alongside GCM?](#when-do-i-need-a-pat-alongside-gcm)).

### Prerequisites

- **Windows:** already installed with Git for Windows.
- **macOS:** `brew install --cask git-credential-manager`
- **Linux:** see the [GCM install docs](https://github.com/git-ecosystem/git-credential-manager/blob/main/docs/install.md)

### How it works

1. Gitbox starts the GCM sign-in.
2. For GitHub and GitLab, GCM opens your browser for OAuth authentication.
3. For Gitea and Forgejo, GCM prompts for a username and password. To cover git and the API with one credential, paste a PAT into the password field.
4. GCM stores the credential in the OS credential store (Windows Credential Manager, macOS Keychain, or Secret Service on Linux).
5. Git operations use that credential, and Gitbox reads it through `git credential fill` for API calls.

GitboxApp always asks GCM for its own sign-in window, whatever your environment says. If the sign-in doesn't finish within 5 minutes, Gitbox stops waiting and names any `GCM_*` variables it found (see [Windows: GCM sign-in times out](#windows-gcm-sign-in-times-out)).

### Global gitconfig requirements

GCM needs two global entries in `~/.gitconfig`: a `credential.helper` that resolves to Git Credential Manager, and `credential.credentialStore = <keychain|wincredman|secretservice>`. Without them, `git credential fill` falls back to a terminal prompt, which fails in a GUI app with `fatal: could not read Password ... Device not configured`.

A helper resolves to GCM when it is `manager`, the legacy `manager-core`, or an absolute path to the `git-credential-manager` binary (the form `git-credential-manager configure` writes).

When at least one account uses GCM and these entries are missing or wrong, Gitbox shows an orange banner with a **Configure** button. It writes `credential.helper = manager` and the OS default `credential.credentialStore` to `~/.gitconfig`, and records the same defaults in `gitbox.json`. A helper chain that already resolves to GCM stays untouched. Browser sign-in during account setup runs the same fix first. Per-host overrides in `~/.gitconfig` (for example `[credential "https://github.com"]` pinning `gh auth git-credential`) still take precedence, so the fix is always safe.

### Browser detection

GCM opens a browser for OAuth sign-in (GitHub, GitLab). Whether a browser can open depends on your environment:

- **Windows:** always works — the system browser opens directly.
- **macOS:** always works, even via SSH — macOS's `open` command forwards to the desktop session.
- **Linux desktop:** works when a display server is available (X11 or Wayland).
- **Linux SSH / headless:** no browser available. GitboxApp itself needs a desktop session, so this only matters for a host you reach over SSH. GCM still prompts on the next `git clone` or `git fetch` from a terminal there. When browser sign-in isn't practical, switch the account to the Token credential type.

### Mirrors with GCM

GCM's OAuth tokens are machine-local, so remote servers can't use them for mirroring. If you need mirrors, store a separate PAT. When a mirror needs one, its row on the Mirrors tab shows a **Fix credentials** button that opens the API token setup for that account. When the GCM credential doesn't cover the API, the Current-status panel in the account's credential settings also offers **Setup API token**.

Gitbox stores this PAT in `~/.config/gitbox/credentials/`. The PAT handles mirror and API operations; GCM keeps handling git operations.

## Token (Personal Access Token)

A PAT is a password-like string you generate on your provider's website. One token handles both discovery and git operations.

### How to create a PAT

When you set up a Token credential, Gitbox shows the link to your provider's token page and the permissions to select:

1. Click the link Gitbox gives you (it opens your provider's token page).
2. Name the token something like `gitbox-<your-account>`.
3. Select the scopes for what you plan to do, as listed in [Token scopes by capability](#token-scopes-by-capability).
4. Copy the generated token.
5. Paste it into Gitbox.

**Storage:** 1 secret → `~/.config/gitbox/credentials/<account>` (git-credential-store format, read by both Gitbox and git).

## SSH

SSH uses a key pair for git operations. Gitbox generates the key pair and configures everything for you.

SSH keys **cannot** call your provider's API, so discovery and repo creation need a separate PAT. This makes SSH a two-secret setup.

### Setup flow

1. Gitbox creates an ed25519 key pair in `~/.ssh/`.
2. It writes the `~/.ssh/config` entry, with the host alias `gitbox-<account-key>`.
3. It shows you the public key and a direct link to your provider's SSH key page.
4. You paste the public key there and click save.
5. Gitbox verifies the SSH connection.
6. You store a PAT for API access (discovery, repo creation, mirrors).

### SSH companion PAT

The companion PAT covers:

- **Discovery** — listing repos from the provider API.
- **Repo creation** — creating new repos through the provider API.
- **Mirrors** — remote servers pushing/pulling on your behalf.

It needs the same scopes as a Token account's PAT: see [Token scopes by capability](#token-scopes-by-capability).

**Storage:** 2 secrets:

- **Operations** → SSH key pair in `~/.ssh/` (private key + public key + config entry).
- **API access** → PAT in `~/.config/gitbox/credentials/<account>` (raw token).

Without the PAT, the account works for git operations but can't discover repos, create repos, or set up mirrors.

## Token scopes by capability

This is the reference for every PAT Gitbox uses: Token accounts, SSH companion PATs, and the PAT a GCM account stores for mirrors or the API. The first column is the minimum every account needs. Each other column adds what one capability needs on top. Give each account only the scopes it needs, and add the destructive ones (delete) only when you plan to use them.

| Provider        | Discovery + clone + fetch            | Owner pickers (orgs)  | Create repo (**Create repo**, move destination) | Delete repo (move with "delete source")        | Mirror                                     |
| --------------- | ------------------------------------ | --------------------- | ----------------------------------------------- | ---------------------------------------------- | ------------------------------------------ |
| GitHub          | `repo`                               | `read:org`            | `repo`                                          | `delete_repo` (sensitive — add only if needed) | `repo`                                     |
| GitLab          | `api`                                | `api`                 | `api`                                           | `api`                                          | `api`                                      |
| Gitea / Forgejo | `read:repository`, `read:user`       | `read:organization`   | `write:repository`                              | `write:repository`                             | `write:repository`                         |
| Bitbucket Cloud | Repositories: Read, Write            | —                     | Repositories: Admin                             | Repositories: Delete (sensitive)               | Not available through the Bitbucket API    |

On a Gitea or Forgejo token page, these scopes appear as permission levels: **repository** Read and Write, **user** Read, and **organization** Read cover every column. On Bitbucket, Gitbox opens the app password page.

When a scope is missing, Gitbox shows a warning naming the scope and the page to regenerate the PAT, for example: _"Source repo delete refused: your github PAT is missing the `delete_repo` scope. Regenerate it at <https://github.com/settings/tokens> (keep existing scopes, add `delete_repo`), then store the new token in the account's credential settings."_

## Changing credentials

You can switch an account's credential type at any time. When you change the type:

- Gitbox removes the old credential and its artifacts (tokens, keys, config entries).
- It sets up the new credential type.
- It reconfigures every existing clone to use the new credential.

You don't need to re-clone anything.

Gitbox also repairs a clone's identity, remote and credential settings before each fetch. If a git include keeps overriding them, it warns and skips that clone's fetch.

## Verifying credentials

You can verify credentials at any time:

- Each account's sidebar entry and card show its health in words next to a mark: green (working), orange (limited), or red (broken or offline). Broken accounts also head the Needs attention view, with their fix button.
- A GitHub or GitLab token that expires within 14 days turns the health words orange: "Token expires in N days". On GitLab this needs GitLab 15.5 or later and a personal access token. A Token account whose token the server rejects (HTTP 401, for example once it expires) shows as failing.
- Pick **Change credential…** from the account's ⋯ menu to open the Change Credential modal. Its Current-status panel shows the real status of the primary credential and of the API token, with the underlying error message (if any).
- Click **Re-check** in that panel to run the verification again.

## Missing tools on the host

Credential types rely on external binaries that may be missing on the current machine: GCM needs `git-credential-manager`, SSH needs `ssh`, `ssh-keygen` and `ssh-add`. Gitbox checks for them before it starts a setup:

- **Setup flows** — opening the add-account or change-credential modal runs a pre-flight check. If a required tool is missing, a yellow banner names it with the install command for your OS. The setup doesn't run automatically until you install it and click **Re-check**; you can still proceed manually.
- **System check** — open **Settings → System check → Run** for a full inventory at any time: every tool Gitbox may call (`git`, `git-credential-manager`, `ssh`, `ssh-keygen`, `ssh-add`, and `wsl` on Windows), where it's installed, its version, and whether your current config needs it.

Typical fix on each OS:

- macOS: `brew install --cask git-credential-manager` (`ssh` and `ssh-keygen` are preinstalled).
- Linux: install your distro's GCM package (see the [GCM install docs](https://github.com/git-ecosystem/git-credential-manager/blob/main/docs/install.md)); `sudo apt install openssh-client` for SSH.
- Windows: GCM comes with Git for Windows and doesn't need to be on `PATH`: Gitbox asks `git credential-manager --version` first, then checks the Git for Windows and standalone GCM install folders. The OpenSSH client is a built-in optional feature.

## Troubleshooting

### Reading the GUI Current-status panel

Pick **Change credential…** from any account's ⋯ menu. The Current-status panel inside the Change Credential modal shows:

- **Primary (GCM/SSH/TOKEN):** the status of the primary credential (OK / Warning / Offline / Error / Not configured) and, below the row, the underlying error. That error text is the first place to look: it names the failing URL, the HTTP code, and what the network or TLS stack reported.
- **API token (PAT):** the status of the companion PAT (GCM and SSH only). When GCM already covers the API, the row reads _Not created_ and its detail starts with _API via GCM_. When no PAT covers the API, a **Setup API token** button appears next to the row; when a stored PAT fails, a **Replace API token** button appears instead.
- A **Re-check** button re-runs the verification without leaving the modal.
- A blue hint appears under the panel when the error looks like the OS blocking network access, with a link to the matching section below.

Copy that error text as-is when you file a bug report.

### "Repository not found" on clone

**Symptom.** A clone fails with `Repository not found`.

**Cause.** The `org/repo` name is wrong, or the account has no access to that repo.

**Fix.**

- Check that the `org/repo` name matches the actual repo.
- Check your credentials from a shell: `git ls-remote <url>`.
- For repos in another organization, make sure your account has access.

### HTTP 401 from Forgejo or Gitea

**Symptom.** A GCM account reads **Works, with a warning**, and the API token row says _API rejected the GCM credential (HTTP 401)_.

**Cause.** Either the GCM credential is wrong, or your Forgejo or Gitea server refuses passwords at its REST API. A real account password works for `git push` / `pull` but not for `/api/v1/*`.

**Fix.** One of:

- Delete the current GCM credential and redo the credential setup, pasting a **PAT** into GCM's password prompt. GCM stores it the same way, and it works for the API too.
- Keep GCM as it is and click **Setup API token** in the Current-status panel to store a companion PAT in the Gitbox credential file. The API uses the PAT; git operations keep using GCM.

### GCM opens the wrong browser account

**Symptom.** GCM setup signs in with a different account than the one configured in Gitbox.

**Cause.** The browser already holds a session for another login, and GCM reuses it.

**Fix.** Clear the stored credentials for that host, sign out of the other account in the browser, and redo the credential setup:

- **Windows:** Control Panel > Credential Manager > remove `git:https://github.com` entries
- **macOS:** Keychain Access > search `github.com` > delete
- **Linux:** `secret-tool clear protocol https host github.com`

### SSH verification fails

**Symptom.** The credential check fails for an SSH account.

**Cause.** The public key isn't registered at the provider, `~/.ssh/config` points at the wrong key, or `ssh-agent` isn't running.

**Fix.** Test the connection from a shell with the account's host alias (`ssh.host` in the account, `gitbox-<account-key>` by default) and read the verbose output:

```bash
ssh -T git@gitbox-<account-key> -v
```

Check that the public key is registered at the provider, that the `IdentityFile` in `~/.ssh/config` points at the right key, and that `ssh-agent` is running.

### DNS errors

**Symptom.** The detail line contains `no such host`, `server misbehaving`, or `i/o timeout` during DNS lookup.

**Cause.** This machine can't resolve the hostname in the account URL: a typo in the URL, a VPN or split-horizon DNS that isn't up, or a private DNS resolver that isn't configured.

**Fix.** Try `dig +short <host>` or `nslookup <host>` from a shell. Fix your DNS, then re-check in Gitbox.

### TLS / certificate errors

**Symptom.** The detail line contains `x509:`, `certificate signed by unknown authority`, or `tls: failed to verify certificate`.

**Cause.** The server presents a certificate that the system can't validate: usually a self-signed certificate, one signed by an internal CA that isn't in the system trust store, or a hostname mismatch.

**Fix.** Add the internal CA certificate to your OS trust store (macOS Keychain, Windows certificate store, or Linux `/usr/local/share/ca-certificates/` + `update-ca-certificates`). Gitbox uses the system trust store and has no per-app bypass.

### "Connection refused" (any OS)

**Symptom.** The detail line contains `connect: connection refused`.

**Cause.** The server is reachable, but nothing listens on the target port: the service is down, or the host or port is wrong.

**Fix.** Check the server URL in the account settings (click the account name on its card), then check the service on the server side.

### macOS: local network permission

**Symptom.** An account whose server is on your LAN (`192.168.x.x`, `10.x.x.x`, or a `.local` hostname) shows _Offline_, and the detail line contains `connect: no route to host`. `curl` or `git` from Terminal reach the same server.

**Cause.** macOS gates connections to the local network behind the **Local Network** privacy permission, per app. GitboxApp hasn't been approved yet, was denied (common for ad-hoc builds), or was moved or renamed, so macOS no longer recognizes it. Short refusals right after launch are normal: Gitbox retries offline credential checks, and fetches refused within milliseconds, for up to 10 seconds before it marks the account offline.

**Fix.** Quit GitboxApp (⌘Q), then as the user who runs it:

```bash
xattr -dr com.apple.quarantine /Applications/GitboxApp.app
codesign --force --deep --sign - /Applications/GitboxApp.app   # ad-hoc signature
tccutil reset LocalNetwork                                     # forget existing decisions
open /Applications/GitboxApp.app
```

The ad-hoc signature needs no Apple Developer account; it gives macOS a stable identity to remember the permission by. On the first LAN connection, macOS asks _"GitboxApp would like to find and connect to devices on your local network"_. Click **Allow**.

**If `tccutil` fails.** It often prints `tccutil: Failed to reset LocalNetwork`. Run it as your normal user, not with `sudo`, from a terminal that has **Full Disk Access**. macOS may still ask for permission on the next LAN connection anyway. If it doesn't, open _System Settings → Privacy & Security → Local Network_ and turn GitboxApp on. If GitboxApp isn't listed, relaunch it and click **Re-check** on a LAN account so macOS adds it.

### Windows: firewall

**Symptom.** An account on your LAN shows _Offline_, and the detail line contains `A connection attempt failed` or `No route to host`.

**Cause.** Windows Defender Firewall, or a third-party endpoint product, blocks outbound connections from `GitboxApp.exe` to the LAN. The rule is per executable, so a browser or `git.exe` getting through says nothing about GitboxApp.

**Fix.**

1. Open _Windows Security → Firewall & network protection → Allow an app through firewall_.
2. Click _Change settings_ → _Allow another app_ → browse to the installed `GitboxApp.exe`.
3. Check the **Private** and **Public** columns for the network you use.
4. If corporate endpoint software is in place, ask IT for a rule, giving them the executable path and the target server.

Re-check in the GUI once the rule is live.

### Linux: network troubleshooting

**Symptom.** An account on your LAN or a private IP shows _Offline_, and the detail line contains `connect: no route to host`, `connect: network is unreachable`, or `connect: connection refused`.

**Cause.** Linux desktops usually have no OS-level network permission. Either the network is down (VPN off, wrong interface, wrong subnet) or a host firewall (`ufw`, `firewalld`, nftables) blocks the outbound connection.

**Fix.**

1. Try the same request from a shell: `curl -sSI https://<host>/`. If that also fails, the problem is the network, not the app.
2. Confirm the VPN is up (if needed) and the route exists: `ip route get <server-ip>`.
3. Inspect your firewall: `sudo ufw status verbose`, `sudo firewall-cmd --list-all`, or `sudo nft list ruleset`. Allow outbound TCP to the target server.
4. If the shell reaches the server but the GUI doesn't, a sandbox (Flatpak, Snap) may restrict the app. Launch it from a plain terminal to confirm.

### macOS: "Device not configured" during GCM setup

**Symptom.** Setting up a GCM credential returns `fatal: could not read Password ... Device not configured`.

**Cause.** `credential.helper` or `credential.credentialStore` is missing or wrong in `~/.gitconfig`, so git falls back to a terminal prompt, which a GUI app doesn't have (see [Global gitconfig requirements](#global-gitconfig-requirements)).

**Fix.** Click **Configure** on the credential helper banner, then redo the credential setup.

### Windows: GCM sign-in times out

**Symptom.** Setting up a GCM credential never shows the GCM window and stops after 5 minutes with a message that names `GCM_*` variables. Or the sign-in works, but GCM keeps the secret in clear text under `%USERPROFILE%\.gcm\store\`. Another Windows user on the same PC, with the same `~/.gitconfig`, doesn't have the problem.

**Cause.** Per-user `GCM_*` environment variables override `~/.gitconfig`. Setup scripts meant for SSH or headless use sometimes set them:

- `GCM_GUI_PROMPT=false` makes GCM ask on a terminal, which GitboxApp doesn't have.
- `GCM_INTERACTIVE=never` makes the sign-in fail instead of prompting.
- `GCM_CREDENTIAL_STORE=plaintext` (or `dpapi`, `cache`) wins over `credential.credentialStore = wincredman`, so GCM keeps secrets outside Windows Credential Manager.
- `GCM_GITHUB_AUTHMODES=device` forces the GitHub device-code flow, whose code shows only on a terminal.

Gitbox forces GCM's window during sign-in, but the variables still change where and how GCM stores the credential. Remove them.

**Check.** As the affected user, in PowerShell:

```powershell
[Environment]::GetEnvironmentVariables('User').Keys | Where-Object { $_ -like 'GCM_*' }
[Environment]::GetEnvironmentVariables('Machine').Keys | Where-Object { $_ -like 'GCM_*' }
```

**Fix as the affected user.** Delete the user-level variables, then sign out and back in, because Explorer reads the environment only at sign-in:

```powershell
'GCM_GUI_PROMPT','GCM_INTERACTIVE','GCM_CREDENTIAL_STORE','GCM_GITHUB_AUTHMODES' |
  ForEach-Object { [Environment]::SetEnvironmentVariable($_, $null, 'User') }
```

A `Machine` hit needs an elevated PowerShell with `'Machine'` in place of `'User'`, and affects every user on the PC.

**Fix from an administrator account.** While the affected user is signed out, load their registry hive, delete the values and unload it. Run elevated, replace `<user>`, and keep the user signed out until `reg unload` succeeds, or Windows gives them a temporary profile:

```powershell
reg load 'HKU\gbx_tmp' 'C:\Users\<user>\NTUSER.DAT'
foreach ($n in 'GCM_GUI_PROMPT','GCM_INTERACTIVE','GCM_CREDENTIAL_STORE','GCM_GITHUB_AUTHMODES') {
  Remove-ItemProperty -Path 'Registry::HKEY_USERS\gbx_tmp\Environment' -Name $n -ErrorAction SilentlyContinue
}
[GC]::Collect(); reg unload 'HKU\gbx_tmp'
```

**Afterwards.** Sign in as the user, redo the GCM setup in GitboxApp and confirm the GCM window appears. Once the credential lives in Windows Credential Manager, delete `%USERPROFILE%\.gcm\store\`, which still holds the old secrets in clear text.

### Reporting an unusual error

Gitbox shows the underlying error verbatim, without translating or summarizing it. Copy the exact string into an issue at [github.com/LuisPalacios/gitbox/issues](https://github.com/LuisPalacios/gitbox/issues) along with:

- The OS and version (e.g. "macOS 14.5 Sonoma").
- The credential type in use (GCM / SSH / Token) and the provider (GitHub / Forgejo / ...).
- The error text from the Current-status panel in the account's credential settings.
- Whether the same request works from `curl` in a shell on the same machine.
