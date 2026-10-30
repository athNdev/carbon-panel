# Carbon design system

This is the shared design contract for Carbon Panel and Carbon Cloud. It is
grounded in their existing IBM Carbon-derived visual language, not Claude's
branding. The goal is to let people and coding agents inspect, use and evolve
the same foundations across both products. The [research](research.md) explains
the reference workflow and source evidence.

## Start here

1. Read the foundations and the relevant interaction patterns below before
   designing a screen. Use existing components before adding new ones.
2. The canonical machine-readable values are in
   [tokens.css](../../web/carbon-panel/src/lib/design-system/tokens.css).
   Both applications load those values; Cloud carries a verified snapshot.
3. Open the standalone [design-system homepage](index.html), or serve the
   repository root with a static HTTP server and visit /docs/design-system/.
   It is independent of either application's runtime and authentication.
   Its example controls are illustrative, not the actual Svelte components.
4. For a new design, record the screen's purpose, primary task, users and
   permissions, content, states, responsive behavior and keyboard path. Review
   the resulting implementation, not just a screenshot.

## Principles

- **Operational clarity:** show what exists, its current state, and what an
  action will do. Never invent values for failed or unavailable requests.
- **Control with consequences:** make the primary next step clear; explain
  destructive, long-running, billable and permission-sensitive operations.
- **Consistent foundations, appropriate density:** use the same roles and
  interaction rules across the self-hosted panel and managed console. A
  compact table and a mobile form can use different layouts.
- **Accessible by default:** keyboard, contrast, text alternatives, reduced
  motion and understandable feedback are requirements, not a final polish.
- **Earn complexity:** reuse a primitive when its behavior fits; extend the
  system when a recurring need cannot be expressed without a local workaround.

## Foundations

### Color and theme

Use semantic roles rather than literal hex values. Background is the page;
card is the first surface; popover is an elevated surface. Foreground and
muted-foreground are text roles; border is structural, input is a visible
control boundary, and ring is for focus. Primary and destructive are fills;
primary-text and destructive-text are their text-safe counterparts. Success
and warning also have text roles. Status tags have dedicated foreground and
background pairs. Do not convey meaning by color alone. Charts must use labels
or patterns as well as color and provide a text alternative.

The source provides light and dark palettes. Panel supports both. Cloud
currently starts with the dark class and does not expose a theme picker; its
light palette is a foundation for future work, not a claim that Cloud's pages
have been qualified in light mode. A preview may use data-carbon-theme on a
container; application themes use the root dark class. Test contrast in the
actual adjacent surfaces, including hover, selection and focus states.

### Type, spacing and geometry

IBM Plex Sans is the interface face and IBM Plex Mono is for code, addresses,
logs and stable-width technical values. Keep prose in sans and use tabular
figures for changing metrics. Use normal case for labels; do not put long
descriptions in all caps. Provide sensible system fallbacks.

The shared spacing steps are 4, 8, 12, 16, 24, 32 and 48 CSS pixels
(expressed as rem). Group related items more tightly than unrelated sections.
Control heights are 32, 40, 48 and 64 pixels. Prefer the 40-pixel default; a
compact control must still satisfy the target-size and spacing rules. The
current system uses square corners, thin structural borders and restrained
elevation. Match nested spacing and avoid decoration that competes with status
or actions. These are current defaults and may be revised centrally.

### Icons, imagery and layers

Use existing product artwork and the icon set already used on the surface.
Icons inherit the text color and state; give icon-only controls an accessible
name. Use a consistent optical weight and align icons with their labels. Do not
use color or an icon as the sole explanation of an important state. User or
third-party imagery retains its own attribution and must have appropriate
alternative text. Layer popovers, menus, dialogs and toasts so they do not
obscure their trigger or required task, and test clipping and focus order.
The existing icon sets are not yet fully consolidated.

### Layout and responsive behavior

Maintain a clear page heading, short explanatory text where needed, then the
primary task. Use a consistent content width and section rhythm. At narrow
widths stack fields and actions, preserve readable line lengths, and let dense
tables scroll inside a named region or offer a tested alternative. Do not hide
critical data or actions solely because the screen is small. Test at 320 CSS
pixels, common desktop widths, 200% zoom and with long translated labels.

### Motion and feedback

The detailed [motion and elevation reference](motion-and-elevation.md) defines
timing, easing, transition, animation, shadow and reduced-motion contracts.

Shared duration and easing tokens define the existing fast, restrained motion
language. Animate only properties that change; feedback should remain clear
without animation. Respect prefers-reduced-motion, including route transitions,
spinners and loading skeletons. A user should never have to wait for a
decorative animation before proceeding.

## Accessibility contract

- Use semantic HTML, a logical heading hierarchy and an accessible name for
  every control, including icon-only actions. Associate labels, hints and
  errors with their fields; indicate required fields in text as well as code.
- All functionality must be keyboard reachable and operable. Show a visible
  focus indicator with sufficient contrast against adjacent colors; never
  remove focus styling without an equivalent. Do not rely on hover alone.
- Aim for WCAG 2.2 AA: normal text contrast at least 4.5:1, large text and
  required non-text boundaries at least 3:1, subject to the standard's
  exceptions. Targets must satisfy the 24-by-24 CSS-pixel minimum or its
  spacing/other exceptions; aim for 44 by 44 on touch-heavy actions.
- Dialogs need a meaningful name, focus placement and containment, Escape
  behavior where appropriate, inert background and focus restoration. For
  tabs, use the selected state, associated panel and arrow-key behavior.
- Announce meaningful async changes using an appropriate status or alert live
  region; do not repeatedly announce polling updates. Give errors in text and
  offer a recovery path. Test with keyboard and a screen reader.
