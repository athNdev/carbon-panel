import test from 'node:test';
import assert from 'node:assert/strict';
import { safePercent, memoryUsagePercent } from './safe-percent';

test('safePercent computes the normal ratio', () => {
	assert.equal(safePercent(50, 200), 25);
	assert.equal(safePercent(1, 4), 25);
});

test('safePercent returns 0 for a zero denominator instead of NaN', () => {
	assert.equal(safePercent(10, 0), 0);
	assert.equal(safePercent(0, 0), 0);
});

test('safePercent returns 0 for non-finite inputs', () => {
	assert.equal(safePercent(NaN, 100), 0);
	assert.equal(safePercent(10, NaN), 0);
	assert.equal(safePercent(10, Infinity), 0);
	assert.equal(safePercent(10, -5), 0);
});

test('memoryUsagePercent computes the normal ratio', () => {
	assert.equal(memoryUsagePercent(512, 1024), 50);
});

test('memoryUsagePercent returns 0 when allocation is zero instead of NaN', () => {
	assert.equal(memoryUsagePercent(512, 0), 0);
	assert.equal(memoryUsagePercent('512', 0), 0);
});

test('memoryUsagePercent clamps over-100 usage and rejects bad input', () => {
	assert.equal(memoryUsagePercent(2048, 1024), 100);
	assert.equal(memoryUsagePercent(undefined, 1024), 0);
	assert.equal(memoryUsagePercent(NaN, 1024), 0);
	assert.equal(memoryUsagePercent(512, undefined), 0);
});
