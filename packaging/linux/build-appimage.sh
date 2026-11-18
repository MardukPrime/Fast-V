#!/usr/bin/env bash
# Build a Linux AppImage for FastV (amd64).
#
# usage: build-appimage.sh <binary> <appimagetool> <version> <outdir> <repo-root>
#
# The AppImage bundles only FastV itself; WebKitGTK/GTK are expected from the
# system (the Wails purego backend loads them dynamically), exactly like the
# portable tar.gz and the .deb package.
set -euo pipefail

BIN="${1:?binary path required}"
TOOL="${2:?appimagetool path required}"
VERSION="${3:?version required}"
OUTDIR="${4:?output directory required}"
ROOT="${5:?repository root required}"

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT
APPDIR="${WORK}/FastV.AppDir"

mkdir -p \
  "${APPDIR}/usr/bin" \
  "${APPDIR}/usr/share/applications" \
  "${APPDIR}/usr/share/icons/hicolor/256x256/apps"

install -m 0755 "${BIN}" "${APPDIR}/usr/bin/FastV"
install -m 0644 "${ROOT}/assets/appicon.png" "${APPDIR}/usr/share/icons/hicolor/256x256/apps/fastv.png"
install -m 0644 "${ROOT}/assets/appicon.png" "${APPDIR}/fastv.png"
install -m 0644 "${ROOT}/LICENSE" "${APPDIR}/LICENSE"

cat > "${APPDIR}/fastv.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=FastV
Comment=Xray proxy client
Exec=FastV
Icon=fastv
Terminal=false
Categories=Network;
DESKTOP

cat > "${APPDIR}/AppRun" <<'APPRUN'
#!/bin/sh
APPDIR="$(dirname "$(readlink -f "$0")")"
exec "$APPDIR/usr/bin/FastV" "$@"
APPRUN
chmod 0755 "${APPDIR}/AppRun"

mkdir -p "${OUTDIR}"
OUT_FILE="${OUTDIR}/FastV-${VERSION}-linux-amd64.AppImage"
chmod +x "${TOOL}"
ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 "${TOOL}" --no-appstream "${APPDIR}" "${OUT_FILE}"
chmod 0755 "${OUT_FILE}"
echo "built ${OUT_FILE}"
