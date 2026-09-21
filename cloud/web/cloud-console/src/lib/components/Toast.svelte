<script lang="ts">
	import { getToasts, dismissToast } from './toast.svelte';
	import { cls } from '$lib/utils';

	const toasts = getToasts();

	const toneClass: Record<string, string> = {
		info: 'border-primary/50',
		success: 'border-success/50',
		error: 'border-destructive/50'
	};

	const icon: Record<string, string> = {
		info: 'ℹ',
		success: '✓',
		error: '✕'
	};
</script>

<div
	class="fixed right-4 bottom-4 z-50 flex w-80 flex-col gap-2"
	aria-live="polite"
	aria-atomic="false"
>
	{#each toasts as toast (toast.id)}
		<div
			class={cls(
				'flex items-start gap-3 border bg-card px-3 py-2.5 text-sm text-foreground',
				toneClass[toast.kind]
			)}
			role="status"
		>
			<span class="mt-px shrink-0 font-mono text-xs" aria-hidden="true">{icon[toast.kind]}</span>
			<p class="min-w-0 flex-1 break-words">{toast.message}</p>
			<button
				class="shrink-0 text-muted-foreground hover:text-foreground focus-ring"
				aria-label="Dismiss notification"
				onclick={() => dismissToast(toast.id)}
			>
				✕
			</button>
		</div>
	{/each}
</div>