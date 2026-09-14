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
