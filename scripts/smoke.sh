#!/usr/bin/env bash
#
# smoke.sh — Run a non-interactive GitboxApp smoke test on one or all platforms.
#
# Runs `GitboxApp --version`, which prints the build version and exits
# without opening a window, so it works over plain SSH. The local host uses
# the local `wails build` output (cmd/gui/build/bin/); remotes use the copy
# ./scripts/ship.sh staged (/tmp/GitboxApp[.app] or ~/GitboxApp.exe). On
# macOS the binary inside the .app bundle runs.
#
# Usage:
#   ./scripts/smoke.sh              # all configured platforms (default)
#   ./scripts/smoke.sh mac-arm      # macOS Apple Silicon only
#   ./scripts/smoke.sh mac-intel    # macOS Intel only
#   ./scripts/smoke.sh linux        # Linux only
#   ./scripts/smoke.sh win          # Windows only

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

header "Smoke tests"

targets="$(available_targets "${1:-}")"
if [[ -z "$targets" ]]; then
    die "no platforms available"
fi

total_pass=0
total_fail=0

for platform in $targets; do
    label="$(platform_label "$platform")"
    exe="$(gui_exe "$platform")"

    printf '  %-18s ' "$label"
    # Expected output: "GitboxApp <version> (<sha>)".
    if output="$(run_on "$platform" "$exe" --version 2>&1)" && [[ "$output" == GitboxApp* ]]; then
        printf '%bok%b    %s\n' "$G" "$N" "$(echo "$output" | head -1)"
        total_pass=$((total_pass + 1))
    else
        printf '%bFAIL%b\n' "$R" "$N"
        total_fail=$((total_fail + 1))
        info "$exe"
        # Show first line of error for debugging
        first_line="$(echo "$output" | head -1)"
        if [[ -n "$first_line" ]]; then
            info "$first_line"
        fi
    fi
done

echo ""

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

if [[ $total_fail -eq 0 ]]; then
    ok "all $total_pass checks passed"
else
    fail "$total_fail failed, $total_pass passed"
    exit 1
fi
