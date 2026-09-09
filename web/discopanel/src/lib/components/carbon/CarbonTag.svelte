<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		type?: 'blue' | 'green' | 'red' | 'purple' | 'cyan' | 'teal' | 'magenta' | 'gray' | 'warm-gray' | 'cool-gray';
		size?: 'sm' | 'md';
		filter?: boolean;
		onclose?: () => void;
		class?: string;
		children?: Snippet;
	}

	let {
		type = 'gray',
		size = 'md',
		filter = false,
		onclose,
		class: className = '',
		children
	}: Props = $props();

	const typeClasses = {
		blue: 'bg-[#0043ce]/30 text-[#78a9ff] border border-[#0043ce]/60',
		green: 'bg-[#198038]/30 text-[#6fdc8c] border border-[#198038]/60',
		red: 'bg-[#da1e28]/30 text-[#ff8389] border border-[#da1e28]/60',
		purple: 'bg-[#8a3ffc]/30 text-[#d4bbff] border border-[#8a3ffc]/60',
		cyan: 'bg-[#00539a]/30 text-[#33b1ff] border border-[#00539a]/60',
		teal: 'bg-[#005d5d]/30 text-[#08bdba] border border-[#005d5d]/60',
		magenta: 'bg-[#9f1853]/30 text-[#ff7eb6] border border-[#9f1853]/60',
		gray: 'bg-[#525252]/40 text-[#c6c6c6] border border-[#6f6f6f]/50',
		'warm-gray': 'bg-[#57534e]/40 text-[#d6d3d1] border border-[#78716c]/50',
		'cool-gray': 'bg-[#475569]/40 text-[#cbd5e1] border border-[#64748b]/50'
	};

	const sizeClasses = {
		sm: 'h-5 px-1.5 text-[10px] font-mono',
		md: 'h-6 px-2 text-xs font-mono'
	};
</script>

<span class="inline-flex items-center gap-1 font-mono uppercase tracking-wider font-semibold rounded-none select-none {sizeClasses[size]} {typeClasses[type]} {className}">
	{@render children?.()}
	{#if filter}
		<button
			type="button"
			onclick={onclose}
			class="hover:opacity-75 focus:outline-none ml-1 cursor-pointer"
			aria-label="Remove filter"
		>
			×
		</button>
	{/if}
</span>
