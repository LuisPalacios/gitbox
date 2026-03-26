# Migration Guide

## From git-config-repos.sh (v1) to gitbox (v2)

If you've been using `git-config-repos.sh`, you can migrate your configuration to gitbox with a single command. Your original config is **never modified**.

| | v1 (legacy) | v2 (gitbox) |
| --- | --- | --- |
| **Tool** | `git-config-repos.sh` | `gitboxcmd` (CLI) / `gitbox` (GUI) |
| **Config** | `~/.config/git-config-repos/git-config-repos.json` | `~/.config/gitbox/gitbox.json` |

## How to Migrate

### 1. Run the migration

```bash
gitboxcmd migrate
```

This reads your v1 config and creates a new v2 config. Your original file is left untouched.

Preview first without writing anything:

```bash
gitboxcmd migrate --dry-run
```

### 2. Verify

```bash
gitboxcmd status
```

You should see all the same repos as before.

### 3. Use both in parallel (optional)

Both tools can coexist — they read different config files. Use both for a while until you're comfortable, then switch fully to gitbox.

> **Note:** Changes made in one tool are NOT reflected in the other. Once you start using gitbox, stick with it.

## What the Migration Does

- **Accounts are split into WHO + WHAT.** Your identity (name, email, credentials) goes into `accounts`. Your repos go into `sources`. This means credentials live in one place instead of being repeated.
- **Accounts on the same server are merged.** If you had three v1 accounts pointing to the same Gitea server with the same username, they become one account with all repos in one source.
- **Repo names get an org prefix.** `my-project` becomes `personal/my-project` based on your v1 URL paths.
- **Credentials are inherited.** Instead of setting the credential type on every repo, the account has a default and repos only override when different.
- **Your folder structure is preserved.** Repos end up in the same filesystem paths as before.

## See Also

- [CLI Quick Start](cli-guide.md) — get started with gitbox after migrating
- [Reference](reference.md) — full config format documentation
- [Annotated example](../gitbox.jsonc) — complete v2 config with comments
