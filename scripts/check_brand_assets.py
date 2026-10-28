#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Check that deployed copies of the approved Carbon Panel artwork match."""

from pathlib import Path
import struct
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parent.parent
MASTER = ROOT / "assets" / "branding"
SURFACES = ("landing/assets", "web/carbon-panel/static", "docs/carbon-panel/public")
NAMES = ("mark-dark.svg", "mark-light.svg", "horizontal-light.svg")


def main():
    errors = []
    for name in NAMES:
        source = (MASTER / name).read_bytes()
        ET.fromstring(source)
        for surface in SURFACES:
            target = ROOT / surface / ("brand-" + name)
            if not target.is_file() or target.read_bytes() != source:
                errors.append(str(target.relative_to(ROOT)))

    for first, second in (
        ("assets/logo.svg", "assets/branding/mark-dark.svg"),
        ("web/carbon-panel/src/lib/assets/favicon.svg", "web/carbon-panel/static/g1_22.svg"),
        ("landing/assets/favicon.png", "web/carbon-panel/static/g1_32x32.png"),
    ):
        if (ROOT / first).read_bytes() != (ROOT / second).read_bytes():
            errors.append(first)

    for relative, dimensions in (
        ("landing/assets/og.png", (1200, 630)),
        ("landing/assets/favicon.png", (32, 32)),
        ("web/carbon-panel/static/g1_16x16.png", (16, 16)),
        ("web/carbon-panel/static/g1_32x32.png", (32, 32)),
        ("web/carbon-panel/static/g1_64x64.png", (64, 64)),
        ("web/carbon-panel/static/g1_256x256.png", (256, 256)),
    ):
        data = (ROOT / relative).read_bytes()
        if data[:8] != b"\x89PNG\r\n\x1a\n" or struct.unpack(">II", data[16:24]) != dimensions:
            errors.append(relative)

    if errors:
        raise SystemExit("Brand asset mismatch: " + ", ".join(errors))
    print("Brand asset copies and image dimensions are consistent.")


if __name__ == "__main__":
    main()
