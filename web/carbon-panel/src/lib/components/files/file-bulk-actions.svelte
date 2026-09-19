<script lang="ts">
	import { Download, Trash2, FolderInput, Archive, Package, X } from '@lucide/svelte';

	interface Props {
		selectedCount: number;
		canExtract: boolean;
		onClear: () => void;
		onDelete: () => void;
		onDownload: () => void;
		onMove: () => void;
		onCompress: () => void;
		onExtract: () => void;
	}

	let {
		selectedCount,
		canExtract,
		onClear,
		onDelete,
		onDownload,
		onMove,
		onCompress,
		onExtract
	}: Props = $props();

	let active = $derived(selectedCount > 0);
</script>

<!-- Always rendered at fixed height to prevent layout shift -->
<div
	class="flex h-[36px] items-center justify-between border-b border-[#393939] px-3 font-sans transition-colors rounded-none {active
		? 'bg-[#262626] text-[#f4f4f4]'
		: 'bg-[#161616] text-[#8d8d8d]'}"
>
	{#if active}
		<div class="flex items-center gap-2">
			<span class="text-xs font-mono font-medium text-[#78a9ff]">{selectedCount} selected</span>
			<button
				type="button"
				class="h-6 px-2 text-xs bg-[#393939] hover:bg-[#4c4c4c] text-white flex items-center gap-1 rounded-none cursor-pointer"
				onclick={onClear}
			>
				<X class="h-3 w-3" />
				<span>Clear</span>
			</button>
		</div>
		<div class="flex items-center gap-1">
			<button
				type="button"
				class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none cursor-pointer"
				onclick={onDownload}
				title="Download selected"
				aria-label="Download selected"
			>
				<Download class="h-3.5 w-3.5" />
			</button>
			<button
				type="button"
				class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none cursor-pointer"
				onclick={onMove}
				title="Move selected"
				aria-label="Move selected"
			>
				<FolderInput class="h-3.5 w-3.5" />
			</button>
			<button
				type="button"
				class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none cursor-pointer"
				onclick={onCompress}
				title="Compress selected"
				aria-label="Compress selected"
			>
				<Archive class="h-3.5 w-3.5" />
			</button>
			{#if canExtract}
				<button
					type="button"
					class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none cursor-pointer"
					onclick={onExtract}
					title="Extract archive"
					aria-label="Extract archive"
				>
					<Package class="h-3.5 w-3.5" />
				</button>
			{/if}
			<button
				type="button"
				class="h-7 w-7 flex items-center justify-center text-[#ff8389] hover:bg-[#da1e28]/20 rounded-none cursor-pointer"
				onclick={onDelete}
				title="Delete selected"
				aria-label="Delete selected"
			>
				<Trash2 class="h-3.5 w-3.5" />
			</button>
		</div>
	{:else}
		<span class="text-[11px] text-[#6f6f6f] font-mono"
			>Ctrl+Click to select • Right-click for file actions</span
		>
	{/if}
</div>
