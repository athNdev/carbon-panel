<script lang="ts">
	import {
		CarbonButton,
		CarbonDataTable,
		CarbonTag,
		CarbonInlineLoading
	} from '$lib/components/carbon';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { Module } from '$lib/proto/carbonpanel/v1/module_pb';
	import { ModuleStatus } from '$lib/proto/carbonpanel/v1/module_pb';
	import {
		Loader2,
		Play,
		Square,
		RotateCw,
		Settings,
		Trash2,
		Terminal,
		Cpu,
		Server,
		Package,
		RefreshCw
	} from '@lucide/svelte';
	import ModuleDialog from '$lib/components/server/ModuleDialog.svelte';
	import ModuleLogsDialog from '$lib/components/server/ModuleLogsDialog.svelte';
	import { onMount, onDestroy } from 'svelte';

	let modules = $state<Module[]>([]);
	let loading = $state(true);
	let actionLoading = $state<string | null>(null);

	// Dialog state
	let editDialogOpen = $state(false);
	let logsDialogOpen = $state(false);
	let selectedModule = $state<Module | null>(null);

	let pollingInterval: ReturnType<typeof setInterval> | null = null;

	onMount(() => {
		loadModules();
		pollingInterval = setInterval(() => loadModules(true), 5000);
	});

	onDestroy(() => {
		if (pollingInterval) {
			clearInterval(pollingInterval);
		}
	});

	async function loadModules(silent = false) {
		try {
			if (!silent) loading = true;
			const response = await rpcClient.module.listModules(
				{},
				silent ? silentCallOptions : undefined
			);
			modules = response.modules;
		} catch {
			if (!silent) toast.error('Failed to load modules');
		} finally {
			if (!silent) loading = false;
		}
	}

	async function handleStartModule(module: Module) {
		actionLoading = module.id;
		try {
			await rpcClient.module.startModule({ id: module.id });
			toast.success(`Starting ${module.name}...`);
			await loadModules(true);
		} catch (error) {
			toast.error(
				`Failed to start module: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = null;
		}
	}

	async function handleStopModule(module: Module) {
		actionLoading = module.id;
		try {
			await rpcClient.module.stopModule({ id: module.id });
			toast.success(`Stopping ${module.name}...`);
			await loadModules(true);
		} catch (error) {
			toast.error(
				`Failed to stop module: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = null;
		}
	}

	async function handleRestartModule(module: Module) {
		actionLoading = module.id;
		try {
			await rpcClient.module.restartModule({ id: module.id });
			toast.success(`Restarting ${module.name}...`);
			await loadModules(true);
		} catch (error) {
			toast.error(
				`Failed to restart module: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = null;
		}
	}

	async function handleDeleteModule(module: Module) {
		const confirmed = confirm(
			`Are you sure you want to delete "${module.name}"?\n\nThis will stop and remove the container and all module data.`
		);
		if (!confirmed) return;

		actionLoading = module.id;
		try {
			await rpcClient.module.deleteModule({ id: module.id });
			toast.success(`Module "${module.name}" deleted`);
			await loadModules(true);
		} catch (error) {
			toast.error(
				`Failed to delete module: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		} finally {
			actionLoading = null;
		}
	}

	function openEditDialog(module: Module) {
		selectedModule = module;
		editDialogOpen = true;
	}

	function openLogsDialog(module: Module) {
		selectedModule = module;
		logsDialogOpen = true;
	}

	function getStatusTagType(status: ModuleStatus): 'green' | 'blue' | 'red' | 'gray' {
		switch (status) {
			case ModuleStatus.RUNNING:
				return 'green';
			case ModuleStatus.STARTING:
			case ModuleStatus.STOPPING:
			case ModuleStatus.CREATING:
				return 'blue';
			case ModuleStatus.ERROR:
				return 'red';
			default:
				return 'gray';
		}
	}

	function getStatusLabel(status: ModuleStatus): string {
		switch (status) {
			case ModuleStatus.RUNNING:
				return 'Running';
			case ModuleStatus.STOPPED:
				return 'Stopped';
			case ModuleStatus.STARTING:
				return 'Starting';
			case ModuleStatus.STOPPING:
				return 'Stopping';
			case ModuleStatus.ERROR:
				return 'Error';
			case ModuleStatus.CREATING:
				return 'Creating';
			default:
				return 'Unknown';
		}
	}
</script>

<div class="space-y-4 rounded-none font-sans text-[#f4f4f4]">
	{#snippet activeToolbar()}
		<CarbonButton
			kind="tertiary"
			size="sm"
			class="rounded-none"
			onclick={() => loadModules()}
			disabled={loading}
			title="Refresh active modules"
		>
			<RefreshCw class={`mr-1.5 h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
			Refresh
		</CarbonButton>
	{/snippet}

	{#snippet activeHeader()}
		<tr>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase"
				>Module / Server</th
			>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase">Template</th>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase">Status</th>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase">Memory</th>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase">CPU</th>
			<th scope="col" class="px-4 py-3 text-xs font-semibold tracking-wider uppercase"
				>Auto-Start</th
			>
			<th scope="col" class="px-4 py-3 text-right text-xs font-semibold tracking-wider uppercase"
				>Actions</th
			>
		</tr>
	{/snippet}

	<CarbonDataTable
		title="Active Instances"
		description="Sidecar modules and containerized services running across all servers"
		toolbar={activeToolbar}
		header={activeHeader}
		class="rounded-none"
	>
		{#if loading && modules.length === 0}
			<tr>
				<td colspan="7" class="py-16 text-center">
					<div class="flex items-center justify-center">
						<CarbonInlineLoading description="Loading active modules..." />
					</div>
				</td>
			</tr>
		{:else if modules.length === 0}
			<tr>
				<td colspan="7" class="py-16 text-center text-[#8d8d8d]">
					<Package class="mx-auto mb-3 h-10 w-10 text-[#525252]" />
					<p class="text-sm font-semibold text-white">No active module instances</p>
					<p class="mt-1 text-xs text-[#8d8d8d]">
						Attach modules to a Minecraft server to provision sidecar containers.
					</p>
				</td>
			</tr>
		{:else}
			{#each modules as module (module.id)}
				{@const isLoading = actionLoading === module.id}
				<tr class="transition-colors hover:bg-[#353535]">
					<td class="px-4 py-3">
						<div class="space-y-0.5">
							<div class="flex items-center gap-2 text-sm font-semibold text-white">
								<span>{module.name}</span>
							</div>
							<div class="flex items-center gap-1.5 font-mono text-xs text-[#a8a8a8]">
								<Server class="h-3 w-3 text-[#0f62fe]" />
								<span>{module.serverName || module.serverId}</span>
							</div>
						</div>
					</td>

					<td class="px-4 py-3 font-mono text-xs text-[#c6c6c6]">
						{module.templateName}
					</td>

					<td class="px-4 py-3">
						<CarbonTag type={getStatusTagType(module.status)} size="sm">
							{getStatusLabel(module.status)}
						</CarbonTag>
					</td>

					<td class="px-4 py-3 font-mono text-xs text-[#a8a8a8]">
						{#if module.status === ModuleStatus.RUNNING && module.memoryUsage > 0}
							{module.memoryUsage.toFixed(0)} MB
						{:else}
							-
						{/if}
					</td>

					<td class="px-4 py-3 font-mono text-xs text-[#a8a8a8]">
						{#if module.status === ModuleStatus.RUNNING}
							{module.cpuPercent.toFixed(1)}%
						{:else}
							-
						{/if}
					</td>

					<td class="px-4 py-3">
						{#if module.autoStart}
							<CarbonTag type="blue" size="sm">Enabled</CarbonTag>
						{:else}
							<span class="font-mono text-xs text-[#8d8d8d]">Disabled</span>
						{/if}
					</td>

					<td class="px-4 py-3 text-right">
						<div class="flex items-center justify-end gap-1">
							<!-- Start / Stop / Restart Control -->
							{#if module.status === ModuleStatus.STOPPED}
								<CarbonButton
									kind="ghost"
									size="sm"
									iconOnly
									class="rounded-none text-emerald-500 hover:bg-emerald-500/20"
									onclick={() => handleStartModule(module)}
									disabled={isLoading}
									title="Start module"
								>
									{#if isLoading}
										<Loader2 class="h-3.5 w-3.5 animate-spin" />
									{:else}
										<Play class="h-3.5 w-3.5" />
									{/if}
								</CarbonButton>
							{:else if module.status === ModuleStatus.RUNNING}
								<CarbonButton
									kind="ghost"
									size="sm"
									iconOnly
									class="rounded-none text-red-500 hover:bg-red-500/20"
									onclick={() => handleStopModule(module)}
									disabled={isLoading}
									title="Stop module"
								>
									{#if isLoading}
										<Loader2 class="h-3.5 w-3.5 animate-spin" />
									{:else}
										<Square class="h-3.5 w-3.5" />
									{/if}
								</CarbonButton>
								<CarbonButton
									kind="ghost"
									size="sm"
									iconOnly
									class="rounded-none text-[#c6c6c6] hover:text-white"
									onclick={() => handleRestartModule(module)}
									disabled={isLoading}
									title="Restart module"
								>
									<RotateCw class="h-3.5 w-3.5" />
								</CarbonButton>
							{:else if module.status === ModuleStatus.STARTING || module.status === ModuleStatus.STOPPING || module.status === ModuleStatus.CREATING}
								<CarbonButton kind="ghost" size="sm" iconOnly disabled class="rounded-none">
									<Loader2 class="h-3.5 w-3.5 animate-spin text-[#0f62fe]" />
								</CarbonButton>
							{:else if module.status === ModuleStatus.ERROR}
								<CarbonButton
									kind="ghost"
									size="sm"
									iconOnly
									class="rounded-none text-emerald-500 hover:bg-emerald-500/20"
									onclick={() => handleStartModule(module)}
									disabled={isLoading}
									title="Retry start"
								>
									{#if isLoading}
										<Loader2 class="h-3.5 w-3.5 animate-spin" />
									{:else}
										<Play class="h-3.5 w-3.5" />
									{/if}
								</CarbonButton>
							{/if}

							<!-- Logs -->
							<CarbonButton
								kind="ghost"
								size="sm"
								iconOnly
								class="rounded-none text-[#c6c6c6] hover:text-white"
								onclick={() => openLogsDialog(module)}
								title="View logs"
							>
								<Terminal class="h-3.5 w-3.5" />
							</CarbonButton>

							<!-- Edit -->
							<CarbonButton
								kind="ghost"
								size="sm"
								iconOnly
								class="rounded-none text-[#c6c6c6] hover:text-white"
								onclick={() => openEditDialog(module)}
								title="Edit module"
							>
								<Settings class="h-3.5 w-3.5" />
							</CarbonButton>

							<!-- Delete -->
							<CarbonButton
								kind="ghost"
								size="sm"
								iconOnly
								class="rounded-none text-[#da1e28] hover:bg-[#da1e28]/20"
								onclick={() => handleDeleteModule(module)}
								disabled={isLoading}
								title="Delete module"
							>
								<Trash2 class="h-3.5 w-3.5" />
							</CarbonButton>
						</div>
					</td>
				</tr>
			{/each}
		{/if}
	</CarbonDataTable>
</div>

{#if selectedModule}
	<ModuleDialog
		bind:open={editDialogOpen}
		mode="edit"
		module={selectedModule}
		onSuccess={() => loadModules(true)}
	/>

	<ModuleLogsDialog bind:open={logsDialogOpen} module={selectedModule} />
{/if}
