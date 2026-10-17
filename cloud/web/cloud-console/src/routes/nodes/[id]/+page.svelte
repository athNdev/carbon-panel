<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { page } from '$app/state';
	import Tile from '$lib/components/Tile.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import Button from '$lib/components/Button.svelte';
	import DataTable from '$lib/components/DataTable.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { pushToast } from '$lib/components/toast.svelte';
	import { call, nodeClient, workloadClient } from '$lib/api/client';
	import { can } from '$lib/auth/permissions';
	import { auth } from '$lib/auth/clerk.svelte';
	import { GetNodeRequestSchema, DrainNodeRequestSchema, ResumeNodeRequestSchema, DeleteNodeRequestSchema } from '$lib/proto/cloud/v1/node_pb';
	import { ListWorkloadsRequestSchema } from '$lib/proto/cloud/v1/workload_pb';
	import { createLoadable } from '$lib/state.svelte';
	import {
		nodeStatusMeta,
		workloadStatusMeta,
		originLabel,
		providerLabel,
		fmtRam,
		fmtCpu,
		fmtGb,
		fmtRelative,
		fmtDateTime,
		tsToDate,
		cls
	} from '$lib/utils';

	const nodeId = $derived(page.params.id ?? '');

	type Detail = {
		node: {
			id: string;
			name: string;
			origin: number;
			provider: number;
			nodeTypeId: string;
			region: string;
			status: number;
			hostname: string;
			publicIp: string;
			privateIp: string;
			isSystem: boolean;
			labels: Record<string, string>;
			capacity: { vcpu: number; ramMb: number; diskGb: number; maxWorkloads: number };
			allocation: {
				cpuMillicores: number;
				ramMb: number;
				diskGb: number;
				workloadCount: number;
				runningCount: number;
			};
			metrics: {
				cpuPercent: number;
				memUsedMb: number;
				diskUsedGb: number;
				dockerVersion: string;
				agentVersion: string;
				load1: number;
			} | null;
			lastHeartbeat: Date | null;
			createdAt: Date | null;
		};
		workloads: Array<{
			id: string;
			name: string;
			status: number;
			hostname: string;
			hostPort: number;
		}>;
	};

	async function load(): Promise<Detail> {
		const [nodeRes, workloadsRes] = await Promise.all([
			call(() => nodeClient.getNode(create(GetNodeRequestSchema, { id: nodeId }))),
			call(() =>
				workloadClient.listWorkloads(
					create(ListWorkloadsRequestSchema, { nodeId })
				)
			)
		]);
		const n = nodeRes.node;
		if (!n) throw new Error('Node not found.');
		return {
			node: {
				id: n.id,
				name: n.name,
				origin: n.origin,
				provider: n.provider,
				nodeTypeId: n.nodeTypeId,
				region: n.region,
				status: n.status,
				hostname: n.hostname,
				publicIp: n.publicIp,
				privateIp: n.privateIp,
				isSystem: n.isSystem,
				labels: { ...(n.labels ?? {}) },
				capacity: {
					vcpu: n.capacity?.vcpu ?? 0,
					ramMb: Number(n.capacity?.ramMb ?? 0),
					diskGb: n.capacity?.diskGb ?? 0,
					maxWorkloads: n.capacity?.maxWorkloads ?? 0
				},
				allocation: {
					cpuMillicores: Number(n.allocation?.cpuMillicores ?? 0),
					ramMb: Number(n.allocation?.ramMb ?? 0),
					diskGb: n.allocation?.diskGb ?? 0,
					workloadCount: n.allocation?.workloadCount ?? 0,
					runningCount: n.allocation?.runningCount ?? 0
				},
				metrics: n.metrics
					? {
							cpuPercent: n.metrics.cpuPercent,
							memUsedMb: Number(n.metrics.memUsedMb ?? 0),
							diskUsedGb: n.metrics.diskUsedGb ?? 0,
							dockerVersion: n.metrics.dockerVersion,
							agentVersion: n.metrics.agentVersion,
							load1: n.metrics.load1
						}
					: null,
				lastHeartbeat: tsToDate(n.lastHeartbeat),
				createdAt: tsToDate(n.createdAt)
			},
			workloads: (workloadsRes.workloads ?? []).map((w) => ({
				id: w.id,
				name: w.name,
				status: w.status,
				hostname: w.hostname,
				hostPort: w.hostPort
			}))
		};
	}

	const pageState = createLoadable(load);

	const node = $derived(pageState.data?.node ?? null);
	const nodeStatus = $derived(node ? nodeStatusMeta(node.status) : null);
	const workloads = $derived(pageState.data?.workloads ?? []);

	function allocPct(alloc: number, cap: number): number {
		return cap > 0 ? Math.min(100, Math.round((alloc / cap) * 100)) : 0;
	}
	const cpuPct = $derived(node ? allocPct(node.allocation.cpuMillicores, node.capacity.vcpu * 1000) : 0);
	const ramPct = $derived(node ? allocPct(node.allocation.ramMb, node.capacity.ramMb) : 0);
	const diskPct = $derived(node ? allocPct(node.allocation.diskGb, node.capacity.diskGb) : 0);

	const canManage = $derived(can(auth.orgRole, 'node.manage'));
	let deleteOpen = $state(false);
	let forceDelete = $state(false);
	let busy = $state(false);

	function pctClass(pct: number): string {
		if (pct >= 90) return 'bg-destructive';
		if (pct >= 70) return 'bg-warning';
		return '';
	}

	async function drain() {
		busy = true;
		try {
			await call(() => nodeClient.drainNode(create(DrainNodeRequestSchema, { id: nodeId })));
			pushToast('success', 'Node is draining.');
			await pageState.reload();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	async function resume() {
		busy = true;
		try {
			await call(() => nodeClient.resumeNode(create(ResumeNodeRequestSchema, { id: nodeId })));
			pushToast('success', 'Node resumed.');
			await pageState.reload();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	async function remove() {
		busy = true;
		try {
			await call(() =>
				nodeClient.deleteNode(create(DeleteNodeRequestSchema, { id: nodeId, force: forceDelete }))
			);
			pushToast('success', 'Node deleted.');
			window.location.href = '/nodes';
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Node {nodeId} · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if pageState.loading}
		<div class="h-48 animate-pulse border border-border bg-card"></div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load node</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else if node}
		<div class="flex flex-wrap items-center justify-between gap-3">
			<div class="flex items-center gap-3">
				<h2 class="text-lg font-semibold text-foreground">{node.name}</h2>
				{#if nodeStatus}
					<Tag tone={nodeStatus.tone}>{nodeStatus.label}</Tag>
				{/if}
				{#if node.isSystem}<Tag tone="purple">system</Tag>{/if}
			</div>
			{#if canManage}
				<div class="flex gap-2">
					{#if node.status === 4}
						<Button variant="secondary" onclick={resume} loading={busy}>Resume</Button>
					{:else}
						<Button variant="secondary" onclick={drain} loading={busy}>Drain</Button>
					{/if}
					{#if !node.isSystem}
						<Button variant="danger" onclick={() => (deleteOpen = true)}>Delete</Button>
					{/if}
				</div>
			{/if}
		</div>

		<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
			<Tile title="Capacity vs allocation">
				<div class="flex flex-col gap-3">
					<div>
						<div class="mb-1 flex justify-between text-xs">
							<span class="text-muted-foreground">CPU</span>
							<span class="font-mono text-foreground">
								{fmtCpu(node.allocation.cpuMillicores)} / {fmtCpu(node.capacity.vcpu * 1000)}
							</span>
						</div>
						<div class="bar"><div class={cls('bar-fill', pctClass(cpuPct))} style={`width:${cpuPct}%`}></div></div>
					</div>
					<div>
						<div class="mb-1 flex justify-between text-xs">
							<span class="text-muted-foreground">RAM</span>
							<span class="font-mono text-foreground">{fmtRam(node.allocation.ramMb)} / {fmtRam(node.capacity.ramMb)}</span>
						</div>
						<div class="bar"><div class={cls('bar-fill', pctClass(ramPct))} style={`width:${ramPct}%`}></div></div>
					</div>
					<div>
						<div class="mb-1 flex justify-between text-xs">
							<span class="text-muted-foreground">Disk</span>
							<span class="font-mono text-foreground">{fmtGb(node.allocation.diskGb)} / {fmtGb(node.capacity.diskGb)}</span>
						</div>
						<div class="bar"><div class={cls('bar-fill', pctClass(diskPct))} style={`width:${diskPct}%`}></div></div>
					</div>
					<p class="text-xs text-muted-foreground">
						{node.allocation.workloadCount} workloads placed
						({node.allocation.runningCount} running){node.capacity.maxWorkloads > 0 ? `, max ${node.capacity.maxWorkloads}` : ''}.
					</p>
				</div>
			</Tile>

			<Tile title="Details">
				<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
					<dt class="text-muted-foreground">ID</dt>
					<dd class="font-mono text-xs text-foreground break-all">{node.id}</dd>
					<dt class="text-muted-foreground">Origin</dt>
					<dd>{originLabel(node.origin)}</dd>
					<dt class="text-muted-foreground">Provider</dt>
					<dd>{providerLabel(node.provider)}</dd>
					<dt class="text-muted-foreground">Node type</dt>
					<dd class="font-mono text-xs">{node.nodeTypeId || '—'}</dd>
					<dt class="text-muted-foreground">Region</dt>
					<dd>{node.region || '—'}</dd>
					<dt class="text-muted-foreground">Hostname</dt>
					<dd class="font-mono text-xs">{node.hostname || '—'}</dd>
					<dt class="text-muted-foreground">Public IP</dt>
					<dd class="font-mono text-xs">{node.publicIp || '—'}</dd>
					<dt class="text-muted-foreground">Private IP</dt>
					<dd class="font-mono text-xs">{node.privateIp || '—'}</dd>
					<dt class="text-muted-foreground">Last heartbeat</dt>
					<dd>{fmtRelative(node.lastHeartbeat)}</dd>
					<dt class="text-muted-foreground">Created</dt>
					<dd class="text-xs">{fmtDateTime(node.createdAt)}</dd>
				</dl>
			</Tile>

			<Tile title="Telemetry">
				{#if node.metrics}
					<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
						<dt class="text-muted-foreground">CPU utilisation</dt>
						<dd class="font-mono text-xs">{node.metrics.cpuPercent.toFixed(1)}%</dd>
						<dt class="text-muted-foreground">Memory used</dt>
						<dd class="font-mono text-xs">{fmtRam(node.metrics.memUsedMb)}</dd>
						<dt class="text-muted-foreground">Disk used</dt>
						<dd class="font-mono text-xs">{fmtGb(node.metrics.diskUsedGb)}</dd>
						<dt class="text-muted-foreground">Load (1m)</dt>
						<dd class="font-mono text-xs">{node.metrics.load1.toFixed(2)}</dd>
						<dt class="text-muted-foreground">Docker</dt>
						<dd class="font-mono text-xs">{node.metrics.dockerVersion || '—'}</dd>
						<dt class="text-muted-foreground">Agent</dt>
						<dd class="font-mono text-xs">{node.metrics.agentVersion || '—'}</dd>
					</dl>
				{:else}
					<p class="text-xs text-muted-foreground">No telemetry yet — waiting for the first heartbeat.</p>
				{/if}
			</Tile>

			<Tile title="Labels">
				{#if Object.keys(node.labels).length === 0}
					<p class="text-xs text-muted-foreground">No labels set.</p>
				{:else}
					<div class="flex flex-wrap gap-1.5">
						{#each Object.entries(node.labels) as [k, v] (k)}
							<Tag tone="neutral"><span class="font-mono">{k}={v}</span></Tag>
						{/each}
					</div>
				{/if}
			</Tile>
		</div>

		<Tile title="Workloads on this node">
			{#if workloads.length === 0}
				<EmptyState title="No workloads" body="No workloads are placed on this node." />
			{:else}
				<DataTable
					label="Workloads on node"
					columns={[
						{ id: 'name', label: 'Name', sortable: true },
						{ id: 'status', label: 'Status' },
						{ id: 'hostname', label: 'Hostname' },
						{ id: 'port', label: 'Port' }
					]}
					rows={workloads.map((w) => ({
						id: w.id,
						name: w.name,
						status: w.status,
						hostname: w.hostname || '—',
						port: w.hostPort ? String(w.hostPort) : '—'
					}))}
					rowHref={(row) => `/workloads/${row.id}`}
					keyFor={(row) => row.id}
				>
					{#snippet cell(row, col)}
						{#if col.id === 'status'}
							{@const meta = workloadStatusMeta(row.status as number)}
							<Tag tone={meta.tone}>{meta.label}</Tag>
						{:else if col.id === 'port'}
							<span class="font-mono text-xs">{row.port}</span>
						{:else}
							{String(row[col.id] ?? '—')}
						{/if}
					{/snippet}
				</DataTable>
			{/if}
		</Tile>
	{/if}
</div>

<Modal open={deleteOpen} title="Delete node" onclose={() => (deleteOpen = false)}>
	<p class="mb-3 text-sm text-foreground">
		Delete node <span class="font-mono text-xs">{node?.name}</span>? This removes the node from the
		organization.
	</p>
	<label class="flex items-center gap-2 text-sm text-foreground">
		<input
			type="checkbox"
			bind:checked={forceDelete}
			class="h-4 w-4 accent-primary focus-ring"
		/>
		Force: stop any running workloads first
	</label>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (deleteOpen = false)}>Cancel</Button>
		<Button variant="danger" onclick={remove} loading={busy}>Delete node</Button>
	{/snippet}
</Modal>