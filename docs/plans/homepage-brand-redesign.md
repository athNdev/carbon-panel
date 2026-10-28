# Carbon Panel homepage and brand rollout plan

**Status:** Implementation is in progress on the brand redesign branch.
Deployment remains a separate step and the shared Pages hosting configuration
still needs to be confirmed.

The branch now includes the landing-page update, distributed brand assets,
application and documentation replacements, and scripts for synchronizing and
checking artwork. Full interactive application journeys, accessibility checks,
CI and live deployment still need verification. The `landing/qa-*.html` pages
are historical review fixtures and are excluded from the published artifact.

**Baseline:** `main` at `97a9e151e79cacc8687eff2d16ab9fd602600511`.

**Approved identity:** Lower-cut block symbol and the 05 Technical capitals
wordmark. The approved source and exports are in
[`assets/branding/`](../../assets/branding/README.md); the identity decision
is recorded in [ADR 0008](../adr/0008-carbon-panel-brand-identity.md).

## 1. Goal and scope

Give Carbon Panel a coherent, recognisable identity across the public landing
page, application, documentation, and repository-owned promotional assets.
The symbol is one solid block with a lower opening; it can suggest an entrance
to a Minecraft world. The technical capitals give the name an operational
character without adding details to the symbol.

The **homepage** in this plan is the public static site in
[`landing/`](../../landing/index.html). The SvelteKit application and docs
are in scope for brand replacement, not a wholesale redesign of their
workflows. Preserve the project's self-hosted identity: the landing page must
not imply that a hosted service or managed cloud is required.

The logo files were added in a separate commit. The implementation updates the
homepage and repository-owned brand surfaces; the baseline observations below
describe the state before those changes.

## 2. Current state and constraints

- The landing page is hand-authored HTML, CSS, and progressive JavaScript:
  [`index.html`](../../landing/index.html),
  [`styles.css`](../../landing/assets/styles.css), and
  [`script.js`](../../landing/assets/script.js). The current page has a
  sticky navigation, hero and product mockup, supported-server strip, feature
  sections, architecture and fleet illustrations, setup steps, comparison,
  FAQ, and footer. It has a dark Carbon-derived palette and blue accents.
- The landing header and footer each contain a blue square and outlined cube
  drawn inline. A third copy is encoded in the favicon data URI. The social
  image embeds the same old mark.
- The application shell currently renders the product name as text, while
  the login page and browser icons use older `g1_*` artwork. The docs splash
  and favicon use another older mark. The README banner uses a different
  inline cube again.
- The public landing artifact is uploaded from `landing/` alone; the docs
  artifact is built separately. New master assets under `assets/branding/`
  will not automatically be present in either deployed artifact or the
  embedded SvelteKit application.
- The landing and docs workflows both target the `github-pages` environment.
  The landing workflow explicitly warns that one deployment can replace the
  other. Confirm the current hosting configuration before changing deployment
  behavior; do not assume both sites are presently available together.
- The application uses IBM Plex supplied locally through `@fontsource` and
  must remain usable offline. The approved wordmark's Bahnschrift lettering is
  already outlined in the committed SVG exports; using the logo must not add
  a runtime font or external network dependency. The editable VectorCraft
  source needs its named fonts to edit type faithfully.

## 3. Brand system to apply

1. Use the approved symbol and wordmark without changing the cutout, adding
   separate pixels, stretching, or recreating the letterforms in CSS. Use the
   outlined horizontal SVG where the full name is appropriate and the symbol
   alone where space is tight. Use the stacked lockup for constrained,
   taller placements.
2. Start from the current Carbon neutral surfaces: `#161616` page background,
   `#262626` raised surfaces, `#393939` borders, and `#f4f4f4` primary
   text. Use the committed light mark on dark surfaces and dark mark on light
   surfaces. The homepage uses a black, white and grey palette with restrained
   metallic highlights. A product illustration may retain the application's
   actual interface and status colors.
3. Keep IBM Plex Sans for body and UI text and IBM Plex Mono for code and
   operational data. Use the approved outlined wordmark for the logo itself,
   not Bahnschrift as a new site-wide font. Verify rights before distributing
   or serving any separately acquired font file.
4. Take spacing cues from the block: square, deliberate geometry, a consistent
   grid, clear edges and generous whitespace. Reserve gradients and motion for
   secondary visual emphasis; the approved mark remains a simple flat shape.
5. Follow the usage notes in `assets/branding/README.md`. Treat clear space,
   dark/light contrast and a recognisable 16-24 px mark as review criteria,
   not merely file-export settings.

## 4. Homepage redesign

### 4.1 Navigation and hero

- Replace both inline brand treatments in the navigation and footer with the
  approved logo. In the 64 px desktop header, test a mark around 24 px high
  with a legible wordmark; switch to the mark with an accessible name when the
  complete lockup no longer fits. Preserve the navigation toggle, keyboard
  focus, anchor destinations, GitHub link and primary action.
