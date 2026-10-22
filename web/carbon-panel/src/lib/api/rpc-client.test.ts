import test from 'node:test';
import assert from 'node:assert/strict';
import { Code, ConnectError } from '@connectrpc/connect';
import { formatRpcError } from './rpc-error';

test('RPC transport errors use useful, protocol-free copy', () => {
	assert.equal(
		formatRpcError(new ConnectError('HTTP 404', Code.Unimplemented), 'fallback'),
		'This operation is not supported by the connected server.'
	);
	assert.equal(
		formatRpcError(new ConnectError('fetch failed', Code.Unavailable), 'fallback'),
		'Carbon Panel could not reach the server. Check the API connection and try again.'
	);
});

test('keeps actionable validation messages and hides internal details', () => {
	assert.equal(
		formatRpcError(new ConnectError('Port is already in use', Code.FailedPrecondition), 'fallback'),
		'Port is already in use'
	);
	assert.equal(
		formatRpcError(new ConnectError('database host=internal', Code.Internal), 'fallback'),
		'fallback'
	);
});
