<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# UI-3: Accessible Server Status Indicators in Dashboard Activity

**Status:** Completed & Verified  
**Priority:** Low  
**Category:** Accessibility / UI  
**Component:** `web/carbon-panel/src/routes/+page.svelte`  

## Problem Description
The Recent Activity feed on the main dashboard previously rendered status pips using colored `<div>` tags without semantic roles, text alternatives, or ARIA labels. Users relying on screen readers or high-contrast color modes could not distinguish the server status associated with each activity item (violating WCAG 1.4.1 Use of Color and WCAG 4.1.2 Name, Role, Value).

## Implementation Details
1. In `web/carbon-panel/src/routes/+page.svelte`:
   - Updated the activity status pip to include:
     - `role="img"`
     - `aria-label={getStringForEnum(ServerStatus, activity.status)}`
     - `title={getStringForEnum(ServerStatus, activity.status)}`
   - Preserves Carbon Design System aesthetic styling while providing full screen reader announcement and hover tooltips.

## Verification
- Svelte diagnostics: `bun run check` reports 0 errors and 0 warnings.
- Accessibility audit suite: `bun test` passes all tests.
