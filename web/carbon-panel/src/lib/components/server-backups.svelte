<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { Server } from '$lib/proto/carbonpanel/v1/common_pb';
	import {
		ListBackupsRequestSchema,
		DeleteBackupRequestSchema,
		RestoreBackupRequestSchema,
		SetBackupLockedRequestSchema,
		type BackupRecord
	} from '$lib/proto/carbonpanel/v1/backup_pb';
	import { toast } from 'svelte-sonner';

	let { server, active = false }: { server: Server; active?: boolean } = $props();

	let backups = $state<BackupRecord[]>([]);
	let loading = $state(false);
	let acting = $state<string | null>(null);

	async function loadBackups() {
		loading = true;
		try {
			const request = create(ListBackupsRequestSchema, { serverId: server.id, limit: 100 });
			const response = await rpcClient.backup.listBackups(request);
			backups = response.backups;
		} catch (error) {
			toast.error(
				'Failed to load backups: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			loading = false;
		}
	}

	async function deleteBackup(id: string) {
		acting = `delete:${id}`;
		try {
			await rpcClient.backup.deleteBackup(create(DeleteBackupRequestSchema, { id }));
			toast.success('Backup deleted');
			await loadBackups();
		} catch (error) {
			toast.error(
				'Failed to delete backup: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			acting = null;
		}
	}

	async function restoreBackup(id: string) {
		acting = `restore:${id}`;
		try {
			const response = await rpcClient.backup.restoreBackup(
				create(RestoreBackupRequestSchema, { id })
			);
			toast.success(response.message || 'Backup restored');
			await loadBackups();
		} catch (error) {
			toast.error(
				'Failed to restore backup: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			acting = null;
		}
	}

	async function toggleLock(id: string, locked: boolean) {
		acting = `lock:${id}`;
		try {
			await rpcClient.backup.setBackupLocked(create(SetBackupLockedRequestSchema, { id, locked }));
			toast.success(locked ? 'Backup locked' : 'Backup unlocked');
			await loadBackups();
		} catch (error) {
			toast.error(
				'Failed to update lock: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			acting = null;
		}
	}

	function formatBytes(n: bigint | number): string {
		const bytes = Number(n);
		if (bytes < 1024) return `${bytes} B`;
		const units = ['KB', 'MB', 'GB'];
		let v = bytes / 1024;
		let u = 0;
		while (v >= 1024 && u < units.length - 1) {
			v /= 1024;
			u++;
		}
		return `${v.toFixed(1)} ${units[u]}`;
	}

	function formatTime(ts: unknown): string {
		try {
			const d = (ts as { toDate?: () => Date })?.toDate?.() ?? new Date(ts as string);
			return d.toLocaleString();
		} catch {
			return '';
		}
	}

	$effect(() => {
		if (active) {
			loadBackups();
		}
	});
</script>

<div class="h-full overflow-y-auto">
	<div class="rounded-none border border-[#393939] bg-[#262626] p-6">
		<div class="mb-1 flex items-center justify-between">
			<h3 class="text-base font-semibold text-[#f4f4f4]">Backups</h3>
			<button
				class="h-8 items-center rounded-none bg-[#393939] px-4 text-xs text-white transition-colors hover:bg-[#4c4c4c]"
				onclick={loadBackups}
				disabled={loading}
			>
				{loading ? 'Loading…' : 'Refresh'}
			</button>
		</div>
		<p class="mb-6 text-xs text-[#a8a8a8]">
			Scheduled and on-demand archives with restore and sticky locks
		</p>

		{#if backups.length === 0}
			<p class="py-8 text-center font-mono text-xs text-[#6f6f6f]">
				{loading ? 'Loading backups…' : 'No backups yet. Create one from a scheduled backup task.'}
			</p>
		{:else}
			<table class="w-full text-xs" aria-label="Server backups">
				<thead>
					<tr class="border-b border-[#393939] text-left text-[#a8a8a8]">
						<th scope="col" class="py-2 pr-4 font-medium">Name</th>
						<th scope="col" class="py-2 pr-4 font-medium">Created</th>
						<th scope="col" class="py-2 pr-4 font-medium">Size</th>
						<th scope="col" class="py-2 pr-4 font-medium">Status</th>
						<th scope="col" class="py-2 font-medium">Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each backups as backup (backup.id)}
						<tr class="border-b border-[#262626] font-mono text-[#f4f4f4]">
							<td class="py-2 pr-4">{backup.name}{backup.locked ? ' 🔒' : ''}</td>
							<td class="py-2 pr-4 whitespace-nowrap">{formatTime(backup.createdAt)}</td>
							<td class="py-2 pr-4">{formatBytes(backup.sizeBytes)}</td>
							<td class="py-2 pr-4">{backup.status}</td>
							<td class="flex flex-wrap gap-2 py-2">
								<button
									class="h-7 rounded-none bg-[#393939] px-3 text-white transition-colors hover:bg-[#4c4c4c] disabled:opacity-50"
									disabled={acting !== null}
									onclick={() => restoreBackup(backup.id)}
								>
									{acting === `restore:${backup.id}` ? '…' : 'Restore'}
								</button>
								<button
									class="h-7 rounded-none bg-[#393939] px-3 text-white transition-colors hover:bg-[#4c4c4c] disabled:opacity-50"
									disabled={acting !== null}
									onclick={() => toggleLock(backup.id, !backup.locked)}
								>
									{acting === `lock:${backup.id}` ? '…' : backup.locked ? 'Unlock' : 'Lock'}
								</button>
								<button
									class="h-7 rounded-none bg-[#393939] px-3 text-white transition-colors hover:bg-[#4c4c4c] disabled:opacity-50"
									disabled={acting !== null || backup.locked}
									onclick={() => deleteBackup(backup.id)}
								>
									{acting === `delete:${backup.id}` ? '…' : 'Delete'}
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</div>
</div>
