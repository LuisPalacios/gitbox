#!/usr/bin/env bash
#
# sign-release.sh — Sign a draft GitHub release and publish it.
#
# CI creates every release as a draft. This script runs on the maintainer's
# machine: it downloads the draft's checksums.sha256, signs
# "gitbox <tag>\n" + checksums with the release key held by the local SSH
# agent (ssh-keygen -Y sign, namespace gitbox-release), checks the
# signature against allowed_signers, uploads checksums.sha256.sig and
# publishes the release. The private key never leaves the agent; the
# public half is pkg/update/release-signing-key.pub, the same key the
# updater pins. See docs/release-signing.md.
#
# Usage:
#   ./scripts/sign-release.sh v2.2.0            # sign and publish the draft
#   ./scripts/sign-release.sh v2.2.0 --dry-run  # sign and verify, publish nothing
#   ./scripts/sign-release.sh --check           # sign a test message: agent setup check
#
# Environment:
#   SSH_KEYGEN  ssh-keygen binary to use (default: ssh-keygen on PATH; on
#               Windows the native OpenSSH one, which reaches the agent's
#               named pipe)


# Standalone on purpose: _common.sh needs the .env of remote test hosts,
# and signing must work in a fork that has none.
set -euo pipefail

G='\033[0;32m' R='\033[0;31m' Y='\033[0;33m' C='\033[0;36m' D='\033[0;90m' N='\033[0m'
header() { printf '\n%b━━ %s ━━%b\n\n' "$C" "$*" "$N"; }
ok()     { printf '  %bok%b    %s\n' "$G" "$N" "$*"; }
warn()   { printf '  %bwarn%b  %s\n' "$Y" "$N" "$*"; }
info()   { printf '  %binfo%b  %s\n' "$D" "$N" "$*"; }
die()    { printf '%berror:%b %s\n' "$R" "$N" "$*" >&2; exit 1; }

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAMESPACE="gitbox-release"
PUBKEY="$REPO_ROOT/pkg/update/release-signing-key.pub"
ALLOWED_SIGNERS="$REPO_ROOT/allowed_signers"

# On Windows, Git Bash's own OpenSSH cannot reach an agent behind the
# Windows named pipe; the native one in System32 can.
on_windows=false
case "${OSTYPE:-}" in msys*|mingw*|cygwin*) on_windows=true ;; esac
default_keygen="ssh-keygen"
if $on_windows; then
    win_keygen="$(cygpath -u "${SYSTEMROOT:-C:\\Windows}")/System32/OpenSSH/ssh-keygen.exe"
    [[ -x "$win_keygen" ]] && default_keygen="$win_keygen"
fi
KEYGEN="${SSH_KEYGEN:-$default_keygen}"

usage() {
    cat >&2 <<'EOF'
usage: sign-release.sh <tag> [--dry-run]   sign and publish the draft release <tag>
       sign-release.sh --check             sign a test message to check the agent setup
EOF
    exit 1
}

# keygen runs ssh-keygen. Windows' native ssh-keygen.exe takes Windows
# paths and talks to the agent's named pipe; an SSH_AUTH_SOCK inherited
# from Git Bash would point it at a Unix socket it cannot use.
native_keygen() { $on_windows && [[ "$KEYGEN" == *.exe ]]; }

keygen() {
    if native_keygen; then
        env -u SSH_AUTH_SOCK "$KEYGEN" "$@"
    else
        "$KEYGEN" "$@"
    fi
}

native_path() {
    if native_keygen; then
        cygpath -w "$1"
    else
        printf '%s\n' "$1"
    fi
}

# sign_and_verify <message-file>: writes <message-file>.sig through the
# agent and checks it against allowed_signers.
sign_and_verify() {
    local msg="$1"
    keygen -Y sign -f "$(native_path "$PUBKEY")" -n "$NAMESPACE" "$(native_path "$msg")" \
        || die "signing failed: is the release key loaded in your SSH agent? (ssh-add -L must list $(cut -d' ' -f1,2 "$PUBKEY" | cut -c1-40)…)"
    keygen -Y verify -f "$(native_path "$ALLOWED_SIGNERS")" -I "$NAMESPACE" -n "$NAMESPACE" \
        -s "$(native_path "$msg.sig")" < "$msg" >/dev/null \
        || die "the new signature does not verify against allowed_signers"
}

