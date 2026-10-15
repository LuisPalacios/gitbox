# Multiplatform testing

The developer workstation can be any OS. Remote machines are available via SSH for cross-platform testing. For runtime changes, I test the local platform and every remote configured in `.env`; documentation-only changes do not require GUI builds or shipping.

**Local first, then ship remotes.** The default test flow for runtime changes is: (1) build the GUI locally per the build rule, (2) smoke-test it on the local host, (3) `./scripts/ship.sh` to fan out to the remotes configured in `.env` (or `./scripts/ship.sh <short-name>` for one). Never skip step 1 — the local host is part of the cycle, and jumping straight to ship reports "green" without verifying on the machine you actually iterated on. If `.env` is absent or empty, the local build + smoke is the entire cycle.

Connection details are in `.env` (gitignored). Copy `docs/.env.example` to `.env` and fill in your SSH hosts. See `docs/multiplatform.md` for the full setup guide.

| Platform            | Env var              | Arch  | Wails target    | Script token              |
| ------------------- | -------------------- | ----- | --------------- | ------------------------- |
| Windows amd64       | `SSH_WIN_INTEL_HOST` | amd64 | `windows/amd64` | `win-intel` (alias `win`) |
| Windows arm64       | `SSH_WIN_ARM_HOST`   | arm64 | `windows/arm64` | `win-arm`                 |
| macOS Apple Silicon | `SSH_MAC_ARM_HOST`   | arm64 | `darwin/arm64`  | `mac-arm` (alias `mac`)   |
| macOS Intel         | `SSH_MAC_INTEL_HOST` | amd64 | `darwin/amd64`  | `mac-intel`               |
| Linux               | `SSH_LINUX_HOST`     | amd64 | `linux/amd64`   | `linux`                   |

Legacy `SSH_WIN_HOST` is still honored as a fallback for `SSH_WIN_INTEL_HOST` — older `.env` files keep working without a rename.

## Build-test-ship cycle

Scripts in `scripts/` automate the full cycle:

```bash
./scripts/ship.sh            # build GitboxApp on every remote in .env and stage it (+ test-gitbox.json)
./scripts/smoke.sh all       # GitboxApp --version on the local build and every staged remote
./scripts/test-commands.sh   # print GitboxApp --test-mode launch commands
./scripts/run-commands.sh    # print GitboxApp launch commands (real config)
./scripts/setup-credentials.sh all  # set up SSH keys + tokens on all platforms
```

The scripts auto-detect the local OS. Local commands run directly, remote commands use SSH. See each script's header for usage.

## Building the GUI on a remote host

`wails build` cannot cross-compile because each target needs the host's native webview (WebView2 on Windows, WebKit on macOS, WebKitGTK on Linux), so `scripts/ship.sh` builds the GUI **on each remote** over SSH. The manual recipe below is what it does under the hood; it uses `tar | ssh` (rsync isn't available in Git Bash on Windows by default).

```bash
# Build GUI for mac from a Windows or Linux host.
# For Apple Silicon: host=$SSH_MAC_ARM_HOST,   target=darwin/arm64
# For Intel Mac:     host=$SSH_MAC_INTEL_HOST, target=darwin/amd64
host="me@mac-host"       # from $SSH_MAC_ARM_HOST or $SSH_MAC_INTEL_HOST
target="darwin/arm64"      # or darwin/amd64 / darwin/universal

# 1. Wipe and prepare the remote scratch dir.
ssh "$host" 'rm -rf ~/gitbox-remote-build && mkdir -p ~/gitbox-remote-build'

# 2. Ship source (exclude artefacts, node_modules, .git, .env, and the Codex
#    symlinks, which Git Bash's tar on Windows can't create).
tar \
  --exclude='./.git' \
  --exclude='./build' \
  --exclude='./cmd/gui/build/bin' \
  --exclude='./cmd/gui/frontend/node_modules' \
  --exclude='./cmd/gui/frontend/dist' \
  --exclude='./.env' \
  --exclude='./AGENTS.md' \
  --exclude='./.agents' \
  -cf - . | ssh "$host" 'cd ~/gitbox-remote-build && tar -xf -'

# 3. Build on the remote. PATH needs Homebrew / $HOME/go/bin for non-login SSH shells.
#    Include both Homebrew prefixes so the recipe works on arm64 (/opt/homebrew)
#    and Intel (/usr/local) without tweaks.
ssh "$host" 'export PATH=/opt/homebrew/bin:/usr/local/bin:$HOME/go/bin:$PATH && \
  export CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 && \
  cd ~/gitbox-remote-build && \
  cp assets/appicon.png cmd/gui/build/appicon.png 2>/dev/null; \
  cd cmd/gui && wails build -platform '"$target"

# 4. Stage somewhere the user can double-click from Finder / open.
ssh "$host" 'rm -rf /tmp/GitboxApp.app && \
  cp -R ~/gitbox-remote-build/cmd/gui/build/bin/GitboxApp.app /tmp/GitboxApp.app'
```

Notes:

- **Non-login SSH shells** don't source `.zshrc` / `.bash_profile`, so `go` and `wails` are usually off `$PATH`. Prefix every remote command with `export PATH=/opt/homebrew/bin:/usr/local/bin:$HOME/go/bin:$PATH` — Apple Silicon Homebrew uses `/opt/homebrew/bin`, Intel Homebrew uses `/usr/local/bin`; including both keeps the same recipe working for `mac-arm` and `mac-intel`.
- **Tar from the repo root.** If the shell cwd is wrong, tar will happily ship a partial tree. Anchor with `cd "$(git rev-parse --show-toplevel)"` first.
- **`.env` is gitignored** — if the worktree doesn't have one yet, copy it in from the main clone (`cp ../gitbox/.env .`) before running `scripts/ship.sh` or `scripts/smoke.sh`.
- Linux builds the same way — swap `$host` to `$SSH_LINUX_HOST`, `$target` to `linux/amd64`, adjust PATH (e.g. `/usr/local/go/bin`). The artifact lands at `cmd/gui/build/bin/GitboxApp`.
- macOS signing/notarization is a separate concern — see `docs/macos-signing.md`. A `wails build` unsigned binary launches fine locally but gets the Gatekeeper quarantine on first download; for smoke tests that's fine.

## Non-interactive vs interactive tests

- **Non-interactive** (`GitboxApp --version`, unit tests): The agent runs via `./scripts/smoke.sh` or directly via SSH.
- **Interactive** (anything in the GUI window: onboarding, credential setup, clone, mirrors): The agent uses `./scripts/test-commands.sh` or `./scripts/run-commands.sh` to print the exact launch commands. The user runs them in a terminal inside each host's desktop session.
