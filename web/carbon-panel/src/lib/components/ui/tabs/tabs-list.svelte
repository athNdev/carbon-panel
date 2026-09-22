<script lang="ts">
	import { Tabs as TabsPrimitive } from 'bits-ui';
	import { cn } from '$lib/utils.js';

	let { ref = $bindable(null), class: className, ...restProps }: TabsPrimitive.ListProps = $props();
</script>

<TabsPrimitive.List
	bind:ref
	data-slot="tabs-list"
	class={cn(
		'inline-flex h-[41px] w-full items-center justify-start gap-0 overflow-x-auto overflow-y-hidden rounded-none border-b border-[#393939] bg-transparent p-0',
		className
	)}
	{...restProps}
/>
