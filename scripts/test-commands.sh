#!/usr/bin/env bash
#
# test-commands.sh — Print commands to launch GitboxApp in test mode on any platform.
#
# Test mode loads test-gitbox.json into an isolated temp directory. GitboxApp
# finds the fixture by walking up from its working directory: the repo root
# on this host, the home directory on remotes (./scripts/ship.sh copies it to
# ~/test-gitbox.json). Commands are printed (not executed) because the GUI
# needs the target's desktop session — run them in a terminal on that host.
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
    warn "test-gitbox.json not found — test mode will fail on targets"
    warn "run: cp json/test-gitbox.json.example test-gitbox.json"
    echo ""
fi

echo "Run these commands in a terminal inside each host's desktop session:"
echo ""

for platform in $targets; do
    label="$(platform_label "$platform")"
    host="$(ssh_host_for "$platform")"
    exe="$(gui_exe "$platform")"

    if [[ -z "$host" ]]; then
        # Local — run from the repo root, where test-gitbox.json lives.
        printf '  %b%s%b:  cd "%s" && "%s" --test-mode\n' "$B" "$label" "$N" "$REPO_ROOT" "$exe"
    else
        # Remote — check the fixture ship.sh copies to the home directory.
        if ! ssh "$host" "test -f ~/test-gitbox.json" 2>/dev/null; then
            warn "$label — test-gitbox.json not found on remote. Run: ./scripts/ship.sh"
        fi
        printf '  %b%s%b (%s):  cd ~ && %s --test-mode\n' "$B" "$label" "$N" "$host" "$exe"
    fi
done
