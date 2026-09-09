<script lang="ts">
	interface TabItem {
		id: string;
		label: string;
		badge?: string | number;
	}

	interface Props {
		tabs: TabItem[];
		selectedTab?: string;
		class?: string;
		onselect?: (id: string) => void;
	}

	let {
		tabs = [],
		selectedTab = $bindable(tabs[0]?.id || ''),
		class: className = '',
		onselect
	}: Props = $props();

	function handleTabClick(id: string) {
		selectedTab = id;
		onselect?.(id);
	}
</script>

<div class="flex items-center border-b border-[#393939] bg-[#161616] font-sans {className}">
	{#each tabs as tab}
		<button
			type="button"
			onclick={() => handleTabClick(tab.id)}
			class="h-10 px-4 flex items-center gap-2 text-sm font-normal transition-colors border-b-2 cursor-pointer select-none {selectedTab === tab.id ? 'border-[#0f62fe] text-[#f4f4f4] bg-[#262626] font-semibold' : 'border-transparent text-[#a8a8a8] hover:text-[#f4f4f4] hover:bg-[#262626]'}"
		>
			<span>{tab.label}</span>
			{#if tab.badge !== undefined}
				<span class="text-[10px] font-mono px-1.5 py-0.5 bg-[#393939] text-[#c6c6c6]">
					{tab.badge}
				</span>
			{/if}
		</button>
	{/each}
</div>
