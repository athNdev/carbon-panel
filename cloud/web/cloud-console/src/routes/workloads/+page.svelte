<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { goto } from '$app/navigation';
	import DataTable from '$lib/components/DataTable.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import Button from '$lib/components/Button.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import Field from '$lib/components/Field.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { pushToast } from '$lib/components/toast.svelte';
	import { call, workloadClient, nodeClient } from '$lib/api/client';
	import {
		ListWorkloadsRequestSchema,
		CreateWorkloadRequestSchema,
		StartWorkloadRequestSchema
	} from '$lib/proto/cloud/v1/workload_pb';
	import { ListNodesRequestSchema } from '$lib/proto/cloud/v1/node_pb';
	import { createLoadable } from '$lib/state.svelte';
	import { workloadStatusMeta, fmtCpu, fmtRam } from '$lib/utils';

	type Row = {
		id: string;
		name: string;
		node: string;
		status: number;
		hostname: string;
		cpu: string;
		ram: string;
	};

	type AvailableNode = {
		id: string;
		name: string;
		status: number;
	};

	async function load(): Promise<{ workloads: Row[]; nodes: AvailableNode[] }> {
		const [wRes, nRes] = await Promise.all([
			call(() => workloadClient.listWorkloads(create(ListWorkloadsRequestSchema, {}))),
			call(() => nodeClient.listNodes(create(ListNodesRequestSchema, {})))
		]);
		return {
			workloads: (wRes.workloads ?? []).map((w) => ({
				id: w.id,
				name: w.name,
				node: w.nodeId || '—',
				status: w.status,
				hostname: w.hostname || '—',
				cpu: fmtCpu(w.spec?.cpuMillicores),
				ram: fmtRam(w.spec?.memoryMb)
			})),
			nodes: (nRes.nodes ?? []).map((n) => ({
				id: n.id,
				name: n.name,
				status: n.status
			}))
		};
	}

	const pageState = createLoadable(load);

	// Modal State
	let deployOpen = $state(false);
	let deploySubmitting = $state(false);
	let deployError = $state('');

	// Form fields
	let serverName = $state('');
	let loader = $state('paper');
	let minecraftVersion = $state('1.21.4');
	let selectedNodeId = $state('');
	let memoryMb = $state(2048);
	let cpuMillicores = $state(1000);
	let hostname = $state('');

	function openDeployModal() {
		deployError = '';
		serverName = '';
		loader = 'paper';
		minecraftVersion = '1.21.4';
		memoryMb = 2048;
		cpuMillicores = 1000;
		hostname = '';

		const activeNodes = (pageState.data?.nodes ?? []).filter((n) => n.status === 1 /* active */);
		if (activeNodes.length > 0) {
			selectedNodeId = activeNodes[0].id;
		} else if ((pageState.data?.nodes ?? []).length > 0) {
			selectedNodeId = pageState.data!.nodes[0].id;
		} else {
			selectedNodeId = '';
		}

		deployOpen = true;
	}

	async function handleDeploy(e: SubmitEvent) {
		e.preventDefault();
		if (!serverName.trim()) {
			deployError = 'Please provide a server name.';
			return;
		}
		if (!selectedNodeId) {
			deployError = 'Please select a node to host this server.';
			return;
		}

		deploySubmitting = true;
		deployError = '';

		try {
			const res = await call(() =>
				workloadClient.createWorkload(
					create(CreateWorkloadRequestSchema, {
						name: serverName.trim(),
						nodeId: selectedNodeId,
						spec: {
							loader,
							minecraftVersion: minecraftVersion.trim() || '1.21.4',
							memoryMb: BigInt(memoryMb),
							cpuMillicores: BigInt(cpuMillicores),
							hostname: hostname.trim() || undefined
						}
					})
				)
			);

			if (res.workload?.id) {
				try {
					await call(() =>
						workloadClient.startWorkload(
							create(StartWorkloadRequestSchema, { id: res.workload!.id })
						)
					);
				} catch {
					// Even if autostart was deferred, workload was created
				}

				pushToast(`Server "${serverName}" created successfully!`, 'success');
				deployOpen = false;
				await goto(`/workloads/${res.workload.id}`);
			} else {
				await pageState.reload();
				deployOpen = false;
			}
		} catch (err) {
			deployError = err instanceof Error ? err.message : String(err);
		} finally {
			deploySubmitting = false;
		}
	}
</script>

