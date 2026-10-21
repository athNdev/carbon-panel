<!-- SPDX-License-Identifier: MPL-2.0 -->
<script lang="ts">
	import changelog from '$lib/changelog.md?raw';

	let edition = $state<'oss' | 'cloud'>('oss');
</script>

<div class="p-6">
	<h1 class="mb-4 text-2xl font-semibold">Changelog</h1>
	<div class="mb-4 flex gap-2">
		<button
			class="border px-3 py-1 text-sm {edition === 'oss' ? 'bg-[#0f62fe] text-white' : 'bg-[#262626] text-[#f4f4f4]'}"
			onclick={() => (edition = 'oss')}
		>
			OSS
		</button>
		<button
			class="border px-3 py-1 text-sm {edition === 'cloud' ? 'bg-[#0f62fe] text-white' : 'bg-[#262626] text-[#f4f4f4]'}"
			onclick={() => (edition = 'cloud')}
		>
			Cloud
		</button>
	</div>

	{#if edition === 'oss'}
		<pre class="whitespace-pre-wrap font-mono text-sm text-[#f4f4f4]">{changelog}</pre>
	{:else}
		<p class="text-sm text-[#a8a8a8]">
			Cloud (hosted) changelog is not bundled with this OSS build.
		</p>
	{/if}
</div>