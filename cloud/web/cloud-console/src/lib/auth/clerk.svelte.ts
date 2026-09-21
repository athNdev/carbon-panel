// Thin reactive adapter over @clerk/clerk-js.
// - Reads the publishable key from $env/dynamic/public; a missing key puts the
//   app into a "not configured" state instead of crashing.
// - Exposes the session token and active organization for API calls.
import { Clerk } from '@clerk/clerk-js';
import { env } from '$env/dynamic/public';

const CLERK_KEY: string | undefined = env.PUBLIC_CLERK_PUBLISHABLE_KEY;

export type AuthUser = {
	id: string;
	email: string | null;
	name: string | null;
	imageUrl: string | null;
};

export type AuthOrg = {
	id: string;
	name: string;
	slug: string | null;
};

export type AuthMembership = {
	org: AuthOrg;
	role: string;
};

export type AuthState = {
	ready: boolean;
	signedIn: boolean;
	configured: boolean;
	notConfigured: boolean;
	user: AuthUser | null;
	org: AuthOrg | null;
	orgRole: string | null;
	initFailed: boolean;
};

/**
 * Maps a Clerk organization role to our console role key.
 * Clerk only exposes org:owner / org:admin / org:member; the finer roles
 * (operator, viewer, billing) are ours. org:member maps to operator.
 */
export function normalizeRole(role: string | null | undefined): string {
	if (!role) return 'viewer';
	const r = role.toLowerCase();
	switch (r) {
		case 'org:owner':
		case 'owner':
			return 'owner';
		case 'org:admin':
		case 'admin':
			return 'admin';
		case 'org:member':
		case 'operator':
			return 'operator';
		case 'org:billing':
		case 'billing':
			return 'billing';
		default:
			return 'viewer';
	}
}

function toAuthUser(clerkUser: unknown): AuthUser | null {
	const u = clerkUser as {
		id?: string;
		primaryEmailAddress?: { emailAddress?: string } | null;
		fullName?: string | null;
		username?: string | null;
		imageUrl?: string;
	} | null;
	if (!u) return null;
	return {
		id: u.id ?? '',
		email: u.primaryEmailAddress?.emailAddress ?? null,
		name: u.fullName || u.username || null,
		imageUrl: u.imageUrl ?? null
	};
}

function toAuthOrg(org: unknown): AuthOrg | null {
	const o = org as { id?: string; name?: string; slug?: string | null } | null;
	if (!o) return null;
	return { id: o.id ?? '', name: o.name ?? '', slug: o.slug ?? null };
}

function membershipsOf(clerkUser: unknown): Array<{ org: AuthOrg; role: string }> {
	const u = clerkUser as {
		organizationMemberships?: Array<{
			role?: string;
			organization?: { id?: string; name?: string; slug?: string | null };
		}>;
	} | null;
	const raw = u?.organizationMemberships ?? [];
	const out: Array<{ org: AuthOrg; role: string }> = [];
	for (const m of raw) {
		const org = toAuthOrg(m.organization);
		if (org) out.push({ org, role: normalizeRole(m.role) });
	}
	return out;
}

function createAuth() {
	const configured = Boolean(CLERK_KEY);
	let clerk: Clerk | null = null;
	let initStarted = false;

	const state = $state<AuthState>({
		ready: !configured,
		signedIn: false,
		configured,
		notConfigured: !configured,
		user: null,
		org: null,
		orgRole: null,
		initFailed: false
	});

	let memberships = $state<AuthMembership[]>([]);
	let initError = $state<string | null>(null);

	function syncFromClerk() {
		if (!clerk) return;
		state.signedIn = clerk.session != null;
		state.user = toAuthUser(clerk.user);
		state.org = toAuthOrg(clerk.organization);
		const mems = membershipsOf(clerk.user);
		memberships = mems;
		const activeId = clerk.organization?.id;
		const active = mems.find((m) => m.org.id === activeId);
		state.orgRole = active ? active.role : null;
	}

	async function init() {
		if (initStarted) return;
		initStarted = true;
		if (!configured) return;
		try {
			const c = new Clerk(CLERK_KEY as string);
			await c.load();
			clerk = c;
			c.addListener(() => syncFromClerk());
			syncFromClerk();
			state.initFailed = false;
		} catch (e) {
			console.error('Clerk failed to initialize:', e);
			state.initFailed = true;
			initError = e instanceof Error ? e.message : String(e);
		} finally {
			state.ready = true;
		}
	}

	async function token(): Promise<string | null> {
		try {
			const t = await clerk?.session?.getToken();
			return t ?? null;
		} catch {
			return null;
		}
	}

	async function setActiveOrg(id: string) {
		if (!clerk) return;
		try {
			await clerk.setActive({ organization: id });
			syncFromClerk();
		} catch (e) {
			console.error('Failed to switch organization:', e);
		}
	}

	async function signOut() {
		try {
			await clerk?.signOut();
			syncFromClerk();
		} catch (e) {
			console.error('Sign out failed:', e);
		}
	}

	function mountSignIn(el: HTMLDivElement) {
		if (!clerk) return;
		try {
			void clerk.mountSignIn(el);
		} catch (e) {
			console.error('Failed to mount Clerk sign-in:', e);
		}
	}

	return {
		init,
		configured,
		token,
		setActiveOrg,
		signOut,
		mountSignIn,
		get ready() {
			return state.ready;
		},
		get signedIn() {
			return state.signedIn;
		},
		get notConfigured() {
			return state.notConfigured;
		},
		get initFailed() {
			return state.initFailed;
		},
		get initError() {
			return initError;
		},
		get user() {
			return state.user;
		},
		get org() {
			return state.org;
		},
		get orgRole() {
			return state.orgRole;
		},
		get memberships() {
			return memberships;
		}
	};
}

export const auth = createAuth();