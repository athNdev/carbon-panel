// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

const inflight = new Map<string, string>();

function randomId(): string {
	if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
		return crypto.randomUUID();
	}
	return `${Date.now().toString(16)}-${Math.random().toString(16).slice(2)}`;
}

/** One idempotency key per in-flight lifecycle action.
 * A second call for the same server and action, while the first has not
 * finished, reuses the key so the server does not apply the mutation twice.
 */
export function beginLifecycleRequest(
	serverId: string,
	action: string
): { requestId: string; finish: () => void } {
	const key = `${action}:${serverId}`;
	const existing = inflight.get(key);
	if (existing) {
		return { requestId: existing, finish: () => {} };
	}
	const requestId = randomId();
	inflight.set(key, requestId);
	let settled = false;
	return {
		requestId,
		finish: () => {
			if (settled) return;
			settled = true;
			if (inflight.get(key) === requestId) {
				inflight.delete(key);
			}
		}
	};
}
