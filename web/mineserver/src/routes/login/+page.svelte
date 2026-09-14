<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { create } from '@bufbuild/protobuf';
	import { authStore } from '$lib/stores/auth';
	import { rpcClient } from '$lib/api/rpc-client';
	import { silentCallOptions } from '$lib/api/rpc-client';
	import { ValidateInviteRequestSchema } from '$lib/proto/mineserver/v1/auth_pb';
	import {
		CarbonButton,
		CarbonTextInput,
		CarbonTabs
	} from '$lib/components/carbon';
	import { toast } from 'svelte-sonner';
	import { Loader2, AlertCircle, TicketCheck, KeyRound, Shield } from '@lucide/svelte';

	let mode = $state<'login' | 'register'>('login');
	let username = $state('');
	let email = $state('');
	let password = $state('');
	let confirmPassword = $state('');
	let loading = $state(false);
	let error = $state('');
	let authStatus = $state({
		enabled: false,
		firstUserSetup: false,
		allowRegistration: false
	});
	let oidcEnabled = $state(false);
	let localAuthEnabled = $state(true);

	// Recovery state
	let showRecovery = $state(false);
	let recoveryKey = $state('');

	// Invite state
	let inviteCode = $state('');
	let inviteValid = $state(false);
	let inviteRequiresPin = $state(false);
	let inviteDescription = $state('');
	let invitePin = $state('');

	onMount(() => {
		// Check for OIDC callback token
		const urlParams = new URLSearchParams(window.location.search);
		const token = urlParams.get('token');
		if (token) {
			authStore.setToken(token);
			window.history.replaceState({}, '', '/login');
			authStore.validateSession().then((valid) => {
				if (valid) {
					goto(resolve('/'));
				} else {
					error = 'Session validation failed. Please try again.';
				}
			});
			return;
		}

		// Check for invite code in URL
		const invite = urlParams.get('invite');

		// If already authenticated, redirect to home
		if ($authStore.isAuthenticated) {
			goto(resolve('/'));
			return;
		}

		// Check auth status
		authStore.checkAuthStatus().then(async (status) => {
			authStatus = status;
			oidcEnabled = $authStore.oidcEnabled;
			localAuthEnabled = $authStore.localAuthEnabled;

			// If auth is disabled and not first user setup, redirect to home
			if (!status.enabled && !status.firstUserSetup) {
				goto(resolve('/'));
				return;
			}

			// If first user setup, show registration
			if (status.firstUserSetup) {
				mode = 'register';
				return;
			}

			// Validate invite code if present
			if (invite) {
				try {
					const resp = await rpcClient.auth.validateInvite(
						create(ValidateInviteRequestSchema, { code: invite }),
						silentCallOptions
					);
					if (resp.valid) {
						inviteCode = invite;
						inviteValid = true;
						inviteRequiresPin = resp.requiresPin;
						inviteDescription = resp.description;
						mode = 'register';
					}
				} catch {
					// Invalid invite, just show normal login
				}
				window.history.replaceState({}, '', '/login');
			}
		});
	});

	async function handleLogin() {
		error = '';
		loading = true;

		try {
			await authStore.login(username, password);
			toast.success('Logged in successfully');
			setTimeout(() => {
				goto(resolve('/'));
			}, 100);
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Login failed';
			loading = false;
		}
	}

	async function handleRegister() {
		error = '';

		if (password !== confirmPassword) {
			error = 'Passwords do not match';
			return;
		}

		if (password.length < 8) {
			error = 'Password must be at least 8 characters';
			return;
		}

		loading = true;

		try {
			await authStore.register(
				username,
				email,
				password,
				inviteValid ? inviteCode : undefined,
				inviteValid && inviteRequiresPin ? invitePin : undefined
			);
			toast.success(
				authStatus.firstUserSetup
					? 'Admin account created successfully'
					: 'Account created successfully'
			);
			setTimeout(() => {
				goto(resolve('/'));
			}, 100);
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Registration failed';
			loading = false;
		}
	}

	async function handleOIDCLogin() {
		try {
			const response = await (
				await import('$lib/api/rpc-client')
			).rpcClient.auth.getOIDCLoginURL({});
			if (response.loginUrl) {
				window.location.href = response.loginUrl;
			}
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Failed to initiate SSO login';
		}
	}

	async function handleRecovery() {
		error = '';
		loading = true;
		try {
			await authStore.useRecoveryKey(recoveryKey);
			toast.success('Panel reset to first-user setup');
			window.location.reload();
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Invalid recovery key';
			loading = false;
		}
	}

	function handleSubmit(e: Event) {
		e.preventDefault();

		if (mode === 'login') {
			handleLogin();
		} else {
			handleRegister();
		}
	}

	const authTabs = [
		{ id: 'login', label: 'Login' },
		{ id: 'register', label: 'Register' }
	];
