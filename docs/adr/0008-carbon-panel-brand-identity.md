<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

# ADR 0008 — Carbon Panel brand identity

- Status: accepted
- Date: 2026-10-28

## Context

The landing page, application, documentation and repository artwork currently
use different marks. A consistent identity needs to work as a small application
icon as well as a complete product wordmark, and must be portable across
deployment environments.

## Decision

- The approved Carbon Panel symbol is the single, solid lower-cut block in
  `assets/branding/`. The opening can suggest a doorway into a Minecraft
  world.
- The approved wordmark is the technical-capitals design. The asset directory
  contains horizontal, stacked, symbol-only and wordmark-only versions in dark
  and light artwork, plus size-specific exports and the editable VectorCraft
  source.
- Treat the outlined SVGs in `assets/branding/` as the portable artwork
  masters when integrating the brand. Their geometry must not depend on the
  presence of a workstation font or a network font service.
- The user interface can continue using IBM Plex for ordinary text; accepting
  the logo does not require a site-wide font change.

## Consequences

- Future product-owned logo replacements should use the approved artwork and
  preserve its proportions, opening and recognisability at the target size.
  Minecraft server favicons and third-party logos remain their own assets.
- VectorCraft's editable source uses Bahnschrift SemiBold for the wordmark and
  Inter for sheet labels. These fonts are not bundled; the exported, outlined
  SVGs retain the approved appearance without them.
- Adding the artwork does not by itself update the deployed landing page,
  console, documentation or external social profiles. Adoption and
  verification are planned separately.
