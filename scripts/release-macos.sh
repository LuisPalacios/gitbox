#!/usr/bin/env bash
#
# release-macos.sh — Build the macOS release assets locally and attach them
# to the draft release.
#
# GitHub's hosted macOS runners queue for a long time and often cancel the
# job before it starts, so the release workflow builds only Linux and
# Windows. This script runs on the maintainer's Mac (arm64) after CI has
# created the draft: it clones the tag into a clean directory, builds
# GitboxApp for darwin/arm64 and darwin/amd64 exactly as the CI jobs did,
# zips each app and packs each DMG, uploads the four files to the draft and
# adds their SHA-256 lines to the draft's checksums.sha256. Then
# sign-release.sh signs and publishes the release as before.
#
# Usage:
#   ./scripts/release-macos.sh v2.7.4             # build and upload
#   ./scripts/release-macos.sh v2.7.4 --no-upload # build only, keep the files
#
# Apple signing (optional, off unless set):
#   APPLE_IDENTITY  codesign identity from the login keychain, for example
#                   "Developer ID Application: Name (TEAMID)"
#   NOTARY_PROFILE  notarytool keychain profile (xcrun notarytool
#                   store-credentials); needs APPLE_IDENTITY
#
# Output stays in ~/.cache/gitbox-release/<tag>/. See docs/release-signing.md.

set -euo pipefail

G='\033[0;32m' R='\033[0;31m' C='\033[0;36m' D='\033[0;90m' N='\033[0m'
header() { printf '\n%b━━ %s ━━%b\n\n' "$C" "$*" "$N"; }
ok()     { printf '  %bok%b    %s\n' "$G" "$N" "$*"; }
info()   { printf '  %binfo%b  %s\n' "$D" "$N" "$*"; }
die()    { printf '%berror:%b %s\n' "$R" "$N" "$*" >&2; exit 1; }

WAILS_VERSION="v2.16.0" # same as ci.yml and gate-lane.sh
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

tag="${1:-}"
upload=true
[[ "${2:-}" == "--no-upload" ]] && upload=false
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "usage: release-macos.sh <vX.Y.Z> [--no-upload]"
[[ "$(uname -s)" == "Darwin" && "$(uname -m)" == "arm64" ]] || die "run this on an arm64 Mac"
[[ -z "${NOTARY_PROFILE:-}" || -n "${APPLE_IDENTITY:-}" ]] || die "NOTARY_PROFILE needs APPLE_IDENTITY"
for t in git go npm zip create-dmg shasum gh; do
    command -v "$t" >/dev/null || die "$t not found"
done

origin_url="$(git -C "$REPO_ROOT" config --get remote.origin.url)"
repo="$(echo "$origin_url" | sed -E 's|.*[:/]([^/]+/[^/]+)$|\1|; s|\.git$||')"
gh auth switch --user "${repo%%/*}" >/dev/null 2>&1 || true

header "macOS release assets for $repo $tag"

# ── The local tag must be the published tag ──
git -C "$REPO_ROOT" fetch -q origin "refs/tags/$tag:refs/tags/$tag" 2>/dev/null || true
local_obj="$(git -C "$REPO_ROOT" rev-parse -q --verify "refs/tags/$tag")" || die "tag $tag not found"
remote_obj="$(git -C "$REPO_ROOT" ls-remote origin "refs/tags/$tag" | awk '{print $1}')"
[[ "$local_obj" == "$remote_obj" ]] || die "local tag $tag differs from origin's (or is not pushed)"
commit="$(git -C "$REPO_ROOT" rev-parse "$tag^{commit}")"
ok "tag $tag → ${commit:0:12}"

if $upload; then
    is_draft="$(gh release view "$tag" --repo "$repo" --json isDraft -q .isDraft 2>/dev/null)" \
        || die "draft release $tag not found: wait for the CI workflow to create it"
    [[ "$is_draft" == "true" ]] || die "release $tag is already published"
    ok "draft release exists"
fi

# ── Clean clone of the tag and a pinned Wails CLI ──
out="$HOME/.cache/gitbox-release/$tag"
src="$out/src"
rm -rf "$out"
mkdir -p "$out/dist" "$out/bin"
git clone -q --no-hardlinks "$REPO_ROOT" "$src"
git -C "$src" -c advice.detachedHead=false checkout -q "$commit"
GOBIN="$out/bin" go install "github.com/wailsapp/wails/v2/cmd/wails@$WAILS_VERSION"
mkdir -p "$src/cmd/gui/build"
cp "$src/assets/appicon.png" "$src/cmd/gui/build/appicon.png"
sed -i '' "s/\"productVersion\": \"0.0.0\"/\"productVersion\": \"${tag#v}\"/" "$src/cmd/gui/wails.json"
ok "clean clone and Wails $WAILS_VERSION"

