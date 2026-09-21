<script lang="ts">
	import type { HTMLInputAttributes, HTMLInputTypeAttribute } from 'svelte/elements';
	import { cn, type WithElementRef } from '$lib/utils.js';

	type InputType = Exclude<HTMLInputTypeAttribute, 'file'>;

	type Props = WithElementRef<
		Omit<HTMLInputAttributes, 'type'> &
			({ type: 'file'; files?: FileList } | { type?: InputType; files?: undefined })
	>;

	let {
		ref = $bindable(null),
		value = $bindable(),
		type,
		files = $bindable(),
		class: className,
		...restProps
	}: Props = $props();
</script>

{#if type === 'file'}
	<input
		bind:this={ref}
		data-slot="input"
		class={cn(
			'flex h-10 w-full min-w-0 rounded-none border border-[#525252] bg-[#262626] px-3 py-2 text-sm text-[#f4f4f4] shadow-none transition-colors outline-none placeholder:text-[#6f6f6f] focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[#0f62fe] disabled:cursor-not-allowed disabled:border-[#393939] disabled:bg-[#161616] disabled:opacity-40',
			'aria-invalid:border-[#da1e28] aria-invalid:focus-visible:outline-[#da1e28]',
			className
		)}
		type="file"
		bind:files
		bind:value
		{...restProps}
	/>
{:else}
	<input
		bind:this={ref}
		data-slot="input"
		class={cn(
			'flex h-10 w-full min-w-0 rounded-none border border-[#525252] bg-[#262626] px-3 py-2 text-sm text-[#f4f4f4] shadow-none transition-colors outline-none placeholder:text-[#6f6f6f] focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[#0f62fe] disabled:cursor-not-allowed disabled:border-[#393939] disabled:bg-[#161616] disabled:opacity-40',
			'aria-invalid:border-[#da1e28] aria-invalid:focus-visible:outline-[#da1e28]',
			className
		)}
		{type}
		bind:value
		{...restProps}
	/>
{/if}
