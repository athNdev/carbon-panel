<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { Server } from '$lib/proto/carbonpanel/v1/common_pb';
	import {
		ListActivityLogsRequestSchema,
		type ActivityLogEntry
	} from '$lib/proto/carbonpanel/v1/activity_pb';
	import { toast } from 'svelte-sonner';

	let { server, active = false }: { server: Server; active?: boolean } = $props();

	let entries = $state<ActivityLogEntry[]>([]);
	let loading = $state(false);

	async function loadActivity() {
		loading = true;
		try {
			const request = create(ListActivityLogsRequestSchema, {
				serverId: server.id,
				limit: 100
			});
			const response = await rpcClient.activity.listActivityLogs(request);
			entries = response.entries;
		} catch (error) {
			toast.error(
				'Failed to load activity: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if (active) {
			loadActivity();
		}
	});

	function formatTime(ts: unknown): string {
		try {
			const d = (ts as { toDate?: () => Date })?.toDate?.() ?? new Date(ts as string);
			return d.toLocaleString();
		} catch {
			return '';
		}
	}
</script>

<div class="h-full overflow-y-auto">
	<div class="rounded-none border border-[#393939] bg-[#262626] p-6">
		<div class="mb-1 flex items-center justify-between">
			<h3 class="text-base font-semibold text-[#f4f4f4]">Activity</h3>
			<button
				class="h-8 items-center rounded-none bg-[#393939] px-4 text-xs text-white transition-colors hover:bg-[#4c4c4c]"
				onclick={loadActivity}
				disabled={loading}
			>
				{loading ? 'Loading…' : 'Refresh'}
			</button>
		</div>
		<p class="mb-6 text-xs text-[#a8a8a8]">Who did what on this server, newest first</p>

		{#if entries.length === 0}
			<p class="py-8 text-center font-mono text-xs text-[#6f6f6f]">
				{loading
					? 'Loading activity…'
					: 'No activity recorded yet. Start or stop the server to generate entries.'}
			</p>
		{:else}
			<table class="w-full text-xs" aria-label="Server activity log">
				<thead>
					<tr class="border-b border-[#393939] text-left text-[#a8a8a8]">
						<th scope="col" class="py-2 pr-4 font-medium">Time</th>
						<th scope="col" class="py-2 pr-4 font-medium">Actor</th>
						<th scope="col" class="py-2 pr-4 font-medium">Event</th>
						<th scope="col" class="py-2 font-medium">IP</th>
					</tr>
				</thead>
				<tbody>
					{#each entries as entry (entry.id)}
						<tr class="border-b border-[#262626] font-mono text-[#f4f4f4]">
							<td class="py-2 pr-4 whitespace-nowrap">{formatTime(entry.createdAt)}</td>
							<td class="py-2 pr-4">{entry.actorName || entry.actorId || '—'}</td>
							<td class="py-2 pr-4">{entry.event}</td>
							<td class="py-2">{entry.ip || '—'}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</div>
</div>
