<script lang="ts">
	import {
		Breadcrumb,
		BreadcrumbItem,
		BreadcrumbLink,
		BreadcrumbList,
		BreadcrumbPage,
		BreadcrumbSeparator
	} from '$lib/components/ui/breadcrumb';
	import { FolderRoot } from '@lucide/svelte';

	interface Props {
		currentPath: string;
		onNavigate: (path: string) => void;
	}

	let { currentPath, onNavigate }: Props = $props();

	let segments = $derived.by(() => {
		if (!currentPath) return [];
		return currentPath.split('/').filter(Boolean);
	});
</script>

<div class="flex items-center border-b border-[#393939] bg-[#161616] px-3 py-1.5 text-xs font-mono text-[#a8a8a8]">
	<Breadcrumb>
		<BreadcrumbList>
			<BreadcrumbItem>
				{#if segments.length === 0}
					<BreadcrumbPage class="flex items-center gap-1.5 text-xs text-[#f4f4f4] font-medium">
						<FolderRoot class="h-3.5 w-3.5 text-[#0f62fe]" />
						<span>/ (root)</span>
					</BreadcrumbPage>
				{:else}
					<BreadcrumbLink
						class="flex cursor-pointer items-center gap-1.5 text-xs text-[#c6c6c6] hover:text-[#78a9ff] transition-colors"
						onclick={() => onNavigate('')}
					>
						<FolderRoot class="h-3.5 w-3.5 text-[#0f62fe]" />
						<span>/ (root)</span>
					</BreadcrumbLink>
				{/if}
			</BreadcrumbItem>
			{#each segments as segment, i (i)}
				<BreadcrumbSeparator class="text-[#6f6f6f]" />
				<BreadcrumbItem>
					{#if i === segments.length - 1}
						<BreadcrumbPage class="text-xs text-[#f4f4f4] font-medium">{segment}</BreadcrumbPage>
					{:else}
						<BreadcrumbLink
							class="cursor-pointer text-xs text-[#c6c6c6] hover:text-[#78a9ff] transition-colors"
							onclick={() => onNavigate(segments.slice(0, i + 1).join('/'))}
						>
							{segment}
						</BreadcrumbLink>
					{/if}
				</BreadcrumbItem>
			{/each}
		</BreadcrumbList>
	</Breadcrumb>
</div>
