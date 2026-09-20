<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { Server } from '$lib/proto/carbonpanel/v1/common_pb';
	import { ListServerPlayersRequestSchema } from '$lib/proto/carbonpanel/v1/server_pb';
	import { SendCommandRequestSchema } from '$lib/proto/carbonpanel/v1/server_pb';
	import { GetFileRequestSchema } from '$lib/proto/carbonpanel/v1/file_pb';
	import { toast } from 'svelte-sonner';

	let { server, active = false }: { server: Server; active?: boolean } = $props();

	let players = $state<string[]>([]);
	let onlineCount = $state(0);
	let banned = $state<string[]>([]);
	let loading = $state(false);
	let search = $state('');
	let acting = $state<string | null>(null);

	const filtered = $derived(
		players.filter((p) => p.toLowerCase().includes(search.toLowerCase()))
	);

	async function loadPlayers() {
		loading = true;
		try {
			const request = create(ListServerPlayersRequestSchema, { serverId: server.id });
			const response = await rpcClient.server.listServerPlayers(request);
			players = response.players;
			onlineCount = response.onlineCount;
		} catch (error) {
			toast.error(
				'Failed to load players: ' + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			loading = false;
		}
	}

	async function loadBanned() {
		try {
			const request = create(GetFileRequestSchema, {
				serverId: server.id,
				path: 'banned-players.json'
			});
			const response = await rpcClient.file.getFile(request);
			const text = new TextDecoder().decode(response.content as Uint8Array);
			const list = JSON.parse(text);
			banned = Array.isArray(list) ? list.map((b) => b.name ?? b.uuid ?? String(b)) : [];
		} catch {
			banned = [];
		}
	}

	async function runPlayerCommand(player: string, cmd: string) {
		acting = `${cmd}:${player}`;
		try {
			const request = create(SendCommandRequestSchema, {
				id: server.id,
				command: `${cmd} ${player}`
			});
			const response = await rpcClient.server.sendCommand(request);
			if (response.success) {
				toast.success(`Sent: ${cmd} ${player}`);
				await loadPlayers();
			} else {
				toast.error(response.error || `Failed: ${cmd} ${player}`);
			}
		} catch (error) {
			toast.error(
				`Failed: ${cmd} ${player}: ` + (error instanceof Error ? error.message : 'Unknown error')
			);
		} finally {
			acting = null;
		}
	}

	$effect(() => {
		if (active) {
			loadPlayers();
			loadBanned();
		}
	});
</script>

<div class="h-full overflow-y-auto">
	<div class="bg-[#262626] border border-[#393939] p-6 rounded-none">
		<div class="flex items-center justify-between mb-1">
			<h3 class="text-base font-semibold text-[#f4f4f4]">
				Players{#if onlineCount > 0} ({onlineCount} online){/if}
			</h3>
			<button
				class="h-8 px-4 bg-[#393939] hover:bg-[#4c4c4c] text-white text-xs items-center rounded-none transition-colors"
				onclick={loadPlayers}
				disabled={loading}
			>
				{loading ? 'Loading…' : 'Refresh'}
			</button>
		</div>
		<p class="text-xs text-[#a8a8a8] mb-6">Live roster with moderation actions (console commands)</p>

		<input
			type="text"
			placeholder="Search players…"
			aria-label="Search players"
			bind:value={search}
			class="mb-4 h-9 w-full max-w-xs bg-[#161616] border border-[#393939] px-3 text-xs text-[#f4f4f4] rounded-none"
		/>

		{#if filtered.length === 0}
			<p class="text-xs font-mono text-[#6f6f6f] py-8 text-center">
				{loading ? 'Loading players…' : 'No players online.'}
			</p>
		{:else}
			<table class="w-full text-xs" aria-label="Online players">
				<thead>
					<tr class="text-left text-[#a8a8a8] border-b border-[#393939]">
						<th scope="col" class="py-2 pr-4 font-medium">Player</th>
						<th scope="col" class="py-2 font-medium">Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each filtered as player (player)}
						<tr class="border-b border-[#262626] text-[#f4f4f4] font-mono">
							<td class="py-2 pr-4">{player}</td>
							<td class="py-2 flex flex-wrap gap-2">
								{#each [['kick', 'Kick'], ['ban', 'Ban'], ['op', 'Op'], ['deop', 'Deop']] as [cmd, label]}
									<button
										class="h-7 px-3 bg-[#393939] hover:bg-[#4c4c4c] disabled:opacity-50 text-white rounded-none transition-colors"
										disabled={acting !== null}
										onclick={() => runPlayerCommand(player, cmd)}
									>
										{acting === `${cmd}:${player}` ? '…' : label}
									</button>
								{/each}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}

		{#if banned.length > 0}
			<h4 class="text-sm font-semibold text-[#f4f4f4] mt-6 mb-2">Banned ({banned.length})</h4>
			<p class="text-xs font-mono text-[#a8a8a8]">{banned.join(', ')}</p>
		{/if}
	</div>
</div>
