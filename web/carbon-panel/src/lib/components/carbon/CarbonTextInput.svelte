<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props extends HTMLInputAttributes {
		label?: string;
		helperText?: string;
		error?: string;
		value?: string;
		class?: string;
	}

	let {
		label = '',
		helperText = '',
		error = '',
		value = $bindable(''),
		class: className = '',
		...restProps
	}: Props = $props();
</script>

<div class="flex flex-col space-y-1 font-sans rounded-none {className}">
	{#if label}
		<label class="text-xs font-normal text-[#c6c6c6] tracking-[0.32px]">
			{label}
		</label>
	{/if}
	<input
		bind:value
		class="h-10 px-4 bg-[#262626] border-b border-[#8d8d8d] text-sm text-[#f4f4f4] placeholder-[#6f6f6f] rounded-none transition-all focus:outline-none focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#353535] disabled:bg-[#161616] disabled:border-[#393939] disabled:text-[#6f6f6f] {error ? '!border-b-2 !border-[#da1e28]' : ''}"
		{...restProps}
	/>
	{#if error}
		<span class="text-xs text-[#ff8389] font-sans mt-0.5">{error}</span>
	{:else if helperText}
		<span class="text-xs text-[#a8a8a8] font-sans mt-0.5">{helperText}</span>
	{/if}
</div>
