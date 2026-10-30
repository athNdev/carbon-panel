#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
"""Build one website with docs and examples from an explicit application commit."""
import argparse
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parent.parent
SOURCE_PATHS = ("docs/carbon-panel/src/content/docs", "docker-compose.yml",
                "config.example.yaml", "oidc", "proto", "buf.yaml", "buf.lock")


def export_snapshot(root, revision, destination):
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("The documentation pin must be a full commit SHA.")
    archive = subprocess.check_output(
        ["git", "archive", "--format=tar", revision, "--", *SOURCE_PATHS], cwd=root)
    with tarfile.open(fileobj=io.BytesIO(archive)) as source:
        source.extractall(destination, filter="data")


def scoped_links(text, docs_base, revision):
    # Rebase prose and HTML links without changing shell commands or external URLs.
    text = re.sub(r"(\]\()/(?!/)", lambda m: m[1] + docs_base, text)
    text = re.sub(r"""((?:href|src)=["'])/(?!/)""", lambda m: m[1] + docs_base, text)
    return re.sub(
        r"(https://github.com/athNdev/(?:carbonpanel|carbon-panel)/(?:blob|tree)/)main/",
        lambda m: m[1] + revision + "/", text)



def publish_design_system(root, output):
    """Publish the current design-system guide and its canonical tokens."""
    source = root / "docs/design-system"
    destination = output / "design-system"
    destination.mkdir(parents=True, exist_ok=True)
    for name in ("site.css", "site.js"):
        shutil.copy2(source / name, destination / name)
    tokens = "web/carbon-panel/src/lib/design-system/tokens.css"
    shutil.copy2(root / tokens, destination / "tokens.css")
    page = (source / "index.html").read_text(encoding="utf-8")
    source_link = "../../" + tokens
    if page.count(source_link) != 2 or page.count("./README.md") != 2:
        raise ValueError("Design-system publication links no longer match the source page.")
    page = page.replace(source_link, "./tokens.css").replace(
        "./README.md", "https://github.com/athNdev/carbonpanel/blob/main/docs/design-system/README.md")
    (destination / "index.html").write_text(page, encoding="utf-8")


def build(root, base, site):
    if not base.startswith("/") or not base.endswith("/") or ".." in base:
        raise ValueError("The website base must be an absolute path ending in /.")
    pin = json.loads((root / "docs/website-source.json").read_text(encoding="utf-8"))
    docs_base = base + "docs/"
    output = root / ".website-build/site"
    # Verify the resolved generated path before replacing build output.
    generated_root = (root / ".website-build").resolve()
    if not generated_root.is_relative_to(root.resolve()) or not output.resolve().is_relative_to(generated_root):
        raise ValueError("Build output escaped its generated directory.")
    with tempfile.TemporaryDirectory(prefix="carbon-website-") as temporary:
        snapshot = Path(temporary)
        export_snapshot(root, pin["revision"], snapshot)
        project = snapshot / "docs/carbon-panel"
        shell = root / "docs/carbon-panel"
        # Current presentation; pinned authored content and relative MDX imports.
        for name in ("astro.config.mjs", "package.json", "package-lock.json", "tsconfig.json"):
            shutil.copy2(shell / name, project / name)
        for name in ("public", "src/styles", "src/components"):
            shutil.copytree(shell / name, project / name, dirs_exist_ok=True)
        shutil.copy2(shell / "src/content.config.ts", project / "src/content.config.ts")
        for name in ("index.mdx", "versions.mdx"):
            shutil.copy2(shell / "src/content/docs" / name, project / "src/content/docs" / name)
        shutil.copy2(root / "docs/website-source.json", snapshot / "docs/website-source.json")
        (project / "public/source.json").write_text(json.dumps(pin, indent=2) + "\n", encoding="utf-8")
        for page in (project / "src/content/docs").rglob("*"):
            if page.suffix in (".md", ".mdx"):
                page.write_text(scoped_links(page.read_text(encoding="utf-8"),
                                           docs_base, pin["revision"]), encoding="utf-8")
        api_page = project / "src/content/docs/api.mdx"
        api_page.write_text(api_page.read_text(encoding="utf-8").replace(
            "This spec is auto-generated from the protobuf definitions and stays in sync with the latest release.",
            "This API schema is generated from the application commit shown in the version banner."),
            encoding="utf-8")
        template = {"version": "v2", "plugins": [{
            "remote": "buf.build/community/sudorandom-connect-openapi:v0.21.3",
            "out": "docs/carbon-panel/public", "opt": ["path=schemav1.yaml"]}]}
        subprocess.run(["buf", "generate", "--template", json.dumps(template)],
                       cwd=snapshot, check=True)
        environment = {**os.environ, "DOCS_BASE_PATH": docs_base,
                       "WEBSITE_BASE_PATH": base, "WEBSITE_ORIGIN": site,
                       "ASTRO_TELEMETRY_DISABLED": "1"}
        npm = shutil.which("npm.cmd" if os.name == "nt" else "npm")
        if not npm:
            raise RuntimeError("Node.js and npm are required to build the website.")
        subprocess.run([npm, "ci", "--no-audit", "--no-fund"], cwd=project,
                       env=environment, check=True)
        subprocess.run([npm, "run", "build"], cwd=project, env=environment, check=True)
        if output.exists():
            shutil.rmtree(output)
        shutil.copytree(root / "landing", output, ignore=shutil.ignore_patterns("qa-*.html"))
        shutil.copytree(project / "dist", output / "docs")
        publish_design_system(root, output)
    namespace = "http://www.sitemaps.org/schemas/sitemap/0.9"
    ET.register_namespace("", namespace)
    sitemap = ET.Element(f"{{{namespace}}}urlset")
    for page in sorted(output.rglob("*.html")):
        if page.name == "404.html":
            continue
        route = page.relative_to(output).as_posix().removesuffix("index.html")
        url = ET.SubElement(sitemap, f"{{{namespace}}}url")
        ET.SubElement(url, f"{{{namespace}}}loc").text = site.rstrip("/") + base + route
    ET.ElementTree(sitemap).write(output / "sitemap.xml", encoding="utf-8", xml_declaration=True)
    print(f"Website built at {output}; docs pinned to {pin['revision']}", flush=True)
    return output


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default="/carbonpanel/")
    parser.add_argument("--site", default="https://athn.dev")
    args = parser.parse_args()
    build(ROOT, args.base, args.site)
