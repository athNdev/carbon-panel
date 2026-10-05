import test from 'node:test';
import assert from 'node:assert/strict';
import { createRequestSequence } from './request-sequence';

test('request sequence hands out increasing ids and the latest is current', () => {
	const seq = createRequestSequence();
	const first = seq.next();
	const second = seq.next();
	assert.ok(second > first);
	assert.equal(seq.isCurrent(second), true);
	assert.equal(seq.isCurrent(first), false);
});

test('request sequence marks an earlier in-flight response as stale', () => {
	const seq = createRequestSequence();
	const slowEarlier = seq.next();
	// A newer keystroke/click starts a second request before the first resolves.
	const fastLater = seq.next();
	// The slow response must be dropped; the fresh one applied.
	assert.equal(seq.isCurrent(slowEarlier), false);
	assert.equal(seq.isCurrent(fastLater), true);
});

test('independent sequences do not interfere', () => {
	const searches = createRequestSequence();
	const versions = createRequestSequence();
	const s = searches.next();
	searches.next();
	versions.next();
	assert.equal(searches.isCurrent(s), false);
	assert.equal(versions.isCurrent(1), true);
});
