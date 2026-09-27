<script lang="ts">
	import type { Snippet } from 'svelte';
	import CarbonModal from './CarbonModal.svelte';
	import CarbonButton from './CarbonButton.svelte';
	import { AlertTriangle } from '@lucide/svelte';

	interface Props {
		open?: boolean;
		title?: string;
		message?: string;
		details?: Snippet;
		confirmLabel?: string;
		cancelLabel?: string;
		danger?: boolean;
		confirming?: boolean;
		onconfirm?: () => void | Promise<void>;
		onclose?: () => void;
	}

	let {
		open = $bindable(false),
		title = 'Are you sure?',
		message = '',
		details,
		confirmLabel = 'Confirm',
		cancelLabel = 'Cancel',
		danger = false,
		confirming = false,
		onconfirm,
		onclose
	}: Props = $props();

	async function handleConfirm() {
		await onconfirm?.();
		open = false;
	}
</script>

<CarbonModal
	bind:open
	{title}
	description={message}
	hasFooter={false}
	size="sm"
	onclose={onclose}
>
	{#if details}
		<div
			class="rounded-none border border-[#393939] bg-[#161616] p-3 font-mono text-xs text-[#c6c6c6]"
		>
			{@render details()}
		</div>
	{/if}
	<div class="flex items-center gap-2">
		{#if danger}
			<AlertTriangle class="h-4 w-4 shrink-0 text-[#da1e28]" />
		{/if}
		<div class="flex flex-1 justify-end gap-2">
			<CarbonButton
				kind="secondary"
				size="md"
				class="justify-center rounded-none"
				onclick={() => (open = false)}
				disabled={confirming}
			>
				{cancelLabel}
			</CarbonButton>
			<CarbonButton
				kind={danger ? 'danger' : 'primary'}
				size="md"
				class="justify-center rounded-none"
				onclick={handleConfirm}
				disabled={confirming}
			>
				{confirming ? 'Working…' : confirmLabel}
			</CarbonButton>
		</div>
	</div>
</CarbonModal>
