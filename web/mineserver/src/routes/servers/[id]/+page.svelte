<script lang="ts">
	import { page } from '$app/state';
	import { onMount, untrack } from 'svelte';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { serversStore } from '$lib/stores/servers';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import ScrollToTop from '$lib/components/scroll-to-top.svelte';
	import { toast } from 'svelte-sonner';
	import {
		Play,
		Square,
		RotateCw,
		RefreshCcw,
		MoreVertical,
		Package,
		Activity,
		Loader2,
		Copy,
		ExternalLink,
		Trash2,
		Cpu,
		Info,
		Network,
		ArrowLeft,
		HardDrive,
		Terminal,
		Settings,
		Boxes,
		Files,
		ListTodo,
		Radio
	} from '@lucide/svelte';
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger
	} from '$lib/components/ui/dropdown-menu';
	import { create } from '@bufbuild/protobuf';
	import type { Timestamp } from '@bufbuild/protobuf/wkt';
	import type { Server } from '$lib/proto/mineserver/v1/common_pb';
	import { ServerStatus, ModLoader } from '$lib/proto/mineserver/v1/common_pb';
	import type { GetServerRoutingResponse } from '$lib/proto/mineserver/v1/proxy_pb';
	import {
		GetServerRequestSchema,
		DeleteServerRequestSchema,
		StartServerRequestSchema,
		StopServerRequestSchema,
		RestartServerRequestSchema,
		RecreateServerRequestSchema
	} from '$lib/proto/mineserver/v1/server_pb';
	import { formatBytes } from '$lib/utils';
	import { copyToClipboard as copyText } from '$lib/utils/clipboard';
	import { CarbonTag, CarbonButton } from '$lib/components/carbon';
	import ServerConsole from '$lib/components/server-console.svelte';
	import ServerConfiguration from '$lib/components/server-configuration.svelte';
	import ServerSettings from '$lib/components/server-settings.svelte';
	import ServerMods from '$lib/components/server-mods.svelte';
	import ServerFiles from '$lib/components/files/server-files.svelte';
	import ServerRouting from '$lib/components/server-routing.svelte';
	import ServerTasks from '$lib/components/server-tasks.svelte';
	import ServerModules from '$lib/components/server/ServerModules.svelte';

	let server = $state<Server | null>(null);
	let loading = $state(true);
	let actionLoading = $state(false);
	let serverId = $derived(page.params.id);
	let prevServerId = $state<string | undefined>(undefined);
	let activeTab = $state('overview');
	let routingInfo = $state<GetServerRoutingResponse | null>(null);

	let interval: ReturnType<typeof setInterval> | undefined;

	function timestampToDate(timestamp: Timestamp | undefined): Date {
		if (!timestamp) return new Date();
		return new Date(Number(timestamp.seconds) * 1000 + timestamp.nanos / 1_000_000);
	}

	onMount(() => {
		return () => {
			if (interval) clearInterval(interval);
		};
	});

	$effect(() => {
		if (serverId) {
			if (interval) clearInterval(interval);
			const prev = untrack(() => prevServerId);
			if (prev !== serverId) {
				untrack(() => {
					loading = true;
					prevServerId = serverId;
				});
			}
			loadServer(true);
			interval = setInterval(() => loadServer(true), 5000);
		}
	});

	async function loadServer(skipLoading = false) {
		if (!serverId) return;
		const requestedId = serverId;
		try {
			const request = create(GetServerRequestSchema, { id: requestedId });
			const callOptions = skipLoading ? silentCallOptions : undefined;
			const response = await rpcClient.server.getServer(request, callOptions);
			if (response.server && serverId === requestedId) {
				server = response.server;
				serversStore.updateServer(server);
				loading = false;
			}
		} catch {
			if (serverId === requestedId && !server) {
				toast.error('Failed to load server');
				loading = false;
			}
		}
	}

	async function handleServerAction(action: 'start' | 'stop' | 'restart' | 'recreate') {
		if (!server) return;

		actionLoading = true;
		try {
			switch (action) {
				case 'start': {
					const startRequest = create(StartServerRequestSchema, { id: server.id });
					await rpcClient.server.startServer(startRequest);
					toast.success('Server is starting...');
					break;
				}
				case 'stop': {
					const stopRequest = create(StopServerRequestSchema, { id: server.id });
					await rpcClient.server.stopServer(stopRequest);
					toast.success('Server is stopping...');
					break;
				}
				case 'restart': {
					const restartRequest = create(RestartServerRequestSchema, { id: server.id });
					await rpcClient.server.restartServer(restartRequest);
					toast.success('Server is restarting...');
					break;
				}
				case 'recreate': {
					const recreateRequest = create(RecreateServerRequestSchema, { id: server.id });
					await rpcClient.server.recreateServer(recreateRequest);
					toast.success('Server is recreating...');
					break;
				}
			}
			await loadServer(true);
		} catch (error) {
			toast.error(
				`Failed to ${action} server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = false;
		}
	}

	async function handleDeleteServer() {
		if (!server) return;

		const confirmed = confirm(
			`Are you sure you want to delete "${server.name}"?\n\nThis will:\n- Stop and remove the Docker container\n- Delete all server files and data\n- Remove all mods and configurations\n\nThis action cannot be undone!`
		);

		if (!confirmed) return;

		actionLoading = true;
		try {
			const deleteRequest = create(DeleteServerRequestSchema, { id: server.id });
			await rpcClient.server.deleteServer(deleteRequest);
			serversStore.removeServer(server.id);
			toast.success('Server deleted successfully');
			goto(resolve('/servers'));
		} catch (error) {
			toast.error(
				`Failed to delete server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = false;
		}
	}

	function getStatusTagType(
		status: ServerStatus
	): 'green' | 'gray' | 'blue' | 'red' | 'warm-gray' | 'purple' {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'green';
			case ServerStatus.STOPPED:
				return 'gray';
			case ServerStatus.STARTING:
			case ServerStatus.CREATING:
				return 'blue';
			case ServerStatus.RESTARTING:
				return 'purple';
			case ServerStatus.STOPPING:
				return 'warm-gray';
			case ServerStatus.ERROR:
			case ServerStatus.UNHEALTHY:
				return 'red';
			default:
				return 'gray';
		}
	}

	function getStatusDisplayName(status: ServerStatus): string {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'RUNNING';
			case ServerStatus.STOPPED:
				return 'STOPPED';
			case ServerStatus.STARTING:
				return 'STARTING';
			case ServerStatus.STOPPING:
				return 'STOPPING';
			case ServerStatus.ERROR:
				return 'ERROR';
			case ServerStatus.CREATING:
				return 'CREATING';
			case ServerStatus.RESTARTING:
				return 'RESTARTING';
			case ServerStatus.UNHEALTHY:
				return 'UNHEALTHY';
			default:
				return 'UNKNOWN';
		}
	}

	function copyConnection(text: string) {
		copyText(text);
		toast.success('Address copied to clipboard');
	}

	const subViewTabs = [
		{ id: 'overview', label: 'Overview', icon: Settings },
		{ id: 'console', label: 'Console', icon: Terminal },
		{ id: 'configuration', label: 'Configuration', icon: HardDrive },
		{ id: 'mods', label: 'Mods', icon: Package },
		{ id: 'modules', label: 'Modules', icon: Boxes },
		{ id: 'files', label: 'Files', icon: Files },
		{ id: 'tasks', label: 'Tasks', icon: ListTodo },
		{ id: 'routing', label: 'Routing', icon: Radio }
	];
