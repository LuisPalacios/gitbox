#!/usr/bin/env bash
#
# ship.sh — Ship gitbox CLI + GitboxApp GUI to remote hosts for smoke testing.
#
# Ships to every configured remote in .env (skipping the local machine) when
# called with no argument. With a single argument, matches it against the host
# short-name of one of the SSH_* vars (e.g. `ship.sh obelix` → SSH_MAC_INTEL_HOST).
#
# Artifacts staged at:
#   Unix remotes: /tmp/gitbox         /tmp/GitboxApp[.app]
#   Windows:      ~/gitbox.exe        ~/GitboxApp.exe
#
# CLI is cross-compiled locally (GOOS/GOARCH). GUI is built on the remote via
# tar | ssh | wails (no cross-compile path exists — each platform needs its
# own native webview). Per-host logs at /tmp/gitbox-ship-<platform>.log.
#
# Usage:
#   ./scripts/ship.sh              # all configured remotes, parallel
#   ./scripts/ship.sh obelix       # only the host matching short name 'obelix'
#   ./scripts/ship.sh -h           # help

# shellcheck source=_common.sh
source "$(dirname "$0")/_common.sh"

# ---------------------------------------------------------------------------
# Argument handling
# ---------------------------------------------------------------------------

arg="${1:-}"
if [[ "$arg" == "-h" || "$arg" == "--help" ]]; then
    grep '^#' "$0" | sed 's/^# \?//'
    exit 0
fi

# Map a user-supplied short name (e.g. "obelix", "bolica") to a platform token.
platform_from_shortname() {
    local want="$1"
    for p in $ALL_PLATFORMS; do
        local host; host="$(ssh_host_for "$p")"
        [[ -z "$host" ]] && continue
        local short="${host#*@}"    # strip user@
        short="${short%%.*}"        # strip trailing domain
        if [[ "$short" == "$want" || "$host" == "$want" ]]; then
            echo "$p"
            return 0
        fi
    done
    return 1
}

# ---------------------------------------------------------------------------
# Version stamping via -ldflags
# ---------------------------------------------------------------------------
# Remote hosts tar the source without `.git`, so the runtime `git describe`
# fallback in fullVersion() / GetAppVersion() returns empty and the binary
# reports "dev-none". Compute the version/commit once on the local side and
# pass them through -ldflags to both `go build` (CLI cross-compile) and the
# remote `wails build` (GUI). Matches the idiom used by CI.
_desc="$(git describe --tags --always 2>/dev/null || echo dev)"
_sha="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
LDFLAGS="-X main.version=${_desc}-dev -X main.commit=${_sha}"
unset _desc _sha

# ---------------------------------------------------------------------------
# Remote paths per platform
# ---------------------------------------------------------------------------

# Remote staged GUI bundle path.
remote_gui_for() {
    case "$1" in
        mac|mac-arm|mac-intel)  echo "/tmp/GitboxApp.app" ;;
        linux)                  echo "/tmp/GitboxApp" ;;
        win|win-intel|win-arm)  echo "~/GitboxApp.exe" ;;
    esac
}

# Where wails drops the GUI artifact on the remote, relative to cmd/gui.
wails_artifact_for() {
    case "$1" in
        mac|mac-arm|mac-intel)  echo "build/bin/GitboxApp.app" ;;
        linux)                  echo "build/bin/GitboxApp" ;;
        win|win-intel|win-arm)  echo "build/bin/GitboxApp.exe" ;;
    esac
}

# Wails -platform value for a platform token.
wails_target_for() {
    case "$1" in
        mac|mac-arm)     echo "darwin/arm64" ;;
        mac-intel)       echo "darwin/amd64" ;;
        linux)           echo "linux/amd64" ;;
        win|win-intel)   echo "windows/amd64" ;;
        win-arm)         echo "windows/arm64" ;;
    esac
}

# ---------------------------------------------------------------------------
# Build + ship helpers (always run on one platform at a time)
# ---------------------------------------------------------------------------

# 1. Cross-compile CLI locally for $platform.
# 2. SCP to remote.
build_and_ship_cli() {
    local p="$1"
    local goos goarch out remote host
    goos="$(platform_goos "$p")"
    goarch="$(platform_goarch "$p")"
    out="$(build_artifact "$p")"
    host="$(ssh_host_for "$p")"
    remote="$(remote_bin_for "$p")"

    mkdir -p "$BUILD_DIR"
    echo ">> go build $goos/$goarch → $out"
    GOOS="$goos" GOARCH="$goarch" go build -ldflags "$LDFLAGS" -o "$out" ./cmd/cli || return 1

    echo ">> scp $out $host:$remote"
    scp -q "$out" "$host:$remote" || return 1
    if ! is_win_platform "$p"; then
        ssh "$host" "chmod +x $remote" || return 1
    fi
    echo "CLI: $host:$remote"
}

