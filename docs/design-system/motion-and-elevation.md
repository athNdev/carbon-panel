# Motion, transitions and elevation

These definitions extend the shared Carbon design system. The canonical
CSS token source is owned by Panel; Cloud consumes a content-pinned snapshot.
The standalone [design-system homepage](index.html#motion) demonstrates the roles.
The keyframes and tokens are defined in the canonical CSS source. Test actual
components before claiming that a product surface uses or conforms to them.

## Timing

| Token | Value | Intended use |
| --- | --- | --- |
| `--motion-instant` | 100 ms | Frequent hover, press and focus-adjacent feedback. |
| `--motion-page` | 140 ms | Short route crossfades where supported. |
| `--motion-fast` | 150 ms | Small state and indicator changes. |
| `--motion-base` | 200 ms | Brief entrance of a new element. |
| `--motion-slow` | 300 ms | Deliberate, infrequent feedback. |
| `--motion-surface` | 500 ms | Infrequent larger surface changes; avoid blocking input. |
| `--motion-stagger-step` | 40 ms | Optional short stagger for a small group, never for routine data updates. |
| `--carbon-motion-pulse-duration` | 1600 ms | Optional loading/status pulse with a persistent text cue. |
| `--carbon-motion-shimmer-duration` | 1400 ms | Optional loading skeleton shimmer. |

`--ease-out-quart` (`cubic-bezier(0.25, 1, 0.5, 1)`) is the default for
responsive changes. The shared source also defines out-quint, out-expo,
in-out-standard and sheet curves for existing needs. Avoid spring bounce and
inventing new easing curves for routine interactions.

## Transition contracts

- `--carbon-transition-color` transitions color, background and border only,
  using the instant duration and default easing. Use when each property is
  actually changing; do not use `transition: all`.
- `--carbon-transition-press` transitions transform over 100 ms. The press
  scale is `--carbon-motion-press-scale: 0.96`. Disable the effect when it
  would distract, and do not make pressing motion the only feedback.
- Transitions should be interruptible. A theme switch should snap rather than
  visibly crossfade the entire interface. A persistent color, icon or label
  must communicate any state after motion has stopped.

## Animation contracts

| Token | Behavior | When to use |
| --- | --- | --- |
| `--carbon-animation-fade-in` | Opacity from 0 to 1 over 200 ms. | A new, infrequent piece of feedback. |
| `--carbon-animation-rise-in` | Fade in while moving up 6 px over 200 ms. | A new surface or result where entrance adds orientation. |
| `--carbon-animation-pulse` | Opacity cycles between 1 and 0.35 over 1600 ms. | Loading or ongoing status with a visible label. |
| `--carbon-animation-shimmer` | Background position moves across a skeleton over 1400 ms. | A short-lived placeholder for content that is actually loading. |

The tokens refer to `carbon-ds-*` keyframes in the canonical CSS source.
No animation is applied automatically. Avoid staging routine content or repeated polling
updates. For exit, use a short fade or small movement and do not hold up an
action to play an effect.

## Reduced motion

Honor `prefers-reduced-motion: reduce` in every consuming surface. Replace
transform, looping pulse, shimmer and route animation with a stable result;
retain labels and non-motion state cues. The standalone homepage and both
applications have reduced-motion rules, but individual legacy components still
need review. Test with the preference enabled and verify that tasks remain
understandable and operable.

## Elevation and shadows

| Role | Token | Use |
| --- | --- | --- |
| Flat | `--carbon-shadow-none` | Page sections, standard cards and dense data. |
| Raised | `--carbon-shadow-raised` | A non-modal floating menu or popover that needs separation. |
| Overlay | `--carbon-shadow-overlay` | A dialog or similarly prominent floating surface. |

The light and dark themes define layered, translucent black shadows with
stronger opacity in dark mode. Use a structural border when a boundary,
selection or focus state must be visible; a shadow alone is not an accessible
state indicator. Avoid stacking shadow levels or adding elevation to every
card. Check clipping and stacking contexts at narrow widths and zoom.

## Change and review

Change the canonical tokens and document their meaning before propagating to
Cloud. Check hover, focus, active, disabled, loading and reduced-motion states
in both themes. Verify keyboard use, focus restoration for overlays and the
actual adjacent colors. A matching token snapshot cannot prove that a legacy
component consumes the tokens or that a screen is accessible.
