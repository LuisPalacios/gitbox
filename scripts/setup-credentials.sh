#!/usr/bin/env bash
#
# setup-credentials.sh — Run test-setup-credentials.sh on any target platform.
#
# Copies the test fixture and credential setup script to the target,
# then runs it. For the local machine, runs directly.
#
# Usage:
#   ./scripts/setup-credentials.sh          # all configured platforms (default)
#   ./scripts/setup-credentials.sh mac      # macOS only

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

SETUP_SCRIPT="$REPO_ROOT/scripts/test-setup-credentials.sh"

if [[ ! -f "$FIXTURE" ]]; then
    die "test-gitbox.json not found.\n  Copy json/test-gitbox.json.example to test-gitbox.json and add your tokens.\n  See docs/testing.md for details."
fi

if [[ ! -f "$SETUP_SCRIPT" ]]; then
    die "scripts/test-setup-credentials.sh not found"
fi

header "Credential setup"

targets="$(available_targets "${1:-}")"
if [[ -z "$targets" ]]; then
    die "no platforms available"
fi

for platform in $targets; do
    label="$(platform_label "$platform")"
    host="$(ssh_host_for "$platform")"

    printf '%b%s%b\n' "$B" "$label" "$N"

    if [[ -z "$host" ]]; then
        # Local — run directly
        "$SETUP_SCRIPT"
    else
        # Remote — copy files and run
        # Windows: use ~/ because SCP and Git Bash disagree on /tmp
        if [[ "$platform" == "win" ]]; then
            remote_script="~/test-setup-credentials.sh"
        else
            remote_script="/tmp/test-setup-credentials.sh"
        fi

        printf '  copying test-gitbox.json... '
        scp "$FIXTURE" "${host}:~/test-gitbox.json" 2>/dev/null
        printf '%bok%b\n' "$G" "$N"

        printf '  copying test-setup-credentials.sh... '
        scp "$SETUP_SCRIPT" "${host}:${remote_script}" 2>/dev/null
        [[ "$platform" != "win" ]] && ssh "$host" "chmod +x $remote_script" 2>/dev/null
        printf '%bok%b\n' "$G" "$N"

        printf '  running credential setup...\n'
        cmd="$remote_script ~/test-gitbox.json"
        # Fix Scoop shim PATH for non-interactive Windows SSH
        [[ "$platform" == "win" ]] && cmd="$(_win_scoop_path_prefix) $cmd"
        # shellcheck disable=SC2029
        ssh "$host" "$cmd"
    fi
    echo ""
done