- Make the first screen explain **what it is, who runs it, and what to do
  next**: self-hosted Minecraft server management, on the operator's own
  infrastructure, with clear **Get started** and **View on GitHub** actions.
  Candidate headline for design exploration: “Your Minecraft servers. Your
  infrastructure.” Final wording remains subject to review.
- Reduce decorative noise around the hero; use the dark neutral grid, restrained
  silver interaction accents and a single credible product visual. Keep the
  lower-cut opening visually readable and do not use the mark as a generic
  feature icon.
- Replace or update the current illustrative dashboard with an accurate
  current UI capture using synthetic/non-sensitive data, or label a simplified
  diagram as an illustration. Do not present fabricated counts, availability,
  or a mock screen as live production evidence. Keep a useful text alternative.
- Retain the install command and copy action, but verify the command against
  the current quick-start instructions and make it usable without JavaScript.

### 4.2 Information architecture

Keep the existing deep-link IDs where possible. Proposed reading order:

1. **Hero:** purpose, self-hosted context and primary actions.
2. **What the panel does:** a short overview of servers, routing, modpacks and
   operations; show server-type support without a decorative scrolling
   dependency.
3. **How it fits together:** one concise diagram for players, proxy, panel,
   nodes, containers and backups. Explain the topology in text as well as
   graphics; retain the useful architecture anchor.
4. **Operational capabilities:** group isolation and placement, routing,
   backups, sleep/wake, modpacks and access control into scannable cards.
   Link the relevant documentation instead of repeating dense marketing copy.
5. **Getting started:** prerequisites and a truthful three-step path to the
   documented install instructions. Do not imply that the clone command alone
   installs the product.
6. **Trust and boundaries:** self-hosted ownership, open-source status and
   documented security properties; link to source, contributing and docs.
7. **Comparison and FAQ:** keep them only as useful, sourced decision aids;
   review claims and avoid broad statements about unnamed competing products.
8. **Final action and footer:** repeat one primary action and provide working
   product, documentation and community links.

Use visual hierarchy and shorter sections before adding new interactions. The
existing architecture and fleet scroll sequences are candidates for a simpler
static or progressively enhanced presentation, provided their explanatory
content and anchor links remain available.

### 4.3 Content accuracy

Before publishing the redesign, check each feature statement against the
current source, documentation and current release. In particular, verify the
conditions and limitations behind migration/replicas, automatic routing,
backups, sleep/wake, keyless modpack access and node placement.

Replace or qualify absolutes such as “infinite machines”, “never the host”,
“every world stays one join away”, and “zero RAM” where the evidence supports
only a narrower statement. Distinguish a stopped server process from total
proxy, panel and host memory use. Do not claim availability, recovery,
isolation or performance guarantees based solely on a UI mockup or CI.

## 5. Repository-wide logo replacement inventory

The following is the initial inventory from the baseline. Re-run a tracked
file and image-reference search at implementation time, including any files
added since this plan.

| Surface | Current location / use | Planned change |
| --- | --- | --- |
| Landing navigation and footer | `landing/index.html` inline `svg.mark` and `Carbon<b>Panel</b>` | Replace both from one shared approved asset; preserve accessible link names and responsive behaviour. |
| Landing favicon | `landing/index.html` data-URI SVG | Replace with a standalone, appropriately contrasted mark; validate browser-tab appearance. |
| Landing social card | `landing/assets/og.png`; OG, Twitter and JSON-LD metadata in `landing/index.html` | Redesign the 1200×630 card using the approved identity; keep metadata, alt text, dimensions, URL and cache strategy consistent. |
| Repository artwork | `assets/logo.svg`, `assets/banner.svg`, `README.md` | Replace the old outlined cube and update the banner while keeping meaningful alt text and stable public paths where feasible. Check externally referenced paths before removal. |
| Application shell | `web/carbon-panel/src/lib/components/carbon/CarbonShell.svelte` | Add the approved compact mark/wordmark to the existing text-only header without disturbing navigation, profile controls or accessible names. |
| Login / initial setup | `web/carbon-panel/src/routes/login/+page.svelte` | Replace `/g1_24x24.png` with the approved mark; test both login and initial-admin states. |
| App browser icons | `web/carbon-panel/src/app.html`, `web/carbon-panel/static/g1_*.png`, `g1_22.svg`, `favicon.png`, `src/lib/assets/favicon.svg` | Replace or retire the old icon family after checking references and external compatibility. Provide tested 16/32/64 px, SVG and touch-icon variants, including suitable contrast on browser surfaces. |
| Legacy app image | `web/carbon-panel/static/carbon_panel_logo.png` | Audit uses and external compatibility; replace or retire the image that still says “MineServer”. |
| Docs | `docs/carbon-panel/astro.config.mjs`, `public/favicon.png`, `public/carbon_panel_logo_transparent_512.png`, `src/content/docs/index.mdx` | Update favicon and splash emblem for both themes; review the existing 3D/animated treatment and reduced-motion behaviour. |
| Docs screenshot | `docs/carbon-panel/public/carbon_panel_glance_widget.png`, referenced by `src/content/docs/faq.mdx` | The screenshot visibly contains a previous product name. Refresh it with a current, synthetic-data example or remove it if no longer relevant; add meaningful alternative text. |
| QA fixtures and checks | `landing/qa-full.html` and other `landing/qa-*.html`; `scripts/manual-review/e2e/run.mjs` | Refresh or remove obsolete visual fixtures deliberately. The E3 check currently treats any header image with Carbon in its alt text as a legacy logo; update the check so it identifies the old asset rather than rejecting the new one. QA pages are excluded from the public landing artifact. |
| Deployable copies | `.github/workflows/landing.yml`, `.github/workflows/docs.yml`, `scripts/web-build.sh` | Ensure each built/deployed surface actually contains its new logo assets. Update build inputs and path filters if canonical assets are copied during build. |

