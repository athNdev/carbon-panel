<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props extends HTMLInputAttributes {
		label?: string;
		helperText?: string;
		error?: string;
		value?: string;
		class?: string;
	}

	const autoId = $props.id();
	let {
		label = '',
		helperText = '',
		error = '',
		value = $bindable(''),
		class: className = '',
		id = autoId,
		...restProps
	}: Props = $props();

	// Associate the visible label and the helper/error text with the input so
	// screen readers announce it and clicking the label focuses the field.
	const describedById = `${id}-desc`;
	const hasDescription = $derived(Boolean(error || helperText));
</script>

<div class="flex flex-col space-y-1 rounded-none font-sans {className}">
	{#if label}
		<label for={id} class="text-xs font-normal tracking-[0.32px] text-[#c6c6c6]">
			{label}
		</label>
	{/if}
	<input
		{id}
		aria-describedby={hasDescription ? describedById : undefined}
		aria-invalid={error ? 'true' : undefined}
		bind:value
		class="h-10 rounded-none border-b border-[#8d8d8d] bg-[#262626] px-4 text-sm text-[#f4f4f4] placeholder-[#6f6f6f] transition-all focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#353535] focus:outline-none disabled:border-[#393939] disabled:bg-[#161616] disabled:text-[#6f6f6f] {error
			? '!border-b-2 !border-[#da1e28]'
			: ''}"
		{...restProps}
	/>
	{#if error}
		<span
			id={describedById}
			role="alert"
			class="motion-rise-in mt-0.5 font-sans text-xs text-[#ff8389]">{error}</span
		>
	{:else if helperText}
		<span id={describedById} class="motion-rise-in mt-0.5 font-sans text-xs text-[#a8a8a8]"
			>{helperText}</span
		>
	{/if}
</div>
