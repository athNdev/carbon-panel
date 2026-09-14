<script lang="ts">
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Progress } from '$lib/components/ui/progress';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { formatBytes, getStringForEnum } from '$lib/utils';
	import { resolve } from '$app/paths';
	import { onMount } from 'svelte';
	import {
		Server,
		MemoryStick,
		Plus,
		LayoutDashboard,
		AlertCircle,
		PlayCircle,
		StopCircle,
		Clock,
		TrendingUp,
		Users,
		Zap,
		ChevronRight,
		Github,
		MessageCircle,
		HelpCircle,
		BookOpen,
		Shield,
		Gauge,
		Database,
		Wifi,
		WifiOff,
		CheckCircle,
		XCircle,
		AlertTriangle,
		RefreshCw
	} from '@lucide/svelte';
	import { ServerStatus, type Server as ServerType } from '$lib/proto/mineserver/v1/common_pb';
	import { rpcClient } from '$lib/api/rpc-client';
	import { serversStore, sortServersByActivity } from '$lib/stores/servers';
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
			dashboardServers.length > 0
				? ` / ${dashboardServers?.[0]?.diskTotal && formatBytes(Number(dashboardServers[0].diskTotal))}`
				: '',
		avgCpu: dashboardServers
			.filter((s) => s.cpuPercent && s.cpuPercent > 0)
			.reduce((acc, s, _, arr) => acc + (s.cpuPercent || 0) / arr.length, 0)
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
		// Load dashboard data on mount
		loadDashboardData().then(() => {
			isLoading = false;
		});

		// Update time for relative timestamps
		const interval = setInterval(() => {
			currentTime = new Date();
		}, 1000);
		return () => clearInterval(interval);
	});

	const getStatusColor = (status: ServerStatus) => {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'text-green-500';
			case ServerStatus.STARTING:
			case ServerStatus.STOPPING:
			case ServerStatus.CREATING:
			case ServerStatus.RESTARTING:
				return 'text-yellow-500';
			case ServerStatus.STOPPED:
				return 'text-gray-400';
			case ServerStatus.ERROR:
			case ServerStatus.UNHEALTHY:
				return 'text-red-500';
			default:
				return 'text-gray-400';
		}
	};

	const getStatusIcon = (status: ServerStatus) => {
		switch (status) {
			case ServerStatus.RUNNING:
				return CheckCircle;
			case ServerStatus.STARTING:
			case ServerStatus.STOPPING:
			case ServerStatus.CREATING:
			case ServerStatus.RESTARTING:
				return AlertCircle;
			case ServerStatus.STOPPED:
				return XCircle;
			case ServerStatus.ERROR:
			case ServerStatus.UNHEALTHY:
				return AlertTriangle;
			default:
				return AlertCircle;
		}
	};

	const getStatusBadgeColor = (status: ServerStatus) => {
		switch (status) {
			case ServerStatus.RUNNING:
				return 'bg-green-500/10 text-green-500 border-green-500/20';
			case ServerStatus.STARTING:
			case ServerStatus.STOPPING:
			case ServerStatus.CREATING:
			case ServerStatus.RESTARTING:
				return 'bg-yellow-500/10 text-yellow-500 border-yellow-500/20';
			case ServerStatus.STOPPED:
				return 'bg-gray-500/10 text-gray-500 border-gray-500/20';
			case ServerStatus.ERROR:
			case ServerStatus.UNHEALTHY:
				return 'bg-red-500/10 text-red-500 border-red-500/20';
			default:
				return 'bg-gray-500/10 text-gray-500 border-gray-500/20';
		}
	};

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
		if (!tps) return 'text-gray-400';
		if (tps >= 19) return 'text-green-500';
		if (tps >= 17) return 'text-yellow-500';
		if (tps >= 15) return 'text-orange-500';
		return 'text-red-500';
	};

	const getCpuColor = (cpu: number | undefined) => {
		if (!cpu) return 'text-gray-400';
		if (cpu <= 50) return 'text-green-500';
		if (cpu <= 70) return 'text-yellow-500';
		if (cpu <= 85) return 'text-orange-500';
		return 'text-red-500';
	};
</script>

