<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import Tile from '$lib/components/Tile.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import DataTable from '$lib/components/DataTable.svelte';
	import Button from '$lib/components/Button.svelte';
	import Field from '$lib/components/Field.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { call, auditClient } from '$lib/api/client';
	import { can } from '$lib/auth/permissions';
	import { auth } from '$lib/auth/clerk.svelte';
	import { createLoadable } from '$lib/state.svelte';
	import { ListAuditEventsRequestSchema } from '$lib/proto/cloud/v1/audit_pb';
	import { fmtDateTime, tsToDate } from '$lib/utils';

	type Row = {
		id: string;
		when: string;
		action: string;
		resource: string;
		resourceId: string;
		actor: string;
		result: string;
		ip: string;
	};

	async function load(filters: {
		actionPrefix: string;
		resourceType: string;
		since: string;
		until: string;
	}): Promise<Row[]> {
		const res = await call(() =>
			auditClient.listAuditEvents(
				create(ListAuditEventsRequestSchema, {
					actionPrefix: filters.actionPrefix || undefined,
					resourceType: filters.resourceType || undefined,
					since: filters.since || undefined,
					until: filters.until || undefined
				})
			)
		);
		return (res.events ?? []).map((e) => ({
			id: e.id,
			when: e.createdAt ? fmtDateTime(tsToDate(e.createdAt)) : '—',
			action: e.action,
			resource: e.resourceType || '—',
			resourceId: e.resourceId || '—',
			actor: e.actorUserId || e.actorApiKeyId || 'system',
			result: e.result,
			ip: e.ip || '—'
		}));
	}

	let actionPrefix = $state('');
	let resourceType = $state('');
	let since = $state('');
	let until = $state('');
	let applied = $state({ actionPrefix: '', resourceType: '', since: '', until: '' });

	const pageState = createLoadable(() => load(applied));

	function apply() {
		applied = { actionPrefix, resourceType, since, until };
		void pageState.reload();
	}

	function reset() {
		actionPrefix = '';
		resourceType = '';
		since = '';
		until = '';
		applied = { actionPrefix: '', resourceType: '', since: '', until: '' };
		void pageState.reload();
	}

	const canView = $derived(can(auth.orgRole, 'audit.view'));
</script>

<svelte:head><title>Audit log · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if !canView}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">No permission</p>
			<p class="mt-1">Your role does not allow viewing the audit log.</p>
		</div>
	{:else}
		<Tile title="Filters">
			<form
				class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5"
				onsubmit={(e) => {
					e.preventDefault();
					apply();
				}}
			>
				<Field id="f-action" label="Action prefix" hint="e.g. node.">
					{#snippet control(f)}
						<input
							id={f.id}
							class="h-9 w-full border border-input bg-card px-3 font-mono text-sm text-foreground focus-ring"
							type="text"
							placeholder="node."
							bind:value={actionPrefix}
							aria-describedby={f.describedBy}
						/>
					{/snippet}
				</Field>
				<Field id="f-resource" label="Resource type">
					{#snippet control(f)}
						<input
							id={f.id}
							class="h-9 w-full border border-input bg-card px-3 font-mono text-sm text-foreground focus-ring"
							type="text"
							placeholder="node"
							bind:value={resourceType}
							aria-describedby={f.describedBy}
						/>
					{/snippet}
				</Field>
				<Field id="f-since" label="Since">
					{#snippet control(f)}
						<input
							id={f.id}
							class="h-9 w-full border border-input bg-card px-3 text-sm text-foreground focus-ring"
							type="datetime-local"
							bind:value={since}
							aria-describedby={f.describedBy}
						/>
					{/snippet}
				</Field>
				<Field id="f-until" label="Until">
					{#snippet control(f)}
						<input
							id={f.id}
							class="h-9 w-full border border-input bg-card px-3 text-sm text-foreground focus-ring"
							type="datetime-local"
							bind:value={until}
							aria-describedby={f.describedBy}
						/>
					{/snippet}
				</Field>
				<div class="flex items-end gap-2">
					<Button type="submit">Filter</Button>
					<Button variant="ghost" onclick={reset}>Reset</Button>
				</div>
			</form>
		</Tile>

		{#if pageState.loading}
			<div class="h-40 animate-pulse border border-border bg-card"></div>
		{:else if pageState.error}
			<div class="border border-destructive/50 bg-card p-4 text-sm">
				<p class="font-medium text-destructive">Could not load audit events</p>
				<p class="mt-1">{pageState.error}</p>
				<div class="mt-3">
					<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
				</div>
			</div>
		{:else if (pageState.data ?? []).length === 0}
			<EmptyState title="No audit events" body="No events match the current filters." />
		{:else}
			<DataTable
				label="Audit events"
				columns={[
					{ id: 'when', label: 'When', sortable: true },
					{ id: 'action', label: 'Action', sortable: true },
					{ id: 'resource', label: 'Resource' },
					{ id: 'resourceId', label: 'Resource ID' },
					{ id: 'actor', label: 'Actor' },
					{ id: 'result', label: 'Result', sortable: true },
					{ id: 'ip', label: 'IP' }
				]}
				rows={pageState.data ?? []}
				keyFor={(row) => row.id}
				emptyTitle="No audit events"
				emptyBody="No events match the current filters."
			>
				{#snippet cell(row, col)}
					{#if col.id === 'action'}
						<span class="font-mono text-xs text-foreground">{row.action}</span>
					{:else if col.id === 'result'}
						<Tag
							tone={row.result === 'success' ? 'green' : row.result === 'denied' ? 'yellow' : 'red'}
						>
							{row.result || '—'}
						</Tag>
					{:else if col.id === 'resourceId'}
						<span class="font-mono text-xs text-muted-foreground">{row.resourceId}</span>
					{:else if col.id === 'when' || col.id === 'actor' || col.id === 'ip'}
						<span class="text-xs text-muted-foreground">{row[col.id]}</span>
					{:else}
						{String(row[col.id] ?? '—')}
					{/if}
				{/snippet}
			</DataTable>
		{/if}
	{/if}
</div>