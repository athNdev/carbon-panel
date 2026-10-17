<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { page } from '$app/state';
	import Tile from '$lib/components/Tile.svelte';
	import Tag from '$lib/components/Tag.svelte';
	import Button from '$lib/components/Button.svelte';
	import DataTable from '$lib/components/DataTable.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import Field from '$lib/components/Field.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { pushToast } from '$lib/components/toast.svelte';
	import { call, workloadClient, fileClient, addonClient, scheduleClient } from '$lib/api/client';
	import { can } from '$lib/auth/permissions';
	import { auth } from '$lib/auth/clerk.svelte';
	import { createLoadable } from '$lib/state.svelte';
	import {
		GetWorkloadRequestSchema,
		StreamWorkloadLogsRequestSchema,
		StartWorkloadRequestSchema,
		StopWorkloadRequestSchema,
		RestartWorkloadRequestSchema,
		DeleteWorkloadRequestSchema,
		SendWorkloadCommandRequestSchema,
		ListWorkloadEventsRequestSchema,
		HibernateWorkloadRequestSchema,
		WakeWorkloadRequestSchema,
		SyncIngressRoutesRequestSchema,
		GetWorkloadNetworkingRequestSchema,
		GetWorkloadConfigRequestSchema,
		UpdateWorkloadConfigRequestSchema,
		CreateWorkloadBackupRequestSchema,
		ListWorkloadBackupsRequestSchema,
		RestoreWorkloadBackupRequestSchema,
		DeleteWorkloadBackupRequestSchema,
		SetWorkloadBackupLockedRequestSchema
	} from '$lib/proto/cloud/v1/workload_pb';
	import type { WorkloadLogLine, WorkloadBackup, IngressRoute } from '$lib/proto/cloud/v1/workload_pb';
	import {
		ListFilesRequestSchema,
		ReadFileRequestSchema,
		DeleteFileRequestSchema,
		CreateDirectoryRequestSchema
	} from '$lib/proto/cloud/v1/file_pb';
	import type { FileInfo } from '$lib/proto/cloud/v1/file_pb';
	import {
		ListWorkloadAddonsRequestSchema,
		InstallAddonRequestSchema,
		ToggleAddonRequestSchema,
		UninstallAddonRequestSchema,
		SearchAddonsRequestSchema,
		AddonType
	} from '$lib/proto/cloud/v1/addon_pb';
	import type { InstalledAddon, AddonSearchResult } from '$lib/proto/cloud/v1/addon_pb';
	import {
		ListSchedulesRequestSchema,
		CreateScheduleRequestSchema,
		DeleteScheduleRequestSchema,
		RunScheduleRequestSchema,
		ScheduleActionType
	} from '$lib/proto/cloud/v1/schedule_pb';
	import type { WorkloadSchedule } from '$lib/proto/cloud/v1/schedule_pb';
	import {
		workloadStatusMeta,
		fmtCpu,
		fmtRam,
		fmtGb,
		fmtDateTime,
		tsToDate
	} from '$lib/utils';
	import { WorkloadStatus } from '$lib/proto/cloud/v1/common_pb';

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
				idleTimeoutMinutes: number;
				hibernationMode: string;
			};
		};
		networking: {
			nodeAddress: string;
			primaryAddress: string;
			srvRecord: string;
			portConflict: boolean;
		} | null;
		events: Array<{ id: string; kind: string; message: string; createdAt: Date | null }>;
	};

	async function load(): Promise<Detail> {
		const [wRes, evRes, netRes] = await Promise.all([
			call(() => workloadClient.getWorkload(create(GetWorkloadRequestSchema, { id: workloadId }))),
			call(() => workloadClient.listWorkloadEvents(create(ListWorkloadEventsRequestSchema, { id: workloadId }))),
			call(() => workloadClient.getWorkloadNetworking(create(GetWorkloadNetworkingRequestSchema, { id: workloadId }))).catch(() => null)
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
					env: { ...(w.spec?.env ?? {}) },
					idleTimeoutMinutes: w.spec?.idleTimeoutMinutes ?? 0,
					hibernationMode: w.spec?.hibernationMode ?? 'pause'
				}
			},
			networking: netRes ? {
				nodeAddress: netRes.nodeAddress,
				primaryAddress: netRes.primaryAddress,
				srvRecord: netRes.srvRecord,
				portConflict: netRes.portConflict
			} : null,
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

	// Active tab
	let activeTab = $state('overview');
	const tabs = [
		{ id: 'overview', label: 'Overview' },
		{ id: 'console', label: 'Console' },
		{ id: 'files', label: 'Files' },
		{ id: 'backups', label: 'Backups' },
		{ id: 'config', label: 'Configuration' },
		{ id: 'addons', label: 'Mods & Addons' },
		{ id: 'schedules', label: 'Schedules' }
	];

	// --- Lifecycle actions ---
	let busy = $state(false);
	let deleteOpen = $state(false);
	let deleteData = $state(false);

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
	function hibernate() {
		void act(() => workloadClient.hibernateWorkload(create(HibernateWorkloadRequestSchema, { id: workloadId, reason: 'manual' })), 'Workload hibernating.');
	}
	function wake() {
		void act(() => workloadClient.wakeWorkload(create(WakeWorkloadRequestSchema, { id: workloadId })), 'Workload waking up.');
	}
	async function syncRoutes() {
		void act(() => workloadClient.syncIngressRoutes(create(SyncIngressRoutesRequestSchema, { workloadId })), 'Ingress routes synchronized.');
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

	// --- Console / Logs ---
	type LogLine = { source: string; line: string; stderr: boolean; at: Date | null };
	let logs = $state<LogLine[]>([]);
	let logError = $state<string | null>(null);
	let logLoading = $state(false);
	let following = $state(false);
	let followIter: AsyncIterator<WorkloadLogLine> | null = null;
	const MAX_LOG_LINES = 500;

	let command = $state('');
	let commandOutput = $state<string | null>(null);

	function appendLine(line: WorkloadLogLine) {
		logs = [...logs.slice(-(MAX_LOG_LINES - 1)), { source: line.source, line: line.line, stderr: line.stderr, at: tsToDate(line.timestamp) }];
	}

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

	// --- Backups state ---
	let backups = $state<WorkloadBackup[]>([]);
	let backupsLoading = $state(false);
	let backupCreateOpen = $state(false);
	let backupName = $state('');

	async function loadBackups() {
		backupsLoading = true;
		try {
			const res = await call(() => workloadClient.listWorkloadBackups(create(ListWorkloadBackupsRequestSchema, { id: workloadId })));
			backups = res.backups ?? [];
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			backupsLoading = false;
		}
	}

	async function createBackup() {
		busy = true;
		try {
			await call(() => workloadClient.createWorkloadBackup(create(CreateWorkloadBackupRequestSchema, { id: workloadId, name: backupName.trim() })));
			pushToast('success', 'Backup created successfully.');
			backupCreateOpen = false;
			backupName = '';
			await loadBackups();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	async function restoreBackup(backupId: string) {
		if (!confirm('Are you sure you want to restore this backup? Current server data will be overwritten.')) return;
		busy = true;
		try {
			const res = await call(() => workloadClient.restoreWorkloadBackup(create(RestoreWorkloadBackupRequestSchema, { id: workloadId, backupId })));
			pushToast('success', res.message || 'Backup restored successfully.');
			await pageState.reload();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	async function toggleBackupLock(backup: WorkloadBackup) {
		try {
			await call(() => workloadClient.setWorkloadBackupLocked(create(SetWorkloadBackupLockedRequestSchema, { id: workloadId, backupId: backup.id, locked: !backup.locked })));
			pushToast('success', `Backup ${backup.locked ? 'unlocked' : 'locked'}.`);
			await loadBackups();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		}
	}

	async function deleteBackup(backupId: string) {
		if (!confirm('Are you sure you want to delete this backup?')) return;
		try {
			await call(() => workloadClient.deleteWorkloadBackup(create(DeleteWorkloadBackupRequestSchema, { id: workloadId, backupId })));
			pushToast('success', 'Backup deleted.');
			await loadBackups();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		}
	}

	// --- Files state ---
	let currentPath = $state('/');
	let files = $state<FileInfo[]>([]);
	let filesLoading = $state(false);
	let filePreviewOpen = $state(false);
	let previewFilename = $state('');
	let previewContent = $state('');
	let newDirOpen = $state(false);
	let newDirName = $state('');

	async function loadFiles(path = currentPath) {
		filesLoading = true;
		try {
			const res = await call(() => fileClient.listFiles(create(ListFilesRequestSchema, { workloadId, path })));
			files = res.files ?? [];
			currentPath = path;
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			filesLoading = false;
		}
	}

	async function readFile(filePath: string, name: string) {
		busy = true;
		try {
			const stream = fileClient.readFile(create(ReadFileRequestSchema, { workloadId, path: filePath }));
			let combined = '';
			const decoder = new TextDecoder();
			for await (const res of stream) {
				combined += decoder.decode(res.chunk, { stream: !res.isLast });
			}
			previewFilename = name;
			previewContent = combined;
			filePreviewOpen = true;
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	async function deleteFile(filePath: string, isDir: boolean) {
		if (!confirm(`Delete ${isDir ? 'directory' : 'file'} "${filePath}"?`)) return;
		try {
			await call(() => fileClient.deleteFile(create(DeleteFileRequestSchema, { workloadId, path: filePath, recursive: isDir })));
			pushToast('success', 'Deleted successfully.');
			await loadFiles();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		}
	}

	async function createDir() {
		if (!newDirName.trim()) return;
		const target = currentPath === '/' ? `/${newDirName.trim()}` : `${currentPath}/${newDirName.trim()}`;
		try {
			await call(() => fileClient.createDirectory(create(CreateDirectoryRequestSchema, { workloadId, path: target })));
			pushToast('success', 'Directory created.');
			newDirOpen = false;
			newDirName = '';
			await loadFiles();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		}
	}

	// --- Config state ---
	let configProps = $state<Record<string, string>>({});
	let configLoading = $state(false);
	let configSaving = $state(false);

	async function loadConfig() {
		configLoading = true;
		try {
			const res = await call(() => workloadClient.getWorkloadConfig(create(GetWorkloadConfigRequestSchema, { id: workloadId, file: 'server.properties' })));
			configProps = { ...(res.properties ?? {}) };
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			configLoading = false;
		}
	}

	async function saveConfig() {
		configSaving = true;
		try {
			const res = await call(() => workloadClient.updateWorkloadConfig(create(UpdateWorkloadConfigRequestSchema, {
				id: workloadId,
				file: 'server.properties',
				properties: configProps,
				restartOrReload: false
			})));
			pushToast('success', `Configuration saved (${res.actionTaken}).`);
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			configSaving = false;
		}
	}

	// --- Addons & Mods state ---
	let addons = $state<InstalledAddon[]>([]);
	let addonsLoading = $state(false);
	let searchQuery = $state('');
	let searchResults = $state<AddonSearchResult[]>([]);
	let searchLoading = $state(false);

	async function loadAddons() {
		addonsLoading = true;
		try {
			const res = await call(() => addonClient.listWorkloadAddons(create(ListWorkloadAddonsRequestSchema, { workloadId, addonType: AddonType.UNSPECIFIED })));
			addons = res.addons ?? [];
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			addonsLoading = false;
		}
	}

	async function searchAddons() {
		if (!searchQuery.trim()) return;
		searchLoading = true;
		try {
			const res = await call(() => addonClient.searchAddons(create(SearchAddonsRequestSchema, { query: searchQuery.trim(), addonType: AddonType.UNSPECIFIED, limit: 10 })));
			searchResults = res.hits ?? [];
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			searchLoading = false;
		}
	}

	async function toggleAddon(addon: InstalledAddon) {
		try {
			await call(() => addonClient.toggleAddon(create(ToggleAddonRequestSchema, { workloadId, addonType: addon.addonType, filename: addon.filename, enable: !addon.enabled })));
			pushToast('success', `${addon.name} ${addon.enabled ? 'disabled' : 'enabled'}.`);
			await loadAddons();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		}
	}

	async function uninstallAddon(addon: InstalledAddon) {
		if (!confirm(`Uninstall ${addon.name}?`)) return;
		try {
			await call(() => addonClient.uninstallAddon(create(UninstallAddonRequestSchema, { workloadId, addonType: addon.addonType, filename: addon.filename })));
			pushToast('success', `${addon.name} uninstalled.`);
			await loadAddons();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		}
	}

	async function installModrinthProject(hit: AddonSearchResult) {
		busy = true;
		try {
			await call(() => addonClient.installAddon(create(InstallAddonRequestSchema, {
				workloadId,
				addonType: hit.addonType,
				modrinthProjectId: hit.projectId,
				filename: `${hit.slug}.jar`
			})));
			pushToast('success', `Installed ${hit.title}!`);
			await loadAddons();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	// --- Schedules state ---
	let schedules = $state<WorkloadSchedule[]>([]);
	let schedulesLoading = $state(false);
	let newScheduleOpen = $state(false);
	let schedName = $state('');
	let schedCron = $state('0 4 * * *');
	let schedAction = $state<ScheduleActionType>(ScheduleActionType.RESTART);
	let schedPayload = $state('');

	async function loadSchedules() {
		schedulesLoading = true;
		try {
			const res = await call(() => scheduleClient.listSchedules(create(ListSchedulesRequestSchema, { workloadId })));
			schedules = res.schedules ?? [];
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			schedulesLoading = false;
		}
	}

	async function createSchedule() {
		if (!schedName.trim()) return;
		busy = true;
		try {
			await call(() => scheduleClient.createSchedule(create(CreateScheduleRequestSchema, {
				workloadId,
				name: schedName.trim(),
				cronExpression: schedCron.trim(),
				actionType: schedAction,
				payload: schedPayload.trim(),
				enabled: true
			})));
			pushToast('success', 'Schedule created.');
			newScheduleOpen = false;
			schedName = '';
			await loadSchedules();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	async function runScheduleNow(schedId: string) {
		busy = true;
		try {
			await call(() => scheduleClient.runSchedule(create(RunScheduleRequestSchema, { scheduleId: schedId })));
			pushToast('success', 'Schedule triggered.');
			await loadSchedules();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		} finally {
			busy = false;
		}
	}

	async function deleteSchedule(schedId: string) {
		if (!confirm('Delete this scheduled task?')) return;
		try {
			await call(() => scheduleClient.deleteSchedule(create(DeleteScheduleRequestSchema, { scheduleId: schedId })));
			pushToast('success', 'Schedule deleted.');
			await loadSchedules();
		} catch (e) {
			pushToast('error', e instanceof Error ? e.message : String(e));
		}
	}

	// Trigger tab loads
	$effect(() => {
		if (activeTab === 'backups' && backups.length === 0) void loadBackups();
		if (activeTab === 'files' && files.length === 0) void loadFiles('/');
		if (activeTab === 'config' && Object.keys(configProps).length === 0) void loadConfig();
		if (activeTab === 'addons' && addons.length === 0) void loadAddons();
		if (activeTab === 'schedules' && schedules.length === 0) void loadSchedules();
	});
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
		{@const net = pageState.data.networking}
		{@const meta = workloadStatusMeta(w.status)}
		<div class="flex flex-wrap items-center justify-between gap-3">
			<div class="flex items-center gap-3">
				<h2 class="text-lg font-semibold text-foreground">{w.name}</h2>
				<Tag tone={meta.tone}>{meta.label}</Tag>
			</div>
			{#if canManage}
				<div class="flex flex-wrap gap-2">
					{#if w.status === WorkloadStatus.RUNNING}
						<Button variant="secondary" onclick={hibernate} loading={busy}>Hibernate</Button>
						<Button variant="secondary" onclick={stop} loading={busy}>Stop</Button>
						<Button variant="secondary" onclick={restart} loading={busy}>Restart</Button>
					{:else if w.status === WorkloadStatus.HIBERNATED}
						<Button variant="primary" onclick={wake} loading={busy}>Wake Server</Button>
					{:else}
						<Button variant="primary" onclick={start} loading={busy}>Start</Button>
					{/if}
					<Button variant="danger" onclick={() => (deleteOpen = true)}>Delete</Button>
				</div>
			{/if}
		</div>

		{#if w.statusDetail}
			<div class="border border-warning/50 bg-card px-3 py-2 text-sm text-foreground" role="status">
				{w.statusDetail}
			</div>
		{/if}

		<Tabs {tabs} value={activeTab} onchange={(id) => (activeTab = id)} />

		{#if activeTab === 'overview'}
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

				<Tile title="Status & Placement">
					<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
						<dt class="text-muted-foreground">Workload ID</dt>
						<dd class="font-mono text-xs break-all">{w.id}</dd>
						<dt class="text-muted-foreground">Node</dt>
						<dd class="font-mono text-xs">{w.nodeId || '—'}</dd>
						<dt class="text-muted-foreground">Container</dt>
						<dd class="font-mono text-xs break-all">{w.containerId || '—'}</dd>
						<dt class="text-muted-foreground">Host Port</dt>
						<dd class="font-mono text-xs">{w.hostPort ? String(w.hostPort) : '—'}</dd>
						<dt class="text-muted-foreground">Created</dt>
						<dd class="text-xs">{fmtDateTime(w.createdAt)}</dd>
						<dt class="text-muted-foreground">Updated</dt>
						<dd class="text-xs">{fmtDateTime(w.updatedAt)}</dd>
					</dl>
				</Tile>

				<!-- Advanced Ingress Routing & Hibernation Tile (MINE-162) -->
				<Tile title="Ingress Routing & Auto-Hibernation" class="lg:col-span-2">
					<div class="grid grid-cols-1 gap-6 md:grid-cols-2">
						<div>
							<h3 class="text-sm font-semibold text-foreground mb-2">Gate TCP Proxy & Ingress</h3>
							<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
								<dt class="text-muted-foreground">Virtual Hostname</dt>
								<dd class="font-mono text-xs font-medium text-primary">{w.hostname || w.spec.hostname || '—'}</dd>
								<dt class="text-muted-foreground">Ingress Port</dt>
								<dd class="font-mono text-xs">25565 (Gate Proxy)</dd>
								<dt class="text-muted-foreground">Direct Node IP:Port</dt>
								<dd class="font-mono text-xs">{net ? `${net.nodeAddress}:${w.hostPort}` : `${w.nodeId}:${w.hostPort}`}</dd>
								<dt class="text-muted-foreground">DNS SRV Record</dt>
								<dd class="font-mono text-xs text-muted-foreground">{net?.srvRecord || `_minecraft._tcp.${w.hostname || 'wl.local'}`}</dd>
							</dl>
							<div class="mt-4">
								<Button variant="secondary" size="sm" onclick={syncRoutes} loading={busy}>
									Synchronize Ingress Routes
								</Button>
							</div>
						</div>
						<div>
							<h3 class="text-sm font-semibold text-foreground mb-2">Idle Hibernation Watchdog</h3>
							<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
								<dt class="text-muted-foreground">Idle Timeout</dt>
								<dd class="text-xs">
									{#if w.spec.idleTimeoutMinutes > 0}
										<span class="font-semibold text-foreground">{w.spec.idleTimeoutMinutes} minutes</span>
										<span class="text-muted-foreground"> (auto-sleep on 0 players)</span>
									{:else}
										<span class="text-muted-foreground">Disabled (always on)</span>
									{/if}
								</dd>
								<dt class="text-muted-foreground">Hibernation Mode</dt>
								<dd class="font-mono text-xs">
									{w.spec.hibernationMode === 'deep_sleep' ? 'deep_sleep (container stop / zero RAM)' : 'pause (cgroup freeze / instant wake)'}
								</dd>
								<dt class="text-muted-foreground">Current State</dt>
								<dd>
									{#if w.status === WorkloadStatus.HIBERNATED}
										<Tag tone="purple">Sleeping (Wakes on player connection)</Tag>
									{:else if w.status === WorkloadStatus.RUNNING}
										<Tag tone="green">Active & Ready</Tag>
									{:else}
										<Tag tone="neutral">Inactive</Tag>
									{/if}
								</dd>
							</dl>
						</div>
					</div>
				</Tile>

				<Tile title="Events" class="lg:col-span-2">
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
			</div>
		{/if}

		{#if activeTab === 'console'}
			<Tile title="Console & RCON">
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
{#each logs as l (l.line + (l.at?.getTime() ?? 0))}
<span class:text-destructive={l.stderr}>{l.line}</span>
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
		{/if}

		{#if activeTab === 'files'}
			<Tile title="Remote File Manager">
				<div class="mb-3 flex items-center justify-between gap-2">
					<div class="flex items-center gap-2 font-mono text-sm">
						<span class="text-muted-foreground">Path:</span>
						<span class="bg-card px-2 py-1 border border-border">{currentPath}</span>
						{#if currentPath !== '/'}
							<Button variant="secondary" size="sm" onclick={() => {
								const parts = currentPath.split('/').filter(Boolean);
								parts.pop();
								void loadFiles('/' + parts.join('/'));
							}}>Up</Button>
						{/if}
					</div>
					<div class="flex gap-2">
						<Button variant="secondary" size="sm" onclick={() => void loadFiles(currentPath)} loading={filesLoading}>Refresh</Button>
						{#if canManage}
							<Button variant="secondary" size="sm" onclick={() => (newDirOpen = true)}>New Folder</Button>
						{/if}
					</div>
				</div>

				{#if filesLoading}
					<div class="h-32 animate-pulse border border-border bg-card"></div>
				{:else if files.length === 0}
					<EmptyState title="Directory is empty" body="No files or folders found here." />
				{:else}
					<DataTable
						label="Files"
						columns={[
							{ id: 'name', label: 'Name', sortable: true },
							{ id: 'size', label: 'Size', sortable: true },
							{ id: 'actions', label: 'Actions' }
						]}
						rows={files.map((f) => ({
							id: f.path,
							name: f.name,
							isDir: f.isDir,
							path: f.path,
							size: f.isDir ? 'DIR' : `${Number(f.size)} B`
						}))}
						keyFor={(r) => r.id}
					>
						{#snippet cell(row, col)}
							{@const fPath = String(row.path ?? '')}
							{@const fName = String(row.name ?? '')}
							{@const fIsDir = Boolean(row.isDir)}
							{#if col.id === 'name'}
								<button
									class="font-mono text-xs text-left hover:underline text-primary"
									onclick={() => {
										if (fIsDir) {
											void loadFiles(fPath);
										} else {
											void readFile(fPath, fName);
										}
									}}
								>
									{fIsDir ? '📁 ' : '📄 '} {fName}
								</button>
							{:else if col.id === 'actions'}
								{#if canManage}
									<div class="flex gap-2">
										<Button variant="danger" size="sm" onclick={() => void deleteFile(fPath, fIsDir)}>Delete</Button>
									</div>
								{/if}
							{:else}
								<span class="font-mono text-xs">{String(row[col.id] ?? '—')}</span>
							{/if}
						{/snippet}
					</DataTable>
				{/if}
			</Tile>
		{/if}

		{#if activeTab === 'backups'}
			<Tile title="Workload Backups & Snapshots">
				<div class="mb-3 flex items-center justify-between gap-2">
					<p class="text-sm text-muted-foreground">Point-in-time snapshots stored with zstandard compression.</p>
					{#if canManage}
						<Button variant="primary" size="sm" onclick={() => (backupCreateOpen = true)}>Create Snapshot</Button>
					{/if}
				</div>

				{#if backupsLoading}
					<div class="h-32 animate-pulse border border-border bg-card"></div>
				{:else if backups.length === 0}
					<EmptyState title="No backups" body="Create a snapshot before upgrading or modifying your server." />
				{:else}
					<DataTable
						label="Backups"
						columns={[
							{ id: 'name', label: 'Name', sortable: true },
							{ id: 'size', label: 'Size', sortable: true },
							{ id: 'status', label: 'Status' },
							{ id: 'locked', label: 'Locked' },
							{ id: 'created', label: 'Created', sortable: true },
							{ id: 'actions', label: 'Actions' }
						]}
						rows={backups.map((b) => ({
							id: b.id,
							raw: b,
							name: b.name || b.id,
							size: `${(Number(b.sizeBytes) / (1024 * 1024)).toFixed(2)} MB`,
							status: b.status,
							locked: b.locked ? '🔒 Yes' : 'No',
							created: b.createdAt ? fmtDateTime(tsToDate(b.createdAt)) : '—'
						}))}
						keyFor={(r) => r.id}
					>
						{#snippet cell(row, col)}
							{@const bId = String(row.id ?? '')}
							{@const bRaw = row.raw as WorkloadBackup}
							{#if col.id === 'actions'}
								{#if canManage}
									<div class="flex gap-1.5">
										<Button variant="secondary" size="sm" onclick={() => void restoreBackup(bId)}>Restore</Button>
										<Button variant="secondary" size="sm" onclick={() => void toggleBackupLock(bRaw)}>
											{bRaw?.locked ? 'Unlock' : 'Lock'}
										</Button>
										<Button variant="danger" size="sm" onclick={() => void deleteBackup(bId)}>Delete</Button>
									</div>
								{/if}
							{:else}
								<span class="text-xs">{String(row[col.id] ?? '—')}</span>
							{/if}
						{/snippet}
					</DataTable>
				{/if}
			</Tile>
		{/if}

		{#if activeTab === 'config'}
			<Tile title="Server Configuration (server.properties)">
				<div class="mb-3 flex items-center justify-between gap-2">
					<p class="text-sm text-muted-foreground">Declarative tuning for game rules and network limits.</p>
					{#if canManage}
						<Button variant="primary" size="sm" onclick={saveConfig} loading={configSaving}>Save Changes</Button>
					{/if}
				</div>

				{#if configLoading}
					<div class="h-32 animate-pulse border border-border bg-card"></div>
				{:else}
					<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
						{#each Object.entries(configProps) as [key, val] (key)}
							<div class="flex flex-col gap-1 border border-border bg-card p-2">
								<span class="font-mono text-xs font-semibold text-foreground">{key}</span>
								<input
									class="h-8 border border-input bg-background px-2 font-mono text-xs text-foreground focus-ring"
									bind:value={configProps[key]}
									disabled={!canManage}
								/>
							</div>
						{/each}
					</div>
				{/if}
			</Tile>
		{/if}

		{#if activeTab === 'addons'}
			<div class="flex flex-col gap-4">
				<Tile title="Installed Mods & Plugins">
					<div class="mb-3 flex items-center justify-between gap-2">
						<p class="text-sm text-muted-foreground">Manage plugins (.jar) and mods on this instance.</p>
						<Button variant="secondary" size="sm" onclick={loadAddons} loading={addonsLoading}>Refresh</Button>
					</div>

					{#if addonsLoading}
						<div class="h-32 animate-pulse border border-border bg-card"></div>
					{:else if addons.length === 0}
						<EmptyState title="No addons installed" body="Use the Modrinth explorer below to find and install mods/plugins." />
					{:else}
						<DataTable
							label="Installed Addons"
							columns={[
								{ id: 'name', label: 'Name', sortable: true },
								{ id: 'type', label: 'Type' },
								{ id: 'size', label: 'Size' },
								{ id: 'status', label: 'State' },
								{ id: 'actions', label: 'Actions' }
							]}
							rows={addons.map((a) => ({
								id: a.filename,
								raw: a,
								name: a.name,
								type: a.addonType === AddonType.PLUGIN ? 'Plugin' : 'Mod',
								size: `${(Number(a.sizeBytes) / 1024).toFixed(1)} KB`,
								status: a.enabled ? 'Enabled' : 'Disabled'
							}))}
							keyFor={(r) => r.id}
						>
							{#snippet cell(row, col)}
								{@const aRaw = row.raw as InstalledAddon}
								{#if col.id === 'status'}
									<Tag tone={aRaw?.enabled ? 'green' : 'neutral'}>{String(row.status ?? '—')}</Tag>
								{:else if col.id === 'actions'}
									{#if canManage}
										<div class="flex gap-1.5">
											<Button variant="secondary" size="sm" onclick={() => void toggleAddon(aRaw)}>
												{aRaw?.enabled ? 'Disable' : 'Enable'}
											</Button>
											<Button variant="danger" size="sm" onclick={() => void uninstallAddon(aRaw)}>Uninstall</Button>
										</div>
									{/if}
								{:else}
									<span class="text-xs">{String(row[col.id] ?? '—')}</span>
								{/if}
							{/snippet}
						</DataTable>
					{/if}
				</Tile>

				<Tile title="Modrinth Search & Quick Install">
					<div class="flex gap-2 mb-4">
						<input
							class="h-9 flex-1 border border-input bg-card px-3 text-sm text-foreground focus-ring"
							type="text"
							placeholder="Search Modrinth mods & plugins (e.g. WorldEdit, EssentialsX, Lithium)..."
							bind:value={searchQuery}
							onkeydown={(e) => e.key === 'Enter' && void searchAddons()}
						/>
						<Button variant="primary" onclick={searchAddons} loading={searchLoading}>Search</Button>
					</div>

					{#if searchResults.length > 0}
						<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
							{#each searchResults as hit (hit.projectId)}
								<div class="flex flex-col justify-between border border-border bg-card p-3">
									<div>
										<div class="flex items-center justify-between gap-2">
											<h4 class="font-semibold text-sm text-foreground">{hit.title}</h4>
											<span class="text-xs text-muted-foreground font-mono">v{hit.latestVersion}</span>
										</div>
										<p class="text-xs text-muted-foreground mt-1 line-clamp-2">{hit.description}</p>
									</div>
									<div class="mt-3 flex items-center justify-between">
										<span class="text-xs text-muted-foreground">{hit.downloads.toLocaleString()} downloads</span>
										{#if canManage}
											<Button variant="secondary" size="sm" onclick={() => void installModrinthProject(hit)} loading={busy}>
												Install
											</Button>
										{/if}
									</div>
								</div>
							{/each}
						</div>
					{/if}
				</Tile>
			</div>
		{/if}

		{#if activeTab === 'schedules'}
			<Tile title="Automated Task Scheduler (Cron)">
				<div class="mb-3 flex items-center justify-between gap-2">
					<p class="text-sm text-muted-foreground">Automate backups, restarts, and periodic maintenance commands.</p>
					{#if canManage}
						<Button variant="primary" size="sm" onclick={() => (newScheduleOpen = true)}>New Schedule</Button>
					{/if}
				</div>

				{#if schedulesLoading}
					<div class="h-32 animate-pulse border border-border bg-card"></div>
				{:else if schedules.length === 0}
					<EmptyState title="No scheduled tasks" body="Set up automated restarts or nightly snapshots." />
				{:else}
					<DataTable
						label="Schedules"
						columns={[
							{ id: 'name', label: 'Name', sortable: true },
							{ id: 'cron', label: 'Cron' },
							{ id: 'action', label: 'Action' },
							{ id: 'lastRun', label: 'Last Run' },
							{ id: 'actions', label: 'Actions' }
						]}
						rows={schedules.map((s) => ({
							id: s.id,
							name: s.name,
							cron: s.cronExpression,
							action: ScheduleActionType[s.actionType] || 'COMMAND',
							lastRun: s.lastRunAtUnix ? fmtDateTime(new Date(Number(s.lastRunAtUnix) * 1000)) : 'Never'
						}))}
						keyFor={(r) => r.id}
					>
						{#snippet cell(row, col)}
							{@const sId = String(row.id ?? '')}
							{#if col.id === 'actions'}
								{#if canManage}
									<div class="flex gap-1.5">
										<Button variant="secondary" size="sm" onclick={() => void runScheduleNow(sId)}>Run Now</Button>
										<Button variant="danger" size="sm" onclick={() => void deleteSchedule(sId)}>Delete</Button>
									</div>
								{/if}
							{:else}
								<span class="text-xs font-mono">{String(row[col.id] ?? '—')}</span>
							{/if}
						{/snippet}
					</DataTable>
				{/if}
			</Tile>
		{/if}
	{/if}
</div>

<!-- Modal: Delete Workload -->
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

<!-- Modal: Create Backup -->
<Modal open={backupCreateOpen} title="Create snapshot" onclose={() => (backupCreateOpen = false)}>
	<Field id="bname" label="Backup label" hint="Optional description for this snapshot.">
		{#snippet control(f)}
			<input
				id={f.id}
				class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
				type="text"
				placeholder="pre-update-snapshot"
				bind:value={backupName}
			/>
		{/snippet}
	</Field>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (backupCreateOpen = false)}>Cancel</Button>
		<Button variant="primary" onclick={createBackup} loading={busy}>Create Snapshot</Button>
	{/snippet}
</Modal>

<!-- Modal: Create Directory -->
<Modal open={newDirOpen} title="Create folder" onclose={() => (newDirOpen = false)}>
	<Field id="dname" label="Folder name">
		{#snippet control(f)}
			<input
				id={f.id}
				class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
				type="text"
				placeholder="plugins"
				bind:value={newDirName}
			/>
		{/snippet}
	</Field>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (newDirOpen = false)}>Cancel</Button>
		<Button variant="primary" onclick={createDir} loading={busy}>Create</Button>
	{/snippet}
</Modal>

<!-- Modal: File Content Preview -->
<Modal open={filePreviewOpen} title={`File: ${previewFilename}`} onclose={() => (filePreviewOpen = false)}>
	<pre class="max-h-96 overflow-auto border border-border bg-background p-3 font-mono text-xs text-foreground">{previewContent}</pre>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (filePreviewOpen = false)}>Close</Button>
	{/snippet}
</Modal>

<!-- Modal: New Schedule -->
<Modal open={newScheduleOpen} title="New scheduled task" onclose={() => (newScheduleOpen = false)}>
	<div class="flex flex-col gap-3">
		<Field id="sname" label="Schedule Name">
			{#snippet control(f)}
				<input
					id={f.id}
					class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
					type="text"
					placeholder="Daily 4AM Restart"
					bind:value={schedName}
				/>
			{/snippet}
		</Field>
		<Field id="scron" label="Cron Expression" hint="Standard 5-segment cron, e.g. 0 4 * * * for 4:00 AM daily.">
			{#snippet control(f)}
				<input
					id={f.id}
					class="h-9 border border-input bg-card px-3 font-mono text-sm text-foreground focus-ring"
					type="text"
					bind:value={schedCron}
				/>
			{/snippet}
		</Field>
		<Field id="saction" label="Action Type">
			{#snippet control(f)}
				<select
					id={f.id}
					class="h-9 border border-input bg-card px-3 text-sm text-foreground focus-ring"
					bind:value={schedAction}
				>
					<option value={ScheduleActionType.RESTART}>Restart Server</option>
					<option value={ScheduleActionType.BACKUP}>Create Backup Snapshot</option>
					<option value={ScheduleActionType.COMMAND}>Run RCON Command</option>
					<option value={ScheduleActionType.STOP}>Stop Server</option>
					<option value={ScheduleActionType.START}>Start Server</option>
				</select>
			{/snippet}
		</Field>
		{#if schedAction === ScheduleActionType.COMMAND}
			<Field id="spayload" label="RCON Command" hint="Command to execute without leading slash.">
				{#snippet control(f)}
					<input
						id={f.id}
						class="h-9 border border-input bg-card px-3 font-mono text-sm text-foreground focus-ring"
						type="text"
						placeholder="say Server restarting in 5 minutes"
						bind:value={schedPayload}
					/>
				{/snippet}
			</Field>
		{/if}
	</div>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (newScheduleOpen = false)}>Cancel</Button>
		<Button variant="primary" onclick={createSchedule} loading={busy}>Save Schedule</Button>
	{/snippet}
</Modal>
