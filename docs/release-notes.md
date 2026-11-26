# Upcoming release notes

## Remote mode is always on

The host switcher next to the title shows in every window, with no flag or setting. Gitbox ignores the retired `--experimental-remote` flag, and the next save drops `experimental_remote` from `gitbox.json`. A `gitbox.json` without remote keys loads as before. See [Remote mode](remote-mode.md#the-host-switcher).

GitboxApp 2.7.4 and earlier remove the `remotes` list from `gitbox.json` when they save it, without a backup. Update every copy of GitboxApp on a machine before you add remotes there.

## Uninstall waits for approval, then stops

When an uninstall from the Remote hosts window waits for the SSH agent, the window says so with the host's name and, for 1Password, points at its menu bar or tray icon. A login still waiting after 75 seconds stops with `ssh_login`, and the uninstall goes on with the next host. Connections, reinstalls and doctor runs share the same limit.
