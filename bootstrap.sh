#!/usr/bin/env bash
# bootstrap.sh — cross-platform installer for gitbox
# Usage: bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/bootstrap.sh)
set -euo pipefail

REPO="LuisPalacios/gitbox"
GITHUB_API="https://api.github.com"
VERSION_TAG=""
INSTALL_DIR="$HOME/bin"
CLI_ONLY=false
PLATFORM=""
ARCH=""
ARTIFACT_NAME=""
DOWNLOAD_URL=""
RELEASE_TAG=""
TMP_DIR=""
USE_GH=false

# ── Output helpers ──────────────────────────────────────────────────

red()    { printf '\033[0;31m%s\033[0m\n' "$*"; }
green()  { printf '\033[0;32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[0;33m%s\033[0m\n' "$*"; }
bold()   { printf '\033[1m%s\033[0m\n' "$*"; }

log()  { green  "[gitbox] $*"; }
warn() { yellow "[gitbox] $*"; }
die()  { red    "[gitbox] $*"; exit 1; }

# ── Help ────────────────────────────────────────────────────────────

show_help() {
  cat <<'HELP'
gitbox installer — download and install gitbox CLI/TUI and GUI

Usage:
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/bootstrap.sh) [OPTIONS]

Options:
  --version <tag>   Install a specific release (e.g. v1.2.18). Default: latest.
  --prefix <dir>    CLI install directory. Default: ~/bin.
  --cli-only        Skip GUI installation.
  -h, --help        Show this help.

Examples:
  # Install latest
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/bootstrap.sh)

  # Install specific version, CLI only
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/bootstrap.sh) --version v1.2.18 --cli-only

  # Custom install directory
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/bootstrap.sh) --prefix ~/.local/bin
HELP
  exit 0
}

# ── Argument parsing ────────────────────────────────────────────────

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --version)  VERSION_TAG="${2:?'--version requires a tag (e.g. v1.2.18)'}"; shift 2 ;;
      --prefix)   INSTALL_DIR="${2:?'--prefix requires a directory'}"; shift 2 ;;
      --cli-only) CLI_ONLY=true; shift ;;
      -h|--help)  show_help ;;
      *)          die "Unknown option: $1 (try --help)" ;;
    esac
  done
}

# ── Platform detection ──────────────────────────────────────────────

detect_platform() {
  # OS
  case "${OSTYPE:-}" in
    darwin*)       PLATFORM="macos" ;;
    linux*)
      if grep -qi microsoft /proc/version 2>/dev/null; then
        PLATFORM="linux"  # WSL — treat as Linux
      else
        PLATFORM="linux"
      fi
      ;;
    msys*|mingw*|cygwin*) PLATFORM="windows" ;;
    *)
      # Fallback for unknown OSTYPE
      local uname_s
      uname_s="$(uname -s 2>/dev/null || true)"
      case "$uname_s" in
        Darwin)  PLATFORM="macos"   ;;
        Linux)   PLATFORM="linux"   ;;
        MINGW*|MSYS*) PLATFORM="windows" ;;
        *)       die "Unsupported OS: ${OSTYPE:-$uname_s}" ;;
      esac
      ;;
  esac

  # Architecture
  local machine
  machine="$(uname -m)"
  case "$machine" in
    arm64|aarch64) ARCH="arm64" ;;
    x86_64|amd64)  ARCH="amd64" ;;
    *)             die "Unsupported architecture: $machine" ;;
  esac

  # Map to artifact name and validate supported combos
  case "${PLATFORM}-${ARCH}" in
    macos-arm64)   ARTIFACT_NAME="gitbox-macos-arm64.zip" ;;
    macos-amd64)   ARTIFACT_NAME="gitbox-macos-amd64.zip" ;;
    linux-amd64)   ARTIFACT_NAME="gitbox-linux-amd64.zip" ;;
    windows-amd64) ARTIFACT_NAME="gitbox-win-amd64.zip" ;;
    linux-arm64)   die "Linux arm64 builds are not available yet." ;;
    windows-arm64) die "Windows arm64 builds are not available yet." ;;
    *)             die "Unsupported platform/arch combo: ${PLATFORM}/${ARCH}" ;;
  esac

  log "Detected: $PLATFORM/$ARCH → $ARTIFACT_NAME"
}

# ── Dependency check ────────────────────────────────────────────────

