#!/usr/bin/env bash
#
# gate-lane.sh — One lane of the local gate (scripts/gate.sh).
#
# gate.sh copies this script and a git bundle of the commit under test to
# each host, then runs it there. The lane clones the bundled commit into a
# clean directory under ~/.cache/gitbox-gate and runs that platform's checks,
# so it never touches a working checkout. It never runs from the repo itself.
#
# Usage:
#   bash gate-lane.sh <mac|linux|win|remote-it> <sha> <bundle>
#
# Lanes:
#   mac    frontend, scripts/health.sh (HEALTH_FUZZ=1), coordination suites,
#          server binaries, Wails build, artifact check
#   linux  frontend, go vet, go test -short, bootstrap fixtures,
#          server binaries, Wails build (webkit2_41), artifact check
#   win    frontend, coordination suites, go test -short, bootstrap fixtures,
#          server binaries, Wails build, artifact check
#   remote-it  the sshd integration tests of remote mode (scripts/remote-it.sh);
#          needs Docker on a Linux host, and builds no GUI
#
# Every lane but remote-it builds GitboxApp the way a release does: with the six
# gitbox-server binaries inside (scripts/build-server.sh, -tags serverbin),
# checked for identity and size by scripts/check-gui-artifact.sh.
#
# Environment:
#   GATE_KEEP=1   keep the lane's clone after a pass (a failure always keeps it)

set -euo pipefail

if [[ $# -ne 3 ]]; then
    echo "usage: bash gate-lane.sh <mac|linux|win|remote-it> <sha> <bundle>" >&2
    exit 2
fi
lane="$1" sha="$2" bundle="$3"
case "$lane" in
    mac|linux|win|remote-it) ;;
    *) echo "unknown lane: $lane" >&2; exit 2 ;;
esac

# Same Wails CLI version as the CI workflows.
WAILS="github.com/wailsapp/wails/v2/cmd/wails@v2.16.0"

# A caller running inside a git hook exports these, and every git call the
# test suite makes would then target the caller's repository.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_COMMON_DIR GIT_OBJECT_DIRECTORY

# Non-interactive SSH sessions skip the login profile. Same prefixes as
# scripts/ship.sh: Homebrew, /usr/local, the Go tarball and per-user bins.
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$HOME/go/bin:$HOME/.local/bin:$PATH"

if [[ "$lane" == "win" ]]; then
    # The Windows CI runner sets these with `git config --global`. Scoping
    # them to this process gives the tests the same Git behaviour without
    # changing the host user's global config.
    export GIT_CONFIG_COUNT=2
    export GIT_CONFIG_KEY_0=core.autocrlf GIT_CONFIG_VALUE_0=false
    export GIT_CONFIG_KEY_1=core.longpaths GIT_CONFIG_VALUE_1=true
fi

cache="$HOME/.cache/gitbox-gate"
repo="$cache/repo.git"
work="$cache/work-$lane-${sha:0:12}"
bundle="$(cd "$(dirname "$bundle")" && pwd)/$(basename "$bundle")"

# Each step runs in a subshell outside any condition: bash ignores set -e
# inside a function called from an `if`, which would let a failed command
# in the middle of a step pass unnoticed.
step() {
    local name="$1"; shift
    local t0=$SECONDS rc
    printf '\n>> %s\n' "$name"
    set +e
    (set -e; "$@")
    rc=$?
    set -e
    if [[ $rc -eq 0 ]]; then
        printf '<< %s ok (%ss)\n' "$name" $((SECONDS - t0))
    else
        printf '!! %s FAILED after %ss (clone kept at %s)\n' "$name" $((SECONDS - t0)) "$work"
        exit 1
    fi
}

