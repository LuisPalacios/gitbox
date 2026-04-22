---
name: ship-builds
description: Cross-compile gitbox CLI and build GitboxApp GUI for remote hosts, then stage them at conventional paths for smoke testing on a feature branch. No arg ships to every configured host in .env in parallel. One arg ships to the host matching that short name. Use when the user says "ship to <host>", "send the builds", "deploy to <host> for testing", or invokes the skill as /ship-builds [host].
---

# Ship builds

Delegates to [scripts/ship.sh](../../../scripts/ship.sh). That script does the entire build+ship pipeline; this skill exists so Claude invokes it with zero ceremony.

## How to invoke

```bash
./scripts/ship.sh [host-short-name]
```

- Empty arg → every non-empty `SSH_*_HOST` in `.env`, shipped in parallel.
- One arg → host short name matched against `.env` (e.g. `obelix` matches `SSH_MAC_INTEL_HOST="luis@obelix"`).

## What it produces

On each remote target:

| Target     | CLI              | GUI                    |
| ---------- | ---------------- | ---------------------- |
| mac-arm    | `/tmp/gitbox`    | `/tmp/GitboxApp.app`   |
| mac-intel  | `/tmp/gitbox`    | `/tmp/GitboxApp.app`   |
| linux      | `/tmp/gitbox`    | `/tmp/GitboxApp`       |
| win        | `~/gitbox.exe`   | `~/GitboxApp.exe`      |

Per-host log at `/tmp/gitbox-ship-<platform>.log` on the **local** machine.

## After invoking

Read the script's final summary. For each platform, tell the user:

- Where `gitbox` / `GitboxApp` landed.
- How to launch it (`open /tmp/GitboxApp.app`, `/tmp/GitboxApp`, etc.).
- If any target failed, surface the last lines the script already printed — do NOT re-read the full log.

**When any mac target succeeded**, also forward the macOS Local Network post-ship block the script already printed. Do NOT re-invent or paraphrase the commands — the script's block is the single source of truth (so the two stay in sync on every edit). Just remind the user that the block exists and that it must be run once per fresh ship, as the user who will launch the GUI, NOT as root.

Keep the response short. The build logs are verbose; resist summarizing them unless the user asks.

## Notes

- CLI is cross-compiled locally (Go handles all 4 GOOS/GOARCH combos natively).
- GUI cannot be cross-compiled (wails needs each platform's native webview), so the script tars source → ssh → `wails build` on the remote. Remote hosts need Go + wails on `$PATH` for non-login SSH shells; the script exports the common prefixes (`/opt/homebrew/bin`, `/usr/local/bin`, `/usr/local/go/bin`, `$HOME/go/bin`) and applies the existing Scoop shim PATH fix on Windows.
- Remote scratch dir `~/gitbox-remote-build` is wiped + recreated each run.
- Parallel execution: if one target fails, others continue.
- **macOS post-ship:** a freshly shipped `/tmp/GitboxApp.app` has no code signature and a transient path, so macOS TCC silently blocks its first LAN connection instead of prompting. Every ship to a mac host needs a one-time (per ship) `/Applications/` copy + `codesign --sign -` + `tccutil reset LocalNetwork com.wails.GitboxApp` + relaunch, performed as the user who will run the GUI. The script's Summary prints the exact commands; forward them — do not substitute your own.
