// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


import { authStore } from '$lib/stores/auth';

/**
 * apiFetch wraps window.fetch to automatically attach Authorization headers
 * from authStore for any internal REST API endpoints (/api/v1/...).
 */
export async function apiFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
	const authHeaders = authStore.getHeaders();
	const headers = new Headers(init?.headers);

	for (const [key, value] of Object.entries(authHeaders)) {
		if (!headers.has(key)) {
			headers.set(key, value);
		}
	}

	return fetch(input, {
		...init,
		headers
	});
}
