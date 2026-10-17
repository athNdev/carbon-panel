# Issue 013: UI-5 - Guard Destructive Backup Actions with Confirmation Modals

## Context & Problem
In `web/carbon-panel/src/lib/components/server-backups.svelte`:
- Clicking "Restore" immediately triggered an asynchronous backup restoration: stopping the container, wiping current state, and extracting the archive without asking for user confirmation.
- Clicking "Delete" immediately deleted the backup record and disk archive without confirmation.

Accidental clicks on these buttons in production could cause irreversible data loss or unexpected server downtime.

## Solution
1. Integrated `<CarbonConfirm>` dialogs for both "Restore" and "Delete" actions in `server-backups.svelte`.
2. Created separate state trackers `pendingRestore` / `restoreConfirmOpen` and `pendingDelete` / `deleteConfirmOpen`.
3. Clear danger warnings and details showing the target backup filename before destructive operations can be executed.
4. Added an automated accessibility and UI assertion in `web/carbon-panel/src/lib/utils/a11y-audit.test.ts`.
