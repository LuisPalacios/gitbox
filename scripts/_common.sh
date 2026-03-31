#!/usr/bin/env bash
#
# _common.sh — Shared helpers for gitbox cross-platform scripts.
#
# Sourced by other scripts, never executed directly.
# Provides OS detection, .env loading, SSH helpers, and color output.

# Fail fast
set -euo pipefail

# ---------------------------------------------------------------------------
# Colors (same palette as test-setup-credentials.sh)
# ---------------------------------------------------------------------------

G='\033[0;32m'  R='\033[0;31m'  Y='\033[0;33m'
C='\033[0;36m'  D='\033[0;90m'  B='\033[1m'  N='\033[0m'

header() { printf '\n%b━━ %s ━━%b\n\n' "$C" "$*" "$N"; }
ok()     { printf '  %bok%b    %s\n' "$G" "$N" "$*"; }
fail()   { printf '  %bFAIL%b  %s\n' "$R" "$N" "$*"; }
warn()   { printf '  %bwarn%b  %s\n' "$Y" "$N" "$*"; }
info()   { printf '  %binfo%b  %s\n' "$D" "$N" "$*"; }
die()    { printf '%berror:%b %s\n' "$R" "$N" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="$REPO_ROOT/build"
FIXTURE="$REPO_ROOT/test-gitbox.json"
FIXTURE_EXAMPLE="$REPO_ROOT/json/test-gitbox.json.example"

# ---------------------------------------------------------------------------
# OS detection
# ---------------------------------------------------------------------------

detect_local_os() {
    case "${OSTYPE:-}" in
        msys*|mingw*|cygwin*) echo "win" ;;
        darwin*)              echo "mac" ;;
        linux*)               echo "linux" ;;
        *)
            case "$(uname -s 2>/dev/null)" in
                MINGW*|MSYS*) echo "win" ;;
                Darwin)       echo "mac" ;;
                Linux)        echo "linux" ;;
                *)            die "unsupported OS: ${OSTYPE:-$(uname -s)}" ;;
            esac ;;
    esac
}

LOCAL_OS="$(detect_local_os)"

# ---------------------------------------------------------------------------
# .env loading
# ---------------------------------------------------------------------------

load_env() {
    local env_file="$REPO_ROOT/.env"
    if [[ ! -f "$env_file" ]]; then
        die ".env not found.\n  Copy docs/.env.example to .env and fill in your SSH hosts.\n  See docs/multiplatform.md for setup instructions."
    fi
    set -a
    # shellcheck disable=SC1090
    source "$env_file"
    set +a
}

load_env

# ---------------------------------------------------------------------------
# Platform mapping
# ---------------------------------------------------------------------------

platform_goos() {
    case "$1" in
        win)   echo "windows" ;;
        mac)   echo "darwin" ;;
        linux) echo "linux" ;;
        *)     die "unknown platform: $1" ;;
    esac
}

platform_goarch() {
    case "$1" in
        win)   echo "amd64" ;;
        mac)   echo "arm64" ;;
        linux) echo "amd64" ;;
        *)     die "unknown platform: $1" ;;
    esac
}

platform_bin() {
    case "$1" in
        win) echo "gitbox.exe" ;;
        *)   echo "gitbox" ;;
    esac
}

platform_label() {
    case "$1" in
        win)   echo "Windows" ;;
        mac)   echo "macOS" ;;
        linux) echo "Linux" ;;
    esac
}

# Cross-compile output path
build_artifact() {
    case "$1" in
        win)   echo "$BUILD_DIR/gitbox.exe" ;;
        mac)   echo "$BUILD_DIR/gitbox-darwin-arm64" ;;
        linux) echo "$BUILD_DIR/gitbox-linux-amd64" ;;
    esac
}

# Remote binary path (platform-aware)
# Windows: use home dir because SCP and Git Bash disagree on /tmp mapping
# Unix: /tmp is consistent across SCP and shell
remote_bin_for() {
    case "$1" in
        win) echo "~/gitbox.exe" ;;
        *)   echo "/tmp/gitbox" ;;
    esac
}

# Where the binary lives on a given platform
binary_path() {
    local platform="$1"
    if [[ "$platform" == "$LOCAL_OS" ]]; then
        build_artifact "$platform"
    else
        remote_bin_for "$platform"
    fi
}

