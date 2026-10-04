#!/usr/bin/env bash
# bootstrap.sh — cross-platform installer for gitbox (the GitboxApp GUI, or
# the 1.x CLI/TUI with --cli-only)
# Usage: bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh)
set -euo pipefail

REPO="LuisPalacios/gitbox"
GITHUB_API="https://api.github.com"
VERSION_TAG=""
INSTALL_DIR="$HOME/bin"
CLI_ONLY=false
NO_DESKTOP=false
PLATFORM=""
ARCH=""
ARTIFACT_NAME=""
DOWNLOAD_URL=""
RELEASE_TAG=""
TMP_DIR=""
USE_GH=false
CLI_INSTALLED=false
GUI_PATH=""
DESKTOP_REGISTERED=false

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
gitbox installer — download and install the GitboxApp GUI (or, with
--cli-only, the gitbox CLI/TUI from the 1.x line)

Usage:
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh) [OPTIONS]

Options:
  --version <tag>   Install a specific release (e.g. v2.0.0). Default: latest
                    (with --cli-only: the latest 1.x release). A 1.x tag
                    without --cli-only installs both the GUI and the CLI.
  --prefix <dir>    Install directory for the CLI and, on Linux and Windows,
                    the GUI. Default: ~/bin. On macOS the GUI always goes to
                    /Applications.
  --cli-only        Install only the gitbox CLI/TUI. It ships in 1.x
                    releases only (v2 is GUI-only), so this installs the
                    latest 1.x. Linux hosts without a display pick this
                    automatically.
  --no-desktop      Linux only: skip registering the GUI in the Activities
                    menu. The binary is still installed; you can register
                    it later with scripts/register-gitbox.sh.
  -h, --help        Show this help.

Examples:
  # Install the latest GUI
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh)

  # Install the latest 1.x CLI/TUI only
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh) --cli-only

  # Install a specific version
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh) --version v2.0.0

  # Custom install directory
  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh) --prefix ~/.local/bin
HELP
  exit 0
}

# ── Argument parsing ────────────────────────────────────────────────

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --version)  VERSION_TAG="${2:?'--version requires a tag (e.g. v2.0.0)'}"; shift 2 ;;
      --prefix)   INSTALL_DIR="${2:?'--prefix requires a directory'}"; shift 2 ;;
      --cli-only) CLI_ONLY=true; shift ;;
      --no-desktop) NO_DESKTOP=true; shift ;;
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
      warn "No display detected — installing the 1.x CLI only (use --cli-only to silence this)."
      CLI_ONLY=true
    fi
  fi
}

# ── CLI release line ────────────────────────────────────────────────

# The CLI/TUI ships only in 1.x releases; v2 and later are GUI-only. For a
# CLI-only install, pin to the newest stable 1.x release unless the user
# asked for a specific tag, and refuse tags that carry no CLI.
resolve_cli_version() {
  [[ "$CLI_ONLY" == true ]] || return 0

  if [[ -n "$VERSION_TAG" ]]; then
    local major="${VERSION_TAG#v}"
    major="${major%%.*}"
    if [[ "$major" =~ ^[0-9]+$ ]] && (( major >= 2 )); then
      die "Release $VERSION_TAG is GUI-only. The CLI/TUI ships in 1.x releases — omit --version to get the latest 1.x."
    fi
    return 0
  fi

  local list
  if [[ "$USE_GH" == true ]]; then
    list="$(gh api "repos/${REPO}/releases?per_page=100" 2>/dev/null || true)"
  else
    local auth=()
    [[ -n "${GITHUB_TOKEN:-}" ]] && auth=(-H "Authorization: token $GITHUB_TOKEN")
    list="$(curl -fsSL ${auth[@]+"${auth[@]}"} "${GITHUB_API}/repos/${REPO}/releases?per_page=100" 2>/dev/null || true)"
  fi
  [[ -n "$list" ]] || die "Failed to list releases. Set GITHUB_TOKEN or pass --version v1.x.y."

  # Releases come newest first; pick the first stable v1.* one. Each release
  # object lists tag_name before draft and prerelease.
  VERSION_TAG="$(printf '%s' "$list" \
    | grep -oE '"(tag_name|draft|prerelease)": *("[^"]*"|true|false)' \
    | awk -F': *' '
        /"tag_name"/   { tag = $2; gsub(/"/, "", tag); draft = ""; next }
        /"draft"/      { draft = $2; next }
        /"prerelease"/ { if (tag ~ /^v1\./ && draft == "false" && $2 == "false") { print tag; exit } }')"

  [[ -n "$VERSION_TAG" ]] || die "No 1.x release found for the CLI."
  log "CLI-only install: using $VERSION_TAG (latest 1.x release)."
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

  # 1.x archives carry the CLI next to the GUI; 2.x archives carry the GUI
  # only. Fail before touching anything when the requested part is missing.
  if [[ "$CLI_ONLY" == true && ! -f "$TMP_DIR/extracted/$(cli_bin_name)" ]]; then
    die "$ARTIFACT_NAME ($RELEASE_TAG) has no CLI. The CLI/TUI ships in 1.x releases only."
  fi
  if [[ "$CLI_ONLY" == false && ! -e "$TMP_DIR/extracted/$(gui_bin_name)" ]]; then
    die "$ARTIFACT_NAME ($RELEASE_TAG) has no $(gui_bin_name)."
  fi
}

# ── Binary names ────────────────────────────────────────────────────

cli_bin_name() {
  if [[ "$PLATFORM" == "windows" ]]; then echo "gitbox.exe"; else echo "gitbox"; fi
}

gui_bin_name() {
  case "$PLATFORM" in
    macos)   echo "GitboxApp.app" ;;
    windows) echo "GitboxApp.exe" ;;
    *)       echo "GitboxApp" ;;
  esac
}

