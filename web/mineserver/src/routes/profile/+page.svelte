<script lang="ts">
	import { authStore, currentUser } from '$lib/stores/auth';
	import {
		CarbonButton,
		CarbonTile,
		CarbonTag,
		CarbonDataTable,
		CarbonModal,
		CarbonTextInput,
		CarbonSelect
	} from '$lib/components/carbon';
	import { toast } from 'svelte-sonner';
	import {
		User,
		Key,
		Loader2,
		Mail,
		Calendar,
		Clock,
		Shield,
		Activity,
		Plus,
		Trash2,
		Copy,
		Check,
		AlertTriangle,
		KeyRound
	} from '@lucide/svelte';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { onMount } from 'svelte';
	import type { ApiToken } from '$lib/proto/mineserver/v1/auth_pb';

	let user = $derived($currentUser);
	let passwordForm = $state({
		oldPassword: '',
		newPassword: '',
		confirmPassword: ''
	});
	let saving = $state(false);

	// API Tokens state
	let apiTokens = $state<ApiToken[]>([]);
	let loadingTokens = $state(false);
	let showCreateTokenDialog = $state(false);
	let creatingToken = $state(false);
	let newTokenForm = $state({ name: '', expiresInDays: '' as string });
	let createdToken = $state<string | null>(null);
	let copied = $state(false);
	let deletingTokenId = $state<string | null>(null);

	let initials = $derived(
		user?.username
			? user.username
					.split(/[\s_-]+/)
					.slice(0, 2)
					.map((w) => w[0]?.toUpperCase() ?? '')
					.join('')
			: '?'
	);

	let primaryRole = $derived(user?.roles?.[0] ?? 'user');

	function getRoleTagType(role: string): 'blue' | 'purple' | 'cyan' | 'green' | 'gray' {
		const r = (role || '').toLowerCase();
		if (r === 'admin' || r === 'superadmin') return 'purple';
		if (r === 'operator' || r === 'mod') return 'blue';
		if (r === 'user') return 'cyan';
		return 'gray';
	}

	let memberSince = $derived(
		user?.createdAt
			? new Date(Number(user.createdAt.seconds) * 1000).toLocaleDateString(undefined, {
					year: 'numeric',
					month: 'long',
					day: 'numeric'
				})
			: 'Unknown'
	);

	let lastActive = $derived(
		user?.lastLogin
			? new Date(Number(user.lastLogin.seconds) * 1000).toLocaleString(undefined, {
					year: 'numeric',
					month: 'short',
					day: 'numeric',
					hour: '2-digit',
					minute: '2-digit'
				})
			: null
	);

	let providerLabel = $derived((user?.authProvider || 'local').toUpperCase());

	onMount(() => {
		loadTokens();
	});

	async function loadTokens() {
		loadingTokens = true;
		try {
			const resp = await rpcClient.auth.listAPITokens({}, silentCallOptions);
			apiTokens = resp.apiTokens;
		} catch {
			// silently fail - tokens will show empty
		} finally {
			loadingTokens = false;
		}
	}

	async function createToken() {
		if (!newTokenForm.name.trim()) {
			toast.error('Token name is required');
			return;
		}

		creatingToken = true;
		try {
			const days = newTokenForm.expiresInDays ? parseInt(newTokenForm.expiresInDays) : undefined;
			const resp = await rpcClient.auth.createAPIToken({
				name: newTokenForm.name.trim(),
				expiresInDays: days
			});
			createdToken = resp.plaintextToken;
			toast.success('API token created');
			await loadTokens();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to create API token');
		} finally {
			creatingToken = false;
		}
	}

	async function deleteToken(id: string) {
		deletingTokenId = id;
		try {
			await rpcClient.auth.deleteAPIToken({ id });
			toast.success('API token deleted');
			await loadTokens();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to delete API token');
		} finally {
			deletingTokenId = null;
		}
	}

	async function copyToken() {
		if (!createdToken) return;
		try {
			await navigator.clipboard.writeText(createdToken);
			copied = true;
			toast.success('Token copied to clipboard');
			setTimeout(() => {
				copied = false;
			}, 2000);
		} catch {
			toast.error('Failed to copy token');
		}
	}

	function closeCreateDialog() {
		showCreateTokenDialog = false;
		createdToken = null;
		copied = false;
		newTokenForm = { name: '', expiresInDays: '' };
	}

	function formatTimestamp(ts: { seconds: bigint } | undefined): string {
		if (!ts) return 'Never';
		return new Date(Number(ts.seconds) * 1000).toLocaleDateString(undefined, {
			year: 'numeric',
			month: 'short',
			day: 'numeric'
		});
	}

	function isExpired(ts: { seconds: bigint } | undefined): boolean {
		if (!ts) return false;
		return new Date(Number(ts.seconds) * 1000) < new Date();
	}

	async function changePassword() {
		if (!passwordForm.oldPassword || !passwordForm.newPassword) {
			toast.error('Please fill in all fields');
			return;
		}

		if (passwordForm.newPassword !== passwordForm.confirmPassword) {
			toast.error('New passwords do not match');
			return;
		}

		if (passwordForm.newPassword.length < 8) {
			toast.error('New password must be at least 8 characters');
			return;
		}

		saving = true;
		try {
			await authStore.changePassword(passwordForm.oldPassword, passwordForm.newPassword);
			toast.success('Password changed successfully');
			passwordForm = {
				oldPassword: '',
				newPassword: '',
				confirmPassword: ''
			};
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to change password');
		} finally {
			saving = false;
		}
	}