# ── One build identity, and the six gitbox-server binaries both apps embed ──
export GITBOX_VERSION="$tag" GITBOX_SERVER_TARGETS=all
VERSION="" COMMIT="" LDFLAGS=""   # assigned by the eval below
eval "$(bash "$src/scripts/stamp.sh")" || die "stamp.sh failed"
[[ "$COMMIT" == "$commit" ]] || die "the clean clone is at $COMMIT, not at the tag's $commit"
bash "$src/scripts/build-server.sh" >"$out/build-server.log" 2>&1 \
    || die "building gitbox-server failed, see $out/build-server.log"
ok "six gitbox-server binaries for $VERSION"

# build_arch <arch>: GUI → zip → optional codesign → DMG (→ optional notarization)
build_arch() {
    local arch="$1" bin="$src/cmd/gui/build/bin" stage="$out/stage-$1"
    info "building darwin/$arch"
    (
        cd "$src/cmd/gui"
        # Go's runtime targets macOS 13; give cgo and the linker the same minimum.
        CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 GOARCH="$arch" \
            "$out/bin/wails" build -clean -platform "darwin/$arch" -tags serverbin \
            -ldflags "$LDFLAGS" >"$out/build-$arch.log" 2>&1
    ) || die "build failed for $arch, see $out/build-$arch.log"
    # This machine runs the arm64 app, so that one answers for its identity;
    # the amd64 app is only measured, never started. Starting it runs it
    # under Rosetta, and macOS then lists GitboxApp as an Intel app that
    # stops opening in a later release (#350).
    local v="" exe="$bin/GitboxApp.app/Contents/MacOS/GitboxApp" what="all"
    if [[ "$arch" == "arm64" ]]; then
        v="$("$exe" --version 2>/dev/null || true)"
    else
        what="--size-only"
    fi
    if [[ "$what" == "all" ]]; then
        VERSION="$VERSION" COMMIT="$COMMIT" bash "$src/scripts/check-gui-artifact.sh" \
            "$exe" "darwin/$arch" >>"$out/build-$arch.log" 2>&1
    else
        bash "$src/scripts/check-gui-artifact.sh" "$what" "$exe" "darwin/$arch" >>"$out/build-$arch.log" 2>&1
    fi || die "the darwin/$arch app failed its check, see $out/build-$arch.log"
    (cd "$bin" && zip -qr "$out/dist/gitbox-macos-$arch.zip" GitboxApp.app)

    mkdir -p "$stage"
    (cd "$stage" && unzip -q "$out/dist/gitbox-macos-$arch.zip")
    cp "$src/scripts/dmg/Install Gitbox.command" "$src/scripts/dmg/README.txt" "$stage/"
    chmod +x "$stage/Install Gitbox.command"
    sed -i '' 's/\r$//' "$stage/Install Gitbox.command"
    if [[ -n "${APPLE_IDENTITY:-}" ]]; then
        codesign --force --deep --sign "$APPLE_IDENTITY" --options runtime "$stage/GitboxApp.app"
    fi
    create-dmg --volname "gitbox" \
        --window-pos 200 120 --window-size 660 500 --icon-size 100 \
        --icon "GitboxApp.app" 150 170 --app-drop-link 450 170 \
        --hide-extension "GitboxApp.app" \
        --icon "Install Gitbox.command" 150 370 --hide-extension "Install Gitbox.command" \
        --icon "README.txt" 450 370 \
        "$out/dist/gitbox-macos-$arch.dmg" "$stage/" >>"$out/build-$arch.log" 2>&1 || test $? -eq 2 \
        || die "create-dmg failed for $arch, see $out/build-$arch.log"
    if [[ -n "${NOTARY_PROFILE:-}" ]]; then
        xcrun notarytool submit "$out/dist/gitbox-macos-$arch.dmg" --keychain-profile "$NOTARY_PROFILE" --wait
        xcrun stapler staple "$out/dist/gitbox-macos-$arch.dmg"
    fi
    ok "darwin/$arch: ${v:-built} → zip and DMG"
}

build_arch arm64
build_arch amd64

if ! $upload; then
    ok "built without uploading: $out/dist"
    exit 0
fi

# ── Attach to the draft and extend its checksums ──
header "Upload to draft $tag"
existing="$(gh release view "$tag" --repo "$repo" --json assets -q '.assets[].name')"
grep -qx "checksums.sha256.sig" <<<"$existing" && die "draft $tag is already signed"
gh release download "$tag" --repo "$repo" --pattern checksums.sha256 --dir "$out" --clobber \
    || die "the draft has no checksums.sha256"
grep -Ev ' \*?gitbox-macos-' "$out/checksums.sha256" > "$out/checksums.new" || true
(cd "$out/dist" && shasum -a 256 gitbox-macos-arm64.zip gitbox-macos-amd64.zip \
    gitbox-macos-arm64.dmg gitbox-macos-amd64.dmg) >> "$out/checksums.new"
sort -k2 "$out/checksums.new" > "$out/dist/checksums.sha256"
gh release upload "$tag" --repo "$repo" --clobber "$out"/dist/gitbox-macos-* "$out/dist/checksums.sha256" \
    || die "upload failed; re-run the script"
ok "uploaded 4 macOS assets and the extended checksums.sha256"
info "next: ./scripts/sign-release.sh $tag"
