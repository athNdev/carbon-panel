<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import Tile from '$lib/components/Tile.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import DataTable from '$lib/components/DataTable.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import Button from '$lib/components/Button.svelte';
	import {
		call,
		systemClient,
		nodeClient,
		workloadClient,
		auditClient
	} from '$lib/api/client';
	import { GetCapabilitiesRequestSchema, GetBuildInfoRequestSchema } from '$lib/proto/cloud/v1/system_pb';
	import { ListNodesRequestSchema } from '$lib/proto/cloud/v1/node_pb';
	import { ListWorkloadsRequestSchema } from '$lib/proto/cloud/v1/workload_pb';
	import { ListAuditEventsRequestSchema } from '$lib/proto/cloud/v1/audit_pb';
	import { createLoadable } from '$lib/state.svelte';
	import {
		nodeStatusMeta,
		workloadStatusMeta,
		fmtRelative,
		tsToDate
	} from '$lib/utils';

	type OverviewData = {
		capabilities: Array<{ id: string; enabled: boolean; missingKeys: string[]; detail: string }>;
		build: { version: string; commit: string; buildTime: string; goVersion: string } | null;
		nodes: Array<{
			id: string;
			name: string;
			status: number;
		}>;
		workloads: Array<{
			id: string;
			name: string;
			status: number;
		}>;
		audit: Array<{
			id: string;
			action: string;
			resourceType: string;
			result: string;
			createdAt: Date | null;
		}>;
	};

	async function load(): Promise<OverviewData> {
		const [caps, build, nodes, workloads, audit] = await Promise.all([
			call(() => systemClient.getCapabilities(create(GetCapabilitiesRequestSchema, {}))),
			call(() => systemClient.getBuildInfo(create(GetBuildInfoRequestSchema, {}))),
			call(() => nodeClient.listNodes(create(ListNodesRequestSchema, {}))),
			call(() => workloadClient.listWorkloads(create(ListWorkloadsRequestSchema, {}))),
			call(() => auditClient.listAuditEvents(create(ListAuditEventsRequestSchema, {})))
		]);
		return {
			capabilities: (caps.capabilities ?? []).map((c) => ({
				id: c.id,
				enabled: c.enabled,
				missingKeys: [...c.missingKeys],
				detail: c.detail
			})),
			build: build.build
				? {
						version: build.build.version,
						commit: build.build.commit,
						buildTime: build.build.buildTime,
						goVersion: build.build.goVersion
					}
				: null,
			nodes: (nodes.nodes ?? []).map((n) => ({ id: n.id, name: n.name, status: n.status })),
			workloads: (workloads.workloads ?? []).map((w) => ({
				id: w.id,
				name: w.name,
				status: w.status
			})),
			audit: (audit.events ?? []).slice(0, 8).map((e) => ({
				id: e.id,
				action: e.action,
				resourceType: e.resourceType,
				result: e.result,
				createdAt: tsToDate(e.createdAt)
			}))
		};
	}

	const pageState = createLoadable(load);

	const nodeCounts = $derived.by(() => {
		const out: Record<string, number> = {};
		for (const n of pageState.data?.nodes ?? []) {
			const meta = nodeStatusMeta(n.status);
			out[meta.label] = (out[meta.label] ?? 0) + 1;
		}
		return out;
	});

	const workloadCounts = $derived.by(() => {
		const out: Record<string, number> = {};
		for (const w of pageState.data?.workloads ?? []) {
			const meta = workloadStatusMeta(w.status);
			out[meta.label] = (out[meta.label] ?? 0) + 1;
		}
		return out;
	});
</script>

