<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import DataTable from '$lib/components/DataTable.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import Button from '$lib/components/Button.svelte';
	import { call, nodeTypeClient } from '$lib/api/client';
	import { ListNodeTypesRequestSchema } from '$lib/proto/cloud/v1/nodetype_pb';
	import { createLoadable } from '$lib/state.svelte';
	import { fmtRam, fmtGb, fmtUsd, providerLabel } from '$lib/utils';

	type Row = {
		id: string;
		name: string;
		vcpu: number;
		ram: string;
		disk: string;
		price: string;
		enabled: boolean;
		instanceTypes: string;
		description: string;
	};

	async function load(): Promise<Row[]> {
		const res = await call(() =>
			nodeTypeClient.listNodeTypes(create(ListNodeTypesRequestSchema, { includeDisabled: true }))
		);
		return (res.nodeTypes ?? []).map((t) => ({
			id: t.id,
			name: t.name,
			vcpu: t.vcpu,
			ram: fmtRam(t.ramMb),
			disk: fmtGb(t.diskGb),
			price: fmtUsd(t.monthlyPriceUsd),
			enabled: t.enabled,
			instanceTypes: (t.instanceTypes ?? [])
				.map((it) => `${providerLabel(it.provider)}/${it.instanceType}`)
				.join(', '),
			description: t.description
		}));
	}

	const pageState = createLoadable(load);
</script>

<svelte:head><title>Node types · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if pageState.loading}
		<div class="h-40 animate-pulse border border-border bg-card"></div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load node types</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else}
		<DataTable
			label="Node types"
			columns={[
				{ id: 'name', label: 'Name', sortable: true },
				{ id: 'vcpu', label: 'vCPU', sortable: true, align: 'right' },
				{ id: 'ram', label: 'RAM', align: 'right' },
				{ id: 'disk', label: 'Disk', align: 'right' },
				{ id: 'price', label: 'Monthly', sortable: true, align: 'right' },
				{ id: 'state', label: 'State' },
				{ id: 'instanceTypes', label: 'Provider instance types' },
				{ id: 'description', label: 'Description' }
			]}
			rows={pageState.data ?? []}
			keyFor={(row) => row.id}
			emptyTitle="No node types"
			emptyBody="The catalog is empty."
		>
			{#snippet cell(row, col)}
				{#if col.id === 'name'}
					<span class="font-medium text-foreground">{row.name}</span>
				{:else if col.id === 'state'}
					<Tag tone={row.enabled ? 'green' : 'neutral'}>{row.enabled ? 'Enabled' : 'Disabled'}</Tag>
				{:else if col.id === 'instanceTypes'}
					<span class="font-mono text-xs text-muted-foreground">{row.instanceTypes || '—'}</span>
				{:else if col.id === 'description'}
					<span class="text-xs text-muted-foreground">{row.description || '—'}</span>
				{:else}
					{String(row[col.id] ?? '—')}
				{/if}
			{/snippet}
		</DataTable>
	{/if}
</div>