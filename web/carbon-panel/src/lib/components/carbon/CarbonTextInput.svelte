<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props extends HTMLInputAttributes {
		label?: string;
		helperText?: string;
		error?: string;
		value?: string;
		class?: string;
		/** Show a reveal toggle for password fields. */
		revealable?: boolean;
	}

	const autoId = $props.id();
	let {
		label = '',
		helperText = '',
		error = '',
		value = $bindable(''),
		class: className = '',
		id = autoId,
		revealable = false,
		type = 'text',
		...restProps
	}: Props = $props();

	let revealed = $state(false);
	const inputType = $derived(revealable && type === 'password' && revealed ? 'text' : type);
	let capsOn = $state(false);
	function trackCaps(event: KeyboardEvent) {
		const on = event.getModifierState?.('CapsLock') ?? false;
		if (on !== capsOn) capsOn = on;
	}

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
	<div class="relative">
		<input
			{id}
			type={inputType}
			aria-describedby={hasDescription ? describedById : undefined}
			aria-invalid={error ? 'true' : undefined}
			bind:value
			onkeyup={type === 'password' ? trackCaps : undefined}
			onkeydown={type === 'password' ? trackCaps : undefined}
			class="h-10 w-full rounded-none border-b border-[#8d8d8d] bg-[#262626] px-4 text-sm text-[#f4f4f4] placeholder-[#6f6f6f] transition-all focus:border-b-2 focus:border-[#0f62fe] focus:bg-[#353535] focus:outline-none disabled:border-[#393939] disabled:bg-[#161616] disabled:text-[#6f6f6f] {error
				? '!border-b-2 !border-[#da1e28]'
				: ''} {revealable && type === 'password' ? 'pr-10' : ''}"
			{...restProps}
		/>
		{#if revealable && type === 'password'}
			<button
				type="button"
				onclick={() => (revealed = !revealed)}
				aria-label={revealed ? 'Hide password' : 'Show password'}
				aria-pressed={revealed}
				class="absolute top-1/2 right-2 flex h-6 w-6 -translate-y-1/2 cursor-pointer items-center justify-center text-[#8d8d8d] transition-colors hover:text-white"
			>
				{#if revealed}
					<svg
						class="h-4 w-4"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						viewBox="0 0 24 24"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							d="M3 3l7.07 16.97 2.51-7.39 7.39-2.51L3 3z"
						/>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							d="M10.71 5.05A16.65 16.65 0 0112 4c7 0 11 8 11 8a18.5 18.5 0 01-2.16 3.19m-6.72-1.05a3 3 0 11-4.24-4.24"
						/>
						<path stroke-linecap="round" stroke-linejoin="round" d="M2 2l20 20" />
					</svg>
				{:else}
					<svg
						class="h-4 w-4"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						viewBox="0 0 24 24"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"
						/>
						<circle cx="12" cy="12" r="3" />
					</svg>
				{/if}
			</button>
		{/if}
	</div>
	{#if error}
		<span
			id={describedById}
			role="alert"
			class="motion-rise-in mt-0.5 font-sans text-xs text-[#ff8389]">{error}</span
		>
	{:else if capsOn && type === 'password'}
		<span class="mt-0.5 font-sans text-xs text-[#f1c21b]">Caps Lock is on</span>
	{:else if helperText}
		<span id={describedById} class="motion-rise-in mt-0.5 font-sans text-xs text-[#a8a8a8]"
			>{helperText}</span
		>
	{/if}
</div>
