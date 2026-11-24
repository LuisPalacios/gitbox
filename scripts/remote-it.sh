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

# The daemon tests of #247 wait minutes for the daemon's own fetch runs,
# beyond go test's default of 10 minutes for the whole battery.
echo ">> go test -race -tags sshd_integration ./pkg/remote/"
GITBOX_IT_SERVER="$out/gitbox-server" \
GITBOX_IT_FAKEFORGE="$out/fakeforge" \
GITBOX_IT_VERSION="$VERSION" \
GITBOX_IT_COMMIT="$COMMIT" \
GITBOX_IT_IMAGE=gitbox-sshd \
    go test -race -count=1 -v -timeout 30m -tags sshd_integration -run "${GITBOX_IT_RUN:-.}" ./pkg/remote/
