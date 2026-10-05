# Multiplatform development

I test gitbox on three platforms: Windows, macOS, and Linux. The scripts in `scripts/` automate the build-ship-test cycle so I can work from any OS and run `GitboxApp` on the other two via SSH.

To test all 3 platforms, you need SSH access to machines running the other two OSs (physical machines, VMs, or cloud instances). If you only have one machine, you can still run unit tests and the scenario test locally — CI covers the other platforms.

The GUI can't be cross-compiled: each platform needs its own native webview (WebView2 on Windows, WebKit on macOS, WebKitGTK on Linux). The scripts therefore ship the source to each remote and run `wails build` there.

## What you need

- **Go 1.26+**, **Node.js 20.19+ or 22.12+**, and the **Wails CLI v2** on your development machine and on every remote that builds the GUI (see [developer-guide.md](developer-guide.md) for the per-OS libraries)
- **SSH key-based auth** to your remote machines (no passwords)
- **Git Bash** on Windows (comes with Git for Windows)
- **jq** and **curl** on all machines (for credential setup)
- **A desktop session** on each machine where you launch the GUI interactively

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

### Build locally

I always start on the machine I work on: build the GUI with `wails build` (see [developer-guide.md](developer-guide.md)) and launch it there first. The output lands in `cmd/gui/build/bin/`.

### Ship to remotes

```bash
./scripts/ship.sh            # every configured remote, in parallel
./scripts/ship.sh myhost     # only the host whose short name matches
```

Ships the source to each remote with `tar | ssh`, runs `wails build` there, and stages the result: `/tmp/GitboxApp.app` on macOS, `/tmp/GitboxApp` on Linux, and `~/GitboxApp.exe` on Windows. When `test-gitbox.json` exists at the repo root, it is copied to `~/test-gitbox.json` on each remote. Per-host logs go to `/tmp/gitbox-ship-<platform>.log`.

### Smoke test

```bash
./scripts/smoke.sh all
```

Runs `GitboxApp --version` on every platform — the local `wails build` output and the copies `ship.sh` staged on the remotes. The flag prints the version and exits without opening a window, so it works over plain SSH. Non-interactive — the script runs everything and reports pass/fail.

### Interactive testing (test-mode)

```bash
./scripts/test-commands.sh
```

Prints the exact command to launch `GitboxApp --test-mode` on each platform. The GUI needs the target's desktop session, so the commands are printed, not executed — run each one in a terminal on that host:

```text
  Windows (me@win-host):  cd ~ && ~/GitboxApp.exe --test-mode
  macOS:  cd "/path/to/gitbox" && "/path/to/gitbox/cmd/gui/build/bin/GitboxApp.app/Contents/MacOS/GitboxApp" --test-mode
  Linux (me@linux-host):  cd ~ && /tmp/GitboxApp --test-mode
```

**What is test-mode?** The `--test-mode` flag runs GitboxApp in an isolated temporary directory. It reads `test-gitbox.json` (walking up from the current directory) instead of your real config, creates all clones in a throwaway temp folder, and injects test tokens as environment variables. Nothing touches your real `~/.config/gitbox/` or existing clones. The temp directory is deleted automatically when the app exits.

### Interactive testing (production)

```bash
./scripts/run-commands.sh
```

Same idea, but the printed commands launch GitboxApp against the real `~/.config/gitbox/gitbox.json` on the target machine.

### Sync production config to a remote

```bash
./scripts/send-my-production-config.sh mac
```

Copies your local `gitbox.json` to the remote. Shows a diff and asks for confirmation first — this overwrites the remote's config.

## Script reference

| Script                                    | What it does                                                      |
| ----------------------------------------- | ----------------------------------------------------------------- |
| `ship.sh [short-name]`                    | Build the GUI on each remote and stage it for testing             |
| `smoke.sh [target]`                       | Non-interactive smoke test (`GitboxApp --version`)                |
| `test-commands.sh [target]`               | Print test-mode launch commands for the user to run               |
| `run-commands.sh [target]`                | Print production-mode launch commands for the user to run         |
| `setup-credentials.sh [target]`           | Set up SSH keys and verify tokens on target                       |
| `send-my-production-config.sh <target>`   | Copy local production config to a remote                          |
| `test-setup-credentials.sh [path]`        | Low-level credential setup (called by setup-credentials)          |

**Targets:** `win-intel`, `win-arm`, `mac-arm`, `mac-intel`, `linux`, or `all`. Back-compat aliases: `win` → `win-intel` and `mac` → `mac-arm` (the historical single-box defaults). Most scripts default to `all` available platforms when no target is given. `ship.sh` takes a host short name instead (e.g. `myhost` for `user@myhost`).

## How it works

The scripts auto-detect your local OS. For local operations, commands run directly. For remote operations, they use SSH with the hosts from `.env`.

- **The GUI** builds locally into `cmd/gui/build/bin/`, and on remotes into a scratch directory before it is staged at `/tmp/GitboxApp[.app]` on Unix and `~/GitboxApp.exe` on Windows
- **test-gitbox.json** goes to `~/test-gitbox.json` on remotes (GitboxApp walks up from the current directory to find it)
- **SSH keys** are named `test-<hostname>-<account>-sshkey` so each OS has unique keys
- **Non-login SSH shells** don't load your shell profile, so the scripts extend `PATH` with the usual Go, Wails, and Homebrew locations before building on a remote

## Local-only testing

If you don't have SSH access to other machines, you can still:

- Run unit tests: `go test -short ./...`
- Run the scenario test: `go test ./...` (requires `test-gitbox.json`)
- Build for your local OS: `cd cmd/gui && wails build`
- Set up local credentials: `./scripts/setup-credentials.sh`

CI (GitHub Actions) runs vet, unit tests, the frontend check, and a Linux GUI build on every pull request, and builds every platform on each release tag, so cross-platform regressions are caught even without remotes.

## Troubleshooting

**"Permission denied" on SSH:**
Check that your SSH key is in `~/.ssh/authorized_keys` on the remote. The scripts require key-based auth (no passwords). Verify with: `ssh -o ConnectTimeout=5 user@host 'echo ok'`

**"error in libcrypto" then "Permission denied" from Git Bash on Windows:**
Your keys live in an SSH agent (a password manager such as 1Password or Bitwarden, or the Windows ssh-agent service) and only `.pub` files exist on disk. Git Bash's own MSYS `ssh` cannot reach the Windows named-pipe agent, so it tries to load the `.pub` as a private key and fails. The scripts now prefer the Windows-native OpenSSH in `C:\Windows\System32\OpenSSH` automatically when it exists. If you run `ssh` by hand from Git Bash, prefix the PATH the same way: `PATH="/c/Windows/System32/OpenSSH:$PATH" ssh host`.

**"command not found: jq" on remote:**
Install jq on the remote machine (`apt install jq` on Debian/Ubuntu, `brew install jq` on macOS).

**"wails: command not found" during ship:**
The remote build runs in a non-login shell. Install the Wails CLI on the remote with `go install github.com/wailsapp/wails/v2/cmd/wails@latest` and check that `$HOME/go/bin` exists. Read the per-host log in `/tmp/gitbox-ship-<platform>.log` for the exact error.

**test-mode can't find test-gitbox.json:**
Run `./scripts/ship.sh` — it copies the fixture to `~/test-gitbox.json` on remotes. Or run `./scripts/setup-credentials.sh <target>` which also copies it. Launch the app from the home directory (`cd ~`) so the upward search finds the file.

**SSH timeout:**
Add `ConnectTimeout 10` to your `~/.ssh/config` for that host.
