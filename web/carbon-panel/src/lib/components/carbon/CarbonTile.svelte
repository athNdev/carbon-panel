<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		clickable?: boolean;
		href?: string;
		class?: string;
		onclick?: () => void;
		children?: Snippet;
	}

	let {
		clickable = false,
		href = undefined,
		class: className = '',
		onclick,
		children
	}: Props = $props();

	const baseClasses =
		'bg-[#262626] border border-[#393939] p-4 text-[#f4f4f4] transition-[background-color,border-color,transform] duration-[var(--motion-base)] ease-[var(--ease-out-quart)] relative select-none';
	const hoverClasses =
		clickable || href
			? 'hover:bg-[#353535] hover:border-[#525252] hover:-translate-y-px cursor-pointer focus:outline-none focus:ring-2 focus:ring-white focus:ring-offset-2 focus:ring-offset-[#161616]'
			: '';
</script>

{#if href}
	<a {href} class="{baseClasses} {hoverClasses} block {className}">
		{@render children?.()}
	</a>
{:else if clickable}
	<div
		role="button"
		tabindex="0"
		{onclick}
		onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && onclick?.()}
		class="{baseClasses} {hoverClasses} {className}"
	>
		{@render children?.()}
	</div>
{:else}
	<div class="{baseClasses} {className}">
		{@render children?.()}
	</div>
{/if}
