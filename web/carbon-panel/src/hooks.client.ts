/**
 * Client-side Svelte hooks.
 *
 * Why this file exists
 * --------------------
 * The app previously had no `hooks.client.ts`, so Svelte's default
 * `handle_error` applied. In Svelte 5 the default rethrows on a render
 * effect's first run, and the effect flush has no try/catch around it. The
 * consequence on the server detail page was severe and long-lived:
 *
 *   - a single throw while any tab panel rendered
 *   - -> the whole flushed batch was abandoned (`#unlink()`)
 *   - -> the tab indicator (a separate binding) still updated to the new tab
 *   - -> the panel below kept rendering the PREVIOUS tab's content
 *
 * Visually: the new tab looks selected, but its content never appears. Because
 * nothing was logged as a page-level failure and polling kept running, this read
 * as "the Console tab is dead" rather than "a render threw", which is why it was
 * misdiagnosed repeatedly.
 *
 * What this does
 * --------------
 * `handleError` logs every uncaught render/effect error with enough context to
 * identify the offending component, and deliberately does NOT rethrow. Not
 * rethrowing is what stops one failing subtree from cancelling the update batch
 * for the rest of the page.
 *
 * `handleError` never swallows silently: the error is reported to the console
 * with its stack, and the page-level `<svelte:boundary>` on the tab content area
 * renders the visible "This tab failed to render" + Retry UI for the subtree
 * that actually failed. So the failure stays both contained AND observable.
 */

export function handleError(error: unknown, event: { component?: unknown } | undefined) {
	const message = error instanceof Error ? error.message : String(error);
	const stack = error instanceof Error ? error.stack : undefined;
	const component =
		event?.component && typeof event.component === 'object'
			? ((event.component as Record<string, unknown>).constructor?.name as string | undefined)
			: undefined;

	console.error(
		`[handleError] Uncaught Svelte ${component ? `<${component}>` : ''} error: ${message}`,
		stack ?? error
	);

	// Intentionally no rethrow: see the file header. Rethrowing here is what
	// cancelled the entire effect-flush batch and wedged the page.
}
