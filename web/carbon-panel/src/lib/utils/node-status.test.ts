import test from 'node:test';
import assert from 'node:assert/strict';
import { describeNodeStatus, pickPrimaryNode } from './node-status';
import { NodeStatus } from '$lib/proto/carbonpanel/v1/node_pb';

test('describeNodeStatus maps known statuses to Carbon labels', () => {
	assert.equal(describeNodeStatus(NodeStatus.ONLINE).label, 'ONLINE');
	assert.equal(describeNodeStatus(NodeStatus.OFFLINE).label, 'OFFLINE');
	assert.equal(describeNodeStatus(NodeStatus.ERROR).label, 'ERROR');
});

test('describeNodeStatus falls back to UNKNOWN for missing values', () => {
	assert.equal(describeNodeStatus(NodeStatus.UNSPECIFIED).label, 'UNKNOWN');
	assert.equal(describeNodeStatus(null).label, 'UNKNOWN');
	assert.equal(describeNodeStatus(undefined).label, 'UNKNOWN');
});

test('pickPrimaryNode returns null when there are no nodes', () => {
	assert.equal(pickPrimaryNode([]), null);
});

test('pickPrimaryNode prefers the local node', () => {
	const nodes = [
		{ id: 'a', name: 'remote', isLocal: false, status: NodeStatus.ONLINE },
		{ id: 'b', name: 'local', isLocal: true, status: NodeStatus.OFFLINE }
	];
	assert.equal(pickPrimaryNode(nodes)?.id, 'b');
});

test('pickPrimaryNode prefers an online node over an offline first node', () => {
	const nodes = [
		{ id: 'a', name: 'one', isLocal: false, status: NodeStatus.OFFLINE },
		{ id: 'b', name: 'two', isLocal: false, status: NodeStatus.ONLINE }
	];
	assert.equal(pickPrimaryNode(nodes)?.id, 'b');
});
