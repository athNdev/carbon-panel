import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

test('all <th> elements in svelte views declare scope attribute', () => {
	const srcDir = path.resolve(import.meta.dirname, '../..');

	function scanDir(dir: string, fileList: string[] = []): string[] {
		const files = fs.readdirSync(dir);
		for (const file of files) {
			const fullPath = path.join(dir, file);
			const stat = fs.statSync(fullPath);
			if (stat.isDirectory()) {
				scanDir(fullPath, fileList);
			} else if (file.endsWith('.svelte')) {
				fileList.push(fullPath);
			}
		}
		return fileList;
	}

	const svelteFiles = scanDir(srcDir);
	const violations: string[] = [];

	for (const file of svelteFiles) {
		const content = fs.readFileSync(file, 'utf-8');
		// Find <th tags that are not comments or type annotations
		const thRegex = /<th(?:\s+[^>]*?)?>/gi;
		let match: RegExpExecArray | null;
		while ((match = thRegex.exec(content)) !== null) {
			const tag = match[0];
			if (!tag.includes('scope=')) {
				violations.push(`${path.relative(srcDir, file)}: ${tag}`);
			}
		}
	}

	assert.deepEqual(
		violations,
		[],
		`Found <th> elements without scope attribute: ${JSON.stringify(violations, null, 2)}`
	);
});

test('CarbonModal enforces dialog semantics, keyboard trap, and ARIA attributes', () => {
	const modalPath = path.resolve(import.meta.dirname, '../components/carbon/CarbonModal.svelte');
	const content = fs.readFileSync(modalPath, 'utf-8');

	assert.ok(content.includes('role="dialog"'), 'CarbonModal must have role="dialog"');
	assert.ok(content.includes('aria-modal="true"'), 'CarbonModal must have aria-modal="true"');
	assert.ok(content.includes('aria-labelledby='), 'CarbonModal must have aria-labelledby');
	assert.ok(content.includes('aria-describedby='), 'CarbonModal must have aria-describedby');
	assert.ok(content.includes("event.key === 'Escape'"), 'CarbonModal must handle Escape key');
	assert.ok(content.includes("event.key === 'Tab'"), 'CarbonModal must handle Tab focus trapping');
	assert.ok(
		content.includes('<svelte:window onkeydown={handleKeydown} />'),
		'CarbonModal must listen for global keydown'
	);
});
