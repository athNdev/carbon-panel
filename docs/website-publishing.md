<!-- SPDX-License-Identifier: MPL-2.0 -->
# Website and pinned documentation

The Pages deployment contains the marketing homepage at `/carbonpanel/` and
Starlight documentation at `/carbonpanel/docs/`. Only
`.github/workflows/landing.yml` publishes Pages. The docs workflow validates the
combined site on pull requests; it cannot replace the homepage.

## Source contract

`docs/website-source.json` records a full application commit SHA, its public
repository URL and a human-readable label. The initial pin is a development
snapshot selected by the maintainer, not a stable release.

The builder exports the authored guides and all imported Compose, configuration,
OIDC and protobuf sources from that commit into a temporary directory. It
generates the API schema using Buf 1.50.0 and the pinned OpenAPI plugin.
The current branch supplies the website presentation, docs entry page and
version explanation. No build reads application examples from a floating branch.

A source pin does not make old prose correct. Authors must review guidance against
the pinned implementation, especially deployment commands, external integrations,
sleep modes, permissions, destructive actions and recovery. Open gaps are tracked
in the [documentation backlog plan](plans/website-documentation-backlog.md).

## Build and verify

Requirements: Git with the pinned commit available, Python 3.12+, Node.js 24.22.1,
npm and Buf 1.50.0. Network access is needed for locked npm packages and the
versioned Buf generator. No application, Docker daemon or database is required.

```sh
python3 -m unittest scripts.test_website scripts.test_landing_page
python3 scripts/check_brand_assets.py
python3 scripts/build_website.py
python3 scripts/check_website.py
```

Generated output stays in ignored `.website-build/site/`. The builder excludes
landing QA pages. The link check validates local page and asset paths, including
the API iframe and schema, under the deployment base.

For a local preview rooted at the server's root:

```sh
python3 scripts/build_website.py --base /
python3 scripts/check_website.py --base /
python3 -m http.server 18765 --directory .website-build/site
```

Open `http://localhost:18765/` and `http://localhost:18765/docs/`.
Use the default base when preparing a Pages artifact. Test search in a built
preview; Starlight's production search index is generated at build time.

The docs project's normal development server previews working-tree content and
labels its banner accordingly. Use the combined build to verify the publication
pin and generated API schema.

## Advance the pin

1. Select and verify the intended full application commit. Prefer a published
   stable release once the project has one; explicitly label development snapshots.
2. Update the manifest revision and label. Keep the referenced commit reachable.
3. Review authored docs, imported examples and API changes against that commit.
   Update the source docs first, commit them, then pin to the resulting commit.
4. Build and check the site. Verify the version banner and source link, install
   guide, search, API reference, mobile navigation, and a link back to the homepage.
5. Merge the reviewed change. The one combined Pages artifact deploys automatically.

Neither publishing a docs pin nor publishing the website creates an application
release. Pinned documentation must not claim live runtime acceptance that has
not been performed.
