// SPDX-License-Identifier: MPL-2.0
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

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

test('robustness: settings only honours query tabs visible to the user', () => {
	const p = path.resolve(import.meta.dirname, '../../routes/settings/+page.svelte');
	const c = fs.readFileSync(p, 'utf-8');
	assert.ok(
		c.includes('settingsTabs.some((tab) => tab.id === queryTab)'),
		'settings should validate the query tab against the visible tab set'
	);
	assert.ok(
		c.includes("settingsTabs[0]?.id ?? ''"),
		'settings should fall back to the first visible tab'
	);
});

test('robustness: server detail exposes first-load error state and retry', () => {
	const p = path.resolve(import.meta.dirname, '../../routes/servers/[id]/+page.svelte');
	const c = fs.readFileSync(p, 'utf-8');
	assert.ok(c.includes('loadError'), 'server detail should track loadError');
	assert.ok(c.includes('CarbonErrorState'), 'server detail should render CarbonErrorState');
	assert.ok(
		c.includes('onretry={() => loadServer()}'),
		'server detail should retry via loadServer'
	);
});

test('robustness: server backups distinguishes load errors from empty results', () => {
	const p = path.resolve(import.meta.dirname, 'server-backups.svelte');
	const c = fs.readFileSync(p, 'utf-8');
	assert.ok(c.includes('loadError'), 'backups should track loadError');
	assert.ok(c.includes('CarbonErrorState'), 'backups should render CarbonErrorState');
	assert.ok(c.includes('onretry={loadBackups}'), 'backups should retry via loadBackups');
});

test('robustness: server players distinguishes load errors from empty results', () => {
	const p = path.resolve(import.meta.dirname, 'server-players.svelte');
	const c = fs.readFileSync(p, 'utf-8');
	assert.ok(c.includes('loadError'), 'players should track loadError');
	assert.ok(c.includes('CarbonErrorState'), 'players should render CarbonErrorState');
	assert.ok(c.includes('onretry={loadPlayers}'), 'players should retry via loadPlayers');
});

test('robustness: server tasks distinguishes load errors from empty results', () => {
	const p = path.resolve(import.meta.dirname, 'server-tasks.svelte');
	const c = fs.readFileSync(p, 'utf-8');
	assert.ok(c.includes('loadError'), 'tasks should track loadError');
	assert.ok(c.includes('CarbonErrorState'), 'tasks should render CarbonErrorState');
	assert.ok(c.includes('onretry={loadTasks}'), 'tasks should retry via loadTasks');
});
