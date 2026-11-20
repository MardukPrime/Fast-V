#!/usr/bin/env python3
"""Verify that published release artifacts were built with production parameters.

Checks performed (all are expected to fail the release if not satisfied):

* every desktop binary carries the injected release version/commit;
* the Windows executable is a GUI-subsystem PE (built with -H=windowsgui);
* no debug/test ProductID suffix (e.g. com.mardukprime.fastv.mobile) leaked into
  any binary;
* macOS app bundle / Linux binary / Android APK+AAB carry the release version
  and the Android artifacts contain both arm64-v8a and x86_64 native libraries;
* checksums and expected asset names are consistent.

Usage: verify-release-artifacts.py <dist-dir> <version> <commit>
"""

from __future__ import annotations

import hashlib
import io
import os
import plistlib
import sys
import tarfile
import zipfile

PRODUCT_ID = b"com.mardukprime.fastv"
DEBUG_SUFFIX_MARKER = b"com.mardukprime.fastv."


def fail(message: str) -> None:
    print(f"[verify] FAIL: {message}", file=sys.stderr)
    raise SystemExit(1)


def require(condition: bool, message: str) -> None:
    if not condition:
        fail(message)


def check_markers(name: str, data: bytes, version: str, commit: str) -> None:
    require(version.encode() in data, f"{name}: release version {version!r} not embedded")
    require(commit.encode() in data, f"{name}: commit {commit!r} not embedded")
    require(PRODUCT_ID in data, f"{name}: FastV ProductID missing")
    require(
        DEBUG_SUFFIX_MARKER not in data,
        f"{name}: debug/test ProductID suffix found (non-production build)",
    )


def check_windows_exe(name: str, data: bytes, version: str, commit: str) -> None:
    check_markers(name, data, version, commit)
    require(data[:2] == b"MZ", f"{name}: not a PE executable")
    pe_offset = int.from_bytes(data[0x3C:0x40], "little")
    require(data[pe_offset:pe_offset + 4] == b"PE\0\0", f"{name}: invalid PE signature")
    optional_header = pe_offset + 24
    subsystem = int.from_bytes(data[optional_header + 68:optional_header + 70], "little")
    require(subsystem == 2, f"{name}: PE subsystem {subsystem}, expected 2 (WINDOWS_GUI)")


def check_macos_zip(path: str, version: str, commit: str) -> None:
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        info_name = next((n for n in names if n.endswith("Contents/Info.plist")), None)
        binary_name = next(
            (n for n in names if n.endswith(("Contents/MacOS/FastV", "Contents/MacOS/Fast-V"))),
            None,
        )
        require(info_name is not None, "macOS zip: Info.plist missing")
        require(binary_name is not None, "macOS zip: FastV binary missing")
        info = plistlib.loads(archive.read(info_name))
        bundle_version = str(info.get("CFBundleShortVersionString", ""))
        require(bundle_version == version, f"macOS Info.plist version {bundle_version!r} != {version!r}")
        check_markers(binary_name, archive.read(binary_name), version, commit)


def check_linux_tar(path: str, version: str, commit: str) -> None:
    with tarfile.open(path, "r:gz") as archive:
        member = next((m for m in archive.getmembers() if m.name in ("FastV", "./FastV")), None)
        require(member is not None, "Linux tar: FastV binary missing")
        data = archive.extractfile(member).read()  # type: ignore[union-attr]
        check_markers("linux FastV", data, version, commit)


def check_android(path: str, version: str, commit: str, label: str) -> None:
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        libs = [n for n in names if n.endswith("libwails.so")]
        require(any("arm64-v8a" in n for n in libs), f"{label}: arm64 libwails.so missing")
        require(any("x86_64" in n for n in libs), f"{label}: x86_64 libwails.so missing")
        require(not any("armeabi" in n or "armv7" in n for n in libs), f"{label}: unexpected 32-bit ABI")
        for name in libs:
            check_markers(f"{label}:{name}", archive.read(name), version, commit)


def main() -> None:
    if len(sys.argv) != 4:
        print(__doc__, file=sys.stderr)
        raise SystemExit(2)
    dist, version, commit = sys.argv[1], sys.argv[2], sys.argv[3]

    windows_zip = os.path.join(dist, f"FastV-{version}-windows-amd64.zip")
    windows_setup = os.path.join(dist, f"FastV-{version}-windows-amd64-setup.exe")
    mac_arm = os.path.join(dist, f"FastV-{version}-macos-arm64.zip")
    mac_amd = os.path.join(dist, f"FastV-{version}-macos-amd64.zip")
    linux_amd = os.path.join(dist, f"FastV-{version}-linux-amd64.tar.gz")
    linux_arm = os.path.join(dist, f"FastV-{version}-linux-arm64.tar.gz")
    apk = os.path.join(dist, f"FastV-{version}-android-universal.apk")
    aab = os.path.join(dist, f"FastV-{version}-android-universal.aab")

    for path in (windows_zip, windows_setup, mac_arm, mac_amd, linux_amd, linux_arm, apk, aab):
        require(os.path.isfile(path), f"missing release asset {os.path.basename(path)}")

    with zipfile.ZipFile(windows_zip) as archive:
        check_windows_exe("windows Fast-V.exe", archive.read("Fast-V.exe"), version, commit)
    with open(windows_setup, "rb") as handle:
        setup = handle.read()
    # The NSIS installer compresses the payload, so only check that it is a GUI
    # PE executable (the embedded app is verified through the portable zip).
    require(setup[:2] == b"MZ", "windows installer: not a PE executable")
    pe_offset = int.from_bytes(setup[0x3C:0x40], "little")
    require(setup[pe_offset:pe_offset + 4] == b"PE\0\0", "windows installer: invalid PE signature")
    optional_header = pe_offset + 24
    subsystem = int.from_bytes(setup[optional_header + 68:optional_header + 70], "little")
    require(subsystem == 2, f"windows installer: PE subsystem {subsystem}, expected 2")

    check_macos_zip(mac_arm, version, commit)
    check_macos_zip(mac_amd, version, commit)
    check_linux_tar(linux_amd, version, commit)
    check_linux_tar(linux_arm, version, commit)
    check_android(apk, version, commit, "Android APK")
    check_android(aab, version, commit, "Android AAB")

    checksums = os.path.join(dist, "SHA256SUMS.txt")
    require(os.path.isfile(checksums), "SHA256SUMS.txt missing")
    with open(checksums, "r", encoding="utf-8") as handle:
        lines = [line for line in handle.read().splitlines() if line.strip()]
    require(len(lines) >= 11, f"SHA256SUMS.txt has only {len(lines)} entries")
    for line in lines:
        digest, name = line.split(None, 1)
        target = os.path.join(dist, name.strip())
        require(os.path.isfile(target), f"checksum target missing: {name}")
        actual = hashlib.sha256(open(target, "rb").read()).hexdigest()
        require(actual == digest, f"checksum mismatch for {name}")

    print(f"[verify] OK: release {version} ({commit}) uses production build parameters")
    print("[verify] desktop binaries carry version/commit, Windows PE is GUI subsystem,")
    print("[verify] Android APK/AAB include arm64-v8a + x86_64, checksums match")


if __name__ == "__main__":
    main()
