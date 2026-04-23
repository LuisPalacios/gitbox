#!/usr/bin/env bash
#
# test-commands.sh — Print commands to launch gitbox in test-mode on any platform.
#
# Test-mode uses test-gitbox.json with an isolated temp directory.
# Commands are printed (not executed) because the TUI needs a real terminal.
#
# Usage:
#   ./scripts/test-commands.sh              # all configured platforms (default)
#   ./scripts/test-commands.sh mac-arm      # macOS Apple Silicon only
#   ./scripts/test-commands.sh mac-intel    # macOS Intel only
#   ./scripts/test-commands.sh win-intel    # Windows amd64 only
#   ./scripts/test-commands.sh win-arm      # Windows arm64 only
#   ./scripts/test-commands.sh linux        # Linux only

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

header "Test mode"

targets="$(available_targets "${1:-}")"
if [[ -z "$targets" ]]; then
    die "no platforms available"
fi

if [[ ! -f "$FIXTURE" ]]; then
    warn "test-gitbox.json not found — test-mode will fail on targets"
    warn "run: cp json/test-gitbox.json.example test-gitbox.json"
    echo ""
fi

echo "Run these commands in your terminal (interactive, needs a real TTY):"
echo ""

for platform in $targets; do
    label="$(platform_label "$platform")"
    host="$(ssh_host_for "$platform")"
    bin="$(binary_path "$platform")"

    if [[ -z "$host" ]]; then
        # Local
        printf '  %b%s%b:  %s --test-mode\n' "$B" "$label" "$N" "$bin"
    else
        # Remote — check fixture exists
        if ! ssh "$host" "test -f ~/test-gitbox.json" 2>/dev/null; then
            warn "$label — test-gitbox.json not found on remote. Run: ./scripts/deploy.sh"
        fi
        if is_win_platform "$platform"; then
            # Windows: TUI needs a real interactive shell, ssh -t "cmd" exits immediately
            printf '  %b%s%b:  ssh %s  →  %s --test-mode\n' "$B" "$label" "$N" "$host" "$bin"
        else
            printf '  %b%s%b:  ssh -t %s "%s --test-mode"\n' "$B" "$label" "$N" "$host" "$bin"
        fi
    fi
done

echo ""
info "pass additional CLI args after --test-mode (e.g., --test-mode account list --json)"