# Ship source tar, run wails build on remote, stage artifact.
#
# set -e is partially disabled inside functions called in `cmd1 && cmd2`
# contexts, so we check every step's exit status explicitly. Silent
# continuation past a failed wails build produced success-looking ship
# runs with a missing /tmp/GitboxApp.app — not acceptable.
build_and_ship_gui() {
    local p="$1"
    local host; host="$(ssh_host_for "$p")"
    local target; target="$(wails_target_for "$p")"
    local artifact; artifact="$(wails_artifact_for "$p")"
    local staged; staged="$(remote_gui_for "$p")"
    local remote_dir="gitbox-remote-build"

    echo ">> prep remote scratch: $host:~/$remote_dir"
    ssh "$host" "rm -rf ~/$remote_dir && mkdir -p ~/$remote_dir" || return 1

    echo ">> tar source → ssh"
    (cd "$REPO_ROOT" && tar \
        --exclude="./.git" \
        --exclude="./build" \
        --exclude="./cmd/gui/build/bin" \
        --exclude="./cmd/gui/frontend/node_modules" \
        --exclude="./cmd/gui/frontend/dist" \
        --exclude="./.env" \
        -cf - .) | ssh "$host" "cd ~/$remote_dir && tar -xf -" || return 1

    # PATH covers: Apple Silicon Homebrew (/opt/homebrew/bin), Intel Homebrew
    # and Linux common prefix (/usr/local/bin), Linux Go default
    # (/usr/local/go/bin), per-user Go bin ($HOME/go/bin). Windows gets the
    # Scoop shim PATH fix as well.
    local path_prefix='/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$HOME/go/bin:$PATH'

    # Always pre-build the frontend directly via bash and pass -s to wails.
    # Motivation is Windows-specific — on hosts with a Scoop-installed Clink
    # AutoRun hook, wails's `npm install` (wrapped in cmd.exe /c by Go 1.22+
    # per CVE-2024-24576) hits "untrusted mount point" on scoop/apps/*/current
    # junctions and fails — but the same pre-build + -s flow works identically
    # on mac/linux, so we keep a single path across platforms.
    local pre_build="(cd cmd/gui/frontend && npm install && npm run build) && "
    # Single-quote the ldflags value so the remote shell groups it as one arg
    # after bash expands $LDFLAGS into the outer double-quoted command string.
    local wails_flags="-s -ldflags '$LDFLAGS' -platform $target"

    # Linux: Wails v2.9+ uses WebKitGTK 4.1 via the webkit2_41 build tag.
    # Ubuntu 24.04+ ships libwebkit2gtk-4.1-dev; without the tag, the Go
    # build fails with "Package webkit2gtk-4.0 not found".
    if [[ "$p" == "linux" ]]; then
        wails_flags="$wails_flags -tags webkit2_41"
    fi

    local build_cmd="export PATH=$path_prefix && cd ~/$remote_dir && \
        mkdir -p cmd/gui/build && \
        { [ -f assets/appicon.png ] && cp assets/appicon.png cmd/gui/build/appicon.png || true; } && \
        { [ -f assets/icon.ico ] && mkdir -p cmd/gui/build/windows && cp assets/icon.ico cmd/gui/build/windows/icon.ico || true; } && \
        ${pre_build}cd cmd/gui && wails build $wails_flags"
    if is_win_platform "$p"; then
        build_cmd="$(_win_scoop_path_prefix) $build_cmd"
    fi

    echo ">> wails build -platform $target (remote)"
    if ! ssh "$host" "$build_cmd"; then
        echo "!! wails build failed on $host — see output above" >&2
        return 1
    fi

    # Stage at the conventional /tmp (Unix) or ~ (Windows) location.
    echo ">> stage → $host:$staged"
    if is_win_platform "$p"; then
        ssh "$host" "cp ~/$remote_dir/cmd/gui/$artifact $staged" || return 1
    elif [[ "$artifact" == *".app" ]]; then
        ssh "$host" "rm -rf $staged && cp -R ~/$remote_dir/cmd/gui/$artifact $staged && \
            xattr -dr com.apple.quarantine $staged 2>/dev/null || true" || return 1
    else
        ssh "$host" "cp ~/$remote_dir/cmd/gui/$artifact $staged && chmod +x $staged" || return 1
    fi
    echo "GUI: $host:$staged"
}

# Do one complete ship (CLI + GUI) for a single platform; capture stdout+stderr
# to a per-host log and record a status file for the summary phase.
ship_one() {
    local p="$1"
    local log="/tmp/gitbox-ship-$p.log"
    local status_file="/tmp/gitbox-ship-$p.status"
    : > "$log"
    rm -f "$status_file"

    if { build_and_ship_cli "$p" && build_and_ship_gui "$p"; } >> "$log" 2>&1; then
        grep -E '^(CLI|GUI): ' "$log" > "$status_file.ok" || true
        echo "ok" > "$status_file"
    else
        echo "fail" > "$status_file"
    fi
}

