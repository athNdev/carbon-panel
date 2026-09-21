<script lang="ts">
	import type { Snippet } from 'svelte';
	import { fade, scale } from 'svelte/transition';
	import { cubicOut, cubicIn } from 'svelte/easing';
	import CarbonButton from './CarbonButton.svelte';

	interface Props {
		open?: boolean;
		title?: string;
		description?: string;
		primaryButtonText?: string;
		secondaryButtonText?: string;
		danger?: boolean;
		size?: 'sm' | 'md' | 'lg' | 'xl' | '2xl' | '3xl' | '4xl' | '5xl' | 'full';
		hasFooter?: boolean;
		onprimary?: () => void;
		onsecondary?: () => void;
		onclose?: () => void;
		footer?: Snippet;
		children?: Snippet;
	}

	let {
		open = $bindable(false),
		title = '',
		description = '',
		primaryButtonText = 'Confirm',
		secondaryButtonText = 'Cancel',
		danger = false,
		size = '2xl',
		hasFooter = true,
		onprimary,
		onsecondary,
		onclose,
		footer,
		children
	}: Props = $props();

	const sizeClasses = {
		sm: 'max-w-sm',
		md: 'max-w-md',
		lg: 'max-w-lg',
		xl: 'max-w-xl',
		'2xl': 'max-w-2xl',
		'3xl': 'max-w-3xl',
		'4xl': 'max-w-4xl',
		'5xl': 'max-w-5xl',
		full: 'max-w-[95vw] w-[95vw] h-[90vh]'
	};

	let modalElement = $state<HTMLDivElement | null>(null);
	let previousActiveElement: HTMLElement | null = null;
	const titleId = `carbon-modal-title-${Math.random().toString(36).slice(2, 9)}`;
	const descId = `carbon-modal-desc-${Math.random().toString(36).slice(2, 9)}`;

	function handleClose() {
		open = false;
		onclose?.();
	}

	function handleBackdropClick(event: MouseEvent) {
		if (event.target === event.currentTarget) {
			handleClose();
		}
	}

	function handleKeydown(event: KeyboardEvent) {
		if (!open) return;

		if (event.key === 'Escape') {
			event.preventDefault();
			event.stopPropagation();
			handleClose();
			return;
		}

		if (event.key === 'Tab' && modalElement) {
			const focusableSelectors = [
				'a[href]',
				'button:not([disabled])',
				'input:not([disabled])',
				'select:not([disabled])',
				'textarea:not([disabled])',
				'[tabindex]:not([tabindex="-1"])'
			].join(', ');

			const focusable = Array.from(
				modalElement.querySelectorAll<HTMLElement>(focusableSelectors)
			).filter((el) => el.offsetParent !== null || el.offsetWidth > 0 || el.offsetHeight > 0);

			if (focusable.length === 0) {
				event.preventDefault();
				return;
			}

			const first = focusable[0];
			const last = focusable[focusable.length - 1];

			if (event.shiftKey) {
				if (document.activeElement === first || !modalElement.contains(document.activeElement)) {
					last.focus();
					event.preventDefault();
				}
			} else {
				if (document.activeElement === last || !modalElement.contains(document.activeElement)) {
					first.focus();
					event.preventDefault();
				}
			}
		}
	}

	$effect(() => {
		if (open) {
			if (typeof document !== 'undefined') {
				previousActiveElement = document.activeElement as HTMLElement | null;
				// Focus the modal or its first actionable element on open
				setTimeout(() => {
					if (!modalElement) return;
					const focusableSelectors = [
						'input:not([disabled])',
						'button:not([disabled])',
						'select:not([disabled])',
						'textarea:not([disabled])',
						'a[href]',
						'[tabindex]:not([tabindex="-1"])'
					].join(', ');
					const firstFocusable = modalElement.querySelector<HTMLElement>(focusableSelectors);
					if (firstFocusable) {
						firstFocusable.focus();
					} else {
						modalElement.focus();
					}
				}, 50);
			}
		} else {
			if (previousActiveElement && typeof previousActiveElement.focus === 'function') {
				previousActiveElement.focus();
				previousActiveElement = null;
			}
		}
	});
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 z-50 flex items-center justify-center rounded-none bg-black/70 p-4 backdrop-blur-xs select-none"
		transition:fade={{ duration: 150, easing: cubicOut }}
		onclick={handleBackdropClick}
	>
		<div
			bind:this={modalElement}
			role="dialog"
			aria-modal="true"
			aria-labelledby={title ? titleId : undefined}
			aria-describedby={description ? descId : undefined}
			tabindex="-1"
			class="w-full {sizeClasses[
				size
			]} flex max-h-[90vh] flex-col rounded-none border border-[#393939] bg-[#161616] shadow-2xl outline-none focus:outline-none"
			in:scale={{ start: 0.97, opacity: 0, duration: 200, easing: cubicOut }}
			out:scale={{ start: 0.97, opacity: 0, duration: 150, easing: cubicIn }}
		>
			<!-- Header -->
			<div
				class="flex items-start justify-between rounded-none border-b border-[#393939] bg-[#262626] p-6"
			>
				<div>
					{#if description}
						<span id={descId} class="font-sans text-xs font-normal text-[#c6c6c6]"
							>{description}</span
						>
					{/if}
					<h3 id={titleId} class="mt-1 font-sans text-xl font-semibold text-[#f4f4f4]">{title}</h3>
				</div>
				<button
					type="button"
					onclick={handleClose}
					class="cursor-pointer rounded-none p-2 text-[#c6c6c6] transition-colors hover:bg-[#353535] hover:text-white focus:ring-1 focus:ring-[#0f62fe] focus:outline-none"
					aria-label="Close modal"
				>
					<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
						<path
							fill-rule="evenodd"
							d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z"
							clip-rule="evenodd"
						/>
					</svg>
				</button>
			</div>

			<!-- Body -->
			<div
				class="flex-1 space-y-4 overflow-y-auto rounded-none p-6 font-sans text-sm text-[#f4f4f4]"
			>
				{@render children?.()}
			</div>

			<!-- Footer -->
			{#if footer}
				<div class="rounded-none border-t border-[#393939] bg-[#262626]">
					{@render footer()}
				</div>
			{:else if hasFooter}
				<div
					class="flex items-center justify-end rounded-none border-t border-[#393939] bg-[#262626]"
				>
					<CarbonButton
						kind="secondary"
						size="lg"
						class="w-1/2 justify-center rounded-none"
						onclick={() => {
							onsecondary ? onsecondary() : handleClose();
						}}
					>
						{secondaryButtonText}
					</CarbonButton>
					<CarbonButton
						kind={danger ? 'danger' : 'primary'}
						size="lg"
						class="w-1/2 justify-center rounded-none"
						onclick={onprimary}
					>
						{primaryButtonText}
					</CarbonButton>
				</div>
			{/if}
		</div>
	</div>
{/if}
