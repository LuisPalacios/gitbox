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

`GitboxApp --version` prints `GitboxApp <version> (<sha>)` and exits without opening a window, so scripts and CI use it as a smoke check.

## Build commands

```bash
# Build-time version stamp. Without -ldflags the GUI launched from Finder
# shows "dev-none" (runtime git fallback can't find .git when the app's CWD
# is outside the repo). Matches the values CI and scripts/ship.sh inject.
LDFLAGS="-X main.version=$(git describe --tags --always)-dev -X main.commit=$(git rev-parse --short HEAD)"

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