# ---------------------------------------------------------------------------
# Target resolution
# ---------------------------------------------------------------------------

targets=""
if [[ -z "$arg" ]]; then
    for p in $ALL_PLATFORMS; do
        [[ "$p" == "$LOCAL_OS" ]] && continue          # skip local
        [[ -z "$(ssh_host_for "$p")" ]] && continue    # skip unconfigured
        targets="$targets $p"
    done
    if [[ -z "$targets" ]]; then
        die "no remote hosts configured in .env"
    fi
else
    p="$(platform_from_shortname "$arg" || true)"
    if [[ -z "$p" ]]; then
        configured=""
        for pp in $ALL_PLATFORMS; do
            h="$(ssh_host_for "$pp")"
            [[ -n "$h" ]] && configured="$configured $h"
        done
        die "no host matching '$arg' (.env has:$configured )"
    fi
    targets="$p"
fi

# ---------------------------------------------------------------------------
# Ship — parallel by default, one process per target.
# ---------------------------------------------------------------------------

header "Shipping:$targets"

pids=()
for p in $targets; do
    info "$(platform_label "$p") → $(ssh_host_for "$p") [log: /tmp/gitbox-ship-$p.log]"
    ship_one "$p" &
    pids+=($!)
done
for pid in "${pids[@]}"; do
    wait "$pid" || true
done

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

header "Summary"
fails=0
any_mac=0
for p in $targets; do
    status_file="/tmp/gitbox-ship-$p.status"
    status="$(cat "$status_file" 2>/dev/null || echo unknown)"
    if [[ "$status" == "ok" ]]; then
        ok "$(platform_label "$p")"
        if [[ -f "$status_file.ok" ]]; then
            sed 's/^/        /' "$status_file.ok"
        fi
        case "$p" in
            mac|mac-arm|mac-intel) any_mac=1 ;;
        esac
    else
        fail "$(platform_label "$p") — see /tmp/gitbox-ship-$p.log"
        tail -10 "/tmp/gitbox-ship-$p.log" | sed 's/^/        /'
        fails=$((fails + 1))
    fi
done

# macOS post-ship reminder. The app was dropped at /tmp/GitboxApp.app, but
# macOS's Local Network TCC permission is keyed on code-signing identity +
# bundle path — and a binary at /tmp with no signature is effectively a new
# app on every ship, so TCC silently denies the first LAN connect instead of
# prompting. The user has to do a one-time move-and-ad-hoc-sign dance,
# otherwise the GUI will show "Offline" / "no route to host" for any account
# whose server is on a LAN (192.168.x.x, 10.x.x.x, .local).
if [[ $any_mac -eq 1 ]]; then
    header "macOS post-ship — Local Network permission"
    cat <<'EOF'

  macOS gates LAN access per app. An app launched ad-hoc from /tmp with no
  signature never triggers the "Allow Local Network" prompt, so the GUI will
  look offline for LAN servers. One-time fix per target host, as the user
  who will run the GUI (NOT as root):

      # 1. Quit GitboxApp fully if it is running (⌘Q, not just close-window).
      rm -rf /Applications/GitboxApp.app
      cp -R /tmp/GitboxApp.app /Applications/GitboxApp.app
      xattr -dr com.apple.quarantine /Applications/GitboxApp.app
      codesign --force --deep --sign - /Applications/GitboxApp.app
      # 2. Try to forget any stale TCC decisions for this identifier.
      tccutil reset LocalNetwork com.wails.GitboxApp 2>/dev/null || true
      open /Applications/GitboxApp.app

  - The ad-hoc `codesign --sign -` gives the bundle a stable identity TCC
    can track. It's NOT an Apple Developer signature; no account is needed.
  - `tccutil reset ...` is best-effort — it fails on some macOS versions
    unless Terminal has Full Disk Access. That's fine: either the reset
    works and the OS re-prompts on first LAN access, or it doesn't and the
    existing decision persists. If the prompt never appears, open
    System Settings → Privacy & Security → Local Network and toggle
    GitboxApp on manually.
  - After approving once, future ships to the same host can skip this dance
    AS LONG AS the app stays at /Applications/GitboxApp.app with the same
    ad-hoc signature. You still need steps 1 and 2 on every fresh ship
    because cp + codesign rewrites the bundle hash.
  - Full walkthrough including Windows Firewall and Linux equivalents:
    https://github.com/LuisPalacios/gitbox/blob/main/docs/credentials.md#troubleshooting

EOF
fi

[[ $fails -eq 0 ]] || exit 1
