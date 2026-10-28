#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Copy the approved vector masters into the independently built sites."""

from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MASTER = ROOT / "assets" / "branding"
SURFACES = ("landing/assets", "web/carbon-panel/static", "docs/carbon-panel/public")
NAMES = ("mark-dark.svg", "mark-light.svg", "horizontal-light.svg")


def main():
    for name in NAMES:
        source = (MASTER / name).read_bytes()
        for surface in SURFACES:
            (ROOT / surface / ("brand-" + name)).write_bytes(source)
    (ROOT / "assets/logo.svg").write_bytes((MASTER / "mark-dark.svg").read_bytes())
    print("Synchronized approved vector artwork.")


if __name__ == "__main__":
    main()
