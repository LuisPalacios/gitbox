# The agentic ecosystem directory

Gitbox decides which AI harnesses to detect from one Markdown table, [`pkg/harness/tools-directory.md`](../pkg/harness/tools-directory.md). The table is embedded into `GitboxApp` with `//go:embed` and parsed by `pkg/harness`, so I add or remove a detected harness by editing a row there, never by changing Go code or adding a file here.

This page is the reference for how the parser reads that table and how detection turns rows into `global.ai_harnesses` entries. For how the menu uses those entries, see [gui-guide.md → AI harness actions](gui-guide.md#ai-harness-actions).

## Row format

The parser (`pkg/harness/harness.go`) reads every pipe-table row except the header (the row containing `Tool Name`) and the alignment row. It reads four columns by position:

- **Tool Name** (1st) — the display name in the menu and the key in `global.ai_harnesses`. Bold markers are stripped, so `**Claude Code**` becomes `Claude Code`.
- **Category** (3rd) — decides whether the row is detected, retired, or reference-only. It must match one of the values below exactly.
- **Executable / CLI Command** (5th) — the binary names to probe.
- **Well-known locations** (8th) — install directories outside a typical PATH.

The other columns (creator, OS compatibility, use case, URL) are reference text and never parsed.

## Categories

A row is auto-detected when its `Category` is one of:

- `Agentic CLI`
- `AI Harness`
- `Headless Harness`
- `Agentic IDE`
- `Agentic IDE / CLI`

Agentic IDEs (Cursor, Devin Desktop) count as AI tools, not editors: they launch from a terminal in a folder, so their entries appear in the AI harness section of the menu, not the editor section.

Every other category (`Agentic Framework`, `Orchestrator`, `Harness Builder`, cloud platforms, and so on) is reference-only. Those tools don't launch from a terminal in a folder, so the detector skips them.

## Executable names

The parser keeps every backticked token in the `Executable / CLI Command` cell that looks like a bare binary name: a letter followed by letters, digits, `_`, `.` or `-`. Tokens with spaces or paths, such as `` `python devika.py` ``, are dropped, and a row with no valid token is never detected. Text outside backticks, such as `_(or Docker)_`, is ignored.

A cell can list several names, for example `` `agent` or `cursor-agent` `` for the renamed Cursor CLI, or `` `devin-desktop` or `windsurf` ``. The first name is the primary binary; the rest are alternates, probed in order, so a renamed tool or one that ships under a different name per platform still resolves.

## Well-known locations

The `Well-known locations` cell lists per-tool install directories that are not on a typical PATH, each as its own backticked token, for example `` `%LOCALAPPDATA%\agy\bin` `` for Antigravity on Windows. `~`, `$VAR` and `%VAR%` are expanded when the probe runs. A cell with no backticked token, such as `_N/A_`, adds no directories.

For each binary name, the probe checks these places in order and stops at the first hit:

1. The row's well-known locations.
2. The Homebrew prefixes, on macOS.
3. `PATH`.
4. `~/.local/bin`, on every OS (native installers for Claude Code, Codex, Antigravity, Goose and uv-managed tools put binaries there, and a GUI started from the Dock, a desktop menu or the Start menu doesn't inherit the shell's PATH).

Tools that live on `PATH` or in `~/.local/bin` therefore need `_N/A_`, not a location. The probe never modifies the process environment, so it is safe to run while fetches and clones are in flight.

## Retired rows

When a vendor retires a CLI, I keep its row and set `Category` to `Retired CLI` (Gemini CLI is the current example). Retired rows are never detected, and the next sync removes any `global.ai_harnesses` entry whose `name` matches a retired row, so a dead binary stops showing up in the menu.

## Detection and config entries

Detection runs in the background, off the startup path: about two seconds after the window opens, then every ten minutes, and whenever the window regains focus (at most once a minute). The menu refreshes when the list changes, with no restart.

Each pass (`harness.Sync` in `pkg/harness/sync.go`) reconciles `global.ai_harnesses` with the table and the host:

- A detected row with no config entry is added with `source: "detected"` and the resolved binary path as `command`.
- Each entry has a `name`, a `command` (a binary path or on-PATH name), and an optional `args` array for harness-specific flags. Most harnesses need no flags.
- When an entry's `command` stops resolving, the entry is flagged `missing: true` and hidden from the menu, but never deleted. For a detected entry, gitbox first retries the row's binaries and well-known locations; a reinstall, even to another folder, clears the flag and updates `command`, and `args` stay untouched.
- Entries written by hand (`source: "user"`) are flagged and hidden the same way, but gitbox never rewrites their `command` or `args`.
- Entries with no `source` get one on the first pass: `detected` when the file name of `command` matches one of the row's binaries, `user` otherwise.
- Duplicate names collapse to the first entry. Catalog entries come first, in table order, followed by entries for tools outside the table in their original order.

## Adding a harness

1. Add a row to [`pkg/harness/tools-directory.md`](../pkg/harness/tools-directory.md) with one of the detected categories and the binary name in backticks in the `Executable / CLI Command` cell.
2. List alternate binary names in the same cell, primary first.
3. Fill `Well-known locations` only when the installer puts the binary outside `PATH`, `~/.local/bin` and the Homebrew prefixes; otherwise use `_N/A_`.
4. Optionally, give it a logo: prepare `logo-<name>.svg` with the `preparing-brand-logos` skill, as described in [developer-guide.md → Tool logos and top-bar icons](developer-guide.md#tool-logos-and-top-bar-icons), and map the harness to it in `harnessLogo` (`cmd/gui/frontend/src/lib/BrandIcon.svelte`). Without a mapping, the entry shows the generic AI icon.
5. Run `go test ./pkg/harness/`.
