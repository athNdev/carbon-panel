<script lang="ts">
	import { cls } from '$lib/utils';

	export type Tab = { id: string; label: string };

	let {
		tabs,
		value = undefined,
		onchange = undefined
	}: {
		tabs: Tab[];
		value?: string;
		onchange?: (id: string) => void;
	} = $props();

	function select(id: string) {
		value = id;
		onchange?.(id);
	}
</script>

<div class="flex border-b border-border" role="tablist" aria-label="Tabs">
	{#each tabs as tab (tab.id)}
		<button
			class={cls(
				'border-b-2 px-4 py-2 text-sm font-medium transition-colors focus-ring',
				value === tab.id
					? 'border-primary bg-card text-foreground'
					: 'border-transparent text-muted-foreground hover:bg-accent hover:text-foreground'
			)}
			role="tab"
			aria-selected={value === tab.id}
			aria-controls={`tabpanel-${tab.id}`}
			onclick={() => select(tab.id)}
		>
			{tab.label}
		</button>
	{/each}
</div>