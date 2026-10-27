<!--
SPDX-License-Identifier: MIT AND MPL-2.0
This file includes DiscoPanel material under the MIT License,
Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
and still applies to that inherited material.
Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
If a copy of the MPL was not distributed with this file, You can obtain
one at https://mozilla.org/MPL/2.0/.
-->

<script lang="ts">
	import { onMount } from 'svelte';
	import ServerConfiguration from '$lib/components/server-configuration.svelte';
	import ScrollToTop from '$lib/components/scroll-to-top.svelte';
	import UserSettings from '$lib/components/user-settings.svelte';
	import RoleSettings from '$lib/components/role-settings.svelte';
	import { CarbonTabs, CarbonInlineLoading, CarbonErrorState } from '$lib/components/carbon';
	import { toast } from 'svelte-sonner';
	import { Settings } from '@lucide/svelte';
	import type { ConfigCategory } from '$lib/proto/carbonpanel/v1/config_pb';
	import { rpcClient } from '$lib/api/rpc-client';
	import RoutingSettings from '$lib/components/routing-settings.svelte';
	import NodeSettings from '$lib/components/node-settings.svelte';
	import AuthSettings from '$lib/components/auth-settings.svelte';
	import SupportSettings from '$lib/components/support-settings.svelte';
	import LogsSettings from '$lib/components/logs-settings.svelte';
	import ApiKeysSettings from '$lib/components/api-keys-settings.svelte';
	import { canReadSettings, canReadUsers, canReadRoles, authEnabled } from '$lib/stores/auth';
	import { page } from '$app/state';

	let globalConfig = $state<ConfigCategory[]>([]);
	let loading = $state(true);
	let globalSettingsError = $state<string | null>(null);
	let saving = $state(false);

	let showSettings = $derived($canReadSettings);
	let showUsers = $derived($canReadUsers && $authEnabled);
	let showRoles = $derived($canReadRoles && $authEnabled);

	let settingsTabs = $derived([
		...(showSettings
			? [
					{ id: 'server-config', label: 'Server Defaults' },
					{ id: 'api-keys', label: 'API Keys' },
					{ id: 'routing', label: 'Routing' },
					{ id: 'nodes', label: 'Docker Nodes' },
					{ id: 'auth', label: 'Auth' },
					{ id: 'logs', label: 'Logs' },
					{ id: 'support', label: 'Support' }
				]
			: []),
		...(showUsers ? [{ id: 'users', label: 'Users' }] : []),
		...(showRoles ? [{ id: 'roles', label: 'Roles' }] : [])
	]);

	let queryTab = $derived(page.url.searchParams.get('tab'));

	// Pick the requested tab only when it is visible, otherwise fall back to the first visible tab.
	let activeTab = $state('');
	$effect(() => {
		if (queryTab && settingsTabs.some((tab) => tab.id === queryTab)) {
			activeTab = queryTab;
		} else if (
			typeof window !== 'undefined' &&
			showSettings &&
			(window.location.hash === '#cfApiKey' || window.location.hash === '#api-keys')
		) {
			activeTab = 'api-keys';
		} else if (!settingsTabs.some((tab) => tab.id === activeTab)) {
			activeTab = settingsTabs[0]?.id ?? '';
		}
	});

	async function loadGlobalSettings() {
		loading = true;
		globalSettingsError = null;
		try {
			const response = await rpcClient.config.getGlobalSettings({});
			globalConfig = response.categories;
		} catch (error) {
			globalSettingsError =
				error instanceof Error ? error.message : 'Failed to load global settings';
			toast.error('Failed to load global settings');
			console.error(error);
		} finally {
			loading = false;
		}
	}

	async function saveGlobalSettings(updates: Record<string, string>) {
		saving = true;
		try {
			const response = await rpcClient.config.updateGlobalSettings({
				updates
			});

			globalConfig = response.categories;
			toast.success('Global settings saved successfully');
		} catch (error) {
			toast.error('Failed to save global settings');
			console.error(error);
		} finally {
			saving = false;
		}
	}

	onMount(() => {
		if (showSettings) {
			loadGlobalSettings();
		} else {
			loading = false;
		}
	});
</script>

<div class="min-h-full flex-1 space-y-6 bg-[#161616] p-6 text-[#f4f4f4] lg:p-8">
	<div class="flex items-center justify-between border-b border-[#393939] pb-6">
		<div class="flex items-center gap-4">
			<div
				class="flex h-14 w-14 shrink-0 items-center justify-center rounded-none border border-[#393939] bg-[#262626] text-accent-text"
			>
				<Settings class="h-7 w-7 text-accent-text" />
			</div>
			<div class="space-y-1">
				<h1 class="font-sans text-2xl font-light tracking-tight text-[#f4f4f4]">Settings</h1>
				<p class="text-xs text-[#a8a8a8]">Configure Carbon Panel and default server settings</p>
			</div>
		</div>
	</div>

	<div class="space-y-6">
		<CarbonTabs
			tabs={settingsTabs}
			bind:selectedTab={activeTab}
			class="overflow-x-auto overflow-y-hidden"
		/>

		{#if activeTab === 'server-config' && showSettings}
			<div class="motion-fade-in space-y-4">
				{#if loading}
					<div class="flex items-center justify-center border border-[#393939] bg-[#262626] p-16">
						<CarbonInlineLoading description="Loading settings..." />
					</div>
				{:else if globalSettingsError}
					<CarbonErrorState
						title="Failed to load settings"
						message={globalSettingsError}
						onretry={loadGlobalSettings}
					/>
				{:else}
					<ServerConfiguration config={globalConfig} onSave={saveGlobalSettings} {saving} />
				{/if}
			</div>
		{:else if activeTab === 'api-keys' && showSettings}
			<div class="motion-fade-in space-y-4">
				<ApiKeysSettings />
			</div>
		{:else if activeTab === 'routing' && showSettings}
			<div class="motion-fade-in space-y-4">
				<RoutingSettings />
			</div>
		{:else if activeTab === 'nodes' && showSettings}
			<div class="motion-fade-in space-y-4">
				<NodeSettings />
			</div>
		{:else if activeTab === 'auth' && showSettings}
			<div class="motion-fade-in space-y-4">
				<AuthSettings />
			</div>
		{:else if activeTab === 'logs' && showSettings}
			<div class="motion-fade-in space-y-4">
				<LogsSettings />
			</div>
		{:else if activeTab === 'support' && showSettings}
			<div class="motion-fade-in space-y-4">
				<SupportSettings />
			</div>
		{:else if activeTab === 'users' && showUsers}
			<div class="motion-fade-in space-y-4">
				<UserSettings />
			</div>
		{:else if activeTab === 'roles' && showRoles}
			<div class="motion-fade-in space-y-4">
				<RoleSettings />
			</div>
		{:else}
			<CarbonErrorState
				title="Settings unavailable"
				message="This section needs permissions that are not loaded, or the page data failed to load. Retry, or contact an administrator if you lack access."
				onretry={() => window.location.reload()}
			/>
		{/if}
	</div>
</div>

<ScrollToTop />
