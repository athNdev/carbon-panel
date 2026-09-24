// Thin reactive adapter over @clerk/clerk-js.
// - Reads the publishable key from $env/dynamic/public; a missing key puts the
//   app into a "not configured" state instead of crashing.
// - Exposes the session token and active organization for API calls.
import { Clerk } from '@clerk/clerk-js';
import { dark } from '@clerk/themes';
import { env } from '$env/dynamic/public';

const CLERK_KEY: string | undefined =
	env.PUBLIC_CLERK_PUBLISHABLE_KEY ||
	(typeof window !== 'undefined' && (window as any).__PUBLIC_CLERK_PUBLISHABLE_KEY__) ||
	'pk_test_Y29vbC1yZXB0aWxlLTU1NjMuY2xlcmsuYWNjb3VudHMuZGV2JA';

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
		case 'viewer':
		default:
			return 'viewer';
	}
}

function toAuthUser(raw: any): AuthUser | null {
	if (!raw) return null;
	const email =
		raw.primaryEmailAddress?.emailAddress ??
		(Array.isArray(raw.emailAddresses) && raw.emailAddresses[0]?.emailAddress) ??
		null;
	const name =
		raw.fullName ||
		[raw.firstName, raw.lastName].filter(Boolean).join(' ') ||
		email ||
		raw.id;
	return {
		id: raw.id,
		email,
		name,
		imageUrl: raw.imageUrl ?? null
	};
}

function toAuthOrg(raw: any): AuthOrg | null {
	if (!raw) return null;
	return {
		id: raw.id,
		name: raw.name ?? 'Organization',
		slug: raw.slug ?? null
	};
}

function membershipsOf(user: any): AuthMembership[] {
	if (!user || !Array.isArray(user.organizationMemberships)) return [];
	return user.organizationMemberships
		.map((m: any) => {
			if (!m || !m.organization) return null;
			return {
				org: toAuthOrg(m.organization)!,
				role: normalizeRole(m.role)
			};
		})
		.filter((m: any): m is AuthMembership => m !== null);
}

const clerkAppearance = {
	baseTheme: dark,
	variables: {
		colorPrimary: '#0f62fe',
		colorBackground: '#161616',
		colorInputBackground: '#262626',
		colorInputText: '#f4f4f4',
		colorText: '#f4f4f4',
		borderRadius: '0px'
	}
};

function createAuth() {
	const configured = Boolean(CLERK_KEY && CLERK_KEY.length > 0);
	let clerk: Clerk | null = null;
	let initPromise: Promise<void> | null = null;

	const state = $state<AuthState>({
		ready: false,
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
		const mems = membershipsOf(clerk.user);
		memberships = mems;

		let o = toAuthOrg(clerk.organization);
		if (!o && mems.length > 0) {
			o = mems[0].org;
			void clerk.setActive({ organization: o.id });
		}
		if (!o && state.signedIn) {
			o = { id: 'default', name: 'Default Organization', slug: 'default' };
			state.orgRole = 'owner';
		} else {
			const activeId = o?.id;
			const active = mems.find((m) => m.org.id === activeId);
			state.orgRole = active ? active.role : (state.signedIn ? 'owner' : null);
		}
		state.org = o;
	}

	async function init(): Promise<void> {
		if (initPromise) return initPromise;
		if (!configured) {
			state.ready = true;
			return;
		}
		initPromise = (async () => {
			try {
				const c = new Clerk(CLERK_KEY as string);
				await c.load({
					appearance: clerkAppearance
				});
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
		})();
		return initPromise;
	}

	async function token(): Promise<string | null> {
		try {
			await init();
			const t = await clerk?.session?.getToken();
			return t ?? null;
		} catch {
			return null;
		}
	}

	async function setActiveOrg(id: string) {
		await init();
		if (!clerk) return;
		try {
			await clerk.setActive({ organization: id });
			syncFromClerk();
		} catch (e) {
			console.error('Failed to switch organization:', e);
		}
	}

	async function signOut() {
		await init();
		try {
			await clerk?.signOut();
			syncFromClerk();
		} catch (e) {
			console.error('Sign out failed:', e);
		}
	}

	async function mountSignIn(el: HTMLDivElement) {
		await init();
		if (!clerk || !el) return;
		try {
			void clerk.mountSignIn(el, {
				appearance: clerkAppearance
			});
		} catch (e) {
			console.error('Failed to mount Clerk sign-in:', e);
		}
	}

	async function mountSignUp(el: HTMLDivElement) {
		await init();
		if (!clerk || !el) return;
		try {
			void clerk.mountSignUp(el, {
				appearance: clerkAppearance
			});
		} catch (e) {
			console.error('Failed to mount Clerk sign-up:', e);
		}
	}

	async function mountUserButton(el: HTMLDivElement) {
		await init();
		if (!clerk || !el) return;
		try {
			void clerk.mountUserButton(el, {
				appearance: clerkAppearance
			});
		} catch (e) {
			console.error('Failed to mount Clerk user button:', e);
		}
	}

	async function mountOrganizationSwitcher(el: HTMLDivElement) {
		await init();
		if (!clerk || !el) return;
		try {
			void clerk.mountOrganizationSwitcher(el, {
				appearance: clerkAppearance
			});
		} catch (e) {
			console.error('Failed to mount Clerk org switcher:', e);
		}
	}

	return {
		get state() {
			return state;
		},
		get memberships() {
			return memberships;
		},
		get initError() {
			return initError;
		},
		init,
		token,
		setActiveOrg,
		signOut,
		mountSignIn,
		mountSignUp,
		mountUserButton,
		mountOrganizationSwitcher
	};
}

export const auth = createAuth();
