// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.


import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import { execSync } from 'child_process';
import { readFileSync, existsSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';

// Get version from env for CI builds || version file || git tag for local
function getVersion() {
	if (process.env.APP_VERSION) {
		return process.env.APP_VERSION;
	}

	// Check version file stored in home
	try {
		for (const name of ['.carbon-panel', '.CARBONPANEL']) {
			const versionFile = join(homedir(), name);
			if (existsSync(versionFile)) {
				const version = readFileSync(versionFile, 'utf8').trim();
				if (version) return version;
			}
		}
	} catch {
		/* ignore */
	}

	// Derive version from git tags
	try {
		const version = execSync('git describe --tags --always').toString().trim();
		if (version) return version;
	} catch {
		/* ignore */
	}

	return 'dev'; // default
}

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	define: {
		__APP_VERSION__: JSON.stringify(getVersion())
	},
	build: {
		sourcemap: false,
		rollupOptions: {
			maxParallelFileOps: 2,
			cache: false
		}
	},
	server: {
		host: '0.0.0.0',
		port: 5174,
		strictPort: true,
		proxy: {
			'/api': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			// Proxy Connect RPC service paths
			'/carbonpanel.v1': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			// Proxy gRPC reflection service
			'/grpc.reflection': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			// Proxy Connect service paths
			'/connect': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			// Proxy WebSocket connections
			'/ws': {
				target: 'ws://localhost:8080',
				ws: true,
				changeOrigin: true
			}
		}
	},
	optimizeDeps: {
		include: ['monaco-editor']
	}
});
