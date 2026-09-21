<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import DataTable from '$lib/components/DataTable.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import Button from '$lib/components/Button.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { call, workloadClient } from '$lib/api/client';
	import { ListWorkloadsRequestSchema } from '$lib/proto/cloud/v1/workload_pb';
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

	async function load(): Promise<Row[]> {
		const res = await call(() => workloadClient.listWorkloads(create(ListWorkloadsRequestSchema, {})));
		return (res.workloads ?? []).map((w) => ({
			id: w.id,
			name: w.name,
			node: w.nodeId || '—',
			status: w.status,
			hostname: w.hostname || '—',
			cpu: fmtCpu(w.spec?.cpuMillicores),
			ram: fmtRam(w.spec?.memoryMb)
		}));
	}

	const pageState = createLoadable(load);
</script>

<svelte:head><title>Workloads · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
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
	{:else if (pageState.data ?? []).length === 0}
		<EmptyState
			title="No workloads"
			body="Workloads are Minecraft servers scheduled onto your nodes."
		/>
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
			rows={pageState.data ?? []}
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