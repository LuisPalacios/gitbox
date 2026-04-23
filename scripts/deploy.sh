#!/usr/bin/env bash
#
# deploy.sh — Build and deploy gitbox binaries to all configured remotes.
#
# Cross-compiles for all 3 platforms, then SCPs binaries to remote hosts.
# Also copies test-gitbox.json to remotes if it exists.
#
# Idempotent — safe to run multiple times.
#
# Usage:
#   ./scripts/deploy.sh

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

header "Build + Deploy"

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

mkdir -p "$BUILD_DIR"

for platform in $ALL_PLATFORMS; do
    artifact="$(build_artifact "$platform")"
    goos="$(platform_goos "$platform")"
    goarch="$(platform_goarch "$platform")"
    label="$(platform_label "$platform")"

    printf '  building %s (%s/%s)... ' "$label" "$goos" "$goarch"
    if GOOS="$goos" GOARCH="$goarch" go build -o "$artifact" ./cmd/cli 2>&1; then
        printf '%bok%b\n' "$G" "$N"
    else
        printf '%bFAIL%b\n' "$R" "$N"
        die "build failed for $label"
    fi
done

# ---------------------------------------------------------------------------
# Deploy to remotes
# ---------------------------------------------------------------------------

header "Deploy"

deployed=0

for platform in $ALL_PLATFORMS; do
    if [[ "$platform" == "$LOCAL_OS" ]]; then
        ok "$(platform_label "$platform") — local, binary at $(build_artifact "$platform")"
        continue
    fi

    host="$(ssh_host_for "$platform")"
    if [[ -z "$host" ]]; then
        info "$(platform_label "$platform") — no SSH host configured, skipping"
        continue
    fi

    label="$(platform_label "$platform")"
    artifact="$(build_artifact "$platform")"
    remote_bin="$(remote_bin_for "$platform")"

    # Deploy binary
    printf '  deploying to %s (%s)... ' "$label" "$host"
    if scp "$artifact" "${host}:${remote_bin}" 2>/dev/null && \
       { is_win_platform "$platform" || ssh "$host" "chmod +x $remote_bin" 2>/dev/null; }; then
        printf '%bok%b\n' "$G" "$N"
        deployed=$((deployed + 1))
    else
        printf '%bFAIL%b\n' "$R" "$N"
        fail "$label — could not deploy binary"
        continue
    fi

    # Deploy test fixture if it exists
    if [[ -f "$FIXTURE" ]]; then
        printf '  copying test-gitbox.json to %s... ' "$label"
        if scp "$FIXTURE" "${host}:~/test-gitbox.json" 2>/dev/null; then
            printf '%bok%b\n' "$G" "$N"
        else
            printf '%bwarn%b\n' "$Y" "$N"
            warn "$label — could not copy test-gitbox.json"
        fi
    fi
done

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

header "Summary"

printf '  %-10s %s\n' "$(platform_label "$LOCAL_OS")" "$(build_artifact "$LOCAL_OS")"
for platform in $ALL_PLATFORMS; do
    [[ "$platform" == "$LOCAL_OS" ]] && continue
    host="$(ssh_host_for "$platform")"
    [[ -z "$host" ]] && continue
    printf '  %-10s %s:%s\n' "$(platform_label "$platform")" "$host" "$(remote_bin_for "$platform")"
done

echo ""
if [[ $deployed -gt 0 ]]; then
    ok "deployed to $deployed remote(s)"
else
    info "no remotes configured — local build only"
fi
