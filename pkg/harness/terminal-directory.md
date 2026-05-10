# The Terminal Directory

This document is the **authoritative list of known terminal emulators** for gitbox to auto-detect per platform. It is embedded into the GUI binary via `//go:embed` and parsed at startup: rows whose `OS` matches the current host are passed to the platform's detection path (`PATH` lookup, Homebrew-augmented `PATH` on macOS, `/Applications` check for `open -a` entries, Windows App Execution Alias for `wt.exe`, `Program Files\Git` for `git-bash.exe`).

The order in this table is the order in which `SyncTerminals` seeds `global.terminals` in a fresh config — users reorder existing entries in `gitbox.json` freely afterwards, and those edits are preserved across syncs. To add a new terminal gitbox should auto-detect, insert a row with its display name, OS, backticked command, and backticked default-arg tokens.

**Argument encoding.** The `Default Args` cell contains each argv element as its own backticked token, separated by spaces (e.g. `` `--workdir` `` `` `{path}` `` `` `-e` `` `` `{command}` ``). Use the literal `{path}` token to mark where the repo path goes, and `{command}` to mark where an AI harness's argv should be spliced. Entries that can't host a command (bare shells, `open -a Terminal`) simply omit `{command}` — harness launches there are either rejected with an actionable dialog or routed via the macOS `osascript` bridge for Terminal.app / iTerm.

**Windows Terminal profiles.** The `wt.exe` row is the bare-binary fallback. On Windows hosts where `settings.json` is parseable, gitbox discovers each visible WT profile at runtime and emits a per-profile entry (same `wt.exe` command, `--profile "<name>" -d "{path}" "{command}"` args) that supersedes this bare entry — that dynamic discovery is not driven by this table.

**WezTerm `launch_menu`.** The `wezterm-gui.exe` (Windows), `wezterm-gui` (Linux), and `open -a WezTerm` (macOS) rows are the bare anchors. When `wezterm.lua` is found and its `config.launch_menu` table is parseable (best-effort regex parser; see `pkg/harness/wezterm.go`), gitbox emits one Profile per `launch_menu` entry whose `args` overrides the row's default-args template — same dynamic-discovery pattern as Windows Terminal.

**Terminals vs shells.** Rows whose `Command` is itself a shell (`cmd.exe`, `pwsh.exe`, `powershell.exe`, `git-bash.exe`, `wsl.exe`) are listed here for backward compatibility with the legacy v2.0 `global.terminals[]` flat model. The v2.1 Profile model treats them as Shells — see [`shell-directory.md`](shell-directory.md) — and pairs them with a real terminal app (Windows Terminal, WezTerm) at launch time. New entries go into the appropriate directory.

| Name | OS | Command | Default Args |
| :--- | :--- | :--- | :--- |
| **Windows Terminal** | Windows | `wt.exe` | `-d` `{path}` `{command}` |
| **WezTerm** | Windows | `wezterm-gui.exe` | `start` `--cwd` `{path}` `--` `{command}` |
| **Git Bash** | Windows | `git-bash.exe` | `--cd={path}` |
| **PowerShell 7** | Windows | `pwsh.exe` | |
| **WSL** | Windows | `wsl.exe` | `--cd` `{path}` |
| **Command Prompt** | Windows | `cmd.exe` | |
| **PowerShell 5** | Windows | `powershell.exe` | |
| **iTerm** | macOS | `open` | `-a` `iTerm` |
| **Terminal** | macOS | `open` | `-a` `Terminal` |
| **Warp** | macOS | `open` | `-a` `Warp` |
| **WezTerm** | macOS | `open` | `-a` `WezTerm` |
| **GNOME Terminal** | Linux | `gnome-terminal` | `--working-directory={path}` `--` `{command}` |
| **Konsole** | Linux | `konsole` | `--workdir` `{path}` `-e` `{command}` |
| **Kitty** | Linux | `kitty` | `--directory={path}` `{command}` |
| **Alacritty** | Linux | `alacritty` | `--working-directory` `{path}` `-e` `{command}` |
| **Xfce Terminal** | Linux | `xfce4-terminal` | `--working-directory={path}` |
| **Terminator** | Linux | `terminator` | `--working-directory={path}` |
| **WezTerm** | Linux | `wezterm-gui` | `start` `--cwd` `{path}` `--` `{command}` |
