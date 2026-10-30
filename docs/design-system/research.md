# Research: a shared Carbon design system

Research date: 30 October 2026. This note records the reference workflow and
the starting evidence; it does not assert that either application is already
conformant with the proposed system.

## What the Claude reference means

Anthropic describes a design system as a reusable collection of color,
typography, components and layout patterns, sourced from an existing codebase,
brand assets or other design references. It describes a workflow in which a
team imports that system, creates a design, reviews and refines it, then hands
it to Claude Code for implementation. This is a model for **how the system is
used**, not a request to copy Claude's visual identity. [Set up your design
system in Claude Design](https://support.claude.com/en/articles/14604397-set-up-your-design-system-in-claude-design),
[Get started with Claude Design](https://support.claude.com/en/articles/14604416-get-started-with-claude-design).

Anthropic's setup guide specifically recommends `/design-sync` for an
existing **React** component system. Carbon Panel and Carbon Cloud use Svelte,
so this research does not establish that their components can be imported as
live, editable components through that command. An [Anthropic issue
report](https://github.com/anthropics/claude-code/issues/71523) describes a
React-only limitation in version 2.1.193; it is a user report, not evidence of
the current behavior in every environment. No sync or authorization was
attempted in this research. A portable documented source and working product
integration are useful independently of Claude, and any later Claude import
needs its own validation.

## Current product evidence

- Panel's `web/carbon-panel/src/app.css` defines light and dark semantic
  colors, Tailwind mappings, IBM Plex fonts loaded through Fontsource, motion
  utilities and reduced-motion handling. Its button primitive also embeds
  several literal color values, which will not automatically follow changed
  semantic tokens.
- Cloud's `web/cloud-console/src/app.css` defines a separate set of semantic
  colors; both its `:root` and `.dark` declarations currently use a dark
  palette. `web/cloud-console/src/app.html` loads IBM Plex from Google Fonts.
  Cloud has its own Svelte components, including button, table, field, modal
  and shell.
- Both use SvelteKit and Tailwind CSS 4. Their existing components and shipped
  states are the baseline to document and evolve; the different implementations
  should not be assumed to have identical behavior or accessibility.

## Accessibility and interaction references

- WCAG 2.2 AA specifies at least 4.5:1 contrast for normal text and 3:1 for
  large text, subject to its exceptions. It also requires a visible keyboard
  focus indicator, and its minimum target-size criterion is 24 by 24 CSS pixels
  with specified exceptions. [WCAG 2.2](https://www.w3.org/TR/WCAG22/).
- Required visual information for user-interface components and their states
  must have sufficient non-text contrast; this can include a custom focus
  indicator. Test the indicator against the adjacent colors where it appears.
  [Understanding non-text contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast).
- The WAI-ARIA modal-dialog pattern calls for an accessible name, focus moving
  into the dialog, a contained tab sequence, Escape to close, and appropriate
  focus restoration after close. Background content is inert while the modal
  is open. [Dialog (Modal) Pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/).
- Respect an operating-system preference for reduced motion when designing
  interaction and feedback. [MDN: Using media queries for accessibility](https://developer.mozilla.org/en-US/docs/Web/CSS/Guides/Media_queries/Using_for_accessibility).

These references inform implementation and testing; a token palette or a
document alone cannot establish application-wide accessibility conformance.

## Portability implication

The [Design Tokens Format Module](https://www.designtokens.org/TR/2025.10/format/)
describes an exchange format for design tokens, including names, types and
aliases. It is a Design Tokens Community Group report, not a W3C standard.
Token interchange is useful when there are real consumers for it; simply
using a JSON file or CSS custom properties does not itself establish
conformance to that format. For Carbon, the important design decision is an
explicit source of truth, repeatable propagation to both repositories, and a
way to detect drift. Component behavior, content guidance and accessibility
still need documentation and verification alongside token values.
