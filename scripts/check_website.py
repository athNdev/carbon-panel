#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
"""Reject broken local routes and assets in the assembled website."""
import argparse
from html.parser import HTMLParser
import json
from pathlib import Path
from urllib.parse import unquote, urljoin, urlsplit


class Links(HTMLParser):
    def __init__(self, not_found=False):
        super().__init__()
        self.urls = []
        self.not_found = not_found

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        # Starlight's synthetic 404 has a canonical /404/ URL but is emitted as
        # 404.html. It is not a navigable content route.
        if self.not_found and tag == "link" and attrs.get("rel") == "canonical":
            return
        for name in ("href", "src"):
            if attrs.get(name):
                self.urls.append(attrs[name])


def check(root, base="/carbonpanel/", site="https://athn.dev"):
    errors = []
    pages = list(root.rglob("*.html"))
    for page in pages:
        parser = Links(not_found=page.name == "404.html")
        parser.feed(page.read_text(encoding="utf-8"))
        page_url = site + base + page.relative_to(root).as_posix()
        for link in parser.urls:
            target = urlsplit(urljoin(page_url, link))
            if target.netloc != urlsplit(site).netloc or target.scheme not in ("http", "https"):
                continue
            if not target.path.startswith(base):
                errors.append(f"{page.relative_to(root)}: outside website base: {link}")
                continue
            path = root / unquote(target.path[len(base):])
            if path.is_dir():
                path = path / "index.html"
            if not path.is_file():
                errors.append(f"{page.relative_to(root)}: missing {link}")
    if errors:
        raise ValueError("\n".join(sorted(set(errors))))
    pin = json.loads((root / "docs/source.json").read_text(encoding="utf-8"))
    assert len(pin["revision"]) == 40
    assert (root / "docs/schemav1.yaml").stat().st_size > 1000
    assert not list(root.glob("qa-*.html")), "QA harnesses must not be published"
    print(f"Website routes and assets valid across {len(pages)} HTML pages.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path, nargs="?", default=Path(".website-build/site"))
    parser.add_argument("--base", default="/carbonpanel/")
    parser.add_argument("--site", default="https://athn.dev")
    args = parser.parse_args()
    check(args.root, args.base, args.site)
