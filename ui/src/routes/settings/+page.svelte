<script lang="ts">
	import {
		getSession,
		getAccessSettings,
		getSessions,
		revokeSession,
		revokeOtherSessions,
		changePassword,
		initSettingsTOTP,
		enableSettingsTOTP,
		disableSettingsTOTP
	} from '#lib/api.js';
	import HookDomainForm from '#lib/components/HookDomainForm.svelte';
	import type {
		SessionInfo,
		AccessSettingsResponse,
		ActiveSessionEntry,
		SetupTOTPInitResponse
	} from '#lib/types/api.js';
	import { onMount } from 'svelte';

	type Tab = 'access' | 'security' | 'hook-domain';

	let tab = $state<Tab>('access');
	let session = $state<SessionInfo | null>(null);
	let access = $state<AccessSettingsResponse | null>(null);
	let sessions = $state<ActiveSessionEntry[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let copied = $state<string | null>(null);

	// Security: password change
	let curPassword = $state('');
	let curTOTPForPass = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let passBusy = $state(false);
	let passMsg = $state<string | null>(null);
	let passErr = $state<string | null>(null);

	// Security: TOTP enable/disable
	let totpInitPassword = $state('');
	let totpInit = $state<SetupTOTPInitResponse | null>(null);
	let totpEnableCode = $state('');
	let totpDisablePassword = $state('');
	let totpDisableCode = $state('');
	let totpBusy = $state(false);
	let totpMsg = $state<string | null>(null);
	let totpErr = $state<string | null>(null);

	// Access: sessions
	let sessBusy = $state(false);
	let sessMsg = $state<string | null>(null);
	let sessErr = $state<string | null>(null);

	async function loadAll() {
		loading = true;
		error = null;
		try {
			session = await getSession();
			if (session.setup_required) {
				window.location.href = '/setup';
				return;
			}
			if (!session.authenticated) {
				window.location.href = '/login';
				return;
			}
			const [accRes, sessRes] = await Promise.all([getAccessSettings(), getSessions()]);
			access = accRes;
			sessions = sessRes.sessions || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load settings';
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		const params = new URLSearchParams(window.location.search);
		const t = params.get('tab');
		if (t === 'security' || t === 'hook-domain' || t === 'access') {
			tab = t;
		}
		loadAll();
	});

	function switchTab(next: Tab) {
		tab = next;
		const url = new URL(window.location.href);
		url.searchParams.set('tab', next);
		history.replaceState({}, '', url.toString());
	}

	async function copyText(key: string, value: string) {
		try {
			await navigator.clipboard.writeText(value);
			copied = key;
			setTimeout(() => {
				if (copied === key) copied = null;
			}, 2000);
		} catch {
			// Clipboard API may be unavailable in headless contexts.
		}
	}

	async function handleRevokeSession(id: string) {
		sessBusy = true;
		sessErr = null;
		sessMsg = null;
		try {
			const res = await revokeSession(id);
			if (res.success) {
				const sessRes = await getSessions();
				sessions = sessRes.sessions || [];
				sessMsg = 'Session revoked.';
			} else {
				sessErr = res.error || 'Failed to revoke session';
			}
		} catch (err) {
			sessErr = err instanceof Error ? err.message : 'Failed to revoke session';
		} finally {
			sessBusy = false;
		}
	}

	async function handleRevokeOthers() {
		sessBusy = true;
		sessErr = null;
		sessMsg = null;
		try {
			const res = await revokeOtherSessions();
			if (res.success) {
				const sessRes = await getSessions();
				sessions = sessRes.sessions || [];
				sessMsg = 'Logged out every other session.';
			} else {
				sessErr = res.error || 'Failed to revoke other sessions';
			}
		} catch (err) {
			sessErr = err instanceof Error ? err.message : 'Failed to revoke other sessions';
		} finally {
			sessBusy = false;
		}
	}

	async function handleChangePassword(e: SubmitEvent) {
		e.preventDefault();
		passErr = null;
		passMsg = null;
		if (newPassword.length < 8) {
			passErr = 'New password must be at least 8 characters long.';
			return;
		}
		if (newPassword !== confirmPassword) {
			passErr = 'New passwords do not match.';
			return;
		}
		passBusy = true;
		try {
			const res = await changePassword({
				current_password: curPassword,
				new_password: newPassword,
				...(session?.totp_enabled ? { totp_code: curTOTPForPass.trim() } : {})
			});
			if (res.success) {
				curPassword = '';
				curTOTPForPass = '';
				newPassword = '';
				confirmPassword = '';
				passMsg = 'Password updated and all other sessions logged out.';
				const sessRes = await getSessions();
				sessions = sessRes.sessions || [];
			} else {
				passErr = res.error || 'Failed to change password';
			}
		} catch (err) {
			passErr = err instanceof Error ? err.message : 'Network error';
		} finally {
			passBusy = false;
		}
	}

	async function handleInitTOTP(e: SubmitEvent) {
		e.preventDefault();
		totpErr = null;
		totpMsg = null;
		totpBusy = true;
		try {
			totpInit = await initSettingsTOTP({ current_password: totpInitPassword });
			totpInitPassword = '';
		} catch (err) {
			totpErr = err instanceof Error ? err.message : 'Failed to start TOTP setup';
		} finally {
			totpBusy = false;
		}
	}

	async function handleEnableTOTP(e: SubmitEvent) {
		e.preventDefault();
		totpErr = null;
		totpMsg = null;
		totpBusy = true;
		try {
			const res = await enableSettingsTOTP({ code: totpEnableCode.trim() });
			if (res.success) {
				totpInit = null;
				totpEnableCode = '';
				totpMsg = 'Two-factor authentication enabled.';
				window.location.reload();
			} else {
				totpErr = res.error || 'Invalid verification code';
			}
		} catch (err) {
			totpErr = err instanceof Error ? err.message : 'Network error';
		} finally {
			totpBusy = false;
		}
	}

	async function handleDisableTOTP(e: SubmitEvent) {
		e.preventDefault();
		totpErr = null;
		totpMsg = null;
		totpBusy = true;
		try {
			const res = await disableSettingsTOTP({
				current_password: totpDisablePassword,
				totp_code: totpDisableCode.trim()
			});
			if (res.success) {
				totpDisablePassword = '';
				totpDisableCode = '';
				totpMsg = 'Two-factor authentication disabled.';
				window.location.reload();
			} else {
				totpErr = res.error || 'Failed to disable TOTP';
			}
		} catch (err) {
			totpErr = err instanceof Error ? err.message : 'Network error';
		} finally {
			totpBusy = false;
		}
	}

	function formatDate(dateStr: string): string {
		try {
			return new Date(dateStr).toLocaleString('en-US', {
				month: 'short',
				day: 'numeric',
				hour: '2-digit',
				minute: '2-digit',
				hour12: false
			});
		} catch {
			return dateStr;
		}
	}
</script>

<svelte:head>
	<title>Settings &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
	<div class="pb-4 border-b border-stone-200 dark:border-stone-800">
		<h1 class="text-2xl font-bold tracking-tight">Settings</h1>
		<p class="text-xs text-stone-600 dark:text-stone-400 mt-1">Panel access, admin security, and webhook domain</p>
	</div>

	<div class="flex flex-wrap gap-1 border-b border-stone-200 dark:border-stone-800 pb-2" role="tablist" aria-label="Settings sections">
		<button
			type="button"
			role="tab"
			aria-selected={tab === 'access'}
			onclick={() => switchTab('access')}
			class="px-3.5 py-2 rounded-md text-xs font-medium transition {tab === 'access'
				? 'bg-stone-900 dark:bg-stone-100 text-white dark:text-stone-900'
				: 'text-stone-700 dark:text-stone-300 hover:bg-stone-100 dark:hover:bg-stone-800'}"
		>
			Access
		</button>
		<button
			type="button"
			role="tab"
			aria-selected={tab === 'security'}
			onclick={() => switchTab('security')}
			class="px-3.5 py-2 rounded-md text-xs font-medium transition {tab === 'security'
				? 'bg-stone-900 dark:bg-stone-100 text-white dark:text-stone-900'
				: 'text-stone-700 dark:text-stone-300 hover:bg-stone-100 dark:hover:bg-stone-800'}"
		>
			Security
		</button>
		<button
			type="button"
			role="tab"
			aria-selected={tab === 'hook-domain'}
			onclick={() => switchTab('hook-domain')}
			class="px-3.5 py-2 rounded-md text-xs font-medium transition {tab === 'hook-domain'
				? 'bg-stone-900 dark:bg-stone-100 text-white dark:text-stone-900'
				: 'text-stone-700 dark:text-stone-300 hover:bg-stone-100 dark:hover:bg-stone-800'}"
		>
			Hook Domain
		</button>
	</div>

	{#if error}
		<div role="alert" class="p-4 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-sm">
			{error}
		</div>
	{:else if loading}
		<div class="py-12 text-center text-sm text-stone-600 dark:text-stone-400">Loading settings...</div>
	{:else if tab === 'access'}
		<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
			<!-- Laptop & Mobile SSH Access (LAP-3) -->
			<section class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-4">
				<div>
					<h2 class="text-base font-semibold">Laptop &amp; Mobile Access</h2>
					<p class="text-xs text-stone-600 dark:text-stone-400 mt-1">
						The panel listens only on <code class="font-mono">127.0.0.1:7070</code> via SSH local port forwarding.
					</p>
				</div>

				{#if access?.laptop_command}
					<div class="space-y-1.5">
						<div class="flex items-center justify-between text-xs">
							<span class="font-medium text-stone-700 dark:text-stone-300">Laptop command (bash, zsh, fish)</span>
							<button type="button" onclick={() => copyText('laptop', access!.laptop_command)} class="text-brand-text font-medium hover:underline">
								{copied === 'laptop' ? 'Copied' : 'Copy'}
							</button>
						</div>
						<pre class="p-3 rounded-md bg-stone-50 dark:bg-stone-950 border border-stone-200 dark:border-stone-800 text-[11px] font-mono overflow-x-auto whitespace-pre-wrap break-all">{access.laptop_command}</pre>
					</div>
				{/if}

				{#if access?.direct_ssh}
					<div class="space-y-1.5">
						<div class="flex items-center justify-between text-xs">
							<span class="font-medium text-stone-700 dark:text-stone-300">Direct SSH tunnel</span>
							<button type="button" onclick={() => copyText('ssh', access!.direct_ssh)} class="text-brand-text font-medium hover:underline">
								{copied === 'ssh' ? 'Copied' : 'Copy'}
							</button>
						</div>
						<code class="block p-2.5 rounded-md bg-stone-50 dark:bg-stone-950 border border-stone-200 dark:border-stone-800 text-xs font-mono break-all">{access.direct_ssh}</code>
					</div>
				{/if}

				<div class="space-y-2 pt-2 border-t border-stone-200 dark:border-stone-800 text-xs text-stone-600 dark:text-stone-400">
					<p><strong class="text-stone-800 dark:text-stone-200">Windows:</strong> {access?.windows_hint}</p>
					<p><strong class="text-stone-800 dark:text-stone-200">Phone:</strong> {access?.mobile_hint}</p>
				</div>
			</section>

			<!-- Active Sessions (ADM-5) -->
			<section class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-4">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<div>
						<h2 class="text-base font-semibold">Active Sessions</h2>
						<p class="text-xs text-stone-600 dark:text-stone-400 mt-1">Idle timeout 12 h, absolute 7 d</p>
					</div>
					{#if sessions.length > 1}
						<button
							type="button"
							onclick={handleRevokeOthers}
							disabled={sessBusy}
							class="px-3 py-1.5 rounded-md border border-red-300 dark:border-red-800 text-red-800 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-950/40 text-xs font-medium transition disabled:opacity-50"
						>
							Log out everywhere else
						</button>
					{/if}
				</div>

				{#if sessErr}
					<div role="alert" class="p-3 rounded-md bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">{sessErr}</div>
				{/if}
				{#if sessMsg}
					<div role="status" class="p-3 rounded-md bg-emerald-50 dark:bg-emerald-950/40 border border-emerald-200 dark:border-emerald-800 text-emerald-800 dark:text-emerald-300 text-xs">{sessMsg}</div>
				{/if}

				<div class="space-y-2.5">
					{#each sessions as s (s.id)}
						<div class="p-3 rounded-lg border border-stone-200 dark:border-stone-800 bg-stone-50/50 dark:bg-stone-950/50 flex flex-col sm:flex-row sm:items-center justify-between gap-2">
							<div class="min-w-0 space-y-0.5">
								<div class="flex items-center gap-2">
									<span class="text-xs font-medium text-stone-900 dark:text-stone-100 truncate">
										{s.user_agent || 'Browser session'}
									</span>
									{#if s.current}
										<span class="px-2 py-0.5 rounded text-[11px] font-medium bg-brand-badge-bg text-brand-badge-text border border-brand-badge-border">
											Current
										</span>
									{/if}
								</div>
								<div class="text-[11px] text-stone-600 dark:text-stone-400">
									Last active: {formatDate(s.last_seen_at)} &bull; Created: {formatDate(s.created_at)}
								</div>
							</div>
							{#if !s.current}
								<button
									type="button"
									onclick={() => handleRevokeSession(s.id)}
									disabled={sessBusy}
									class="px-2.5 py-1 rounded border border-stone-300 dark:border-stone-700 text-xs font-medium text-stone-700 dark:text-stone-300 hover:bg-stone-100 dark:hover:bg-stone-800 shrink-0"
								>
									Revoke
								</button>
							{/if}
						</div>
					{/each}
				</div>
			</section>
		</div>
	{:else if tab === 'security'}
		<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
			<!-- Change Password (ADM-4, SEC-10) -->
			<section class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-4">
				<div>
					<h2 class="text-base font-semibold">Change Admin Password</h2>
					<p class="text-xs text-stone-600 dark:text-stone-400 mt-1">Requires re-authentication. Ends every other active session.</p>
				</div>

				{#if passErr}
					<div role="alert" class="p-3 rounded-md bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">{passErr}</div>
				{/if}
				{#if passMsg}
					<div role="status" class="p-3 rounded-md bg-emerald-50 dark:bg-emerald-950/40 border border-emerald-200 dark:border-emerald-800 text-emerald-800 dark:text-emerald-300 text-xs">{passMsg}</div>
				{/if}

				<form onsubmit={handleChangePassword} class="space-y-3.5">
					<div>
						<label for="cur-password" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">Current Password</label>
						<input id="cur-password" type="password" bind:value={curPassword} required autocomplete="current-password" class="w-full px-3 py-2 text-sm rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
					</div>
					{#if session?.totp_enabled}
						<div>
							<label for="cur-totp-pass" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">Current TOTP Code</label>
							<input id="cur-totp-pass" type="text" inputmode="numeric" maxlength="6" bind:value={curTOTPForPass} required placeholder="123456" class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
					{/if}
					<div>
						<label for="new-password" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">New Password</label>
						<input id="new-password" type="password" bind:value={newPassword} required minlength="8" autocomplete="new-password" class="w-full px-3 py-2 text-sm rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
					</div>
					<div>
						<label for="confirm-new-password" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">Confirm New Password</label>
						<input id="confirm-new-password" type="password" bind:value={confirmPassword} required minlength="8" autocomplete="new-password" class="w-full px-3 py-2 text-sm rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
					</div>
					<button type="submit" disabled={passBusy} class="py-2 px-4 rounded-md bg-brand-primary hover:bg-brand-hover text-brand-btn-text text-sm font-medium disabled:opacity-50">
						{passBusy ? 'Updating...' : 'Update Password'}
					</button>
				</form>
			</section>

			<!-- TOTP Management (ADM-4, SEC-10) -->
			<section class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-4">
				<div>
					<h2 class="text-base font-semibold">Two-Factor Authentication (TOTP)</h2>
					<p class="text-xs text-stone-600 dark:text-stone-400 mt-1">
						Status: {session?.totp_enabled ? 'Enabled' : 'Disabled'}
					</p>
				</div>

				{#if totpErr}
					<div role="alert" class="p-3 rounded-md bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">{totpErr}</div>
				{/if}
				{#if totpMsg}
					<div role="status" class="p-3 rounded-md bg-emerald-50 dark:bg-emerald-950/40 border border-emerald-200 dark:border-emerald-800 text-emerald-800 dark:text-emerald-300 text-xs">{totpMsg}</div>
				{/if}

				{#if session?.totp_enabled}
					<form onsubmit={handleDisableTOTP} class="space-y-3.5">
						<p class="text-xs text-stone-600 dark:text-stone-400">Disabling TOTP requires both your current password and a current 6-digit authenticator code.</p>
						<div>
							<label for="disable-totp-pass" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">Current Password</label>
							<input id="disable-totp-pass" type="password" bind:value={totpDisablePassword} required class="w-full px-3 py-2 text-sm rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
						<div>
							<label for="disable-totp-code" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">Current TOTP Code</label>
							<input id="disable-totp-code" type="text" inputmode="numeric" maxlength="6" bind:value={totpDisableCode} required placeholder="123456" class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
						<button type="submit" disabled={totpBusy} class="py-2 px-4 rounded-md border border-red-300 dark:border-red-800 text-red-800 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-950/40 text-sm font-medium disabled:opacity-50">
							{totpBusy ? 'Disabling...' : 'Disable TOTP'}
						</button>
					</form>
				{:else if totpInit}
					<div class="space-y-4">
						<div class="p-2 bg-white rounded-md border border-stone-200 w-fit mx-auto">
							{@html totpInit.qr_svg}
						</div>
						<code class="block p-2 rounded bg-stone-50 dark:bg-stone-950 border border-stone-200 dark:border-stone-800 text-xs font-mono break-all select-all">{totpInit.secret}</code>
						<form onsubmit={handleEnableTOTP} class="space-y-3">
							<div>
								<label for="enable-totp-code" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">6-Digit Verification Code</label>
								<input id="enable-totp-code" type="text" inputmode="numeric" maxlength="6" bind:value={totpEnableCode} required placeholder="123456" class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
							</div>
							<button type="submit" disabled={totpBusy || totpEnableCode.trim().length !== 6} class="py-2 px-4 rounded-md bg-brand-primary hover:bg-brand-hover text-brand-btn-text text-sm font-medium disabled:opacity-50">
								{totpBusy ? 'Verifying...' : 'Enable TOTP'}
							</button>
						</form>
					</div>
				{:else}
					<form onsubmit={handleInitTOTP} class="space-y-3.5">
						<div>
							<label for="init-totp-pass" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">Confirm Current Password</label>
							<input id="init-totp-pass" type="password" bind:value={totpInitPassword} required class="w-full px-3 py-2 text-sm rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
						<button type="submit" disabled={totpBusy} class="py-2 px-4 rounded-md bg-brand-primary hover:bg-brand-hover text-brand-btn-text text-sm font-medium disabled:opacity-50">
							{totpBusy ? 'Generating...' : 'Set Up TOTP'}
						</button>
					</form>
				{/if}
			</section>
		</div>
	{:else if tab === 'hook-domain'}
		<section class="max-w-xl bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-4">
			<h2 class="text-base font-semibold">Reserved Hook Domain</h2>
			<HookDomainForm onSaved={() => loadAll()} />
		</section>
	{/if}
</div>
