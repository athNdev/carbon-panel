<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		class?: string;
		header?: Snippet;
		children?: Snippet;
	}

	let { class: className = '', header, children }: Props = $props();
</script>

<div class="w-full border-t border-[#393939] bg-[#161616] font-sans text-sm rounded-none {className}">
	{#if header}
		<div class="grid border-b border-[#525252] bg-[#393939] text-[#f4f4f4] text-xs font-semibold uppercase tracking-wider py-2.5 px-4 rounded-none">
			{@render header()}
		</div>
	{/if}
	<div class="divide-y divide-[#393939] bg-[#262626] text-[#f4f4f4] rounded-none">
		{@render children?.()}
	</div>
</div>
