#!/usr/bin/env bash
#
# run-commands.sh — Print commands to launch gitbox in production mode on any platform.
#
# Uses the real config (~/.config/gitbox/gitbox.json) on each target machine.
# Commands are printed (not executed) because the TUI needs a real terminal.
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

echo "Run these commands in your terminal (interactive, needs a real TTY):"
echo ""

for platform in $targets; do
    label="$(platform_label "$platform")"
    host="$(ssh_host_for "$platform")"
    bin="$(binary_path "$platform")"

    if [[ -z "$host" ]]; then
        printf '  %b%s%b:  %s\n' "$B" "$label" "$N" "$bin"
    else
        if is_win_platform "$platform"; then
            # Windows: TUI needs a real interactive shell, ssh -t "cmd" exits immediately
            printf '  %b%s%b:  ssh %s  →  %s\n' "$B" "$label" "$N" "$host" "$bin"
        else
            printf '  %b%s%b:  ssh -t %s "%s"\n' "$B" "$label" "$N" "$host" "$bin"
        fi
    fi
done

echo ""
info "pass additional CLI args (e.g., account list --json, status, clone)"
