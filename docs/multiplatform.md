# Multiplatform development

I test gitbox on three platforms: Windows, macOS, and Linux. The scripts in `scripts/` automate the build-deploy-test cycle so I can work from any OS and run gitbox on the other two via SSH.

To test all 3 platforms, you need SSH access to machines running the other two OSs (physical machines, VMs, or cloud instances). If you only have one machine, you can still run unit and integration tests locally — CI covers the other platforms on push.

All three developer workstation perspectives have been validated: Windows (v1.0.4), macOS (v1.0.5), and Linux (v1.0.6). Each validation runs the full cycle — credential setup, cross-compile, deploy, smoke tests, unit tests, integration tests, and interactive TUI verification on all 3 platforms.

## What you need

- **Go 1.26+** on your development machine (cross-compiles for all platforms)
- **SSH key-based auth** to your remote machines (no passwords)
- **Git Bash** on Windows (comes with Git for Windows)
- **jq** and **curl** on all machines (for credential setup)

## First-time setup

### 1. Configure SSH hosts

```bash
cp docs/.env.example .env
```

Edit `.env` with your remote SSH hosts. The scripts auto-detect your local OS, so leave that platform's variable empty. Set the others to `user@hostname`. Windows and macOS each split into two targets by arch — `SSH_WIN_INTEL_HOST` / `SSH_WIN_ARM_HOST` and `SSH_MAC_ARM_HOST` / `SSH_MAC_INTEL_HOST` — so amd64 and arm64 machines can coexist in one `.env`:

```bash
# Developing on Windows amd64, remotes are both Macs and Linux:
SSH_WIN_INTEL_HOST=""
SSH_WIN_ARM_HOST=""
SSH_MAC_ARM_HOST="user@mac-arm-host"
SSH_MAC_INTEL_HOST="user@mac-intel-host"
SSH_LINUX_HOST="user@linux-host"

# Developing on Apple Silicon, remotes include an amd64 Windows box, a
# Windows-on-ARM VM (Parallels/VMware Fusion), Intel Mac, and Linux:
SSH_WIN_INTEL_HOST="user@win-amd64-host"
SSH_WIN_ARM_HOST="user@win-arm-vm"
SSH_MAC_ARM_HOST=""
SSH_MAC_INTEL_HOST="user@mac-intel-host"
SSH_LINUX_HOST="user@linux-host"

# Developing on Linux, remote is a single Mac:
SSH_WIN_INTEL_HOST=""
SSH_WIN_ARM_HOST=""
SSH_MAC_ARM_HOST="user@mac-host"
SSH_MAC_INTEL_HOST=""
SSH_LINUX_HOST=""
```

Older `.env` files with a single `SSH_WIN_HOST` keep working — the scripts fall back to it when `SSH_WIN_INTEL_HOST` is unset.

Leave a variable empty or omit it to skip that platform. Verify SSH works before continuing:

```bash
ssh -o ConnectTimeout=5 user@mac-host 'echo ok'
```

### 2. Prepare the test fixture

```bash
cp json/test-gitbox.json.example test-gitbox.json
```

Edit `test-gitbox.json` and fill in real accounts and tokens. Each account with a `_test` key needs a valid API token — create them on your provider's website:

| Provider            | Where to create                                          | Required scopes                                        |
| ------------------- | -------------------------------------------------------- | ------------------------------------------------------ |
| **GitHub**          | Settings → Developer settings → Personal access tokens   | `repo` (full), `read:user`                             |
| **Gitea / Forgejo** | Settings → Applications → Manage Access Tokens           | Repository: Read+Write, User: Read, Organization: Read |
| **GitLab**          | Preferences → Access Tokens                              | `api` scope                                            |
| **Bitbucket**       | Personal settings → App passwords                        | Repositories: Read+Write                               |

See the [test-gitbox.json.example](../json/test-gitbox.json.example) for the full structure with inline comments.

### 3. Set up credentials on all machines

```bash
./scripts/setup-credentials.sh all
```

This does several things on each target:

1. Copies `test-gitbox.json` to the remote
2. Verifies API tokens against each provider
3. Generates SSH key pairs unique to that machine (named `test-<hostname>-<account>-sshkey`)
4. Writes SSH config entries for each account
5. Tests SSH connections

After running the script, register each machine's public keys on your providers. The script prints the exact key to paste for any that fail verification:

```text
  FAIL  gitbox-gb-github-personal — public key not registered
        Add key at https://github.com/settings/keys: ssh-ed25519 AAAAC3... test-bolica-gb-github-personal
```

Where to register SSH keys:

- **GitHub:** Settings → SSH and GPG keys → New SSH key
- **GitLab:** Settings → SSH keys → Add key
- **Gitea / Forgejo:** Settings → SSH / GPG Keys → Add Key
- **Bitbucket:** Personal settings → SSH keys → Add key

