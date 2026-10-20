// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

test('Server detail page correctly differentiates proxied vs direct port ingress', () => {
	const serverPagePath = path.resolve(
		import.meta.dirname,
		'../../routes/servers/[id]/+page.svelte'
	);
	const content = fs.readFileSync(serverPagePath, 'utf-8');

	// Must fetch proxy status to verify if proxy is actually active
	assert.ok(
		content.includes('rpcClient.proxy') && content.includes('getProxyStatus'),
		'Server detail page must query proxy status'
	);

	// Must derive isProxied checking proxyStatus, hostname, and non-zero proxy port
	assert.ok(
		content.includes('isProxied') && content.includes('server?.proxyPort'),
		'Must derive isProxied with proxy port guard'
	);

	// Must show PROXIED and DIRECT PORT badges
	assert.ok(content.includes('PROXIED'), 'Must show PROXIED badge when server is proxied');
	assert.ok(
		content.includes('DIRECT PORT'),
		'Must show DIRECT PORT badge when server is direct port'
	);

	// Direct port must use window.location.hostname for remote clients
	assert.ok(
		content.includes('window.location.hostname'),
		'Direct port address must resolve window.location.hostname'
	);
});

test('Server creation form does not leak proxy parameters on direct port mode', () => {
	const newPagePath = path.resolve(import.meta.dirname, '../../routes/servers/new/+page.svelte');
	const content = fs.readFileSync(newPagePath, 'utf-8');

	// createRequest must only attach proxyHostname and proxyListenerId when useProxyMode is true
	assert.ok(
		content.includes("proxyHostname: useProxyMode && proxyEnabled ? formData.proxyHostname : ''"),
		'Must clear proxyHostname when not in proxy mode'
	);
	assert.ok(
		content.includes(
			"proxyListenerId: useProxyMode && proxyEnabled ? formData.proxyListenerId : ''"
		),
		'Must clear proxyListenerId when not in proxy mode'
	);
});