{#if isLoading}
	<div class="flex h-64 items-center justify-center p-6">
		<div class="flex flex-col items-center gap-3">
			<div class="h-10 w-10 border-4 border-[#0f62fe] border-t-transparent animate-spin"></div>
			<p class="font-sans text-xs text-[#a8a8a8]">Loading telemetry & cluster data...</p>
		</div>
	</div>
{:else}
	<div class="space-y-6">
		<!-- Carbon Page Header -->
		<div class="flex flex-col sm:flex-row sm:items-center justify-between pb-4 border-b border-[#393939] gap-4">
			<div>
				<div class="flex items-center gap-2">
					<h1 class="font-sans text-2xl font-light text-[#f4f4f4]">Cluster Overview</h1>
					<span class="px-2 py-0.5 bg-[#0f62fe]/20 text-[#78a9ff] border border-[#0f62fe]/40 text-xs font-mono">
						MINESERVER
					</span>
				</div>
				<p class="font-sans text-xs text-[#a8a8a8] mt-1">
					Hardware utilization, game servers status, and network routing topology
				</p>
			</div>
			<div class="flex items-center gap-2">
				<button
					type="button"
					onclick={refreshDashboard}
					disabled={isRefreshing}
					class="h-10 px-4 flex items-center gap-2 bg-[#262626] hover:bg-[#353535] text-[#f4f4f4] border border-[#393939] text-sm font-sans cursor-pointer transition-colors"
				>
					<RefreshCw class="h-4 w-4 {isRefreshing ? 'animate-spin' : ''}" />
					<span>Refresh</span>
				</button>
				<a
					href="/servers/new"
					class="h-10 px-4 flex items-center gap-2 bg-[#0f62fe] hover:bg-[#0353e9] text-white text-sm font-sans font-medium cursor-pointer transition-colors"
				>
					<Plus class="h-4 w-4" />
					<span>New Server</span>
				</a>
			</div>
		</div>

		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
			<Card
				class="group relative animate-in overflow-hidden border-border/50 transition-all duration-500 fade-in-50 slide-in-from-bottom-2 hover:border-primary/30 hover:shadow-lg"
			>
				<div
					class="absolute inset-0 bg-linear-to-br from-primary/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
				></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
					<CardTitle class="text-xs font-medium tracking-wider text-muted-foreground uppercase"
						>Total Servers</CardTitle
					>
					<div
						class="flex h-10 w-10 items-center justify-center rounded-xl bg-linear-to-br from-blue-500/20 to-blue-600/10 transition-transform group-hover:scale-110"
					>
						<Server class="h-5 w-5 text-blue-500" />
					</div>
				</CardHeader>
				<CardContent>
					{#if isLoading}
						<Skeleton class="mb-2 h-8 w-16" />
						<Skeleton class="h-4 w-32" />
					{:else}
						<div class="text-2xl font-bold">{stats.total}</div>
						<div class="mt-2 flex items-center gap-3">
							<div class="flex items-center gap-1">
								<div class="h-2 w-2 animate-pulse rounded-full bg-green-500"></div>
								<span class="text-xs text-muted-foreground">{stats.running} active</span>
							</div>
							{#if stats.error > 0}
								<div class="flex items-center gap-1">
									<div class="h-2 w-2 rounded-full bg-red-500"></div>
									<span class="text-xs text-red-500">{stats.error} issues</span>
								</div>
							{/if}
						</div>
					{/if}
				</CardContent>
			</Card>

			<Card
				class="group relative animate-in overflow-hidden border-border/50 transition-all duration-500 fade-in-50 slide-in-from-bottom-2 hover:border-primary/30 hover:shadow-lg"
				style="animation-delay: 50ms"
			>
				<div
					class="absolute inset-0 bg-linear-to-br from-green-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
				></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
					<CardTitle class="text-xs font-medium tracking-wider text-muted-foreground uppercase"
						>Active Players</CardTitle
					>
					<div
						class="flex h-10 w-10 items-center justify-center rounded-xl bg-linear-to-br from-green-500/20 to-green-600/10 transition-transform group-hover:scale-110"
					>
						<Users class="h-5 w-5 text-green-500" />
					</div>
				</CardHeader>
				<CardContent>
					{#if isLoading}
						<Skeleton class="mb-2 h-8 w-20" />
						<Skeleton class="h-2 w-full" />
					{:else if stats.totalPlayers > 0}
						<div class="text-2xl font-bold">{stats.totalPlayers}</div>
						<p class="mt-1 text-xs text-muted-foreground">
							{stats.totalPlayers === 1 ? 'player' : 'players'} online
						</p>
					{:else}
						<div class="text-2xl font-bold">0</div>
						<p class="mt-1 text-xs text-muted-foreground">No players online</p>
					{/if}
				</CardContent>
			</Card>

			<Card
				class="group relative animate-in overflow-hidden border-border/50 transition-all duration-500 fade-in-50 slide-in-from-bottom-2 hover:border-primary/30 hover:shadow-lg"
				style="animation-delay: 100ms"
			>
				<div
					class="absolute inset-0 bg-linear-to-br from-purple-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
				></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
					<CardTitle class="text-xs font-medium tracking-wider text-muted-foreground uppercase"
						>Memory Usage</CardTitle
					>
					<div
						class="flex h-10 w-10 items-center justify-center rounded-xl bg-linear-to-br from-purple-500/20 to-purple-600/10 transition-transform group-hover:scale-110"
					>
						<MemoryStick class="h-5 w-5 text-purple-500" />
					</div>
				</CardHeader>
				<CardContent>
					{#if isLoading}
						<Skeleton class="mb-2 h-8 w-24" />
						<Skeleton class="h-2 w-full" />
					{:else if stats.totalMemory > 0}
						<div class="flex items-baseline gap-1">
							<span class="text-2xl font-bold">{(stats.usedMemory / 1024).toFixed(1)}</span>
							<span class="text-sm text-muted-foreground"
								>/ {(stats.totalMemory / 1024).toFixed(1)} GB</span
							>
						</div>
						<Progress
							value={(stats.usedMemory / Math.max(stats.totalMemory, 1)) * 100}
							class="mt-2 h-2 bg-purple-500/10"
						/>
						<p class="mt-1 text-xs text-muted-foreground">Used / Allocated</p>
					{:else}
						<div class="text-2xl font-bold text-muted-foreground">â€”</div>
						<p class="mt-1 text-xs text-muted-foreground">No data available</p>
					{/if}
				</CardContent>
			</Card>

			<Card
				class="group relative animate-in overflow-hidden border-border/50 transition-all duration-500 fade-in-50 slide-in-from-bottom-2 hover:border-primary/30 hover:shadow-lg"
				style="animation-delay: 150ms"
			>
				<div
					class="absolute inset-0 bg-linear-to-br from-orange-500/5 via-transparent to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100"
				></div>
				<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
					<CardTitle class="text-xs font-medium tracking-wider text-muted-foreground uppercase"
						>Performance</CardTitle
					>
					<div
						class="flex h-10 w-10 items-center justify-center rounded-xl bg-linear-to-br from-orange-500/20 to-orange-600/10 transition-transform group-hover:scale-110"
					>
						<Gauge class="h-5 w-5 text-orange-500" />
					</div>
				</CardHeader>
				<CardContent>
					{#if isLoading}
						<Skeleton class="mb-2 h-8 w-20" />
						<Skeleton class="h-4 w-24" />
					{:else if stats.avgTps > 0}
						<div class="flex items-baseline gap-2">
							<span class="text-2xl font-bold {getTpsColor(stats.avgTps)}"
								>{stats.avgTps.toFixed(1)}</span
							>
							<span class="text-sm text-muted-foreground">Avg. TPS</span>
						</div>
						<p class="mt-1 text-xs text-muted-foreground">
							{stats.avgCpu > 0 ? `${stats.avgCpu.toFixed(1)}% CPU` : 'CPU data unavailable'}
						</p>
					{:else}
						<div class="text-2xl font-bold text-muted-foreground">â€”</div>
						<p class="mt-1 text-xs text-muted-foreground">Performance monitoring inactive</p>
					{/if}
				</CardContent>
			</Card>
		</div>

		{#if serversByStatus.critical.length > 0 || serversByStatus.warning.length > 0}
			<Alert
				class="animate-in border-orange-500/20 bg-orange-500/5 duration-500 fade-in-50 slide-in-from-top-2"
			>
				<AlertCircle class="h-4 w-4 text-orange-500" />
				<AlertDescription class="ml-2">
					{#if serversByStatus.critical.length > 0}
						<span class="font-medium text-red-500"
							>{serversByStatus.critical.length} server{serversByStatus.critical.length > 1
								? 's'
								: ''} need attention.</span
						>
					{/if}
					{#if serversByStatus.warning.length > 0}
						<span class="font-medium text-yellow-500"
							>{serversByStatus.warning.length} server{serversByStatus.warning.length > 1
								? 's'
								: ''} running slow.</span
						>
					{/if}
					<a href={resolve('/servers')} class="ml-2 text-primary hover:underline">View details â†’</a>
				</AlertDescription>
			</Alert>
		{/if}

		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-7">
			<Card
				class="col-span-full animate-in duration-500 fade-in-50 slide-in-from-left-5 lg:col-span-4"
				style="animation-delay: 200ms"
			>
				<CardHeader>
					<div class="flex items-center justify-between">
						<div>
							<CardTitle>Server Overview</CardTitle>
							<CardDescription>Quick status of all your servers</CardDescription>
						</div>
						<Button variant="ghost" size="sm" href="/servers">
							View All
							<ChevronRight class="ml-1 h-4 w-4" />
						</Button>
					</div>
				</CardHeader>
				<CardContent>
					{#if dashboardServers.length === 0}
						<div class="py-12 text-center">
							<div
								class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-muted"
							>
								<Server class="h-6 w-6 text-muted-foreground" />
							</div>
							<h3 class="mb-1 text-sm font-semibold">No servers yet</h3>
							<p class="mb-4 text-sm text-muted-foreground">
								Create your first server to get started
							</p>
							<Button href="/servers/new" size="sm">
								<Plus class="mr-2 h-4 w-4" />
								Create Server
							</Button>
						</div>
					{:else}
						<div class="space-y-3">
							{#each sortServersByActivity([...dashboardServers]).slice(0, 5) as server (server.id)}
								{@const StatusIcon = getStatusIcon(server.status)}
								<div
									class="group flex items-center justify-between rounded-lg p-3 transition-colors hover:bg-muted/50"
								>
									<div class="flex flex-1 items-center gap-3">
										<div class="relative">
											<StatusIcon class="h-5 w-5 {getStatusColor(server.status)}" />
											{#if server.status === ServerStatus.RUNNING}
												<div
													class="absolute -top-1 -right-1 h-2 w-2 animate-pulse rounded-full bg-green-500"
												></div>
											{/if}
										</div>
										<div class="min-w-0 flex-1">
											<div class="flex items-center gap-2">
												<p class="truncate text-sm font-medium">{server.name}</p>
												<Badge
													variant="outline"
													class="text-xs {getStatusBadgeColor(server.status)} border"
												>
													{getStringForEnum(ServerStatus, server.status)}
												</Badge>
											</div>
											<div class="mt-1 flex items-center gap-3">
												<span class="text-xs text-muted-foreground">{server.mcVersion}</span>
												{#if server.status === ServerStatus.RUNNING}
													<span class="flex items-center gap-1 text-xs text-muted-foreground">
														<Users class="h-3 w-3" />
														{server.playersOnline || 0}/{server.maxPlayers}
													</span>
													{#if server.tps}
														<span class="flex items-center gap-1 text-xs {getTpsColor(server.tps)}">
															<Zap class="h-3 w-3" />
															{server.tps.toFixed(1)} TPS
														</span>
													{/if}
												{/if}
											</div>
										</div>
									</div>
									<div
										class="flex items-center gap-2 opacity-0 transition-opacity group-hover:opacity-100"
									>
										{#if server.status === ServerStatus.STOPPED}
											<Button variant="ghost" size="sm" class="h-8 w-8 p-0">
												<PlayCircle class="h-4 w-4" />
											</Button>
										{:else if server.status === ServerStatus.RUNNING}
											<Button variant="ghost" size="sm" class="h-8 w-8 p-0">
												<StopCircle class="h-4 w-4" />
											</Button>
										{/if}
										<Button variant="ghost" size="sm" href="/servers/{server.id}">Manage</Button>
									</div>
								</div>
							{/each}
						</div>
					{/if}
				</CardContent>
			</Card>

			<Card
				class="col-span-full animate-in duration-500 fade-in-50 slide-in-from-right-5 lg:col-span-3"
				style="animation-delay: 250ms"
			>
				<CardHeader>
					<CardTitle>Recent Activity</CardTitle>
					<CardDescription>Latest server events and actions</CardDescription>
				</CardHeader>
				<CardContent>
					{#if recentActivity.length === 0}
						<div class="py-8 text-center text-muted-foreground">
							<Clock class="mx-auto mb-2 h-8 w-8" />
							<p class="text-sm">No recent activity</p>
						</div>
					{:else}
						<div class="space-y-3">
							{#each recentActivity as activity, i (activity)}
								<div
									class="flex animate-in items-start gap-3 fade-in-50 slide-in-from-right-2"
									style="animation-delay: {300 + i * 50}ms"
								>
									<div class="mt-1">
										{#if activity.status === ServerStatus.RUNNING}
											<div class="h-2 w-2 animate-pulse rounded-full bg-green-500"></div>
										{:else if activity.status === ServerStatus.STOPPED}
											<div class="h-2 w-2 rounded-full bg-gray-400"></div>
										{:else}
											<div class="h-2 w-2 rounded-full bg-yellow-500"></div>
										{/if}
									</div>
									<div class="flex-1 space-y-1">
										<p class="text-sm">
											<span class="font-medium">{activity.server}</span>
											<span class="ml-1 text-muted-foreground">{activity.action}</span>
										</p>
										<p class="text-xs text-muted-foreground">
											{formatUptime(activity.time)} ago
										</p>
									</div>
								</div>
							{/each}
						</div>
					{/if}
				</CardContent>
			</Card>
		</div>

		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
			<Card
				class="group animate-in border-border/50 transition-all duration-500 fade-in-50 slide-in-from-bottom-5 hover:border-primary/30 hover:shadow-lg"
				style="animation-delay: 350ms"
			>
				<CardHeader>
					<div class="flex items-center justify-between">
						<div class="flex items-center gap-3">
							<div
								class="flex h-10 w-10 items-center justify-center rounded-xl bg-linear-to-br from-blue-500/20 to-blue-600/10"
							>
								<HelpCircle class="h-5 w-5 text-blue-500" />
							</div>
							<div>
								<CardTitle class="text-base">Need Help?</CardTitle>
								<CardDescription class="text-xs">Get support from our community</CardDescription>
							</div>
						</div>
					</div>
				</CardHeader>
				<CardContent class="space-y-3">
					<Button
						variant="outline"
						class="w-full justify-start gap-3 transition-all hover:border-primary/50 hover:bg-muted/50"
						onclick={() => window.open('https://discord.gg/6Z9yKTbsrP', '_blank')}
					>
						<MessageCircle class="h-4 w-4 text-[#5865F2]" />
						<span class="flex-1 text-left">Join Discord Server</span>
						<ChevronRight class="h-4 w-4 text-muted-foreground" />
					</Button>
					<Button
						variant="outline"
						class="w-full justify-start gap-3 transition-all hover:border-primary/50 hover:bg-muted/50"
						onclick={() => window.open('https://github.com/athNdev/mineserver/issues', '_blank')}
					>
						<Github class="h-4 w-4" />
						<span class="flex-1 text-left">Report an Issue</span>
						<ChevronRight class="h-4 w-4 text-muted-foreground" />
					</Button>
					<Button
						variant="outline"
						class="w-full justify-start gap-3 transition-all hover:border-primary/50 hover:bg-muted/50"
						onclick={() => window.open('https://github.com/athNdev/mineserver', '_blank')}
					>
						<BookOpen class="h-4 w-4 text-green-500" />
						<span class="flex-1 text-left">Documentation</span>
						<ChevronRight class="h-4 w-4 text-muted-foreground" />
					</Button>
				</CardContent>
			</Card>

			<Card
				class="animate-in border-border/50 transition-all duration-500 fade-in-50 slide-in-from-bottom-5 hover:border-primary/30 hover:shadow-lg"
				style="animation-delay: 400ms"
			>
				<CardHeader>
					<div class="flex items-center gap-3">
						<div
							class="flex h-10 w-10 items-center justify-center rounded-xl bg-linear-to-br from-green-500/20 to-green-600/10"
						>
							<Shield class="h-5 w-5 text-green-500" />
						</div>
						<div>
							<CardTitle class="text-base">System Health</CardTitle>
							<CardDescription class="text-xs">Overall infrastructure status</CardDescription>
						</div>
					</div>
				</CardHeader>
				<CardContent>
					<div class="space-y-3">
						<div class="flex items-center justify-between">
							<span class="text-sm text-muted-foreground">Services</span>
							<div class="flex items-center gap-1">
								<CheckCircle class="h-4 w-4 text-green-500" />
								<span class="text-sm font-medium">Operational</span>
							</div>
						</div>
						<div class="flex items-center justify-between">
							<span class="text-sm text-muted-foreground">Network</span>
							<div class="flex items-center gap-1">
								{#if dashboardServers.some((s) => s.status === ServerStatus.RUNNING)}
									<Wifi class="h-4 w-4 text-green-500" />
									<span class="text-sm font-medium">Connected</span>
								{:else}
									<WifiOff class="h-4 w-4 text-gray-400" />
									<span class="text-sm text-muted-foreground">Idle</span>
								{/if}
							</div>
						</div>
						<div class="flex items-center justify-between">
							<span class="text-sm text-muted-foreground">Storage</span>
							<div class="flex items-center gap-1">
								{#if stats.totalDiskUsage > 0}
									<Database class="h-4 w-4 text-blue-500" />
									<span class="text-sm font-medium"
										>{formatBytes(stats.totalDiskUsage)}{stats.totalDiskSize}</span
									>
								{:else}
									<Database class="h-4 w-4 text-gray-400" />
									<span class="text-sm text-muted-foreground">No data</span>
								{/if}
							</div>
						</div>
					</div>
				</CardContent>
			</Card>

			<Card
				class="animate-in border-border/50 transition-all duration-500 fade-in-50 slide-in-from-bottom-5 hover:border-primary/30 hover:shadow-lg"
				style="animation-delay: 450ms"
			>
				<CardHeader>
					<div class="flex items-center gap-3">
						<div
							class="flex h-10 w-10 items-center justify-center rounded-xl bg-linear-to-br from-purple-500/20 to-purple-600/10"
						>
							<TrendingUp class="h-5 w-5 text-purple-500" />
						</div>
						<div>
							<CardTitle class="text-base">Quick Stats</CardTitle>
							<CardDescription class="text-xs">Server performance metrics</CardDescription>
						</div>
					</div>
				</CardHeader>
				<CardContent>
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-1">
							<p class="text-xs text-muted-foreground">Uptime</p>
							<p class="text-xl font-bold">
								{stats.running > 0
									? `${((stats.running / Math.max(stats.total, 1)) * 100).toFixed(0)}%`
									: 'â€”'}
							</p>
						</div>
						<div class="space-y-1">
							<p class="text-xs text-muted-foreground">Load</p>
							<p class="text-xl font-bold {getCpuColor(stats.avgCpu)}">
								{stats.avgCpu > 0 ? `${stats.avgCpu.toFixed(0)}%` : 'â€”'}
							</p>
						</div>
						<div class="space-y-1">
							<p class="text-xs text-muted-foreground">Avg TPS</p>
							<p class="text-xl font-bold {getTpsColor(stats.avgTps)}">
								{stats.avgTps > 0 ? stats.avgTps.toFixed(1) : 'â€”'}
							</p>
						</div>
						<div class="space-y-1">
							<p class="text-xs text-muted-foreground">Players</p>
							<p class="text-xl font-bold text-green-500">
								{stats.totalPlayers}
							</p>
						</div>
					</div>
				</CardContent>
			</Card>
		</div>
	</div>
{/if}
