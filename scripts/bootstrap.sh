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
RELEASE_TAG=""
TMP_DIR=""
USE_GH=false
CLI_INSTALLED=false
GUI_PATH=""
DESKTOP_REGISTERED=false
INSTALLER_COPY=""
# Uninstall key of the Windows setup exe; must match AppId in scripts/installer.iss.
INNO_APP_ID='{8B2F4E3A-1C5D-4F8E-9A7B-3D6E2F1A8C4B}_is1'

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

Verification:
  OpenSSH 8.2+ (ssh-keygen) and SHA-256 support are required. Every release,
  including --cli-only 1.x, must carry checksums.sha256.sig signed by the
  pinned release key. Missing signatures stop installation.

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
  command -v ssh-keygen &>/dev/null || missing+=("ssh-keygen (OpenSSH 8.2+)")

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

# I pin the same public key as the updater; an archive cannot replace its trust root.
release_public_key() {
  printf '%s\n' 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBNmJEAj33/oXqXX/fA6C8JvQk1ju0Jb5qD7K99qo2pK gitbox-release'
}

get_release_info() {
  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT

  # Resolve latest once, before downloading any asset, to avoid mixing releases.
  if [[ -n "$VERSION_TAG" ]]; then
    RELEASE_TAG="$VERSION_TAG"
  elif [[ "$USE_GH" == true ]]; then
    RELEASE_TAG="$(gh release view --repo "$REPO" --json tagName -q .tagName)" \
      || die "Could not resolve the latest release."
  else
    curl -fsSL -o "$TMP_DIR/api.json" "${GITHUB_API}/repos/${REPO}/releases/latest" \
      || die "Could not resolve the latest release. Check the network or use --version."
    RELEASE_TAG="$(sed -n 's/.*"tag_name": *"\([^" ]*\)".*/\1/p' "$TMP_DIR/api.json" | head -1)"
  fi
  [[ "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] \
    || die "Invalid release tag: $RELEASE_TAG"

  log "Downloading $ARTIFACT_NAME and verification files ($RELEASE_TAG)..."
  local asset
  for asset in "$ARTIFACT_NAME" checksums.sha256 checksums.sha256.sig; do
    if [[ "$USE_GH" == true ]]; then
      gh release download "$RELEASE_TAG" --repo "$REPO" --pattern "$asset" --dir "$TMP_DIR" \
        || die "Release $RELEASE_TAG lacks a required download ($asset). Unsigned releases, including legacy 1.x, cannot be installed."
    else
      curl -fSL --progress-bar -o "$TMP_DIR/$asset" "https://github.com/${REPO}/releases/download/${RELEASE_TAG}/${asset}" \
        || die "Release $RELEASE_TAG lacks a required download ($asset). Unsigned releases, including legacy 1.x, cannot be installed."
    fi
  done
}

archive_sha256() {
  local archive="$TMP_DIR/$ARTIFACT_NAME"
  if command -v sha256sum &>/dev/null; then
    sha256sum "$archive" | cut -d ' ' -f1
  elif command -v shasum &>/dev/null; then
    shasum -a 256 "$archive" | cut -d ' ' -f1
  elif [[ "$PLATFORM" == windows ]] && command -v powershell &>/dev/null; then
    # PowerShell expands its own environment references.
    # shellcheck disable=SC2016
    GITBOX_VERIFY_ARCHIVE="$(cygpath -w "$archive")" powershell -NoProfile -NonInteractive -Command \
      '(Get-FileHash -LiteralPath $env:GITBOX_VERIFY_ARCHIVE -Algorithm SHA256 -ErrorAction Stop).Hash.ToLowerInvariant()' | tr -d '\r'
  else
    die "SHA-256 verification needs sha256sum, shasum, or Windows PowerShell."
  fi
}

verify_release() {
  command -v ssh-keygen &>/dev/null || die "Verification requires ssh-keygen from OpenSSH 8.2 or later."
  local asset
  for asset in "$ARTIFACT_NAME" checksums.sha256 checksums.sha256.sig; do
    [[ -s "$TMP_DIR/$asset" ]] || die "Missing verification input: $asset. Refusing to install an unsigned release."
  done
  printf 'gitbox-release namespaces="gitbox-release" %s\n' "$(release_public_key)" > "$TMP_DIR/allowed_signers"
  { printf 'gitbox %s\n' "$RELEASE_TAG"; cat "$TMP_DIR/checksums.sha256"; } > "$TMP_DIR/signed-message"
  ssh-keygen -Y verify -f "$TMP_DIR/allowed_signers" -I gitbox-release -n gitbox-release \
    -s "$TMP_DIR/checksums.sha256.sig" < "$TMP_DIR/signed-message" >/dev/null 2>&1 \
    || die "Release signature verification failed. Refusing to extract or install. OpenSSH 8.2+ is required."

  # Select exactly one well-formed entry; never let a checksum file select paths.
  local expected actual
  expected="$(awk -v name="$ARTIFACT_NAME" '
    $2 == name || $2 == "*" name {
      count++
      if (NF != 2 || length($1) != 64 || $1 ~ /[^0-9a-fA-F]/) bad = 1
      hash = tolower($1)
    }
    END { if (count != 1 || bad) exit 1; print hash }
  ' "$TMP_DIR/checksums.sha256")" || die "Signed checksums must contain exactly one SHA-256 entry for $ARTIFACT_NAME."
  actual="$(archive_sha256)" || die "Could not compute archive SHA-256."
  [[ "$actual" == "$expected" ]] || die "Archive checksum mismatch. Refusing to extract or install."
  log "Release signature and archive checksum verified."
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

# reg_value KEY NAME — prints a REG_SZ value, or nothing when the key or value
# is missing. MSYS_NO_PATHCONV stops Git Bash from rewriting /v into a path.
reg_value() {
  MSYS_NO_PATHCONV=1 reg.exe query "$1" /v "$2" 2>/dev/null \
    | tr -d '\r' | sed -n "s/^ *$2 *REG_SZ *//p" || true
}

# The setup exe installs into Program Files with a Start menu entry; bootstrap
# installs into INSTALL_DIR with none. They are alternatives, so warn when the
# installer's copy is present (the Start menu keeps launching it) or
# half-removed. Read-only: never runs that copy, whatever its version.
detect_windows_installer() {
  [[ "$PLATFORM" == "windows" ]] || return 0

  local uninstall='Software\Microsoft\Windows\CurrentVersion\Uninstall'
  local key="" loc="" ver="" k
  for k in "HKLM\\$uninstall\\$INNO_APP_ID" \
           "HKLM\\Software\\WOW6432Node\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\$INNO_APP_ID" \
           "HKCU\\$uninstall\\$INNO_APP_ID"; do
    loc="$(reg_value "$k" InstallLocation)"
    if [[ -n "$loc" ]]; then key="$k"; break; fi
  done
  [[ -n "$key" ]] || return 0

  ver="$(reg_value "$key" DisplayVersion)"
  loc="$(cygpath -u "$loc" 2>/dev/null || echo "$loc")"
  loc="${loc%/}"
  [[ "$loc" -ef "$INSTALL_DIR" ]] && return 0

  if [[ -e "$loc/GitboxApp.exe" ]]; then
    INSTALLER_COPY="$loc"
    warn "The Windows installer already installed gitbox ${ver:-(unknown version)} in $loc."
    warn "Its Start menu entry keeps launching that copy, not $INSTALL_DIR/GitboxApp.exe."
    warn "Keep one: run gitbox-win-amd64-setup.exe from the release instead of this"
    warn "script, or uninstall it (Settings → Apps → gitbox) and keep this copy."
  else
    local ps_key="${key/#HKLM/HKLM:}"
    ps_key="${ps_key/#HKCU/HKCU:}"
    warn "Found a half-removed Windows installer entry for gitbox ${ver:-} ($loc is gone)."
    warn "Remove the stale Start menu entry and uninstall key from an admin PowerShell:"
    echo "    Remove-Item -Recurse -Force 'C:\\ProgramData\\Microsoft\\Windows\\Start Menu\\Programs\\gitbox'"
    echo "    Remove-Item -Recurse -Force '$ps_key'"
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
  # The literal $PATH is written to the rc file and expands when it is sourced.
  # shellcheck disable=SC2016
  printf '\n%s\nexport PATH="%s:$PATH"\n' "$marker" "$dir" >> "$rc_file"
}

# ── macOS install ───────────────────────────────────────────────────

# I retain quarantine for Developer ID signatures (including notarized apps).
# Ad-hoc signatures only seal local code; they do not identify a publisher.
prepare_macos_binary() {
  local binary="$1" details
  command -v codesign &>/dev/null || die "codesign is required to inspect macOS signing."
  details="$(LC_ALL=C codesign -dv --verbose=2 "$binary" 2>&1)" || {
    [[ "$details" == *"code object is not signed at all"* ]] || die "Cannot inspect macOS signature: $binary"
  }
  if [[ "$details" == *"Authority="* ]]; then
    codesign --verify --deep --strict "$binary" || die "Invalid macOS code signature: $binary"
    return
  fi
  [[ "$details" == *"Signature=adhoc"* || "$details" == *"code object is not signed at all"* ]] \
    || die "Unrecognized macOS signing state: $binary"
  warn "This release has no Apple publisher signature; clearing quarantine after release verification."
  xattr -cr "$binary"
}

prepare_windows_binary() {
  local binary="$1"
  # Environment transport keeps paths out of PowerShell source code.
  # shellcheck disable=SC2016
  GITBOX_INSTALL_BINARY="$(cygpath -w "$binary")" powershell -NoProfile -NonInteractive -Command '
    $ErrorActionPreference = "Stop"
    $signature = Get-AuthenticodeSignature -LiteralPath $env:GITBOX_INSTALL_BINARY
    if ($signature.Status -eq "NotSigned") {
      Write-Host "[gitbox] This release has no Authenticode signature; unblocking after release verification."
      Unblock-File -LiteralPath $env:GITBOX_INSTALL_BINARY
    } elseif ($signature.Status -ne "Valid") {
      throw "Invalid Windows code signature: $($signature.Status)"
    }
  ' || die "Windows code-signature inspection failed."
}

install_macos() {
  # CLI (1.x archives only)
  if [[ -f "$TMP_DIR/extracted/gitbox" ]]; then
    mkdir -p "$INSTALL_DIR"
    prepare_macos_binary "$TMP_DIR/extracted/gitbox"
    cp "$TMP_DIR/extracted/gitbox" "$INSTALL_DIR/gitbox"
    chmod +x "$INSTALL_DIR/gitbox"
    CLI_INSTALLED=true
    log "CLI installed: $INSTALL_DIR/gitbox"
  fi

  # GUI
  if [[ "$CLI_ONLY" == false ]]; then
    prepare_macos_binary "$TMP_DIR/extracted/GitboxApp.app"
    rm -rf /Applications/GitboxApp.app
    cp -R "$TMP_DIR/extracted/GitboxApp.app" /Applications/GitboxApp.app
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
  local script_path="$TMP_DIR/extracted/register-gitbox.sh"
  local icon_path="$TMP_DIR/extracted/appicon.png"
  if [[ ! -f "$script_path" || ! -f "$icon_path" ]]; then
    warn "This verified release has no bundled desktop helper/icon; skipping menu entry."
    return
  fi
  if GITBOX_GUI_BIN="$INSTALL_DIR/GitboxApp" GITBOX_ICON_FILE="$icon_path" bash "$script_path"; then
    DESKTOP_REGISTERED=true
  else
    warn "Desktop registration failed; the verified application is installed."
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
    prepare_windows_binary "$TMP_DIR/extracted/gitbox.exe"
    cp "$TMP_DIR/extracted/gitbox.exe" "$INSTALL_DIR/gitbox.exe"
    CLI_INSTALLED=true
    log "CLI installed: $INSTALL_DIR/gitbox.exe"
  fi

  # GUI
  if [[ "$CLI_ONLY" == false ]]; then
    prepare_windows_binary "$TMP_DIR/extracted/GitboxApp.exe"
    cp "$TMP_DIR/extracted/GitboxApp.exe" "$INSTALL_DIR/GitboxApp.exe"
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
  if [[ -n "$INSTALLER_COPY" ]]; then
    warn "  Another copy from the Windows installer is still in $INSTALLER_COPY (see above)."
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
  verify_release
  extract_archive
  detect_existing_install
  detect_windows_installer

  case "$PLATFORM" in
    macos)   install_macos   ;;
    linux)   install_linux   ;;
    windows) install_windows ;;
  esac

  print_summary
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
