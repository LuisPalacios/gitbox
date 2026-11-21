#!/usr/bin/env bash
#
# stamp.sh — The build identity that GitboxApp and gitbox-server share.
#
# Remote mode only connects a GitboxApp to a gitbox-server of the same build,
# so every path that builds either one takes its version and commit from here.
# It prints shell assignments for VERSION, COMMIT and LDFLAGS:
#
#   eval "$(scripts/stamp.sh)"
#   (cd cmd/gui && wails build -ldflags "$LDFLAGS")
#
# COMMIT is always the full SHA of HEAD.
# VERSION is $GITBOX_VERSION when that is set: CI, the release script and the
# gate set it. Otherwise it is `git describe --tags --always --dirty` plus
# "-dev", a development build.
#
# It reads the repository and nothing else: no .env, no network.

set -euo pipefail

cd "$(dirname "$0")/.."

COMMIT="$(git rev-parse HEAD)"
if [[ -n "${GITBOX_VERSION:-}" ]]; then
    VERSION="$GITBOX_VERSION"
else
    VERSION="$(git describe --tags --always --dirty)-dev"
fi

# Both values end up inside -ldflags and inside generated Go source.
[[ "$COMMIT" =~ ^[0-9a-f]{40}$ ]] || { echo "stamp.sh: unexpected commit: $COMMIT" >&2; exit 1; }
[[ "$VERSION" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] || { echo "stamp.sh: unexpected version: $VERSION" >&2; exit 1; }

printf 'VERSION=%q\n' "$VERSION"
printf 'COMMIT=%q\n' "$COMMIT"
printf 'LDFLAGS=%q\n' "-X main.version=$VERSION -X main.commit=$COMMIT"
