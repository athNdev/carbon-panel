<script lang="ts">
	import { onMount } from 'svelte';
	import ServerConfiguration from '$lib/components/server-configuration.svelte';
	import ScrollToTop from '$lib/components/scroll-to-top.svelte';
	import UserSettings from '$lib/components/user-settings.svelte';
	import RoleSettings from '$lib/components/role-settings.svelte';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { toast } from 'svelte-sonner';
	import {
		Settings,
		Globe,
		Server,
		Shield,
		HelpCircle,
		ScrollText,
		Users,
		KeyRound,
		Key,
		Layers
	} from '@lucide/svelte';
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
	let saving = $state(false);

	let showSettings = $derived($canReadSettings);
	let showUsers = $derived($canReadUsers && $authEnabled);
	let showRoles = $derived($canReadRoles && $authEnabled);

	let queryTab = $derived(page.url.searchParams.get('tab'));

	// Pick the first visible tab as default or read from URL
	let activeTab = $state('');
	$effect(() => {
		if (queryTab && ['server-config', 'api-keys', 'routing', 'nodes', 'auth', 'logs', 'support', 'users', 'roles'].includes(queryTab)) {
			activeTab = queryTab;
		} else if (typeof window !== 'undefined' && (window.location.hash === '#cfApiKey' || window.location.hash === '#api-keys')) {
			activeTab = 'api-keys';
		} else if (!activeTab) {
			if (showSettings) activeTab = 'server-config';
			else if (showUsers) activeTab = 'users';
			else if (showRoles) activeTab = 'roles';
		}
	});

	async function loadGlobalSettings() {
		loading = true;
		try {
			const response = await rpcClient.config.getGlobalSettings({});
			globalConfig = response.categories;
		} catch (error) {
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

<div class="min-h-full flex-1 space-y-6 bg-[#161616] text-[#f4f4f4] p-6 lg:p-8">
	<div class="flex items-center justify-between border-b border-[#393939] pb-6">
		<div class="flex items-center gap-4">
			<div
				class="flex h-14 w-14 shrink-0 items-center justify-center rounded-none border border-[#393939] bg-[#262626] text-[#0f62fe]"
			>
				<Settings class="h-7 w-7 text-[#0f62fe]" />
			</div>
			<div class="space-y-1">
				<h1 class="font-sans text-2xl font-light tracking-tight text-[#f4f4f4]">
					Settings
				</h1>
				<p class="text-xs text-[#a8a8a8]">
					Configure Carbon Panel and default server settings
				</p>
			</div>
		</div>
	</div>

	<Tabs value={activeTab} onValueChange={(v) => (activeTab = v || activeTab)} class="space-y-6">
		<TabsList class="w-full justify-start gap-0 h-[41px] border-b border-[#393939] bg-transparent p-0 rounded-none overflow-x-auto overflow-y-hidden">
			{#if showSettings}
				<TabsTrigger value="server-config" class="flex items-center gap-2 px-4 rounded-none">
					<Server class="h-4 w-4" />
					Server Defaults
				</TabsTrigger>
				<TabsTrigger value="api-keys" class="flex items-center gap-2 px-4 rounded-none">
					<Key class="h-4 w-4" />
					API Keys
				</TabsTrigger>
				<TabsTrigger value="routing" class="flex items-center gap-2 px-4 rounded-none">
					<Globe class="h-4 w-4" />
					Routing
				</TabsTrigger>
				<TabsTrigger value="nodes" class="flex items-center gap-2 px-4 rounded-none">
					<Layers class="h-4 w-4" />
					Docker Nodes
				</TabsTrigger>
				<TabsTrigger value="auth" class="flex items-center gap-2 px-4 rounded-none">
					<Shield class="h-4 w-4" />
					Auth
				</TabsTrigger>
				<TabsTrigger value="logs" class="flex items-center gap-2 px-4 rounded-none">
					<ScrollText class="h-4 w-4" />
					Logs
				</TabsTrigger>
				<TabsTrigger value="support" class="flex items-center gap-2 px-4 rounded-none">
					<HelpCircle class="h-4 w-4" />
					Support
				</TabsTrigger>
			{/if}
			{#if showUsers}
				<TabsTrigger value="users" class="flex items-center gap-2 px-4 rounded-none">
					<Users class="h-4 w-4" />
					Users
				</TabsTrigger>
			{/if}
			{#if showRoles}
				<TabsTrigger value="roles" class="flex items-center gap-2 px-4 rounded-none">
					<KeyRound class="h-4 w-4" />
					Roles
				</TabsTrigger>
			{/if}
		</TabsList>

		{#if showSettings}
			<TabsContent value="server-config" class="space-y-4">
				{#if loading}
					<div class="border border-[#393939] bg-[#262626] p-16">
						<div class="flex items-center justify-center">
							<div class="space-y-3 text-center">
								<div
									class="mx-auto flex h-10 w-10 items-center justify-center border border-[#393939] bg-[#161616]"
								>
									<Settings class="h-5 w-5 text-[#0f62fe] animate-spin" />
								</div>
								<div class="text-xs font-sans text-[#a8a8a8]">Loading settings...</div>
							</div>
						</div>
					</div>
				{:else}
					<ServerConfiguration config={globalConfig} onSave={saveGlobalSettings} {saving} />
				{/if}
			</TabsContent>

			<TabsContent value="api-keys" class="space-y-4">
				<ApiKeysSettings />
			</TabsContent>

			<TabsContent value="routing" class="space-y-4">
				<RoutingSettings />
			</TabsContent>

			<TabsContent value="nodes" class="space-y-4">
				<NodeSettings />
			</TabsContent>

			<TabsContent value="auth" class="space-y-4">
				<AuthSettings />
			</TabsContent>

			<TabsContent value="logs" class="space-y-4">
				<LogsSettings />
			</TabsContent>

			<TabsContent value="support" class="space-y-4">
				<SupportSettings />
			</TabsContent>
		{/if}

		{#if showUsers}
			<TabsContent value="users" class="space-y-4">
				<UserSettings />
			</TabsContent>
		{/if}

		{#if showRoles}
			<TabsContent value="roles" class="space-y-4">
				<RoleSettings />
			</TabsContent>
		{/if}
	</Tabs>
</div>

<ScrollToTop />
