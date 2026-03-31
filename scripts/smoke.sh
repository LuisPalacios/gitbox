#!/usr/bin/env bash
#
# smoke.sh — Run non-interactive smoke tests on one or all platforms.
#
# Runs gitbox version, help, and JSON output commands to verify the binary
# works on each target platform.
#
# Usage:
#   ./scripts/smoke.sh              # all configured platforms (default)
#   ./scripts/smoke.sh mac          # macOS only
#   ./scripts/smoke.sh linux        # Linux only

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

header "Smoke tests"

targets="$(available_targets "${1:-}")"
if [[ -z "$targets" ]]; then
    die "no platforms available"
fi

COMMANDS=(
    "version"
    "help"
    "global show --json"
    "account list --json"
    "status --json"
)

total_pass=0
total_fail=0

for platform in $targets; do
    label="$(platform_label "$platform")"
    bin="$(binary_path "$platform")"

    printf '%b%s%b\n' "$B" "$label" "$N"

    for cmd in "${COMMANDS[@]}"; do
        printf '  %-25s ' "$cmd"
        # shellcheck disable=SC2086
        if output="$(run_on "$platform" "$bin" $cmd 2>&1)"; then
            printf '%bok%b\n' "$G" "$N"
            total_pass=$((total_pass + 1))
        else
            printf '%bFAIL%b\n' "$R" "$N"
            total_fail=$((total_fail + 1))
            # Show first line of error for debugging
            first_line="$(echo "$output" | head -1)"
            if [[ -n "$first_line" ]]; then
                info "$first_line"
            fi
        fi
    done
    echo ""
done

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

if [[ $total_fail -eq 0 ]]; then
    ok "all $total_pass checks passed"
else
    fail "$total_fail failed, $total_pass passed"
    exit 1
fi
