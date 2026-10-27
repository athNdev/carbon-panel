<!-- SPDX-License-Identifier: MPL-2.0 -->
<script lang="ts">
	import changelog from '$lib/changelog.md?raw';
	import { marked } from 'marked';

	// Drop the markdown's leading H1 so it does not duplicate the page title.
	const body = changelog.replace(/^#\s+Changelog\s*$/m, '');
	// The changelog is trusted repository content (not user input), so rendering
	// it as HTML is safe. `async: false` keeps the return a plain string.
	const rendered = marked.parse(body, { async: false }) as string;
</script>

<div class="p-6">
	<h1 class="mb-4 text-2xl font-semibold">Changelog</h1>
	<!-- eslint-disable-next-line svelte/no-at-html-tags -- changelog.md is trusted repository content, not user input -->
	<div class="changelog-md">{@html rendered}</div>
</div>

<style>
	.changelog-md {
		max-width: 72ch;
		color: #f4f4f4;
		font-size: 0.9375rem;
		line-height: 1.7;
	}
	.changelog-md :global(h1) {
		margin: 1.5rem 0 0.75rem;
		font-size: 1.5rem;
		font-weight: 600;
		color: #ffffff;
		border-bottom: 1px solid #393939;
		padding-bottom: 0.5rem;
	}
	.changelog-md :global(h2) {
		margin: 1.5rem 0 0.75rem;
		font-size: 1.25rem;
		font-weight: 600;
		color: #ffffff;
		border-bottom: 1px solid #393939;
		padding-bottom: 0.5rem;
	}
	.changelog-md :global(h3) {
		margin: 1.25rem 0 0.5rem;
		font-size: 1.0625rem;
		font-weight: 600;
		color: #c6c6c6;
	}
	.changelog-md :global(ul) {
		margin: 0.5rem 0;
		padding-left: 1.5rem;
		list-style: disc;
	}
	.changelog-md :global(li) {
		margin: 0.25rem 0;
	}
	.changelog-md :global(p) {
		margin: 0.75rem 0;
	}
	.changelog-md :global(a) {
		color: #78a9ff;
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	.changelog-md :global(a:hover) {
		color: #a6c8ff;
	}
	.changelog-md :global(code) {
		font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
		font-size: 0.875em;
		background: #262626;
		border: 1px solid #393939;
		padding: 0.1em 0.35em;
		color: #ff8389;
	}
	.changelog-md :global(pre) {
		background: #262626;
		border: 1px solid #393939;
		padding: 0.75rem 1rem;
		overflow-x: auto;
		margin: 0.75rem 0;
	}
	.changelog-md :global(pre code) {
		background: none;
		border: none;
		padding: 0;
		color: #f4f4f4;
	}
	.changelog-md :global(hr) {
		border: none;
		border-top: 1px solid #393939;
		margin: 1.5rem 0;
	}
	.changelog-md :global(blockquote) {
		border-left: 3px solid #525252;
		margin: 0.75rem 0;
		padding-left: 1rem;
		color: #a8a8a8;
	}
</style>
