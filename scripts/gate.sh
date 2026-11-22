#!/usr/bin/env bash
#
# gate.sh — Verify one exact commit on macOS, Linux and Windows in parallel.
#
# The local gate replaces the slow GitHub PR checks. It tests the committed
# HEAD, never the working tree: it refuses to run with uncommitted changes
# to tracked files. It bundles HEAD with its tags, sends the bundle and
# scripts/gate-lane.sh to each lane's host (this machine or an SSH host from
# .env), and runs every lane at once in a clean clone. See docs/local-gate.md.
#
# Lanes and hosts:
#   mac    macOS arm64   local, or SSH_MAC_ARM_HOST
#   linux  Linux amd64   local, or SSH_LINUX_HOST
#   win    Windows amd64 local, or SSH_WIN_INTEL_HOST / SSH_WIN_HOST
#   remote-it  the sshd integration tests of remote mode, on the linux
#              lane's host, which needs Docker (scripts/remote-it.sh)
#
# When every lane passes, gate.sh records a marker for the SHA in the git
# common dir (shared by all worktrees). .githooks/pre-push refuses to update
# main without it. Skipping a lane records a "partial" marker, which the hook
# accepts only with GATE_ALLOW_PARTIAL=1. A failed lane removes the commit's
# marker; a passing partial re-run keeps an earlier full pass.
#
# Usage:
#   ./scripts/gate.sh                  # every lane
#   ./scripts/gate.sh --only linux     # one lane (repeatable); records "partial"
#   ./scripts/gate.sh --skip win       # all but one lane (repeatable); "partial"
#   ./scripts/gate.sh --report         # post the HEAD marker as a "local-gate"
#                                      # commit status (the commit must be pushed)
#   ./scripts/gate.sh -h               # help
#
# Logs: ${TMPDIR:-/tmp}/gitbox-gate/<sha>/<lane>.log

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

ALL_LANES="mac linux win remote-it"

usage() {
    sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
    exit "${1:-0}"
}

is_lane() {
    case "$1" in
        mac|linux|win|remote-it) return 0 ;;
        *)                       return 1 ;;
    esac
}

only=() skip=() report=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        -h|--help) usage 0 ;;
        --only|--skip)
            if [[ $# -lt 2 ]] || ! is_lane "$2"; then
                die "$1 needs a lane: mac, linux, win or remote-it"
            fi
            if [[ "$1" == "--only" ]]; then only+=("$2"); else skip+=("$2"); fi
            shift 2 ;;
        --report) report=1; shift ;;
        *) die "unknown argument: $1 (see --help)" ;;
    esac
done

cd "$REPO_ROOT" || exit 1
sha="$(git rev-parse HEAD)"
marker_dir="$(git rev-parse --path-format=absolute --git-common-dir)/gitbox-gate"
marker="$marker_dir/$sha"

# ---------------------------------------------------------------------------
# --report: publish an existing marker as a commit status
# ---------------------------------------------------------------------------

if [[ $report -eq 1 ]]; then
    [[ -f "$marker" ]] || die "no gate marker for HEAD $sha; run ./scripts/gate.sh first"
    status="$(sed -n 's/^status=//p' "$marker")"
    lanes="$(sed -n 's/^lanes=//p' "$marker")"
    origin="$(git config --get remote.origin.url)"
    slug="$(printf '%s\n' "$origin" | sed -E 's|\.git$||; s|.*[:/]([^/]+/[^/]+)$|\1|')"
    owner="${slug%%/*}"
    gh auth switch --hostname github.com --user "$owner" >/dev/null 2>&1 ||
        die "cannot select the gh account '$owner' for $slug"
    if [[ "$status" == "pass" ]]; then state=success; else state=pending; fi
    gh api -X POST "repos/$slug/statuses/$sha" \
        -f state="$state" -f context=local-gate \
        -f description="$status: $lanes" >/dev/null ||
        die "could not post the status (is $sha pushed to $slug?)"
    ok "local-gate $state on $slug@${sha:0:7} ($status: $lanes)"
    exit 0
fi

# ---------------------------------------------------------------------------
# Lane selection
# ---------------------------------------------------------------------------

