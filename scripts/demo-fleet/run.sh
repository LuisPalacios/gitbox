#!/usr/bin/env bash
#
# run.sh — Launch GitboxApp against the demo fleet.
#
# Starts mock.py (the fake provider APIs on 127.0.0.1:3001-3003), then runs
# GitboxApp with XDG_CONFIG_HOME / GIT_CONFIG_GLOBAL pointing at the demo
# state. Closing the app stops the mock. Build the fleet first with
# build.sh, and the GUI with `wails build` (see docs/developer-guide.md).
#
# Close any running GitboxApp first: its single-instance lock would focus
# that window instead of starting the demo.
#
# Usage:
#   ./scripts/demo-fleet/run.sh                  # local wails build output
#   ./scripts/demo-fleet/run.sh <path/to/GitboxApp>

set -euo pipefail
# shellcheck source=env.sh
source "$(dirname "$0")/env.sh"

app="${1:-}"
if [[ -z "$app" ]]; then
    case "$(uname -s)" in
        MINGW*|MSYS*|CYGWIN*) app="$REPO_ROOT/cmd/gui/build/bin/GitboxApp.exe" ;;
        Darwin)               app="$REPO_ROOT/cmd/gui/build/bin/GitboxApp.app/Contents/MacOS/GitboxApp" ;;
        *)                    app="$REPO_ROOT/cmd/gui/build/bin/GitboxApp" ;;
    esac
fi
[[ -x "$app" ]] || { echo "error: $app not found — run wails build first" >&2; exit 1; }
[[ -f "$DEMO_ROOT/config/gitbox/gitbox.json" ]] || { echo "error: no demo fleet — run build.sh first" >&2; exit 1; }

uv run "$KIT_DIR/mock.py" 2> "$DEMO_ROOT/mock.log" &
mock=$!
trap 'kill "$mock" 2>/dev/null || true' EXIT

for _ in $(seq 30); do
    curl -s -o /dev/null http://127.0.0.1:3001/api/v1/user && break
    sleep 0.5
done

echo "GitboxApp on the demo fleet (mock log: $DEMO_ROOT/mock.log)"
"$app"
