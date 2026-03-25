# Migration Guide

## From git-config-repos.sh (v1) to gitbox (v2)

### Overview

gitbox uses a new configuration format (v2) stored at a different path. The migration is **non-destructive** — your original v1 config file is never modified.

| | v1 | v2 |
| - | -- | -- |
| **Tool** | `git-config-repos.sh` | `gitboxcmd` (CLI) / `gitbox` (GUI) |
| **Config file** | `~/.config/git-config-repos/git-config-repos.json` | `~/.config/gitbox/gitbox.json` |
| **Schema** | `git-config-repos.schema.json` | `gitbox.schema.json` |

### Running the Migration

```bash
# Automatic migration
gitboxcmd migrate

# What happens:
#   1. Reads ~/.config/git-config-repos/git-config-repos.json
#   2. Converts to v2 format
#   3. Writes ~/.config/gitbox/gitbox.json
#   4. Original file is NOT modified

# Dry run (preview without writing):
gitboxcmd migrate --dry-run
```

### Structural Change: accounts (WHO) + sources (WHAT)

The biggest change in v2 is splitting the flat v1 `accounts` into **two separate sections**:

| Section | Purpose | Contains |
| ------- | ------- | -------- |
| `accounts` | **WHO** you are on each server | Identity (name, email), credentials (SSH, GCM), provider, URL, username, `default_credential_type`, `default_branch` |
| `sources` | **WHAT** you clone from each account | References an account, lists repos in `org/repo` format, optional folder overrides |

This separation means one account (one login) can be referenced by multiple sources, and credential settings live in one place instead of being duplicated per repo.

### What Changes