<svelte:head><title>Workloads · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	<div class="flex items-center justify-between gap-3">
		<div>
			<h1 class="text-base font-semibold text-foreground">Workloads</h1>
			<p class="text-xs text-muted-foreground">
				Minecraft game servers scheduled and running across your cluster nodes.
			</p>
		</div>
		<Button onclick={openDeployModal}>Deploy server</Button>
	</div>

	{#if pageState.loading}
		<div class="h-40 animate-pulse border border-border bg-card"></div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load workloads</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else if (pageState.data?.workloads ?? []).length === 0}
		<EmptyState
			title="No Minecraft servers deployed"
			body="Workloads are Minecraft servers scheduled onto your cluster nodes."
		>
			{#snippet action()}
				<Button onclick={openDeployModal}>Deploy server</Button>
			{/snippet}
		</EmptyState>
	{:else}
		<DataTable
			label="Workloads"
			columns={[
				{ id: 'name', label: 'Name', sortable: true },
				{ id: 'node', label: 'Node' },
				{ id: 'status', label: 'Status', sortable: true },
				{ id: 'hostname', label: 'Hostname' },
				{ id: 'cpu', label: 'CPU' },
				{ id: 'ram', label: 'RAM' }
			]}
			rows={pageState.data?.workloads ?? []}
			rowHref={(row) => `/workloads/${row.id}`}
			keyFor={(row) => row.id}
			emptyTitle="No workloads"
			emptyBody="No workloads match the current view."
		>
			{#snippet cell(row, col)}
				{#if col.id === 'name'}
					<span class="font-medium text-foreground">{row.name}</span>
				{:else if col.id === 'status'}
					{@const meta = workloadStatusMeta(row.status as number)}
					<Tag tone={meta.tone}>{meta.label}</Tag>
				{:else if col.id === 'node'}
					<span class="font-mono text-xs text-muted-foreground">{row.node}</span>
				{:else if col.id === 'cpu' || col.id === 'ram'}
					<span class="font-mono text-xs text-muted-foreground">{row[col.id]}</span>
				{:else}
					{String(row[col.id] ?? '—')}
				{/if}
			{/snippet}
		</DataTable>
	{/if}
</div>

<!-- Deploy Server Modal -->
<Modal open={deployOpen} title="Deploy Minecraft Server" onclose={() => (deployOpen = false)}>
	<form onsubmit={handleDeploy} class="flex flex-col gap-4">
		{#if deployError}
			<div class="border border-destructive/50 bg-destructive/10 p-3 text-xs text-destructive">
				{deployError}
			</div>
		{/if}

		<Field id="server-name" label="Server Name" required>
			{#snippet control({ id, describedBy })}
				<input
					{id}
					aria-describedby={describedBy}
					type="text"
					bind:value={serverName}
					placeholder="e.g. survival-01"
					class="w-full border border-border bg-input px-3 py-2 text-sm text-foreground focus-ring"
					required
				/>
			{/snippet}
		</Field>

		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			<Field id="server-loader" label="Server Software (Loader)">
				{#snippet control({ id, describedBy })}
					<select
						{id}
						aria-describedby={describedBy}
						bind:value={loader}
						class="w-full border border-border bg-input px-3 py-2 text-sm text-foreground focus-ring"
					>
						<option value="paper">Paper (High Performance)</option>
						<option value="purpur">Purpur</option>
						<option value="fabric">Fabric</option>
						<option value="vanilla">Vanilla</option>
						<option value="velocity">Velocity (Proxy)</option>
					</select>
				{/snippet}
			</Field>

			<Field id="server-version" label="Minecraft Version">
				{#snippet control({ id, describedBy })}
					<input
						{id}
						aria-describedby={describedBy}
						type="text"
						bind:value={minecraftVersion}
						placeholder="1.21.4"
						class="w-full border border-border bg-input px-3 py-2 text-sm text-foreground focus-ring"
					/>
				{/snippet}
			</Field>
		</div>

		<Field id="target-node" label="Target Node" required hint="Cluster node that will run this Minecraft server container.">
			{#snippet control({ id, describedBy })}
				<select
					{id}
					aria-describedby={describedBy}
					bind:value={selectedNodeId}
					class="w-full border border-border bg-input px-3 py-2 text-sm text-foreground focus-ring"
					required
				>
					{#if (pageState.data?.nodes ?? []).length === 0}
						<option value="" disabled>No cluster nodes available</option>
					{:else}
						{#each pageState.data?.nodes ?? [] as node (node.id)}
							<option value={node.id}>
								{node.name} ({node.status === 1 ? 'Online' : 'Status: ' + node.status}) — {node.id.slice(0, 8)}
							</option>
						{/each}
					{/if}
				</select>
			{/snippet}
		</Field>

		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			<Field id="memory-mb" label="RAM Allocation (MB)">
				{#snippet control({ id, describedBy })}
					<input
						{id}
						aria-describedby={describedBy}
						type="number"
						bind:value={memoryMb}
						min="512"
						step="512"
						class="w-full border border-border bg-input px-3 py-2 text-sm text-foreground focus-ring"
					/>
				{/snippet}
			</Field>

			<Field id="cpu-cores" label="CPU Limit">
				{#snippet control({ id, describedBy })}
					<select
						{id}
						aria-describedby={describedBy}
						bind:value={cpuMillicores}
						class="w-full border border-border bg-input px-3 py-2 text-sm text-foreground focus-ring"
					>
						<option value={1000}>1 Core (1000 millicores)</option>
						<option value={2000}>2 Cores (2000 millicores)</option>
						<option value={4000}>4 Cores (4000 millicores)</option>
						<option value={8000}>8 Cores (8000 millicores)</option>
					</select>
				{/snippet}
			</Field>
		</div>

		<Field id="server-hostname" label="Public Hostname (Optional)" hint="Domain or subdomain if using reverse proxy routing.">
			{#snippet control({ id, describedBy })}
				<input
					{id}
					aria-describedby={describedBy}
					type="text"
					bind:value={hostname}
					placeholder="e.g. play.carbon.local"
					class="w-full border border-border bg-input px-3 py-2 text-sm text-foreground focus-ring"
				/>
			{/snippet}
		</Field>

		<div class="mt-4 flex justify-end gap-2 border-t border-border pt-4">
			<Button variant="secondary" onclick={() => (deployOpen = false)} type="button">Cancel</Button>
			<Button type="submit" disabled={deploySubmitting || (pageState.data?.nodes ?? []).length === 0}>
				{deploySubmitting ? 'Deploying...' : 'Deploy Server'}
			</Button>
		</div>
	</form>
</Modal>
