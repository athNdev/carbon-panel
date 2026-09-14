<script lang="ts">
	import { Switch as SwitchPrimitive } from 'bits-ui';
	import { cn, type WithoutChildrenOrChild } from '$lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		checked = $bindable(false),
		...restProps
	}: WithoutChildrenOrChild<SwitchPrimitive.RootProps> = $props();
</script>

<SwitchPrimitive.Root
	bind:ref
	bind:checked
	data-slot="switch"
	class={cn(
		'peer inline-flex h-6 w-12 shrink-0 cursor-pointer items-center rounded-full border border-transparent transition-colors outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0f62fe] disabled:cursor-not-allowed disabled:opacity-40 data-[state=checked]:bg-[#24a148] data-[state=unchecked]:bg-[#525252]',
		className
	)}
	{...restProps}
>
	<SwitchPrimitive.Thumb
		data-slot="switch-thumb"
		class={cn(
			'pointer-events-none block size-4 rounded-full bg-white transition-transform data-[state=checked]:translate-x-[1.65rem] data-[state=unchecked]:translate-x-1'
		)}
	/>
</SwitchPrimitive.Root>
