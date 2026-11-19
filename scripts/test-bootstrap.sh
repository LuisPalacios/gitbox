#!/usr/bin/env bash
# I exercise bootstrap verification with local fixtures and a throwaway signing key.
# Test doubles replace functions called by the sourced installer.
# shellcheck disable=SC2329
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
# shellcheck source=scripts/bootstrap.sh
source scripts/bootstrap.sh

fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
ARTIFACT_NAME=gitbox-linux-amd64.zip
RELEASE_TAG=v9.9.9
PLATFORM=linux
TMP_DIR="$fixture/release"
mkdir -p "$TMP_DIR"
[[ "$(release_public_key)" == "$(cat pkg/update/release-signing-key.pub)" ]] || die "Bootstrap and updater trust roots differ."
ssh-keygen -q -t ed25519 -N '' -f "$fixture/key"
# I substitute the test trust root only after sourcing; the installer has no key override.
release_public_key() { cat "$fixture/key.pub"; }
printf 'archive fixture\n' > "$TMP_DIR/$ARTIFACT_NAME"

sign_checksums() {
  rm -f "$fixture/message.sig"
  { printf 'gitbox %s\n' "$RELEASE_TAG"; cat "$TMP_DIR/checksums.sha256"; } > "$fixture/message"
  ssh-keygen -Y sign -f "$fixture/key" -n gitbox-release "$fixture/message" >/dev/null 2>&1
  cp "$fixture/message.sig" "$TMP_DIR/checksums.sha256.sig"
}

reset_release() {
  printf 'archive fixture\n' > "$TMP_DIR/$ARTIFACT_NAME"
  printf '%s  %s\n' "$(archive_sha256)" "$ARTIFACT_NAME" > "$TMP_DIR/checksums.sha256"
  sign_checksums
}

expect_failure() {
  local name="$1" expected="$2"; shift 2
  if ( "$@" ) > "$fixture/failure.log" 2>&1; then
    die "$name unexpectedly passed"
  fi
  grep -qF "$expected" "$fixture/failure.log" || { cat "$fixture/failure.log"; die "$name failed for the wrong reason"; }
}

reset_release
verify_release
printf 'tampered\n' >> "$TMP_DIR/$ARTIFACT_NAME"
expect_failure tampered-archive 'checksum mismatch' verify_release
reset_release
printf 'tampered\n' >> "$TMP_DIR/checksums.sha256"
expect_failure tampered-checksums 'signature verification failed' verify_release
reset_release
RELEASE_TAG=v9.9.8
expect_failure replayed-tag 'signature verification failed' verify_release
RELEASE_TAG=v9.9.9
reset_release
cat "$TMP_DIR/checksums.sha256" >> "$fixture/duplicate"
cat "$fixture/duplicate" >> "$TMP_DIR/checksums.sha256"
sign_checksums
expect_failure duplicate-entry 'exactly one SHA-256 entry' verify_release
printf '%064d  unrelated.zip\n' 0 > "$TMP_DIR/checksums.sha256"
sign_checksums
expect_failure missing-entry 'exactly one SHA-256 entry' verify_release
reset_release
rm "$TMP_DIR/checksums.sha256.sig"
expect_failure unsigned-release 'Missing verification input' verify_release
reset_release
(
  release_public_key() { cat pkg/update/release-signing-key.pub; }
  expect_failure wrong-key 'signature verification failed' verify_release
)
(
  command() {
    if [[ "$*" == '-v ssh-keygen' ]]; then return 1; fi
    builtin command "$@"
  }
  expect_failure missing-keygen 'Verification requires ssh-keygen' verify_release
)
ssh-keygen() { return 1; }
expect_failure unsupported-keygen 'signature verification failed' verify_release
unset -f ssh-keygen

# A failed verification must stop main before extraction or any install action.
reset_release
printf 'tampered\n' >> "$TMP_DIR/$ARTIFACT_NAME"
(
  parse_args() { :; }
  detect_platform() { :; }
  check_dependencies() { :; }
  detect_headless() { :; }
  resolve_cli_version() { :; }
  get_release_info() { :; }
  extract_archive() { touch "$fixture/extracted"; }
  install_linux() { touch "$fixture/installed"; }
  expect_failure install-order 'checksum mismatch' main
)
[[ ! -e "$fixture/extracted" && ! -e "$fixture/installed" ]] || die "Unverified content reached installation."

# Both download paths use the resolved tag for all three assets.
reset_release
(
  local_release="$TMP_DIR"
  gh() {
    if [[ "$1 $2" == 'release view' ]]; then printf '%s\n' v9.9.9; return; fi
    [[ "$1 $2 $3" == 'release download v9.9.9' ]] || return 1
    cp "$local_release/$7" "$9/$7"
  }
  USE_GH=true
  VERSION_TAG=''
  get_release_info
  verify_release
)
(
  local_release="$TMP_DIR"
  curl() {
    if [[ "$*" == *'/releases/latest'* ]]; then
      printf '{"tag_name": "v9.9.9"}\n' > "$3"
    else
      [[ "$5" == "https://github.com/$REPO/releases/download/v9.9.9/"* ]] || return 1
      cp "$local_release/${5##*/}" "$4"
    fi
  }
  USE_GH=false
  VERSION_TAG=''
  get_release_info
  verify_release
)