</script>

<svelte:head>
	<title>Profile - MineServer</title>
</svelte:head>

<div class="flex-1 space-y-6 font-sans text-[#f4f4f4] rounded-none">
	{#if user}
		<!-- Header with Sharp Avatar -->
		<div class="flex items-center gap-6 border-b border-[#393939] pb-6 rounded-none">
			<div class="flex h-16 w-16 items-center justify-center bg-[#0f62fe] text-white font-mono text-2xl font-bold rounded-none shadow-lg">
				{initials}
			</div>
			<div class="space-y-1">
				<div class="flex items-center gap-3">
					<h1 class="text-3xl font-semibold tracking-tight text-white">{user.username}</h1>
					<CarbonTag type={getRoleTagType(primaryRole)} size="md">{primaryRole}</CarbonTag>
				</div>
				<p class="text-sm text-[#a8a8a8]">Manage your user account credentials, security, and API tokens</p>
			</div>
		</div>

		<div class="grid gap-6 md:grid-cols-2">
			<!-- Account Information Tile -->
			<CarbonTile class="rounded-none">
				<div class="flex items-center gap-3 pb-4 border-b border-[#393939] mb-4">
					<div class="h-8 w-8 bg-[#393939] text-[#0f62fe] flex items-center justify-center rounded-none">
						<User class="h-4 w-4" />
					</div>
					<div>
						<h2 class="text-base font-semibold text-white">Account Information</h2>
						<p class="text-xs text-[#a8a8a8]">Identity and profile configuration</p>
					</div>
				</div>

				<div class="space-y-3">
					<!-- Username -->
					<div class="flex items-center justify-between p-3 bg-[#161616] border border-[#393939] rounded-none">
						<div class="flex items-center gap-2 text-xs text-[#c6c6c6]">
							<User class="h-3.5 w-3.5 text-[#a8a8a8]" />
							<span>Username</span>
						</div>
						<span class="text-sm font-medium text-white font-mono">{user.username}</span>
					</div>

					<!-- Auth Provider -->
					<div class="flex items-center justify-between p-3 bg-[#161616] border border-[#393939] rounded-none">
						<div class="flex items-center gap-2 text-xs text-[#c6c6c6]">
							<Shield class="h-3.5 w-3.5 text-[#a8a8a8]" />
							<span>Auth Provider</span>
						</div>
						<CarbonTag type="blue" size="sm">{providerLabel}</CarbonTag>
					</div>

					<!-- Email -->
					{#if user.email}
						<div class="flex items-center justify-between p-3 bg-[#161616] border border-[#393939] rounded-none">
							<div class="flex items-center gap-2 text-xs text-[#c6c6c6]">
								<Mail class="h-3.5 w-3.5 text-[#a8a8a8]" />
								<span>Email</span>
							</div>
							<span class="text-sm text-white font-mono">{user.email}</span>
						</div>
					{/if}

					<!-- Roles -->
					<div class="flex items-center justify-between p-3 bg-[#161616] border border-[#393939] rounded-none">
						<div class="flex items-center gap-2 text-xs text-[#c6c6c6]">
							<Shield class="h-3.5 w-3.5 text-[#a8a8a8]" />
							<span>Roles</span>
						</div>
						<div class="flex flex-wrap gap-1">
							{#each user.roles || [] as role (role)}
								<CarbonTag type={getRoleTagType(role)} size="sm">{role}</CarbonTag>
							{/each}
							{#if !user.roles?.length}
								<span class="text-xs text-[#8d8d8d]">No roles</span>
							{/if}
						</div>
					</div>

					<!-- Member Since -->
					<div class="flex items-center justify-between p-3 bg-[#161616] border border-[#393939] rounded-none">
						<div class="flex items-center gap-2 text-xs text-[#c6c6c6]">
							<Calendar class="h-3.5 w-3.5 text-[#a8a8a8]" />
							<span>Member Since</span>
						</div>
						<span class="text-xs text-[#c6c6c6]">{memberSince}</span>
					</div>

					<!-- Last Active -->
					{#if lastActive}
						<div class="flex items-center justify-between p-3 bg-[#161616] border border-[#393939] rounded-none">
							<div class="flex items-center gap-2 text-xs text-[#c6c6c6]">
								<Clock class="h-3.5 w-3.5 text-[#a8a8a8]" />
								<span>Last Active</span>
							</div>
							<span class="text-xs text-[#c6c6c6]">{lastActive}</span>
						</div>
					{/if}

					<!-- Account Status -->
					<div class="flex items-center justify-between p-3 bg-[#161616] border border-[#393939] rounded-none">
						<div class="flex items-center gap-2 text-xs text-[#c6c6c6]">
							<Activity class="h-3.5 w-3.5 text-[#a8a8a8]" />
							<span>Account Status</span>
						</div>
						<CarbonTag type={user.isActive ? 'green' : 'red'} size="sm">
							{user.isActive ? 'Active' : 'Inactive'}
						</CarbonTag>
					</div>
				</div>
			</CarbonTile>

			<!-- Security Tile -->
			<CarbonTile class="rounded-none">
				<div class="flex items-center gap-3 pb-4 border-b border-[#393939] mb-4">
					<div class="h-8 w-8 bg-[#393939] text-[#0f62fe] flex items-center justify-center rounded-none">
						<Key class="h-4 w-4" />
					</div>
					<div>
						<h2 class="text-base font-semibold text-white">Security & Password</h2>
						<p class="text-xs text-[#a8a8a8]">Manage credentials and session access</p>
					</div>
				</div>

				<div class="space-y-4">
					<div class="flex items-center justify-between p-3 bg-[#161616] border border-[#393939] rounded-none">
						<span class="text-xs text-[#c6c6c6]">Active Session Provider</span>
						<CarbonTag type="blue" size="sm">{providerLabel}</CarbonTag>
					</div>

					{#if user.authProvider === 'local' || !user.authProvider}
						<div class="pt-2">
							<h3 class="text-sm font-semibold text-white mb-3">Change Password</h3>
							<form
								onsubmit={(e) => {
									e.preventDefault();
									changePassword();
								}}
								class="space-y-3"
							>
								<CarbonTextInput
									type="password"
									label="Current Password"
									bind:value={passwordForm.oldPassword}
									required
									disabled={saving}
								/>
								<CarbonTextInput
									type="password"
									label="New Password"
									placeholder="Minimum 8 characters"
									bind:value={passwordForm.newPassword}
									required
									disabled={saving}
								/>
								<CarbonTextInput
									type="password"
									label="Confirm New Password"
									placeholder="Confirm your new password"
									bind:value={passwordForm.confirmPassword}
									required
									disabled={saving}
								/>
								<CarbonButton type="submit" disabled={saving} class="w-full justify-center rounded-none">
									{#if saving}
										<Loader2 class="mr-2 h-4 w-4 animate-spin" />
										Updating Password...
									{:else}
										<Key class="mr-2 h-4 w-4" />
										Update Password
									{/if}
								</CarbonButton>
							</form>
						</div>
					{:else}
						<div class="p-4 border border-dashed border-[#525252] bg-[#161616] text-center rounded-none">
							<Key class="mx-auto mb-2 h-6 w-6 text-[#8d8d8d]" />
							<p class="text-xs text-[#a8a8a8]">
								Your account is authenticated via <span class="font-semibold text-white">{providerLabel}</span> SSO. Password changes must be performed through your identity provider.
							</p>
						</div>
					{/if}
				</div>
			</CarbonTile>
		</div>

		<!-- API Tokens Table in Carbon Tile -->
		{#snippet tokenToolbar()}
			<CarbonButton
				size="sm"
				class="rounded-none"
				onclick={() => (showCreateTokenDialog = true)}
			>
				<Plus class="mr-1.5 h-3.5 w-3.5" />
				Create Token
			</CarbonButton>
		{/snippet}

		{#snippet tokenHeader()}
			<tr>
				<th class="py-3 px-4 font-semibold text-xs uppercase tracking-wider">Name</th>
				<th class="py-3 px-4 font-semibold text-xs uppercase tracking-wider">Created</th>
				<th class="py-3 px-4 font-semibold text-xs uppercase tracking-wider">Expires</th>
				<th class="py-3 px-4 font-semibold text-xs uppercase tracking-wider">Last Used</th>
				<th class="py-3 px-4 font-semibold text-xs uppercase tracking-wider text-right w-20">Action</th>
			</tr>
		{/snippet}

		<CarbonTile class="p-0 rounded-none overflow-hidden">
			<CarbonDataTable
				title="API Tokens"
				description="Programmatic bearer tokens that inherit your user identity and permissions"
				toolbar={tokenToolbar}
				header={tokenHeader}
				class="border-0"
			>
				{#if loadingTokens}
					<tr>
						<td colspan="5" class="py-12 text-center text-[#8d8d8d]">
							<Loader2 class="mx-auto h-6 w-6 animate-spin text-[#0f62fe] mb-2" />
							Loading API tokens...
						</td>
					</tr>
				{:else if apiTokens.length === 0}
					<tr>
						<td colspan="5" class="py-12 text-center text-[#8d8d8d]">
							<KeyRound class="mx-auto mb-2 h-8 w-8 text-[#525252]" />
							<p class="text-sm font-medium text-[#c6c6c6]">No API tokens generated</p>
							<p class="text-xs text-[#8d8d8d] mt-1">
								Generate a token to interact with the MineServer gRPC/Connect API programmatically.
							</p>
						</td>
					</tr>
				{:else}
					{#each apiTokens as token (token.id)}
						<tr class="hover:bg-[#353535] transition-colors">
							<td class="py-3 px-4 font-medium text-white">
								<div class="flex items-center gap-2">
									<KeyRound class="h-3.5 w-3.5 text-[#0f62fe] shrink-0" />
									<span class="font-mono text-sm">{token.name}</span>
								</div>
							</td>
							<td class="py-3 px-4 text-xs text-[#a8a8a8] font-mono">
								{formatTimestamp(token.createdAt)}
							</td>
							<td class="py-3 px-4">
								{#if token.expiresAt}
									<CarbonTag type={isExpired(token.expiresAt) ? 'red' : 'gray'} size="sm">
										{isExpired(token.expiresAt) ? 'Expired' : formatTimestamp(token.expiresAt)}
									</CarbonTag>
								{:else}
									<CarbonTag type="gray" size="sm">Never</CarbonTag>
								{/if}
							</td>
							<td class="py-3 px-4 text-xs text-[#a8a8a8] font-mono">
								{formatTimestamp(token.lastUsedAt)}
							</td>
							<td class="py-3 px-4 text-right">
								<CarbonButton
									kind="ghost"
									size="sm"
									iconOnly
									class="text-[#da1e28] hover:bg-[#da1e28]/20 rounded-none"
									onclick={() => deleteToken(token.id)}
									disabled={deletingTokenId === token.id}
									title="Delete token"
								>
									{#if deletingTokenId === token.id}
										<Loader2 class="h-3.5 w-3.5 animate-spin" />
									{:else}
										<Trash2 class="h-3.5 w-3.5" />
									{/if}
								</CarbonButton>
							</td>
						</tr>
					{/each}
				{/if}
			</CarbonDataTable>
		</CarbonTile>
	{/if}
</div>

<!-- Create API Token Modal with Carbon Design System Fidelity -->
<CarbonModal
	bind:open={showCreateTokenDialog}
	title={createdToken ? 'Token Created Successfully' : 'Create New API Token'}
	description={createdToken ? 'Copy your token now — it will not be displayed again' : 'Provision a new programmatic access token'}
	hasFooter={false}
	onclose={closeCreateDialog}
	size="3xl"
>
	{#if createdToken}
		<div class="space-y-4">
			<div class="p-4 bg-[#0f62fe]/10 border-l-4 border-[#0f62fe] text-xs text-white space-y-2 rounded-none">
				<div class="flex items-center gap-2 text-[#78a9ff] font-semibold">
					<Check class="h-4 w-4" />
					<span>API Token Generated</span>
				</div>
				<div class="relative mt-2">
					<div class="p-3 bg-[#161616] border border-[#393939] font-mono text-xs text-[#6fdc8c] break-all select-all pr-12 rounded-none">
						{createdToken}
					</div>
					<button
						type="button"
						onclick={copyToken}
						class="absolute top-2 right-2 p-1.5 bg-[#262626] hover:bg-[#353535] text-[#f4f4f4] border border-[#393939] cursor-pointer rounded-none transition-colors"
						title="Copy Token"
					>
						{#if copied}
							<Check class="h-4 w-4 text-[#6fdc8c]" />
						{:else}
							<Copy class="h-4 w-4" />
						{/if}
					</button>
				</div>
			</div>

			<div class="p-3 bg-[#da1e28]/10 border-l-4 border-[#da1e28] text-xs text-[#ff8389] flex items-start gap-2 rounded-none">
				<AlertTriangle class="h-4 w-4 shrink-0 mt-0.5" />
				<div>
					<p class="font-semibold">Save this secret now</p>
					<p class="text-[#c6c6c6] mt-0.5">This bearer token will never be displayed again. Store it securely in your secrets manager or CI/CD environment.</p>
				</div>
			</div>

			<div class="space-y-1.5 pt-2">
				<span class="text-xs font-semibold text-[#c6c6c6]">Example Usage</span>
				<pre class="overflow-x-auto p-3 bg-[#161616] border border-[#393939] font-mono text-xs text-[#a8a8a8] rounded-none">curl {typeof window !== 'undefined' ? window.location.origin : ''}/mineserver.v1.UserService/ListUsers \
  -X POST \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer {createdToken}' \
  -d '{'{}'}'</pre>
			</div>

			<div class="flex items-center justify-end gap-3 pt-4 border-t border-[#393939]">
				<CarbonButton
					kind="secondary"
					onclick={copyToken}
					class="rounded-none"
				>
					{#if copied}
						<Check class="mr-2 h-4 w-4" />
						Copied!
					{:else}
						<Copy class="mr-2 h-4 w-4" />
						Copy Token
					{/if}
				</CarbonButton>
				<CarbonButton
					kind="primary"
					onclick={closeCreateDialog}
					class="rounded-none"
				>
					Done
				</CarbonButton>
			</div>
		</div>
	{:else}
		<div class="space-y-4">
			<CarbonTextInput
				label="Token Name *"
				placeholder="e.g. CI/CD Deployment, Monitoring Bot"
				bind:value={newTokenForm.name}
				disabled={creatingToken}
				helperText="A descriptive identifier for tracking this token's purpose"
			/>

			<CarbonSelect
				label="Expiration Duration"
				bind:value={newTokenForm.expiresInDays}
				disabled={creatingToken}
				helperText={newTokenForm.expiresInDays ? `Token will expire after ${newTokenForm.expiresInDays} days` : 'Token will never expire unless revoked'}
			>
				<option value="">No expiration (Never)</option>
				<option value="7">7 days</option>
				<option value="30">30 days</option>
				<option value="90">90 days</option>
				<option value="365">1 year (365 days)</option>
			</CarbonSelect>

			<div class="p-3 bg-[#161616] border-l-4 border-[#0f62fe] text-xs text-[#c6c6c6] space-y-1 rounded-none">
				<p class="font-semibold text-white">Security note:</p>
				<p>This token inherits all permissions and roles granted to your account ({user?.username}). Treat it with the same confidentiality as your password.</p>
			</div>

			<div class="flex items-center justify-end gap-3 pt-4 border-t border-[#393939]">
				<CarbonButton
					kind="secondary"
					onclick={closeCreateDialog}
					disabled={creatingToken}
					class="rounded-none"
				>
					Cancel
				</CarbonButton>
				<CarbonButton
					kind="primary"
					onclick={createToken}
					disabled={creatingToken || !newTokenForm.name.trim()}
					class="rounded-none"
				>
					{#if creatingToken}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Creating...
					{:else}
						<KeyRound class="mr-2 h-4 w-4" />
						Create Token
					{/if}
				</CarbonButton>
			</div>
		</div>
	{/if}
</CarbonModal>
