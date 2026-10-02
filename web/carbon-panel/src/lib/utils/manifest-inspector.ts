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
	raw: unknown;
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

type JsonRec = Record<string, unknown>;

function asRec(v: unknown): JsonRec {
	return typeof v === 'object' && v !== null ? (v as JsonRec) : {};
}

function str(v: unknown, fallback = ''): string {
	return typeof v === 'string' ? v : fallback;
}

function strNum(v: unknown, fallback: string | number = ''): string | number {
	return typeof v === 'string' || typeof v === 'number' ? v : fallback;
}

function isStrRec(v: unknown): v is Record<string, string> {
	if (typeof v !== 'object' || v === null) return false;
	return Object.values(v).every((x) => typeof x === 'string');
}

/**
 * Detects format of manifest content
 */
export function detectManifestFormat(input: unknown): ModpackFormat {
	const data = asRec(input);
	if (Object.keys(data).length === 0 && (input === null || typeof input !== 'object'))
		return 'unknown';
	if (data.formatVersion !== undefined && (data.game === 'minecraft' || data.dependencies)) {
		return 'modrinth';
	}
	if (data.manifestType === 'minecraftModpack' || data.manifestVersion !== undefined) {
		return 'curseforge';
	}
	// Fallback detection
	if (Array.isArray(data.files) && data.files.length > 0) {
		const first = asRec(data.files[0]);
		if (first.projectID !== undefined || first.projectId !== undefined) {
			return 'curseforge';
		}
		if (first.hashes !== undefined || first.env !== undefined) {
			return 'modrinth';
		}
	}
	return 'unknown';
}

/**
 * Parses and inspects a Modrinth modrinth.index.json
 */
export function inspectModrinthManifest(input: unknown): ManifestInspectionResult {
	const data = asRec(input);
	const name = str(data.name, 'Unnamed Modrinth Modpack');
	const version = str(data.versionId, '') || str(data.version, '1.0.0');
	const summary = str(data.summary);
	const deps = asRec(data.dependencies);
	const gameVersion = str(deps.minecraft, 'Unknown');

	let modLoader = 'vanilla';
	for (const [key, val] of Object.entries(deps)) {
		if (
			key.includes('fabric') ||
			key.includes('forge') ||
			key.includes('neoforge') ||
			key.includes('quilt')
		) {
			modLoader = `${key} (${String(val)})`;
			break;
		}
	}

	const files: unknown[] = Array.isArray(data.files) ? data.files : [];
	const mods: InspectedMod[] = [];

	let universalCount = 0;
	let clientOnlyCount = 0;
	let serverOnlyCount = 0;
	const unknownCount = 0;

	for (const rawFile of files) {
		const file = asRec(rawFile);
		const filePath = str(file.path);
		const fileName = filePath.split('/').pop() || filePath;
		const env = asRec(file.env);
		const clientEnv = str(env.client, 'required');
		const serverEnv = str(env.server, 'required');

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
			downloadUrl:
				Array.isArray(file.downloads) && typeof file.downloads[0] === 'string'
					? file.downloads[0]
					: undefined,
			hashes: isStrRec(file.hashes) ? file.hashes : undefined,
			fileSize: typeof file.fileSize === 'number' ? file.fileSize : undefined
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
export function inspectCurseForgeManifest(input: unknown): ManifestInspectionResult {
	const data = asRec(input);
	const name = str(data.name, 'Unnamed CurseForge Modpack');
	const version = str(data.version, '1.0.0');
	const mc = asRec(data.minecraft);
	const gameVersion = str(mc.version, 'Unknown');

	let modLoader = 'vanilla';
	const loaders = Array.isArray(mc.modLoaders) ? mc.modLoaders.map(asRec) : [];
	if (loaders.length > 0) {
		const primary = loaders.find((l) => l.primary === true) ?? loaders[0];
		modLoader = str(primary?.id, 'custom');
	}

	const files: unknown[] = Array.isArray(data.files) ? data.files : [];
	const mods: InspectedMod[] = [];

	let universalCount = 0;
	let clientOnlyCount = 0;
	let serverOnlyCount = 0;
	const unknownCount = 0;

	for (const rawFile of files) {
		const file = asRec(rawFile);
		const projId = strNum(file.projectID ?? file.projectId);
		const fId = strNum(file.fileID ?? file.fileId);
		const req = file.required !== false;

		// Fallback environment classification based on name/hints if available
		let resolvedEnv: ModEnvironment = 'unknown';

		// If filename or name exists in file object
		const displayName = str(file.name) || `Mod #${String(projId)}`;
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
	let data: unknown = input;
	if (typeof input === 'string') {
		try {
			data = JSON.parse(input);
		} catch (err) {
			const detail = err instanceof Error ? err.message : String(err);
			throw new Error(`Failed to parse manifest JSON: ${detail}`);
		}
	}

	const format = detectManifestFormat(data);
	if (format === 'modrinth') {
		return inspectModrinthManifest(data);
	} else if (format === 'curseforge') {
		return inspectCurseForgeManifest(data);
	}

	throw new Error(
		'Unsupported manifest format. Expected Modrinth modrinth.index.json or CurseForge manifest.json.'
	);
}

/**
 * Exports a filtered server-side manifest containing only server-compatible mods
 */
export function exportServerManifest(inspection: ManifestInspectionResult): string {
	const serverMods = inspection.mods.filter((m) => m.env === 'server' || m.env === 'both');

	if (inspection.format === 'modrinth') {
		const rawCopy = asRec(JSON.parse(JSON.stringify(inspection.raw)));
		const rawFiles: unknown[] = Array.isArray(rawCopy.files) ? rawCopy.files : [];
		rawCopy.files = rawFiles.filter((f) => {
			const env = asRec(asRec(f).env);
			return env.server !== 'unsupported';
		});
		return JSON.stringify(rawCopy, null, 2);
	}

	if (inspection.format === 'curseforge') {
		const rawCopy = JSON.parse(JSON.stringify(inspection.raw));
		const clientOnlyIds = new Set(
			inspection.mods.filter((m) => m.env === 'client').map((m) => `${m.projectId}:${m.fileId}`)
		);
		const rawFiles: unknown[] = Array.isArray(rawCopy.files) ? rawCopy.files : [];
		rawCopy.files = rawFiles.filter((f) => {
			const rec = asRec(f);
			return !clientOnlyIds.has(
				`${String(rec.projectID ?? rec.projectId)}:${String(rec.fileID ?? rec.fileId)}`
			);
		});
		return JSON.stringify(rawCopy, null, 2);
	}

	return JSON.stringify({ mods: serverMods }, null, 2);
}
