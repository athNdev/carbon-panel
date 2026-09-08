import test from 'node:test';
import assert from 'node:assert/strict';
import {
	detectManifestFormat,
	inspectManifest,
	exportServerManifest
} from './manifest-inspector.ts';

test('detectManifestFormat identifies modrinth format', () => {
	const data = {
		formatVersion: 1,
		game: 'minecraft',
		versionId: '1.0.0',
		dependencies: { minecraft: '1.20.1' },
		files: []
	};
	assert.equal(detectManifestFormat(data), 'modrinth');
});

test('detectManifestFormat identifies curseforge format', () => {
	const data = {
		manifestType: 'minecraftModpack',
		manifestVersion: 1,
		minecraft: { version: '1.20.1' },
		files: [{ projectID: 123, fileID: 456 }]
	};
	assert.equal(detectManifestFormat(data), 'curseforge');
});

test('inspectManifest accurately parses Modrinth manifest and separates client vs server mods', () => {
	const sampleModrinth = {
		formatVersion: 1,
		game: 'minecraft',
		versionId: '2.5.0',
		name: 'Performance Pack',
		dependencies: {
			minecraft: '1.20.1',
			'fabric-loader': '0.15.7'
		},
		files: [
			{
				path: 'mods/sodium.jar',
				env: { client: 'required', server: 'unsupported' },
				downloads: ['https://cdn.modrinth.com/sodium.jar'],
				hashes: { sha1: 'abc' }
			},
			{
				path: 'mods/ferritecore.jar',
				env: { client: 'required', server: 'required' },
				downloads: ['https://cdn.modrinth.com/ferritecore.jar']
			},
			{
				path: 'mods/chunky.jar',
				env: { client: 'unsupported', server: 'required' },
				downloads: ['https://cdn.modrinth.com/chunky.jar']
			}
		]
	};

	const result = inspectManifest(JSON.stringify(sampleModrinth));
	assert.equal(result.format, 'modrinth');
	assert.equal(result.name, 'Performance Pack');
	assert.equal(result.gameVersion, '1.20.1');
	assert.equal(result.totalMods, 3);
	assert.equal(result.clientOnlyCount, 1);
	assert.equal(result.serverOnlyCount, 1);
	assert.equal(result.universalCount, 1);

	const clientMod = result.mods.find((m) => m.name === 'sodium');
	assert.equal(clientMod?.env, 'client');

	const serverExport = exportServerManifest(result);
	const parsedExport = JSON.parse(serverExport);
	assert.equal(parsedExport.files.length, 2);
	assert.ok(!parsedExport.files.some((f: any) => f.path.includes('sodium')));
});

test('inspectManifest parses CurseForge manifest and excludes client-only mods', () => {
	const sampleCurse = {
		minecraft: {
			version: '1.20.1',
			modLoaders: [{ id: 'forge-47.2.0', primary: true }]
		},
		manifestType: 'minecraftModpack',
		manifestVersion: 1,
		name: 'Tech & Magic',
		version: '1.0',
		files: [
			{ projectID: 101, fileID: 1001, name: 'iris-shaders.jar', required: true },
			{ projectID: 102, fileID: 1002, name: 'applied-energistics.jar', required: true }
		]
	};

	const result = inspectManifest(sampleCurse);
	assert.equal(result.format, 'curseforge');
	assert.equal(result.totalMods, 2);
	assert.equal(result.clientOnlyCount, 1);
	assert.equal(result.universalCount, 1);

	const iris = result.mods.find((m) => m.projectId === 101);
	assert.equal(iris?.env, 'client');

	const serverExport = JSON.parse(exportServerManifest(result));
	assert.equal(serverExport.files.length, 1);
	assert.equal(serverExport.files[0].projectID, 102);
});
