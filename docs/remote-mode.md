# Remote mode (experimental)

Remote mode lets GitboxApp on my laptop manage the repositories of another computer over SSH, such as a headless Linux server. GitboxApp installs a matching `gitbox-server` on that host by itself: no root, no internet on the host, no listening port. The design follows VS Code Remote-SSH.

This version connects, installs, verifies and uninstalls the server, and shows the connection. The repositories of the host appear in a later version. Only Linux hosts are supported for now; macOS and Windows hosts follow.

## Before I start: an alias that needs no prompt

Remote mode uses the system `ssh` and my own SSH configuration: `~/.ssh/config`, `known_hosts`, `ProxyJump` and my agent apply unchanged. It never adds `StrictHostKeyChecking`, `UserKnownHostsFile` or `-F`.

Each remote is a `Host` alias in my `ssh_config`. GitboxApp stores the alias and nothing else: no host name, user or key.

```text
Host my-server
  HostName my-server.example.org
  User me
```

Remote mode runs `ssh` with `BatchMode=yes`, so it never answers a prompt. Before I add a remote, `ssh my-server` must log in without asking anything:

- The key is loaded in an agent, or it has no passphrase. A host that only accepts passwords, or an encrypted key with no agent, fails with `ssh_auth`.
- The host key is already in `known_hosts`. If it is not, the connection fails at once with `ssh_hostkey`. I run `ssh my-server` once in a terminal and accept the key.
- The same holds for every hop of a `ProxyJump`.

## Turning remote mode on

Remote mode is off by default. It turns on with the `--experimental-remote` flag or with this key in my `gitbox.json`:

```json
"global": { "experimental_remote": true }
```

Either one shows **Remotes (experimental)** in Settings.

## Adding a remote

In Settings, under **Remotes (experimental)**, I type the alias and, if I need one, a server folder (`server_dir`), then **Add**.

GitboxApp checks the alias with `ssh -G <alias>`, which reads the configuration without connecting. That catches a broken `ssh_config`, not an unknown host: `ssh` resolves any name. A wrong host only shows when I connect. The remote is saved in the `remotes` list of this computer's `gitbox.json`.

## Connecting

**Connect** opens the remote in a window of its own: a new GitboxApp process started as `GitboxApp --remote <alias> --experimental-remote`. A second **Connect** brings the open window to the front. The remote window keeps running when I close the main window.

The window appears at once and connects in the background. It reads **Connecting to my-server…**, **Installing gitbox on my-server…** on the first connection, and **Connected to my-server** when the server answers. Advanced mode also shows the system of the host and the build of its server. The window has **Disconnect**, **Show log** and **Uninstall server**. Until the repositories arrive, it follows the light or dark theme of the system.

`--remote` without `--experimental-remote` exits with a message.

## What is installed and where

Everything lives under the **server root**: `server_dir` of the remote if I set one, otherwise `~/.gitbox-server`.

```text
<root>/<build>/gitbox-server       the server, mode 0700
<root>/<build>/in-use.<client id>  one per running server
<root>/.gc.lock/                   the garbage-collection lock
<root>/logs/<build>.log[.1..3]     the server log
```

`<build>` is `<version>-<commit>-<pin8>`, and `<pin8>` is the start of the SHA-256 that this GitboxApp pins for the binary. Two builds of the same commit can carry different bytes, so they install side by side. Every folder is created with mode 0700, and nothing goes to `/tmp`.

The server root must be a folder of its own. GitboxApp refuses a `server_dir` that is the home folder or the root folder, also through a link, with `server_dir_unsafe`.

### Install

The install runs only when the server of this build is missing:

1. GitboxApp probes the host for its system and architecture in one command.
2. It checks the free space with `df` and needs twice the size of the server.
3. It streams the server, which it carries inside, to a temporary file in the build folder.
4. It checks the SHA-256 of that file against the pin. A mismatch deletes the file before it ever runs.
5. It runs the file once with `--version`. On a home mounted `noexec` this fails, the file is deleted, and the error says to set `server_dir` to a folder that allows programs to run. GitboxApp never falls back to `/tmp`.
6. It renames the file into place, which is atomic. Two clients that install at once converge on the same file.

A server file that is already there with the wrong hash is never replaced silently. It fails verification, and the window offers **Reinstall server**, which deletes that build folder after I confirm and installs it again.

### Verify before every start