[[ -f "$PUBKEY" ]] || die "missing $PUBKEY"
[[ -f "$ALLOWED_SIGNERS" ]] || die "missing $ALLOWED_SIGNERS"
command -v "$KEYGEN" >/dev/null || die "$KEYGEN not found"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# ── Agent setup check ──
if [[ "${1:-}" == "--check" ]]; then
    header "Release signing check"
    info "key: $(keygen -l -f "$(native_path "$PUBKEY")")"
    printf 'gitbox signing check %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$work/check.txt"
    info "signing a test message (approve the prompt if your agent shows one)"
    sign_and_verify "$work/check.txt"
    ok "the agent signs with the release key and allowed_signers verifies it"
    exit 0
fi

tag="${1:-}"
dry_run=false
[[ "${2:-}" == "--dry-run" ]] && dry_run=true
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage

command -v gh >/dev/null || die "gh not found"
origin_url="$(git -C "$REPO_ROOT" config --get remote.origin.url)"
repo="$(echo "$origin_url" | sed -E 's|.*[:/]([^/]+/[^/]+)$|\1|; s|\.git$||')"
owner="${repo%%/*}"
# With several gh accounts, make the repo owner's the active one.
gh auth switch --user "$owner" >/dev/null 2>&1 || true

header "Sign $repo $tag"

# ── The release must be an unsigned draft with a checksums file ──
is_draft="$(gh release view "$tag" --repo "$repo" --json isDraft -q .isDraft)" \
    || die "release $tag not found (CI creates it as a draft when the tag is pushed)"
[[ "$is_draft" == "true" ]] || die "release $tag is already published; only drafts are signed"
assets="$(gh release view "$tag" --repo "$repo" --json assets -q '.assets[].name' | sort)"
grep -qx "checksums.sha256" <<<"$assets" || die "release $tag has no checksums.sha256"
grep -qx "checksums.sha256.sig" <<<"$assets" && die "release $tag already has checksums.sha256.sig"
ok "draft release with checksums.sha256"

gh release download "$tag" --repo "$repo" --pattern checksums.sha256 --dir "$work" \
    || die "downloading checksums.sha256 failed"

# Every asset is listed in the checksums and every listed file is an asset,
# so the signature covers exactly what the release ships.
listed="$(awk '{print $2}' "$work/checksums.sha256" | sed 's/^\*//' | sort)"
shipped="$(grep -vx 'checksums.sha256' <<<"$assets")"
[[ "$listed" == "$shipped" ]] || die "checksums.sha256 and the release assets differ:
$(diff <(echo "$listed") <(echo "$shipped") || true)"
ok "checksums.sha256 lists all $(wc -l <<<"$shipped" | tr -d ' ') assets"

{ printf 'gitbox %s\n' "$tag"; cat "$work/checksums.sha256"; } > "$work/message"
info "signing with $(keygen -l -f "$(native_path "$PUBKEY")" | awk '{print $2}') (approve the prompt if your agent shows one)"
sign_and_verify "$work/message"
mv "$work/message.sig" "$work/checksums.sha256.sig"
ok "signed and verified against allowed_signers"

# Only the highest major line may become "latest": a v1.x maintenance
# release published after v2.0.0 must not hijack releases/latest, which
# every v2 GUI's updater follows.
major="${tag#v}"; major="${major%%.*}"
top="$(gh release list --repo "$repo" --limit 100 --exclude-drafts --json tagName -q '.[].tagName' \
         | sed -E 's/^v([0-9]+)\..*/\1/' | grep -E '^[0-9]+$' | sort -n | tail -1 || true)"
latest=true
if [[ -n "$top" && "$major" -lt "$top" ]]; then latest=false; fi

if $dry_run; then
    warn "dry run: would upload checksums.sha256.sig and publish $tag with --latest=$latest"
    exit 0
fi

gh release upload "$tag" --repo "$repo" "$work/checksums.sha256.sig" \
    || die "uploading the signature failed; the draft stays unpublished"
gh release edit "$tag" --repo "$repo" --draft=false --latest="$latest" >/dev/null \
    || die "publishing failed; the signature is uploaded, re-run: gh release edit $tag --draft=false --latest=$latest"
ok "published $tag (--latest=$latest, highest existing major: ${top:-none})"
