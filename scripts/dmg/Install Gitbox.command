#!/usr/bin/env bash
# Install Gitbox.command — macOS installer bundled inside the DMG
# Double-click this file in Finder or run: bash "/Volumes/gitbox/Install Gitbox.command"
set -euo pipefail

APP_DIR="/Applications"

# ── Output helpers ──────────────────────────────────────────────────

red()    { printf '\033[0;31m%s\033[0m\n' "$*"; }
green()  { printf '\033[0;32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[0;33m%s\033[0m\n' "$*"; }
bold()   { printf '\033[1m%s\033[0m\n' "$*"; }

log()  { green  "[gitbox] $*"; }
warn() { yellow "[gitbox] $*"; }
die()  { red    "[gitbox] $*"; exit 1; }

# ── Locate DMG contents ────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

[[ -d "$SCRIPT_DIR/GitboxApp.app" ]] || die "GitboxApp.app not found next to this script."

# ── Confirm ─────────────────────────────────────────────────────────

echo ""
bold "── gitbox installer ──"
echo ""
echo "  This script will:"
echo "    1. Copy GitboxApp.app → $APP_DIR/"
echo "    2. Remove quarantine attributes (xattr -cr)"
echo ""
warn "gitbox is NOT signed or notarized by Apple."
warn "You are trusting unsigned code. Audit the source: https://github.com/LuisPalacios/gitbox"
echo ""
printf "  Continue? [Y/n] "
read -r answer
case "${answer:-Y}" in
  [Yy]*) ;;
  *)     echo "Aborted."; exit 0 ;;
esac

echo ""

# ── Install GUI ─────────────────────────────────────────────────────

log "Installing GitboxApp.app → $APP_DIR/"
rm -rf "$APP_DIR/GitboxApp.app"
cp -R "$SCRIPT_DIR/GitboxApp.app" "$APP_DIR/GitboxApp.app"
xattr -cr "$APP_DIR/GitboxApp.app" 2>/dev/null || true
log "Done."

# ── Summary ─────────────────────────────────────────────────────────

echo ""
bold "── Installation complete ──"
echo ""
echo "  GUI:  $APP_DIR/GitboxApp.app"
echo ""

bold "  Get started:"
echo "    open $APP_DIR/GitboxApp.app"
echo ""
echo "  The gitbox CLI/TUI ships only in 1.x releases. To install it:"
echo "    bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh) --cli-only"
echo ""
