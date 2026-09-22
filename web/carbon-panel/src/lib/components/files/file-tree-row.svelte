<script lang="ts">
	import { Checkbox } from '$lib/components/ui/checkbox';
	import {
		Folder,
		FolderOpen,
		File,
		FileText,
		FileCode,
		Image,
		Archive,
		ChevronRight,
		ChevronDown
	} from '@lucide/svelte';
	import type { FileInfo } from '$lib/proto/carbonpanel/v1/file_pb';
	import { formatBytes } from '$lib/utils';

	interface Props {
		file: FileInfo;
		depth: number;
		isExpanded: boolean;
		isSelected: boolean;
		isFocused: boolean;
		isDragOver: boolean;
		hasSelection: boolean;
		onToggleExpand: (path: string) => void;
		onSelect: (file: FileInfo, event: MouseEvent) => void;
		onCheckboxToggle: (file: FileInfo) => void;
		onContextMenu: (file: FileInfo, event: MouseEvent) => void;
		onDragStart: (file: FileInfo, event: DragEvent) => void;
		onDragOver: (file: FileInfo, event: DragEvent) => void;
		onDragLeave: () => void;
		onDrop: (file: FileInfo, event: DragEvent) => void;
	}

	let {
		file,
		depth,
		isExpanded,
		isSelected,
		isFocused,
		isDragOver,
		hasSelection,
		onToggleExpand,
		onSelect,
		onCheckboxToggle,
		onContextMenu,
		onDragStart,
		onDragOver,
		onDragLeave,
		onDrop
	}: Props = $props();

	function getFileIcon(f: FileInfo) {
		if (f.isDir) return isExpanded ? FolderOpen : Folder;
		const ext = f.name.toLowerCase().split('.').pop() || '';
		const textExts = [
			'txt',
			'md',
			'json',
			'yml',
			'yaml',
			'toml',
			'properties',
			'conf',
			'cfg',
			'log'
		];
		const codeExts = [
			'js',
			'ts',
			'jsx',
			'tsx',
			'py',
			'java',
			'cpp',
			'c',
			'h',
			'cs',
			'go',
			'rs',
			'php',
			'rb',
			'lua'
		];
		const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'svg', 'webp'];
		const archiveExts = [
			'zip',
			'tar',
			'gz',
			'tgz',
			'rar',
			'7z',
			'bz2',
			'xz',
			'lz',
			'zst',
			'tbz2',
			'txz'
		];
		if (textExts.includes(ext)) return FileText;
		if (codeExts.includes(ext)) return FileCode;
		if (imageExts.includes(ext)) return Image;
		if (archiveExts.includes(ext)) return Archive;
		return File;
	}

	function formatModified(timestamp: number | bigint): string {
		const ts = Number(timestamp);
		if (!ts) return '';
		const diff = Date.now() - ts * 1000;
		const mins = Math.floor(diff / 60000);
		if (mins < 1) return 'just now';
		if (mins < 60) return `${mins}m ago`;
		const hours = Math.floor(mins / 60);
		if (hours < 24) return `${hours}h ago`;
		const days = Math.floor(hours / 24);
		if (days < 30) return `${days}d ago`;
		return new Date(ts * 1000).toLocaleDateString();
	}

	let Icon = $derived(getFileIcon(file));
	let showCheckbox = $derived(hasSelection || isSelected);
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="file-row group flex h-[30px] cursor-pointer items-center rounded-none border-b border-[#222222] pr-3 text-xs transition-colors select-none
		{isSelected
		? 'border-l-2 border-l-[#0f62fe] bg-[#262626]'
		: isFocused
			? 'bg-[#2a2a2a]'
			: 'bg-[#161616]'}
		{isDragOver && file.isDir ? 'bg-[#0f62fe]/20 ring-1 ring-[#0f62fe]' : ''}
		hover:bg-[#353535]"
	draggable="true"
	onclick={(e) => onSelect(file, e)}
	oncontextmenu={(e) => {
		e.preventDefault();
		onContextMenu(file, e);
	}}
	ondragstart={(e) => onDragStart(file, e)}
	ondragover={(e) => onDragOver(file, e)}
	ondragleave={() => onDragLeave()}
	ondrop={(e) => onDrop(file, e)}
	role="treeitem"
	aria-selected={isSelected}
	aria-expanded={file.isDir ? isExpanded : undefined}
>
	<!-- Checkbox -->
	<div
		class="flex w-6 shrink-0 items-center justify-center {showCheckbox
			? 'visible'
			: 'invisible group-hover:visible'}"
		onclick={(e) => e.stopPropagation()}
	>
		<Checkbox
			checked={isSelected}
			onCheckedChange={() => onCheckboxToggle(file)}
			class="h-3.5 w-3.5 rounded-none"
		/>
	</div>

	<!-- Indent + Chevron -->
	<div class="flex shrink-0 items-center" style="width: {depth * 16}px"></div>
	<div class="flex w-4 shrink-0 items-center justify-center">
		{#if file.isDir}
			<button
				class="cursor-pointer rounded-none p-0 text-[#8d8d8d] hover:text-[#f4f4f4]"
				onclick={(e) => {
					e.stopPropagation();
					onToggleExpand(file.path);
				}}
			>
				{#if isExpanded}
					<ChevronDown class="h-3.5 w-3.5" />
				{:else}
					<ChevronRight class="h-3.5 w-3.5" />
				{/if}
			</button>
		{/if}
	</div>

	<!-- Icon + Name -->
	<div class="flex min-w-0 flex-1 items-center gap-1.5 pl-1 font-mono">
		<Icon class="h-4 w-4 shrink-0 {file.isDir ? 'text-[#78a9ff]' : 'text-[#8d8d8d]'}" />
		<span class="truncate text-[#f4f4f4]"
			>{file.name}{#if file.isDir}/{/if}</span
		>
	</div>

	<!-- Size (right-aligned) -->
	<span class="w-20 shrink-0 text-right font-mono text-[11px] text-[#8d8d8d] tabular-nums">
		{#if !file.isDir}
			{formatBytes(Number(file.size))}
		{/if}
	</span>

	<!-- Modified (right-aligned) -->
	<span
		class="hidden w-24 shrink-0 text-right font-mono text-[11px] text-[#8d8d8d] sm:inline-block"
	>
		{formatModified(file.modified)}
	</span>
</div>
