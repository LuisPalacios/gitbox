#!/usr/bin/env bash
# Install Gitbox.command — macOS installer bundled inside the DMG
# Double-click this file in Finder or run: bash "/Volumes/gitbox/Install Gitbox.command"
set -euo pipefail

INSTALL_DIR="$HOME/bin"
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
[[ -f "$SCRIPT_DIR/gitbox" ]]        || die "gitbox CLI binary not found next to this script."

# ── Confirm ─────────────────────────────────────────────────────────

echo ""
bold "── gitbox installer ──"
echo ""
echo "  This script will:"
echo "    1. Copy GitboxApp.app  → $APP_DIR/"
echo "    2. Copy gitbox CLI     → $INSTALL_DIR/"
echo "    3. Remove quarantine attributes (xattr -cr)"
echo "    4. Add $INSTALL_DIR to PATH if needed"
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

# ── Install CLI ─────────────────────────────────────────────────────

log "Installing gitbox CLI → $INSTALL_DIR/"
mkdir -p "$INSTALL_DIR"
cp "$SCRIPT_DIR/gitbox" "$INSTALL_DIR/gitbox"
chmod +x "$INSTALL_DIR/gitbox"
xattr -cr "$INSTALL_DIR/gitbox" 2>/dev/null || true
log "Done."

# ── PATH helper ─────────────────────────────────────────────────────

ensure_path() {
  local dir="$1"
  local marker="# gitbox"

  if echo "$PATH" | tr ':' '\n' | grep -qx "$dir"; then
    return
  fi

  local rc_file="$HOME/.zshrc"
  if [[ "$(basename "${SHELL:-/bin/zsh}")" != "zsh" ]]; then
    rc_file="$HOME/.bashrc"
  fi

  # Already added by a previous run?
  if [[ -f "$rc_file" ]] && grep -qF "$marker" "$rc_file"; then
    return
  fi

  log "Adding $dir to PATH in $rc_file"
  printf '\n%s\nexport PATH="%s:$PATH"\n' "$marker" "$dir" >> "$rc_file"
}

ensure_path "$INSTALL_DIR"

# ── Summary ─────────────────────────────────────────────────────────

echo ""
bold "── Installation complete ──"
echo ""
echo "  GUI:  $APP_DIR/GitboxApp.app"
echo "  CLI:  $INSTALL_DIR/gitbox"
echo ""

if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
  rc_hint="$HOME/.zshrc"
  if [[ "$(basename "${SHELL:-/bin/zsh}")" != "zsh" ]]; then
    rc_hint="$HOME/.bashrc"
  fi
  bold "  Reload your shell to pick up PATH changes:"
  echo "    source $rc_hint"
  echo ""
fi

bold "  Get started:"
echo "    gitbox help"
echo ""
