<script lang="ts" module>
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLAnchorAttributes, HTMLButtonAttributes } from 'svelte/elements';
	import { type VariantProps, tv } from 'tailwind-variants';

	export const buttonVariants = tv({
		base: "focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[#0f62fe] inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-none text-sm font-normal outline-none transition-colors disabled:pointer-events-none disabled:opacity-40 disabled:bg-[#393939] disabled:text-[#8d8d8d] aria-disabled:pointer-events-none aria-disabled:opacity-40 [&_svg:not([class*='size-'])]:size-4 [&_svg]:pointer-events-none [&_svg]:shrink-0 cursor-pointer",
		variants: {
			variant: {
				default:
					'bg-[#0f62fe] text-white hover:bg-[#0353e9] active:bg-[#002d9c] border-none shadow-none',
				destructive:
					'bg-[#da1e28] text-white hover:bg-[#ba1b23] active:bg-[#750e13] border-none shadow-none',
				outline:
					'bg-transparent border border-[#0f62fe] text-[#78a9ff] hover:bg-[#0f62fe] hover:text-white shadow-none',
				secondary:
					'bg-[#393939] text-white hover:bg-[#4c4c4c] active:bg-[#6f6f6f] border-none shadow-none',
				ghost:
					'bg-transparent text-[#78a9ff] hover:bg-[#353535] hover:text-white border-none shadow-none',
				link: 'text-[#78a9ff] underline-offset-4 hover:underline shadow-none'
			},
			size: {
				default: 'h-10 px-4 py-2 has-[>svg]:px-3',
				sm: 'h-8 gap-1.5 px-3 has-[>svg]:px-2 text-xs',
				lg: 'h-12 px-6 has-[>svg]:px-4 text-base',
				icon: 'size-10'
			}
		},
		defaultVariants: {
			variant: 'default',
			size: 'default'
		}
	});

	export type ButtonVariant = VariantProps<typeof buttonVariants>['variant'];
	export type ButtonSize = VariantProps<typeof buttonVariants>['size'];

	export type ButtonProps = WithElementRef<HTMLButtonAttributes> &
		WithElementRef<HTMLAnchorAttributes> & {
			variant?: ButtonVariant;
			size?: ButtonSize;
		};
</script>

<script lang="ts">
	let {
		class: className,
		variant = 'default',
		size = 'default',
		ref = $bindable(null),
		href = undefined,
		type = 'button',
		disabled,
		children,
		...restProps
	}: ButtonProps = $props();
</script>

{#if href}
	<!-- eslint-disable svelte/no-navigation-without-resolve -- generic component; callers must resolve -->
	<a
		bind:this={ref}
		data-slot="button"
		class={cn(buttonVariants({ variant, size }), className)}
		href={disabled ? undefined : href}
		aria-disabled={disabled}
		role={disabled ? 'link' : undefined}
		tabindex={disabled ? -1 : undefined}
		{...restProps}
	>
		{@render children?.()}
	</a>
	<!-- eslint-enable svelte/no-navigation-without-resolve -->
{:else}
	<button
		bind:this={ref}
		data-slot="button"
		class={cn(buttonVariants({ variant, size }), className)}
		{type}
		{disabled}
		{...restProps}
	>
		{@render children?.()}
	</button>
{/if}
