#!/usr/bin/env bash
#
# run-commands.sh — Print commands to launch GitboxApp in production mode on any platform.
#
# Uses the real config (~/.config/gitbox/gitbox.json) on each target machine.
# The local host runs its `wails build` output; remotes run the copy
# ./scripts/ship.sh staged. Commands are printed (not executed) because the
# GUI needs the target's desktop session — run them in a terminal on that
# host.
#
# Usage:
#   ./scripts/run-commands.sh               # all configured platforms (default)
#   ./scripts/run-commands.sh mac-arm       # macOS Apple Silicon only
#   ./scripts/run-commands.sh mac-intel     # macOS Intel only
#   ./scripts/run-commands.sh win-intel     # Windows amd64 only
#   ./scripts/run-commands.sh win-arm       # Windows arm64 only
#   ./scripts/run-commands.sh linux         # Linux only

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

header "Production mode"

targets="$(available_targets "${1:-}")"
if [[ -z "$targets" ]]; then
    die "no platforms available"
fi

echo "Run these commands in a terminal inside each host's desktop session:"
echo ""

for platform in $targets; do
    label="$(platform_label "$platform")"
    host="$(ssh_host_for "$platform")"
    path="$(gui_path "$platform")"

    # The local path may contain spaces; remote paths start with ~ or /tmp
    # and stay unquoted so the remote shell expands ~.
    if [[ -z "$host" ]]; then
        path="\"$path\""
    fi
    # macOS launches the .app bundle through `open`; elsewhere the binary
    # runs directly.
    case "$platform" in
        mac|mac-arm|mac-intel) cmd="open $path" ;;
        *)                     cmd="$path" ;;
    esac

    if [[ -n "$host" ]]; then
        printf '  %b%s%b (%s):  %s\n' "$B" "$label" "$N" "$host" "$cmd"
    else
        printf '  %b%s%b:  %s\n' "$B" "$label" "$N" "$cmd"
    fi
done
