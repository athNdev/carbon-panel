<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLSelectAttributes } from 'svelte/elements';

	interface Props extends HTMLSelectAttributes {
		label?: string;
		helperText?: string;
		error?: string;
		value?: any;
		class?: string;
		children?: Snippet;
	}

	let {
		label = '',
		helperText = '',
		error = '',
		value = $bindable(''),
		class: className = '',
		children,
		...restProps
	}: Props = $props();
</script>

<div class="flex flex-col space-y-1 font-sans {className}">
	{#if label}
		<label class="text-xs font-normal text-[#c6c6c6] tracking-[0.32px]">
			{label}
		</label>
	{/if}
	<div class="relative">
		<select
			bind:value
			class="w-full h-10 pl-4 pr-10 bg-[#262626] border-b border-[#8d8d8d] text-sm text-[#f4f4f4] transition-all appearance-none focus:outline-none focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#353535] cursor-pointer disabled:bg-[#161616] disabled:border-[#393939] disabled:text-[#6f6f6f] {error ? '!border-b-2 !border-[#da1e28]' : ''}"
			{...restProps}
		>
			{@render children?.()}
		</select>
		<div class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-3 text-[#c6c6c6]">
			<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
				<path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd" />
			</svg>
		</div>
	</div>
	{#if error}
		<span class="text-xs text-[#ff8389] font-sans mt-0.5">{error}</span>
	{:else if helperText}
		<span class="text-xs text-[#a8a8a8] font-sans mt-0.5">{helperText}</span>
	{/if}
</div>
