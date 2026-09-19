<script lang="ts">
	import { resolve } from '$app/paths';
	import { serversStore, sortServersByActivity } from '$lib/stores/servers';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import {
		Plus,
		Search,
		Play,
		Square,
		RotateCw,
		RefreshCcw,
		Trash2,
		Server as ServerIcon,
		Users,
		Zap,
		MemoryStick,
		Wifi,
		Table as TableIcon,
		LayoutGrid,
		ArrowRight
	} from '@lucide/svelte';
	import { type Server, ServerStatus, ModLoader } from '$lib/proto/carbonpanel/v1/common_pb';
	import { CarbonTag, CarbonButton } from '$lib/components/carbon';

	let servers = $derived($serversStore);
	let filteredServers = $state<Server[]>([]);
	let searchQuery = $state('');
	let statusFilter = $state<'all' | 'running' | 'stopped' | 'issues'>('all');
	let viewMode = $state<'table' | 'tiles'>('table');
	let loading = $state(false);

	$effect(() => {
		filterServers();
	});

	function filterServers() {
		let sorted = sortServersByActivity([...servers]);

		// Status filter
		if (statusFilter === 'running') {
			sorted = sorted.filter((s) => s.status === ServerStatus.RUNNING);
		} else if (statusFilter === 'stopped') {
			sorted = sorted.filter((s) => s.status === ServerStatus.STOPPED);
		} else if (statusFilter === 'issues') {
			sorted = sorted.filter(
				(s) => s.status === ServerStatus.ERROR || s.status === ServerStatus.UNHEALTHY
			);
		}

		if (!searchQuery.trim()) {
			filteredServers = sorted;
		} else {
			const query = searchQuery.toLowerCase().trim();
			filteredServers = sorted.filter(
				(server) =>
					server.name.toLowerCase().includes(query) ||
					server.description.toLowerCase().includes(query) ||
					server.mcVersion.toLowerCase().includes(query) ||
					String(server.modLoader).toLowerCase().includes(query) ||
					String(server.port).includes(query)
			);
		}
	}

	async function handleServerAction(
		action: 'start' | 'stop' | 'restart' | 'recreate',
		server: Server,
		event?: MouseEvent
	) {
		event?.stopPropagation();
		loading = true;
		try {
			switch (action) {
				case 'start':
					await rpcClient.server.startServer({ id: server.id });
					toast.success(`Starting ${server.name}...`);
					break;
				case 'stop':
					await rpcClient.server.stopServer({ id: server.id });
					toast.success(`Stopping ${server.name}...`);
					break;
				case 'restart':
					await rpcClient.server.restartServer({ id: server.id });
					toast.success(`Restarting ${server.name}...`);
					break;
				case 'recreate':
					await rpcClient.server.recreateServer({ id: server.id });
					toast.success(`Recreating ${server.name}...`);
					break;
			}
		} catch (error) {
			toast.error(
				`Failed to ${action} server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			loading = false;
		}
	}

	async function deleteServer(server: Server, event?: MouseEvent) {
		event?.stopPropagation();
		if (
			!confirm(`Are you sure you want to delete "${server.name}"? This action cannot be undone.`)
		) {
			return;
		}

		loading = true;
		try {
			await rpcClient.server.deleteServer({ id: server.id });
			serversStore.removeServer(server.id);
			toast.success(`Deleted ${server.name}`);
		} catch (error) {
			toast.error(
				`Failed to delete server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			loading = false;
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

	function getModLoaderDisplay(modLoader: ModLoader): string {
		return ModLoader[modLoader].replace('_', ' ').toLowerCase();
	}
</script>

<div class="h-full flex-1 space-y-6 bg-[#161616] p-6 text-[#f4f4f4] font-sans">
	<!-- Carbon Page Header -->
	<div class="flex flex-col sm:flex-row sm:items-center justify-between pb-4 border-b border-[#393939] gap-4">
		<div class="flex items-center gap-4">
			<div class="flex h-12 w-12 shrink-0 items-center justify-center bg-[#262626] border border-[#393939] text-[#0f62fe]">
				<ServerIcon class="h-6 w-6" />
			</div>
			<div>
				<div class="flex items-center gap-2.5">
					<h1 class="text-2xl font-light tracking-tight text-[#f4f4f4]">Servers</h1>
					<span class="px-2 py-0.5 bg-[#0f62fe]/20 text-[#78a9ff] border border-[#0f62fe]/40 text-xs font-mono">
						MANAGEMENT
					</span>
				</div>
				<p class="text-xs text-[#a8a8a8] mt-1 font-sans">
					Manage, inspect, and deploy your Minecraft server instances
				</p>
			</div>
		</div>
		<div class="flex items-center gap-2">
			<CarbonButton kind="primary" size="md" href="/servers/new" class="justify-center gap-2">
				<Plus class="h-4 w-4" />
				<span>Create Server</span>
			</CarbonButton>
		</div>
	</div>

	<!-- Carbon Action Bar: Search, Filters, View Toggles -->
	<div class="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3 bg-[#262626] border border-[#393939] p-3">
		<!-- Search Input (Carbon Style) -->
		<div class="relative flex-1 max-w-lg">
			<div class="relative flex items-center">
				<Search class="absolute left-3 h-4 w-4 text-[#8d8d8d] pointer-events-none" />
				<input
					type="search"
					placeholder="Search by name, description, version, loader, or port..."
					bind:value={searchQuery}
					class="w-full h-9 pl-9 pr-3 bg-[#161616] border-b border-[#8d8d8d] focus:border-b-2 focus:border-[#0f62fe] focus:outline-none text-xs font-sans text-[#f4f4f4] placeholder-[#6f6f6f] rounded-none transition-all"
				/>
			</div>
		</div>

		<!-- Status Filter Tags and View Toggles -->
		<div class="flex flex-wrap items-center justify-between sm:justify-end gap-2">
			<!-- Filter Tags -->
			<div class="flex items-center gap-1 border-r border-[#393939] pr-2 mr-1">
				<button
					type="button"
					onclick={() => (statusFilter = 'all')}
					class="px-2.5 py-1 text-xs font-mono uppercase transition-colors cursor-pointer rounded-none border {statusFilter === 'all' ? 'bg-[#0f62fe] text-white border-[#0f62fe]' : 'bg-[#161616] text-[#c6c6c6] border-[#393939] hover:bg-[#353535]'}"
				>
					All ({servers.length})
				</button>
				<button
					type="button"
					onclick={() => (statusFilter = 'running')}
					class="px-2.5 py-1 text-xs font-mono uppercase transition-colors cursor-pointer rounded-none border {statusFilter === 'running' ? 'bg-[#198038] text-white border-[#198038]' : 'bg-[#161616] text-[#6fdc8c] border-[#198038]/50 hover:bg-[#353535]'}"
				>
					Running ({servers.filter((s) => s.status === ServerStatus.RUNNING).length})
				</button>
				<button
					type="button"
					onclick={() => (statusFilter = 'stopped')}
					class="px-2.5 py-1 text-xs font-mono uppercase transition-colors cursor-pointer rounded-none border {statusFilter === 'stopped' ? 'bg-[#525252] text-white border-[#525252]' : 'bg-[#161616] text-[#c6c6c6] border-[#525252]/60 hover:bg-[#353535]'}"
				>
					Stopped ({servers.filter((s) => s.status === ServerStatus.STOPPED).length})
				</button>
				{#if servers.some((s) => s.status === ServerStatus.ERROR || s.status === ServerStatus.UNHEALTHY)}
					<button
						type="button"
						onclick={() => (statusFilter = 'issues')}
						class="px-2.5 py-1 text-xs font-mono uppercase transition-colors cursor-pointer rounded-none border {statusFilter === 'issues' ? 'bg-[#da1e28] text-white border-[#da1e28]' : 'bg-[#161616] text-[#ff8389] border-[#da1e28]/50 hover:bg-[#353535]'}"
					>
						Issues ({servers.filter((s) => s.status === ServerStatus.ERROR || s.status === ServerStatus.UNHEALTHY).length})
					</button>
				{/if}
			</div>

			<!-- View Mode Switcher -->
			<div class="flex items-center border border-[#393939] bg-[#161616]">
				<button
					type="button"
					onclick={() => (viewMode = 'table')}
					title="Table View"
					aria-label="Table View"
					class="h-8 w-8 flex items-center justify-center transition-colors cursor-pointer rounded-none {viewMode === 'table' ? 'bg-[#393939] text-[#f4f4f4]' : 'text-[#8d8d8d] hover:text-[#f4f4f4] hover:bg-[#262626]'}"
				>
					<TableIcon class="h-4 w-4" />
				</button>
				<button
					type="button"
					onclick={() => (viewMode = 'tiles')}
					title="Tile View"
					aria-label="Tile View"
					class="h-8 w-8 flex items-center justify-center transition-colors cursor-pointer rounded-none {viewMode === 'tiles' ? 'bg-[#393939] text-[#f4f4f4]' : 'text-[#8d8d8d] hover:text-[#f4f4f4] hover:bg-[#262626]'}"
				>
					<LayoutGrid class="h-4 w-4" />
				</button>
			</div>
		</div>
	</div>

	<!-- Empty State -->
	{#if filteredServers.length === 0}
		<div class="bg-[#262626] border border-[#393939] p-12 text-center rounded-none">
			{#if servers.length === 0}
				<div class="mx-auto mb-4 flex h-14 w-14 items-center justify-center bg-[#161616] border border-[#393939] text-[#0f62fe]">
					<ServerIcon class="h-7 w-7" />
				</div>
				<h3 class="mb-1 text-base font-semibold text-[#f4f4f4]">No servers configured</h3>
				<p class="mb-6 text-xs text-[#a8a8a8] max-w-md mx-auto font-sans">
					Create your first Minecraft server instance to begin managing containers and game topology.
				</p>
				<CarbonButton kind="primary" size="md" href="/servers/new" class="inline-flex justify-center gap-2">
					<Plus class="h-4 w-4" />
					<span>Create Server</span>
				</CarbonButton>
			{:else}
				<div class="mx-auto mb-4 flex h-14 w-14 items-center justify-center bg-[#161616] border border-[#393939] text-[#8d8d8d]">
					<Search class="h-7 w-7" />
				</div>
				<h3 class="mb-1 text-base font-semibold text-[#f4f4f4]">No matching servers</h3>
				<p class="text-xs text-[#a8a8a8]">Try adjusting your search criteria or status filter.</p>
				<CarbonButton
					kind="secondary"
					size="sm"
					class="mt-4 justify-center"
					onclick={() => {
						searchQuery = '';
						statusFilter = 'all';
					}}
				>
					Reset filters
				</CarbonButton>
			{/if}
		</div>

	<!-- Carbon DataTable Layout (Requirement 2) -->
	{:else if viewMode === 'table'}
		<div class="w-full bg-[#161616] border border-[#393939] rounded-none overflow-x-auto">
			<table class="w-full border-collapse text-left font-sans text-sm">
				<!-- Sharp Header Row #393939 -->
				<thead class="bg-[#393939] text-[#f4f4f4] border-b border-[#525252] text-xs uppercase font-medium tracking-wider select-none">
					<tr>
						<th scope="col" class="px-4 py-3 w-28">Status</th>
						<th scope="col" class="px-4 py-3">Server Instance</th>
						<th scope="col" class="px-4 py-3">Engine & Loader</th>
						<th scope="col" class="px-4 py-3">Host / Port</th>
						<th scope="col" class="px-4 py-3">Memory</th>
						<th scope="col" class="px-4 py-3">Players & TPS</th>
						<th scope="col" class="px-4 py-3 text-right">Actions</th>
					</tr>
				</thead>

				<!-- Sharp Alternating/Hover Rows #353535 -->
				<tbody class="divide-y divide-[#393939] bg-[#262626] text-[#f4f4f4]">
					{#each filteredServers as server (server.id)}
						<tr
							class="hover:bg-[#353535] transition-colors group cursor-pointer"
							onclick={() => window.location.assign(resolve(`/servers/${server.id}`))}
						>
							<!-- Status Cell -->
							<td class="px-4 py-3.5 whitespace-nowrap">
								<CarbonTag type={getStatusTagType(server.status)} size="sm">
									{getStatusDisplayName(server.status)}
								</CarbonTag>
							</td>

							<!-- Name & Description -->
							<td class="px-4 py-3.5 min-w-[200px]">
								<div class="flex items-center gap-2.5">
									<div class="flex h-8 w-8 shrink-0 items-center justify-center bg-[#161616] border border-[#393939] text-[#78a9ff]">
										<ServerIcon class="h-4 w-4" />
									</div>
									<div class="min-w-0">
										<a
											href={resolve(`/servers/${server.id}`)}
											class="font-medium text-sm text-[#f4f4f4] hover:text-[#78a9ff] hover:underline truncate block"
											onclick={(e) => e.stopPropagation()}
										>
											{server.name}
										</a>
										<p class="text-xs text-[#a8a8a8] line-clamp-1">
											{server.description || 'No description provided'}
										</p>
									</div>
								</div>
							</td>

							<!-- Engine & Loader -->
							<td class="px-4 py-3.5 whitespace-nowrap">
								<div class="flex items-center gap-1.5 font-mono text-xs">
									<span class="text-[#f4f4f4]">{server.mcVersion || 'Latest'}</span>
									{#if server.modLoader !== ModLoader.VANILLA && server.modLoader !== ModLoader.UNSPECIFIED}
										<CarbonTag type="teal" size="sm">
											{getModLoaderDisplay(server.modLoader)}
										</CarbonTag>
									{:else}
										<span class="text-[#8d8d8d] text-[11px]">Vanilla</span>
									{/if}
								</div>
							</td>

							<!-- Host / Port -->
							<td class="px-4 py-3.5 whitespace-nowrap">
								<div class="flex items-center gap-1.5 font-mono text-xs">
									<Wifi class="h-3.5 w-3.5 text-[#8d8d8d]" />
									<span class="text-[#c6c6c6]">
										{server.proxyHostname ? server.proxyHostname : `:${server.port}`}
									</span>
								</div>
							</td>

							<!-- Memory -->
							<td class="px-4 py-3.5 whitespace-nowrap">
								<div class="flex items-center gap-1.5 font-mono text-xs text-[#c6c6c6]">
									<MemoryStick class="h-3.5 w-3.5 text-[#8d8d8d]" />
									<span>{(server.memory / 1024).toFixed(1)} GB</span>
								</div>
							</td>

							<!-- Players & TPS -->
							<td class="px-4 py-3.5 whitespace-nowrap">
								{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY}
									<div class="flex items-center gap-3 font-mono text-xs">
										<span class="text-[#6fdc8c] flex items-center gap-1">
											<Users class="h-3.5 w-3.5" />
											{server.playersOnline || 0}/{server.maxPlayers}
										</span>
										<span class="flex items-center gap-1 {server.tps && server.tps >= 18 ? 'text-[#6fdc8c]' : server.tps && server.tps >= 15 ? 'text-[#f1c21b]' : 'text-[#ff8389]'}">
											<Zap class="h-3.5 w-3.5" />
											{server.tps ? server.tps.toFixed(1) : '—'}
										</span>
									</div>
								{:else}
									<span class="text-xs text-[#6f6f6f] font-mono">—</span>
								{/if}
							</td>

							<!-- Actions -->
							<td class="px-4 py-3.5 text-right whitespace-nowrap" onclick={(e) => e.stopPropagation()}>
								<div class="inline-flex items-center border border-[#393939] bg-[#161616]">
									{#if server.status === ServerStatus.STOPPED || server.status === ServerStatus.ERROR}
										<button
											title="Start"
											aria-label={`Start server ${server.name}`}
											disabled={loading}
											class="h-8 w-8 flex items-center justify-center border-r border-[#393939] text-[#6fdc8c] hover:bg-[#198038]/20 transition-colors disabled:opacity-40 cursor-pointer rounded-none"
											onclick={(e) => handleServerAction('start', server, e)}
										>
											<Play class="h-3.5 w-3.5 fill-current" />
										</button>
									{/if}

									{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY || server.status === ServerStatus.STARTING}
										<button
											title="Stop"
											aria-label={`Stop server ${server.name}`}
											disabled={loading}
											class="h-8 w-8 flex items-center justify-center border-r border-[#393939] text-[#ff8389] hover:bg-[#da1e28]/20 transition-colors disabled:opacity-40 cursor-pointer rounded-none"
											onclick={(e) => handleServerAction('stop', server, e)}
										>
											<Square class="h-3.5 w-3.5 fill-current" />
										</button>
									{/if}

									{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY}
										<button
											title="Restart"
											aria-label={`Restart server ${server.name}`}
											disabled={loading}
											class="h-8 w-8 flex items-center justify-center border-r border-[#393939] text-[#f1c21b] hover:bg-[#f1c21b]/20 transition-colors disabled:opacity-40 cursor-pointer rounded-none"
											onclick={(e) => handleServerAction('restart', server, e)}
										>
											<RotateCw class="h-3.5 w-3.5" />
										</button>
									{/if}

									<button
										title="Recreate"
										aria-label={`Recreate server ${server.name}`}
										disabled={loading}
										class="h-8 w-8 flex items-center justify-center border-r border-[#393939] text-[#c6c6c6] hover:bg-[#353535] transition-colors disabled:opacity-40 cursor-pointer rounded-none"
										onclick={(e) => handleServerAction('recreate', server, e)}
									>
										<RefreshCcw class="h-3.5 w-3.5" />
									</button>

									<button
										title="Delete"
										aria-label={`Delete server ${server.name}`}
										disabled={loading}
										class="h-8 w-8 flex items-center justify-center border-r border-[#393939] text-[#ff8389] hover:bg-[#da1e28]/20 transition-colors disabled:opacity-40 cursor-pointer rounded-none"
										onclick={(e) => deleteServer(server, e)}
									>
										<Trash2 class="h-3.5 w-3.5" />
									</button>

									<a
										href={resolve(`/servers/${server.id}`)}
										title="View Details"
										aria-label={`View details for ${server.name}`}
										class="h-8 w-8 flex items-center justify-center text-[#78a9ff] hover:bg-[#0f62fe]/20 transition-colors cursor-pointer rounded-none"
									>
										<ArrowRight class="h-3.5 w-3.5" />
									</a>
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

	<!-- Carbon Tile / Grid Layout -->
	{:else}
		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
			{#each filteredServers as server (server.id)}
				<div class="bg-[#262626] border border-[#393939] hover:border-[#525252] transition-colors rounded-none p-4 flex flex-col justify-between">
					<div>
						<!-- Header & Actions -->
						<div class="flex items-start justify-between gap-3 pb-3 border-b border-[#393939]">
							<div class="flex items-center gap-3 min-w-0">
								<div class="flex h-10 w-10 shrink-0 items-center justify-center bg-[#161616] border border-[#393939] text-[#78a9ff]">
									<ServerIcon class="h-5 w-5" />
								</div>
								<div class="min-w-0">
									<a
										href={resolve(`/servers/${server.id}`)}
										class="font-semibold text-base text-[#f4f4f4] hover:text-[#78a9ff] truncate block"
									>
										{server.name}
									</a>
									<p class="text-xs text-[#a8a8a8] truncate">
										{server.description || 'No description provided'}
									</p>
								</div>
							</div>

							<!-- Action Buttons Bar -->
							<div class="flex shrink-0 border border-[#393939] bg-[#161616]">
								{#if server.status === ServerStatus.STOPPED || server.status === ServerStatus.ERROR}
									<button
										title="Start"
										aria-label={`Start server ${server.name}`}
										disabled={loading}
										class="flex h-7 w-7 items-center justify-center border-r border-[#393939] text-[#6fdc8c] hover:bg-[#198038]/20 transition-colors disabled:opacity-50 cursor-pointer rounded-none"
										onclick={(e) => handleServerAction('start', server, e)}
									>
										<Play class="h-3 w-3 fill-current" />
									</button>
								{/if}
								{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY || server.status === ServerStatus.STARTING}
									<button
										title="Stop"
										aria-label={`Stop server ${server.name}`}
										disabled={loading}
										class="flex h-7 w-7 items-center justify-center border-r border-[#393939] text-[#ff8389] hover:bg-[#da1e28]/20 transition-colors disabled:opacity-50 cursor-pointer rounded-none"
										onclick={(e) => handleServerAction('stop', server, e)}
									>
										<Square class="h-2.5 w-2.5 fill-current" />
									</button>
								{/if}
								{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY}
									<button
										title="Restart"
										aria-label={`Restart server ${server.name}`}
										disabled={loading}
										class="flex h-7 w-7 items-center justify-center border-r border-[#393939] text-[#f1c21b] hover:bg-[#f1c21b]/20 transition-colors disabled:opacity-50 cursor-pointer rounded-none"
										onclick={(e) => handleServerAction('restart', server, e)}
									>
										<RotateCw class="h-3 w-3" />
									</button>
								{/if}
								<button
									title="Recreate"
									aria-label={`Recreate server ${server.name}`}
									disabled={loading}
									class="flex h-7 w-7 items-center justify-center border-r border-[#393939] text-[#c6c6c6] hover:bg-[#353535] transition-colors disabled:opacity-50 cursor-pointer rounded-none"
									onclick={(e) => handleServerAction('recreate', server, e)}
								>
									<RefreshCcw class="h-3 w-3" />
								</button>
								<button
									title="Delete"
									aria-label={`Delete server ${server.name}`}
									disabled={loading}
									class="flex h-7 w-7 items-center justify-center text-[#ff8389] hover:bg-[#da1e28]/20 transition-colors disabled:opacity-50 cursor-pointer rounded-none"
									onclick={(e) => deleteServer(server, e)}
								>
									<Trash2 class="h-3 w-3" />
								</button>
							</div>
						</div>

						<!-- Status & Version row -->
						<div class="my-3 flex items-center gap-2">
							<CarbonTag type={getStatusTagType(server.status)} size="sm">
								{getStatusDisplayName(server.status)}
							</CarbonTag>
							<span class="font-mono text-xs text-[#a8a8a8]">{server.mcVersion}</span>
							{#if server.modLoader !== ModLoader.VANILLA && server.modLoader !== ModLoader.UNSPECIFIED}
								<CarbonTag type="teal" size="sm">
									{getModLoaderDisplay(server.modLoader)}
								</CarbonTag>
							{/if}
						</div>

						<!-- Stats Sharp Grid -->
						<div class="grid grid-cols-2 gap-2 mt-3">
							<div class="flex items-center gap-2 bg-[#161616] border border-[#393939] p-2 rounded-none">
								<Wifi class="h-3.5 w-3.5 shrink-0 text-[#8d8d8d]" />
								<div class="min-w-0">
									<p class="text-[10px] tracking-wider text-[#8d8d8d] uppercase font-mono">Port</p>
									<p class="truncate font-mono text-xs font-semibold text-[#f4f4f4]">{server.port}</p>
								</div>
							</div>
							<div class="flex items-center gap-2 bg-[#161616] border border-[#393939] p-2 rounded-none">
								<MemoryStick class="h-3.5 w-3.5 shrink-0 text-[#8d8d8d]" />
								<div class="min-w-0">
									<p class="text-[10px] tracking-wider text-[#8d8d8d] uppercase font-mono">Memory</p>
									<p class="truncate font-mono text-xs font-semibold text-[#f4f4f4]">
										{(server.memory / 1024).toFixed(1)} GB
									</p>
								</div>
							</div>
							{#if server.status === ServerStatus.RUNNING || server.status === ServerStatus.UNHEALTHY}
								<div class="flex items-center gap-2 bg-[#161616] border border-[#393939] p-2 rounded-none">
									<Users class="h-3.5 w-3.5 shrink-0 text-[#6fdc8c]" />
									<div class="min-w-0">
										<p class="text-[10px] tracking-wider text-[#8d8d8d] uppercase font-mono">Players</p>
										<p class="font-mono text-xs font-semibold text-[#6fdc8c]">
											{server.playersOnline || 0} / {server.maxPlayers}
										</p>
									</div>
								</div>
								<div class="flex items-center gap-2 bg-[#161616] border border-[#393939] p-2 rounded-none">
									<Zap class="h-3.5 w-3.5 shrink-0 {server.tps && server.tps >= 18 ? 'text-[#6fdc8c]' : server.tps && server.tps >= 15 ? 'text-[#f1c21b]' : 'text-[#ff8389]'}" />
									<div class="min-w-0">
										<p class="text-[10px] tracking-wider text-[#8d8d8d] uppercase font-mono">TPS</p>
										<p class="font-mono text-xs font-semibold {server.tps && server.tps >= 18 ? 'text-[#6fdc8c]' : server.tps && server.tps >= 15 ? 'text-[#f1c21b]' : 'text-[#ff8389]'}">
											{server.tps ? server.tps.toFixed(1) : '—'}
										</p>
									</div>
								</div>
							{/if}
						</div>
					</div>

					<!-- Bottom Link -->
					<div class="mt-4 pt-3 border-t border-[#393939]">
						<a
							href={resolve(`/servers/${server.id}`)}
							class="w-full h-8 px-3 bg-[#161616] hover:bg-[#353535] border border-[#393939] text-xs font-sans text-[#f4f4f4] flex items-center justify-between transition-colors rounded-none"
						>
							<span>View Server Details</span>
							<ArrowRight class="h-3.5 w-3.5 text-[#78a9ff]" />
						</a>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
