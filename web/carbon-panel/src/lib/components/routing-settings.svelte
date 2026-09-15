<script lang="ts">
	import { onMount } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { ProxyListener } from '$lib/proto/carbonpanel/v1/common_pb';
	import type { ProxyListenerWithCount, ProxyRoute } from '$lib/proto/carbonpanel/v1/proxy_pb';
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Button } from '$lib/components/ui/button';
	import { Switch } from '$lib/components/ui/switch';
	import { Badge } from '$lib/components/ui/badge';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { CarbonInlineLoading } from '$lib/components/carbon';
	import { toast } from 'svelte-sonner';
	import {
		Save,
		Plus,
		Trash2,
		Loader2,
		AlertCircle,
		Server,
		Activity,
		CheckCircle2,
		XCircle,
		Network,
		Info,
		Edit,
		Star
	} from '@lucide/svelte';

	let loading = $state(true);
	let saving = $state(false);
	let proxyEnabled = $state(false);
	let baseURL = $state('');
	let listenersWithCount = $state<ProxyListenerWithCount[]>([]);
	let editingListener = $state<ProxyListener | null>(null);
	let newListener = $state<Partial<ProxyListener>>({
		port: 25565,
		name: '',
		description: '',
		enabled: true,
		isDefault: false
	});
	let portError = $state('');
	let activeRoutes = $state<ProxyRoute[]>([]);

	onMount(() => {
		loadAll();
	});

	async function loadAll() {
		loading = true;
		try {
			await Promise.all([loadProxyConfig(), loadListeners(), loadActiveRoutes()]);
		} finally {
			loading = false;
		}
	}

	async function loadProxyConfig() {
		try {
			const status = await rpcClient.proxy.getProxyStatus({});
			proxyEnabled = status.enabled;
			baseURL = status.baseUrl || '';
		} catch (_e) {
			toast.error('Failed to load proxy configuration');
		}
	}

	async function loadListeners() {
		try {
			const response = await rpcClient.proxy.getProxyListeners({});
			listenersWithCount = response.listeners;
			// Set default port for new listener
			if (listenersWithCount.length > 0) {
				const usedPorts = new Set(listenersWithCount.map((lwc) => lwc.listener?.port || 0));
				let nextPort = 25565;
				while (usedPorts.has(nextPort)) {
					nextPort++;
				}
				newListener.port = nextPort;
			}
		} catch (_e) {
			toast.error('Failed to load proxy listeners');
		}
	}

	async function loadActiveRoutes() {
		try {
			const response = await rpcClient.proxy.getProxyRoutes({});
			activeRoutes = response.routes;
		} catch (error) {
			console.error('Failed to load active routes:', error);
		}
	}

	function validatePort(port: number): boolean {
		portError = '';

		if (!port || port < 1 || port > 65535) {
			portError = 'Port must be between 1 and 65535';
			return false;
		}

		// Check if port is already used by another listener
		const existingListener = listenersWithCount.find(
			(lwc) => lwc.listener?.port === port && lwc.listener?.id !== editingListener?.id
		);
		if (existingListener) {
			portError = `Port ${port} is already used by listener "${existingListener.listener?.name}"`;
			return false;
		}

		return true;
	}

	async function saveProxyConfig() {
		saving = true;
		try {
			await rpcClient.proxy.updateProxyConfig({
				enabled: proxyEnabled,
				baseUrl: baseURL
			});

			toast.success('Proxy configuration saved');
			await loadAll();
		} catch (_e) {
			toast.error('Failed to save proxy configuration');
		} finally {
			saving = false;
		}
	}

	async function createListener() {
		if (!newListener.name) {
			toast.error('Listener name is required');
			return;
		}

		if (!validatePort(newListener.port!)) {
			return;
		}

		try {
			await rpcClient.proxy.createProxyListener({
				port: newListener.port!,
				name: newListener.name,
				description: newListener.description || '',
				enabled: newListener.enabled,
				isDefault: newListener.isDefault
			});

			toast.success(`Listener "${newListener.name}" created`);

			// Reset form
			newListener = {
				port: 25565,
				name: '',
				description: '',
				enabled: true,
				isDefault: false
			};

			await loadListeners();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to create listener');
		}
	}

	async function updateListener(listener: ProxyListener) {
		try {
			await rpcClient.proxy.updateProxyListener({
				id: listener.id,
				name: listener.name,
				description: listener.description,
				enabled: listener.enabled,
				isDefault: listener.isDefault
			});

			toast.success(`Listener "${listener.name}" updated`);
			editingListener = null;
			await loadListeners();
		} catch (_e) {
			toast.error('Failed to update listener');
		}
	}

	async function deleteListener(listenerWithCount: ProxyListenerWithCount) {
		const listener = listenerWithCount.listener;
		if (!listener) return;

		// Check server count from the response
		if (listenerWithCount.serverCount > 0) {
			toast.error(
				`Cannot delete: ${listenerWithCount.serverCount} servers are using this listener`
			);
			return;
		}

		if (confirm(`Delete listener "${listener.name}" on port ${listener.port}?`)) {
			try {
				await rpcClient.proxy.deleteProxyListener({ id: listener.id });
				toast.success(`Listener "${listener.name}" deleted`);
				await loadListeners();
			} catch (error: unknown) {
				toast.error(error instanceof Error ? error.message : 'Failed to delete listener');
			}
		}
	}

	async function setDefaultListener(listener: ProxyListener) {
		listener.isDefault = true;
		await updateListener(listener);
	}

	function getListenerStatus(
		listener: ProxyListener | undefined,
		serverCount: number
	): 'active' | 'inactive' | 'disabled' {
		if (!listener || !listener.enabled) return 'disabled';
		if (!proxyEnabled) return 'inactive';
		return serverCount > 0 ? 'active' : 'inactive';
	}

	function getStatusColor(status: string): string {
		switch (status) {
			case 'active':
				return 'text-green-500';
			case 'inactive':
				return 'text-yellow-500';
			case 'disabled':
				return 'text-gray-500';
			default:
				return 'text-gray-500';
		}
	}

	function getStatusIcon(status: string) {
		switch (status) {
			case 'active':
				return CheckCircle2;
			case 'disabled':
				return XCircle;
			default:
				return AlertCircle;
		}
	}