lanes=()
if [[ ${#only[@]} -gt 0 ]]; then
    lanes=("${only[@]}")
else
    for l in $ALL_LANES; do
        [[ " ${skip[*]} " == *" $l "* ]] || lanes+=("$l")
    done
fi
[[ ${#lanes[@]} -gt 0 ]] || die "no lanes left to run"

skipped=()
for l in $ALL_LANES; do
    [[ " ${lanes[*]} " == *" $l "* ]] || skipped+=("$l")
done

lane_platform() {
    case "$1" in
        mac)   echo "mac-arm" ;;
        linux|remote-it) echo "linux" ;;
        win)   echo "win-intel" ;;
    esac
}

# ---------------------------------------------------------------------------
# Preconditions: the gate tests HEAD, so tracked changes would go untested
# ---------------------------------------------------------------------------

if ! git diff --quiet HEAD --; then
    die "uncommitted changes to tracked files; commit or stash them (the gate tests HEAD $sha)"
fi

out="${TMPDIR:-/tmp}"
out="${out%/}/gitbox-gate/$sha"
mkdir -p "$out"
bundle="$out/gate.bundle"
git bundle create -q "$bundle" HEAD --tags 2>/dev/null || die "git bundle failed"

# ---------------------------------------------------------------------------
# One lane: ship bundle + lane script, run it, record status and duration
# ---------------------------------------------------------------------------

# Remote paths are relative to the SSH home directory, which works for scp
# and for Git Bash on Windows alike.
# shellcheck disable=SC2029
run_lane() {
    local lane="$1"
    local p host in_dir="${2}" rc
    p="$(lane_platform "$lane")"
    host="$(ssh_host_for "$p")"
    # Per lane: linux and remote-it share a host, and each lane deletes
    # its bundle once it has fetched it.
    local name_bundle="gate-$sha-$lane.bundle" name_script="gate-lane-$sha-$lane.sh"

    if [[ "$p" == "$LOCAL_OS" ]]; then
        echo "host: local ($(uname -sm))"
        mkdir -p "$HOME/$in_dir"
        cp "$bundle" "$HOME/$in_dir/$name_bundle"
        cp "$REPO_ROOT/scripts/gate-lane.sh" "$HOME/$in_dir/$name_script"
        (cd "$HOME" && bash "$in_dir/$name_script" "$lane" "$sha" "$in_dir/$name_bundle")
        return
    fi

    if [[ -z "$host" ]]; then
        echo "no SSH host configured for $(platform_label "$p") in .env"
        return 1
    fi
    echo "host: $host"
    if ! ssh -o BatchMode=yes -o ConnectTimeout=10 "$host" true 2>&1; then
        echo "host unreachable: $host"
        return 1
    fi
    if ! { ssh "$host" "mkdir -p $in_dir" &&
        scp -q "$bundle" "$host:$in_dir/$name_bundle" &&
        scp -q "$REPO_ROOT/scripts/gate-lane.sh" "$host:$in_dir/$name_script"; }; then
        echo "could not copy the bundle to $host"
        return 1
    fi
    run_on "$p" "bash $in_dir/$name_script $lane $sha $in_dir/$name_bundle"
    rc=$?
    return $rc
}

lane_job() {
    local lane="$1" t0=$SECONDS
    local log="$out/$lane.log"
    : > "$log"
    if run_lane "$lane" ".cache/gitbox-gate/in" >> "$log" 2>&1; then
        echo "pass" > "$out/$lane.status"
    else
        echo "fail" > "$out/$lane.status"
    fi
    echo $((SECONDS - t0)) > "$out/$lane.secs"
}

fmt_secs() { printf '%dm%02ds' $(($1 / 60)) $(($1 % 60)); }

header "Gate ${sha:0:12}: ${lanes[*]}"
[[ ${#skipped[@]} -eq 0 ]] || warn "skipping: ${skipped[*]} (records a partial marker)"

t_start=$SECONDS
pids=()
for lane in "${lanes[@]}"; do
    rm -f "$out/$lane.status" "$out/$lane.secs"
    info "$lane → $(platform_label "$(lane_platform "$lane")") [log: $out/$lane.log]"
    lane_job "$lane" &
    pids+=($!)
done
for pid in "${pids[@]}"; do
    wait "$pid" || true
done
rm -f "$bundle"

# ---------------------------------------------------------------------------
# Summary and marker
# ---------------------------------------------------------------------------

header "Summary ($(fmt_secs $((SECONDS - t_start))) wall)"
fails=0
results=()
for lane in "${lanes[@]}"; do
    status="$(cat "$out/$lane.status" 2>/dev/null || echo fail)"
    secs="$(cat "$out/$lane.secs" 2>/dev/null || echo 0)"
    results+=("$lane:$status($(fmt_secs "$secs"))")
    if [[ "$status" == "pass" ]]; then
        ok "$(printf '%-6s %s' "$lane" "$(fmt_secs "$secs")")"
    else
        fail "$(printf '%-6s %s — %s' "$lane" "$(fmt_secs "$secs")" "$out/$lane.log")"
        tail -15 "$out/$lane.log" | sed 's/^/        /'
        fails=$((fails + 1))
    fi
done

if [[ $fails -gt 0 ]]; then
    rm -f "$marker"
    die "$fails lane(s) failed; no marker recorded for ${sha:0:12}"
fi

overall=pass
[[ ${#skipped[@]} -eq 0 ]] || overall=partial
# A passing subset re-run never downgrades a full pass of the same commit.
if [[ "$overall" == "partial" && -f "$marker" ]] &&
    [[ "$(sed -n 's/^status=//p' "$marker")" == "pass" ]]; then
    ok "marker: kept the earlier full pass for ${sha:0:12}"
    exit 0
fi
mkdir -p "$marker_dir"
{
    echo "sha=$sha"
    echo "status=$overall"
    echo "lanes=${results[*]}"
    echo "skipped=${skipped[*]}"
    echo "date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} > "$marker"
ok "marker: $overall for ${sha:0:12}"
