// SPDX-License-Identifier: MPL-2.0
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const routes = [
	'../../routes/+page.svelte',
	'../../routes/modules/ActiveModules.svelte',
	'../../routes/profile/+page.svelte',
	'../../routes/settings/+page.svelte'
];

test('robustness: dashboard exposes load error state and retry', () => {
	const p = path.resolve(import.meta.dirname, '../../routes/+page.svelte');
	const c = fs.readFileSync(p, 'utf-8');
	assert.ok(c.includes('loadError'), 'dashboard should track loadError');
	assert.ok(c.includes('CarbonErrorState'), 'dashboard should render CarbonErrorState');
	assert.ok(
		c.includes('onretry={refreshDashboard}'),
		'dashboard should retry via refreshDashboard'
	);
});

test('robustness: ActiveModules exposes load error state and retry', () => {
	const p = path.resolve(import.meta.dirname, '../../routes/modules/ActiveModules.svelte');
	const c = fs.readFileSync(p, 'utf-8');
	assert.ok(c.includes('loadError'), 'ActiveModules should track loadError');
	assert.ok(c.includes('CarbonErrorState'), 'ActiveModules should render CarbonErrorState');
	assert.ok(
		c.includes('onretry={() => loadModules()}'),
		'ActiveModules should retry via loadModules'
	);
});

test('robustness: profile tracks API token load errors and retry', () => {
	const p = path.resolve(import.meta.dirname, '../../routes/profile/+page.svelte');
	const c = fs.readFileSync(p, 'utf-8');
	assert.ok(c.includes('tokensError'), 'profile should track tokensError');
	assert.ok(c.includes('CarbonErrorState'), 'profile should render CarbonErrorState');
	assert.ok(c.includes('onretry={loadTokens}'), 'profile should retry via loadTokens');
});

test('robustness: settings tracks global settings load errors and retry', () => {
	const p = path.resolve(import.meta.dirname, '../../routes/settings/+page.svelte');
	const c = fs.readFileSync(p, 'utf-8');
	assert.ok(c.includes('globalSettingsError'), 'settings should track globalSettingsError');
	assert.ok(c.includes('CarbonErrorState'), 'settings should render CarbonErrorState');
	assert.ok(
		c.includes('onretry={loadGlobalSettings}'),
		'settings should retry via loadGlobalSettings'
	);
});