Every start verifies the server and starts it in the same remote command, so a connection to an installed server costs two SSH commands: the probe and the start. The command runs the server only when its hash equals the pin, and the client checks the hash again before it talks to the server.

This catches corruption, a truncated upload and a file replaced by mistake. It cannot stop an attacker who already runs code as my remote user and swaps the file between the check and the start. Against that attacker, the client treats everything the server says as untrusted.

### Garbage collection

After each start, the server deletes what nobody uses any more:

- every other build folder with no in-use marker refreshed in the last 10 minutes and no upload in progress;
- temporary upload files older than one hour;
- the logs of builds that are gone.

A running server refreshes its marker every minute and deletes its own marker, and no other, when it stops. Garbage collection takes the lock by creating `.gc.lock`, which works on NFS too, and breaks a lock older than 5 minutes, saying so in the log. It touches only names that gitbox creates, so other files in the server root stay.

## Uninstalling

**Uninstall server**, in the remote window or next to the remote in Settings, disconnects and removes the server from the host. If another window still uses the server there, GitboxApp asks again first.

It deletes only what gitbox put there: the build folders, `logs/`, `.gc.lock/` and temporary upload files. Then it removes the server root if it is empty. A file I put in the server root myself stays, and so does the root that holds it. The repositories and the `gitbox.json` of the host are not touched.

**Remove** in Settings only forgets the remote on this computer.

## Reading the connection log

**Show log** opens the connection log of the window: the last 2,000 lines, each with its time. It records:

- every step with its exit code and duration, and the size of the login banner as `[banner: N bytes]`;
- the install decisions, the hash of the uploaded file and the pin;
- what `ssh` and the server print on stderr.

**Copy log** copies it. **Run doctor** runs `gitbox-server --doctor` on the host in a separate command, verified like a start, and shows the server build, the git version, the config path and whether that folder allows writing and running programs.

On the host, the server also writes `<root>/logs/<build>.log`, rotated at 1 MiB with three older files kept.

The log never holds a token or a password: GitboxApp scrubs every line before it shows it.

## Error codes

Simple mode shows a plain sentence; the code itself appears in the log.

- `ssh_missing`: no `ssh` command on this computer. Install OpenSSH.
- `ssh_auth`: `ssh` could not log in without a prompt. Load the key in an agent or use a key without a passphrase, then run `ssh <alias>` once.
- `ssh_hostkey`: the host key is not in `known_hosts`, or it changed. Run `ssh <alias>` once in a terminal.
- `ssh_timeout`: the host did not answer in time.
- `ssh_config`: `ssh` cannot read the configuration of the alias. Fix `ssh_config`.
- `ssh_failed`: any other `ssh` failure, such as an unknown host or a refused connection. The log shows what `ssh` printed.
- `banner_overflow`: the login shell printed more than 1 MiB before the answer. Quiet the shell startup files for non-interactive sessions.
- `unsupported_os`: the host is not Linux. macOS and Windows hosts come in a later version.
- `unsupported_arch`: the host is neither amd64 nor arm64.
- `server_target_missing`: this GitboxApp carries no server for that host. A development build carries only the Linux ones; rebuild with the six targets.
- `server_build_mismatch` and `unstamped_build`: this GitboxApp was built without a matching stamp. Rebuild it as the developer guide says.
- `hash_mismatch`: the server on the host does not match the pin and did not run. Use **Reinstall server**.
- `version_mismatch`: the server that answered is another build. Connect again.
- `disk_full`: the server folder has less free space than twice the server.
- `no_hash_tool`: the host has none of `sha256sum`, `shasum` or `openssl`.
- `noexec`: the server folder does not allow programs to run. Set `server_dir` to a folder that does.
- `server_dir_unsafe`: `server_dir` is the home or the root folder. Choose a folder of its own.
- `server_crashed`: the server stopped while connected. The log shows its last lines.
- `not_connected`: the window is not connected.
- `not_implemented_remote`: the action has no remote side yet.

## Windows and macOS clients

On macOS and Linux, GitboxApp reuses one SSH connection for all its commands. When my `ssh_config` already runs a control master for the alias, it uses that one; otherwise it starts its own, with its socket in the gitbox config folder, kept for 60 seconds.

Win32-OpenSSH has no control master, so on Windows each command authenticates on its own: two per connection to an installed server, a few more during an install. With a key in the agent this is invisible.