</script>

<div class="space-y-6">
	<!-- Global Proxy Configuration -->
	<div class="border border-[#393939] bg-[#262626] p-5 rounded-none shadow-none space-y-4">
		<div class="flex items-center justify-between border-b border-[#393939] pb-4">
			<div class="flex items-center gap-3">
				<Network class="h-5 w-5 text-[#0f62fe]" />
				<div>
					<h3 class="font-sans text-base font-normal text-[#f4f4f4]">Proxy Configuration</h3>
					<p class="text-xs text-[#a8a8a8]">Global proxy settings and base domain configuration</p>
				</div>
			</div>
			<Switch
				checked={proxyEnabled}
				onCheckedChange={(checked) => (proxyEnabled = checked)}
				disabled={loading || saving}
			/>
		</div>
		<div class="space-y-4">
			<div class="space-y-2">
				<Label for="base-url" class="text-xs text-[#c6c6c6]">Base Domain</Label>
				<Input
					id="base-url"
					type="text"
					bind:value={baseURL}
					placeholder="minecraft.example.com"
					disabled={saving || !proxyEnabled}
					class="rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4] placeholder:text-[#6f6f6f] h-10"
				/>
				<p class="text-xs text-[#8d8d8d]">
					Optional base domain that will be appended to server hostnames (e.g., "survival" becomes
					"survival.minecraft.example.com")
				</p>
			</div>

			<div class="flex justify-end">
				<button
					type="button"
					onclick={saveProxyConfig}
					disabled={saving}
					class="h-10 px-6 inline-flex items-center gap-2 rounded-none bg-[#0f62fe] hover:bg-[#0353e9] text-sm font-sans font-medium text-white cursor-pointer transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
				>
					{#if saving}
						<Loader2 class="h-4 w-4 animate-spin" />
						<span>Saving...</span>
					{:else}
						<Save class="h-4 w-4" />
						<span>Save Configuration</span>
					{/if}
				</button>
			</div>
		</div>
	</div>

	{#if loading}
		<div class="border border-[#393939] bg-[#262626] p-12 rounded-none flex items-center justify-center">
			<CarbonInlineLoading description="Loading proxy configuration..." />
		</div>
	{:else if !proxyEnabled}
		<Alert class="rounded-none border border-[#393939] bg-[#262626] text-[#c6c6c6]">
			<Info class="h-4 w-4 text-[#78a9ff]" />
			<AlertDescription class="text-xs text-[#a8a8a8]">
				Enable the proxy system to allow servers to use custom hostnames instead of direct port
				connections.
			</AlertDescription>
		</Alert>
	{:else}
		<!-- Proxy Listeners -->
		<div class="border border-[#393939] bg-[#262626] p-5 rounded-none shadow-none space-y-4">
			<div class="flex items-center justify-between border-b border-[#393939] pb-4">
				<div>
					<h3 class="font-sans text-base font-normal text-[#f4f4f4]">Proxy Listeners</h3>
					<p class="text-xs text-[#a8a8a8]">Configure individual proxy listening ports</p>
				</div>
				<span class="inline-flex items-center gap-1 rounded-none border border-[#525252] bg-[#161616] px-2 py-0.5 text-xs font-mono text-[#c6c6c6]">
					<Server class="h-3 w-3 text-[#78a9ff]" />
					{listenersWithCount.length}
					{listenersWithCount.length === 1 ? 'Listener' : 'Listeners'}
				</span>
			</div>
			<div class="space-y-4">
				<!-- Existing Listeners -->
				{#if listenersWithCount.length > 0}
					<div class="space-y-3">
						{#each listenersWithCount as lwc (lwc.listener?.id)}
							{@const listener = lwc.listener}
							{@const status = getListenerStatus(listener, lwc.serverCount)}
							{@const StatusIcon = getStatusIcon(status)}
							{#if listener}
								<div class="rounded-none border border-[#393939] bg-[#1e1e1e] p-4">
									{#if editingListener?.id === listener.id}
										<!-- Edit Mode -->
										<div class="space-y-3">
											<div class="grid grid-cols-2 gap-3">
												<div class="space-y-2">
													<Label class="text-xs text-[#c6c6c6]">Name</Label>
													<Input bind:value={editingListener.name} placeholder="Listener name" class="rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4]" />
												</div>
												<div class="space-y-2">
													<Label class="text-xs text-[#c6c6c6]">Port</Label>
													<Input type="number" value={listener.port} disabled class="rounded-none border border-[#393939] bg-[#161616] text-[#8d8d8d]" />
												</div>
											</div>
											<div class="space-y-2">
												<Label class="text-xs text-[#c6c6c6]">Description</Label>
												<Input
													bind:value={editingListener.description}
													placeholder="Optional description"
													class="rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4]"
												/>
											</div>
											<div class="flex items-center justify-between pt-2">
												<div class="flex items-center gap-4">
													<div class="flex items-center gap-2">
														<Switch
															checked={editingListener?.enabled ?? false}
															onCheckedChange={(checked) => {
																if (editingListener) editingListener.enabled = checked;
															}}
														/>
														<Label class="text-xs text-[#c6c6c6]">Enabled</Label>
													</div>
													<div class="flex items-center gap-2">
														<Switch
															checked={editingListener?.isDefault ?? false}
															onCheckedChange={(checked) => {
																if (editingListener) editingListener.isDefault = checked;
															}}
														/>
														<Label class="text-xs text-[#c6c6c6]">Default</Label>
													</div>
												</div>
												<div class="flex gap-2">
													<button
														type="button"
														class="h-8 px-3 inline-flex items-center rounded-none border border-[#393939] bg-[#353535] text-xs text-[#f4f4f4] hover:bg-[#393939] cursor-pointer"
														onclick={() => (editingListener = null)}
													>
														Cancel
													</button>
													<button
														type="button"
														class="h-8 px-4 inline-flex items-center rounded-none bg-[#0f62fe] text-white hover:bg-[#0353e9] text-xs font-medium cursor-pointer"
														onclick={() => updateListener(editingListener!)}
													>
														Save
													</button>
												</div>
											</div>
										</div>
									{:else}
										<!-- View Mode -->
										<div class="flex items-start justify-between">
											<div class="space-y-2">
												<div class="flex items-center gap-3">
													<StatusIcon class="h-4 w-4 {getStatusColor(status)}" />
													<span class="font-normal text-sm text-[#f4f4f4]">{listener.name}</span>
													<span class="rounded-none border border-[#525252] bg-[#161616] px-1.5 py-0.5 text-xs font-mono text-[#c6c6c6]">:{listener.port}</span>
													{#if listener.isDefault}
														<span class="inline-flex items-center gap-1 rounded-none border border-[#0f62fe] bg-[#0043ce]/20 px-1.5 py-0.5 text-xs font-mono text-[#78a9ff]">
															<Star class="h-3 w-3" />
															Default
														</span>
													{/if}
													{#if !listener.enabled}
														<span class="rounded-none border border-[#525252] bg-transparent px-1.5 py-0.5 text-xs font-mono text-[#8d8d8d]">Disabled</span>
													{/if}
												</div>

												{#if listener.description}
													<p class="text-xs text-[#a8a8a8]">{listener.description}</p>
												{/if}

												{#if lwc.serverCount > 0}
													<p class="text-xs text-[#8d8d8d]">
														{lwc.serverCount}
														{lwc.serverCount === 1 ? 'server' : 'servers'} using this listener
													</p>
												{:else}
													<p class="text-xs text-[#8d8d8d]">
														No servers using this listener
													</p>
												{/if}
											</div>

											<div class="flex gap-1">
												{#if !listener.isDefault}
													<button
														type="button"
														class="h-8 w-8 rounded-none inline-flex items-center justify-center hover:bg-[#353535] text-[#8d8d8d] hover:text-[#f4f4f4] cursor-pointer transition-colors"
														onclick={() => setDefaultListener(listener)}
														title="Set as default"
													>
														<Star class="h-4 w-4" />
													</button>
												{/if}
												<button
													type="button"
													class="h-8 w-8 rounded-none inline-flex items-center justify-center hover:bg-[#353535] text-[#8d8d8d] hover:text-[#f4f4f4] cursor-pointer transition-colors"
													onclick={() => (editingListener = { ...listener })}
												>
													<Edit class="h-4 w-4" />
												</button>
												{#if listenersWithCount.length > 1 && lwc.serverCount === 0}
													<button
														type="button"
														class="h-8 w-8 rounded-none inline-flex items-center justify-center hover:bg-[#da1e28]/20 text-[#8d8d8d] hover:text-[#ff8389] cursor-pointer transition-colors"
														onclick={() => deleteListener(lwc)}
													>
														<Trash2 class="h-4 w-4" />
													</button>
												{/if}
											</div>
										</div>
									{/if}
								</div>
							{/if}
						{/each}
					</div>
				{/if}

				<!-- Add New Listener -->
				<div class="border-t border-[#393939] pt-4">
					<h4 class="mb-3 text-sm font-normal text-[#f4f4f4]">Add New Listener</h4>
					<div class="space-y-3">
						<div class="grid grid-cols-2 gap-3">
							<div class="space-y-2">
								<Label class="text-xs text-[#c6c6c6]">Name</Label>
								<Input bind:value={newListener.name} placeholder="e.g., Secondary, Development" class="rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4]" />
							</div>
							<div class="space-y-2">
								<Label class="text-xs text-[#c6c6c6]">Port</Label>
								<Input
									type="number"
									bind:value={newListener.port}
									oninput={(e) => validatePort(Number(e.currentTarget.value))}
									class="rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4] {portError ? 'border-[#da1e28]' : ''}"
								/>
								{#if portError}
									<p class="text-xs text-[#ff8389]">{portError}</p>
								{/if}
							</div>
						</div>
						<div class="space-y-2">
							<Label class="text-xs text-[#c6c6c6]">Description (Optional)</Label>
							<Input
								bind:value={newListener.description}
								placeholder="Optional description for this listener"
								class="rounded-none border border-[#525252] bg-[#161616] text-[#f4f4f4]"
							/>
						</div>
						<div class="flex items-center justify-between pt-2">
							<div class="flex items-center gap-4">
								<div class="flex items-center gap-2">
									<Switch
										checked={newListener.enabled}
										onCheckedChange={(checked) => (newListener.enabled = checked)}
									/>
									<Label class="text-xs text-[#c6c6c6]">Enabled</Label>
								</div>
								{#if listenersWithCount.length === 0}
									<div class="flex items-center gap-2">
										<Switch
											checked={newListener.isDefault}
											onCheckedChange={(checked) => (newListener.isDefault = checked)}
										/>
										<Label class="text-xs text-[#c6c6c6]">Set as Default</Label>
									</div>
								{/if}
							</div>
							<button
								type="button"
								onclick={createListener}
								disabled={!newListener.name || !!portError}
								class="h-10 px-4 inline-flex items-center gap-2 rounded-none bg-[#0f62fe] hover:bg-[#0353e9] text-xs font-sans font-medium text-white cursor-pointer transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
							>
								<Plus class="h-4 w-4" />
								<span>Add Listener</span>
							</button>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- Active Routes -->
		{#if activeRoutes.length > 0}
			<div class="border border-[#393939] bg-[#262626] p-5 rounded-none shadow-none space-y-4">
				<div class="border-b border-[#393939] pb-4">
					<h3 class="font-sans text-base font-normal text-[#f4f4f4]">Active Routes</h3>
					<p class="text-xs text-[#a8a8a8]">Servers currently using proxy routing</p>
				</div>
				<div class="space-y-2">
					{#each activeRoutes as route (route.serverId)}
						<div class="flex items-center justify-between rounded-none border border-[#393939] bg-[#1e1e1e] p-3">
							<div class="flex items-center gap-3">
								<Activity class="h-4 w-4 {route.active ? 'text-[#42be65]' : 'text-[#8d8d8d]'}" />
								<div>
									<p class="font-mono text-xs text-[#f4f4f4]">{route.hostname}</p>
									<p class="text-xs text-[#8d8d8d]">
										Server: {route.serverId.slice(0, 8)}...
									</p>
								</div>
							</div>
							<span class="rounded-none border px-2 py-0.5 text-xs font-mono {route.active ? 'border-[#24a148] bg-[#24a148]/20 text-[#42be65]' : 'border-[#525252] bg-transparent text-[#8d8d8d]'}">
								{route.active ? 'Active' : 'Inactive'}
							</span>
						</div>
					{/each}
				</div>
			</div>
		{/if}
	{/if}
</div>