# A bundled helper and icon work offline; a legacy archive never fetches code.
mkdir -p "$TMP_DIR/extracted"
cat > "$TMP_DIR/extracted/register-gitbox.sh" <<'HELPER'
[[ -f "$GITBOX_ICON_FILE" && "$GITBOX_GUI_BIN" == */GitboxApp ]] || exit 1
HELPER
printf 'icon fixture\n' > "$TMP_DIR/extracted/appicon.png"
register_linux_desktop
[[ "$DESKTOP_REGISTERED" == true ]] || die "Bundled helper did not run."
rm "$TMP_DIR/extracted/register-gitbox.sh"
DESKTOP_REGISTERED=false
register_linux_desktop
[[ "$DESKTOP_REGISTERED" == false ]] || die "Missing helper reported success."

# I stub platform tools to prove signed/invalid code never loses quarantine.
(
  codesign() {
    if [[ "$1" == -dv ]]; then printf '%s\n' 'Authority=Developer ID Application: Fixture'; return; fi
    [[ "$1" == --verify ]]
  }
  xattr() { touch "$fixture/quarantine-cleared"; }
  prepare_macos_binary "$fixture/app"
  [[ ! -e "$fixture/quarantine-cleared" ]] || die "Signed app lost quarantine."
  codesign() {
    if [[ "$1" == -dv ]]; then printf '%s\n' 'Authority=Developer ID Application: Fixture'; else return 1; fi
  }
  expect_failure invalid-macos-signature 'Invalid macOS code signature' prepare_macos_binary "$fixture/app"
  [[ ! -e "$fixture/quarantine-cleared" ]] || die "Invalid signed app lost quarantine."
  codesign() { printf '%s\n' 'Signature=adhoc'; }
  prepare_macos_binary "$fixture/app"
  [[ -e "$fixture/quarantine-cleared" ]] || die "Unsigned app kept quarantine."
)
# On macOS I also inspect a copied Apple-signed executable with native tools.
if [[ "$(uname -s)" == Darwin ]]; then
  cp /usr/bin/true "$fixture/native-binary"
  xattr -w com.apple.quarantine '0081;00000000;gitbox-fixture;' "$fixture/native-binary"
  prepare_macos_binary "$fixture/native-binary"
  xattr -p com.apple.quarantine "$fixture/native-binary" >/dev/null \
    || die "Native signed executable lost quarantine."
  cp "$fixture/native-binary" "$fixture/native-invalid"
  printf 'tampered' >> "$fixture/native-invalid"
  expect_failure native-invalid-signature 'Invalid macOS code signature' prepare_macos_binary "$fixture/native-invalid"
  codesign --force --sign - "$fixture/native-binary" >/dev/null 2>&1
  prepare_macos_binary "$fixture/native-binary"
  if xattr -p com.apple.quarantine "$fixture/native-binary" >/dev/null 2>&1; then
    die "Native ad-hoc executable kept quarantine."
  fi
fi

# On Windows I execute the actual PowerShell policy with cmdlet test doubles.
if command -v powershell &>/dev/null; then
  (
    native_powershell="$(command -v powershell)"
    export GITBOX_TEST_MARKER
    GITBOX_TEST_MARKER="$(cygpath -w "$fixture/unblocked")"
    powershell() {
      local script="${!#}"
      # PowerShell, not Bash, expands these variables.
      # shellcheck disable=SC2016
      "$native_powershell" -NoProfile -NonInteractive -Command '
        function Get-AuthenticodeSignature { [pscustomobject]@{Status=$env:GITBOX_TEST_SIGNATURE} }
        function Unblock-File { Set-Content -LiteralPath $env:GITBOX_TEST_MARKER -Value unblocked }
      '"$script"
    }
    export GITBOX_TEST_SIGNATURE=Valid
    prepare_windows_binary "$fixture/app.exe"
    [[ ! -e "$fixture/unblocked" ]] || die "Signed Windows binary was unblocked."
    export GITBOX_TEST_SIGNATURE=HashMismatch
    expect_failure invalid-windows-signature 'Windows code-signature inspection failed' prepare_windows_binary "$fixture/app.exe"
    [[ ! -e "$fixture/unblocked" ]] || die "Invalid Windows binary was unblocked."
    export GITBOX_TEST_SIGNATURE=NotSigned
    prepare_windows_binary "$fixture/app.exe"
    [[ -e "$fixture/unblocked" ]] || die "Unsigned Windows binary was not unblocked."
    unset -f powershell
    PLATFORM=windows
    expected_hash="$(archive_sha256)"
    command() {
      if [[ "$*" == '-v sha256sum' || "$*" == '-v shasum' ]]; then return 1; fi
      builtin command "$@"
    }
    [[ "$(archive_sha256)" == "$expected_hash" ]] || die "PowerShell SHA-256 fallback differs."
  )
fi
printf 'Bootstrap verification fixtures passed.\n'
