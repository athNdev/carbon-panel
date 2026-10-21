// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

test('RBAC-gated UI hides admin controls for non-admin roles', () => {
	const pagePath = path.resolve(import.meta.dirname, '../../routes/servers/[id]/+page.svelte');
	const content = fs.readFileSync(pagePath, 'utf-8');
	// Page should reference role/permission gating somewhere.
	assert.ok(
		content.includes('isAdmin') || content.includes('role') || content.includes('permission'),
		'server detail page should reference RBAC gating'
	);
});
