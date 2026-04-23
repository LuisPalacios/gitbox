#!/usr/bin/env bash
#
# send-my-production-config.sh — Copy local production config to a remote machine.
#
# DANGEROUS: overwrites the remote's gitbox.json with your local copy.
# Shows a diff and requires confirmation before proceeding.
#
# Usage:
#   ./scripts/send-my-production-config.sh mac-arm    # send to macOS Apple Silicon
#   ./scripts/send-my-production-config.sh mac-intel  # send to macOS Intel
#   ./scripts/send-my-production-config.sh linux      # send to Linux
#   ./scripts/send-my-production-config.sh win-intel  # send to Windows amd64
#   ./scripts/send-my-production-config.sh win-arm    # send to Windows arm64

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

target="${1:-}"
if [[ -z "$target" ]] || [[ "$target" == "-h" ]] || [[ "$target" == "--help" ]]; then
    echo "Usage: $(basename "$0") <target>"
    echo ""
    echo "Targets: win-intel, win-arm, mac-arm, mac-intel, linux (one at a time, no 'all')"
    echo ""
    echo "Copies your local ~/.config/gitbox/gitbox.json to the remote machine."
    echo "Shows a diff and asks for confirmation before overwriting."
    exit 0
fi

if [[ "$target" == "all" ]]; then
    die "send-my-production-config does not support 'all' — too dangerous. Specify one target."
fi

if [[ "$target" == "$LOCAL_OS" ]]; then
    die "target is your local machine — nothing to sync"
fi

host="$(ssh_host_for "$target")"
if [[ -z "$host" ]]; then
    die "no SSH host configured for $(platform_label "$target")"
fi

# ---------------------------------------------------------------------------
# Locate local config
# ---------------------------------------------------------------------------

LOCAL_CONFIG="$HOME/.config/gitbox/gitbox.json"
if [[ ! -f "$LOCAL_CONFIG" ]]; then
    die "local config not found: $LOCAL_CONFIG"
fi

label="$(platform_label "$target")"

header "Sync config → $label"

# ---------------------------------------------------------------------------
# Fetch remote config and diff
# ---------------------------------------------------------------------------

remote_config="$(ssh "$host" "cat ~/.config/gitbox/gitbox.json 2>/dev/null" || true)"

if [[ -n "$remote_config" ]]; then
    echo "Diff (local → remote):"
    echo ""
    diff --color=auto <(echo "$remote_config") "$LOCAL_CONFIG" || true
    echo ""
else
    warn "no config exists on $label yet — will create it"
    echo ""
fi

# ---------------------------------------------------------------------------
# Confirm
# ---------------------------------------------------------------------------

printf '%b⚠️  This will overwrite gitbox.json on %s (%s).%b\n' "$Y" "$label" "$host" "$N"
printf 'Continue? [y/N] '
read -r answer
if [[ "$answer" != "y" && "$answer" != "Y" ]]; then
    echo "Aborted."
    exit 0
fi

# ---------------------------------------------------------------------------
# Sync
# ---------------------------------------------------------------------------

ssh "$host" "mkdir -p ~/.config/gitbox" 2>/dev/null
scp "$LOCAL_CONFIG" "${host}:~/.config/gitbox/gitbox.json"
ok "config synced to $label"
