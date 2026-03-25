# Legacy Scripts

The original Bash scripts that preceded the Go-based **gitbox** tool. Both remain fully functional and independent — they read the v1 configuration format and require no Go tooling.

Legacy Scripts remain under [legacy/](legacy/README.md). They were an exercise to work with multi-account Git management: [Git Multicuenta](https://luispa.com/posts/2024-09-21-git-multicuenta/).

- **git-config-repos.sh** — Automated repo configuration (v1 format)
- **git-status-pull.sh** — Repo sync status and auto-pull

If you're starting fresh, use [gitbox](../README.md) instead. If you're migrating from these scripts, see the [Migration Guide](../docs/migration.md).

---

## git-config-repos.sh

Automated multi-account Git repository configuration. Reads `~/.config/git-config-repos/git-config-repos.json` (v1 format) and for each configured account/repo: verifies credentials, clones repos that don't exist, and fixes the configuration of existing ones.

**Supports:** GitHub, GitLab, Gitea with HTTPS+GCM or SSH authentication.

### Usage

```bash
./legacy/git-config-repos/git-config-repos.sh            # Run
./legacy/git-config-repos/git-config-repos.sh --dry-run   # Preview without changes
./legacy/git-config-repos/git-config-repos.sh --help      # Help
```

### Features

- Multi-account support (personal, corporate, homelab)
- GCM and SSH credential types per-repo
- Cross-platform: Linux, macOS, WSL2, Git Bash
- Split-account and cross-org patterns
- Per-repo overrides for name, email, folder, and credential type

### Documentation

- [Full README](git-config-repos/docs/README.md) (Spanish)
- [Annotated config example](git-config-repos/git-config-repos.jsonc)
- [JSON Schema](git-config-repos/git-config-repos.schema.json)

---

## git-status-pull.sh

Scans all `.git` directories from the current working directory and reports sync status. Can auto-pull repos that are safe to fast-forward.

### Usage

```bash
./legacy/git-status-pull/git-status-pull.sh              # Show status
./legacy/git-status-pull/git-status-pull.sh pull          # Auto-pull where safe
./legacy/git-status-pull/git-status-pull.sh -v            # Verbose output
./legacy/git-status-pull/git-status-pull.sh -v pull       # Verbose + auto-pull
```

### Features

- Detects: commits ahead/behind, divergence, uncommitted changes, stash, untracked files
- Color-coded output: CLEAN, NEEDS PULL, PULLING, MUST REVIEW
- Cross-platform: Linux, macOS, WSL2, Git Bash

### Documentation

- [Full README](git-status-pull/docs/README.md) (Spanish)

---

## Platform Detection

Both scripts share the same platform detection block:

| Platform | Detection | Git command |
| -------- | --------- | ----------- |
| Linux | default | `git` |
| macOS | `$OSTYPE == darwin*` | `git` |
| WSL2 | `/proc/version` contains "Microsoft"/"WSL" | `git.exe` |
| Git Bash | `$MSYSTEM` defined | `git` |

## Configuration (v1 format)

Both scripts use the v1 config file at `~/.config/git-config-repos/git-config-repos.json`. This is separate from the v2 config used by gitbox (`~/.config/gitbox/gitbox.json`). Both formats can coexist — changes in one are NOT reflected in the other.

To migrate from v1 to v2: `gitbox migrate`
