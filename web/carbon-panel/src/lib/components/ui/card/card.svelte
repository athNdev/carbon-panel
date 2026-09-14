<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';
	import { cn, type WithElementRef } from '$lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> = $props();
</script>

<div
	bind:this={ref}
	data-slot="card"
	class={cn(
		'flex flex-col gap-4 rounded-none border border-[#393939] bg-[#262626] p-6 text-[#f4f4f4] shadow-none',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</div>
