# Upcoming release notes

## Global Git hooks no longer pause every clone

Gitbox 2.8.0 and 2.8.1 paused fetch and pull on every clone when the global Git config set `core.hooksPath`, for example a personal folder of `commit-msg` and `pre-commit` hooks, and showed only `unsafe_config (core.hooksPath)`. Gitbox now trusts the global and system Git config. It pauses a clone only when the clone's own config sets `core.hooksPath` outside the clone, `core.fsmonitor` to a program, or a `url.*.insteadOf` rule that sends `origin` to another server. The row then explains the setting, where it lives and how to remove it, and result cards count those clones as skipped for safety. See [Credentials](credentials.md).

## Remote mode is always on

The host switcher next to the title shows in every window, with no flag or setting. Gitbox ignores the retired `--experimental-remote` flag, and the next save drops `experimental_remote` from `gitbox.json`. A `gitbox.json` without remote keys loads as before. See [Remote mode](remote-mode.md#the-host-switcher).

GitboxApp 2.7.4 and earlier remove the `remotes` list from `gitbox.json` when they save it, without a backup. Update every copy of GitboxApp on a machine before you add remotes there.

## Uninstall waits for approval, then stops

When an uninstall from the Remote hosts window waits for the SSH agent, the window says so with the host's name and, for 1Password, points at its menu bar or tray icon. A login still waiting after 75 seconds stops with `ssh_login`, and the uninstall goes on with the next host. Connections, reinstalls and doctor runs share the same limit. A refusal from 1Password after a slow login reads as the agent's error, with the same hint. After an uninstall, the host stays in the list and its card shows **Not installed** instead of the old server version.
