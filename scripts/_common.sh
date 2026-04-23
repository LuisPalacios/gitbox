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
    # macOS splits into mac-arm (Apple Silicon) and mac-intel (x86_64). The
    # kernel name is "Darwin" in both cases — we key off uname -m for arch.
    _detect_mac_arch() {
        case "$(uname -m 2>/dev/null)" in
            arm64) echo "mac-arm" ;;
            x86_64) echo "mac-intel" ;;
            *) echo "mac-arm" ;;
        esac
    }
    # Windows splits the same way now that Windows-on-ARM is a first-class
    # dev target — Git Bash / MSYS2 report `aarch64` on ARM64, `x86_64` on
    # AMD64 (both native and under the WOW64 layer, which we treat as amd64
    # since the toolchain is amd64 in that case).
    _detect_win_arch() {
        case "$(uname -m 2>/dev/null)" in
            aarch64|arm64) echo "win-arm" ;;
            *)             echo "win-intel" ;;
        esac
    }
    case "${OSTYPE:-}" in
        msys*|mingw*|cygwin*) _detect_win_arch ;;
        darwin*)              _detect_mac_arch ;;
        linux*)               echo "linux" ;;
        *)
            case "$(uname -s 2>/dev/null)" in
                MINGW*|MSYS*) _detect_win_arch ;;
                Darwin)       _detect_mac_arch ;;
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

# Platform tokens:
#   win-intel  → windows/amd64
#   win-arm    → windows/arm64  (Windows on ARM, e.g. Parallels/VMware Fusion on Apple Silicon)
#   mac-arm    → darwin/arm64   (Apple Silicon)
#   mac-intel  → darwin/amd64   (Intel Mac)
#   linux      → linux/amd64
#   win        → alias for win-intel (back-compat)
#   mac        → alias for mac-arm   (back-compat)

# Returns 0 if the platform is any Windows variant (win, win-intel, win-arm).
is_win_platform() {
    case "$1" in
        win|win-intel|win-arm) return 0 ;;
        *)                     return 1 ;;
    esac
}

platform_goos() {
    case "$1" in
        win|win-intel|win-arm)   echo "windows" ;;
        mac|mac-arm|mac-intel)   echo "darwin" ;;
        linux)                   echo "linux" ;;
        *)                       die "unknown platform: $1" ;;
    esac
}

platform_goarch() {
    case "$1" in
        win|win-intel) echo "amd64" ;;
        win-arm)       echo "arm64" ;;
        mac|mac-arm)   echo "arm64" ;;
        mac-intel)     echo "amd64" ;;
        linux)         echo "amd64" ;;
        *)             die "unknown platform: $1" ;;
    esac
}

platform_bin() {
    if is_win_platform "$1"; then
        echo "gitbox.exe"
    else
        echo "gitbox"
    fi
}

platform_label() {
    case "$1" in
        win|win-intel) echo "Windows (amd64)" ;;
        win-arm)       echo "Windows (arm64)" ;;
        mac|mac-arm)   echo "macOS (arm64)" ;;
        mac-intel)     echo "macOS (Intel)" ;;
        linux)         echo "Linux" ;;
    esac
}

# Cross-compile output path
build_artifact() {
    case "$1" in
        win|win-intel) echo "$BUILD_DIR/gitbox-windows-amd64.exe" ;;
        win-arm)       echo "$BUILD_DIR/gitbox-windows-arm64.exe" ;;
        mac|mac-arm)   echo "$BUILD_DIR/gitbox-darwin-arm64" ;;
        mac-intel)     echo "$BUILD_DIR/gitbox-darwin-amd64" ;;
        linux)         echo "$BUILD_DIR/gitbox-linux-amd64" ;;
    esac
}

# Remote binary path (platform-aware)
# Windows: use home dir because SCP and Git Bash disagree on /tmp mapping.
#          Same path for amd64 and arm64 — the host runs whichever was shipped.
# Unix:    /tmp is consistent across SCP and shell.
remote_bin_for() {
    if is_win_platform "$1"; then
        echo "~/gitbox.exe"
    else
        echo "/tmp/gitbox"
    fi
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

# SSH host env var name for a platform.
# `win` is a back-compat alias for `win-intel` — both map to the canonical
# SSH_WIN_INTEL_HOST. Legacy SSH_WIN_HOST is honored as a fallback inside
# ssh_host_for(), not here, so the var-name interface stays single-valued.
_ssh_var() {
    case "$1" in
        win|win-intel) echo "SSH_WIN_INTEL_HOST" ;;
        win-arm)       echo "SSH_WIN_ARM_HOST" ;;
        mac|mac-arm)   echo "SSH_MAC_ARM_HOST" ;;
        mac-intel)     echo "SSH_MAC_INTEL_HOST" ;;
        linux)         echo "SSH_LINUX_HOST" ;;
    esac
}

# Get the SSH host for a platform (empty if local or not configured).
# Back-compat: if SSH_WIN_INTEL_HOST is unset but the legacy SSH_WIN_HOST is
# set, use the legacy value so existing .env files keep working without
# forcing a rename.
ssh_host_for() {
    local platform="$1"
    if [[ "$platform" == "$LOCAL_OS" ]]; then
        echo ""
        return
    fi
    local var_name value
    var_name="$(_ssh_var "$platform")"
    value="${!var_name:-}"
    if [[ -z "$value" && "$var_name" == "SSH_WIN_INTEL_HOST" ]]; then
        value="${SSH_WIN_HOST:-}"
    fi
    echo "$value"
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
        if is_win_platform "$platform"; then
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

ALL_PLATFORMS="win-intel win-arm mac-arm mac-intel linux"

# Resolve user target into platform list.
# Back-compat aliases:
#   win → win-intel  (historical single-Windows default, amd64)
#   mac → mac-arm    (historical single-Mac default, Apple Silicon)
resolve_targets() {
    local target="${1:-all}"
    case "$target" in
        all)                                              echo "$ALL_PLATFORMS" ;;
        mac)                                              echo "mac-arm" ;;
        win)                                              echo "win-intel" ;;
        win-intel|win-arm|mac-arm|mac-intel|linux)        echo "$target" ;;
        *)                                                die "invalid target: $target (use win-intel, win-arm, mac-arm, mac-intel, linux, or all)" ;;
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
        echo "  win-intel   Windows amd64 (Intel/AMD x64)"
        echo "  win-arm     Windows arm64 (Windows-on-ARM)"
        echo "  win         alias for win-intel"
        echo "  mac-arm     macOS Apple Silicon (arm64)"
        echo "  mac-intel   macOS Intel (amd64)"
        echo "  mac         alias for mac-arm"
        echo "  linux       Linux"
        echo "  all         All configured platforms"
        echo "  (empty)     Local machine ($LOCAL_OS)"
        exit 0
    fi
}
