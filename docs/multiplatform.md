# Multiplatform development

Gitbox runs on Windows, macOS and Linux. The scripts in `scripts/` automate the build, ship and smoke-test cycle, so work happens on one machine and `GitboxApp` runs on the others over SSH.

The GUI can't be cross-compiled: each platform needs its own native webview (WebView2 on Windows, WebKit on macOS, WebKitGTK on Linux). The scripts therefore ship the source to each remote and run `wails build` there.

Without remote machines, unit tests and the scenario test still run locally, and CI covers the other platforms. See [Local-only testing](#local-only-testing).

## What you need

- The build prerequisites from [developer-guide.md](developer-guide.md#prerequisites) (Go, Node.js, the Wails CLI and the per-OS libraries) on the development machine and on every remote that builds the GUI.
- SSH key-based authentication to every remote (no passwords).
- Git Bash on Windows (it comes with Git for Windows).
- `jq` and `curl` on every machine, for the credential setup.
- A desktop session on each machine where the GUI is launched interactively.

## First-time setup

### 1. Configure SSH hosts

```bash
cp docs/.env.example .env
```

Edit `.env` with your remote SSH hosts. The scripts detect the local OS, so leave that platform's variable empty and set the others to `user@hostname`. Windows and macOS each split into two targets by architecture, so amd64 and arm64 machines can coexist in one `.env`:

| Variable             | Target      | Platform                     |
| -------------------- | ----------- | ---------------------------- |
| `SSH_WIN_INTEL_HOST` | `win-intel` | Windows amd64                |
| `SSH_WIN_ARM_HOST`   | `win-arm`   | Windows arm64                |
| `SSH_MAC_ARM_HOST`   | `mac-arm`   | macOS Apple Silicon (arm64)  |
| `SSH_MAC_INTEL_HOST` | `mac-intel` | macOS Intel (amd64)          |
| `SSH_LINUX_HOST`     | `linux`     | Linux amd64                  |

For example, on a Windows amd64 machine with two Macs and a Linux box as remotes:

```bash
SSH_WIN_INTEL_HOST=""
SSH_WIN_ARM_HOST=""
SSH_MAC_ARM_HOST="user@mac-arm-host"
SSH_MAC_INTEL_HOST="user@mac-intel-host"
SSH_LINUX_HOST="user@linux-host"
```

`docs/.env.example` lists more scenarios. Leave a variable empty or omit it to skip that platform. An older `.env` with a single `SSH_WIN_HOST` keeps working: the scripts fall back to it when `SSH_WIN_INTEL_HOST` is unset.

Check SSH before continuing:

```bash
ssh -o ConnectTimeout=5 user@mac-arm-host 'echo ok'
```

### 2. Prepare the test fixture

Copy `json/test-gitbox.json.example` to `test-gitbox.json` at the repo root and fill in real accounts and tokens. [testing.md](testing.md#the-test-fixture) explains the fixture and its `_test` key, and [credentials.md](credentials.md#how-to-create-a-pat) shows how to create each provider's token.

### 3. Set up credentials on all machines

```bash
./scripts/setup-credentials.sh all
```

For each target, the script copies `test-gitbox.json` and `scripts/test-setup-credentials.sh` to the remote and runs it there. On every machine it:

1. Verifies the API token of each account that has a `_test` key.
2. Generates an SSH key pair unique to that machine, named `test-<hostname>-<account>-sshkey` (existing keys are kept).
3. Adds a `Host` entry per account to `~/.ssh/config` (existing entries are kept).
4. Tests each SSH connection.

Then register each machine's public keys on your providers. For every key that fails verification, the script prints the page to open and the exact key to paste:

```text
  FAIL  gitbox-github-myaccount — public key not registered
        Add key at https://github.com/settings/keys: ssh-ed25519 AAAAC3... test-<hostname>-github-MyAccount
```

Run `./scripts/setup-credentials.sh all` again after registering the keys. Every line should show a green `ok`. The script is idempotent.

## Daily workflow

### Build locally

Start on the machine you work on: build the GUI as described in [developer-guide.md](developer-guide.md#building-from-source) and launch it there first. The output lands in `cmd/gui/build/bin/`.

### Ship to remotes

```bash
./scripts/ship.sh            # every configured remote, in parallel
./scripts/ship.sh myhost     # only the host whose short name matches
```

The short name is the host part of `user@host` without its domain; the full `user@host` value works too. For each remote, `ship.sh`:

1. Sends the source with `tar | ssh` into `~/gitbox-remote-build`, without `.git`, `.env`, `node_modules` or build output.
2. Builds the frontend, then runs `wails build -s` with the version and commit of the local checkout stamped through `-ldflags`, so the remote binary doesn't report `dev-none`.
3. Stages the result at `/tmp/GitboxApp.app` on macOS, `/tmp/GitboxApp` on Linux and `~/GitboxApp.exe` on Windows.
4. Copies `test-gitbox.json` to `~/test-gitbox.json` when it exists at the repo root.

Per-host logs go to `/tmp/gitbox-ship-<platform>.log`, and the summary shows the last lines of each failed log. After shipping to a Mac, `ship.sh` prints the one-time steps that let an app staged in `/tmp` reach servers on the local network; [credentials.md](credentials.md#macos-local-network-permission) explains why.

### Smoke test

```bash
./scripts/smoke.sh all
```

Runs `GitboxApp --version` on every platform: the local `wails build` output and the copies `ship.sh` staged on the remotes. The flag prints `GitboxApp <version> (<sha>)` and exits without opening a window, so it works over plain SSH. The script reports pass or fail per platform and exits non-zero on any failure.

### Interactive testing (test mode)

```bash
./scripts/test-commands.sh
```

Prints the command that launches `GitboxApp --test-mode` on each platform. The GUI needs the target's desktop session, so the script prints the commands instead of running them. Run each one in a terminal on that host:

```text
  Windows (amd64) (user@win-host):  cd ~ && ~/GitboxApp.exe --test-mode
  macOS (arm64):  cd "<repo>" && "<repo>/cmd/gui/build/bin/GitboxApp.app/Contents/MacOS/GitboxApp" --test-mode
  Linux (user@linux-host):  cd ~ && /tmp/GitboxApp --test-mode
```

Test mode runs the app against `test-gitbox.json` in a throwaway directory and never touches the real config or clones; [testing.md](testing.md#running-the-tests) has the details. The app finds the fixture by walking up from its working directory, which is why the remote commands start with `cd ~`. The script warns when a remote has no `~/test-gitbox.json`.

### Interactive testing (production)

```bash
./scripts/run-commands.sh
```

Same idea, but the printed commands launch `GitboxApp` against the real `~/.config/gitbox/gitbox.json` on each machine. On macOS they use `open` on the bundle.

[testing.md](testing.md#focused-regression-runs) describes how to measure startup on a native desktop.

### Sync production config to a remote

```bash
./scripts/send-my-production-config.sh mac-arm
```

Copies the local `~/.config/gitbox/gitbox.json` to one remote. It shows a diff and asks for confirmation first, because it overwrites the remote's config. It takes a single target and refuses `all`.

## Script reference

| Script                                  | What it does                                                    |
| --------------------------------------- | --------------------------------------------------------------- |
| `ship.sh [short-name]`                  | Build the GUI on each remote and stage it for testing           |
| `smoke.sh [target]`                     | Non-interactive smoke test (`GitboxApp --version`)              |
| `test-commands.sh [target]`             | Print the test-mode launch commands                             |
| `run-commands.sh [target]`              | Print the production-mode launch commands                       |
| `setup-credentials.sh [target]`         | Verify tokens and set up SSH keys on each target                |
| `send-my-production-config.sh <target>` | Copy the local production config to one remote                  |
| `test-setup-credentials.sh [path]`      | Low-level credential setup that `setup-credentials.sh` runs     |

**Targets:** `win-intel`, `win-arm`, `mac-arm`, `mac-intel`, `linux` or `all`. The aliases `win` and `mac` mean `win-intel` and `mac-arm`. Scripts that take `[target]` default to `all` and skip platforms with no host in `.env`. `ship.sh` takes a host short name instead and defaults to every configured remote. `ship.sh -h` prints its help.

## How it works

The scripts source `scripts/_common.sh`, which detects the local OS and loads `.env`. Commands for the local platform run directly; commands for the others run over SSH with the hosts from `.env`.

- The GUI builds locally into `cmd/gui/build/bin/`. On remotes it builds in `~/gitbox-remote-build` and is staged at `/tmp/GitboxApp[.app]` on Unix and `~/GitboxApp.exe` on Windows, because `scp` and Git Bash disagree on where `/tmp` is.
- `test-gitbox.json` goes to `~/test-gitbox.json` on remotes.
- SSH keys are named `test-<hostname>-<account>-sshkey`, so every machine has its own keys and all of them can be registered on a provider at once.
- Non-login SSH shells don't load the shell profile, so the scripts add the usual Go, Wails and Homebrew locations to `PATH` before building on a remote. On Windows remotes they also put Scoop's app folders ahead of its shims, which fail in SSH sessions.
- On a Windows development machine the scripts prefer the native OpenSSH in `C:\Windows\System32\OpenSSH`, which reaches SSH agents behind the Windows named pipe.

## Local-only testing

Without SSH access to other machines:

- Run the unit tests: `go test -short ./...`
- Run the scenario test: `go test ./pkg/ops/` (requires `test-gitbox.json`)
- Build for the local OS: see [developer-guide.md](developer-guide.md#building-from-source)
- Set up local credentials: `./scripts/setup-credentials.sh`

CI catches cross-platform regressions even without remotes: `pr.yml` runs the health check and a Linux GUI build on every pull request, and `ci.yml` builds every platform on each release tag. See [developer-guide.md](developer-guide.md#continuous-integration).

## Troubleshooting

**"Permission denied" on SSH.** Check that your public key is in `~/.ssh/authorized_keys` on the remote. The scripts require key-based authentication. Test with `ssh -o ConnectTimeout=5 user@host 'echo ok'`.

**"error in libcrypto" then "Permission denied" from Git Bash on Windows.** The keys live in an SSH agent (a password manager such as 1Password or Bitwarden, or the Windows ssh-agent service) and only `.pub` files exist on disk. Git Bash's own MSYS `ssh` can't reach the Windows named-pipe agent, so it tries to load the `.pub` as a private key and fails. The scripts already use the Windows-native OpenSSH when it exists. When running `ssh` by hand from Git Bash, put it first on `PATH`: `PATH="/c/Windows/System32/OpenSSH:$PATH" ssh host`.

**"command not found: jq" on a remote.** Install `jq` there (`apt install jq` on Debian or Ubuntu, `brew install jq` on macOS).

**"wails: command not found" during ship.** The remote build runs in a non-login shell. Install the Wails CLI on the remote with `go install github.com/wailsapp/wails/v2/cmd/wails@latest` and check that `$HOME/go/bin` exists. The per-host log in `/tmp/gitbox-ship-<platform>.log` shows the exact error.

**Test mode can't find `test-gitbox.json`.** Run `./scripts/ship.sh` or `./scripts/setup-credentials.sh <target>`; both copy the fixture to `~/test-gitbox.json` on remotes. Launch the app from the home directory (`cd ~`) so the upward search finds it.

**SSH timeout.** Add `ConnectTimeout 10` to that host's entry in `~/.ssh/config`.
