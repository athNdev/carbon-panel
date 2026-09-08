/**
 * Client-Side CurseForge & Modrinth Manifest Inspector (MINE-22)
 * Parses manifest.json (CurseForge) and modrinth.index.json (Modrinth mrpack),
 * identifies client-only vs server-only mods, and checks compatibility.
 */

export type ModpackFormat = 'modrinth' | 'curseforge' | 'unknown';

export type ModEnvironment = 'client' | 'server' | 'both' | 'unknown';

export interface InspectedMod {
	name: string;
	filename?: string;
	path?: string;
	projectId?: string | number;
	fileId?: string | number;
	env: ModEnvironment;
	required: boolean;
	downloadUrl?: string;
	hashes?: Record<string, string>;
	fileSize?: number;
}

export interface ManifestInspectionResult {
	format: ModpackFormat;
	name: string;
	version: string;
	summary?: string;
	gameVersion: string;
	modLoader: string;
	totalMods: number;
	universalCount: number;
	clientOnlyCount: number;
	serverOnlyCount: number;
	unknownCount: number;
	mods: InspectedMod[];
	raw: any;
}

// Known client-only mod name patterns for CurseForge manifests
const KNOWN_CLIENT_ONLY_PATTERNS = [
	/sodium/i,
	/rubidium/i,
	/embeddium/i,
	/iris/i,
	/oculus/i,
	/optifine/i,
	/entityculling/i,
	/dynamiclights/i,
	/appleskin/i,
	/controlling/i,
	/cloth-config/i,
	/soundphysics/i,
	/xaero/i,
	/voxelmap/i,
	/journeymap/i,
	/resourceloader/i,
	/customskinloader/i,
	/modmenu/i,
	/cherished-worlds/i,
	/defaultoptions/i,
	/presence-footsteps/i,
	/ambientsounds/i,
	/reauth/i,
	/authme/i,
	/borderless/i,
	/itemphysic-lite/i,
	/wavey-capes/i,
	/notenoughanimations/i,
	/firstperson/i,
	/visuality/i,
	/chat-heads/i
];

// Known server-only mod name patterns
const KNOWN_SERVER_ONLY_PATTERNS = [
	/chunky/i,
	/spark/i,
	/carpet/i,
	/luckperms/i,
	/ledger/i,
	/vane/i,
	/dynmap/i,
	/bluemap/i,
	/squaremap/i,
	/geyser/i,
	/floodgate/i,
	/fastbackups/i,
	/simple-backup/i
];

/**
 * Detects format of manifest content
 */
export function detectManifestFormat(data: any): ModpackFormat {
	if (!data || typeof data !== 'object') return 'unknown';
	if (data.formatVersion !== undefined && (data.game === 'minecraft' || data.dependencies)) {
		return 'modrinth';
	}
	if (data.manifestType === 'minecraftModpack' || data.manifestVersion !== undefined) {
		return 'curseforge';
	}
	// Fallback detection
	if (Array.isArray(data.files)) {
		if (data.files.length > 0 && (data.files[0].projectID !== undefined || data.files[0].projectId !== undefined)) {
			return 'curseforge';
		}
		if (data.files.length > 0 && (data.files[0].hashes !== undefined || data.files[0].env !== undefined)) {
			return 'modrinth';
		}
	}
	return 'unknown';
}

/**
 * Parses and inspects a Modrinth modrinth.index.json
 */
export function inspectModrinthManifest(data: any): ManifestInspectionResult {
	const name = data.name || 'Unnamed Modrinth Modpack';
	const version = data.versionId || data.version || '1.0.0';
	const summary = data.summary || '';
	const deps = data.dependencies || {};
	const gameVersion = deps.minecraft || 'Unknown';

	let modLoader = 'vanilla';
	for (const [key, val] of Object.entries(deps)) {
		if (key.includes('fabric') || key.includes('forge') || key.includes('neoforge') || key.includes('quilt')) {
			modLoader = `${key} (${val})`;
			break;
		}
	}

	const files = Array.isArray(data.files) ? data.files : [];
	const mods: InspectedMod[] = [];

	let universalCount = 0;
	let clientOnlyCount = 0;
	let serverOnlyCount = 0;
	let unknownCount = 0;

	for (const file of files) {
		const filePath = file.path || '';
		const fileName = filePath.split('/').pop() || filePath;
		const env = file.env || {};
		const clientEnv = env.client || 'required';
		const serverEnv = env.server || 'required';

		let resolvedEnv: ModEnvironment = 'both';
		if (serverEnv === 'unsupported') {
			resolvedEnv = 'client';
			clientOnlyCount++;
		} else if (clientEnv === 'unsupported') {
			resolvedEnv = 'server';
			serverOnlyCount++;
		} else {
			resolvedEnv = 'both';
			universalCount++;
		}

		mods.push({
			name: fileName.replace(/\.jar$/i, ''),
			filename: fileName,
			path: filePath,
			env: resolvedEnv,
			required: clientEnv === 'required' || serverEnv === 'required',
			downloadUrl: Array.isArray(file.downloads) && file.downloads.length > 0 ? file.downloads[0] : undefined,
			hashes: file.hashes,
			fileSize: file.fileSize
		});
	}

	return {
		format: 'modrinth',
		name,
		version,
		summary,
		gameVersion,
		modLoader,
		totalMods: mods.length,
		universalCount,
		clientOnlyCount,
		serverOnlyCount,
		unknownCount,
		mods,
		raw: data
	};
}

