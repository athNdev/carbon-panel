<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title?: string;
		description?: string;
		class?: string;
		toolbar?: Snippet;
		header?: Snippet;
		children?: Snippet;
	}

	let {
		title = '',
		description = '',
		class: className = '',
		toolbar,
		header,
		children
	}: Props = $props();
</script>

<div class="w-full border border-[#393939] bg-[#161616] {className}">
	{#if title || toolbar}
		<div class="flex items-center justify-between border-b border-[#393939] bg-[#262626] p-4">
			<div>
				{#if title}
					<h3 class="font-sans text-base font-semibold text-[#f4f4f4]">{title}</h3>
				{/if}
				{#if description}
					<p class="mt-0.5 font-sans text-xs text-[#a8a8a8]">{description}</p>
				{/if}
			</div>
			{#if toolbar}
				<div class="flex items-center gap-2">
					{@render toolbar()}
				</div>
			{/if}
		</div>
	{/if}

	<div class="overflow-x-auto">
		<table class="w-full border-collapse text-left font-sans text-sm">
			{#if header}
				<thead class="border-b border-[#525252] bg-[#393939] text-[#f4f4f4]">
					{@render header()}
				</thead>
			{/if}
			<tbody class="divide-y divide-[#393939] bg-[#262626] text-[#f4f4f4]">
				{@render children?.()}
			</tbody>
		</table>
	</div>
</div>
