<script lang="ts">
	interface Props {
		description?: string;
		status?: 'active' | 'finished' | 'error';
		class?: string;
	}

	let {
		description = 'Loading...',
		status = 'active',
		class: className = ''
	}: Props = $props();
</script>

<div class="inline-flex items-center gap-2 font-sans text-xs text-[#c6c6c6] {className}">
	{#if status === 'active'}
		<div class="h-4 w-4 border-2 border-[#0f62fe] border-t-transparent animate-spin"></div>
	{:else if status === 'finished'}
		<svg class="h-4 w-4 text-[#24a148]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path stroke-linecap="square" stroke-linejoin="miter" stroke-width="2" d="M5 13l4 4L19 7" />
		</svg>
	{:else}
		<svg class="h-4 w-4 text-[#da1e28]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path stroke-linecap="square" stroke-linejoin="miter" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
		</svg>
	{/if}
	<span>{description}</span>
</div>