</script>

{#if loading && !server}
	<div class="flex h-96 items-center justify-center bg-[#161616]">
		<div class="flex flex-col items-center gap-3">
			<div class="h-10 w-10 border-4 border-[#0f62fe] border-t-transparent animate-spin"></div>
			<p class="text-xs font-mono text-[#a8a8a8]">Connecting to server daemon...</p>
		</div>
	</div>
{:else if server}
	<div class="flex h-full flex-col bg-[#161616] p-6 text-[#f4f4f4] font-sans space-y-6">
		<!-- Carbon Header & Server Management Actions -->
		<div class="flex flex-col sm:flex-row sm:items-center justify-between pb-4 border-b border-[#393939] gap-4">
			<div class="flex items-center gap-4">
				<a
					href="/servers"
					class="flex h-10 w-10 shrink-0 items-center justify-center bg-[#262626] hover:bg-[#353535] border border-[#393939] text-[#c6c6c6] hover:text-white transition-colors rounded-none"
					title="Back to Servers"
				>
					<ArrowLeft class="h-4 w-4" />
				</a>
				<div class="flex h-12 w-12 shrink-0 items-center justify-center bg-[#262626] border border-[#393939] text-[#78a9ff] rounded-none">
					<Package class="h-6 w-6" />
				</div>
				<div>
					<div class="flex flex-wrap items-center gap-2.5">
						<h1 class="text-2xl font-light tracking-tight text-[#f4f4f4]">{server.name}</h1>
						<!-- Status Badge (Carbon Tag Requirement) -->
						<CarbonTag type={getStatusTagType(server.status)} size="md">
							{getStatusDisplayName(server.status)}
						</CarbonTag>
						{#if server.nodeId}
							<span class="px-2 py-0.5 bg-[#0f62fe]/20 text-[#78a9ff] border border-[#0f62fe]/40 text-xs font-mono rounded-none">
								NODE: {server.nodeId}
							</span>
						{/if}
					</div>
					<p class="text-xs text-[#a8a8a8] mt-1 font-sans">
						{server.description || 'Minecraft Server Instance'} • Created {timestampToDate(server.createdAt).toLocaleDateString()}
					</p>
				</div>
			</div>

			<!-- Sharp Action Buttons -->
			<div class="flex items-center gap-2">
				{#if server.status === ServerStatus.STOPPED || !server.containerId}
					<button
						type="button"
						onclick={() => handleServerAction('start')}
						disabled={actionLoading}
						class="h-10 px-4 bg-[#198038] hover:bg-[#24a148] text-white text-sm font-sans flex items-center gap-2 rounded-none transition-colors cursor-pointer disabled:opacity-50"
					>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 animate-spin" />
						{:else}
							<Play class="h-4 w-4 fill-current" />
						{/if}
						<span>Start Server</span>
					</button>
				{:else if server.status === ServerStatus.ERROR}
					<button
						type="button"
						onclick={() => handleServerAction('restart')}
						disabled={actionLoading}
						class="h-10 px-4 bg-[#f1c21b] hover:bg-[#d2a106] text-black font-medium text-sm font-sans flex items-center gap-2 rounded-none transition-colors cursor-pointer disabled:opacity-50"
					>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 animate-spin" />
						{:else}
							<RotateCw class="h-4 w-4" />
						{/if}
						<span>Restart</span>
					</button>
					<button
						type="button"
						onclick={() => handleServerAction('stop')}
						disabled={actionLoading}
						class="h-10 px-4 bg-[#da1e28] hover:bg-[#ba1b23] text-white text-sm font-sans flex items-center gap-2 rounded-none transition-colors cursor-pointer disabled:opacity-50"
					>
						<Square class="h-4 w-4 fill-current" />
						<span>Stop</span>
					</button>
				{:else if server.status === ServerStatus.RUNNING || server.status === ServerStatus.STARTING || server.status === ServerStatus.UNHEALTHY}
					<button
						type="button"
						onclick={() => handleServerAction('stop')}
						disabled={actionLoading}
						class="h-10 px-4 bg-[#da1e28] hover:bg-[#ba1b23] text-white text-sm font-sans flex items-center gap-2 rounded-none transition-colors cursor-pointer disabled:opacity-50"
					>
						{#if actionLoading}
							<Loader2 class="h-4 w-4 animate-spin" />
						{:else}
							<Square class="h-4 w-4 fill-current" />
						{/if}
						<span>Stop</span>
					</button>
					<button
						type="button"
						onclick={() => handleServerAction('restart')}
						disabled={actionLoading}
						class="h-10 px-4 bg-[#393939] hover:bg-[#4c4c4c] text-white text-sm font-sans flex items-center gap-2 rounded-none border border-[#525252] transition-colors cursor-pointer disabled:opacity-50"
					>
						<RotateCw class="h-4 w-4" />
						<span>Restart</span>
					</button>
				{:else if server.status === ServerStatus.STOPPING}
					<div class="h-10 px-4 bg-[#393939] text-[#c6c6c6] text-sm font-sans flex items-center gap-2 rounded-none border border-[#525252]">
						<Loader2 class="h-4 w-4 animate-spin" />
						<span>Stopping...</span>
					</div>
				{/if}

				<!-- Dropdown for recreate and delete -->
				<DropdownMenu>
					<DropdownMenuTrigger>
						{#snippet child({ props })}
							<button
								type="button"
								{...props}
								disabled={actionLoading}
								class="h-10 w-10 flex items-center justify-center bg-[#262626] hover:bg-[#353535] border border-[#393939] text-[#c6c6c6] hover:text-white transition-colors cursor-pointer rounded-none"
							>
								<MoreVertical class="h-4 w-4" />
							</button>
						{/snippet}
					</DropdownMenuTrigger>
					<DropdownMenuContent align="end" class="bg-[#161616] border border-[#393939] text-[#f4f4f4] rounded-none p-1">
						<DropdownMenuItem
							class="flex items-center gap-2 px-3 py-2 text-xs hover:bg-[#353535] text-[#f4f4f4] cursor-pointer rounded-none"
							onclick={() => handleServerAction('recreate')}
						>
							<RefreshCcw class="h-3.5 w-3.5" />
							<span>Force Recreate</span>
						</DropdownMenuItem>
						<DropdownMenuItem
							class="flex items-center gap-2 px-3 py-2 text-xs hover:bg-[#da1e28]/20 text-[#ff8389] cursor-pointer rounded-none"
							onclick={() => handleDeleteServer()}
						>
							<Trash2 class="h-3.5 w-3.5" />
							<span>Delete Server</span>
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			</div>
		</div>

		<!-- Carbon Metric Tiles (ZERO rounded corners) -->
		<div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-4">
			<!-- Tile 1: Status & Heartbeat -->
			<div class="bg-[#262626] border border-[#393939] p-4 rounded-none flex flex-col justify-between">
				<div class="flex items-center justify-between pb-3 border-b border-[#393939]">
					<span class="text-xs uppercase font-mono tracking-wider text-[#8d8d8d]">Runtime Status</span>
					<CarbonTag type={getStatusTagType(server.status)} size="sm">
						{getStatusDisplayName(server.status)}
					</CarbonTag>
				</div>
				<div class="py-4 flex items-center justify-center gap-2">
					<div class="heartbeat-container">
						{#if server.status === ServerStatus.RUNNING}
							{#each Array(5) as _, i (i)}
								<div class="heartbeat-bar bg-[#24a148]" style="animation-delay: {i * 0.15}s"></div>
							{/each}
						{:else if server.status === ServerStatus.ERROR || server.status === ServerStatus.UNHEALTHY}
							{#each Array(5) as _, i (i)}
								<div class="heartbeat-bar heartbeat-erratic bg-[#da1e28]" style="animation-delay: {i * 0.1}s"></div>
							{/each}
						{:else}
							{#each Array(5) as _, i (i)}
								<div class="heartbeat-bar bg-[#525252]" style="animation-delay: {i * 0.25}s"></div>
							{/each}
						{/if}
					</div>
				</div>
				<div class="text-center pt-2 border-t border-[#393939]">
					<p class="text-xs text-[#a8a8a8] font-sans">
						{#if server.status === ServerStatus.RUNNING}
							Server healthy and processing ticks
						{:else if server.status === ServerStatus.STOPPED}
							Container offline
						{:else}
							Daemon status: {server.status}
						{/if}
					</p>
				</div>
			</div>

			<!-- Tile 2: Connection Address -->
			<div class="bg-[#262626] border border-[#393939] p-4 rounded-none flex flex-col justify-between">
				<div class="flex items-center justify-between pb-3 border-b border-[#393939]">
					<span class="text-xs uppercase font-mono tracking-wider text-[#8d8d8d]">Network Ingress</span>
					<ExternalLink class="h-4 w-4 text-[#78a9ff]" />
				</div>
				<div class="py-3">
					<p class="text-[11px] text-[#8d8d8d] font-mono uppercase">Connect String</p>
					<p class="font-mono text-base font-semibold text-[#f4f4f4] truncate mt-0.5">
						{#if server.proxyHostname}
							{server.proxyHostname}
						{:else}
							localhost:{server.port}
						{/if}
					</p>
				</div>
				<div class="pt-2 border-t border-[#393939]">
					<button
						type="button"
						onclick={() => {
							const addr = server?.proxyHostname || `localhost:${server?.port}`;
							copyConnection(addr);
						}}
						class="w-full h-8 px-3 bg-[#161616] hover:bg-[#353535] border border-[#393939] text-xs font-mono text-[#78a9ff] flex items-center justify-center gap-2 transition-colors cursor-pointer rounded-none"
					>
						<Copy class="h-3.5 w-3.5" />
						<span>Copy Address</span>
					</button>
				</div>
			</div>

			<!-- Tile 3: Engine Details -->
			<div class="bg-[#262626] border border-[#393939] p-4 rounded-none flex flex-col justify-between">
				<div class="flex items-center justify-between pb-3 border-b border-[#393939]">
					<span class="text-xs uppercase font-mono tracking-wider text-[#8d8d8d]">Engine Specs</span>
					<Info class="h-4 w-4 text-[#d4bbff]" />
				</div>
				<div class="space-y-2 py-2 text-xs font-mono">
					<div class="flex items-center justify-between">
						<span class="text-[#8d8d8d]">Minecraft:</span>
						<span class="text-[#f4f4f4] font-semibold">{server.mcVersion || 'Latest'}</span>
					</div>
					<div class="flex items-center justify-between">
						<span class="text-[#8d8d8d]">Loader:</span>
						<CarbonTag type="teal" size="sm">
							{ModLoader[server.modLoader].toLowerCase()}
						</CarbonTag>
					</div>
					{#if server.javaVersion}
						<div class="flex items-center justify-between">
							<span class="text-[#8d8d8d]">Java:</span>
							<span class="text-[#c6c6c6]">JDK {server.javaVersion}</span>
						</div>
					{/if}
				</div>
				<div class="pt-2 border-t border-[#393939] flex items-center justify-between text-[11px] font-mono text-[#8d8d8d]">
					<span>Port: {server.port}</span>
					<span>Max Players: {server.maxPlayers}</span>
				</div>
			</div>

			<!-- Tile 4: Hardware Quotas -->
			<div class="bg-[#262626] border border-[#393939] p-4 rounded-none flex flex-col justify-between">
				<div class="flex items-center justify-between pb-3 border-b border-[#393939]">
					<span class="text-xs uppercase font-mono tracking-wider text-[#8d8d8d]">Resource Telemetry</span>
					<Cpu class="h-4 w-4 text-[#ff8389]" />
				</div>
				<div class="space-y-2.5 py-1">
					<!-- Memory Bar -->
					<div>
						<div class="flex items-center justify-between text-xs font-mono">
							<span class="text-[#8d8d8d]">RAM</span>
							<span class="text-[#f4f4f4]">
								{server.memoryUsage ? (Number(server.memoryUsage) / 1024).toFixed(1) : '0'} / {(server.memory / 1024).toFixed(1)} GB
							</span>
						</div>
						<div class="w-full h-1.5 bg-[#161616] border border-[#393939] rounded-none mt-1">
							<div
								class="h-full bg-[#0f62fe] rounded-none transition-all"
								style="width: {Math.min(server.memoryUsage ? (Number(server.memoryUsage) / server.memory) * 100 : 0, 100)}%"
							></div>
						</div>
					</div>

					<!-- CPU Bar -->
					<div>
						<div class="flex items-center justify-between text-xs font-mono">
							<span class="text-[#8d8d8d]">CPU</span>
							<span class="text-[#33b1ff]">
								{server.cpuPercent !== undefined ? `${server.cpuPercent.toFixed(1)}%` : '—'}
							</span>
						</div>
						<div class="w-full h-1.5 bg-[#161616] border border-[#393939] rounded-none mt-1">
							<div
								class="h-full bg-[#33b1ff] rounded-none transition-all"
								style="width: {Math.min(server.cpuPercent || 0, 100)}%"
							></div>
						</div>
					</div>
				</div>

				<div class="pt-2 border-t border-[#393939] flex items-center justify-between text-[11px] font-mono">
					<span class="text-[#8d8d8d]">TPS:</span>
					<span class="{server.tps && server.tps >= 18 ? 'text-[#6fdc8c]' : 'text-[#f1c21b]'} font-semibold">
						{server.tps ? server.tps.toFixed(1) : '20.0'}
					</span>
					<span class="text-[#8d8d8d]">Players:</span>
					<span class="text-[#6fdc8c] font-semibold">{server.playersOnline || 0}</span>
				</div>
			</div>
		</div>

		<!-- Carbon Tabs for Sub-Views with bottom blue line indicator (Requirement 4) -->
		<div class="flex min-h-0 flex-1 flex-col space-y-4">
			<!-- Tab Bar -->
			<div class="w-full border-b border-[#393939] bg-[#161616] overflow-x-auto flex items-center">
				{#each subViewTabs as tab (tab.id)}
					<button
						type="button"
						onclick={() => (activeTab = tab.id)}
						class="h-10 px-5 flex items-center gap-2 text-sm font-normal transition-colors border-b-2 cursor-pointer select-none whitespace-nowrap rounded-none {activeTab === tab.id ? 'border-[#0f62fe] text-[#f4f4f4] bg-[#262626] font-semibold' : 'border-transparent text-[#a8a8a8] hover:text-[#f4f4f4] hover:bg-[#262626]'}"
					>
						<span>{tab.label}</span>
					</button>
				{/each}
			</div>

			<!-- Tab Content Areas -->
			<div class="min-h-0 flex-1">
				{#if activeTab === 'overview'}
					<div class="bg-[#262626] border border-[#393939] p-6 rounded-none">
						<h3 class="text-base font-semibold text-[#f4f4f4] mb-1">Server Settings</h3>
						<p class="text-xs text-[#a8a8a8] mb-6">Modify runtime container settings and server parameters</p>
						<ServerSettings {server} onUpdate={loadServer} />
					</div>
				{:else if activeTab === 'console'}
					<ServerConsole {server} active={activeTab === 'console'} />
				{:else if activeTab === 'configuration'}
					<div class="h-full overflow-y-auto">
						<ServerConfiguration {server} />
					</div>
				{:else if activeTab === 'mods'}
					<ServerMods {server} active={activeTab === 'mods'} />
				{:else if activeTab === 'modules'}
					<ServerModules {server} active={activeTab === 'modules'} />
				{:else if activeTab === 'files'}
					<ServerFiles {server} active={activeTab === 'files'} />
				{:else if activeTab === 'tasks'}
					<div class="h-full overflow-y-auto">
						<ServerTasks {server} active={activeTab === 'tasks'} />
					</div>
				{:else if activeTab === 'routing'}
					<div class="h-full overflow-y-auto">
						<ServerRouting {server} bind:router={routingInfo} active={activeTab === 'routing'} />
					</div>
				{/if}
			</div>
		</div>
	</div>
{:else}
	<div class="flex h-96 items-center justify-center bg-[#161616]">
		<div class="text-center p-8 bg-[#262626] border border-[#393939] rounded-none">
			<p class="text-sm text-[#ff8389] font-mono">Server instance not found</p>
			<a href="/servers" class="mt-4 inline-flex h-8 px-4 bg-[#393939] hover:bg-[#4c4c4c] text-white text-xs font-sans items-center rounded-none transition-colors">
				Back to servers
			</a>
		</div>
	</div>
{/if}

<ScrollToTop />

<style>
	.heartbeat-container {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 3px;
		height: 36px;
	}

	.heartbeat-bar {
		width: 4px;
		height: 32px;
		border-radius: 0px;
		animation: heartbeat 3.5s ease-in-out infinite;
	}

	.heartbeat-bar.heartbeat-erratic {
		animation: heartbeat-erratic 1.5s ease-in-out infinite;
	}

	@keyframes heartbeat {
		0%,
		100% {
			height: 6px;
			opacity: 0.3;
		}
		50% {
			height: 32px;
			opacity: 1;
		}
	}

	@keyframes heartbeat-erratic {
		0%,
		100% {
			height: 4px;
			opacity: 0.2;
		}
		25% {
			height: 24px;
			opacity: 0.9;
		}
		50% {
			height: 12px;
			opacity: 0.5;
		}
		75% {
			height: 30px;
			opacity: 1;
		}
	}
</style>
