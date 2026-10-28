#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Regenerate the checked-in product icons from the approved SVG mark.

Requires Pillow 12.3.0. Pass --check to compare without changing files.
"""

import argparse
from io import BytesIO
from pathlib import Path
import re
import xml.etree.ElementTree as ET

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent.parent
SVG = ROOT / "assets/branding/mark-dark.svg"


def points():
    root = ET.parse(SVG).getroot()
    paths = root.findall(".//{http://www.w3.org/2000/svg}path")
    if len(paths) != 1:
        raise ValueError("Expected one approved symbol path")
    d = paths[0].attrib["d"]
    tokens = re.findall(r"[MLZ]|-?\d+(?:\.\d+)?", d)
    if "".join(tokens) != re.sub(r"[\s,]", "", d):
        raise ValueError("Unsupported symbol geometry")
    numbers = [float(x) for x in tokens if x not in ("M", "L", "Z")]
    return list(zip(numbers[::2], numbers[1::2]))


def render(size, *, light=True, background=True):
    factor = 4
    canvas = size * factor
    image = Image.new("RGBA", (canvas, canvas), (22, 22, 22, 255) if background else (0, 0, 0, 0))
    scale = canvas * 0.78 / 87.75
    xy = [(round(canvas / 2 + (x - 40.5) * scale),
           round(canvas / 2 + (y - 43.875) * scale)) for x, y in points()]
    ImageDraw.Draw(image).polygon(xy, fill=(244, 244, 244, 255) if light else (32, 36, 39, 255))
    image = image.resize((size, size), Image.Resampling.LANCZOS)
    output = BytesIO()
    image.save(output, format="PNG")
    return output.getvalue()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    outputs = {f"web/carbon-panel/static/g1_{n}x{n}.png": render(n)
               for n in (16, 20, 24, 32, 64, 128, 256, 512)}
    outputs.update({
        "web/carbon-panel/static/favicon.png": render(64),
        "docs/carbon-panel/public/favicon.png": render(64),
        "landing/assets/favicon.png": render(32),
        "web/carbon-panel/static/carbon_panel_logo.png": render(512),
        "docs/carbon-panel/public/carbon_panel_logo_transparent_512.png":
            render(512, light=False, background=False),
    })
    mismatches = []
    for relative, data in outputs.items():
        path = ROOT / relative
        if args.check:
            if not path.is_file() or path.read_bytes() != data:
                mismatches.append(relative)
        else:
            path.write_bytes(data)
    if mismatches:
        raise SystemExit("Icon mismatch: " + ", ".join(mismatches))
    print(f"{'Checked' if args.check else 'Generated'} {len(outputs)} brand icons.")


if __name__ == "__main__":
    main()
