<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Badge } from '$lib/components/ui/badge';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Search, Loader2, AlertTriangle, Package } from '@lucide/svelte';
	import { rpcClient } from '$lib/api/rpc-client';

	export interface DatapackSearchResult {
		id: string;
		indexer: string;
		name: string;
		slug: string;
		summary: string;
		iconUrl: string;
		author: string;
		downloadCount: number;
		gameVersions: string[];
	}

	interface Props {
		gameVersion?: string;
		emptyHint?: string;
		/** Per-result action, e.g. an Install or Add button. */
		actions: Snippet<[DatapackSearchResult]>;
	}

	let {
		gameVersion = '',
		emptyHint = 'Search for datapacks to install.',
		actions
	}: Props = $props();

	let query = $state('');
	let results = $state<DatapackSearchResult[]>([]);
	let searching = $state(false);
	let searched = $state(false);
	let error = $state('');

	export async function search() {
		searching = true;
		error = '';
		try {
			const res = await rpcClient.datapack.searchDatapacks({
				query,
				gameVersion,
				limit: 20,
				offset: 0
			});
			results = (res.datapacks ?? []).map((d) => ({
				id: d.id,
				indexer: d.indexer,
				name: d.name,
				slug: d.slug,
				summary: d.summary,
				iconUrl: d.iconUrl,
				author: d.author,
				downloadCount: Number(d.downloadCount),
				gameVersions: d.gameVersions ?? []
			}));
			searched = true;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Datapack search failed';
		} finally {
			searching = false;
		}
	}

	function onkeydown(event: KeyboardEvent) {
		if (event.key === 'Enter') search();
	}
</script>

<div class="w-full min-w-0 space-y-3">
	<div class="flex items-center gap-2">
		<div class="relative flex-1">
			<Search class="absolute top-1/2 left-2 h-4 w-4 -translate-y-1/2 text-[#8d8d8d]" />
			<Input
				bind:value={query}
				{onkeydown}
				placeholder="Search datapacks..."
				class="border-[#393939] bg-[#262626] pl-8 text-[#f4f4f4]"
			/>
		</div>
		<Button onclick={search} disabled={searching} class="rounded-none bg-[#0f62fe] text-white">
			{#if searching}<Loader2 class="h-4 w-4 animate-spin" />{:else}Search{/if}
		</Button>
	</div>

	{#if error}
		<div
			class="flex items-start gap-2 border border-[#da1e28]/40 bg-[#da1e28]/10 p-3 text-sm text-[#ff8389]"
		>
			<AlertTriangle class="mt-0.5 h-4 w-4 shrink-0" />
			<span>{error}</span>
		</div>
	{/if}

	<div class="max-h-[50vh] min-w-0 space-y-2 overflow-y-auto">
		{#each results as d (d.id)}
			<Card class="border-[#393939] bg-[#262626]">
				<CardContent class="flex items-start gap-3 p-3">
					{#if d.iconUrl}
						<img src={d.iconUrl} alt="" class="h-10 w-10 shrink-0 object-cover" />
					{:else}
						<div class="flex h-10 w-10 shrink-0 items-center justify-center bg-[#393939]">
							<Package class="h-5 w-5 text-[#8d8d8d]" />
						</div>
					{/if}
					<div class="min-w-0 flex-1">
						<div class="flex items-center gap-2">
							<span class="truncate font-medium text-white">{d.name}</span>
							<Badge class="border-[#393939] bg-[#393939] text-[#c6c6c6]">datapack</Badge>
							{#if d.indexer}
								<Badge class="border-[#393939] bg-[#262626] text-[#8d8d8d]">{d.indexer}</Badge>
							{/if}
						</div>
						<p class="line-clamp-2 text-xs text-[#a8a8a8]">{d.summary}</p>
						<div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-[#8d8d8d]">
							<span>by {d.author}</span>
							<span>· {d.downloadCount.toLocaleString()} downloads</span>
							{#if d.gameVersions.length}
								<span>· {d.gameVersions.slice(0, 3).join(', ')}</span>
							{/if}
						</div>
					</div>
					<div class="shrink-0">{@render actions(d)}</div>
				</CardContent>
			</Card>
		{:else}
			{#if searched && !searching}
				<p class="py-8 text-center text-sm text-[#8d8d8d]">No datapacks found.</p>
			{:else if !searching}
				<p class="py-8 text-center text-sm text-[#8d8d8d]">{emptyHint}</p>
			{/if}
		{/each}
	</div>
</div>
