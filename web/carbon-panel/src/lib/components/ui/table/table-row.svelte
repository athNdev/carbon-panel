<script lang="ts">
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLTableRowElement>> = $props();
</script>

<tr
	bind:this={ref}
	data-slot="table-row"
	class={cn(
		'border-b border-[#393939] bg-[#262626] transition-colors data-[state=selected]:bg-[#393939] hover:[&,&>svelte-css-wrapper]:[&>th,td]:bg-[#353535]',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</tr>
