# Remote mode (experimental)

Remote mode lets GitboxApp on my laptop manage the repositories of another computer over SSH, such as a headless Linux server. GitboxApp installs a matching `gitbox-server` on that host by itself: no root, no internet on the host, no listening port. The design follows VS Code Remote-SSH.

This version connects, installs, verifies and uninstalls the server, shows the repositories of the host, changes its config and its clones, sets up token and SSH credentials on it, and opens its folders in my local editor, terminal and AI harnesses. Only Linux hosts are supported for now; macOS and Windows hosts follow.

## Before I start: an alias that needs no prompt

Remote mode uses the system `ssh` and my own SSH configuration: `~/.ssh/config`, `known_hosts`, `ProxyJump` and my agent apply unchanged. It never adds `StrictHostKeyChecking`, `UserKnownHostsFile` or `-F`.

Each remote is a `Host` alias in my `ssh_config`. GitboxApp stores the alias and nothing else: no host name, user or key.

```text
Host my-server
  HostName my-server.example.org
  User me
```

Remote mode runs `ssh` with `BatchMode=yes`, so it never answers a prompt. Before I add a remote, `ssh my-server` must log in without asking anything:

- The key is loaded in an agent, or it has no passphrase. A host that only accepts passwords, or an encrypted key with no agent, fails with `ssh_auth`. An agent that asks for approval for each app, such as 1Password, shows the request of Gitbox in its menu bar or tray icon rather than in a window; an unanswered one fails with `ssh_agent` after about a minute. When the login takes more than 3 seconds, the connecting window says so and, for 1Password, points at its icon.
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

