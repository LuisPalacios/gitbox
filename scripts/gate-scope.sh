#!/usr/bin/env bash
#
# gate-scope.sh — Decide whether a diff can affect the remote-it lane (#403).
#
# The remote-it lane (scripts/remote-it.sh) is the slowest lane of the local
# gate. It builds a server and a fake forge, then runs the sshd integration
# tests of pkg/remote. A diff that changes none of the files those builds and
# tests read cannot change its result.
#
# Usage:
#   scripts/gate-scope.sh [BASE]    # BASE defaults to origin/main; HEAD is the head
#   scripts/gate-scope.sh --files   # read the changed paths from stdin
#
# Prints "needed: <path>" and exits 1 when the lane must run, or prints
# "unaffected" and exits 0 when it can be skipped. Any doubt (a failed
# git or go call) counts as needed.
#
# A path needs the lane when it is one of:
#   - under pkg/remote/, test/sshd/ or test/fakeforge/
#   - go.mod, go.sum, or one of the scripts that build or run the lane
#   - a non-test source or embedded file of any Go package that the lane's
#     builds or tests compile (go list -deps of the server build, the fake
#     forge and the sshd_integration test build of pkg/remote)

set -euo pipefail

cd "$(dirname "$0")/.."

needed() { echo "needed: $1"; exit 1; }

static_match() {
    case "$1" in
        pkg/remote/*|test/sshd/*|test/fakeforge/*) return 0 ;;
        go.mod|go.sum) return 0 ;;
        scripts/remote-it.sh|scripts/gate.sh|scripts/gate-lane.sh|scripts/gate-scope.sh) return 0 ;;
        scripts/stamp.sh|scripts/_common.sh) return 0 ;;
    esac
    return 1
}

# Repo-relative files of every package the lane compiles, one per line.
lane_sources() {
    local root
    root="$(pwd)"
    # The lane runs on linux/amd64, so list that platform's files.
    export GOOS=linux GOARCH=amd64 CGO_ENABLED=0
    {
        go list -deps -tags gitboxserver,e2e_sentinel \
            -f '{{.Dir}}{{"\t"}}{{join .GoFiles " "}} {{join .CgoFiles " "}} {{join .EmbedFiles " "}}' ./cmd/gui
        go list -deps -tags sshd_integration \
            -f '{{.Dir}}{{"\t"}}{{join .GoFiles " "}} {{join .EmbedFiles " "}}' ./pkg/remote ./test/fakeforge
    } | awk -F'\t' -v root="$root/" '
        index($1, root) == 1 {
            dir = substr($1, length(root) + 1)
            n = split($2, f, " ")
            for (i = 1; i <= n; i++) print dir "/" f[i]
        }' | sort -u
}

if [[ "${1:-}" == "--files" ]]; then
    changed="$(cat)"
else
    base="${1:-origin/main}"
    changed="$(git -c core.quotepath=false diff --name-only "$base"...HEAD --)" ||
        needed "git diff against $base failed"
fi

[[ -n "$changed" ]] || { echo "unaffected"; exit 0; }

sources=""
while IFS= read -r path; do
    [[ -n "$path" ]] || continue
    if static_match "$path"; then needed "$path"; fi
    case "$path" in
        *.go|*.s|*.h|*.c|*/embed/*|*/gen/*|*.json|*.txt|*.tmpl) ;;
        *) continue ;;
    esac
    case "$path" in *_test.go) continue ;; esac
    if [[ -z "$sources" ]]; then
        sources="$(lane_sources)" || needed "go list failed"
        [[ -n "$sources" ]] || needed "go list returned no packages"
    fi
    if grep -qxF -- "$path" <<<"$sources"; then needed "$path"; fi
done <<<"$changed"

echo "unaffected"