# ---------------------------------------------------------------------------
# SSH helpers
# ---------------------------------------------------------------------------

# Scoop PATH fix for non-interactive Windows SSH sessions.
# Scoop shims fail when Git Bash is invoked by sshd (NTFS junction issue).
# This prepends versioned app directories so real binaries are found first.
_win_scoop_path_prefix() {
    cat <<'SCOOPFIX'
for _sa in "$HOME/scoop/apps"/*/; do _sv="$(command ls "$_sa" 2>/dev/null | grep -E '^[0-9]' | sort -V | tail -1)"; [ -n "$_sv" ] && PATH="$_sa$_sv:$PATH"; done; unset _sa _sv;
SCOOPFIX
}

# SSH host env var name for a platform
_ssh_var() {
    case "$1" in
        win)   echo "SSH_WIN_HOST" ;;
        mac)   echo "SSH_MAC_HOST" ;;
        linux) echo "SSH_LINUX_HOST" ;;
    esac
}

# Get the SSH host for a platform (empty if local or not configured)
ssh_host_for() {
    local platform="$1"
    if [[ "$platform" == "$LOCAL_OS" ]]; then
        echo ""
        return
    fi
    local var_name
    var_name="$(_ssh_var "$platform")"
    echo "${!var_name:-}"
}

# Check if a platform is available (local or has SSH host)
is_available() {
    local platform="$1"
    [[ "$platform" == "$LOCAL_OS" ]] || [[ -n "$(ssh_host_for "$platform")" ]]
}

# Run a command on a target platform
run_on() {
    local platform="$1"; shift
    local host
    host="$(ssh_host_for "$platform")"
    if [[ -z "$host" ]]; then
        "$@"
    else
        local cmd="$*"
        # Fix Scoop shim PATH for non-interactive Windows SSH
        if [[ "$platform" == "win" ]]; then
            cmd="$(_win_scoop_path_prefix) $cmd"
        fi
        # shellcheck disable=SC2029
        ssh "$host" "$cmd"
    fi
}

# Run with TTY allocation (for interactive commands)
run_on_tty() {
    local platform="$1"; shift
    local host
    host="$(ssh_host_for "$platform")"
    if [[ -z "$host" ]]; then
        "$@"
    else
        # shellcheck disable=SC2029
        ssh -t "$host" "$*"
    fi
}

# Copy a file to a target platform
copy_to() {
    local platform="$1" local_path="$2" remote_path="$3"
    local host
    host="$(ssh_host_for "$platform")"
    if [[ -z "$host" ]]; then
        if [[ "$(cd "$(dirname "$local_path")" && pwd)/$(basename "$local_path")" != \
              "$(cd "$(dirname "$remote_path")" 2>/dev/null && pwd)/$(basename "$remote_path")" ]]; then
            cp "$local_path" "$remote_path"
        fi
    else
        scp "$local_path" "${host}:${remote_path}"
    fi
}

# ---------------------------------------------------------------------------
# Target resolution
# ---------------------------------------------------------------------------

ALL_PLATFORMS="win mac linux"

# Resolve user target into platform list
resolve_targets() {
    local target="${1:-all}"
    case "$target" in
        all)           echo "$ALL_PLATFORMS" ;;
        win|mac|linux) echo "$target" ;;
        *)             die "invalid target: $target (use win, mac, linux, or all)" ;;
    esac
}

# Filter to available targets, warn on skipped
available_targets() {
    local targets
    targets="$(resolve_targets "${1:-}")"
    local available=""
    for t in $targets; do
        if is_available "$t"; then
            available="$available $t"
        else
            warn "$(platform_label "$t") — no SSH host configured, skipping"
        fi
    done
    echo "$available"
}

# ---------------------------------------------------------------------------
# Usage helper
# ---------------------------------------------------------------------------

require_target_arg() {
    local script_name="$1"
    local target="${2:-}"
    if [[ -z "$target" ]] || [[ "$target" == "-h" ]] || [[ "$target" == "--help" ]]; then
        echo "Usage: $script_name [target]"
        echo ""
        echo "Targets:"
        echo "  win      Windows"
        echo "  mac      macOS"
        echo "  linux    Linux"
        echo "  all      All configured platforms"
        echo "  (empty)  Local machine ($LOCAL_OS)"
        exit 0
    fi
}
