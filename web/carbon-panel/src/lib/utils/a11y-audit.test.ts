// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

test('all <th> elements in svelte views declare scope attribute', () => {
	const srcDir = path.resolve(import.meta.dirname, '../..');

	function scanDir(dir: string, fileList: string[] = []): string[] {
		const files = fs.readdirSync(dir);
		for (const file of files) {
			const fullPath = path.join(dir, file);
			const stat = fs.statSync(fullPath);
			if (stat.isDirectory()) {
				scanDir(fullPath, fileList);
			} else if (file.endsWith('.svelte')) {
				fileList.push(fullPath);
			}
		}
		return fileList;
	}

	const svelteFiles = scanDir(srcDir);
	const violations: string[] = [];

	for (const file of svelteFiles) {
		const content = fs.readFileSync(file, 'utf-8');
		// Find <th tags that are not comments or type annotations
		const thRegex = /<th(?:\s+[^>]*?)?>/gi;
		let match: RegExpExecArray | null;
		while ((match = thRegex.exec(content)) !== null) {
			const tag = match[0];
			if (!tag.includes('scope=')) {
				violations.push(`${path.relative(srcDir, file)}: ${tag}`);
			}
		}
	}

	assert.deepEqual(
		violations,
		[],
		`Found <th> elements without scope attribute: ${JSON.stringify(violations, null, 2)}`
	);
});

test('CarbonModal enforces dialog semantics, keyboard trap, and ARIA attributes', () => {
	const modalPath = path.resolve(import.meta.dirname, '../components/carbon/CarbonModal.svelte');
	const content = fs.readFileSync(modalPath, 'utf-8');

	assert.ok(content.includes('role="dialog"'), 'CarbonModal must have role="dialog"');
	assert.ok(content.includes('aria-modal="true"'), 'CarbonModal must have aria-modal="true"');
	assert.ok(content.includes('aria-labelledby='), 'CarbonModal must have aria-labelledby');
	assert.ok(content.includes('aria-describedby='), 'CarbonModal must have aria-describedby');
	assert.ok(content.includes("event.key === 'Escape'"), 'CarbonModal must handle Escape key');
	assert.ok(content.includes("event.key === 'Tab'"), 'CarbonModal must handle Tab focus trapping');
	assert.ok(
		content.includes('<svelte:window onkeydown={handleKeydown} />'),
		'CarbonModal must listen for global keydown'
	);
});

test('ServerBackups guards restore and delete with CarbonConfirm modals', () => {
	const backupsPath = path.resolve(import.meta.dirname, '../components/server-backups.svelte');
	const content = fs.readFileSync(backupsPath, 'utf-8');

	assert.ok(content.includes('CarbonConfirm'), 'ServerBackups must import and use CarbonConfirm');
	assert.ok(content.includes('restoreConfirmOpen'), 'ServerBackups must track restore confirmation state');
	assert.ok(content.includes('deleteConfirmOpen'), 'ServerBackups must track delete confirmation state');
	assert.ok(content.includes('title="Restore backup?"'), 'ServerBackups must confirm before restoring');
	assert.ok(content.includes('title="Delete backup?"'), 'ServerBackups must confirm before deleting');
	assert.ok(content.includes('promptRestore(backup)'), 'Restore action button must trigger confirmation prompt');
	assert.ok(content.includes('promptDelete(backup)'), 'Delete action button must trigger confirmation prompt');
});

test('Root layout mounts Toaster component with sonner wrapper for live announcements', () => {
	const layoutPath = path.resolve(import.meta.dirname, '../../routes/+layout.svelte');
	const layoutContent = fs.readFileSync(layoutPath, 'utf-8');

	assert.ok(
		layoutContent.includes("import { Toaster } from '$lib/components/ui/sonner'"),
		'Root layout must import Toaster'
	);
	assert.ok(
		layoutContent.includes('<Toaster'),
		'Root layout must render <Toaster /> component'
	);

	const sonnerPath = path.resolve(import.meta.dirname, '../components/ui/sonner/sonner.svelte');
	const sonnerContent = fs.readFileSync(sonnerPath, 'utf-8');

	assert.ok(
		sonnerContent.includes("from 'svelte-sonner'"),
		'Sonner wrapper must import from svelte-sonner'
	);
	assert.ok(
		sonnerContent.includes('{...restProps}'),
		'Sonner component must forward props to preserve aria attributes'
	);
});
