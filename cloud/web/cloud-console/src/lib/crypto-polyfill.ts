import { sha1 } from '@noble/hashes/sha1';
import { sha256 } from '@noble/hashes/sha256';

// Browsers restrict window.crypto.subtle to Secure Contexts (HTTPS / localhost).
// When accessing the Carbon Cloud console over LAN HTTP (e.g. http://192.168.0.118:3000),
// Clerk attempts to compute digests (SHA-1 / SHA-256) for suffixed cookies and throws:
// "Suffixed cookie failed due to Cannot read properties of undefined (reading 'digest')"
// This polyfill provides standard SHA-1 and SHA-256 digests in non-secure contexts.
if (typeof window !== 'undefined') {
	const g = window as any;
	if (!g.crypto) {
		g.crypto = {};
	}
	if (!g.crypto.subtle) {
		g.crypto.subtle = {
			digest: async (algorithm: any, data: BufferSource): Promise<ArrayBuffer> => {
				const rawName = typeof algorithm === 'string' ? algorithm : algorithm?.name;
				const name = String(rawName || '').toUpperCase();

				let u8: Uint8Array;
				if (data instanceof Uint8Array) {
					u8 = data;
				} else if (ArrayBuffer.isView(data)) {
					u8 = new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
				} else {
					u8 = new Uint8Array(data as ArrayBuffer);
				}

				if (name === 'SHA-1' || name === 'SHA1') {
					const res = sha1(u8);
					return res.buffer.slice(res.byteOffset, res.byteOffset + res.byteLength);
				}
				if (name === 'SHA-256' || name === 'SHA256') {
					const res = sha256(u8);
					return res.buffer.slice(res.byteOffset, res.byteOffset + res.byteLength);
				}

				throw new Error(`Unsupported digest algorithm in polyfill: ${name}`);
			}
		};
	}
}
