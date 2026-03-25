# Credential Management

**gitbox** supports three credential types for authenticating with Git hosting providers. Each account declares its `default_credential_type`, and that credential handles everything for the account.

## Credential types at a glance

| Type      | Git operations                        | API access (discovery, status)        |
|-----------|---------------------------------------|---------------------------------------|
| **token** | PAT embedded in HTTPS URLs            | Same PAT                              |
| **gcm**   | Git Credential Manager (browser flow) | GCM's stored OAuth token              |
| **ssh**   | SSH key pairs                         | Optional (requires PAT or GCM setup)  |

The credential type you choose is the credential used for **everything**. There is no need to configure a separate PAT for API access when using `token` or `gcm`.

SSH is the exception: SSH key pairs cannot authenticate REST APIs. If you need discovery on an SSH account, you can optionally store a PAT with `--token`.

---

## Token (PAT)

The simplest credential type. A single PAT handles git operations (embedded in HTTPS clone URLs) and API access (discovery, status checks).

### Creating a PAT

#### GitHub

1. Visit: <https://github.com/settings/tokens/new>
2. Name: `gitbox-<account-key>` (e.g., `gitbox-github-personal`)
3. Scopes:
   - **repo** (full control of private repositories)
   - **read:user**
4. Click "Generate token" and copy it

#### GitLab

1. Visit: `<your-gitlab-url>/-/user_settings/personal_access_tokens`
2. Name: `gitbox-<account-key>`
3. Scopes: **api** (full API access, read+write repos)
4. Click "Create personal access token" and copy it

#### Gitea / Forgejo

1. Visit: `<your-server-url>/user/settings/applications`
2. Name: `gitbox-<account-key>`
3. Permissions:
   - **repository**: Read and Write
   - **user**: Read
   - **organization**: Read
4. Click "Generate Token" and copy it

#### Bitbucket

1. Visit: <https://bitbucket.org/account/settings/app-passwords/new>
2. Label: `gitbox-<account-key>`
3. Permissions: **Repositories — Read, Write**
4. Click "Create" and copy the app password

### Setup

```bash
gitboxcmd account credential setup <account-key>
```

Prompts for the PAT and stores it in the OS keyring.

### Verify

```bash
gitboxcmd account credential verify <account-key>
```

Checks the token exists and tests it against the provider API.

### Remove

```bash
gitboxcmd account credential del <account-key>
```

---

## GCM (Git Credential Manager)

GCM handles **all authentication** for the account: git operations via browser-based OAuth, and API access using the stored OAuth token (extracted automatically via `git credential fill`).

### Prerequisites

GCM is typically installed alongside Git for Windows. On macOS and Linux, install it separately:

- **macOS**: `brew install git-credential-manager`
- **Linux**: See [GCM install docs](https://github.com/git-ecosystem/git-credential-manager/blob/release/docs/install.md)

### Adding GCM credentials

```bash
gitboxcmd account credential setup <account-key>
```

If no credential is stored, this opens a browser window for GCM OAuth authentication. Once you log in, the credential is stored automatically. If a credential already exists, it reports success immediately.

### GCM provider hints

For self-hosted servers (Gitea, Forgejo, GitLab), GCM needs a provider hint to know which OAuth flow to use. gitbox sets this via `gcm.provider` in the account config:

| Provider          | GCM provider value |
|-------------------|--------------------|
| GitHub            | `github`           |
| GitLab            | `gitlab`           |
| Bitbucket         | `bitbucket`        |
| Gitea / Forgejo   | `generic`          |

For `generic` providers, GCM falls back to basic username/password prompts instead of OAuth.

### Verifying GCM credentials

```bash
gitboxcmd account credential verify <account-key>
```

Extracts the GCM credential and tests it against the provider API.

---

## SSH

SSH key pairs provide secure, passwordless authentication for **git operations**.

SSH keys cannot authenticate provider REST APIs. Discovery and status checks are optional for SSH accounts. If you need them, store a PAT:

```bash
gitboxcmd account credential setup <account-key> --token
```

### Key setup

1. **Generate a key pair** (one per account to support multi-account setups):

   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_<ssh-host-alias> -C "<your-email>"
   ```

   Example:

   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_gh-MyGitHubUser -C "myuser@example.com"
   ```

2. **Add the public key** to your provider:
   - **GitHub**: <https://github.com/settings/keys> → "New SSH key"
   - **GitLab**: `<url>/-/user_settings/ssh_keys`
   - **Gitea/Forgejo**: `<url>/user/settings/keys`
   - **Bitbucket**: `<url>/account/settings/ssh-keys/`

   Paste the contents of `~/.ssh/id_ed25519_<alias>.pub`.

3. **Add an entry to `~/.ssh/config`**:

   ```text
   Host gh-MyGitHubUser
       HostName github.com
       User git
       IdentityFile ~/.ssh/id_ed25519_gh-MyGitHubUser
       IdentitiesOnly yes
   ```

4. **Test the connection**:

   ```bash
   ssh -T gh-MyGitHubUser
   ```

   You should see a greeting like `Hi MyGitHubUser!`.

### Adding SSH credentials

```bash
gitboxcmd account credential setup <account-key>
```

Validates the SSH key and config entry.

### Verifying SSH credentials

```bash
gitboxcmd account credential verify <account-key>
```

Checks SSH key, config entry, and SSH connectivity. If a PAT is available, also tests API access.

---

## Verifying credentials on all platforms

```bash
# Windows
gitboxcmd account credential verify <account-key>

# macOS (remote)
ssh mac-host "/tmp/gitboxcmd account credential verify <account-key>"

# Linux (remote)
ssh linux-host "/tmp/gitboxcmd account credential verify <account-key>"
```

The output shows a checklist of what's configured and what's missing.

---

## Where are credentials stored?

| What              | Where                                                 |
|-------------------|-------------------------------------------------------|
| PATs (token type) | OS keyring (service: `gitbox`, user: account key)     |
| GCM credentials   | OS credential store (managed by GCM)                  |
| SSH keys          | `~/.ssh/` (key files and config, managed by the user) |
| Config (accounts) | `~/.config/gitbox/gitbox.json`                        |

PATs stored by gitbox are **never written to the config file**. They live exclusively in the OS keyring:

- **Windows**: Windows Credential Manager (DPAPI-encrypted)
- **macOS**: Keychain
- **Linux**: Secret Service (GNOME Keyring / KDE Wallet)
