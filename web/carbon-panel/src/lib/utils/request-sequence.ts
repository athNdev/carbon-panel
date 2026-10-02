/**
 * Monotonically increasing sequence guard against stale async responses.
 *
 * Each fetch takes `next()` before awaiting; after the await it drops the
 * response when `!isCurrent(seq)`, so a slow earlier response can never
 * overwrite newer results.
 */
export function createRequestSequence() {
	let latest = 0;
	return {
		next(): number {
			latest += 1;
			return latest;
		},
		isCurrent(seq: number): boolean {
			return seq === latest;
		},
		get current(): number {
			return latest;
		}
	};
}
