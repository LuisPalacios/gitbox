Last updated: 2026-04-08

- Goal (incl. success criteria):
  - Fix "Open in editor" not detecting installed editors on macOS
  - Success: GUI and CLI/TUI both find editors (code, cursor, zed) installed via Homebrew on macOS

- Constraints/Assumptions:
  - macOS GUI apps inherit minimal PATH excluding `/opt/homebrew/bin` and `/usr/local/bin`
  - Project already solves this for git commands via `git.Environ()` / `ensureHomebrewPATH()` in `pkg/git/git.go`
  - Editor detection is GUI-only (`cmd/gui/app.go`); TUI reads editors from `config.Global.Editors` which is populated by GUI's `SyncEditors()`
  - Wails GUI cannot be cross-compiled from Windows to macOS (needs CGO for Objective-C bindings) — must build natively on mac
  - Mac SSH host: `luis@bolica` (arm64, Go 1.26.1 at `/opt/homebrew/bin/go`, no Wails CLI installed)

- Key decisions:
  - Added `lookPathWithBrewPATH()` helper in `cmd/gui/app.go:487-504` that temporarily sets Homebrew-augmented PATH before `exec.LookPath`
  - Applied fix to three callsites: `DetectEditors()`, `SyncEditors()`, `OpenInApp()`
  - W6 (Open in browser) marked as shipped

- State:
  - Done:
    - `/wish done W6` — moved to Shipped section in feature-radar.md
    - `/wish` skill updated with new `list` subcommand
    - Fix implemented in `cmd/gui/app.go` — three callsites patched
    - CLI binary cross-compiled and deployed to mac (`luis@bolica:~/gitbox`)
  - Now:
    - Build GUI binary on macOS natively (can't cross-compile Wails from Windows)
    - Test editor detection on macOS with the fix
  - Next:
    - Verify editors are detected in GUI on mac
    - Verify `SyncEditors` writes to config so TUI also sees editors
    - Run tests, commit

- Open questions:
  - UNCONFIRMED: Does bolica have npm/node for building the Svelte frontend? Needed for `wails build` or manual frontend build + `go build -tags production`

- Working set:
  - `cmd/gui/app.go` — main fix (lookPathWithBrewPATH helper + 3 callsites)
  - `pkg/git/git.go` — existing `Environ()` / `ensureHomebrewPATH()` (unchanged, reused)
  - `.claude/context/feature-radar.md` — W6 shipped
  - `.claude/skills/wish/SKILL.md` — added `list` subcommand
  - Mac host: `luis@bolica`, Go at `/opt/homebrew/bin/go`
  - Source already rsynced to mac: NO — interrupted before transfer
  - CLI binary deployed to mac: `~/gitbox` (has the fix)
  - GUI binary: NOT YET BUILT for mac