</script>

<svelte:head>
	<title>MineServer - Login</title>
</svelte:head>

<div class="min-h-screen flex items-center justify-center bg-[#161616] p-4 font-sans text-[#f4f4f4] rounded-none">
	<div class="w-full max-w-md bg-[#262626] border border-[#393939] p-8 rounded-none shadow-2xl">
		<!-- Header -->
		<div class="mb-8 text-center">
			<div class="flex items-center justify-center gap-3 mb-2">
				<img src="/g1_24x24.png" alt="MineServer Logo" class="h-8 w-8 rounded-none" />
				<h1 class="text-2xl font-semibold tracking-wide text-white">MineServer</h1>
			</div>
			{#if authStatus.firstUserSetup}
				<p class="text-xs text-[#c6c6c6]">
					Welcome! Create your initial administrator account.
				</p>
			{:else}
				<p class="text-xs text-[#a8a8a8]">
					Sign in to manage your Minecraft servers
				</p>
			{/if}
		</div>

		{#if error}
			<div class="mb-6 p-4 bg-[#da1e28]/10 border-l-4 border-[#da1e28] text-xs text-[#ff8389] flex items-start gap-2.5 rounded-none">
				<AlertCircle class="h-4 w-4 shrink-0 mt-0.5" />
				<span>{error}</span>
			</div>
		{/if}

		{#if authStatus.firstUserSetup}
			<!-- First User Setup Form -->
			<form onsubmit={handleSubmit} class="space-y-4">
				<CarbonTextInput
					label="Admin Username"
					placeholder="Choose admin username"
					bind:value={username}
					required
					disabled={loading}
				/>
				<CarbonTextInput
					type="email"
					label="Email (optional)"
					placeholder="admin@example.com"
					bind:value={email}
					disabled={loading}
				/>
				<CarbonTextInput
					type="password"
					label="Password"
					placeholder="Choose a strong password (min 8 chars)"
					bind:value={password}
					required
					disabled={loading}
				/>
				<CarbonTextInput
					type="password"
					label="Confirm Password"
					placeholder="Confirm your password"
					bind:value={confirmPassword}
					required
					disabled={loading}
				/>

				<div class="p-3.5 bg-[#161616] border-l-4 border-[#0f62fe] text-xs text-[#c6c6c6] rounded-none">
					{#if oidcEnabled}
						A local admin account is required for initial setup, even with SSO enabled. This ensures you always have a fallback login to manage the system if your identity provider becomes unavailable.
					{:else}
						This account will have full administrator permissions and access to the control panel.
					{/if}
				</div>

				<CarbonButton type="submit" class="w-full justify-center rounded-none" disabled={loading}>
					{#if loading}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Creating admin account...
					{:else}
						Create Admin Account
					{/if}
				</CarbonButton>
			</form>
		{:else}
			{#if (authStatus.allowRegistration || inviteValid) && localAuthEnabled}
				<CarbonTabs
					tabs={authTabs}
					bind:selectedTab={mode}
					class="mb-6"
				/>
			{/if}

			{#if mode === 'login'}
				<div class="space-y-4">
					{#if localAuthEnabled}
						<form onsubmit={handleSubmit} class="space-y-4">
							<CarbonTextInput
								label="Username"
								placeholder="Enter your username"
								bind:value={username}
								required
								disabled={loading}
							/>
							<CarbonTextInput
								type="password"
								label="Password"
								placeholder="Enter your password"
								bind:value={password}
								required
								disabled={loading}
							/>
							<CarbonButton type="submit" class="w-full justify-center rounded-none" disabled={loading}>
								{#if loading}
									<Loader2 class="mr-2 h-4 w-4 animate-spin" />
									Signing in...
								{:else}
									Sign In
								{/if}
							</CarbonButton>
						</form>
					{/if}

					{#if oidcEnabled}
						{#if localAuthEnabled}
							<div class="relative my-6 text-center">
								<div class="absolute inset-0 flex items-center">
									<div class="w-full border-t border-[#393939]"></div>
								</div>
								<span class="relative bg-[#262626] px-3 text-xs uppercase tracking-wider text-[#8d8d8d]">
									Or
								</span>
							</div>
						{/if}
						<CarbonButton
							kind={localAuthEnabled ? 'tertiary' : 'primary'}
							class="w-full justify-center rounded-none"
							onclick={handleOIDCLogin}
							disabled={loading}
						>
							<Shield class="mr-2 h-4 w-4" />
							Sign in with SSO
						</CarbonButton>
					{/if}
				</div>
			{:else}
				<!-- Registration Form -->
				<form onsubmit={handleSubmit} class="space-y-4">
					{#if inviteValid && inviteDescription}
						<div class="p-3 bg-[#198038]/15 border-l-4 border-[#198038] text-xs text-[#6fdc8c] flex items-center gap-2 rounded-none">
							<TicketCheck class="h-4 w-4 shrink-0" />
							<span>{inviteDescription}</span>
						</div>
					{/if}
					<CarbonTextInput
						label="Username"
						placeholder="Choose a username"
						bind:value={username}
						required
						disabled={loading}
					/>
					<CarbonTextInput
						type="email"
						label="Email (optional)"
						placeholder="your@email.com"
						bind:value={email}
						disabled={loading}
					/>
					<CarbonTextInput
						type="password"
						label="Password"
						placeholder="Choose a password (min 8 chars)"
						bind:value={password}
						required
						disabled={loading}
					/>
					<CarbonTextInput
						type="password"
						label="Confirm Password"
						placeholder="Confirm your password"
						bind:value={confirmPassword}
						required
						disabled={loading}
					/>
					{#if inviteValid && inviteRequiresPin}
						<CarbonTextInput
							type="password"
							label="Invite PIN"
							placeholder="Enter invite PIN"
							bind:value={invitePin}
							required
							disabled={loading}
						/>
					{/if}
					<CarbonButton type="submit" class="w-full justify-center rounded-none" disabled={loading}>
						{#if loading}
							<Loader2 class="mr-2 h-4 w-4 animate-spin" />
							Creating account...
						{:else}
							Create Account
						{/if}
					</CarbonButton>
				</form>
			{/if}
		{/if}

		<!-- Recovery Drawer / Options -->
		{#if showRecovery}
			<div class="mt-6 pt-6 border-t border-[#393939] space-y-4">
				<div class="p-3.5 bg-[#da1e28]/10 border-l-4 border-[#da1e28] text-xs text-[#ff8389] rounded-none">
					<div class="font-semibold mb-1">Warning: Destructive Reset</div>
					This will reset panel authentication and delete all users, sessions, and invites. Server files and data are preserved.
				</div>
				<CarbonTextInput
					type="password"
					label="Recovery Key"
					placeholder="Paste your emergency recovery key"
					bind:value={recoveryKey}
					disabled={loading}
				/>
				<div class="flex items-center gap-2">
					<CarbonButton
						kind="secondary"
						class="w-1/2 justify-center rounded-none"
						onclick={() => {
							showRecovery = false;
							error = '';
						}}
						disabled={loading}
					>
						Cancel
					</CarbonButton>
					<CarbonButton
						kind="danger"
						class="w-1/2 justify-center rounded-none"
						onclick={handleRecovery}
						disabled={loading || !recoveryKey}
					>
						{#if loading}
							<Loader2 class="mr-2 h-4 w-4 animate-spin" />
							Resetting...
						{:else}
							Reset Panel
						{/if}
					</CarbonButton>
				</div>
			</div>
		{:else if !authStatus.firstUserSetup}
			<div class="mt-6 pt-4 border-t border-[#393939] text-center">
				<button
					type="button"
					class="inline-flex items-center gap-1.5 text-xs text-[#8d8d8d] hover:text-[#f4f4f4] transition-colors cursor-pointer rounded-none"
					onclick={() => (showRecovery = true)}
				>
					<KeyRound class="h-3.5 w-3.5" />
					Forgot access? Recovery
				</button>
			</div>
		{/if}

		{#if $authStore.anonymousAccessEnabled}
			<div class="relative my-4 text-center">
				<div class="absolute inset-0 flex items-center">
					<div class="w-full border-t border-[#393939]"></div>
				</div>
				<span class="relative bg-[#262626] px-3 text-xs uppercase tracking-wider text-[#8d8d8d]">
					Or
				</span>
			</div>
			<CarbonButton kind="ghost" class="w-full justify-center rounded-none" onclick={() => goto(resolve('/'))}>
				Continue as Guest
			</CarbonButton>
		{/if}
	</div>
</div>
