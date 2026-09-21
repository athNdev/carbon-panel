<script lang="ts">
	import { tick } from 'svelte';

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

	let container = $state<HTMLDivElement | null>(null);
	let indicatorLeft = $state(0);
	let indicatorWidth = $state(0);

	function measure() {
		if (!container) return;
		const buttons = container.querySelectorAll<HTMLButtonElement>('[data-tab]');
		const index = tabs.findIndex((tab) => tab.id === selectedTab);
		const button = buttons[index >= 0 ? index : 0];
		if (!button) {
			indicatorWidth = 0;
			return;
		}
		indicatorLeft = button.offsetLeft;
		indicatorWidth = button.offsetWidth;
	}

	$effect(() => {
		tabs;
		selectedTab;
		tick().then(() => requestAnimationFrame(measure));
	});

	$effect(() => {
		const onResize = () => measure();
		window.addEventListener('resize', onResize);
		return () => window.removeEventListener('resize', onResize);
	});

	function handleTabClick(id: string) {
		selectedTab = id;
		onselect?.(id);
	}
</script>

<div
	bind:this={container}
	role="tablist"
	class="relative flex items-center border-b border-[#393939] bg-[#161616] font-sans {className}"
>
	{#each tabs as tab}
		<button
			type="button"
			role="tab"
			aria-selected={selectedTab === tab.id}
			data-tab
			data-testid="tab-{tab.id}"
			onclick={() => handleTabClick(tab.id)}
			class="relative z-10 flex h-10 cursor-pointer items-center gap-2 border-b-2 border-transparent px-4 text-sm font-normal transition-colors select-none {selectedTab ===
			tab.id
				? 'bg-[#262626] font-semibold text-[#f4f4f4]'
				: 'text-[#a8a8a8] hover:bg-[#262626] hover:text-[#f4f4f4]'}"
		>
			<span>{tab.label}</span>
			{#if tab.badge !== undefined}
				<span class="bg-[#393939] px-1.5 py-0.5 font-mono text-[10px] text-[#c6c6c6]">
					{tab.badge}
				</span>
			{/if}
		</button>
	{/each}
	{#if indicatorWidth > 0}
		<div
			class="pointer-events-none absolute bottom-[-1px] h-0.5 bg-[#0f62fe] transition-[transform,width] duration-[var(--motion-fast)] ease-[var(--ease-in-out-standard)]"
			style="width: {indicatorWidth}px; transform: translateX({indicatorLeft}px)"
		></div>
	{/if}
</div>
