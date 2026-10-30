#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
"""Integration checks for snapshot isolation and subpath publication."""
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urlsplit
import subprocess
import tempfile
import unittest
from scripts.build_website import export_snapshot, publish_design_system, scoped_links
from scripts.check_website import Links


class DesignPageParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.ids = []
        self.fragments = []
        self.local_assets = []
        self.labels = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if "id" in attrs:
            self.ids.append(attrs["id"])
        if tag == "label" and attrs.get("for"):
            self.labels.append(attrs["for"])
        attr = "href" if tag in ("a", "link") else "src" if tag == "script" else None
        if not attr or not attrs.get(attr):
            return
        target = urlsplit(attrs[attr])
        if target.scheme or target.netloc:
            return
        if target.path:
            self.local_assets.append(target.path)
        elif target.fragment:
            self.fragments.append(target.fragment)


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


    def test_design_system_source_links_labels_and_fragments(self):
        page = Path(__file__).resolve().parent.parent / "docs/design-system/index.html"
        parser = DesignPageParser()
        parser.feed(page.read_text(encoding="utf-8"))
        self.assertEqual(len(parser.ids), len(set(parser.ids)), "Duplicate page IDs")
        for fragment in parser.fragments:
            self.assertIn(fragment, parser.ids, f"Missing section: {fragment}")
        for label in parser.labels:
            self.assertIn(label, parser.ids, f"Missing labelled control: {label}")
        for asset in parser.local_assets:
            self.assertTrue((page.parent / asset).is_file(), f"Missing local asset: {asset}")

    def test_design_system_is_published_with_current_canonical_tokens(self):
        root = Path(__file__).resolve().parent.parent
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            publish_design_system(root, output)
            design = output / "design-system"
            page = (design / "index.html").read_text(encoding="utf-8")
            self.assertIn('href="./tokens.css"', page)
            self.assertIn("github.com/athNdev/carbonpanel/blob/main/docs/design-system/README.md", page)
            self.assertNotIn("../../web/carbon-panel/", page)
            self.assertEqual(
                (design / "tokens.css").read_bytes(),
                (root / "web/carbon-panel/src/lib/design-system/tokens.css").read_bytes())
            self.assertTrue((design / "site.css").is_file())
            self.assertTrue((design / "site.js").is_file())
            links = Links()
            links.feed((root / "landing/index.html").read_text(encoding="utf-8"))
            self.assertIn("./design-system/", links.urls)

    def test_not_found_ignores_only_its_synthetic_canonical(self):
        parser = Links(not_found=True)
        parser.feed('<link rel="canonical" href="/404/"><a href="/docs/">Docs</a>')
        self.assertEqual(parser.urls, ["/docs/"])


if __name__ == "__main__":
    unittest.main()