| v1 | v2 | Notes |
| -- | -- | ----- |
| `"accounts": { ... }` | `"accounts": { ... }` + `"sources": { ... }` | Split into WHO + WHAT |
| Account contains repos | Account has NO repos; repos live in `sources` | Separation of concerns |
| Repo name: `"my-repo"` | Repo name: `"org/my-repo"` | Org extracted from v1 URL path |
| `"enabled": "true"` | *(removed)* | Global sections are now settings holders, not gatekeepers. Credential type is set per-account. |
| `"gcm_useHttpPath": "true"` | `"gcm": { "useHttpPath": true }` | Flat → nested + boolean (camelCase preserved, matches git's naming) |
| `"credentialStore": "wincredman"` | `"credential_store": "wincredman"` | camelCase → snake_case |
| `"ssh_host": "gh-User"` | `"ssh": { "host": "gh-User" }` | Flat → nested |
| `"ssh_hostname": "github.com"` | `"ssh": { "hostname": "github.com" }` | Flat → nested |
| `"ssh_type": "ed25519"` | `"ssh": { "key_type": "ed25519" }` | Flat → nested + rename |
| `"gcm_provider": "github"` | `"gcm": { "provider": "github" }` | Flat → nested |
| Per-repo `"credential_type"` | Optional — inherits from `account.default_credential_type` | Only set when different from the account default |
| (no version field) | `"version": 2` | Added |
| (no provider field) | `"provider": "github"` | Inferred from `gcm_provider` or set to `"generic"` |
| (no default_branch) | `"default_branch": "main"` | Added with default |
| (no default_credential_type) | `"default_credential_type": "gcm"` | Detected as most common credential across repos |
| Per-repo `"folder"` override | `"clone_folder"` (3rd level) or `"id_folder"` (2nd level) | More granular folder control |

### Credential Inheritance

In v2, repos **inherit** `credential_type` from their account's `default_credential_type`. During migration, the tool detects the most common credential type across all repos in a group and sets it as `default_credential_type` on the account. Individual repos only keep `credential_type` when it differs from that default.

For example, if 8 out of 10 repos use `gcm`:

- Account gets `"default_credential_type": "gcm"`
- Those 8 repos have empty `{}` bodies (inheriting the default)
- The 2 remaining repos keep their explicit `"credential_type": "ssh"` or `"credential_type": "token"`

### Organization Merging

When multiple v1 accounts point to the **same server** with the **same username**, they are automatically merged into a single v2 account + source. This is common for Gitea/Forgejo servers where one user belongs to multiple organizations.

**Before (v1 — 3 separate accounts):**

```json
{
    "accounts": {
        "git-example-personal": { "url": "https://git.example.org/personal", "username": "myuser", "folder": "git-example-personal", ... },
        "git-example-infra":   { "url": "https://git.example.org/infra",   "username": "myuser", "folder": "git-example-infra", ... },
        "git-example-myuser":  { "url": "https://git.example.org/myuser",  "username": "myuser", "folder": "git-example-myuser", ... }
    }
}
```

**After (v2 — 1 account + 1 source):**

```json
{
    "accounts": {
        "git-example": {
            "provider": "generic",
            "url": "https://git.example.org",
            "username": "myuser",
            "name": "My Name",
            "email": "myuser@example.com",
            "default_branch": "main",
            "default_credential_type": "gcm",
            "ssh": { "host": "gt-myuser", "hostname": "git.example.org", "key_type": "ed25519" },
            "gcm": { "provider": "generic", "useHttpPath": false }
        }
    },
    "sources": {
        "git-example": {
            "account": "git-example",
            "repos": {
                "personal/my-project": {},
                "infra/homelab": {},
                "myuser/config-json": {}
            }
        }
    }
}
```

**What happens:**

- The org is extracted from each v1 account's URL path (e.g., `/personal` → `personal`)
- Repos get prefixed with `org/` (e.g., `my-project` → `personal/my-project`)
- The merged account URL points to the server root (no org path)
- The account key is derived from the common prefix of original v1 names, or built as `hostname-username`
- Credentials, name, email are taken from the first account alphabetically
- `default_credential_type` is detected from the most common credential across all repos
- Repos with the default credential type get empty `{}` bodies (inheriting the default)

### Split-Account Handling

When v1 has two accounts with the **same (hostname, username)** but **different folders** (e.g., `github-myorg` with folder `"github-myorg"` and `github-myorg-rest` with folder `"github-myorg-rest"`), they merge into one account + source but repos from the non-primary folder get `id_folder` overrides to preserve the original directory layout.

**Example:** if `github-myorg` is the primary folder and `github-myorg-rest` is a secondary folder, repos from the secondary get:

```json
"MyOrg/myorg.github.io": {
    "id_folder": "github-myorg-rest"
}
```

This ensures cloned repos land in the same filesystem paths as before.

**When accounts are NOT merged:**

- Different server hostnames → separate accounts + sources
- Different usernames on the same server → separate accounts + sources

### Folder Structure (3 levels)

v2 uses a three-level directory structure under `global.folder`:

```text
global.folder / source-key / org / repo
     1st             2nd      3rd
```

Each level can be overridden:

- **1st level:** source key, or `source.folder` if set
- **2nd level:** part before `/` in repo key, or `repo.id_folder` if set
- **3rd level:** part after `/` in repo key, or `repo.clone_folder` if set

If `clone_folder` is absolute (starts with `/`, `~`, or `../`), it **replaces the entire path**, ignoring source folder and id_folder.

### What Stays the Same

- Global folder path
- Per-repo overrides (name, email)
- Global SSH and GCM settings (just restructured)

### Side-by-Side Usage

Both tools can coexist indefinitely:

- `git-config-repos.sh` continues reading `~/.config/git-config-repos/git-config-repos.json` (v1)
- `gitbox` reads `~/.config/gitbox/gitbox.json` (v2)

**Important:** Changes made in one tool are NOT automatically reflected in the other. If you add a new repo via `gitbox`, you'll need to manually add it to the v1 config too (or stop using `git-config-repos.sh`).

### Recommended Migration Path

1. **Run `gitboxcmd migrate`** to create the v2 config
2. **Verify the result:** `gitboxcmd global config show` and compare with your v1 config
3. **Test basic operations:** `gitboxcmd status` should show the same repos as `git-status-pull.sh`
4. **Use both tools in parallel** for a while to build confidence
5. **Switch fully to gitbox** once you're comfortable
6. **Keep the v1 config** as a backup — it doesn't hurt anything

### Manual Migration

If you prefer to migrate manually, the v2 format is documented in the [annotated example](../gitbox.jsonc). The key steps are:

1. **Create the `accounts` section** — one entry per unique (hostname, username) pair from v1. Deduplicate accounts that share the same server and login. Each account gets: `provider`, `url` (server root, no path), `username`, `name`, `email`, and optionally `default_branch`, `default_credential_type`, `ssh`, `gcm`
2. **Create the `sources` section** — one source per account (or more if you want folder separation). Each source references an account key via `"account": "..."` and contains a `"repos": { ... }` map
3. **Convert repos to `org/repo` format** — extract the org from the v1 URL path and prefix each repo name (e.g., if URL is `https://git.example.org/personal` and repo is `my-project`, the v2 key is `"personal/my-project"`)
4. **Set `default_credential_type`** on each account and remove `credential_type` from repos that match the default (they inherit it automatically)
5. **Convert string booleans** (`"true"` / `"false"`) to real booleans (`true` / `false`)
6. **Group SSH fields** into `"ssh": { "host", "hostname", "key_type" }` objects on accounts
7. **Group GCM fields** into `"gcm": { "provider", "useHttpPath" }` objects on accounts (note: `useHttpPath` is camelCase, matching git's own `credential.useHttpPath` setting)
8. **Rename `credentialStore`** to `credential_store` in global GCM settings
9. **Add `"version": 2`** at the root
10. **Save to `~/.config/gitbox/gitbox.json`**
