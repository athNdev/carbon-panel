<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		class?: string;
		header?: Snippet;
		children?: Snippet;
	}

	let { class: className = '', header, children }: Props = $props();
</script>

<div
	class="w-full rounded-none border-t border-[#393939] bg-[#161616] font-sans text-sm {className}"
>
	{#if header}
		<div
			class="grid rounded-none border-b border-[#525252] bg-[#393939] px-4 py-2.5 text-xs font-semibold tracking-wider text-[#f4f4f4] uppercase"
		>
			{@render header()}
		</div>
	{/if}
	<div class="divide-y divide-[#393939] rounded-none bg-[#262626] text-[#f4f4f4]">
		{@render children?.()}
	</div>
</div>
