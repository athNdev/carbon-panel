<!-- SPDX-License-Identifier: MPL-2.0 -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Inbox } from '@lucide/svelte';
	import CarbonButton from './CarbonButton.svelte';

	interface Props {
		title?: string;
		description?: string;
		icon?: Snippet;
		actionLabel?: string;
		onaction?: () => void;
		class?: string;
	}

	let {
		title = 'Nothing here yet',
		description = '',
		icon,
		actionLabel,
		onaction,
		class: className = ''
	}: Props = $props();
</script>

<div
	class="flex flex-col items-center justify-center gap-4 border border-[#393939] bg-[#262626] p-12 text-center {className}"
>
	<div class="flex h-12 w-12 items-center justify-center border border-[#393939] bg-[#161616] text-[#8d8d8d]">
		{#if icon}
			{@render icon()}
		{:else}
			<Inbox class="h-6 w-6" />
		{/if}
	</div>
	<div class="space-y-1">
		<h3 class="text-sm font-semibold text-[#f4f4f4]">{title}</h3>
		{#if description}
			<p class="max-w-md text-xs text-[#a8a8a8]">{description}</p>
		{/if}
	</div>
	{#if actionLabel && onaction}
		<CarbonButton kind="primary" size="sm" onclick={onaction}>{actionLabel}</CarbonButton>
	{/if}
</div>
