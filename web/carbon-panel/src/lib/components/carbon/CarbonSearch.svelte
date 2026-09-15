<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';
	import { fade } from 'svelte/transition';

	interface Props extends Omit<HTMLInputAttributes, 'size'> {
		value?: string;
		placeholder?: string;
		size?: 'sm' | 'md' | 'lg';
		disabled?: boolean;
		class?: string;
		onclear?: () => void;
		onkeydown?: (e: KeyboardEvent) => void;
	}

	let {
		value = $bindable(''),
		placeholder = 'Search...',
		size = 'md',
		disabled = false,
		class: className = '',
		onclear,
		onkeydown,
		...restProps
	}: Props = $props();

	const sizeClasses = {
		sm: 'h-8 text-xs pl-9 pr-7',
		md: 'h-10 text-sm pl-10 pr-9',
		lg: 'h-12 text-base pl-12 pr-10'
	};

	const iconSizeClasses = {
		sm: 'h-3.5 w-3.5',
		md: 'h-4 w-4',
		lg: 'h-4.5 w-4.5'
	};

	function handleClear() {
		value = '';
		onclear?.();
	}
</script>

<div class="relative w-full font-sans rounded-none {className}">
	<div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-[#8d8d8d]">
		<svg class="{iconSizeClasses[size]}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
		</svg>
	</div>
	<input
		type="text"
		bind:value
		{placeholder}
		{disabled}
		{onkeydown}
		class="w-full bg-[#262626] border-b border-[#8d8d8d] text-[#f4f4f4] placeholder-[#6f6f6f] rounded-none transition-colors focus:outline-none focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#353535] disabled:bg-[#161616] disabled:border-[#393939] disabled:text-[#6f6f6f] {sizeClasses[size]}"
		{...restProps}
	/>
	{#if value}
		<button
			type="button"
			onclick={handleClear}
			aria-label="Clear search"
			transition:fade={{ duration: 100 }}
			class="absolute inset-y-0 right-0 flex items-center px-2.5 text-[#a8a8a8] hover:text-[#f4f4f4] cursor-pointer rounded-none"
		>
			<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
				<path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
			</svg>
		</button>
	{/if}
</div>
