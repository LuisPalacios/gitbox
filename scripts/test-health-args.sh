#!/usr/bin/env bash
# I check that health.sh refuses an invocation that would run no check (#257),
# and that a failed go test shows its failure ahead of other output (#346).
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

# A failed go test puts the failure and its messages first, so the
# truncated summary names the test, and drops only the ok lines (#346).
eval "$(sed -n '/^failures_first() {$/,/^}$/p' "$health")"
declare -F failures_first >/dev/null || fail 'failures_first not found in health.sh'
{
    for _ in $(seq 20); do echo 'warning: config backup failed: all 10 backup names already exist'; done
    printf 'ok  \tgithub.com/example/other\t0.1s\n'
    printf -- '--- FAIL: TestExample (0.01s)\n'
    printf '    example_test.go:12: want 1, got 2\n'
    printf 'FAIL\n'
    printf 'FAIL\tgithub.com/example/pkg\t1.0s\n'
} >"$fixture/test.out"
failures_first "$fixture/test.out" >"$fixture/test.sorted"
[[ "$(head -n 1 "$fixture/test.sorted")" == '--- FAIL: TestExample (0.01s)' ]] \
    || fail "failures_first does not start with the failing test: $(head -n 1 "$fixture/test.sorted")"
grep -n 'example_test.go:12' "$fixture/test.sorted" | grep -q '^2:' \
    || fail 'failures_first does not put the failure message second'
[[ $(wc -l <"$fixture/test.sorted") -eq 24 ]] \
    || fail "failures_first kept $(wc -l <"$fixture/test.sorted") lines, want the 24 that are not ok lines"
grep -q '^ok ' "$fixture/test.sorted" && fail 'failures_first kept an ok line'

[[ $failures -eq 0 ]] || exit 1
echo "health.sh argument checks passed"