Do **not** replace Minecraft server favicons, user-supplied server images,
modpack/provider logos, generic UI icons, third-party badges, historical
attribution or licence text. These are not Carbon Panel brand marks.

## 6. Asset distribution and implementation order

1. **Establish one source:** keep `assets/branding/` as the authoritative
   approved artwork. The outlined SVGs are usable without proprietary or
   workstation-specific fonts. Agree which filenames remain public interfaces
   before deleting or renaming existing files.
2. **Make deployable assets portable:** choose one deterministic process to
   copy or derive the required logo files into `landing/assets/`,
   `web/carbon-panel/static/` and `docs/carbon-panel/public/` as needed.
   Avoid filesystem symlinks that escape each deployment root. Record the
   generation process and pinned tools where feasible; verify generated
   copies match the master. Keep the application working without network font
   access and with supported base paths.
3. **Implement the landing redesign:** update markup, styles, visual assets,
   metadata and only the JavaScript needed for retained interactions. Keep
   progressive enhancement: content and navigation remain usable with
   JavaScript disabled, and reduced-motion users get a static equivalent.
4. **Update the application and docs:** replace the audited brand references,
   maintain relevant light/dark contrast and ensure the embedded production
   frontend contains the final files. Update old screenshots and QA fixtures.
5. **Check distribution:** verify README/banner rendering, social-card output,
   app and docs build artifacts, and cache invalidation. Confirm how landing
   and docs are hosted before changing the shared Pages deployment.
6. **Review before release:** compare the implemented result against this
   plan, inspect the actual screenshots and functionality, and resolve
   outstanding copy or asset decisions. Any publishing/merge/deployment is a
   later step, not implied by this proposal.

## 7. Verification and acceptance

- **Artwork:** compare mark geometry and dark/light silhouettes; verify SVGs
  contain outlined lettering and no external dependencies; inspect 16, 24,
  32, 40 and 64 px icons and the larger touch/social assets on both light and
  dark backgrounds. Ensure the cutout remains legible and colours contrast.
- **Landing page:** render at 320, 375/390, 768, 1024 and 1440 px; inspect
  200% zoom, no horizontal overflow, navigation, anchors, FAQ, copy action,
  install links, metadata and social preview. Test JavaScript disabled,
  keyboard-only use and reduced motion. Verify text and control contrast
  against their actual backgrounds.
- **Application:** run `bun install --frozen-lockfile`, `bun run check`
  and `bun run test` in `web/carbon-panel`, followed by
  `bash scripts/web-build.sh`. Inspect the embedded build, login,
  initial-setup state, shell on desktop/mobile, favicon and offline rendering.
  Use synthetic/non-sensitive test data.
- **Docs:** run `npm ci` and `npm run build` in `docs/carbon-panel`;
  inspect splash and favicon in both themes and verify links and image paths.
- **Repository:** re-run the focused old-brand inventory, inspect referenced
  raster images, check SVG/PNG dimensions and transparency, and check that
  legitimate server/modpack icons and legal attribution were not touched.
  Update or remove stale QA copies and fix the logo-specific E3 check.
- **CI and operational evidence:** run the checks for each affected surface
  and inspect CI results. A local screenshot or passing build does not prove
  deployment. After a separately authorised release, verify the live landing,
  docs, app and social preview at their actual URLs and report any unverified
  surfaces.

## 8. Decisions for review

- Confirm the proposed information hierarchy and how much of the existing
  architecture/fleet animation should remain.
- Confirm the headline direction and how strongly Minecraft imagery should
  feature alongside the operational aesthetic.
- Resolve the landing/docs hosting arrangement before deploying either site;
  the current workflows share one Pages environment.
- Review specific feature and comparison claims against current implementation
  evidence before approving final public copy.
