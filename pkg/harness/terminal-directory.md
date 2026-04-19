# The Terminal Directory

This document is the **authoritative list of known terminal emulators** for gitbox to auto-detect per platform. It is embedded into the GUI binary via `//go:embed` and parsed at startup: rows whose `OS` matches the current host are passed to the platform's detection path (`PATH` lookup, Homebrew-augmented `PATH` on macOS, `/Applications` check for `open -a` entries, Windows App Execution Alias for `wt.exe`, `Program Files\Git` for `git-bash.exe`).

The order in this table is the order in which `SyncTerminals` seeds `global.terminals` in a fresh config — users reorder existing entries in `gitbox.json` freely afterwards, and those edits are preserved across syncs. To add a new terminal gitbox should auto-detect, insert a row with its display name, OS, backticked command, and backticked default-arg tokens.

**Argument encoding.** The `Default Args` cell contains each argv element as its own backticked token, separated by spaces (e.g. `` `--workdir` `` `` `{path}` `` `` `-e` `` `` `{command}` ``). Use the literal `{path}` token to mark where the repo path goes, and `{command}` to mark where an AI harness's argv should be spliced. Entries that can't host a command (bare shells, `open -a Terminal`) simply omit `{command}` — harness launches there are either rejected with an actionable dialog or routed via the macOS `osascript` bridge for Terminal.app / iTerm.

**Windows Terminal profiles.** The `wt.exe` row is the bare-binary fallback. On Windows hosts where `settings.json` is parseable, gitbox discovers each visible WT profile at runtime and emits a per-profile entry (same `wt.exe` command, `--profile "<name>" -d "{path}" "{command}"` args) that supersedes this bare entry — that dynamic discovery is not driven by this table.

| Name | OS | Command | Default Args |
| :--- | :--- | :--- | :--- |
| **Windows Terminal** | Windows | `wt.exe` | `-d` `{path}` `{command}` |
| **Git Bash** | Windows | `git-bash.exe` | `--cd={path}` |
| **PowerShell 7** | Windows | `pwsh.exe` | |
| **WSL** | Windows | `wsl.exe` | `--cd` `{path}` |
| **Command Prompt** | Windows | `cmd.exe` | |
| **PowerShell 5** | Windows | `powershell.exe` | |
| **iTerm** | macOS | `open` | `-a` `iTerm` |
| **Terminal** | macOS | `open` | `-a` `Terminal` |
| **Warp** | macOS | `open` | `-a` `Warp` |
| **GNOME Terminal** | Linux | `gnome-terminal` | `--working-directory={path}` `--` `{command}` |
| **Konsole** | Linux | `konsole` | `--workdir` `{path}` `-e` `{command}` |
| **Kitty** | Linux | `kitty` | `--directory={path}` `{command}` |
| **Alacritty** | Linux | `alacritty` | `--working-directory` `{path}` `-e` `{command}` |
| **Xfce Terminal** | Linux | `xfce4-terminal` | `--working-directory={path}` |
| **Terminator** | Linux | `terminator` | `--working-directory={path}` |