<svelte:head><title>Overview · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if pageState.loading}
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
			{#each Array(4) as _, i (i)}
				<div class="h-24 animate-pulse border border-border bg-card"></div>
			{/each}
		</div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm text-foreground">
			<p class="font-medium text-destructive">Could not load overview</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else}
		{#if pageState.data}
			{#if pageState.data.capabilities.some((c) => !c.enabled)}
				<div class="flex flex-col gap-2">
					{#each pageState.data.capabilities.filter((c) => !c.enabled) as cap (cap.id)}
						<div
							class="flex flex-wrap items-center gap-2 border border-warning/50 bg-card px-3 py-2 text-sm"
							role="status"
						>
							<span class="font-medium text-foreground">Capability “{cap.id}” is disabled.</span>
							{#if cap.missingKeys.length > 0}
								<span class="text-muted-foreground">
									Missing env keys:
									{#each cap.missingKeys as k, i (k)}
										<code class="font-mono text-xs text-warning">{k}</code
										>{i < cap.missingKeys.length - 1 ? ', ' : ''}
									{/each}
								</span>
							{/if}
							{#if cap.detail}
								<span class="w-full text-xs text-muted-foreground">{cap.detail}</span>
							{/if}
						</div>
					{/each}
				</div>
			{/if}

			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
				<Tile title="Nodes">
					<div class="flex flex-wrap gap-1.5">
						{#if (pageState.data.nodes ?? []).length === 0}
							<span class="text-xs text-muted-foreground">No nodes</span>
						{/if}
						{#each Object.entries(nodeCounts) as [label, count] (label)}
							<Tag tone={label === 'Online' ? 'green' : label === 'Offline' || label === 'Error' ? 'red' : label === 'Draining' ? 'blue' : label === 'Pending' ? 'yellow' : 'neutral'}>
								{label}: {count}
							</Tag>
						{/each}
					</div>
				</Tile>
				<Tile title="Workloads">
					<div class="flex flex-wrap gap-1.5">
						{#if (pageState.data.workloads ?? []).length === 0}
							<span class="text-xs text-muted-foreground">No workloads</span>
						{/if}
						{#each Object.entries(workloadCounts) as [label, count] (label)}
							<Tag tone={label === 'Running' ? 'green' : label === 'Error' ? 'red' : label === 'Pending' ? 'yellow' : 'neutral'}>
								{label}: {count}
							</Tag>
						{/each}
					</div>
				</Tile>
				<Tile title="Build">
					{#if pageState.data.build}
						<p class="font-mono text-xs text-foreground">v{pageState.data.build.version}</p>
						<p class="mt-1 font-mono text-xs text-muted-foreground">
							{pageState.data.build.commit ? pageState.data.build.commit.slice(0, 8) : 'no commit'}
						</p>
						<p class="mt-1 text-xs text-muted-foreground">
							{pageState.data.build.buildTime || '—'}
						</p>
					{:else}
						<p class="text-xs text-muted-foreground">Build info unavailable</p>
					{/if}
				</Tile>
				<Tile title="API">
					<a class="text-xs text-primary hover:underline focus-ring" href="/settings">
						Go to settings
					</a>
				</Tile>
			</div>

			<Tile title="Recent audit activity">
				{#if (pageState.data.audit ?? []).length === 0}
					<EmptyState title="No audit events" body="Mutating actions will appear here." />
				{:else}
					<DataTable
						label="Recent audit events"
						columns={[
							{ id: 'when', label: 'When', sortable: true },
							{ id: 'action', label: 'Action', sortable: true },
							{ id: 'resource', label: 'Resource' },
							{ id: 'result', label: 'Result' }
						]}
						rows={(pageState.data.audit ?? []).map((e) => ({
							when: e.createdAt ? fmtRelative(e.createdAt) : '—',
							action: e.action,
							resource: e.resourceType || '—',
							result: e.result
						}))}
					>
						{#snippet cell(row, col)}
							{#if col.id === 'result'}
								<Tag
									tone={row.result === 'success' ? 'green' : row.result === 'denied' ? 'yellow' : 'red'}
								>
									{row.result || '—'}
								</Tag>
							{:else if col.id === 'when' && row.when}
								<span class="text-muted-foreground">{row.when}</span>
							{:else}
								{String(row[col.id] ?? '—')}
							{/if}
						{/snippet}
					</DataTable>
					<div class="mt-3">
						<Button variant="ghost" href="/audit">View full audit log →</Button>
					</div>
				{/if}
			</Tile>
		{/if}
	{/if}
</div>