#!/usr/bin/env bash
# register-gitbox.sh — register GitboxApp with the Linux desktop (menu + icon).
#
# This script does NOT install binaries. It assumes GitboxApp is already on
# disk (placed there by scripts/bootstrap.sh, a manual copy, or `gitbox update`)
# and only registers the XDG `.desktop` entry and icon so the app appears in
# the Activities menu and can be pinned to the dock.
#
# Usage:
#   bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/register-gitbox.sh)
#   ... --bin ~/.local/bin/GitboxApp
#   ... --uninstall
#
# Env vars (alternative to flags, used by bootstrap.sh):
#   GITBOX_GUI_BIN   absolute path to GitboxApp (default: $HOME/bin/GitboxApp)
#   GITBOX_REF       git ref to fetch the icon from (default: main)

set -euo pipefail

REPO="LuisPalacios/gitbox"
GUI_BIN="${GITBOX_GUI_BIN:-$HOME/bin/GitboxApp}"
REF="${GITBOX_REF:-main}"
UNINSTALL=false

DESKTOP_DIR="$HOME/.local/share/applications"
DESKTOP_FILE="$DESKTOP_DIR/gitbox.desktop"
ICON_DIR="$HOME/.local/share/icons/hicolor/256x256/apps"
ICON_FILE="$ICON_DIR/gitbox.png"

# ── Output helpers ──────────────────────────────────────────────────

red()    { printf '\033[0;31m%s\033[0m\n' "$*"; }
green()  { printf '\033[0;32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[0;33m%s\033[0m\n' "$*"; }

log()  { green  "[gitbox] $*"; }
warn() { yellow "[gitbox] $*"; }
die()  { red    "[gitbox] $*"; exit 1; }

# ── Help ────────────────────────────────────────────────────────────

show_help() {
  cat <<'HELP'
register-gitbox.sh — register GitboxApp with the Linux desktop.

Usage:
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/register-gitbox.sh) [OPTIONS]

Options:
  --bin <path>     Absolute path to the GitboxApp binary.
                   Default: $HOME/bin/GitboxApp (or $GITBOX_GUI_BIN).
  --ref <git-ref>  Branch or tag to fetch the icon from on
                   raw.githubusercontent.com. Default: main.
  --uninstall      Remove the .desktop entry and icon (no binary touched).
  -h, --help       Show this help.

The script is idempotent: re-running it overwrites the same two files and
re-runs the desktop/icon caches. No need to run it again after a
`gitbox update` or a re-run of bootstrap.sh — the .desktop Exec= path is
absolute and stable.
HELP
  exit 0
}

# ── Argument parsing ────────────────────────────────────────────────

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --bin)       GUI_BIN="${2:?'--bin requires a path'}"; shift 2 ;;
      --ref)       REF="${2:?'--ref requires a git ref'}"; shift 2 ;;
      --uninstall) UNINSTALL=true; shift ;;
      -h|--help)   show_help ;;
      *)           die "Unknown option: $1 (try --help)" ;;
    esac
  done
}

# ── Platform check ──────────────────────────────────────────────────

check_platform() {
  local uname_s
  uname_s="$(uname -s 2>/dev/null || true)"
  if [[ "$uname_s" != "Linux" ]]; then
    log "Not on Linux ($uname_s) — nothing to register. Exiting."
    exit 0
  fi
}

# ── Refresh caches (best-effort) ────────────────────────────────────

refresh_caches() {
  if command -v update-desktop-database &>/dev/null; then
    update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true
  fi
  if command -v gtk-update-icon-cache &>/dev/null; then
    gtk-update-icon-cache -f -t "$HOME/.local/share/icons/hicolor" 2>/dev/null || true
  fi
}

# ── Uninstall ───────────────────────────────────────────────────────

do_uninstall() {
  local removed=false
  if [[ -f "$DESKTOP_FILE" ]]; then
    rm -f "$DESKTOP_FILE"
    log "Removed $DESKTOP_FILE"
    removed=true
  fi
  if [[ -f "$ICON_FILE" ]]; then
    rm -f "$ICON_FILE"
    log "Removed $ICON_FILE"
    removed=true
  fi
  if [[ "$removed" == false ]]; then
    log "Nothing to remove — no gitbox desktop entry was registered."
  fi
  refresh_caches
  log "Done. Menu entry unregistered."
}

# ── Install ─────────────────────────────────────────────────────────

check_binary() {
  if [[ ! -x "$GUI_BIN" ]]; then
    die "GitboxApp not found or not executable at: $GUI_BIN
       Install it first (e.g. via scripts/bootstrap.sh) or pass --bin <path>."
  fi
}

install_icon() {
  local icon_url="https://raw.githubusercontent.com/${REPO}/${REF}/assets/appicon.png"
  mkdir -p "$ICON_DIR"
  if ! command -v curl &>/dev/null; then
    warn "curl not found — skipping icon fetch. Menu entry will use a fallback icon."
    return
  fi
  if curl -fsSL -o "$ICON_FILE" "$icon_url"; then
    log "Icon installed: $ICON_FILE"
  else
    warn "Could not download icon from $icon_url — menu entry will use a fallback icon."
    rm -f "$ICON_FILE"
  fi
}

install_desktop() {
  mkdir -p "$DESKTOP_DIR"
  cat > "$DESKTOP_FILE" <<EOF
[Desktop Entry]
Name=Gitbox
Comment=Manage Git multi-account environments
Exec=${GUI_BIN}
Icon=gitbox
Type=Application
Categories=Development;
Terminal=false
StartupWMClass=GitboxApp
EOF
  chmod 644 "$DESKTOP_FILE"
  log "Desktop entry installed: $DESKTOP_FILE"
}

do_install() {
  check_binary
  install_icon
  install_desktop
  refresh_caches

  echo ""
  green "── gitbox desktop entry registered ──"
  echo ""
  echo "  Binary:  $GUI_BIN"
  echo "  Entry:   $DESKTOP_FILE"
  echo "  Icon:    $ICON_FILE"
  echo ""
  echo "  Search 'Gitbox' in Activities, or pin it to the dock."
  echo "  To remove: bash <(curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/register-gitbox.sh) --uninstall"
  echo ""
}

# ── Main ────────────────────────────────────────────────────────────

main() {
  parse_args "$@"
  check_platform
  if [[ "$UNINSTALL" == true ]]; then
    do_uninstall
  else
    do_install
  fi
}

main "$@"
