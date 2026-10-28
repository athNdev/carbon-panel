// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

import test from 'node:test';
import assert from 'node:assert/strict';
import { beginLifecycleRequest } from './lifecycle-request';

test('an in-flight lifecycle action reuses its request id', () => {
	const first = beginLifecycleRequest('srv-1', 'start');
	const second = beginLifecycleRequest('srv-1', 'start');
	assert.equal(second.requestId, first.requestId);
	second.finish();
	assert.equal(beginLifecycleRequest('srv-1', 'start').requestId, first.requestId);
	first.finish();
	const third = beginLifecycleRequest('srv-1', 'start');
	assert.notEqual(third.requestId, first.requestId);
	third.finish();
});

test('different actions and servers get different request ids', () => {
	const start = beginLifecycleRequest('srv-1', 'start');
	const stop = beginLifecycleRequest('srv-1', 'stop');
	const other = beginLifecycleRequest('srv-2', 'start');
	assert.notEqual(start.requestId, stop.requestId);
	assert.notEqual(start.requestId, other.requestId);
	start.finish();
	stop.finish();
	other.finish();
});