check_dependencies() {
  local missing=()

  command -v curl &>/dev/null || missing+=("curl")

  # unzip is required on macOS/Linux; on Windows we can fall back to PowerShell
  if [[ "$PLATFORM" != "windows" ]]; then
    command -v unzip &>/dev/null || missing+=("unzip")
  fi

  if [[ ${#missing[@]} -gt 0 ]]; then
    die "Missing required tools: ${missing[*]}. Install them and try again."
  fi

  # Check if gh CLI is available and authenticated
  if command -v gh &>/dev/null && gh auth status &>/dev/null 2>&1; then
    USE_GH=true
  fi
}

# ── Headless detection (Linux only) ────────────────────────────────

detect_headless() {
  if [[ "$PLATFORM" == "linux" && "$CLI_ONLY" == false ]]; then
    if [[ -z "${DISPLAY:-}" && -z "${WAYLAND_DISPLAY:-}" ]]; then
      warn "No display detected — installing CLI only (use --cli-only to silence this)."
      CLI_ONLY=true
    fi
  fi
}

# ── Download release ────────────────────────────────────────────────

get_release_info() {
  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT

  if [[ "$USE_GH" == true ]]; then
    log "Downloading via gh CLI..."
    local gh_args=(release download --repo "$REPO" --pattern "$ARTIFACT_NAME" --dir "$TMP_DIR")
    if [[ -n "$VERSION_TAG" ]]; then
      gh_args=(release download "$VERSION_TAG" --repo "$REPO" --pattern "$ARTIFACT_NAME" --dir "$TMP_DIR")
    fi
    if ! gh "${gh_args[@]}"; then
      die "Failed to download $ARTIFACT_NAME from $REPO. Check the version tag and try again."
    fi
    # Determine the tag we actually downloaded
    if [[ -n "$VERSION_TAG" ]]; then
      RELEASE_TAG="$VERSION_TAG"
    else
      RELEASE_TAG="$(gh release view --repo "$REPO" --json tagName -q .tagName 2>/dev/null || echo "latest")"
    fi
  else
    log "Downloading via GitHub API..."
    local api_url
    if [[ -n "$VERSION_TAG" ]]; then
      api_url="${GITHUB_API}/repos/${REPO}/releases/tags/${VERSION_TAG}"
    else
      api_url="${GITHUB_API}/repos/${REPO}/releases/latest"
    fi

    local api_response http_code
    http_code="$(curl -fsSL -w '%{http_code}' -o "$TMP_DIR/api.json" "$api_url" 2>/dev/null || true)"

    if [[ "$http_code" == "403" ]]; then
      die "GitHub API rate limit hit. Set GITHUB_TOKEN env var or install gh CLI (gh auth login)."
    elif [[ "$http_code" != "200" ]]; then
      die "Failed to fetch release info (HTTP $http_code). Check the version tag and network."
    fi

    api_response="$(<"$TMP_DIR/api.json")"

    RELEASE_TAG="$(printf '%s' "$api_response" | grep -m1 '"tag_name"' | sed 's/.*: *"\([^"]*\)".*/\1/')"
    DOWNLOAD_URL="$(printf '%s' "$api_response" | grep -o "https://[^\"]*/${ARTIFACT_NAME}" | head -1)"

    if [[ -z "$DOWNLOAD_URL" ]]; then
      die "Artifact $ARTIFACT_NAME not found in release $RELEASE_TAG."
    fi

    log "Downloading $ARTIFACT_NAME ($RELEASE_TAG)..."

    if [[ -n "${GITHUB_TOKEN:-}" ]]; then
      local curl_cmd=(curl -fSL --progress-bar -H "Authorization: token $GITHUB_TOKEN" -o "$TMP_DIR/$ARTIFACT_NAME" "$DOWNLOAD_URL")
    else
      local curl_cmd=(curl -fSL --progress-bar -o "$TMP_DIR/$ARTIFACT_NAME" "$DOWNLOAD_URL")
    fi

    if ! "${curl_cmd[@]}"; then
      die "Download failed. Check your network and try again."
    fi
  fi
}

# ── Extract ─────────────────────────────────────────────────────────

extract_archive() {
  log "Extracting..."
  mkdir -p "$TMP_DIR/extracted"

  if command -v unzip &>/dev/null; then
    unzip -o -q "$TMP_DIR/$ARTIFACT_NAME" -d "$TMP_DIR/extracted"
  elif [[ "$PLATFORM" == "windows" ]]; then
    powershell -Command "Expand-Archive -Force -Path '$TMP_DIR/$ARTIFACT_NAME' -DestinationPath '$TMP_DIR/extracted'" 2>/dev/null \
      || die "Failed to extract archive. Install unzip or ensure PowerShell is available."
  else
    die "unzip is required but not found."
  fi
}

# ── Existing install detection ──────────────────────────────────────

detect_existing_install() {
  local cli_bin="gitbox"
  [[ "$PLATFORM" == "windows" ]] && cli_bin="gitbox.exe"

  if [[ -x "$INSTALL_DIR/$cli_bin" ]]; then
    local old_version
    old_version="$("$INSTALL_DIR/$cli_bin" version 2>/dev/null || echo "unknown")"
    warn "Existing installation found ($old_version) — upgrading to $RELEASE_TAG."
  fi
}

# ── PATH helper ─────────────────────────────────────────────────────

ensure_path() {
  local dir="$1"
  local marker="# gitbox"

  # Already in PATH? Nothing to do.
  if echo "$PATH" | tr ':' '\n' | grep -qx "$dir"; then
    return
  fi

  local rc_file=""
  case "$PLATFORM" in
    macos)   rc_file="$HOME/.zshrc" ;;
    linux)
      if [[ "$(basename "${SHELL:-/bin/bash}")" == "zsh" ]]; then
        rc_file="$HOME/.zshrc"
      else
        rc_file="$HOME/.bashrc"
      fi
      ;;
    windows) rc_file="$HOME/.bashrc" ;;
  esac

  if [[ -z "$rc_file" ]]; then
    warn "Could not determine shell rc file. Add $dir to your PATH manually."
    return
  fi

  # Already added by a previous run?
  if [[ -f "$rc_file" ]] && grep -qF "$marker" "$rc_file"; then
    return
  fi

  log "Adding $dir to PATH in $rc_file"
  printf '\n%s\nexport PATH="%s:$PATH"\n' "$marker" "$dir" >> "$rc_file"
}

# ── macOS install ───────────────────────────────────────────────────

install_macos() {
  mkdir -p "$INSTALL_DIR"

  # CLI
  cp "$TMP_DIR/extracted/gitbox" "$INSTALL_DIR/gitbox"
  chmod +x "$INSTALL_DIR/gitbox"
  xattr -cr "$INSTALL_DIR/gitbox" 2>/dev/null || true
  log "CLI installed: $INSTALL_DIR/gitbox"

  # GUI
  if [[ "$CLI_ONLY" == false ]]; then
    rm -rf /Applications/GitboxApp.app
    cp -R "$TMP_DIR/extracted/GitboxApp.app" /Applications/GitboxApp.app
    xattr -cr /Applications/GitboxApp.app 2>/dev/null || true
    log "GUI installed: /Applications/GitboxApp.app"
  fi

  ensure_path "$INSTALL_DIR"
}

# ── Linux install ───────────────────────────────────────────────────

install_linux() {
  mkdir -p "$INSTALL_DIR"

  # CLI
  cp "$TMP_DIR/extracted/gitbox" "$INSTALL_DIR/gitbox"
  chmod +x "$INSTALL_DIR/gitbox"
  log "CLI installed: $INSTALL_DIR/gitbox"

  # GUI
  if [[ "$CLI_ONLY" == false ]]; then
    cp "$TMP_DIR/extracted/GitboxApp" "$INSTALL_DIR/GitboxApp"
    chmod +x "$INSTALL_DIR/GitboxApp"
    log "GUI installed: $INSTALL_DIR/GitboxApp"
  fi

  ensure_path "$INSTALL_DIR"
}

# ── Windows (Git Bash) install ──────────────────────────────────────

install_windows() {
  mkdir -p "$INSTALL_DIR"

  local win_path
  win_path="$(cygpath -w "$INSTALL_DIR" 2>/dev/null || echo "$INSTALL_DIR")"

  # CLI
  cp "$TMP_DIR/extracted/gitbox.exe" "$INSTALL_DIR/gitbox.exe"
  # Remove "downloaded from internet" mark so SmartScreen doesn't block it
  powershell -Command "Unblock-File -Path '${win_path}\\gitbox.exe'" 2>/dev/null || true
  log "CLI installed: $INSTALL_DIR/gitbox.exe"

  # GUI
  if [[ "$CLI_ONLY" == false ]]; then
    cp "$TMP_DIR/extracted/GitboxApp.exe" "$INSTALL_DIR/GitboxApp.exe"
    powershell -Command "Unblock-File -Path '${win_path}\\GitboxApp.exe'" 2>/dev/null || true
    log "GUI installed: $INSTALL_DIR/GitboxApp.exe"
    log "Windows path: $win_path"
  fi

  ensure_path "$INSTALL_DIR"
}

# ── Summary ─────────────────────────────────────────────────────────

print_summary() {
  echo ""
  bold "── gitbox $RELEASE_TAG installed ──"
  echo ""

  local cli_bin="gitbox"
  [[ "$PLATFORM" == "windows" ]] && cli_bin="gitbox.exe"
  echo "  CLI/TUI:  $INSTALL_DIR/$cli_bin"

  if [[ "$CLI_ONLY" == false ]]; then
    case "$PLATFORM" in
      macos)   echo "  GUI:      /Applications/GitboxApp.app" ;;
      linux)   echo "  GUI:      $INSTALL_DIR/GitboxApp" ;;
      windows) echo "  GUI:      $INSTALL_DIR/GitboxApp.exe" ;;
    esac
  fi

  echo ""

  # Check if user needs to reload shell
  if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    local rc_file=""
    case "$PLATFORM" in
      macos) rc_file="~/.zshrc" ;;
      linux)
        if [[ "$(basename "${SHELL:-/bin/bash}")" == "zsh" ]]; then
          rc_file="~/.zshrc"
        else
          rc_file="~/.bashrc"
        fi
        ;;
      windows) rc_file="~/.bashrc" ;;
    esac
    if [[ -n "$rc_file" ]]; then
      bold "  Reload your shell to pick up PATH changes:"
      echo "    source $rc_file"
      echo ""
    fi
  fi

  bold "  Get started:"
  echo "    gitbox help"
  echo ""
}

# ── Main ────────────────────────────────────────────────────────────

main() {
  parse_args "$@"
  detect_platform
  check_dependencies
  detect_headless
  get_release_info
  extract_archive
  detect_existing_install

  case "$PLATFORM" in
    macos)   install_macos   ;;
    linux)   install_linux   ;;
    windows) install_windows ;;
  esac

  print_summary
}

main "$@"
