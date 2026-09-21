<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cls } from '$lib/utils';

	type Variant = 'primary' | 'secondary' | 'danger' | 'ghost';
	type Size = 'sm' | 'md';

	let {
		variant = 'primary',
		size = 'md',
		disabled = false,
		loading = false,
		type = 'button',
		href = undefined,
		onclick = undefined,
		class: className = '',
		children
	}: {
		variant?: Variant;
		size?: Size;
		disabled?: boolean;
		loading?: boolean;
		type?: 'button' | 'submit';
		href?: string;
		onclick?: (e: MouseEvent) => void;
		class?: string;
		children: Snippet;
	} = $props();

	const base =
		'inline-flex items-center justify-center gap-2 border font-medium transition-colors focus-ring disabled:pointer-events-none disabled:opacity-50';

	const variants: Record<Variant, string> = {
		primary: 'border-primary bg-primary text-primary-foreground hover:bg-primary/90',
		secondary: 'border-border bg-secondary text-secondary-foreground hover:bg-accent',
		danger: 'border-destructive bg-destructive text-white hover:bg-destructive/90',
		ghost: 'border-transparent bg-transparent text-foreground hover:bg-accent'
	};

	const sizes: Record<Size, string> = {
		sm: 'h-7 px-3 text-xs',
		md: 'h-9 px-4 text-sm'
	};

	const btnClass = $derived(cls(base, variants[variant], sizes[size], className));

	function handle(e: MouseEvent) {
		if (disabled || loading) return;
		onclick?.(e);
	}
</script>

{#if href}
	<a href={href} class={btnClass} aria-disabled={disabled || loading} onclick={handle}>
		{#if loading}<span class="animate-pulse" aria-hidden="true">…</span>{/if}
		{@render children()}
	</a>
{:else}
	<button
		class={btnClass}
		type={type}
		disabled={disabled || loading}
		onclick={handle}
		aria-busy={loading}
	>
		{#if loading}<span class="animate-pulse" aria-hidden="true">…</span>{/if}
		{@render children()}
	</button>
{/if}