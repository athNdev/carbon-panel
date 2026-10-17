<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cls } from '$lib/utils';

	// Field wires label + input + error/hint together with aria-describedby.
	// The `control` snippet receives { id, describedBy } so the input can wire
	// aria-describedby back to the error/hint paragraph.

	let {
		id = '',
		label = '',
		error = '',
		hint = '',
		required = false,
		control
	}: {
		id?: string;
		label?: string;
		error?: string;
		hint?: string;
		required?: boolean;
		control: Snippet<[{ id: string; describedBy: string | undefined }]>;
	} = $props();

	const describedBy = $derived(error ? `${id}-error` : hint ? `${id}-hint` : undefined);
</script>

<div class="flex flex-col gap-1.5">
	<label class="text-xs font-medium text-foreground" for={id}>
		{label}{required ? ' *' : ''}
	</label>
	{@render control({ id, describedBy })}
	{#if error}
		<p id={`${id}-error`} class="text-xs text-destructive" role="alert">{error}</p>
	{:else if hint}
		<p id={`${id}-hint`} class={cls('text-xs', 'text-muted-foreground')}>{hint}</p>
	{/if}
</div>