/**
 * Parses and inspects a CurseForge manifest.json
 */
export function inspectCurseForgeManifest(data: any): ManifestInspectionResult {
	const name = data.name || 'Unnamed CurseForge Modpack';
	const version = data.version || '1.0.0';
	const mc = data.minecraft || {};
	const gameVersion = mc.version || 'Unknown';

	let modLoader = 'vanilla';
	if (Array.isArray(mc.modLoaders) && mc.modLoaders.length > 0) {
		const primary = mc.modLoaders.find((l: any) => l.primary) || mc.modLoaders[0];
		modLoader = primary.id || 'custom';
	}

	const files = Array.isArray(data.files) ? data.files : [];
	const mods: InspectedMod[] = [];

	let universalCount = 0;
	let clientOnlyCount = 0;
	let serverOnlyCount = 0;
	let unknownCount = 0;

	for (const file of files) {
		const projId = file.projectID ?? file.projectId ?? '';
		const fId = file.fileID ?? file.fileId ?? '';
		const req = file.required !== false;

		// Fallback environment classification based on name/hints if available
		const identifier = `${projId}:${fId}`;
		let resolvedEnv: ModEnvironment = 'unknown';

		// If filename or name exists in file object
		const displayName = file.name || `Mod #${projId}`;
		if (KNOWN_CLIENT_ONLY_PATTERNS.some((p) => p.test(displayName))) {
			resolvedEnv = 'client';
			clientOnlyCount++;
		} else if (KNOWN_SERVER_ONLY_PATTERNS.some((p) => p.test(displayName))) {
			resolvedEnv = 'server';
			serverOnlyCount++;
		} else {
			resolvedEnv = 'both';
			universalCount++;
		}

		mods.push({
			name: displayName,
			projectId: projId,
			fileId: fId,
			env: resolvedEnv,
			required: req
		});
	}

	return {
		format: 'curseforge',
		name,
		version,
		gameVersion,
		modLoader,
		totalMods: mods.length,
		universalCount,
		clientOnlyCount,
		serverOnlyCount,
		unknownCount,
		mods,
		raw: data
	};
}

/**
 * Main entry point: Inspects any manifest content (string JSON or parsed object)
 */
export function inspectManifest(input: string | object): ManifestInspectionResult {
	let data: any = input;
	if (typeof input === 'string') {
		try {
			data = JSON.parse(input);
		} catch (err: any) {
			throw new Error(`Failed to parse manifest JSON: ${err?.message || err}`);
		}
	}

	const format = detectManifestFormat(data);
	if (format === 'modrinth') {
		return inspectModrinthManifest(data);
	} else if (format === 'curseforge') {
		return inspectCurseForgeManifest(data);
	}

	throw new Error('Unsupported manifest format. Expected Modrinth modrinth.index.json or CurseForge manifest.json.');
}

/**
 * Exports a filtered server-side manifest containing only server-compatible mods
 */
export function exportServerManifest(inspection: ManifestInspectionResult): string {
	const serverMods = inspection.mods.filter((m) => m.env === 'server' || m.env === 'both');

	if (inspection.format === 'modrinth') {
		const rawCopy = JSON.parse(JSON.stringify(inspection.raw));
		rawCopy.files = (rawCopy.files || []).filter((f: any) => {
			const env = f.env || {};
			return env.server !== 'unsupported';
		});
		return JSON.stringify(rawCopy, null, 2);
	}

	if (inspection.format === 'curseforge') {
		const rawCopy = JSON.parse(JSON.stringify(inspection.raw));
		const clientOnlyIds = new Set(
			inspection.mods.filter((m) => m.env === 'client').map((m) => `${m.projectId}:${m.fileId}`)
		);
		rawCopy.files = (rawCopy.files || []).filter(
			(f: any) => !clientOnlyIds.has(`${f.projectID ?? f.projectId}:${f.fileID ?? f.fileId}`)
		);
		return JSON.stringify(rawCopy, null, 2);
	}

	return JSON.stringify({ mods: serverMods }, null, 2);
}