The window appears at once and connects in the background. It reads **Connecting to my-server…** and **Installing gitbox on my-server…** on the first connection, with **Disconnect**, **Show log** and **Uninstall server**. Until the repositories arrive, it follows the light or dark theme of the system. When the server answers, the window shows the repositories of the host: see [What a remote window can do](#what-a-remote-window-can-do). Its title stays `gitbox · my-server`.

`--remote` without `--experimental-remote` exits with a message.

## What a remote window can do

The remote window is the full gitbox window, fed by the host. It needs a host that already has a `gitbox.json`, for example a computer that also runs GitboxApp; token and SSH credentials can be set up from the window. A host without one keeps the connection card, which says so; setting it up from here comes in a later version.

A text that speaks of this computer names the host instead, because the action runs there over SSH: the row menu reads **Delete from my-server…**, and the delete and move dialogs say which host's folder goes.

These work, all run on the host:

- the status of every repository, **Fetch all**, **Fetch** on one repository and the periodic status check;
- the repository details, the pull request badges and their lists, and the mirror status;
- **Find projects** of an account, which lists the repositories of the provider without adding them;
- credential checks, the sweep preview and the move preflight;
- **System check**, which shows the tools of the host;
- opening a repository or account folder of the host in VS Code, a terminal profile or an AI harness of this computer: see [Opening the host's folders](#opening-the-hosts-folders);
- the **Roots** window, which takes a folder typed for the host: see [Typing a folder of the host](#typing-a-folder-of-the-host).

These change the host's config, and also run there:

- editing, renaming and deleting an account;
- adding, removing and choosing the default root, and the nested scan depth;
- mirror groups and their repositories;
- adding discovered repositories to the config, and ignoring a clone;
- the settings that live in the host's `gitbox.json`, such as the pull request badges and the global gitignore check;
- the fixes of the host's git config (the global identity, the GCM helper and the global gitignore);
- repairing a broken `gitbox.json` or restoring one of its backups.

These change the host's clones and the providers, and also run there:

- cloning, pulling, deleting, creating and moving repositories;
- the sweep of merged branches, mirror setup and adding the mirrors that discovery found;
- the reconcile with the clones on disk, which also runs when the window gets focus, and adopting the clones it finds.

These set up the host's credentials, and also run there (see [Remote hosts](credentials.md#remote-hosts)):

- adding an account with a token, and storing, replacing or deleting its token;
- changing an account's credential type, except to GCM, which a host refuses;
- setting up, verifying and regenerating an account's SSH key, whose private key stays on the host.

The compact view and autostart are disabled, with the tooltip "Not available in a remote window": a remote window keeps them off.

These have no remote equivalent, so they are disabled with the tooltip "Not available for remote hosts": **Reveal in Finder** or **Reveal in Explorer**, opening the account folder, and **Open in Editor** of the config file in Settings.

My own preferences, such as language, theme, Simple or Advanced, the sidebar and the window size, still work and stay on this computer.

### Typing a folder of the host

A remote window never opens the folder dialog: it would pick a folder of this computer. Onboarding and the **Roots** window show a text field instead. I type a folder of the host, such as `~/git` or `/srv/git`, and 300 ms after I stop typing the host says what it resolves to: the absolute path, that it will be created, that it is a file, or that it cannot be written. The button stays off until the folder can be used.

The host checks every folder itself, whatever the window sent: it must be an absolute path once `~` is expanded, with no control characters, or the action fails with `remote_path_invalid`. Restoring a backup takes only a file of the backup list.

### When another window saved first

Several processes can save the host's `gitbox.json`: my windows, a GitboxApp running on the host and its terminals window. A save never overwrites a change another one made since I read the file. Instead it fails with "The settings changed in another window or process and were reloaded. Try again.", and the window shows what is in the file now. A change of my own preferences never fails this way.

If the connection drops while a change is being saved, the host's `gitbox.json` holds either the old version or the new one, never a mix: the file is replaced in one step. A change sent twice, for example after a timeout, is applied once: the host answers a repeat with the first answer, and after a reconnect a change that is already there fails with "That already exists" or "That no longer exists" instead of applying again. The AI harness detection on the host saves what it finds, and the window asks the host to detect its harnesses again when it gets focus. Fetch and pull repair each clone's `.git/config` first, as on a desktop. A collapsed account or repository stays collapsed in this window only.

### When the connection drops during a long action

A clone, pull, move or mirror setup runs on the host while the window that started it stays connected. Closing the window, disconnecting or losing the connection stops it there, together with its git processes:

- A clone stops, and git removes the folder it created. If the host killed git outright, the folder stays: the next reconcile reports it as a clone that stopped before it finished, and I delete the folder and clone again. Gitbox never deletes it for me.
- A pull stops, and git leaves the clone as it was or fast-forwarded.
- A move stops between two steps and never starts deleting the source repository or the local clone. A destination it already created stays on the provider, and the move reports it; once the origin points at the destination, the config says so too.
- A mirror setup stops, and what the provider already created stays.

The window marks the action as failed with the connection error, so nothing waits for an answer that will not come.

### The badge and the connection

The title bar shows a badge with the alias, the system of the host in Advanced mode, and the connection state. Clicking it opens the connection log.

When the connection drops, the window keeps the last state it got, dimmed and inert, under **Disconnected from my-server.** and a **Connect** button. While it is disconnected, the window starts no background reload or periodic check, so no other error appears. **Connect** runs the same connection again, clears what the disconnection left behind and reloads everything. The window then reconciles the host's clones and refreshes their status before it offers actions again, under **Reconnected to my-server. Reading its clones before the actions come back…**

### What the window trusts

Everything the host sends is untrusted. Links open only through one check on this computer: `https` links open in the browser, an `http` link opens after I confirm the full address, and anything else, such as `vscode:` or `ms-settings:`, is refused. Names and paths show as text and never run as a command. The window takes at most 10,000 repositories and 1,000 accounts; a longer list is cut, the badge warns, and the connection log says which list was cut.

## Opening the host's folders

The launchers of a remote window open the folder of the host from this computer, the way VS Code Remote-SSH does. The folder comes from the host, so GitboxApp never puts it in a command line as text. It checks it first: it must be an absolute path of the host, at most 4,096 bytes, with no NUL, CR or LF. Anything else fails with `not_available_remote_path` before anything starts. The alias always comes from my own `remotes` list, never from the host.

### Editors

VS Code (`code`) and VS Code Insiders (`code-insiders`) open the folder with `code --remote ssh-remote+<alias> <folder>`, each part as an argument of its own. VS Code needs its Remote-SSH extension, and it connects with my own `ssh` and the same alias. A workspace file of a container opens the same way.

Every other editor, such as Zed or VSCodium, shows disabled with "This editor can't open folders on remote hosts". The remote smoke test from macOS and Windows covers the VS Code entry; the Insiders entry takes the same command line from the Remote-SSH documentation.

Each remote can have its own editor. In Settings, under **Remotes**, the editor list of a remote offers **Auto** and the configured editors that open remote folders. With an editor chosen, the window of that remote opens folders only in it: the other editors stay visible, dimmed with "This remote opens in the editor chosen in Settings → Remotes", and a call that names another editor fails with `remote_editor_not_chosen`. **Auto** offers every editor that opens remote folders. The choice is saved in the `editor` field of the remote's entry in this computer's `gitbox.json`, and an open remote window uses it from its next launch. If the chosen editor is no longer installed, Settings shows it as not installed.

On Windows, `code` is a batch file that `cmd.exe` reads again, so a folder with `%`, `!`, `^`, `&`, `|`, `<`, `>` or `"` fails with `not_available_remote_path`.

### Terminals

A terminal profile opens a local terminal that runs `ssh -t <alias>` and changes into the folder on the host:

```text
ssh -t -- my-server 'cd -- '\''/srv/my repo'\'' && exec "$SHELL" -l'
```

The `cd` is written for the login shell the host reported: sh, bash, zsh, dash and ksh get POSIX quotes, and fish gets its own. Any other shell hands the `cd` to `sh` with single-quoted words only. Remote mode adds no `ssh` option here, so my `ssh_config` decides, and a passphrase or host key prompt shows in the terminal. On a host without a UTF-8 locale, fish shows a folder name with non-ASCII letters garbled in its prompt and `pwd`, but the terminal is in the right folder.

How the terminal starts depends on this computer:

- **macOS:** the profile's app runs `ssh`. Terminal.app and iTerm run it through my login shell, quoted for it, fish included; a profile whose app cannot run a command, such as `open -a Warp`, is refused.
- **Linux:** the profile's terminal runs `ssh` as its program, with no shell in between.
- **Windows:** a WezTerm or Alacritty profile with its default arguments runs `ssh` as its program, the way it does on Linux. Every other profile, including Windows Terminal, a shell profile, a terminal I added myself, or WezTerm or Alacritty with edited arguments, starts `ssh.exe` in a new console of its own, with no `cmd.exe` in between, and Windows shows it in its default terminal: those launchers read the command line again.

### AI harnesses

An AI harness runs on the host, in the folder, in a terminal of my default profile: the same `ssh -t` with `exec <harness>` after the `cd`. The harnesses are the ones detected on the host. Only a harness of the catalog runs, by its name or by an absolute path that ends in it, and with no arguments; anything else the host lists fails with `harness_not_allowed`.

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

A server file that is already there with the wrong hash, or that cannot be read, is never replaced silently. It fails verification, and the window offers **Reinstall server**, which deletes that build folder after I confirm and installs it again.

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

It deletes only what gitbox put there: the build folders, the build logs in `logs/`, `.gc.lock/` and temporary upload files. It removes `logs/` and then the server root only when they are empty. A file I put in the server root myself stays, and so does the root that holds it. The repositories and the `gitbox.json` of the host are not touched.

**Remove** in Settings only forgets the remote on this computer.

## Reading the connection log

**Show log** opens the connection log of the window: the last 2,000 lines, each with its time. It records:

- every step with its exit code and duration, and the size of the login banner as `[banner: N bytes]`;
- the install decisions, the hash of the uploaded file and the pin;
- what `ssh` and the server print on stderr.

**Copy log** copies it. **Run doctor** runs `gitbox-server --doctor` on the host in a separate command, verified like a start, and shows the server build, the git version, the config path and whether that folder allows writing and running programs.

On the host, the server also writes `<root>/logs/<build>.log`, rotated at 1 MiB with three older files kept.

The log never holds a token or a password: GitboxApp scrubs every line before it shows it. A token I type travels to the host once and is never logged on either side.

## Error codes

Simple mode shows a plain sentence; the code itself appears in the log.

- `ssh_missing`: no `ssh` command on this computer. Install OpenSSH.
- `ssh_auth`: `ssh` could not log in without a prompt. Load the key in an agent or use a key without a passphrase, then run `ssh <alias>` once.
- `ssh_agent`: the agent did not sign the login. An agent that asks for approval, such as 1Password, shows the request in its menu bar or tray icon and gives up after about a minute. Click **Connect** and approve it in time.
- `ssh_hostkey`: the host key is not in `known_hosts`, or it changed. Run `ssh <alias>` once in a terminal.
- `ssh_timeout`: the host did not answer in time.
- `ssh_config`: `ssh` cannot read the configuration of the alias. Fix `ssh_config`.
- `ssh_failed`: any other `ssh` failure, such as an unknown host or a refused connection. The log shows what `ssh` printed.
- `banner_overflow`: the login shell printed more than 1 MiB before the answer. Quiet the shell startup files for non-interactive sessions.
- `unsupported_os`: the host is not Linux. macOS and Windows hosts come in a later version.
- `unsupported_arch`: the host is neither amd64 nor arm64.
- `server_target_missing`: this GitboxApp carries no server for that host. A development build carries only the Linux ones; rebuild with the six targets.
- `server_build_mismatch` and `unstamped_build`: this GitboxApp was built without a matching stamp. Rebuild it as the developer guide says.
- `hash_mismatch`: the server on the host does not match the pin, or cannot be read to check it, and did not run. Use **Reinstall server**.
- `version_mismatch`: the server that answered is another build. Connect again.
- `disk_full`: the server folder has less free space than twice the server.
- `no_hash_tool`: the host has none of `sha256sum`, `shasum` or `openssl`.
- `noexec`: the server folder does not allow programs to run. Set `server_dir` to a folder that does.
- `server_dir_unsafe`: `server_dir` is the home or the root folder. Choose a folder of its own.
- `server_crashed`: the server stopped while connected. The log shows its last lines.
- `not_connected`: the window is not connected.
- `not_implemented_remote`: the action has no remote side yet.
- `not_available_remote`: the action has no remote equivalent, such as the file manager.
- `not_available_remote_path`: the folder is not a valid absolute path of the host, or a layer on this computer cannot carry it safely.
- `harness_not_allowed`: the AI harness is not one of the catalog, or it has arguments.
- `gcm_unsupported_remote`: the host does not run GCM, whose sign-in and store need a desktop session. Use a token: see [Remote hosts](credentials.md#remote-hosts).
- `read_only`: the window refuses the action without sending it, because the server of this remote does not serve it. Every action a remote window offers is served today.
- `remote_path_invalid`: the folder is not an absolute path of the host, or it holds a control character.

## Windows and macOS clients

On macOS and Linux, GitboxApp reuses one SSH connection for all its commands. When my `ssh_config` already runs a control master for the alias, it uses that one; otherwise it starts its own, with its socket in the gitbox config folder, kept for 10 minutes. A reconnect in that time logs in without a new signature, so an agent that asks for approval asks once.

Win32-OpenSSH has no control master, so on Windows each command authenticates on its own: two per connection to an installed server, a few more during an install. With a key in the agent this is invisible.
