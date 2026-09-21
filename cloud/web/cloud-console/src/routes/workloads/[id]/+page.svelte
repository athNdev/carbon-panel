<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { page } from '$app/state';
	import Tile from '$lib/components/Tile.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import Button from '$lib/components/Button.svelte';
	import DataTable from '$lib/components/DataTable.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import Field from '$lib/components/Field.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { pushToast } from '$lib/components/toast.svelte';
	import { call, workloadClient } from '$lib/api/client';
	import { can } from '$lib/auth/permissions';
	import { auth } from '$lib/auth/clerk.svelte';
	import { createLoadable } from '$lib/state.svelte';
	import { GetWorkloadRequestSchema, StreamWorkloadLogsRequestSchema, StartWorkloadRequestSchema, StopWorkloadRequestSchema, RestartWorkloadRequestSchema, DeleteWorkloadRequestSchema, SendWorkloadCommandRequestSchema, ListWorkloadEventsRequestSchema } from '$lib/proto/cloud/v1/workload_pb';
	import type { WorkloadLogLine } from '$lib/proto/cloud/v1/workload_pb';
	import {
		workloadStatusMeta,
		fmtCpu,
		fmtRam,
		fmtGb,
		fmtDateTime,
		tsToDate
	} from '$lib/utils';

	const workloadId = $derived(page.params.id ?? '');

	type Detail = {
		workload: {
			id: string;
			name: string;
			nodeId: string;
			status: number;
			hostname: string;
			containerId: string;
			hostPort: number;
			statusDetail: string;
			createdAt: Date | null;
			updatedAt: Date | null;
			spec: {
				loader: string;
				minecraftVersion: string;
				memoryMb: number;
				cpuMillicores: number;
				diskGb: number;
				hostname: string;
				allowByoNodes: boolean;
				jvmFlags: string[];
				env: Record<string, string>;
			};
		};
		events: Array<{ id: string; kind: string; message: string; createdAt: Date | null }>;
	};

	async function load(): Promise<Detail> {
		const [wRes, evRes] = await Promise.all([
			call(() => workloadClient.getWorkload(create(GetWorkloadRequestSchema, { id: workloadId }))),
			call(() => workloadClient.listWorkloadEvents(create(ListWorkloadEventsRequestSchema, { id: workloadId })))
		]);
		const w = wRes.workload;
		if (!w) throw new Error('Workload not found.');
		return {
			workload: {
				id: w.id,
				name: w.name,
				nodeId: w.nodeId,
				status: w.status,
				hostname: w.hostname,
				containerId: w.containerId,
				hostPort: w.hostPort,
				statusDetail: w.statusDetail,
				createdAt: tsToDate(w.createdAt),
				updatedAt: tsToDate(w.updatedAt),
				spec: {
					loader: w.spec?.loader ?? '',
					minecraftVersion: w.spec?.minecraftVersion ?? '',
					memoryMb: Number(w.spec?.memoryMb ?? 0),
					cpuMillicores: Number(w.spec?.cpuMillicores ?? 0),
					diskGb: w.spec?.diskGb ?? 0,
					hostname: w.spec?.hostname ?? '',
					allowByoNodes: w.spec?.allowByoNodes ?? false,
					jvmFlags: [...(w.spec?.jvmFlags ?? [])],
					env: { ...(w.spec?.env ?? {}) }
				}
			},
			events: (evRes.events ?? []).map((e) => ({
				id: e.id,
				kind: e.kind,
				message: e.message,
				createdAt: tsToDate(e.createdAt)
			}))
		};
	}

	const pageState = createLoadable(load);

	const canManage = $derived(can(auth.orgRole, 'workload.manage'));

	type LogLine = { source: string; line: string; stderr: boolean; at: Date | null };
	let logs = $state<LogLine[]>([]);
	let logError = $state<string | null>(null);
	let logLoading = $state(false);
	let following = $state(false);
	let followIter: AsyncIterator<WorkloadLogLine> | null = null;
	const MAX_LOG_LINES = 500;

	function appendLine(line: WorkloadLogLine) {
		logs = [...logs.slice(-(MAX_LOG_LINES - 1)), { source: line.source, line: line.line, stderr: line.stderr, at: tsToDate(line.timestamp) }];
	}

	/** Tail: replay trailing lines, stop when the server pauses or ends the stream. */
	async function refreshLogs() {
		logLoading = true;
		logError = null;
		following = false;
		try {
			const iter = workloadClient
				.streamWorkloadLogs(create(StreamWorkloadLogsRequestSchema, { id: workloadId, tailLines: 200 }))
				[Symbol.asyncIterator]();
			logs = [];
			const idleMs = 1500;
			for (;;) {
				const idle = new Promise<'idle'>((resolve) => setTimeout(() => resolve('idle'), idleMs));
				const result = await Promise.race([iter.next(), idle]);
				if (result === 'idle') break;
				if (result.done) break;
				appendLine(result.value);
				if (logs.length >= 200) break;
			}
			await iter.return?.();
		} catch (e) {
			logError = e instanceof Error ? e.message : String(e);
		} finally {
			logLoading = false;
		}
	}

	/** Follow: keep the stream open and append lines live. */
	async function follow() {
		if (following) return;
		following = true;
		logError = null;
		const iter = workloadClient
			.streamWorkloadLogs(create(StreamWorkloadLogsRequestSchema, { id: workloadId, tailLines: 0 }))
			[Symbol.asyncIterator]();
		followIter = iter;
		try {
			for (;;) {
				if (!following) break;
				const result = await iter.next();
				if (result.done) break;
				appendLine(result.value);
			}
		} catch (e) {
			logError = e instanceof Error ? e.message : String(e);
		} finally {
			following = false;
			followIter = null;
			await iter.return?.();
		}
	}

	function stopFollow() {
		following = false;
		void followIter?.return?.();
	}

	let busy = $state(false);
	let deleteOpen = $state(false);
	let deleteData = $state(false);
	let command = $state('');
	let commandOutput = $state<string | null>(null);

	async function act(fn: () => Promise<unknown>, okMessage: string) {
		busy = true;
		try {
			await fn();
			pushToast('success', okMessage);
			await pageState.reload();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	function start() {
		void act(() => workloadClient.startWorkload(create(StartWorkloadRequestSchema, { id: workloadId })), 'Workload starting.');
	}
	function stop() {
		void act(() => workloadClient.stopWorkload(create(StopWorkloadRequestSchema, { id: workloadId, timeoutSeconds: 30 })), 'Workload stopping.');
	}
	function restart() {
		void act(() => workloadClient.restartWorkload(create(RestartWorkloadRequestSchema, { id: workloadId })), 'Workload restarting.');
	}
	async function remove() {
		busy = true;
		try {
			await call(() =>
				workloadClient.deleteWorkload(
					create(DeleteWorkloadRequestSchema, { id: workloadId, deleteData })
				)
			);
			pushToast('success', 'Workload deleted.');
			window.location.href = '/workloads';
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	async function sendCommand() {
		commandOutput = null;
		if (!command.trim()) return;
		busy = true;
		try {
			const res = await call(() =>
				workloadClient.sendWorkloadCommand(
					create(SendWorkloadCommandRequestSchema, { id: workloadId, command: command.trim() })
				)
			);
			commandOutput = res.output;
		} catch (e) {
			commandOutput = e instanceof Error ? e.message : String(e);
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Workload {workloadId} · Carbon Cloud</title></svelte:head>

<div class="flex flex-col gap-4">
	{#if pageState.loading}
		<div class="h-48 animate-pulse border border-border bg-card"></div>
	{:else if pageState.error}
		<div class="border border-destructive/50 bg-card p-4 text-sm">
			<p class="font-medium text-destructive">Could not load workload</p>
			<p class="mt-1">{pageState.error}</p>
			<div class="mt-3">
				<Button variant="secondary" onclick={() => void pageState.reload()}>Retry</Button>
			</div>
		</div>
	{:else if pageState.data}
		{@const w = pageState.data.workload}
		{@const meta = workloadStatusMeta(w.status)}
		<div class="flex flex-wrap items-center justify-between gap-3">
			<div class="flex items-center gap-3">
				<h2 class="text-lg font-semibold text-foreground">{w.name}</h2>
				<Tag tone={meta.tone}>{meta.label}</Tag>
			</div>
			{#if canManage}
				<div class="flex gap-2">
					<Button variant="secondary" onclick={start} loading={busy}>Start</Button>
					<Button variant="secondary" onclick={stop} loading={busy}>Stop</Button>
					<Button variant="secondary" onclick={restart} loading={busy}>Restart</Button>
					<Button variant="danger" onclick={() => (deleteOpen = true)}>Delete</Button>
				</div>
			{/if}
		</div>

		{#if w.statusDetail}
			<div class="border border-warning/50 bg-card px-3 py-2 text-sm text-foreground" role="status">
				{w.statusDetail}
			</div>
		{/if}

		<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
			<Tile title="Spec">
				<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
					<dt class="text-muted-foreground">Loader</dt>
					<dd class="font-mono text-xs">{w.spec.loader || '—'}</dd>
					<dt class="text-muted-foreground">Minecraft version</dt>
					<dd class="font-mono text-xs">{w.spec.minecraftVersion || '—'}</dd>
					<dt class="text-muted-foreground">Memory</dt>
					<dd class="font-mono text-xs">{fmtRam(w.spec.memoryMb)}</dd>
					<dt class="text-muted-foreground">CPU</dt>
					<dd class="font-mono text-xs">{fmtCpu(w.spec.cpuMillicores)}</dd>
					<dt class="text-muted-foreground">Disk</dt>
					<dd class="font-mono text-xs">{fmtGb(w.spec.diskGb)}</dd>
					<dt class="text-muted-foreground">Hostname</dt>
					<dd class="font-mono text-xs">{w.spec.hostname || '—'}</dd>
					<dt class="text-muted-foreground">BYO nodes</dt>
					<dd>{w.spec.allowByoNodes ? 'Allowed' : 'Not allowed'}</dd>
					<dt class="text-muted-foreground">JVM flags</dt>
					<dd class="font-mono text-xs break-all">{w.spec.jvmFlags.join(' ') || '—'}</dd>
				</dl>
				{#if Object.keys(w.spec.env).length > 0}
					<p class="mt-3 mb-1 text-xs font-medium text-muted-foreground">Environment</p>
					<div class="flex flex-wrap gap-1.5">
						{#each Object.entries(w.spec.env) as [k, v] (k)}
							<Tag tone="neutral"><span class="font-mono">{k}={v}</span></Tag>
						{/each}
					</div>
				{/if}
			</Tile>

			<Tile title="Status">
				<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
					<dt class="text-muted-foreground">ID</dt>
					<dd class="font-mono text-xs break-all">{w.id}</dd>
					<dt class="text-muted-foreground">Node</dt>
					<dd class="font-mono text-xs">{w.nodeId || '—'}</dd>
					<dt class="text-muted-foreground">Container</dt>
					<dd class="font-mono text-xs break-all">{w.containerId || '—'}</dd>
					<dt class="text-muted-foreground">Host port</dt>
					<dd class="font-mono text-xs">{w.hostPort ? String(w.hostPort) : '—'}</dd>
					<dt class="text-muted-foreground">Player hostname</dt>
					<dd class="font-mono text-xs">{w.hostname || '—'}</dd>
					<dt class="text-muted-foreground">Created</dt>
					<dd class="text-xs">{fmtDateTime(w.createdAt)}</dd>
					<dt class="text-muted-foreground">Updated</dt>
					<dd class="text-xs">{fmtDateTime(w.updatedAt)}</dd>
				</dl>
			</Tile>
		</div>

		<Tile title="Console">
			<div class="mb-2 flex items-center justify-between gap-2">
				<div class="flex gap-2">
					<Button variant="secondary" size="sm" onclick={refreshLogs} loading={logLoading}>Tail logs</Button>
					{#if following}
						<Button variant="danger" size="sm" onclick={stopFollow}>Stop following</Button>
					{:else}
						<Button variant="secondary" size="sm" onclick={follow}>Follow live</Button>
					{/if}
				</div>
				<span class="text-xs text-muted-foreground" role="status">
					{#if following}Following live…{:else}{logs.length} lines{/if}
				</span>
			</div>
			{#if logError}
				<div class="border border-destructive/50 bg-card px-3 py-2 text-sm text-destructive">
					{logError}
				</div>
			{/if}
			{#if logs.length === 0 && !logLoading}
				<EmptyState title="No logs yet" body="Start the workload and tail its console." />
			{:else}
				<pre class="max-h-96 overflow-auto border border-border bg-background p-3 font-mono text-xs text-foreground">
{#each logs as l (l.line + l.at?.getTime())}
<span class:stderr-line={l.stderr}>{l.line}</span>
{/each}</pre>
			{/if}

			<form
				class="mt-3 flex max-w-md flex-col gap-3"
				onsubmit={(e) => {
					e.preventDefault();
					void sendCommand();
				}}
			>
				<Field id="cmd" label="Console command" hint="Sent over RCON, without a leading slash.">
					{#snippet control(f)}
						<input
							id={f.id}
							class="h-9 border border-input bg-card px-3 font-mono text-sm text-foreground focus-ring"
							type="text"
							placeholder="list"
							bind:value={command}
							disabled={!canManage}
							aria-describedby={f.describedBy}
						/>
					{/snippet}
				</Field>
				<div>
					<Button type="submit" size="sm" loading={busy} disabled={!canManage}>Send</Button>
				</div>
				{#if commandOutput !== null}
					<pre class="max-h-40 overflow-auto border border-border bg-background p-3 font-mono text-xs text-foreground">{commandOutput}</pre>
				{/if}
			</form>
		</Tile>

		<Tile title="Events">
			{#if pageState.data.events.length === 0}
				<EmptyState title="No events" body="Lifecycle transitions will appear here." />
			{:else}
				<DataTable
					label="Workload events"
					columns={[
						{ id: 'when', label: 'When', sortable: true },
						{ id: 'kind', label: 'Kind', sortable: true },
						{ id: 'message', label: 'Message' }
					]}
					rows={pageState.data.events.map((e) => ({
						id: e.id,
						when: e.createdAt ? fmtDateTime(e.createdAt) : '—',
						kind: e.kind,
						message: e.message
					}))}
					keyFor={(row) => row.id}
				>
					{#snippet cell(row, col)}
						{#if col.id === 'kind'}
							<Tag tone="neutral"><span class="font-mono">{row.kind}</span></Tag>
						{:else if col.id === 'when'}
							<span class="text-xs text-muted-foreground">{row.when}</span>
						{:else}
							{String(row[col.id] ?? '—')}
						{/if}
					{/snippet}
				</DataTable>
			{/if}
		</Tile>
	{/if}
</div>

<Modal open={deleteOpen} title="Delete workload" onclose={() => (deleteOpen = false)}>
	<p class="mb-3 text-sm text-foreground">
		Delete workload <span class="font-mono text-xs">{pageState.data?.workload.name}</span>?
	</p>
	<label class="flex items-center gap-2 text-sm text-foreground">
		<input
			type="checkbox"
			bind:checked={deleteData}
			class="h-4 w-4 accent-primary focus-ring"
		/>
		Also delete the world data
	</label>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (deleteOpen = false)}>Cancel</Button>
		<Button variant="danger" onclick={remove} loading={busy}>Delete workload</Button>
	{/snippet}
</Modal>