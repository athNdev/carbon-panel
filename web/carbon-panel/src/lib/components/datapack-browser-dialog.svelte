<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

<script lang="ts">
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import {
		DialogContent,
		DialogDescription,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Download, Loader2, Package } from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import DatapackSearch from '$lib/components/datapack-search.svelte';

	interface Props {
		open: boolean;
		gameVersion?: string;
		description?: string;
		/** Installs a datapack by index id. Injected so the dialog works for
		 * servers (world datapacks/) and studio packs (override datapacks/). */
		install: (datapackId: string, gameVersion: string, indexer: string) => Promise<void>;
		onInstalled?: () => void;
	}

	let {
		open = $bindable(false),
		gameVersion = '',
		description = "Search and install datapacks into the world's datapacks/ folder. Datapacks run on Vanilla servers with no mod loader.",
		install,
		onInstalled
	}: Props = $props();

	let installing = $state<Record<string, boolean>>({});

	async function installDatapack(id: string, name: string, indexer: string) {
		installing = { ...installing, [id]: true };
		try {
			await install(id, gameVersion, indexer);
			toast.success(`Installed datapack ${name}`);
			onInstalled?.();
		} catch (e) {
			toast.error(e instanceof Error ? e.message : 'Install failed');
		} finally {
			installing = { ...installing, [id]: false };
		}
	}
</script>

<DialogPrimitive.Root bind:open>
	<DialogContent class="border-[#393939] bg-[#161616] text-[#f4f4f4] sm:max-w-2xl">
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2">
				<Package class="h-4 w-4" /> Browse Datapacks
			</DialogTitle>
			<DialogDescription class="text-[#a8a8a8]">{description}</DialogDescription>
		</DialogHeader>

		<DatapackSearch {gameVersion} emptyHint="Search for datapacks to install into this world.">
			{#snippet actions(d)}
				<Button
					onclick={() => installDatapack(d.id, d.name, d.indexer)}
					disabled={installing[d.id]}
					class="rounded-none bg-[#0f62fe] text-white"
				>
					{#if installing[d.id]}<Loader2 class="h-4 w-4 animate-spin" />{:else}<Download
							class="h-4 w-4"
						/>{/if}
					Install
				</Button>
			{/snippet}
		</DatapackSearch>
	</DialogContent>
</DialogPrimitive.Root>
