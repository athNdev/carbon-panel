#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
"""Integration checks for snapshot isolation and subpath publication."""
from pathlib import Path
import subprocess
import tempfile
import unittest
from scripts.build_website import export_snapshot, scoped_links
from scripts.check_website import Links


class WebsiteTests(unittest.TestCase):
    def test_snapshot_uses_committed_docs_and_examples_not_working_tree(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=root).decode().strip()
            git("init", "--quiet")
            for name in ("docs/carbon-panel/src/content/docs/intro.md",
                         "config.example.yaml", "docker-compose.yml", "buf.yaml", "buf.lock",
                         "proto/example.proto", "oidc/example/config.yml"):
                file = root / name
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_text("pinned", encoding="utf-8")
            git("add", ".")
            git("-c", "user.name=Website Test", "-c", "user.email=test@example.com",
                "commit", "--quiet", "-m", "snapshot")
            revision = git("rev-parse", "HEAD")
            (root / "config.example.yaml").write_text("unreleased", encoding="utf-8")
            (root / "docs/carbon-panel/src/content/docs/intro.md").write_text("unreleased", encoding="utf-8")
            export = root / "export"
            export.mkdir()
            export_snapshot(root, revision, export)
            self.assertEqual((export / "config.example.yaml").read_text(), "pinned")
            self.assertEqual((export / "docs/carbon-panel/src/content/docs/intro.md").read_text(), "pinned")

    def test_subpath_links_and_source_links(self):
        source = '[Config](/configuration/) <iframe src="/api-reference.html"></iframe> ' + \
                 '[External](https://example.com/) git clone https://example.com/repo ' + \
                 'https://github.com/athNdev/carbonpanel/blob/main/README.md'
        result = scoped_links(source, "/carbonpanel/docs/", "a" * 40)
        self.assertIn("](/carbonpanel/docs/configuration/)", result)
        self.assertIn('src="/carbonpanel/docs/api-reference.html"', result)
        self.assertIn("[External](https://example.com/)", result)
        self.assertIn("git clone https://example.com/repo", result)
        self.assertIn("/blob/" + "a" * 40 + "/README.md", result)
        external = "https://github.com/another/project/blob/main/README.md"
        self.assertEqual(scoped_links(external, "/docs/", "a" * 40), external)

    def test_floating_ref_is_rejected_before_archive(self):
        with self.assertRaises(ValueError):
            export_snapshot(Path("."), "main", Path("."))

    def test_not_found_ignores_only_its_synthetic_canonical(self):
        parser = Links(not_found=True)
        parser.feed('<link rel="canonical" href="/404/"><a href="/docs/">Docs</a>')
        self.assertEqual(parser.urls, ["/docs/"])


if __name__ == "__main__":
    unittest.main()
