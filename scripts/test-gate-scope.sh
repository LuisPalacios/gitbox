#!/usr/bin/env bash
# I check that gate-scope.sh (#403) classifies changed paths for the remote-it lane:
# remote-mode files and Go sources the lane compiles are "needed", unrelated files are
# "unaffected", every static path still exists, and no input means unaffected.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

failures=0
fail() { printf 'FAIL  %s\n' "$*"; failures=$((failures + 1)); }

# expect CODE PATH... — gate-scope.sh --files exits CODE for these paths.
expect() {
    local code="$1" rc=0; shift
    printf '%s\n' "$@" | bash scripts/gate-scope.sh --files >/dev/null || rc=$?
    [[ $rc -eq $code ]] || fail "paths '$*' exited $rc, want $code"
}

expect 1 pkg/remote/session.go
expect 1 test/sshd/Dockerfile
expect 1 scripts/remote-it.sh
expect 1 go.mod
expect 1 pkg/git/browser.go
expect 1 docs/a.md pkg/remote/x_test.go
expect 0 docs/gui-guide.md
expect 0 cmd/gui/frontend/src/main.ts
expect 0 pkg/git/hidewindow_windows.go
expect 0 pkg/reconcile/anything_test.go
printf '' | bash scripts/gate-scope.sh --files >/dev/null || fail "empty input must be unaffected"

for p in scripts/remote-it.sh scripts/gate.sh scripts/gate-lane.sh scripts/gate-scope.sh \
    scripts/stamp.sh scripts/_common.sh go.mod go.sum pkg/remote test/sshd test/fakeforge; do
    [[ -e "$p" ]] || fail "gate-scope.sh lists $p, which no longer exists"
done

[[ $failures -eq 0 ]] || exit 1
echo "gate-scope: all cases pass"
