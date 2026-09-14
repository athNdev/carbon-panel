<script lang="ts">
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { formatBytes, getStringForEnum } from '$lib/utils';
	import {
		Server,
		MemoryStick,
		Plus,
		Clock,
		Users,
		Zap,
		ChevronRight,
		Package,
		Globe,
		Gauge,
		AlertTriangle,
		RefreshCw,
		PlayCircle,
		StopCircle
	} from '@lucide/svelte';
	import { ServerStatus, type Server as ServerType } from '$lib/proto/mineserver/v1/common_pb';
	import { rpcClient } from '$lib/api/rpc-client';
	import { serversStore, sortServersByActivity } from '$lib/stores/servers';
	import { CarbonButton, CarbonTag } from '$lib/components/carbon';
	import type { Timestamp } from '@bufbuild/protobuf/wkt';

	// Dashboard data
	let dashboardServers: ServerType[] = $state([]);
	let isLoading = $state(true);
	let isRefreshing = $state(false);
	let currentTime = $state(new Date());

	// Load dashboard data with full stats
	async function loadDashboardData() {
		try {
			const response = await rpcClient.server.listServers({ fullStats: true });
			dashboardServers = response.servers;
			serversStore.set(response.servers);
		} catch (error) {
			console.error('Failed to load dashboard data:', error);
		}
	}

	// Refresh function for manual updates
	async function refreshDashboard() {
		isRefreshing = true;
		await loadDashboardData();
		isRefreshing = false;
	}

	async function handleServerAction(action: 'start' | 'stop', server: ServerType) {
		try {
			if (action === 'start') {
				await rpcClient.server.startServer({ id: server.id });
				toast.success(`Starting ${server.name}...`);
			} else {
				await rpcClient.server.stopServer({ id: server.id });
				toast.success(`Stopping ${server.name}...`);
			}
			await loadDashboardData();
		} catch (error) {
			toast.error(
				`Failed to ${action} server: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		}
	}

	let stats = $derived({
		total: dashboardServers.length,
		running: dashboardServers.filter((s) => s.status === ServerStatus.RUNNING).length,
		stopped: dashboardServers.filter((s) => s.status === ServerStatus.STOPPED).length,
		error: dashboardServers.filter(
			(s) => s.status === ServerStatus.ERROR || s.status === ServerStatus.UNHEALTHY
		).length,
		totalMemory: dashboardServers.reduce((acc, s) => acc + (s.memory || 0), 0),
		usedMemory: dashboardServers
			.filter((s) => s.status === ServerStatus.RUNNING)
			.reduce((acc, s) => acc + Number(s.memoryUsage || s.memory || 0), 0),
		totalPlayers: dashboardServers
			.filter((s) => s.status === ServerStatus.RUNNING)
			.reduce((acc, s) => acc + (s.playersOnline || 0), 0),
		totalMaxPlayers: dashboardServers.reduce((acc, s) => acc + (s.maxPlayers || 0), 0),
		avgTps: dashboardServers
			.filter((s) => s.tps && s.tps > 0)
			.reduce((acc, s, _, arr) => acc + (s.tps || 0) / arr.length, 0),
		totalDiskUsage: dashboardServers.reduce((acc, s) => acc + Number(s.diskUsage || 0), 0),
		totalDiskSize:
			dashboardServers.length > 0 && dashboardServers[0]?.diskTotal
				? ` / ${formatBytes(Number(dashboardServers[0].diskTotal))}`
				: '',
		avgCpu: dashboardServers
			.filter((s) => s.cpuPercent && s.cpuPercent > 0)
			.reduce((acc, s, _, arr) => acc + (s.cpuPercent || 0) / arr.length, 0),
		memUsagePercent:
			dashboardServers.reduce((acc, s) => acc + (s.memory || 0), 0) > 0
				? Math.min(
						100,
						Math.round(
							(dashboardServers
								.filter((s) => s.status === ServerStatus.RUNNING)
								.reduce((acc, s) => acc + Number(s.memoryUsage || s.memory || 0), 0) /
								dashboardServers.reduce((acc, s) => acc + (s.memory || 0), 0)) *
								100
						)
					)
				: 0
	});

	let recentActivity = $derived(
		dashboardServers
			.filter((s) => s.lastStarted)
			.sort(
				(a, b) =>
					new Date(Number(b.lastStarted!.seconds) * 1000).getTime() -
					new Date(Number(a.lastStarted!.seconds) * 1000).getTime()
			)
			.slice(0, 5)
			.map((s) => ({
				server: s.name,
				action: s.status === ServerStatus.RUNNING ? 'Started' : 'Activity',
				time: s.lastStarted,
				status: s.status
			}))
	);

	let serversByStatus = $derived({
		healthy: dashboardServers.filter(
			(s) => s.status === ServerStatus.RUNNING && (!s.tps || s.tps >= 18)
		),
		warning: dashboardServers.filter(
			(s) => s.status === ServerStatus.RUNNING && s.tps && s.tps < 18 && s.tps >= 15
		),
		critical: dashboardServers.filter(
			(s) =>
				s.status === ServerStatus.ERROR ||
				s.status === ServerStatus.UNHEALTHY ||
				(s.status === ServerStatus.RUNNING && s.tps && s.tps < 15)
		)
	});

	onMount(() => {
		loadDashboardData().then(() => {
			isLoading = false;
		});

		const interval = setInterval(() => {
			currentTime = new Date();
		}, 1000);
		return () => clearInterval(interval);
	});

	function getStatusTagType(
		status: ServerStatus
	): 'green' | 'red' | 'purple' | 'gray' {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'green';
			case ServerStatus.STARTING:
			case ServerStatus.STOPPING:
			case ServerStatus.CREATING:
			case ServerStatus.RESTARTING:
				return 'purple';
			case ServerStatus.ERROR:
			case ServerStatus.UNHEALTHY:
				return 'red';
			case ServerStatus.STOPPED:
			default:
				return 'gray';
		}
	}

	const formatUptime = (lastStarted?: Timestamp) => {
		if (!lastStarted) return 'Never';
		const start = new Date(Number(lastStarted.seconds) * 1000);
		const diff = currentTime.getTime() - start.getTime();
		const days = Math.floor(diff / (1000 * 60 * 60 * 24));
		const hours = Math.floor((diff % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
		const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60));

		if (days > 0) return `${days}d ${hours}h`;
		if (hours > 0) return `${hours}h ${minutes}m`;
		return `${minutes}m`;
	};

	const getTpsColor = (tps: number | undefined) => {
		if (!tps) return 'text-[#8d8d8d]';
		if (tps >= 19) return 'text-[#24a148]';
		if (tps >= 17) return 'text-[#f1c21b]';
		return 'text-[#da1e28]';
	};
</script>

{#if isLoading}
	<div class="flex h-64 items-center justify-center p-6 bg-[#161616]">
		<div class="flex flex-col items-center gap-3">
			<div class="h-10 w-10 border-4 border-[#0f62fe] border-t-transparent animate-spin rounded-none"></div>
			<p class="font-sans text-xs text-[#8d8d8d] uppercase tracking-wider font-mono">Loading telemetry & cluster data...</p>
		</div>
	</div>
{:else}
	<div class="space-y-6 bg-[#161616] text-[#f4f4f4]">
		<!-- Carbon Page Header -->
		<div class="flex flex-col sm:flex-row sm:items-center justify-between pb-4 border-b border-[#393939] gap-4">
			<div>
				<div class="flex items-center gap-2">
					<h1 class="font-sans text-2xl font-light text-[#f4f4f4] tracking-tight">Cluster Overview</h1>
					<CarbonTag type="blue" size="sm">MINESERVER</CarbonTag>
				</div>
				<p class="font-sans text-xs text-[#8d8d8d] mt-1">
					Hardware utilization, game servers status, and network routing topology
				</p>
			</div>
			<div class="flex items-center gap-2">
				<CarbonButton
					kind="secondary"
					size="md"
					class="justify-center gap-2"
					onclick={refreshDashboard}
					disabled={isRefreshing}
				>
					<RefreshCw class="h-4 w-4 {isRefreshing ? 'animate-spin' : ''}" />
					<span>Refresh</span>
				</CarbonButton>
				<CarbonButton
					kind="primary"
					size="md"
					class="justify-center gap-2"
					href="/servers/new"
				>
					<Plus class="h-4 w-4" />
					<span>Create Server</span>
				</CarbonButton>
			</div>
		</div>

		<!-- Carbon KPI Tiles (cds--tile) -->
		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
			<!-- Tile 1: Total Servers -->
			<div class="cds--tile rounded-none bg-[#262626] border border-[#393939] p-5 flex flex-col justify-between select-none transition-colors hover:border-[#525252]">
				<div>
					<div class="flex items-center justify-between text-xs font-mono uppercase tracking-wider text-[#8d8d8d]">
						<span>Total Servers</span>
						<Server class="h-4 w-4 text-[#8d8d8d]" />
					</div>
					<div class="mt-2 text-3xl font-bold tracking-tight text-[#f4f4f4] font-mono">
						{stats.total}
					</div>
				</div>
				<div class="mt-4 pt-3 border-t border-[#393939] flex items-center justify-between text-xs font-mono">
					<div class="flex items-center gap-1.5 text-[#24a148]">
						<span class="w-1.5 h-1.5 bg-[#24a148]"></span>
						<span>{stats.running} ACTIVE</span>
					</div>
					{#if stats.error > 0}
						<div class="flex items-center gap-1.5 text-[#da1e28]">
							<span class="w-1.5 h-1.5 bg-[#da1e28]"></span>
							<span>{stats.error} ISSUES</span>
						</div>
					{:else}
						<span class="text-[#8d8d8d]">{stats.stopped} STOPPED</span>
					{/if}
				</div>
			</div>

			<!-- Tile 2: Active Players -->
			<div class="cds--tile rounded-none bg-[#262626] border border-[#393939] p-5 flex flex-col justify-between select-none transition-colors hover:border-[#525252]">
				<div>
					<div class="flex items-center justify-between text-xs font-mono uppercase tracking-wider text-[#8d8d8d]">
						<span>Active Players</span>
						<Users class="h-4 w-4 text-[#8d8d8d]" />
					</div>
					<div class="mt-2 text-3xl font-bold tracking-tight text-[#f4f4f4] font-mono">
						{stats.totalPlayers}
					</div>
				</div>
				<div class="mt-4 pt-3 border-t border-[#393939] flex items-center justify-between text-xs font-mono">
					{#if stats.totalPlayers > 0}
						<div class="flex items-center gap-1.5 text-[#24a148]">
							<span class="w-1.5 h-1.5 bg-[#24a148]"></span>
							<span>{stats.totalPlayers === 1 ? '1 PLAYER' : `${stats.totalPlayers} PLAYERS`} ONLINE</span>
						</div>
					{:else}
						<span class="text-[#8d8d8d]">0 PLAYERS ONLINE</span>
					{/if}
					{#if stats.totalMaxPlayers > 0}
						<span class="text-[#8d8d8d]">MAX: {stats.totalMaxPlayers}</span>
					{/if}
				</div>
			</div>

			<!-- Tile 3: Memory Usage -->
			<div class="cds--tile rounded-none bg-[#262626] border border-[#393939] p-5 flex flex-col justify-between select-none transition-colors hover:border-[#525252]">
				<div>
					<div class="flex items-center justify-between text-xs font-mono uppercase tracking-wider text-[#8d8d8d]">
						<span>Memory Allocation</span>
						<MemoryStick class="h-4 w-4 text-[#8d8d8d]" />
					</div>
					<div class="mt-2 flex items-baseline gap-1.5">
						<span class="text-3xl font-bold tracking-tight text-[#f4f4f4] font-mono">
							{stats.totalMemory > 0 ? (stats.usedMemory / 1024).toFixed(1) : '0.0'}
						</span>
						<span class="text-sm font-mono text-[#8d8d8d]">
							/ {stats.totalMemory > 0 ? (stats.totalMemory / 1024).toFixed(1) : '0.0'} GB
						</span>
					</div>
					<!-- Sharp Carbon Progress Bar -->
					<div class="w-full bg-[#393939] h-1.5 mt-3 rounded-none overflow-hidden">
						<div
							class="h-full bg-[#0f62fe] transition-all duration-300"
							style="width: {stats.memUsagePercent}%"
						></div>
					</div>
				</div>
				<div class="mt-4 pt-3 border-t border-[#393939] flex items-center justify-between text-xs font-mono">
					<div class="flex items-center gap-1.5 {stats.memUsagePercent > 85 ? 'text-[#da1e28]' : stats.memUsagePercent > 70 ? 'text-[#f1c21b]' : 'text-[#24a148]'}">
						<span class="w-1.5 h-1.5 {stats.memUsagePercent > 85 ? 'bg-[#da1e28]' : stats.memUsagePercent > 70 ? 'bg-[#f1c21b]' : 'bg-[#24a148]'}"></span>
						<span>{stats.memUsagePercent}% UTILIZED</span>
					</div>
					<span class="text-[#8d8d8d]">RAM POOL</span>
				</div>
			</div>

			<!-- Tile 4: Cluster Performance -->
			<div class="cds--tile rounded-none bg-[#262626] border border-[#393939] p-5 flex flex-col justify-between select-none transition-colors hover:border-[#525252]">
				<div>
					<div class="flex items-center justify-between text-xs font-mono uppercase tracking-wider text-[#8d8d8d]">
						<span>Cluster Performance</span>
						<Gauge class="h-4 w-4 text-[#8d8d8d]" />
					</div>
					<div class="mt-2 flex items-baseline gap-2">
						<span class="text-3xl font-bold tracking-tight {stats.avgTps >= 18 ? 'text-[#24a148]' : stats.avgTps > 0 ? 'text-[#da1e28]' : 'text-[#f4f4f4]'} font-mono">
							{stats.avgTps > 0 ? stats.avgTps.toFixed(1) : '20.0'}
						</span>
						<span class="text-sm font-mono text-[#8d8d8d]">AVG TPS</span>
					</div>
				</div>
				<div class="mt-4 pt-3 border-t border-[#393939] flex items-center justify-between text-xs font-mono">
					<div class="flex items-center gap-1.5 {stats.avgTps >= 18 || stats.avgTps === 0 ? 'text-[#24a148]' : 'text-[#da1e28]'}">
						<span class="w-1.5 h-1.5 {stats.avgTps >= 18 || stats.avgTps === 0 ? 'bg-[#24a148]' : 'bg-[#da1e28]'}"></span>
						<span>{stats.avgTps >= 18 || stats.avgTps === 0 ? 'HEALTHY' : 'DEGRADED'}</span>
					</div>
					<span class="text-[#8d8d8d]">
						{stats.avgCpu > 0 ? `${stats.avgCpu.toFixed(0)}% CPU` : 'LOAD NORMAL'}
					</span>
				</div>
			</div>
		</div>

		<!-- Carbon Inline Notification (when servers need attention) -->
		{#if serversByStatus.critical.length > 0 || serversByStatus.warning.length > 0}
			<div class="rounded-none border-l-4 {serversByStatus.critical.length > 0 ? 'border-l-[#da1e28]' : 'border-l-[#f1c21b]'} border-t border-r border-b border-[#393939] bg-[#262626] p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-sm">
				<div class="flex items-center gap-3">
					<AlertTriangle class="h-5 w-5 {serversByStatus.critical.length > 0 ? 'text-[#da1e28]' : 'text-[#f1c21b]'} shrink-0" />
					<div class="text-xs font-sans text-[#f4f4f4]">
						{#if serversByStatus.critical.length > 0}
							<span class="font-bold text-[#da1e28] font-mono">{serversByStatus.critical.length} SERVER{serversByStatus.critical.length > 1 ? 'S' : ''} CRITICAL</span>: require operator intervention.
						{/if}
						{#if serversByStatus.warning.length > 0}
							<span class="font-bold text-[#f1c21b] font-mono {serversByStatus.critical.length > 0 ? 'ml-2' : ''}">{serversByStatus.warning.length} SERVER{serversByStatus.warning.length > 1 ? 'S' : ''} RUNNING SLOW</span>: sub-optimal TPS detected.
						{/if}
					</div>
				</div>
				<a href="/servers" class="text-xs font-mono uppercase tracking-wider text-[#78a9ff] hover:text-white flex items-center gap-1 shrink-0">
					<span>Inspect Servers</span>
					<ChevronRight class="h-3.5 w-3.5" />
				</a>
			</div>
		{/if}

		<!-- Server list & Recent activity Grid -->
		<div class="grid gap-6 lg:grid-cols-7">
			<!-- Carbon DataTable: Server Overview (4 cols on lg) -->
			<div class="lg:col-span-4 border border-[#393939] bg-[#262626] rounded-none flex flex-col justify-between">
				<div>
					<!-- Table Toolbar / Header -->
					<div class="flex items-center justify-between p-4 bg-[#262626] border-b border-[#393939]">
						<div>
							<h2 class="font-sans text-sm font-semibold uppercase tracking-wider text-[#f4f4f4]">Server Overview</h2>
							<p class="font-sans text-xs text-[#8d8d8d] mt-0.5">Instance telemetry & state across cluster</p>
						</div>
						<a
							href="/servers"
							class="h-8 px-3 inline-flex items-center gap-1 text-xs font-mono uppercase tracking-wider text-[#78a9ff] hover:bg-[#353535] hover:text-white transition-colors"
						>
							<span>View All ({dashboardServers.length})</span>
							<ChevronRight class="h-3.5 w-3.5" />
						</a>
					</div>

					<!-- Table Content -->
					{#if dashboardServers.length === 0}
						<div class="py-12 px-4 text-center">
							<div class="mx-auto mb-3 flex h-10 w-10 items-center justify-center bg-[#161616] border border-[#393939]">
								<Server class="h-5 w-5 text-[#8d8d8d]" />
							</div>
							<h3 class="text-sm font-semibold text-[#f4f4f4]">No servers configured</h3>
							<p class="mt-1 mb-4 text-xs text-[#8d8d8d] font-mono">Create your first Minecraft server instance</p>
							<CarbonButton kind="primary" size="sm" href="/servers/new" class="inline-flex justify-center gap-2">
								<Plus class="h-3.5 w-3.5" />
								<span>Create Server</span>
							</CarbonButton>
						</div>
					{:else}
						<div class="overflow-x-auto">
							<table class="w-full border-collapse text-left font-sans text-sm">
								<thead class="bg-[#393939] text-[#f4f4f4] border-b border-[#525252]">
									<tr>
										<th class="px-4 py-2.5 text-xs font-semibold uppercase tracking-wider text-[#f4f4f4]">Server</th>
										<th class="px-4 py-2.5 text-xs font-semibold uppercase tracking-wider text-[#f4f4f4]">Status</th>
										<th class="px-4 py-2.5 text-xs font-semibold uppercase tracking-wider text-[#f4f4f4]">Version</th>
										<th class="px-4 py-2.5 text-xs font-semibold uppercase tracking-wider text-[#f4f4f4]">Metrics</th>
										<th class="px-4 py-2.5 text-right text-xs font-semibold uppercase tracking-wider text-[#f4f4f4]">Actions</th>
									</tr>
								</thead>
								<tbody class="divide-y divide-[#393939] bg-[#262626] text-[#f4f4f4]">
									{#each sortServersByActivity([...dashboardServers]).slice(0, 5) as server (server.id)}
										<tr class="hover:bg-[#353535] transition-colors group">
											<td class="px-4 py-3 align-middle">
												<a href="/servers/{server.id}" class="hover:text-[#78a9ff] transition-colors block">
													<div class="font-medium text-sm text-[#f4f4f4]">{server.name}</div>
													<div class="text-xs font-mono text-[#8d8d8d]">{server.id.slice(0, 8)}</div>
												</a>
											</td>
											<td class="px-4 py-3 align-middle">
												<CarbonTag type={getStatusTagType(server.status)} size="sm">
													{getStringForEnum(ServerStatus, server.status)}
												</CarbonTag>
											</td>
											<td class="px-4 py-3 align-middle text-xs font-mono text-[#c6c6c6]">
												{server.mcVersion || '—'}
											</td>
											<td class="px-4 py-3 align-middle">
												{#if server.status === ServerStatus.RUNNING}
													<div class="flex items-center gap-3 text-xs font-mono">
														<span class="text-[#c6c6c6] flex items-center gap-1">
															<Users class="h-3.5 w-3.5 text-[#8d8d8d]" />
															{server.playersOnline || 0}/{server.maxPlayers}
														</span>
														{#if server.tps}
															<span class="flex items-center gap-1 {getTpsColor(server.tps)}">
																<Zap class="h-3.5 w-3.5" />
																{server.tps.toFixed(1)}
															</span>
														{/if}
													</div>
												{:else}
													<span class="text-xs font-mono text-[#8d8d8d]">OFFLINE</span>
												{/if}
											</td>
											<td class="px-4 py-3 align-middle text-right">
												<div class="flex items-center justify-end gap-1">
													{#if server.status === ServerStatus.STOPPED}
														<button
															type="button"
															onclick={() => handleServerAction('start', server)}
															class="h-7 w-7 flex items-center justify-center text-[#24a148] hover:bg-[#393939] hover:text-[#42be65] transition-colors cursor-pointer"
															title="Start Server"
															aria-label="Start Server"
														>
															<PlayCircle class="h-4 w-4" />
														</button>
													{:else if server.status === ServerStatus.RUNNING}
														<button
															type="button"
															onclick={() => handleServerAction('stop', server)}
															class="h-7 w-7 flex items-center justify-center text-[#da1e28] hover:bg-[#393939] hover:text-[#ff8389] transition-colors cursor-pointer"
															title="Stop Server"
															aria-label="Stop Server"
														>
															<StopCircle class="h-4 w-4" />
														</button>
													{/if}
													<a
														href="/servers/{server.id}"
														class="h-7 px-2.5 inline-flex items-center text-xs font-mono uppercase tracking-wider text-[#78a9ff] hover:bg-[#393939] hover:text-white transition-colors"
													>
														Manage
													</a>
												</div>
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</div>
			</div>

			<!-- Carbon Structured List: Recent Activity (3 cols on lg) -->
			<div class="lg:col-span-3 border border-[#393939] bg-[#262626] rounded-none flex flex-col">
				<!-- Header -->
				<div class="flex items-center justify-between p-4 bg-[#262626] border-b border-[#393939]">
					<div>
						<h2 class="font-sans text-sm font-semibold uppercase tracking-wider text-[#f4f4f4]">Recent Activity</h2>
						<p class="font-sans text-xs text-[#8d8d8d] mt-0.5">Latest lifecycle and cluster events</p>
					</div>
					<Clock class="h-4 w-4 text-[#8d8d8d]" />
				</div>

				<!-- Content -->
				{#if recentActivity.length === 0}
					<div class="p-8 text-center text-[#8d8d8d] flex-1 flex flex-col items-center justify-center">
						<Clock class="mb-2 h-8 w-8 text-[#525252]" />
						<p class="text-xs font-mono uppercase tracking-wider">No recent events recorded</p>
					</div>
				{:else}
					<!-- Structured List Header -->
					<div class="bg-[#393939] text-[#f4f4f4] border-b border-[#525252] px-4 py-2.5 flex items-center justify-between text-xs font-semibold uppercase tracking-wider">
						<span>Event / Instance</span>
						<span>Timestamp</span>
					</div>
					<!-- Structured List Rows -->
					<div class="divide-y divide-[#393939] bg-[#262626] flex-1">
						{#each recentActivity as activity (activity.server + (activity.time?.seconds ?? ''))}
							<div class="flex items-center justify-between px-4 py-3 hover:bg-[#353535] transition-colors">
								<div class="flex items-center gap-3 min-w-0">
									<!-- Sharp status pip -->
									<div class="w-2 h-2 shrink-0 {activity.status === ServerStatus.RUNNING ? 'bg-[#24a148]' : activity.status === ServerStatus.STOPPED ? 'bg-[#8d8d8d]' : 'bg-[#da1e28]'}"></div>
									<div class="min-w-0">
										<p class="font-medium text-sm text-[#f4f4f4] truncate">{activity.server}</p>
										<p class="text-xs text-[#8d8d8d] font-mono">{activity.action}</p>
									</div>
								</div>
								<div class="text-xs font-mono text-[#8d8d8d] shrink-0 pl-3">
									{formatUptime(activity.time)} ago
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		</div>

		<!-- Carbon Information Grid (3 columns) -->
		<div class="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
			<!-- Cluster Operations & Shortcuts -->
			<div class="border border-[#393939] bg-[#262626] rounded-none p-5 flex flex-col justify-between">
				<div>
					<div class="flex items-center justify-between pb-3 border-b border-[#393939]">
						<div>
							<h2 class="font-sans text-sm font-semibold uppercase tracking-wider text-[#f4f4f4]">Cluster Operations</h2>
							<p class="font-sans text-xs text-[#8d8d8d] mt-0.5">Management shortcuts & ingress</p>
						</div>
					</div>
					<div class="mt-4 space-y-2">
						<a
							href="/servers/new"
							class="flex items-center justify-between p-3 bg-[#161616] hover:bg-[#353535] border border-[#393939] text-xs font-mono text-[#f4f4f4] transition-colors rounded-none"
						>
							<div class="flex items-center gap-2.5">
								<Plus class="h-4 w-4 text-[#0f62fe]" />
								<span>Deploy Game Server</span>
							</div>
							<ChevronRight class="h-4 w-4 text-[#8d8d8d]" />
						</a>
						<a
							href="/modpacks/studio"
							class="flex items-center justify-between p-3 bg-[#161616] hover:bg-[#353535] border border-[#393939] text-xs font-mono text-[#f4f4f4] transition-colors rounded-none"
						>
							<div class="flex items-center gap-2.5">
								<Package class="h-4 w-4 text-[#be95ff]" />
								<span>Modpack Studio & TOML</span>
							</div>
							<ChevronRight class="h-4 w-4 text-[#8d8d8d]" />
						</a>
						<a
							href="/settings?tab=routing"
							class="flex items-center justify-between p-3 bg-[#161616] hover:bg-[#353535] border border-[#393939] text-xs font-mono text-[#f4f4f4] transition-colors rounded-none"
						>
							<div class="flex items-center gap-2.5">
								<Globe class="h-4 w-4 text-[#42be65]" />
								<span>Virtual Host Routing (:25565)</span>
							</div>
							<ChevronRight class="h-4 w-4 text-[#8d8d8d]" />
						</a>
					</div>
				</div>
			</div>

			<!-- System Infrastructure Health -->
			<div class="border border-[#393939] bg-[#262626] rounded-none p-5 flex flex-col justify-between">
				<div>
					<div class="flex items-center justify-between pb-3 border-b border-[#393939]">
						<div>
							<h2 class="font-sans text-sm font-semibold uppercase tracking-wider text-[#f4f4f4]">System Health</h2>
							<p class="font-sans text-xs text-[#8d8d8d] mt-0.5">Underlying daemon and network state</p>
						</div>
					</div>
					<div class="mt-4 divide-y divide-[#393939]">
						<div class="flex items-center justify-between py-2.5">
							<span class="text-xs font-mono text-[#c6c6c6]">RPC TELEMETRY</span>
							<CarbonTag type="green" size="sm">OPERATIONAL</CarbonTag>
						</div>
						<div class="flex items-center justify-between py-2.5">
							<span class="text-xs font-mono text-[#c6c6c6]">NETWORK PROXY</span>
							<CarbonTag type={dashboardServers.some((s) => s.status === ServerStatus.RUNNING) ? 'green' : 'gray'} size="sm">
								{dashboardServers.some((s) => s.status === ServerStatus.RUNNING) ? 'CONNECTED' : 'IDLE'}
							</CarbonTag>
						</div>
						<div class="flex items-center justify-between py-2.5">
							<span class="text-xs font-mono text-[#c6c6c6]">STORAGE ALLOCATION</span>
							<span class="text-xs font-mono text-[#f4f4f4]">
								{stats.totalDiskUsage > 0 ? `${formatBytes(stats.totalDiskUsage)}${stats.totalDiskSize}` : 'LOCAL VOLUMES'}
							</span>
						</div>
					</div>
				</div>
			</div>

			<!-- Quick Cluster Stats -->
			<div class="border border-[#393939] bg-[#262626] rounded-none p-5 flex flex-col justify-between">
				<div>
					<div class="flex items-center justify-between pb-3 border-b border-[#393939]">
						<div>
							<h2 class="font-sans text-sm font-semibold uppercase tracking-wider text-[#f4f4f4]">Quick Metrics</h2>
							<p class="font-sans text-xs text-[#8d8d8d] mt-0.5">High-level cluster telemetry</p>
						</div>
					</div>
					<div class="mt-4 grid grid-cols-2 gap-2">
						<div class="bg-[#161616] border border-[#393939] p-3 rounded-none">
							<p class="text-[10px] font-mono text-[#8d8d8d] uppercase">ACTIVE RATIO</p>
							<p class="text-xl font-bold font-mono text-[#24a148] mt-1">
								{stats.running > 0 ? `${((stats.running / Math.max(stats.total, 1)) * 100).toFixed(0)}%` : '0%'}
							</p>
						</div>
						<div class="bg-[#161616] border border-[#393939] p-3 rounded-none">
							<p class="text-[10px] font-mono text-[#8d8d8d] uppercase">AVG CPU</p>
							<p class="text-xl font-bold font-mono {stats.avgCpu > 80 ? 'text-[#da1e28]' : 'text-[#f4f4f4]'} mt-1">
								{stats.avgCpu > 0 ? `${stats.avgCpu.toFixed(0)}%` : 'NORMAL'}
							</p>
						</div>
						<div class="bg-[#161616] border border-[#393939] p-3 rounded-none">
							<p class="text-[10px] font-mono text-[#8d8d8d] uppercase">TICK RATE</p>
							<p class="text-xl font-bold font-mono {stats.avgTps >= 18 || stats.avgTps === 0 ? 'text-[#24a148]' : 'text-[#da1e28]'} mt-1">
								{stats.avgTps > 0 ? stats.avgTps.toFixed(1) : '20.0'}
							</p>
						</div>
						<div class="bg-[#161616] border border-[#393939] p-3 rounded-none">
							<p class="text-[10px] font-mono text-[#8d8d8d] uppercase">PLAYERS</p>
							<p class="text-xl font-bold font-mono text-[#24a148] mt-1">
								{stats.totalPlayers}
							</p>
						</div>
					</div>
				</div>
			</div>
		</div>
	</div>
{/if}
