<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import DataTable from '$lib/components/DataTable.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import Button from '$lib/components/Button.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { call, nodeClient } from '$lib/api/client';
	import { ListNodesRequestSchema } from '$lib/proto/cloud/v1/node_pb';
	import { createLoadable } from '$lib/state.svelte';
	import { nodeStatusMeta, originLabel, fmtRelative, tsToDate, cls } from '$lib/utils';

	type Row = {
		id: string;
		name: string;
		origin: string;
		nodeType: string;
		region: string;
		status: number;
		cpuPct: number | null;
		ramPct: number | null;
		heartbeat: string;
	};

	async function load(): Promise<Row[]> {
		const res = await call(() => nodeClient.listNodes(create(ListNodesRequestSchema, {})));
		return (res.nodes ?? []).map((n) => {
			const capacityCpu = Number(n.capacity?.vcpu ?? 0) * 1000;
			const allocatedCpu = Number(n.allocation?.cpuMillicores ?? 0);
			const capacityRam = Number(n.capacity?.ramMb ?? 0);
			const allocatedRam = Number(n.allocation?.ramMb ?? 0);
			return {
				id: n.id,
				name: n.name,
				origin: originLabel(n.origin),
				nodeType: n.nodeTypeId || '—',
				region: n.region || '—',
				status: n.status,
				cpuPct: capacityCpu > 0 ? Math.round((allocatedCpu / capacityCpu) * 100) : null,
				ramPct: capacityRam > 0 ? Math.round((allocatedRam / capacityRam) * 100) : null,
				heartbeat: fmtRelative(tsToDate(n.lastHeartbeat))
			};
		});
	}

	const pageState = createLoadable(load);

	function pctClass(pct: number | null): string {
		if (pct === null) return '';
		if (pct >= 90) return 'bg-destructive';
		if (pct >= 70) return 'bg-warning';
		return '';
	}
</script>

<svelte:head><title>Nodes · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	<div class="flex items-center justify-between gap-3">
		<p class="text-sm text-muted-foreground">
			Machines available to run workloads for this organization.
		</p>
		<div class="flex gap-2">
			<Button href="/nodes/join">Join a node</Button>
			<Button variant="secondary" href="/nodes/provision">Provision</Button>
		</div>
	</div>

	{#if pageState.loading}
		<div class="h-40 animate-pulse border border-border bg-card"></div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load nodes</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else if (pageState.data ?? []).length === 0}
		<EmptyState
			title="No nodes yet"
			body="Bring your own machine with a join command, or provision a managed node."
		>
			{#snippet action()}
				<div class="flex gap-2">
					<Button href="/nodes/join">Join a node</Button>
					<Button variant="secondary" href="/nodes/provision">Provision a node</Button>
				</div>
			{/snippet}
		</EmptyState>
	{:else}
		<DataTable
			label="Nodes"
			columns={[
				{ id: 'name', label: 'Name', sortable: true },
				{ id: 'origin', label: 'Origin', sortable: true },
				{ id: 'nodeType', label: 'Type' },
				{ id: 'region', label: 'Region' },
				{ id: 'status', label: 'Status', sortable: true },
				{ id: 'cpu', label: 'CPU' },
				{ id: 'ram', label: 'RAM' },
				{ id: 'heartbeat', label: 'Last heartbeat', sortable: true }
			]}
			rows={pageState.data ?? []}
			rowHref={(row) => `/nodes/${row.id}`}
			keyFor={(row) => row.id}
			emptyTitle="No nodes"
			emptyBody="Join or provision a node to get started."
		>
			{#snippet cell(row, col)}
				{#if col.id === 'name'}
					<span class="font-medium text-foreground">{row.name}</span>
				{:else if col.id === 'status'}
					{@const meta = nodeStatusMeta(row.status as number)}
					<Tag tone={meta.tone}>{meta.label}</Tag>
				{:else if col.id === 'cpu' || col.id === 'ram'}
					{#if row[col.id] === null}
						<span class="text-xs text-muted-foreground">—</span>
					{:else}
						<div class="w-28">
							<div class="bar">
								<div
									class={cls('bar-fill', pctClass(row[col.id] as number | null))}
									style={`width:${row[col.id]}%`}
								></div>
							</div>
							<span class="font-mono text-[10px] text-muted-foreground">{row[col.id]}%</span>
						</div>
					{/if}
				{:else if col.id === 'heartbeat'}
					<span class="text-xs text-muted-foreground">{row.heartbeat}</span>
				{:else}
					{String(row[col.id] ?? '—')}
				{/if}
			{/snippet}
		</DataTable>
	{/if}
</div>