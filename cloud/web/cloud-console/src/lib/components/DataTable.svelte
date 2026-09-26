<script lang="ts">
	import { cls } from '$lib/utils';

	export type TableColumn = {
		id: string;
		label: string;
		sortable?: boolean;
		align?: 'left' | 'right';
		width?: string;
	};

	export type TableRow = Record<string, unknown>;

	let {
		columns,
		rows,
		loading = false,
		rowHref = undefined,
		keyFor = undefined,
		emptyTitle = 'No items',
		emptyBody = 'There is nothing here yet.',
		cell = undefined,
		label = 'Data table'
	}: {
		columns: TableColumn[];
		rows: TableRow[];
		loading?: boolean;
		rowHref?: (row: TableRow) => string;
		keyFor?: (row: TableRow, index: number) => unknown;
		emptyTitle?: string;
		emptyBody?: string;
		cell?: import('svelte').Snippet<[TableRow, TableColumn]>;
		label?: string;
	} = $props();

	let sortColumn = $state<string | null>(null);
	let sortDir = $state<'asc' | 'desc'>('asc');

	function sortable(id: string): boolean {
		return columns.find((c) => c.id === id)?.sortable ?? false;
	}

	function toggleSort(id: string) {
		if (!sortable(id)) return;
		if (sortColumn === id) {
			sortDir = sortDir === 'asc' ? 'desc' : 'asc';
		} else {
			sortColumn = id;
			sortDir = 'asc';
		}
	}

	const sorted = $derived.by(() => {
		if (!sortColumn) return rows;
		const col = sortColumn;
		const dir = sortDir === 'asc' ? 1 : -1;
		return [...rows].sort((a, b) => {
			const av = a[col];
			const bv = b[col];
			if (av == null && bv == null) return 0;
			if (av == null) return 1;
			if (bv == null) return -1;
			if (typeof av === 'number' && typeof bv === 'number') return (av - bv) * dir;
			const as = typeof av === 'string' ? av : String(av);
			const bs = typeof bv === 'string' ? bv : String(bv);
			return as.localeCompare(bs) * dir;
		});
	});

	function arrowFor(id: string): string {
		if (sortColumn !== id) return '';
		return sortDir === 'asc' ? ' ↑' : ' ↓';
	}

	function keyForRow(row: TableRow, index: number): unknown {
		return keyFor ? keyFor(row, index) : index;
	}

	function openRow(row: TableRow) {
		if (!rowHref) return;
		const href = rowHref(row);
		if (href) window.location.href = href;
	}
</script>

<div class="overflow-x-auto border border-border bg-card">
	<table class="w-full border-collapse text-sm" aria-label={label}>
		<thead>
			<tr class="border-b border-border bg-muted/40">
				{#each columns as col (col.id)}
					<th
						class="px-3 py-2 text-left text-xs font-semibold tracking-wide text-muted-foreground uppercase"
						class:sortable={sortable(col.id)}
						aria-sort={sortColumn === col.id ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}
						style={col.width ? `width:${col.width}` : ''}
					>
						{#if sortable(col.id)}
							<button
								class="inline-flex items-center gap-1 text-left font-semibold text-muted-foreground hover:text-foreground focus-ring uppercase"
								onclick={() => toggleSort(col.id)}
							>
								{col.label}{arrowFor(col.id)}
							</button>
						{:else}
							{col.label}
						{/if}
					</th>
				{/each}
			</tr>
		</thead>
		<tbody>
			{#if loading}
				{#each Array(4) as _, i (i)}
					<tr class="border-b border-border/50">
						{#each columns as col (col.id)}
							<td class="px-3 py-2.5">
								<span class="block h-3 w-24 animate-pulse bg-muted"></span>
							</td>
						{/each}
					</tr>
				{/each}
			{:else if sorted.length === 0}
				<tr>
					<td class="px-3 py-10 text-center" colspan={columns.length}>
						<p class="text-sm font-medium text-foreground">{emptyTitle}</p>
						<p class="mt-1 text-xs text-muted-foreground">{emptyBody}</p>
					</td>
				</tr>
			{:else}
				{#each sorted as row, i (keyForRow(row, i))}
					<tr
						class="border-b border-border/50 last:border-b-0 hover:bg-accent/40"
						class:cursor-pointer={!!rowHref}
						class:focus-ring={!!rowHref}
						tabindex={rowHref ? 0 : -1}
						role={rowHref ? 'link' : undefined}
						onclick={() => openRow(row)}
						onkeydown={(e) => {
							if (rowHref && (e.key === 'Enter' || e.key === ' ')) {
								e.preventDefault();
								openRow(row);
							}
						}}
					>
						{#each columns as col (col.id)}
							<td
								class="px-3 py-2.5 align-middle text-foreground"
								class:text-right={col.align === 'right'}
							>
								{#if cell}
								{@render cell(row, col)}
							{:else}
								{String(row[col.id] ?? '')}
							{/if}
							</td>
						{/each}
					</tr>
				{/each}
			{/if}
		</tbody>
	</table>
</div>