"""Deterministic in-memory media fixtures for the offline mock driver and tests.

Built with the standard library only (zlib + struct) so CI never needs network
access, Pillow, or a real Muse account. The bytes are byte-for-byte stable,
which lets tests assert a fixed SHA-256 digest.
"""

from __future__ import annotations

import base64
import struct
import zlib

PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"
MOCK_IMAGE_WIDTH = 8
MOCK_IMAGE_HEIGHT = 8


def _chunk(tag: bytes, payload: bytes) -> bytes:
    body = tag + payload
    return struct.pack(">I", len(payload)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)


def build_png_bytes(width: int = MOCK_IMAGE_WIDTH, height: int = MOCK_IMAGE_HEIGHT) -> bytes:
    """Return a valid, tiny RGBA PNG whose pixels depend only on width/height."""
    width = max(1, int(width))
    height = max(1, int(height))
    raw = bytearray()
    for y in range(height):
        raw.append(0)  # filter type: none
        for x in range(width):
            raw.extend((x * 31 % 256, y * 17 % 256, (x + y) * 7 % 256, 255))
    return (
        PNG_SIGNATURE
        + _chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
        + _chunk(b"IDAT", zlib.compress(bytes(raw), 9))
        + _chunk(b"IEND", b"")
    )


def mock_image_base64(width: int = MOCK_IMAGE_WIDTH, height: int = MOCK_IMAGE_HEIGHT) -> str:
    return base64.b64encode(build_png_bytes(width, height)).decode("ascii")


def build_video_bytes() -> bytes:
    """Opaque deterministic payload used as a transport fixture only.

    It is deliberately NOT a playable container: the mock driver proves that
    bytes, ownership and polling move through the stack, not that Muse emitted
    a valid MP4. Real decoding is only ever exercised by the live opt-in check.
    """
    return b"DS2API-MUSE-MOCK-VIDEO\x00" + struct.pack(">I", 1) + b"\x00" * 32


MOCK_PNG_BYTES = build_png_bytes()
MOCK_PNG_BASE64 = base64.b64encode(MOCK_PNG_BYTES).decode("ascii")
MOCK_VIDEO_BYTES = build_video_bytes()
