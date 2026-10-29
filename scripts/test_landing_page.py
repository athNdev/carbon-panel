#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Regression checks for the standalone landing page."""

from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urlsplit
import unittest


class InstallCommandParser(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.copy_targets = []
        self.command_parts = []
        self.command_depth = 0

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "button" and attrs.get("aria-label") == "Copy install command":
            self.copy_targets.append(attrs.get("data-copy"))
        if self.command_depth:
            self.command_depth += 1
        elif attrs.get("id") == "install-cmd":
            self.command_depth = 1

    def handle_endtag(self, tag):
        if self.command_depth:
            self.command_depth -= 1

    def handle_data(self, data):
        if self.command_depth:
            self.command_parts.append(data)


class PreviewMetadataParser(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.metadata = {}

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "meta":
            key = attrs.get("property", attrs.get("name", ""))
            self.metadata[key] = attrs.get("content", "")


class LandingPageTests(unittest.TestCase):
    def test_copy_target_contains_a_runnable_command_without_the_prompt(self):
        page = Path(__file__).resolve().parent.parent / "landing" / "index.html"
        parser = InstallCommandParser()
        parser.feed(page.read_text(encoding="utf-8"))
        self.assertEqual(parser.copy_targets, ["install-cmd"])
        self.assertEqual(
            "".join(parser.command_parts).strip(),
            "git clone https://github.com/athNdev/carbonpanel && cd carbonpanel",
        )

    def test_social_previews_have_no_em_dash_and_share_a_real_image(self):
        root = Path(__file__).resolve().parent.parent
        parser = PreviewMetadataParser()
        parser.feed((root / "landing/index.html").read_text(encoding="utf-8"))
        metadata = parser.metadata
        for key in ("og:title", "og:description", "og:image:alt",
                    "twitter:title", "twitter:description", "twitter:image:alt"):
            self.assertTrue(metadata[key], key)
            self.assertNotIn(chr(0x2014), metadata[key], key)
        self.assertEqual(metadata["og:image"], metadata["twitter:image"])
        image = urlsplit(metadata["og:image"])
        self.assertTrue(image.query, "A replaced preview needs a cache version")
        self.assertTrue((root / "landing" / image.path.removeprefix("/carbonpanel/")).is_file())


if __name__ == "__main__":
    unittest.main()
