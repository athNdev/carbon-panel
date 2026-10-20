// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

/**
 * Zero/NaN-safe percent helper.
 * Returns 0 whenever the denominator is missing, non-finite, or <= 0
 * (or the numerator is non-finite), instead of producing NaN/Infinity.
 */
export function safePercent(numerator: number, denominator: number): number {
	if (!Number.isFinite(numerator) || !Number.isFinite(denominator) || denominator <= 0) {
		return 0;
	}
	return (numerator / denominator) * 100;
}

/**
 * Memory-bar width helper. Returns a 0-100 percent, guarding against a
 * zero/missing `memory` allocation (which would otherwise render `width: NaN%`).
 */
export function memoryUsagePercent(memoryUsage: unknown, memory: unknown): number {
	const used = Number(memoryUsage);
	const total = Number(memory);
	if (!Number.isFinite(used) || !Number.isFinite(total) || total <= 0 || used <= 0) {
		return 0;
	}
	return Math.min((used / total) * 100, 100);
}