checkout() {
    mkdir -p "$cache"
    # Clones from a failed or abandoned run older than a day.
    find "$cache" -maxdepth 1 -name 'work-*' -mtime +1 -exec rm -rf {} + 2>/dev/null || true
    [[ -d "$repo" ]] || git init -q --bare "$repo"
    # fetch would start a detached auto gc that repacks the cache while the
    # clone below hardlinks its objects ("hardlink different from source").
    git -C "$repo" config gc.auto 0
    git -C "$repo" config maintenance.auto false
    # A per-SHA ref, so concurrent gates from parallel worktrees never
    # fight over the same ref lock.
    git -C "$repo" fetch -q --force "$bundle" \
        "+HEAD:refs/heads/gate-$sha" "+refs/tags/*:refs/tags/*"
    rm -rf "$work"
    git clone -q --no-checkout "$repo" "$work"
    git -C "$work" checkout -q --detach "$sha"
    git -C "$repo" update-ref -d "refs/heads/gate-$sha"
    rm -f "$bundle"
    [[ "$(git -C "$work" rev-parse HEAD)" == "$sha" ]]
}

frontend() {
    (cd cmd/gui/frontend && npm ci --no-audit --no-fund && npm run build)
}

health() {
    HEALTH_FUZZ=1 ./scripts/health.sh
}

coordination() {
    uv run ai/scripts/test_coordinate_issues.py &&
        uv run ai/scripts/test_coordinate_roadmap.py
}

vet() { go vet ./...; }

unit_tests() { go test -short ./...; }

bootstrap() { bash scripts/test-bootstrap.sh; }

# The six gitbox-server binaries GitboxApp embeds, and the test that reads
# each one back against its pin.
server_build() {
    bash scripts/build-server.sh
    go test -short -tags serverbin ./cmd/gui/serverbin/
}

gui_build() {
    mkdir -p cmd/gui/build/windows
    cp assets/appicon.png cmd/gui/build/appicon.png
    cp assets/icon.ico cmd/gui/build/windows/icon.ico
    eval "$(bash scripts/stamp.sh)"
    local tags=serverbin
    case "$lane" in
        linux) tags=webkit2_41,serverbin ;;
        mac)
            # Go's runtime targets macOS 13; same minimum as ci.yml.
            export CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0
            ;;
    esac
    # -s: the frontend step already built frontend/dist.
    (cd cmd/gui && go run "$WAILS" build -s -tags "$tags" -ldflags "$LDFLAGS")
}

# The clean clone's `wails build` regenerated the bindings, so any difference
# from the committed ones is drift. Wails rewrites trailing whitespace and
# file modes on every run; those are not drift.
bindings() {
    git -c core.fileMode=false diff --exit-code --ignore-space-at-eol -- cmd/gui/frontend/wailsjs/go
}

# The built executable reports the stamp it was built from, carries the six
# server binaries of that same build, and is within its size ceiling.
artifact() {
    local exe
    case "$lane" in
        mac)   exe=cmd/gui/build/bin/GitboxApp.app/Contents/MacOS/GitboxApp ;;
        linux) exe=cmd/gui/build/bin/GitboxApp ;;
        win)   exe=cmd/gui/build/bin/GitboxApp.exe ;;
    esac
    eval "$(bash scripts/stamp.sh)"
    VERSION="$VERSION" COMMIT="$COMMIT" bash scripts/check-gui-artifact.sh \
        "$exe" "$(go env GOOS)/$(go env GOARCH)"
}

remote_it() { bash scripts/remote-it.sh; }

printf 'lane %s on %s, commit %s\n' "$lane" "$(uname -sm)" "$sha"
step "checkout" checkout
cd "$work"
if [[ "$lane" == "remote-it" ]]; then
    step "remote-it" remote_it
    cd "$cache"
    [[ "${GATE_KEEP:-}" == "1" ]] || rm -rf "$work"
    printf '\nlane %s passed\n' "$lane"
    exit 0
fi
# One identity for the whole lane, taken before any step changes the tree.
GITBOX_VERSION="$(git describe --tags --always)-gate"
export GITBOX_VERSION GITBOX_SERVER_TARGETS=all
step "frontend" frontend
case "$lane" in
    mac)
        step "health" health
        step "coordination" coordination
        ;;
    linux)
        step "go vet" vet
        step "go test -short" unit_tests
        step "bootstrap" bootstrap
        ;;
    win)
        step "coordination" coordination
        step "go test -short" unit_tests
        step "bootstrap" bootstrap
        ;;
esac
step "server binaries" server_build
step "gui build" gui_build
step "bindings" bindings
step "artifact" artifact

cd "$cache"
[[ "${GATE_KEEP:-}" == "1" ]] || rm -rf "$work"
printf '\nlane %s passed\n' "$lane"
