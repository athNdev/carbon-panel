<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cls } from '$lib/utils';

	let {
		open = false,
		title = '',
		children,
		footer = undefined,
		onclose = undefined,
		labelledBy = undefined
	}: {
		open?: boolean;
		title?: string;
		children: Snippet;
		footer?: Snippet;
		onclose?: () => void;
		labelledBy?: string;
	} = $props();

	function close() {
		open = false;
		onclose?.();
	}

	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') close();
	}
</script>

{#if open}
	<div
		class="fixed inset-0 z-40 flex items-start justify-center overflow-y-auto bg-black/60 p-4 pt-24"
		role="presentation"
		onclick={(e) => {
			if (e.target === e.currentTarget) close();
		}}
	>
		<div
			class="w-full max-w-lg border border-border bg-card p-5 shadow-2xl"
			role="dialog"
			aria-modal="true"
			aria-labelledby={labelledBy ?? undefined}
			tabindex="-1"
			onkeydown={onKeydown}
		>
			<div class="mb-4 flex items-start justify-between gap-4">
				<h2 id={labelledBy} class="text-base font-semibold text-foreground">{title}</h2>
				<button
					class="shrink-0 text-muted-foreground hover:text-foreground focus-ring"
					aria-label="Close dialog"
					onclick={close}
				>
					✕
				</button>
			</div>
			<div class="text-sm text-foreground">{@render children()}</div>
			{#if footer}
				<div class="mt-5 flex justify-end gap-2">{@render footer()}</div>
			{/if}
		</div>
	</div>
{/if}