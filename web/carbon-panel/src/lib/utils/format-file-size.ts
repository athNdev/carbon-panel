/**
 * Human-readable file size. Guards against non-numeric, non-finite, and
 * negative input (which would flow into Math.log and render "NaN undefined").
 */
export function formatFileSize(bytes: number): string {
	if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
	const k = 1024;
	const sizes = ['B', 'KB', 'MB', 'GB'];
	const i = Math.floor(Math.log(bytes) / Math.log(k));
	return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

/**
 * Parse a size value coming from an RPC response. Non-numeric or negative
 * values collapse to 0 so downstream formatting never sees NaN.
 */
export function parseFileSize(value: unknown): number {
	const parsed = Number(value);
	if (!Number.isFinite(parsed) || parsed < 0) return 0;
	return parsed;
}
