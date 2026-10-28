import { authStore } from '$lib/stores/auth';

export function triggerStreamDownload(sessionId: string, filename: string) {
	const token = authStore.getToken();
	if (token) {
		document.cookie = `cp_session=${encodeURIComponent(token)}; Path=/; SameSite=Strict`;
	}
	const url = `/api/v1/download/${sessionId}`;
	const anchor = document.createElement('a');
	anchor.href = url;
	anchor.download = filename;
	anchor.click();
}
