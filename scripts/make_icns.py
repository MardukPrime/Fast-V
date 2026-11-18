#!/usr/bin/env python3
"""Generate a macOS .icns icon from a square PNG.

Uses the modern PNG-based ICNS layout (types icp4/icp5/icp6/ic07..ic10), which
macOS 10.7+ reads natively. Run via:

    python scripts/make_icns.py assets/appicon.png assets/appicon.icns
"""

import io
import struct
import sys

from PIL import Image

# (icns type code, pixel size)
ICNS_SLOTS = [
    (b"icp4", 16),
    (b"icp5", 32),
    (b"icp6", 64),
    (b"ic07", 128),
    (b"ic08", 256),
    (b"ic09", 512),
    (b"ic10", 1024),
]


def build_icns(source: str) -> bytes:
    image = Image.open(source).convert("RGBA")
    if image.width != image.height:
        side = min(image.width, image.height)
        offset = ((image.width - side) // 2, (image.height - side) // 2)
        image = image.crop((offset[0], offset[1], offset[0] + side, offset[1] + side))
    entries = []
    for code, size in ICNS_SLOTS:
        if size > image.width:
            continue
        resized = image.resize((size, size), Image.LANCZOS)
        buffer = io.BytesIO()
        resized.save(buffer, format="PNG", optimize=True)
        payload = buffer.getvalue()
        entries.append(code + struct.pack(">I", 8 + len(payload)) + payload)
    body = b"".join(entries)
    return b"icns" + struct.pack(">I", 8 + len(body)) + body


def main() -> int:
    source = sys.argv[1] if len(sys.argv) > 1 else "assets/appicon.png"
    target = sys.argv[2] if len(sys.argv) > 2 else "assets/appicon.icns"
    data = build_icns(source)
    with open(target, "wb") as handle:
        handle.write(data)
    print(f"wrote {target} ({len(data)} bytes)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
