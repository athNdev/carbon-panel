<script lang="ts">
	import { Tabs as TabsPrimitive } from 'bits-ui';
	import { cn } from '$lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: TabsPrimitive.TriggerProps = $props();
</script>

<TabsPrimitive.Trigger
	bind:ref
	data-slot="tabs-trigger"
	class={cn(
		"inline-flex h-10 cursor-pointer items-center justify-center gap-2 rounded-none border-b-2 border-transparent px-4 py-2 text-sm font-normal whitespace-nowrap text-[#8d8d8d] transition-colors hover:bg-[#353535] hover:text-[#f4f4f4] focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[#0f62fe] disabled:pointer-events-none disabled:opacity-40 data-[state=active]:border-b-[#0f62fe] data-[state=active]:bg-[#262626] data-[state=active]:font-medium data-[state=active]:text-[#f4f4f4] [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
		className
	)}
	{...restProps}
/>
