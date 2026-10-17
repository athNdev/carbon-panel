# UI-4: Accessible Toasts and Live Region Verification

## Overview
Ensures notifications and toast messages are accessible to screen reader users across all views in the Carbon Panel web interface.

## Changes Made
- Verified root layout (`+layout.svelte`) embeds `<Toaster />` from `$lib/components/ui/sonner`.
- Verified that `sonner.svelte` wraps `svelte-sonner` and forwards component props to preserve polite live region semantics (`role="region"`, `aria-label="Notifications"`).
- Added automated a11y regression test in `web/carbon-panel/src/lib/utils/a11y-audit.test.ts`.
