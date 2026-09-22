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

	const filtered = $derived(players.filter((p) => p.toLowerCase().includes(search.toLowerCase())));

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
	<div class="rounded-none border border-[#393939] bg-[#262626] p-6">
		<div class="mb-1 flex items-center justify-between">
			<h3 class="text-base font-semibold text-[#f4f4f4]">
				Players{#if onlineCount > 0}
					({onlineCount} online){/if}
			</h3>
			<button
				class="h-8 items-center rounded-none bg-[#393939] px-4 text-xs text-white transition-colors hover:bg-[#4c4c4c]"
				onclick={loadPlayers}
				disabled={loading}
			>
				{loading ? 'Loading…' : 'Refresh'}
			</button>
		</div>
		<p class="mb-6 text-xs text-[#a8a8a8]">
			Live roster with moderation actions (console commands)
		</p>

		<input
			type="text"
			placeholder="Search players…"
			aria-label="Search players"
			bind:value={search}
			class="mb-4 h-9 w-full max-w-xs rounded-none border border-[#393939] bg-[#161616] px-3 text-xs text-[#f4f4f4]"
		/>

		{#if filtered.length === 0}
			<p class="py-8 text-center font-mono text-xs text-[#6f6f6f]">
				{loading ? 'Loading players…' : 'No players online.'}
			</p>
		{:else}
			<table class="w-full text-xs" aria-label="Online players">
				<thead>
					<tr class="border-b border-[#393939] text-left text-[#a8a8a8]">
						<th scope="col" class="py-2 pr-4 font-medium">Player</th>
						<th scope="col" class="py-2 font-medium">Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each filtered as player (player)}
						<tr class="border-b border-[#262626] font-mono text-[#f4f4f4]">
							<td class="py-2 pr-4">{player}</td>
							<td class="flex flex-wrap gap-2 py-2">
								{#each [['kick', 'Kick'], ['ban', 'Ban'], ['op', 'Op'], ['deop', 'Deop']] as [cmd, label]}
									<button
										class="h-7 rounded-none bg-[#393939] px-3 text-white transition-colors hover:bg-[#4c4c4c] disabled:opacity-50"
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
			<h4 class="mt-6 mb-2 text-sm font-semibold text-[#f4f4f4]">Banned ({banned.length})</h4>
			<p class="font-mono text-xs text-[#a8a8a8]">{banned.join(', ')}</p>
		{/if}
	</div>
</div>
