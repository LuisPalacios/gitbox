#!/usr/bin/env bash
#
# remote-it.sh — sshd integration tests of the remote-mode bootstrap (#239).
#
# Builds the gitbox-sshd test image (test/sshd) and a linux/amd64
# gitbox-server with the e2e_sentinel tag and the fake forge of the token
# canary (test/fakeforge, #242), then runs the sshd_integration
# tests of pkg/remote against fresh containers on an internal Docker network.
# It needs Docker and a Linux host: Docker Desktop on macOS cannot reach
# container IPs from the host. The local gate runs it in its remote-it lane.
#
# Usage:
#   scripts/remote-it.sh                 # every test
#   GITBOX_IT_RUN=TestFish scripts/remote-it.sh   # a -run pattern
#
# It changes nothing outside a temporary folder, the gitbox-sshd image and
# the containers and network the tests start and remove.

set -euo pipefail

cd "$(dirname "$0")/.."

command -v docker >/dev/null 2>&1 || { echo "remote-it: docker is required" >&2; exit 1; }
[[ "$(uname -s)" == "Linux" ]] || { echo "remote-it: runs on Linux only (container IPs must be reachable)" >&2; exit 1; }

echo ">> image gitbox-sshd"
docker build -q -t gitbox-sshd test/sshd >/dev/null

eval "$(bash scripts/stamp.sh)"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

echo ">> server linux/amd64 (gitboxserver,e2e_sentinel) $VERSION"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
    -tags gitboxserver,e2e_sentinel -ldflags "$LDFLAGS" \
    -o "$out/gitbox-server" ./cmd/gui

echo ">> fakeforge linux/amd64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$out/fakeforge" ./test/fakeforge

# The tests run in GITBOX_SHARDS test processes at the same time (default 3,
# 1 runs them in one process). Each process creates its own Docker network,
# named after its process id, so they never collide. GITBOX_IT_RUN narrows the
# battery before it is split.
shards="${GITBOX_SHARDS:-3}"
[[ "$shards" =~ ^[1-9][0-9]*$ ]] || { echo "remote-it: GITBOX_SHARDS must be a positive number" >&2; exit 1; }

echo ">> go test -c -race -tags sshd_integration ./pkg/remote/"
go test -c -race -tags sshd_integration -o "$out/remote.test" ./pkg/remote/

# The test binary cannot list its tests without the harness's environment, so
# read the names from the source files of the tagged build instead.
mapfile -t files < <(go list -tags sshd_integration -f '{{range .TestGoFiles}}{{.}}{{"\n"}}{{end}}{{range .XTestGoFiles}}{{.}}{{"\n"}}{{end}}' ./pkg/remote/)
mapfile -t tests < <(cd pkg/remote && sed -n 's/^func \(Test[A-Za-z0-9_]*\)(t \*testing\.T).*/\1/p' "${files[@]}" | { grep -E -- "${GITBOX_IT_RUN:-.}" || true; } | sort -u)
[[ ${#tests[@]} -gt 0 ]] || { echo "remote-it: no test matches '${GITBOX_IT_RUN:-.}'" >&2; exit 1; }
[[ $shards -le ${#tests[@]} ]] || shards=${#tests[@]}

# Round-robin over the listed order, then check the shards add up to the list.
declare -a pattern
for i in "${!tests[@]}"; do
    n=$((i % shards))
    pattern[n]+="${pattern[n]:+|}${tests[i]}"
done
covered=0
for n in "${!pattern[@]}"; do
    covered=$((covered + $(tr -cd '|' <<<"${pattern[n]}" | wc -c) + 1))
done
[[ $covered -eq ${#tests[@]} ]] || { echo "remote-it: shards cover $covered of ${#tests[@]} tests" >&2; exit 1; }

# The daemon tests of #247 wait minutes for the daemon's own fetch runs,
# beyond go test's default of 10 minutes for the whole battery.
echo ">> ${#tests[@]} tests in $shards shard(s)"
pids=()
for n in "${!pattern[@]}"; do
    (
        cd pkg/remote
        GITBOX_IT_SERVER="$out/gitbox-server" \
        GITBOX_IT_FAKEFORGE="$out/fakeforge" \
        GITBOX_IT_VERSION="$VERSION" \
        GITBOX_IT_COMMIT="$COMMIT" \
        GITBOX_IT_IMAGE=gitbox-sshd \
            "$out/remote.test" -test.count=1 -test.v -test.timeout 30m -test.run "^(${pattern[n]})\$"
    ) >"$out/shard-$n.log" 2>&1 &
    pids+=($!)
done

rc=0
for n in "${!pids[@]}"; do
    if wait "${pids[n]}"; then status=ok; else status=FAILED; rc=1; fi
    echo ">> shard $((n + 1))/$shards: $status"
    cat "$out/shard-$n.log"
done
exit $rc
