<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title = 'Nothing here',
		body = 'There is nothing to show yet.',
		action = undefined
	}: {
		title?: string;
		body?: string;
		action?: Snippet;
	} = $props();
</script>

<div class="flex flex-col items-center justify-center border border-dashed border-border bg-card/50 px-6 py-14 text-center">
	<svg
		class="mb-3 h-8 w-8 text-muted-foreground"
		fill="none"
		viewBox="0 0 24 24"
		stroke="currentColor"
		stroke-width="1.5"
		aria-hidden="true"
	>
		<path
			stroke-linecap="round"
			stroke-linejoin="round"
			d="M3.75 9.75v9a.75.75 0 0 0 .75.75h15a.75.75 0 0 0 .75-.75v-9m-16.5 0 1.5-4.5h13.5l1.5 4.5m-16.5 0h16.5M12 12.75v3"
		/>
	</svg>
	<p class="text-sm font-semibold text-foreground">{title}</p>
	<p class="mt-1 max-w-sm text-xs text-muted-foreground">{body}</p>
	{#if action}
		<div class="mt-4">{@render action()}</div>
	{/if}
</div>