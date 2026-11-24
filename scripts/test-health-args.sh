#!/usr/bin/env bash
# I check that health.sh refuses an invocation that would run no check (#257).
# Every case exits during argument validation, so no checker runs.
# HEALTH_SH points the fixture at another copy, for example the script before a fix.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
health="${HEALTH_SH:-scripts/health.sh}"
unset HEALTH_FUZZ

fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
failures=0

fail() { printf 'FAIL  %s\n' "$*"; failures=$((failures + 1)); }

# expect CODE NEEDLE ARGS... — health.sh exits CODE, its output contains
# NEEDLE, and it never reaches the check phase.
expect() {
    local code="$1" needle="$2"; shift 2
    local rc=0
    bash "$health" "$@" >"$fixture/out" 2>&1 || rc=$?
    [[ $rc -eq $code ]] || fail "health.sh $* exited $rc, want $code"
    grep -qF -- "$needle" "$fixture/out" || fail "health.sh $*: output lacks '$needle'"
    if grep -qF 'Health check' "$fixture/out"; then
        fail "health.sh $* started running checks"
    fi
}

expect 2 'unknown check: golang' golang
expect 2 'unknown option: --only=go' --only=go
expect 2 'unknown option: -x' -x
expect 2 'unknown check: bogus' go bogus
expect 2 'unknown check: bogus' -v bogus
expect 2 'Valid checks: gofmt' golang
expect 2 'fuzz requires HEALTH_FUZZ=1' fuzz
expect 0 'Usage:' -h
expect 0 'Usage:' --help

# The names health.sh accepts, the names it dispatches and the names its help
# lists must agree. A fixture line proves the extraction pattern matches.
want_names() { sed -nE 's/^(\[\[ .* \]\] && )?want ([a-z-]+) .*/\2/p'; }
[[ "$(printf 'want gofmt go       && check gofmt run_gofmt\n' | want_names)" == gofmt ]] \
    || fail 'want-line pattern no longer matches a dispatch line'
[[ "$(printf "[[ \${HEALTH_FUZZ:-0} == 1 ]] && want fuzz go && check fuzz run_fuzz\n" | want_names)" == fuzz ]] \
    || fail 'want-line pattern no longer matches the fuzz dispatch line'

dispatched="$(want_names <"$health" | sort -u)"
accepted="$(sed -n '/^CHECKS="/,/"$/p' "$health" | tr -d '"' | sed 's/^CHECKS=//' | tr ' ' '\n' | sed '/^$/d' | sort -u)"
[[ $(wc -l <<<"$accepted") -ge 20 ]] || fail "read only $(wc -l <<<"$accepted") names from CHECKS"
[[ "$dispatched" == "$accepted" ]] \
    || fail "CHECKS and the want lines differ: $(diff <(echo "$accepted") <(echo "$dispatched") | tr '\n' ' ')"

bash "$health" -h >"$fixture/help"
while read -r name; do
    grep -qw -- "$name" "$fixture/help" || fail "help does not list check $name"
done <<<"$accepted"

[[ $failures -eq 0 ]] || exit 1
echo "health.sh argument checks passed"
