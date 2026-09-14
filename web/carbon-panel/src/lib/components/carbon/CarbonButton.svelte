<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes, HTMLAnchorAttributes } from 'svelte/elements';

	type BaseProps = {
		kind?: 'primary' | 'secondary' | 'tertiary' | 'ghost' | 'danger';
		size?: 'sm' | 'md' | 'lg' | 'xl';
		iconOnly?: boolean;
		disabled?: boolean;
		children?: Snippet;
		class?: string;
	};

	type ButtonProps = BaseProps & HTMLButtonAttributes & { href?: undefined };
	type AnchorProps = BaseProps & HTMLAnchorAttributes & { href: string };
	type Props = ButtonProps | AnchorProps;

	let {
		kind = 'primary',
		size = 'md',
		iconOnly = false,
		disabled = false,
		href = undefined,
		class: className = '',
		children,
		...restProps
	}: Props = $props();

	const baseClasses = 'inline-flex items-center justify-between font-sans text-sm font-normal tracking-[0.16px] transition-colors focus:outline-none focus:ring-2 focus:ring-white focus:ring-offset-2 focus:ring-offset-[#161616] cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 select-none';

	const sizeClasses = {
		sm: 'h-8 px-3 text-xs',
		md: 'h-10 px-4 text-sm',
		lg: 'h-12 px-4 text-sm',
		xl: 'h-16 px-4 text-base'
	};

	const kindClasses = {
		primary: 'bg-[#0f62fe] text-white hover:bg-[#0353e9] active:bg-[#002d9c] border border-transparent disabled:bg-[#393939] disabled:text-[#8d8d8d]',
		secondary: 'bg-[#393939] text-white hover:bg-[#4c4c4c] active:bg-[#6f6f6f] border border-transparent disabled:bg-[#262626] disabled:text-[#8d8d8d]',
		tertiary: 'bg-transparent text-white hover:bg-[#393939] active:bg-[#525252] border border-[#f4f4f4] hover:border-transparent disabled:border-[#6f6f6f] disabled:text-[#8d8d8d]',
		ghost: 'bg-transparent text-[#0f62fe] hover:bg-[#353535] active:bg-[#525252] border border-transparent disabled:text-[#8d8d8d] disabled:hover:bg-transparent',
		danger: 'bg-[#da1e28] text-white hover:bg-[#ba1b23] active:bg-[#750e13] border border-transparent disabled:bg-[#393939] disabled:text-[#8d8d8d]'
	};
</script>

{#if href}
	<a
		{href}
		class="{baseClasses} {sizeClasses[size]} {kindClasses[kind]} {iconOnly ? '!p-2 !w-10 !h-10 !justify-center' : ''} {className}"
		{...(restProps as HTMLAnchorAttributes)}
	>
		{@render children?.()}
	</a>
{:else}
	<button
		type="button"
		{disabled}
		class="{baseClasses} {sizeClasses[size]} {kindClasses[kind]} {iconOnly ? '!p-2 !w-10 !h-10 !justify-center' : ''} {className}"
		{...(restProps as HTMLButtonAttributes)}
	>
		{@render children?.()}
	</button>
{/if}
