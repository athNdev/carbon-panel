<script lang="ts">
	import { FilePlus, FolderPlus, Upload, RefreshCw, Search, X } from '@lucide/svelte';

	interface Props {
		filterText: string;
		onRefresh: () => void;
		onNewFile: () => void;
		onNewFolder: () => void;
		onUpload: () => void;
		onFilterChange: (value: string) => void;
	}

	let { filterText, onRefresh, onNewFile, onNewFolder, onUpload, onFilterChange }: Props = $props();

	let showSearch = $state(false);
</script>

<div class="flex items-center justify-between border-b border-[#393939] bg-[#262626] px-3 py-1.5 font-sans">
	<div class="flex items-center gap-1">
		<button
			type="button"
			class="h-7 px-2 flex items-center gap-1 text-xs text-[#c6c6c6] hover:text-white hover:bg-[#353535] transition-colors rounded-none cursor-pointer"
			onclick={onNewFile}
			title="New File"
			aria-label="New File"
		>
			<FilePlus class="h-3.5 w-3.5" />
			<span class="hidden sm:inline">New File</span>
		</button>
		<button
			type="button"
			class="h-7 px-2 flex items-center gap-1 text-xs text-[#c6c6c6] hover:text-white hover:bg-[#353535] transition-colors rounded-none cursor-pointer"
			onclick={onNewFolder}
			title="New Folder"
			aria-label="New Folder"
		>
			<FolderPlus class="h-3.5 w-3.5" />
			<span class="hidden sm:inline">New Folder</span>
		</button>
		<button
			type="button"
			class="h-7 px-2 flex items-center gap-1 text-xs text-[#c6c6c6] hover:text-white hover:bg-[#353535] transition-colors rounded-none cursor-pointer"
			onclick={onUpload}
			title="Upload Files"
			aria-label="Upload Files"
		>
			<Upload class="h-3.5 w-3.5" />
			<span class="hidden sm:inline">Upload</span>
		</button>
	</div>

	<div class="flex items-center gap-1">
		{#if showSearch}
			<div class="flex items-center gap-1">
				<input
					class="h-7 w-48 px-2 bg-[#161616] border border-[#525252] focus:border-[#0f62fe] focus:outline-none text-xs text-[#f4f4f4] placeholder-[#6f6f6f] rounded-none font-sans"
					placeholder="Search files..."
					value={filterText}
					oninput={(e) => onFilterChange((e.target as HTMLInputElement).value)}
					autofocus
				/>
				<button
					type="button"
					title="Close search"
					aria-label="Close search"
					class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none transition-colors cursor-pointer"
					onclick={() => {
						showSearch = false;
						onFilterChange('');
					}}
				>
					<X class="h-3.5 w-3.5" />
				</button>
			</div>
		{:else}
			<button
				type="button"
				class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none transition-colors cursor-pointer"
				onclick={() => (showSearch = true)}
				title="Filter / Search"
				aria-label="Filter / Search"
			>
				<Search class="h-3.5 w-3.5" />
			</button>
		{/if}
		<button
			type="button"
			class="h-7 w-7 flex items-center justify-center text-[#c6c6c6] hover:text-white hover:bg-[#353535] rounded-none transition-colors cursor-pointer"
			onclick={onRefresh}
			title="Refresh tree"
			aria-label="Refresh tree"
		>
			<RefreshCw class="h-3.5 w-3.5" />
		</button>
	</div>
</div>
