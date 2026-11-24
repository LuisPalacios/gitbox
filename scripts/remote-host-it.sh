#!/usr/bin/env bash
#
# remote-host-it.sh — remote-mode integration tests against a real host (#245).
#
# The sshd lane (remote-it.sh) proves remote mode against Linux containers.
# This script runs the tests that need no container of their own against a
# real host, such as a Mac with Remote Login: the bootstrap, the token
# canary and the remote terminal. It asks the host for its platform, builds
# gitbox-server with the e2e_sentinel tag and the fake forge for it, and
# runs the remote_host tests of pkg/remote.
#
# The alias must reach a disposable account with key-only ssh, set up as
# docs/testing.md describes: the tests write and delete files in its home,
# and refuse to start without its ~/.gitbox-it-host marker.
#
# Usage:
#   scripts/remote-host-it.sh [alias]            # default: SSH_REMOTE_MAC_HOST from .env
#   GITBOX_IT_RUN=TestTokenCanary scripts/remote-host-it.sh   # a -run pattern
#
# Environment:
#   GITBOX_REMOTE_TEST_KNOWN_HOSTS   known_hosts file that holds the host key
#                                    (default: SSH_REMOTE_MAC_KNOWN_HOSTS from
#                                    .env, else the alias's UserKnownHostsFile)
#
# This side changes nothing outside a temporary folder.

set -euo pipefail

cd "$(dirname "$0")/.."

host="${1:-}"
known="${GITBOX_REMOTE_TEST_KNOWN_HOSTS:-}"
if [[ -z "$host" || -z "$known" ]]; then
    # shellcheck source=_common.sh
    source scripts/_common.sh
    host="${host:-${SSH_REMOTE_MAC_HOST:-}}"
    known="${known:-${SSH_REMOTE_MAC_KNOWN_HOSTS:-}}"
fi
[[ -n "$host" ]] || { echo "remote-host-it: give an ssh alias or set SSH_REMOTE_MAC_HOST in .env" >&2; exit 2; }

platform="$(ssh -o BatchMode=yes -o ConnectTimeout=10 "$host" 'uname -sm' </dev/null | tail -n 1)"
case "$platform" in
    "Darwin arm64")   target=darwin/arm64 ;;
    "Linux x86_64")   target=linux/amd64 ;;
    "Linux aarch64")  target=linux/arm64 ;;
    *) echo "remote-host-it: $host reports '$platform', which remote mode does not support" >&2; exit 1 ;;
esac
os="${target%/*}" arch="${target#*/}"

eval "$(bash scripts/stamp.sh)"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

echo ">> server $target (gitboxserver,e2e_sentinel) $VERSION"
CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -tags gitboxserver,e2e_sentinel -ldflags "$LDFLAGS" \
    -o "$out/gitbox-server" ./cmd/gui

echo ">> fakeforge $target"
CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -o "$out/fakeforge" ./test/fakeforge

echo ">> go test -race -tags remote_host ./pkg/remote/ against $host"
GITBOX_REMOTE_TEST_HOST="$host" \
GITBOX_REMOTE_TEST_KNOWN_HOSTS="$known" \
GITBOX_IT_TARGET="$target" \
GITBOX_IT_SERVER="$out/gitbox-server" \
GITBOX_IT_FAKEFORGE="$out/fakeforge" \
GITBOX_IT_VERSION="$VERSION" \
GITBOX_IT_COMMIT="$COMMIT" \
    go test -race -count=1 -v -tags remote_host -run "${GITBOX_IT_RUN:-.}" ./pkg/remote/
