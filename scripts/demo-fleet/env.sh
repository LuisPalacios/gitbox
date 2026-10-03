#!/usr/bin/env bash
#
# env.sh — Shared paths for the demo fleet scripts (sourced, not run).
#
# DEMO_ROOT holds everything the demo creates: config/, fleet/, upstreams/,
# gitconfig and logs. It defaults to a short temp path because Windows
# MAX_PATH (260) breaks git object writes under deep folders. Paths are
# exported in native form so GitboxApp.exe and Python see the same thing.

KIT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$KIT_DIR/../.." && pwd)"

native() { # native <path> — Windows-style path under Git Bash, unchanged elsewhere
    if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi
}

if [[ -z "${DEMO_ROOT:-}" ]]; then
    DEMO_ROOT="$(native "${TEMP:-${TMPDIR:-/tmp}}")/gbdemo"
fi
export DEMO_ROOT

# Isolation: the app and every git call read only demo state.
export XDG_CONFIG_HOME="$DEMO_ROOT/config"
export GIT_CONFIG_GLOBAL="$DEMO_ROOT/gitconfig"
export GIT_CONFIG_NOSYSTEM=1