# Where the GUI lives once installed: /Applications on macOS, the install
# directory elsewhere.
gui_install_path() {
  if [[ "$PLATFORM" == "macos" ]]; then
    echo "/Applications/GitboxApp.app"
  else
    echo "$INSTALL_DIR/$(gui_bin_name)"
  fi
}

# ── Existing install detection ──────────────────────────────────────

detect_existing_install() {
  local found="" cli gui
  cli="$INSTALL_DIR/$(cli_bin_name)"
  gui="$(gui_install_path)"

  if [[ -x "$cli" ]]; then
    found="CLI $("$cli" version 2>/dev/null || echo "unknown version")"
  fi
  # Only check that an existing GitboxApp is there, never run it: v1 builds
  # have no --version flag and would open their window instead of exiting.
  if [[ -e "$gui" ]]; then
    found="${found:+$found, }GUI at $gui"
  fi

  if [[ -n "$found" ]]; then
    warn "Existing installation found ($found) — upgrading to $RELEASE_TAG."
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
  # CLI (1.x archives only)
  if [[ -f "$TMP_DIR/extracted/gitbox" ]]; then
    mkdir -p "$INSTALL_DIR"
    cp "$TMP_DIR/extracted/gitbox" "$INSTALL_DIR/gitbox"
    chmod +x "$INSTALL_DIR/gitbox"
    xattr -cr "$INSTALL_DIR/gitbox" 2>/dev/null || true
    CLI_INSTALLED=true
    log "CLI installed: $INSTALL_DIR/gitbox"
  fi

  # GUI
  if [[ "$CLI_ONLY" == false ]]; then
    rm -rf /Applications/GitboxApp.app
    cp -R "$TMP_DIR/extracted/GitboxApp.app" /Applications/GitboxApp.app
    xattr -cr /Applications/GitboxApp.app 2>/dev/null || true
    GUI_PATH="/Applications/GitboxApp.app"
    log "GUI installed: $GUI_PATH"
  fi

  # The GUI lives in /Applications, so only the CLI needs the PATH entry.
  if [[ "$CLI_INSTALLED" == true ]]; then
    ensure_path "$INSTALL_DIR"
  fi
}

# ── Linux install ───────────────────────────────────────────────────

register_linux_desktop() {
  local script_url="https://raw.githubusercontent.com/${REPO}/main/scripts/register-gitbox.sh"
  local script_path="$TMP_DIR/register-gitbox.sh"
  if ! curl -fsSL -o "$script_path" "$script_url"; then
    warn "Could not download register-gitbox.sh — skipping menu entry."
    warn "Register later with: bash <(curl -fsSL $script_url)"
    return
  fi
  if GITBOX_GUI_BIN="$INSTALL_DIR/GitboxApp" bash "$script_path"; then
    DESKTOP_REGISTERED=true
  else
    warn "Desktop registration failed — run manually: bash <(curl -fsSL $script_url)"
  fi
}

