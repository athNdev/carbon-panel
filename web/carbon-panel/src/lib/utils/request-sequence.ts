// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


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
