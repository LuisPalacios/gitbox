# The Shell Directory

This document is the **authoritative list of known command-line interpreters** for gitbox to auto-detect per platform. It is embedded into the GUI binary via `//go:embed` and parsed at startup: rows whose `OS` matches the current host are passed to the platform's shell-resolver path (`PATH` lookup, Homebrew-augmented `PATH` on macOS, login-shell from `/etc/passwd`, `Program Files\Git` for `git-bash.exe`).

The order in this table is the seed priority — `SyncProfiles` emits `shells[]` in this order on a fresh config so the resulting "Open in &lt;default profile&gt;" lands on a sensible choice without per-platform bias inside Go.

**Argument encoding.** The `Default Args` cell contains each argv element as its own backticked token, separated by spaces. Use `-l` for login shells and protocol-specific selectors like `-d {distro}` for WSL. The `{distro}` token is filled at runtime from `wsl --list --quiet`; the bare `wsl.exe` row stays as a fallback "WSL (default distro)".

**Per-distro WSL discovery.** The `wsl.exe` row is the bare fallback. On Windows hosts where `wsl --list --quiet` returns at least one distro, gitbox emits one Shell entry per distro at runtime (same `wsl.exe` command, `-d "<distro>"` args) that supersedes this bare entry — that dynamic discovery is not driven by this table.

**Login shell.** On macOS and Linux the host's login shell (read from `/etc/passwd` for the current `$USER`) is marked default among the inferred Shells, regardless of its position here. The bare entries below are only used to seed the candidate list.

| Name | OS | Command | Default Args |
| :--- | :--- | :--- | :--- |
| **PowerShell 7** | Windows | `pwsh.exe` | |
| **PowerShell 5** | Windows | `powershell.exe` | |
| **Git Bash** | Windows | `git-bash.exe` | |
| **Command Prompt** | Windows | `cmd.exe` | |
| **WSL (default distro)** | Windows | `wsl.exe` | |
| **Zsh** | macOS | `zsh` | `-l` |
| **Bash** | macOS | `bash` | `-l` |
| **Fish** | macOS | `fish` | `-l` |
| **Bash** | Linux | `bash` | `-l` |
| **Zsh** | Linux | `zsh` | `-l` |
| **Fish** | Linux | `fish` | `-l` |