install_linux() {
  mkdir -p "$INSTALL_DIR"

  # CLI (1.x archives only)
  if [[ -f "$TMP_DIR/extracted/gitbox" ]]; then
    cp "$TMP_DIR/extracted/gitbox" "$INSTALL_DIR/gitbox"
    chmod +x "$INSTALL_DIR/gitbox"
    CLI_INSTALLED=true
    log "CLI installed: $INSTALL_DIR/gitbox"
  fi

  # GUI
  if [[ "$CLI_ONLY" == false ]]; then
    cp "$TMP_DIR/extracted/GitboxApp" "$INSTALL_DIR/GitboxApp"
    chmod +x "$INSTALL_DIR/GitboxApp"
    GUI_PATH="$INSTALL_DIR/GitboxApp"
    log "GUI installed: $GUI_PATH"
    if [[ "$NO_DESKTOP" == false ]]; then
      register_linux_desktop
    fi
  fi

  ensure_path "$INSTALL_DIR"
}

# ── Windows (Git Bash) install ──────────────────────────────────────

install_windows() {
  mkdir -p "$INSTALL_DIR"

  local win_path
  win_path="$(cygpath -w "$INSTALL_DIR" 2>/dev/null || echo "$INSTALL_DIR")"

  # CLI (1.x archives only)
  if [[ -f "$TMP_DIR/extracted/gitbox.exe" ]]; then
    cp "$TMP_DIR/extracted/gitbox.exe" "$INSTALL_DIR/gitbox.exe"
    # Remove "downloaded from internet" mark so SmartScreen doesn't block it
    powershell -Command "Unblock-File -Path '${win_path}\\gitbox.exe'" 2>/dev/null || true
    CLI_INSTALLED=true
    log "CLI installed: $INSTALL_DIR/gitbox.exe"
  fi

  # GUI
  if [[ "$CLI_ONLY" == false ]]; then
    cp "$TMP_DIR/extracted/GitboxApp.exe" "$INSTALL_DIR/GitboxApp.exe"
    powershell -Command "Unblock-File -Path '${win_path}\\GitboxApp.exe'" 2>/dev/null || true
    GUI_PATH="$INSTALL_DIR/GitboxApp.exe"
    log "GUI installed: $GUI_PATH"
    log "Windows path: $win_path"
  fi

  ensure_path "$INSTALL_DIR"
}

# ── Summary ─────────────────────────────────────────────────────────

print_summary() {
  echo ""
  bold "── gitbox $RELEASE_TAG installed ──"
  echo ""

  if [[ -n "$GUI_PATH" ]]; then
    echo "  GUI:      $GUI_PATH"
    if [[ "$DESKTOP_REGISTERED" == true ]]; then
      echo "  Menu:     registered in Activities — search 'Gitbox' or drag to dock"
    fi
  fi
  if [[ "$CLI_INSTALLED" == true ]]; then
    echo "  CLI/TUI:  $INSTALL_DIR/$(cli_bin_name)"
  fi

  echo ""

  # Reload hint only when something landed in the install directory, which
  # on macOS means the CLI (the GUI goes to /Applications).
  local uses_dir=false
  if [[ "$CLI_INSTALLED" == true ]] || { [[ -n "$GUI_PATH" ]] && [[ "$PLATFORM" != "macos" ]]; }; then
    uses_dir=true
  fi
  if [[ "$uses_dir" == true ]] && ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    local rc_file=""
    case "$PLATFORM" in
      macos) rc_file=".zshrc" ;;
      linux)
        if [[ "$(basename "${SHELL:-/bin/bash}")" == "zsh" ]]; then
          rc_file=".zshrc"
        else
          rc_file=".bashrc"
        fi
        ;;
      windows) rc_file=".bashrc" ;;
    esac
    if [[ -n "$rc_file" ]]; then
      bold "  Reload your shell to pick up PATH changes:"
      echo "    source ~/$rc_file"
      echo ""
    fi
  fi

  bold "  Get started:"
  if [[ -n "$GUI_PATH" ]]; then
    case "$PLATFORM" in
      macos) echo "    open $GUI_PATH" ;;
      *)     echo "    $GUI_PATH" ;;
    esac
  fi
  if [[ "$CLI_INSTALLED" == true ]]; then
    echo "    gitbox help"
  fi
  echo ""
}

# ── Main ────────────────────────────────────────────────────────────

main() {
  parse_args "$@"
  detect_platform
  check_dependencies
  detect_headless
  resolve_cli_version
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
