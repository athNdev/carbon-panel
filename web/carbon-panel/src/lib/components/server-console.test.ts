import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

test('ServerConsole does not depend on Tooltip context and has accessible tooltips', () => {
	const consolePath = path.resolve(import.meta.dirname, 'server-console.svelte');
	const content = fs.readFileSync(consolePath, 'utf-8');

	// Must never import or use shadcn/bits-ui Tooltip inside ServerConsole.
	// Tooltip.Root throws Context "Tooltip.Provider" not found if mounted outside a provider,
	// which previously broke the Console tab and wedged Svelte 5's reactive effect scheduler.
	assert.ok(
		!content.includes('Tooltip.Root'),
		'ServerConsole must not use Tooltip.Root (causes Context "Tooltip.Provider" not found)'
	);
	assert.ok(!content.includes('Tooltip.Trigger'), 'ServerConsole must not use Tooltip.Trigger');
	assert.ok(!content.includes('Tooltip.Content'), 'ServerConsole must not use Tooltip.Content');
	assert.ok(
		!content.includes('$lib/components/ui/tooltip'),
		'ServerConsole must not import tooltip UI primitives'
	);

	// Ensure action buttons have both aria-label and title for accessibility and native tooltips
	assert.ok(
		content.includes('aria-label="Refresh logs"') && content.includes('title="Refresh logs"'),
		'Refresh logs button must declare aria-label and title'
	);
	assert.ok(
		content.includes('aria-label="Upload logs to mclo.gs"') &&
			content.includes('title="Upload logs to mclo.gs"'),
		'Upload logs button must declare aria-label and title'
	);
	assert.ok(
		content.includes('aria-label="Download raw log"') &&
			content.includes('title="Download raw log"'),
		'Download logs button must declare aria-label and title'
	);
	assert.ok(
		content.includes('aria-label="Clear log buffer"') &&
			content.includes('title="Clear log buffer"'),
		'Clear logs button must declare aria-label and title'
	);
});

test('Root layout provides global Tooltip.Provider safety net', () => {
	const layoutPath = path.resolve(import.meta.dirname, '../../routes/+layout.svelte');
	const content = fs.readFileSync(layoutPath, 'utf-8');

	assert.ok(
		content.includes("import * as Tooltip from '$lib/components/ui/tooltip/index.js';") ||
			content.includes("from '$lib/components/ui/tooltip"),
		'Root layout must import Tooltip provider'
	);
	assert.ok(
		content.includes('<Tooltip.Provider>'),
		'Root layout must wrap application with <Tooltip.Provider>'
	);
});

test('Server detail page wraps tab panels with svelte:boundary to prevent tab wedging', () => {
	const serverPagePath = path.resolve(
		import.meta.dirname,
		'../../routes/servers/[id]/+page.svelte'
	);
	const content = fs.readFileSync(serverPagePath, 'utf-8');

	assert.ok(
		content.includes('<svelte:boundary>'),
		'Server detail page must wrap tab switching in <svelte:boundary>'
	);
	assert.ok(
		content.includes('{#snippet failed(error, reset)}'),
		'Server detail page must declare failed fallback snippet for svelte:boundary'
	);
	assert.ok(
		content.includes("<ServerConsole {server} active={activeTab === 'console'} />"),
		'Server detail page must render ServerConsole'
	);
});
