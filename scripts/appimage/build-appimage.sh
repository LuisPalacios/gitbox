#!/usr/bin/env bash
# Build the self-contained gitbox AppImage from prebuilt Linux binaries.
#
# linuxdeploy copies GitboxApp and every shared library it links (GTK 3,
# WebKitGTK 4.1, JavaScriptCore, libsoup, ...) into the AppDir and its GTK
# plugin adds GLib schemas, pixbuf loaders, GIO modules and typelibs. This
# script adds the WebKitGTK helper processes, the AppStream metainfo and our
# own AppRun, then packs the AppDir with appimagetool. The result runs on a
# system that has none of those libraries installed, which is what the
# AppImage catalog (appimage.github.io) tests for.
#
# Usage:
#   scripts/appimage/build-appimage.sh <GitboxApp> <version> <out.AppImage>
#
# Needs an x86_64 Linux host with the GUI's runtime and dev libraries
# installed (libgtk-3-dev, libwebkit2gtk-4.1-dev, libgles2,
# gobject-introspection, libgirepository1.0-dev), plus wget, file,
# desktop-file-utils and appstream. CI runs it on ubuntu-22.04, the oldest
# supported base, so the bundled libraries need no newer glibc than 2.35;
# locally, run it inside a matching container to keep that baseline.
#
# APPIMAGE_TOOLS_DIR caches the tool downloads between runs (optional).
set -euo pipefail

usage() {
  sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}
[ $# -eq 3 ] || usage

gui="$(realpath "$1")"
version="${2#v}"
out="$3"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
desktop="$here/io.github.luispalacios.gitbox.desktop"
metainfo="$here/io.github.luispalacios.gitbox.appdata.xml"

[ "$(uname -m)" = x86_64 ] || { echo "error: build on an x86_64 Linux host" >&2; exit 1; }
for tool in wget file appstreamcli desktop-file-validate; do
  command -v "$tool" >/dev/null || { echo "error: $tool not found" >&2; exit 1; }
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
appdir="$work/AppDir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/metainfo"

# --- tools ------------------------------------------------------------------
# linuxdeploy, its GTK plugin and appimagetool from their "continuous"
# channels, like the rest of the AppImage ecosystem. The tool AppImages
# self-extract at run time so no FUSE is needed in CI runners or containers.
tools="${APPIMAGE_TOOLS_DIR:-$work/tools}"
mkdir -p "$tools"
fetch() {
  [ -s "$tools/$2" ] || wget -q "$1" -O "$tools/$2"
  chmod +x "$tools/$2"
}
fetch https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage linuxdeploy-x86_64.AppImage
fetch https://raw.githubusercontent.com/linuxdeploy/linuxdeploy-plugin-gtk/master/linuxdeploy-plugin-gtk.sh linuxdeploy-plugin-gtk.sh
fetch https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage appimagetool-x86_64.AppImage
export PATH="$tools:$PATH"
export APPIMAGE_EXTRACT_AND_RUN=1

# --- WebKitGTK helper processes --------------------------------------------
# libwebkit2gtk spawns them from a compiled-in absolute directory (no
# environment override in distro builds). Mirror that directory inside the
# AppDir (--deploy-deps-only makes linuxdeploy bundle their dependencies and
# set their rpath) and, after linuxdeploy has bundled the library, rewrite
# its "/usr" prefix to "././" (same length, so the ELF layout is untouched).
# AppRun then runs the app from AppDir/usr so the relative path resolves.
webkit_lib="$(ldconfig -p | awk '/libwebkit2gtk-4\.1\.so\.0 / {print $NF; exit}')"
webkit_src="$(dirname "$(realpath "$webkit_lib")")/webkit2gtk-4.1"
webkit_dst="$appdir/usr/lib/$(basename "$(dirname "$(realpath "$webkit_lib")")")/webkit2gtk-4.1"
mkdir -p "$webkit_dst/injected-bundle"
for f in WebKitWebProcess WebKitNetworkProcess WebKitGPUProcess; do
  [ -f "$webkit_src/$f" ] || { echo "error: $webkit_src/$f not found; install libwebkit2gtk-4.1-0" >&2; exit 1; }
  install -m 755 "$webkit_src/$f" "$webkit_dst/$f"
done
install -m 644 "$webkit_src/injected-bundle/libwebkit2gtkinjectedbundle.so" "$webkit_dst/injected-bundle/"

# WebKit's ANGLE backend dlopens libGLESv2 at run time, so linuxdeploy cannot
# see the dependency. The libglvnd dispatcher is small and not on the AppImage
# exclude list (unlike libGL/libEGL, which stay with the host's driver).
gles_lib="$(ldconfig -p | awk '/libGLESv2\.so\.2 / {print $NF; exit}')"
[ -n "$gles_lib" ] || { echo "error: libGLESv2.so.2 not found; install libgles2" >&2; exit 1; }

# --- static inputs ---------------------------------------------------------
sed -e "s/@VERSION@/$version/" -e "s/@DATE@/$(date -u +%F)/" "$metainfo" \
  > "$appdir/usr/share/metainfo/$(basename "$metainfo")"

# --- deploy -----------------------------------------------------------------
export ARCH=x86_64
export VERSION="$version"
export DEPLOY_GTK_VERSION=3
(
  cd "$work"
  linuxdeploy-x86_64.AppImage --appdir "$appdir" \
    --executable "$gui" \
\
    --deploy-deps-only "$webkit_dst" \
    --deploy-deps-only "$webkit_dst/injected-bundle" \
    --library "$gles_lib" \
    --desktop-file "$desktop" \
    --icon-file "$here/gitbox.png" \
    --plugin gtk
)

# "/usr/lib/..." becomes "././/lib/...": a double slash, but the same length.
sed -i -e 's|/usr|././|g' "$appdir/usr/lib/libwebkit2gtk-4.1.so.0"
install -m 755 "$here/AppRun" "$appdir/AppRun"
# Artifacts downloaded in CI lose their execute bit.
chmod 755 "$appdir/usr/bin/GitboxApp"

# --- verify -----------------------------------------------------------------
# Fail here rather than at a user's first launch.
for f in usr/bin/GitboxApp usr/lib/libwebkit2gtk-4.1.so.0 usr/lib/libgtk-3.so.0 \
         usr/lib/libGLESv2.so.2 "${webkit_dst#"$appdir/"}/WebKitWebProcess" \
         apprun-hooks/linuxdeploy-plugin-gtk.sh usr/share/glib-2.0/schemas/gschemas.compiled; do
  [ -e "$appdir/$f" ] || { echo "error: $f missing from AppDir" >&2; exit 1; }
done
grep -q -a -F '././/lib/x86_64-linux-gnu/webkit2gtk-4.1' "$appdir/usr/lib/libwebkit2gtk-4.1.so.0" \
  || { echo "error: libwebkit2gtk path patch did not apply" >&2; exit 1; }
desktop-file-validate "$appdir/"*.desktop
appstreamcli validate --no-net "$appdir/usr/share/metainfo/"*.xml

# --- pack -------------------------------------------------------------------
mkdir -p "$(dirname "$out")"
appimagetool-x86_64.AppImage "$appdir" "$out"
ls -lh "$out"
