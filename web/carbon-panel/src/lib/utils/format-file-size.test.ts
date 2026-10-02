import test from 'node:test';
import assert from 'node:assert/strict';
import { formatFileSize, parseFileSize } from './format-file-size';

test('formatFileSize formats normal sizes', () => {
	assert.equal(formatFileSize(0), '0 B');
	assert.equal(formatFileSize(1024), '1 KB');
	assert.equal(formatFileSize(1536), '1.5 KB');
});

test('formatFileSize never renders NaN for bad input', () => {
	for (const bad of [NaN, Infinity, -1, -1024]) {
		const out = formatFileSize(bad);
		assert.ok(!out.includes('NaN'), `expected no NaN for ${bad}, got "${out}"`);
		assert.ok(!out.includes('undefined'), `expected no undefined for ${bad}, got "${out}"`);
	}
	assert.equal(formatFileSize(NaN), '0 B');
	assert.equal(formatFileSize(-5), '0 B');
});

test('parseFileSize collapses non-numeric or negative sizes to 0', () => {
	assert.equal(parseFileSize('1234'), 1234);
	assert.equal(parseFileSize(2048), 2048);
	assert.equal(parseFileSize('abc'), 0);
	assert.equal(parseFileSize(undefined), 0);
	assert.equal(parseFileSize(-10), 0);
	assert.equal(parseFileSize(NaN), 0);
});
