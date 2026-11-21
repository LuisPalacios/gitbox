#!/usr/bin/env bash
#
# check-gui-artifact.sh — Check a GitboxApp executable that was just built.
#
# Every path that builds the GUI with the embedded gitbox-server runs this
# on its result: the gate lanes, ship.sh, release-macos.sh and CI.
#
#   size      the executable is within its ceiling in scripts/size-ceilings.txt
#   identity  `--version` and `--list-server-targets` report exactly the
#             version and commit of the stamp, and the embedded targets are
#             the ones build-server.sh was asked for. The app itself reads
#             every embedded binary against its pin while it lists them.
#
# Usage:
#   check-gui-artifact.sh [--no-size | --size-only] <executable> <os>/<arch>
#
#   --no-size     identity only: development builds embed fewer targets
#   --size-only   size only: for an executable this machine cannot run
#
# Environment (not needed with --size-only):
#   VERSION, COMMIT          from `eval "$(scripts/stamp.sh)"`
#   GITBOX_SERVER_TARGETS    as it was given to build-server.sh

set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"

fail() { echo "check-gui-artifact: $*" >&2; exit 1; }

mode="all"
case "${1:-}" in
    --no-size)   mode="identity"; shift ;;
    --size-only) mode="size"; shift ;;
esac
[[ $# -eq 2 ]] || fail "usage: check-gui-artifact.sh [--no-size | --size-only] <executable> <os>/<arch>"
exe="$1" target="$2"
[[ -f "$exe" ]] || fail "no executable at $exe"

if [[ "$mode" != "identity" ]]; then
    size="$(wc -c < "$exe" | tr -d '[:space:]')"
    line="$(awk -v t="$target" '$1 == t { print $2, $3 }' "$here/size-ceilings.txt")"
    [[ -n "$line" ]] || fail "no size ceiling for $target in scripts/size-ceilings.txt"
    read -r baseline ceiling <<<"$line"
    echo "size: $target is $size bytes (baseline $baseline, ceiling $ceiling)"
    (( size <= ceiling )) || fail "$exe is $size bytes, over the ceiling of $ceiling for $target"
fi

if [[ "$mode" != "size" ]]; then
    [[ -n "${VERSION:-}" && -n "${COMMIT:-}" ]] || fail "VERSION and COMMIT are not set: run eval \"\$(scripts/stamp.sh)\" first"

    # "GitboxApp <version> (<the first seven characters of the commit>)"
    want="GitboxApp $VERSION (${COMMIT:0:7})"
    got="$("$exe" --version | tr -d '\r')" || fail "$exe --version failed"
    echo "$got"
    [[ "$got" == "$want" ]] || fail "--version printed '$got', want '$want'"

    list="$("$exe" --list-server-targets | tr -d '\r')" || fail "$exe --list-server-targets failed"
    echo "$list"
    want="commit $COMMIT version $VERSION"
    got="$(head -n 1 <<<"$list")"
    [[ "$got" == "$want" ]] || fail "the embedded server is '$got', want '$want'"

    want="$(bash "$here/build-server.sh" --targets | sort | tr '\n' ' ')"
    got="$(tail -n +2 <<<"$list" | cut -f 1 | sort | tr '\n' ' ')"
    [[ "$got" == "$want" ]] || fail "embedded targets are '$got', want '$want'"
fi

echo "check-gui-artifact: $target ok"