After registering all keys, re-run `./scripts/setup-credentials.sh all` to verify everything shows green `ok`.

## Daily workflow

### Build and deploy

```bash
./scripts/deploy.sh
```

Cross-compiles for all 3 platforms and SCPs binaries to every configured remote. Also copies `test-gitbox.json` if it exists. Takes about 10 seconds.

### Smoke test

```bash
./scripts/smoke.sh all
```

Runs `version`, `help`, and JSON output commands on all platforms. Non-interactive — the script runs everything and reports pass/fail.

### Interactive testing (test-mode)

```bash
./scripts/test-commands.sh
```

Prints the exact commands to run on each platform. Copy and paste them into your terminal. The output adapts to your `.env` — local platforms run directly, remotes use SSH:

```text
  Windows:  ssh luis@kymera  →  ~/gitbox.exe --test-mode
  macOS:    build/gitbox-darwin-arm64 --test-mode
  Linux:    ssh -t luis@luix "/tmp/gitbox --test-mode"
```

**Windows SSH note:** the TUI doesn't work with `ssh -t host "command"` on Windows Git Bash — it exits immediately. SSH into the machine first, then run the command (the two-step approach shown above with `→`).

**What is test-mode?** The `--test-mode` flag runs gitbox in an isolated temporary directory. It reads `test-gitbox.json` instead of your real config, creates all clones in a throwaway temp folder, and injects test tokens as environment variables. Nothing touches your real `~/.config/gitbox/` or existing clones. The temp directory is deleted automatically when gitbox exits.

### Interactive testing (production)

```bash
./scripts/run-commands.sh
```

Same idea, but uses the real `~/.config/gitbox/gitbox.json` on the target machine.

### Sync production config to a remote

```bash
./scripts/send-my-production-config.sh mac
```

Copies your local `gitbox.json` to the remote. Shows a diff and asks for confirmation first — this overwrites the remote's config.

## Script reference

| Script                                    | What it does                                             |
| ----------------------------------------- | -------------------------------------------------------- |
| `deploy.sh`                               | Build all 3 binaries + deploy to remotes                 |
| `smoke.sh [target]`                       | Non-interactive smoke tests                              |
| `test-commands.sh [target]`               | Print test-mode commands for the user to run             |
| `run-commands.sh [target]`                | Print production-mode commands for the user to run       |
| `setup-credentials.sh [target]`           | Set up SSH keys and verify tokens on target              |
| `send-my-production-config.sh <target>`   | Copy local production config to a remote                 |
| `test-setup-credentials.sh [path]`        | Low-level credential setup (called by setup-credentials) |

**Targets:** `win-intel`, `win-arm`, `mac-arm`, `mac-intel`, `linux`, or `all`. Back-compat aliases: `win` → `win-intel` and `mac` → `mac-arm` (the historical single-box defaults). Most scripts default to `all` available platforms when no target is given.

## How it works

The scripts auto-detect your local OS. For local operations, commands run directly. For remote operations, they use SSH with the hosts from `.env`.

- **Binaries** go to `build/` locally, `/tmp/gitbox` on Unix remotes, and `~/gitbox.exe` on Windows remotes
- **test-gitbox.json** goes to `~/test-gitbox.json` on remotes (gitbox walks up from cwd to find it)
- **SSH keys** are named `test-<hostname>-<account>-sshkey` so each OS has unique keys
- **Cross-compilation** happens on your dev machine — Go handles this natively

## Local-only testing

If you don't have SSH access to other machines, you can still:

- Run unit tests: `go test -short ./...`
- Run integration tests: `go test ./...` (requires `test-gitbox.json`)
- Build for your local OS: `go build -o build/gitbox ./cmd/cli`
- Set up local credentials: `./scripts/setup-credentials.sh`

CI (GitHub Actions) tests all 3 platforms on every push, so cross-platform regressions are caught automatically even without remotes.

## Troubleshooting

**"Permission denied" on SSH:**
Check that your SSH key is in `~/.ssh/authorized_keys` on the remote. The scripts require key-based auth (no passwords). Verify with: `ssh -o ConnectTimeout=5 user@host 'echo ok'`

**"command not found: jq" on remote:**
Install jq on the remote machine (`apt install jq` on Debian/Ubuntu, `brew install jq` on macOS).

**Binary crashes on remote:**
The deploy script handles GOOS/GOARCH automatically. If you built manually, verify: Windows amd64 = `windows/amd64`, Windows arm64 = `windows/arm64`, macOS Apple Silicon = `darwin/arm64`, macOS Intel = `darwin/amd64`, Linux = `linux/amd64`.

**test-mode can't find test-gitbox.json:**
Run `./scripts/deploy.sh` — it copies the fixture to `~/test-gitbox.json` on remotes. Or run `./scripts/setup-credentials.sh <target>` which also copies it.

**SSH timeout:**
Add `ConnectTimeout 10` to your `~/.ssh/config` for that host.
