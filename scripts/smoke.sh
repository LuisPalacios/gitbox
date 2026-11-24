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
#   ./scripts/smoke.sh remote-mac   # remote mode against the macOS host
#
# remote-mac runs the remote smoke (cmd/gui/remote_smoke_test.go) on this
# machine against SSH_REMOTE_MAC_HOST from .env: connect, which installs the
# server in a scratch server_dir, the log, the doctor, disconnect and
# uninstall. The default run includes it when that variable is set.

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

# remote_mac_smoke builds the embedded servers and runs the remote smoke
# against the macOS host.
remote_mac_smoke() {
    local host="${SSH_REMOTE_MAC_HOST:-}" pkg=github.com/LuisPalacios/gitbox/cmd/gui
    [[ -n "$host" ]] || { fail "SSH_REMOTE_MAC_HOST is not set in .env"; return 1; }
    eval "$(bash "$REPO_ROOT/scripts/stamp.sh")"
    "$REPO_ROOT/scripts/build-server.sh" >/dev/null || { fail "scripts/build-server.sh failed"; return 1; }
    # The server_dir is expanded on the host, so the ~ stays literal here.
    # shellcheck disable=SC2088
    (cd "$REPO_ROOT" && GITBOX_SMOKE_ALIAS="$host" GITBOX_SMOKE_OS=darwin \
        GITBOX_SMOKE_SERVER_DIR="~/.cache/gitbox-smoke-$$" \
        go test -count=1 -tags serverbin,remote_smoke -run TestRemoteSmoke \
        -ldflags "-X $pkg.version=$VERSION -X $pkg.commit=$COMMIT" ./cmd/gui/)
}

header "Smoke tests"

if [[ "${1:-}" == "remote-mac" ]]; then
    if remote_mac_smoke; then ok "remote mode against $SSH_REMOTE_MAC_HOST passed"; else exit 1; fi
    exit 0
fi

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

if [[ -z "${1:-}" || "${1:-}" == "all" ]] && [[ -n "${SSH_REMOTE_MAC_HOST:-}" ]]; then
    printf '  %-18s ' "remote mode (mac)"
    if output="$(remote_mac_smoke 2>&1)"; then
        printf '%bok%b\n' "$G" "$N"
        total_pass=$((total_pass + 1))
    else
        printf '%bFAIL%b\n' "$R" "$N"
        total_fail=$((total_fail + 1))
        echo "$output" | tail -n 20
    fi
fi

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
