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
	<div class="bg-[#262626] border border-[#393939] p-6 rounded-none">
		<div class="flex items-center justify-between mb-1">
			<h3 class="text-base font-semibold text-[#f4f4f4]">Activity</h3>
			<button
				class="h-8 px-4 bg-[#393939] hover:bg-[#4c4c4c] text-white text-xs items-center rounded-none transition-colors"
				onclick={loadActivity}
				disabled={loading}
			>
				{loading ? 'Loading…' : 'Refresh'}
			</button>
		</div>
		<p class="text-xs text-[#a8a8a8] mb-6">Who did what on this server, newest first</p>

		{#if entries.length === 0}
			<p class="text-xs font-mono text-[#6f6f6f] py-8 text-center">
				{loading ? 'Loading activity…' : 'No activity recorded yet. Start or stop the server to generate entries.'}
			</p>
		{:else}
			<table class="w-full text-xs" aria-label="Server activity log">
				<thead>
					<tr class="text-left text-[#a8a8a8] border-b border-[#393939]">
						<th scope="col" class="py-2 pr-4 font-medium">Time</th>
						<th scope="col" class="py-2 pr-4 font-medium">Actor</th>
						<th scope="col" class="py-2 pr-4 font-medium">Event</th>
						<th scope="col" class="py-2 font-medium">IP</th>
					</tr>
				</thead>
				<tbody>
					{#each entries as entry (entry.id)}
						<tr class="border-b border-[#262626] text-[#f4f4f4] font-mono">
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
