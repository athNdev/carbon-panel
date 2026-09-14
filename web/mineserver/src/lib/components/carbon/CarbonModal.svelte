<script lang="ts">
	import type { Snippet } from 'svelte';
	import CarbonButton from './CarbonButton.svelte';

	interface Props {
		open?: boolean;
		title?: string;
		description?: string;
		primaryButtonText?: string;
		secondaryButtonText?: string;
		danger?: boolean;
		onprimary?: () => void;
		onsecondary?: () => void;
		onclose?: () => void;
		children?: Snippet;
	}

	let {
		open = $bindable(false),
		title = '',
		description = '',
		primaryButtonText = 'Confirm',
		secondaryButtonText = 'Cancel',
		danger = false,
		onprimary,
		onsecondary,
		onclose,
		children
	}: Props = $props();

	function handleClose() {
		open = false;
		onclose?.();
	}
</script>

{#if open}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70 backdrop-blur-xs select-none">
		<div class="w-full max-w-2xl bg-[#161616] border border-[#393939] shadow-2xl flex flex-col max-h-[90vh]">
			<!-- Header -->
			<div class="p-6 border-b border-[#393939] flex items-start justify-between bg-[#262626]">
				<div>
					{#if description}
						<span class="font-sans text-xs font-normal text-[#c6c6c6]">{description}</span>
					{/if}
					<h3 class="font-sans text-xl font-semibold text-[#f4f4f4] mt-1">{title}</h3>
				</div>
				<button
					type="button"
					onclick={handleClose}
					class="text-[#c6c6c6] hover:text-white hover:bg-[#353535] p-2 transition-colors cursor-pointer"
					aria-label="Close modal"
				>
					<svg class="h-4 w-4" fill="currentColor" viewBox="0 0 20 20">
						<path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
					</svg>
				</button>
			</div>

			<!-- Body -->
			<div class="p-6 overflow-y-auto flex-1 font-sans text-sm text-[#f4f4f4] space-y-4">
				{@render children?.()}
			</div>

			<!-- Footer -->
			<div class="flex items-center justify-end border-t border-[#393939] bg-[#262626]">
				<CarbonButton
					kind="secondary"
					size="lg"
					class="w-1/2 justify-center"
					onclick={() => {
						onsecondary ? onsecondary() : handleClose();
					}}
				>
					{secondaryButtonText}
				</CarbonButton>
				<CarbonButton
					kind={danger ? 'danger' : 'primary'}
					size="lg"
					class="w-1/2 justify-center"
					onclick={onprimary}
				>
					{primaryButtonText}
				</CarbonButton>
			</div>
		</div>
	</div>
{/if}
