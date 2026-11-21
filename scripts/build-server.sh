#!/usr/bin/env bash
#
# build-server.sh — Build the gitbox-server binaries that GitboxApp embeds.
#
# Remote mode installs gitbox-server on the other host from inside GitboxApp,
# with no download. This script cross-compiles the server for each target,
# and cmd/gui/serverbin/gen compresses the binaries and writes the Go file
# that pins their SHA-256. A GUI built afterwards with `-tags serverbin`
# carries them:
#
#   eval "$(scripts/stamp.sh)"
#   scripts/build-server.sh
#   (cd cmd/gui && wails build -tags serverbin -ldflags "$LDFLAGS")
#
# Usage:
#   scripts/build-server.sh             # build and generate
#   scripts/build-server.sh --targets   # print the resolved targets and exit
#
# Environment:
#   GITBOX_SERVER_TARGETS   "all" (the six below) or a comma-separated list of
#                           <os>/<arch>. Default: linux/amd64,linux/arm64,
#                           enough for development against Linux hosts.
#   GITBOX_VERSION          see stamp.sh
#
# Output, ignored by git: cmd/gui/serverbin/gitbox-server-<os>-<arch>[.exe].gz
# and cmd/gui/serverbin/zz_generated.go.

set -euo pipefail

cd "$(dirname "$0")/.."

ALL_TARGETS="linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64"

targets="${GITBOX_SERVER_TARGETS:-linux/amd64,linux/arm64}"
[[ "$targets" == "all" ]] && targets="$ALL_TARGETS"
IFS=',' read -r -a list <<<"$targets"
[[ ${#list[@]} -gt 0 ]] || { echo "build-server.sh: no targets" >&2; exit 2; }
for t in "${list[@]}"; do
    case ",$ALL_TARGETS," in
        *",$t,"*) ;;
        *) echo "build-server.sh: unknown target '$t' (known: $ALL_TARGETS)" >&2; exit 2 ;;
    esac
done

if [[ "${1:-}" == "--targets" ]]; then
    printf '%s\n' "${list[@]}"
    exit 0
fi
[[ $# -eq 0 ]] || { echo "usage: build-server.sh [--targets]" >&2; exit 2; }

eval "$(scripts/stamp.sh)"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

for t in "${list[@]}"; do
    os="${t%/*}" arch="${t#*/}" ext=""
    [[ "$os" == "windows" ]] && ext=".exe"
    echo "build-server: $t"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -tags gitboxserver -trimpath \
        -ldflags "-s -w $LDFLAGS" -o "$work/gitbox-server-$os-$arch$ext" ./cmd/gui
done

go run ./cmd/gui/serverbin/gen -in "$work" -out cmd/gui/serverbin \
    -version "$VERSION" -commit "$COMMIT" -targets "$targets"
