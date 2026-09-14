<script lang="ts">
	interface BreadcrumbItem {
		href?: string;
		label: string;
	}

	interface Props {
		items: BreadcrumbItem[];
		class?: string;
	}

	let { items = [], class: className = '' }: Props = $props();
</script>

<nav class="flex items-center gap-1 font-sans text-xs text-[#a8a8a8] {className}" aria-label="Breadcrumbs">
	{#each items as item, i}
		{#if i > 0}
			<span class="text-[#6f6f6f]">/</span>
		{/if}
		{#if item.href && i < items.length - 1}
			<a href={item.href} class="hover:text-white hover:underline transition-colors">
				{item.label}
			</a>
		{:else}
			<span class="text-[#f4f4f4] font-medium">{item.label}</span>
		{/if}
	{/each}
</nav>
