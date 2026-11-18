#!/usr/bin/env bash
# Build a Debian package for FastV.
#
# usage: build-deb.sh <binary> <amd64|arm64> <version> <outdir> <repo-root>
#
# The package installs the binary under /opt/fastv, exposes a /usr/bin/fastv
# symlink and registers a desktop entry. Runtime dependencies are GTK3 and
# WebKitGTK 4.1, which the purego (CGO-free) Wails backend loads dynamically.
set -euo pipefail

BIN="${1:?binary path required}"
ARCH="${2:?architecture required}"
VERSION="${3:?version required}"
OUTDIR="${4:?output directory required}"
ROOT="${5:?repository root required}"

# Debian versions use a tilde for pre-release markers so that
# 0.6.0~pre.2 sorts before 0.6.0.
DEB_VERSION="${VERSION//-/\~}"
case "${ARCH}" in
  amd64|arm64) ;;
  *) echo "unsupported deb architecture: ${ARCH}" >&2; exit 1 ;;
esac

STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT

mkdir -p \
  "${STAGE}/DEBIAN" \
  "${STAGE}/opt/fastv" \
  "${STAGE}/usr/bin" \
  "${STAGE}/usr/share/applications" \
  "${STAGE}/usr/share/icons/hicolor/256x256/apps" \
  "${STAGE}/usr/share/doc/fastv"

install -m 0755 "${BIN}" "${STAGE}/opt/fastv/FastV"
ln -sf /opt/fastv/FastV "${STAGE}/usr/bin/fastv"
install -m 0644 "${ROOT}/assets/appicon.png" "${STAGE}/usr/share/icons/hicolor/256x256/apps/fastv.png"
install -m 0644 "${ROOT}/LICENSE" "${STAGE}/usr/share/doc/fastv/copyright"
install -m 0644 "${ROOT}/README.md" "${STAGE}/usr/share/doc/fastv/README.md"

cat > "${STAGE}/usr/share/applications/fastv.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=FastV
Comment=Xray proxy client
Exec=/opt/fastv/FastV
Icon=fastv
Terminal=false
Categories=Network;
DESKTOP

cat > "${STAGE}/DEBIAN/control" <<CONTROL
Package: fastv
Version: ${DEB_VERSION}
Section: net
Priority: optional
Architecture: ${ARCH}
Maintainer: FastV Maintainers <mardukprime@users.noreply.github.com>
Depends: libgtk-3-0, libwebkit2gtk-4.1-0
Installed-Size: $(du -sk "${STAGE}" | cut -f1)
Description: FastV - Xray proxy client
 A modern, lightweight desktop proxy client for Xray-core with
 subscriptions, routing rules, diagnostics and WebDAV backup.
CONTROL

cat > "${STAGE}/DEBIAN/postinst" <<'POSTINST'
#!/bin/sh
set -e
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications || true
fi
exit 0
POSTINST
chmod 0755 "${STAGE}/DEBIAN/postinst"

mkdir -p "${OUTDIR}"
OUT_FILE="${OUTDIR}/FastV-${VERSION}-linux-${ARCH}.deb"
dpkg-deb --root-owner-group --build "${STAGE}" "${OUT_FILE}"
echo "built ${OUT_FILE}"
