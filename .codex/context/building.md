---
read-when: Before building GitboxApp or running Go commands that require the embedded frontend.
---

# Building GitboxApp

## Binary naming

The GUI binary is `GitboxApp`. The v1 CLI/TUI binary was `gitbox` — a different name, so the two never collided on the case-insensitive NTFS filesystem when shipped side by side. The CLI/TUI is maintained on the `release/v1` branch and installed with `scripts/bootstrap.sh --cli-only` (latest 1.x).

| Platform | GUI             |
| -------- | --------------- |
| Windows  | `GitboxApp.exe` |
| macOS    | `GitboxApp.app` |
| Linux    | `GitboxApp`     |

`GitboxApp --version` prints `GitboxApp <version> (<sha>)` and exits without opening a window, so scripts and CI use it as a smoke check. `GitboxApp --list-server-targets` prints the `gitbox-server` binaries the build carries and exits with an error when it carries none, is not stamped, or carries those of another build.

## Build commands

```bash
# Build-time version stamp: sets VERSION, COMMIT (the full SHA) and LDFLAGS.
# Without -ldflags the GUI launched from Finder shows "dev-none" (runtime git
# fallback can't find .git when the app's CWD is outside the repo). Every
# build path (gate, ship.sh, release-macos.sh, CI) takes its stamp from here.
eval "$(scripts/stamp.sh)"

# cmd/gui embeds frontend/dist (go:embed), so `go build ./...`, `go vet ./...`
# and `go test ./...` need the frontend built once (wails build also does it).
(cd cmd/gui/frontend && npm ci && npm run build)

# Build GUI (requires Wails CLI)
# ALWAYS copy icons before building — they are not checked in under cmd/gui/build/
cp assets/appicon.png cmd/gui/build/appicon.png
mkdir -p cmd/gui/build/windows   # absent in a fresh clone or worktree
cp assets/icon.ico    cmd/gui/build/windows/icon.ico
(cd cmd/gui && wails build -ldflags "$LDFLAGS")   # Linux: add -tags webkit2_41
# macOS: export CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0
# first (Go's runtime minimum; cmd/gui/build/darwin/Info.plist declares the same 13.0)
# Output: cmd/gui/build/bin/GitboxApp[.exe] (GitboxApp.app on macOS)

# Smoke check
cmd/gui/build/bin/GitboxApp --version        # macOS: GitboxApp.app/Contents/MacOS/GitboxApp

# Run tests
go test ./pkg/...              # shared library (pkg/ops holds the lifecycle scenario)
go test ./cmd/gui/             # Wails-bound App methods
go test ./...                  # everything
```

## Embedding gitbox-server

Remote mode installs `gitbox-server` from inside GitboxApp, so a GUI that must run remote mode carries the server binaries, built from the same commit:

```bash
eval "$(scripts/stamp.sh)"
scripts/build-server.sh          # GITBOX_SERVER_TARGETS: "all" (six targets) or a list; default linux/amd64,linux/arm64,darwin/arm64
(cd cmd/gui && wails build -tags serverbin -ldflags "$LDFLAGS")   # Linux: -tags webkit2_41,serverbin
cmd/gui/build/bin/GitboxApp --list-server-targets
```

- `build-server.sh` writes `cmd/gui/serverbin/*.gz` and `zz_generated.go`. Git ignores both; never commit them.
- Without `-tags serverbin` the app has no server inside and needs none of those files. `go vet`, `go test` and `./scripts/health.sh` run without the tag; test the embedded build with `go test -tags serverbin ./cmd/gui/serverbin/` after `build-server.sh`.
- `wails build` rewrites whitespace in `cmd/gui/frontend/wailsjs`, so the next development stamp says `-dirty`. Restore it with `git checkout -- cmd/gui/frontend/wailsjs`.
- A new path that builds the GUI does the same four steps as the gate lanes, `ship.sh`, `release-macos.sh` and CI: `stamp.sh`, `build-server.sh`, `wails build -tags serverbin`, `scripts/check-gui-artifact.sh`.
- Raising a line of `scripts/size-ceilings.txt` is my decision, not a fix for a failing check.