- Reduced motion, browser zoom, text enlargement, high-contrast/forced colors,
  translation, RTL and narrow screens are review dimensions; their inclusion
  here does not certify every existing surface.

## Component inventory

| Need | Panel implementation | Cloud implementation | Contract |
| --- | --- | --- | --- |
| Action | CarbonButton | Button | One primary action per task; distinguish secondary and destructive actions. A disabled/loading control must not execute. |
| Surface | CarbonTile | Tile | Group related content; a whole tile is interactive only when its destination is clear and keyboard-operable. |
| Status | CarbonTag | Tag | Include a readable state label; color is supplemental. |
| Text input | CarbonTextInput / CarbonSelect | Field plus native control | Label, hint, invalid state and error association; retain entered data on recoverable errors. |
| Table | CarbonDataTable | DataTable | Semantic headers, responsive overflow, honest loading/empty/error states, raw-value sorting. |
| Dialog | CarbonModal / CarbonConfirm | Modal | Name, focus lifecycle, keyboard support and an explicit cancellation path. Confirm irreversible actions. |
| Navigation | CarbonShell / CarbonTabs / CarbonBreadcrumbs | AppShell / Tabs | Identify current location and keep navigation predictable; do not expose inaccessible actions as usable. |
| Feedback | CarbonInlineLoading / CarbonErrorState / CarbonEmptyState | LoadError / EmptyState / Toast | Explain what happened, whether data may be stale, and the next available step. |

These are separate Svelte implementations, not a shared component package.
Their props and behavior are not interchangeable. The tokenized Panel button,
tile, input and tag and Cloud button/tag are initial adoption points. Many
older Panel views and primitives still contain literal colors; changing a token
will not yet update those literals. Cloud has its own component-level choices
and external font loading. Audit and migrate each surface before claiming
complete propagation or theme/accessibility conformance.

## Interaction patterns

- **Navigation:** group by user goals, highlight the current destination and
  preserve a route back. Permission-aware navigation supplements server-side
  authorization; it never replaces it.
- **Forms:** state units and constraints before submission. Validate near the
  field, preserve edits, move or announce focus to an error summary for large
  forms, and provide an explicit save/cancel path. Do not expose secrets after
  their one-time reveal.
- **Tables and lists:** separate initial loading, no results, a truly empty
  collection, partial results and a failed request. Explain truncation,
  pagination and stale data. Keep per-row actions labelled.
- **Search and filtering:** label the field, describe active filters and allow
  each filter to be removed. Distinguish no matches from a failed request and
  avoid changing focus unexpectedly as results update.
- **Menus, tooltips and disclosure:** use a menu for actions, a tooltip for a
  brief supplementary explanation and a disclosure for optional content. Make
  each usable with a keyboard and do not hide required instructions in hover.
- **Console and logs:** use monospace for output, preserve meaningful line
  breaks, expose connection and reconnect states, and avoid moving the user's
  scroll position while they inspect older output. Never leak secrets in logs.
- **File transfers:** state the selected file, progress when measurable,
  failure and retry options. Respect size limits and retain user data when an
  upload, download or connection is interrupted.
- **Async operations:** distinguish submitted, pending, running, complete,
  failed and unknown. Show progress only when measured. Indicate whether an
  action can be retried safely and preserve the operation's real server state.
- **Destructive or expensive actions:** name the affected resource and
  consequences, require proportionate confirmation and make cancellation easy.
  Do not imply completion until the backend confirms it.
- **Server health and metrics:** label units, timestamps and source. A missing
  metric is unavailable, not zero; distinguish disconnected, stale and
  stopped. Use shape/text in addition to color for state.
- **Files, worlds and backups:** explain overwrite, restore and deletion
  consequences; preserve user data and surface errors without hiding partial
  results. Follow the product's actual data-safety rules.
- **Cloud tenancy and billing:** always make the active organization and
  authorization context clear. Display only authoritative prices, capacity,
  charges and dates; if unknown, say so. Never imply a frontend control grants
  authorization.
- **Content:** use direct sentence-case labels and verbs; avoid unexplained
  acronyms. Dates, numbers and units must be localizable. Keep error detail
  useful without revealing credentials or private infrastructure.

## Change and propagation workflow

1. Propose a change here and in the source tokens or component contract. Review
   both light and dark implications, all states, responsive behavior and
   compatibility. Avoid changing meanings behind an existing token silently.
2. Update the Panel source and standalone homepage, run its frontend checks and tests,
   and review the result. Record a breaking change and migration instructions
   when a role or component contract changes.
3. Update Cloud's committed snapshot using its documented sync script. The
   script can compare an explicitly selected source file during paired branch
   work or fetch a full upstream commit after publication. Its lock records the
   content digest; ordinary Cloud builds and tests need no Panel checkout or
   network. A local digest check detects modifications to the snapshot; an
   explicit upstream comparison is needed to detect a newer upstream version.
4. Verify affected screens in both applications, including keyboard, error,
   loading, reduced-motion, theme and narrow-screen states. Tokens alone do
   not propagate through hard-coded colors or prove behavioral equivalence.
5. Review changes together across the two repositories. Keep the independent
   build and deployment boundaries intact.

## Claude Design handoff

The guide, source tokens and real components provide a portable starting point
for a design system imported into a design tool. Anthropic documents a
React-oriented code sync; Svelte support and a live round trip have not been
verified for these repositories. If importing, inspect the resulting colors,
components and states, validate edits against this source, and bring approved
changes back through the workflow above. Do not treat an imported or generated
design as proof that the running applications have changed.
