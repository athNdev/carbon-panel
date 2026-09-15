<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		subtitle?: string;
		open?: boolean;
		disabled?: boolean;
		class?: string;
		children?: Snippet;
	}

	let {
		title,
		subtitle = '',
		open = $bindable(false),
		disabled = false,
		class: className = '',
		children
	}: Props = $props();
</script>

<li class="border-b border-[#393939] list-none rounded-none {className}">
	<button
		type="button"
		{disabled}
		onclick={() => (open = !open)}
		class="w-full h-12 px-4 flex items-center justify-between text-left text-sm font-medium text-[#f4f4f4] bg-transparent hover:bg-[#262626] active:bg-[#353535] focus:outline-none focus:ring-2 focus:ring-white transition-colors cursor-pointer select-none rounded-none disabled:opacity-50 disabled:cursor-not-allowed"
	>
		<div class="flex items-center gap-3 min-w-0 pr-4">
			<span class="truncate">{title}</span>
			{#if subtitle}
				<span class="text-xs text-[#a8a8a8] font-normal truncate">{subtitle}</span>
			{/if}
		</div>
		<svg
			class="h-4 w-4 shrink-0 text-[#c6c6c6] transition-transform duration-[var(--motion-base)] ease-[var(--ease-out-quart)] {open ? 'rotate-90' : ''}"
			fill="currentColor"
			viewBox="0 0 20 20"
		>
			<path fill-rule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clip-rule="evenodd" />
		</svg>
	</button>
	<div
		inert={!open}
		class="grid transition-[grid-template-rows] duration-[var(--motion-slow)] ease-[var(--ease-in-out-standard)] {open ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]'}"
	>
		<div class="overflow-hidden">
			<div class="p-4 bg-[#262626]/40 text-sm text-[#c6c6c6] rounded-none">{@render children?.()}</div>
		</div>
	</div>
</li>
