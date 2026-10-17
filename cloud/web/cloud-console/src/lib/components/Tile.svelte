<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cls } from '$lib/utils';

	let {
		title = undefined,
		description = undefined,
		class: className = '',
		children
	}: {
		title?: string;
		description?: string;
		class?: string;
		children: Snippet;
	} = $props();
</script>

<div class={cls('border border-border bg-card p-4', className)}>
	{#if title}
		<h3 class="mb-1 text-sm font-semibold text-foreground">{title}</h3>
	{/if}
	{#if description}
		<p class="mb-3 text-xs text-muted-foreground">{description}</p>
	{/if}
	{@render children()}
</div>