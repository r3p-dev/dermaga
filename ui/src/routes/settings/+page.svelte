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
	} from '#lib/api.js'
	import HookDomainForm from '#lib/components/HookDomainForm.svelte'
	import GitHubSettings from '#lib/components/GitHubSettings.svelte'
	import PageHeader from '#lib/components/PageHeader.svelte'
	import StatusBadge from '#lib/components/StatusBadge.svelte'
	import SubNav, { type SubNavItem } from '#lib/components/SubNav.svelte'
	import type {
		SessionInfo,
		AccessSettingsResponse,
		ActiveSessionEntry,
		SetupTOTPInitResponse
	} from '#lib/types/api.js'
	import { onMount } from 'svelte'

	type Tab = 'access' | 'security' | 'hook-domain' | 'github'

	const settingsTabs: SubNavItem[] = [
		{ id: 'access', label: 'Access' },
		{ id: 'security', label: 'Security' },
		{ id: 'hook-domain', label: 'Hook Domain' },
		{ id: 'github', label: 'GitHub' }
	]

	let tab = $state<Tab>('access')
	let session = $state<SessionInfo | null>(null)
	let access = $state<AccessSettingsResponse | null>(null)
	let sessions = $state<ActiveSessionEntry[]>([])
	let loading = $state(true)
	let error = $state<string | null>(null)
	let copied = $state<string | null>(null)

	// Security: password change
	let curPassword = $state('')
	let curTOTPForPass = $state('')
	let newPassword = $state('')
	let confirmPassword = $state('')
	let passBusy = $state(false)
	let passMsg = $state<string | null>(null)
	let passErr = $state<string | null>(null)

	// Security: TOTP enable/disable
	let totpInitPassword = $state('')
	let totpInit = $state<SetupTOTPInitResponse | null>(null)
	let totpEnableCode = $state('')
	let totpDisablePassword = $state('')
	let totpDisableCode = $state('')
	let totpBusy = $state(false)
	let totpMsg = $state<string | null>(null)
	let totpErr = $state<string | null>(null)

	// Access: sessions
	let sessBusy = $state(false)
	let sessMsg = $state<string | null>(null)
	let sessErr = $state<string | null>(null)

	async function loadAll() {
		loading = true
		error = null
		try {
			session = await getSession()
			if (session.setup_required) {
				window.location.href = '/setup'
				return
			}
			if (!session.authenticated) {
				window.location.href = '/login'
				return
			}
			const [accRes, sessRes] = await Promise.all([
				getAccessSettings(),
				getSessions()
			])
			access = accRes
			sessions = sessRes.sessions || []
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load settings'
		} finally {
			loading = false
		}
	}

	onMount(() => {
		const params = new URLSearchParams(window.location.search)
		const t = params.get('tab')
		if (
			t === 'security' ||
			t === 'hook-domain' ||
			t === 'github' ||
			t === 'access'
		) {
			tab = t
		}
		loadAll()
	})

	function switchTab(next: Tab) {
		tab = next
		const url = new URL(window.location.href)
		url.searchParams.set('tab', next)
		history.replaceState({}, '', url.toString())
	}

	async function copyText(key: string, value: string) {
		try {
			await navigator.clipboard.writeText(value)
			copied = key
			setTimeout(() => {
				if (copied === key) copied = null
			}, 2000)
		} catch {
			// Clipboard API may be unavailable in headless contexts.
		}
	}

	async function handleRevokeSession(id: string) {
		sessBusy = true
		sessErr = null
		sessMsg = null
		try {
			const res = await revokeSession(id)
			if (res.success) {
				const sessRes = await getSessions()
				sessions = sessRes.sessions || []
				sessMsg = 'Session revoked.'
			} else {
				sessErr = res.error || 'Failed to revoke session'
			}
		} catch (err) {
			sessErr = err instanceof Error ? err.message : 'Failed to revoke session'
		} finally {
			sessBusy = false
		}
	}

	async function handleRevokeOthers() {
		sessBusy = true
		sessErr = null
		sessMsg = null
		try {
			const res = await revokeOtherSessions()
			if (res.success) {
				const sessRes = await getSessions()
				sessions = sessRes.sessions || []
				sessMsg = 'Logged out every other session.'
			} else {
				sessErr = res.error || 'Failed to revoke other sessions'
			}
		} catch (err) {
			sessErr =
				err instanceof Error ? err.message : 'Failed to revoke other sessions'
		} finally {
			sessBusy = false
		}
	}

	async function handleChangePassword(e: SubmitEvent) {
		e.preventDefault()
		passErr = null
		passMsg = null
		if (newPassword.length < 8) {
			passErr = 'New password must be at least 8 characters long.'
			return
		}
		if (newPassword !== confirmPassword) {
			passErr = 'New passwords do not match.'
			return
		}
		passBusy = true
		try {
			const res = await changePassword({
				current_password: curPassword,
				new_password: newPassword,
				...(session?.totp_enabled ? { totp_code: curTOTPForPass.trim() } : {})
			})
			if (res.success) {
				curPassword = ''
				curTOTPForPass = ''
				newPassword = ''
				confirmPassword = ''
				passMsg = 'Password updated and all other sessions logged out.'
				const sessRes = await getSessions()
				sessions = sessRes.sessions || []
			} else {
				passErr = res.error || 'Failed to change password'
			}
		} catch (err) {
			passErr = err instanceof Error ? err.message : 'Network error'
		} finally {
			passBusy = false
		}
	}

	async function handleInitTOTP(e: SubmitEvent) {
		e.preventDefault()
		totpErr = null
		totpMsg = null
		totpBusy = true
		try {
			totpInit = await initSettingsTOTP({ current_password: totpInitPassword })
			totpInitPassword = ''
		} catch (err) {
			totpErr =
				err instanceof Error ? err.message : 'Failed to start TOTP setup'
		} finally {
			totpBusy = false
		}
	}

	async function handleEnableTOTP(e: SubmitEvent) {
		e.preventDefault()
		totpErr = null
		totpMsg = null
		totpBusy = true
		try {
			const res = await enableSettingsTOTP({ code: totpEnableCode.trim() })
			if (res.success) {
				totpInit = null
				totpEnableCode = ''
				totpMsg = 'Two-factor authentication enabled.'
				window.location.reload()
			} else {
				totpErr = res.error || 'Invalid verification code'
			}
		} catch (err) {
			totpErr = err instanceof Error ? err.message : 'Network error'
		} finally {
			totpBusy = false
		}
	}

	async function handleDisableTOTP(e: SubmitEvent) {
		e.preventDefault()
		totpErr = null
		totpMsg = null
		totpBusy = true
		try {
			const res = await disableSettingsTOTP({
				current_password: totpDisablePassword,
				totp_code: totpDisableCode.trim()
			})
			if (res.success) {
				totpDisablePassword = ''
				totpDisableCode = ''
				totpMsg = 'Two-factor authentication disabled.'
				window.location.reload()
			} else {
				totpErr = res.error || 'Failed to disable TOTP'
			}
		} catch (err) {
			totpErr = err instanceof Error ? err.message : 'Network error'
		} finally {
			totpBusy = false
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
			})
		} catch {
			return dateStr
		}
	}
</script>

<svelte:head>
	<title>Settings &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
	<PageHeader
		title="Settings"
		subtitle="Panel access, admin security, and webhook domain"
	/>

	<div class="flex flex-col items-start gap-6 md:flex-row">
		<SubNav
			items={settingsTabs}
			active={tab}
			onSelect={(id) => switchTab(id as Tab)}
			ariaLabel="Settings sections"
		/>

		<div class="w-full min-w-0 flex-1 space-y-4">
			{#if error}
				<div
					role="alert"
					class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
				>
					{error}
				</div>
			{:else if loading}
				<div
					class="py-12 text-center text-sm text-stone-600 dark:text-stone-400"
				>
					Loading settings...
				</div>
			{:else if tab === 'access'}
				<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
					<!-- Laptop & Mobile SSH Access (LAP-3) -->
					<section
						class="space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
					>
						<div>
							<h2 class="text-base font-semibold">
								Laptop &amp; Mobile Access
							</h2>
							<p class="mt-1 text-xs text-stone-600 dark:text-stone-400">
								The panel listens only on <code class="font-mono">
									127.0.0.1:7070
								</code> via SSH local port forwarding.
							</p>
						</div>

						{#if access?.laptop_command}
							<div class="space-y-1.5">
								<div class="flex items-center justify-between text-xs">
									<span class="font-medium text-stone-700 dark:text-stone-300">
										Laptop command (bash, zsh, fish)
									</span>
									<button
										type="button"
										onclick={() => copyText('laptop', access!.laptop_command)}
										class="font-medium text-brand-text hover:underline"
									>
										{copied === 'laptop' ? 'Copied' : 'Copy'}
									</button>
								</div>
								<pre
									class="overflow-x-auto rounded-md border border-stone-200 bg-stone-50 p-3 font-mono text-[11px] break-all whitespace-pre-wrap dark:border-stone-800 dark:bg-stone-950">{access.laptop_command}</pre>
							</div>
						{/if}

						{#if access?.direct_ssh}
							<div class="space-y-1.5">
								<div class="flex items-center justify-between text-xs">
									<span class="font-medium text-stone-700 dark:text-stone-300">
										Direct SSH tunnel
									</span>
									<button
										type="button"
										onclick={() => copyText('ssh', access!.direct_ssh)}
										class="font-medium text-brand-text hover:underline"
									>
										{copied === 'ssh' ? 'Copied' : 'Copy'}
									</button>
								</div>
								<code
									class="block rounded-md border border-stone-200 bg-stone-50 p-2.5 font-mono text-xs break-all dark:border-stone-800 dark:bg-stone-950"
								>
									{access.direct_ssh}
								</code>
							</div>
						{/if}

						<div
							class="space-y-2 border-t border-stone-200 pt-2 text-xs text-stone-600 dark:border-stone-800 dark:text-stone-400"
						>
							<p>
								<strong class="text-stone-800 dark:text-stone-200">
									Windows:
								</strong>
								{access?.windows_hint}
							</p>
							<p>
								<strong class="text-stone-800 dark:text-stone-200">
									Phone:
								</strong>
								{access?.mobile_hint}
							</p>
						</div>
					</section>

					<!-- Active Sessions (ADM-5) -->
					<section
						class="space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
					>
						<div class="flex flex-wrap items-center justify-between gap-2">
							<div>
								<h2 class="text-base font-semibold">Active Sessions</h2>
								<p class="mt-1 text-xs text-stone-600 dark:text-stone-400">
									Idle timeout 12 h, absolute 7 d
								</p>
							</div>
							{#if sessions.length > 1}
								<button
									type="button"
									onclick={handleRevokeOthers}
									disabled={sessBusy}
									class="rounded-md border border-red-300 px-3 py-1.5 text-xs font-medium text-red-800 transition hover:bg-red-50 disabled:opacity-50 dark:border-red-800 dark:text-red-300 dark:hover:bg-red-950/40"
								>
									Log out everywhere else
								</button>
							{/if}
						</div>

						{#if sessErr}
							<div
								role="alert"
								class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
							>
								{sessErr}
							</div>
						{/if}
						{#if sessMsg}
							<div
								role="status"
								class="rounded-md border border-emerald-200 bg-emerald-50 p-3 text-xs text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300"
							>
								{sessMsg}
							</div>
						{/if}

						<div class="space-y-2.5">
							{#each sessions as s (s.id)}
								<div
									class="flex flex-col justify-between gap-2 rounded-lg border border-stone-200 bg-stone-50/50 p-3 sm:flex-row sm:items-center dark:border-stone-800 dark:bg-stone-950/50"
								>
									<div class="min-w-0 space-y-0.5">
										<div class="flex items-center gap-2">
											<span
												class="truncate text-xs font-medium text-stone-900 dark:text-stone-100"
											>
												{s.user_agent || 'Browser session'}
											</span>
											{#if s.current}
												<span
													class="rounded border border-brand-badge-border bg-brand-badge-bg px-2 py-0.5 text-[11px] font-medium text-brand-badge-text"
												>
													Current
												</span>
											{/if}
										</div>
										<div class="text-[11px] text-stone-600 dark:text-stone-400">
											Last active: {formatDate(s.last_seen_at)} &bull; Created: {formatDate(
												s.created_at
											)}
										</div>
									</div>
									{#if !s.current}
										<button
											type="button"
											onclick={() => handleRevokeSession(s.id)}
											disabled={sessBusy}
											class="shrink-0 rounded border border-stone-300 px-2.5 py-1 text-xs font-medium text-stone-700 hover:bg-stone-100 dark:border-stone-700 dark:text-stone-300 dark:hover:bg-stone-800"
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
				<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
					<!-- Change Password (ADM-4, SEC-10) -->
					<section
						class="space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
					>
						<div>
							<h2 class="text-base font-semibold">Change Admin Password</h2>
							<p class="mt-1 text-xs text-stone-600 dark:text-stone-400">
								Requires re-authentication. Ends every other active session.
							</p>
						</div>

						{#if passErr}
							<div
								role="alert"
								class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
							>
								{passErr}
							</div>
						{/if}
						{#if passMsg}
							<div
								role="status"
								class="rounded-md border border-emerald-200 bg-emerald-50 p-3 text-xs text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300"
							>
								{passMsg}
							</div>
						{/if}

						<form onsubmit={handleChangePassword} class="space-y-3.5">
							<div>
								<label
									for="cur-password"
									class="mb-1 block text-xs font-medium text-stone-700 dark:text-stone-300"
								>
									Current Password
								</label>
								<input
									id="cur-password"
									type="password"
									bind:value={curPassword}
									required
									autocomplete="current-password"
									class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 text-sm dark:border-stone-700 dark:bg-stone-950"
								/>
							</div>
							{#if session?.totp_enabled}
								<div>
									<label
										for="cur-totp-pass"
										class="mb-1 block text-xs font-medium text-stone-700 dark:text-stone-300"
									>
										Current TOTP Code
									</label>
									<input
										id="cur-totp-pass"
										type="text"
										inputmode="numeric"
										maxlength="6"
										bind:value={curTOTPForPass}
										required
										placeholder="123456"
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono text-sm dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
							{/if}
							<div>
								<label
									for="new-password"
									class="mb-1 block text-xs font-medium text-stone-700 dark:text-stone-300"
								>
									New Password
								</label>
								<input
									id="new-password"
									type="password"
									bind:value={newPassword}
									required
									minlength="8"
									autocomplete="new-password"
									class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 text-sm dark:border-stone-700 dark:bg-stone-950"
								/>
							</div>
							<div>
								<label
									for="confirm-new-password"
									class="mb-1 block text-xs font-medium text-stone-700 dark:text-stone-300"
								>
									Confirm New Password
								</label>
								<input
									id="confirm-new-password"
									type="password"
									bind:value={confirmPassword}
									required
									minlength="8"
									autocomplete="new-password"
									class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 text-sm dark:border-stone-700 dark:bg-stone-950"
								/>
							</div>
							<button
								type="submit"
								disabled={passBusy}
								class="rounded-md bg-brand-primary px-4 py-2 text-sm font-medium text-brand-btn-text hover:bg-brand-hover disabled:opacity-50"
							>
								{passBusy ? 'Updating...' : 'Update Password'}
							</button>
						</form>
					</section>

					<!-- TOTP Management (ADM-4, SEC-10) -->
					<section
						class="space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
					>
						<div class="flex items-center justify-between gap-2">
							<h2 class="text-base font-semibold">
								Two-Factor Authentication (TOTP)
							</h2>
							<StatusBadge
								label={session?.totp_enabled ? 'enabled' : 'disabled'}
							/>
						</div>

						{#if totpErr}
							<div
								role="alert"
								class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
							>
								{totpErr}
							</div>
						{/if}
						{#if totpMsg}
							<div
								role="status"
								class="rounded-md border border-emerald-200 bg-emerald-50 p-3 text-xs text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300"
							>
								{totpMsg}
							</div>
						{/if}

						{#if session?.totp_enabled}
							<form onsubmit={handleDisableTOTP} class="space-y-3.5">
								<p class="text-xs text-stone-600 dark:text-stone-400">
									Disabling TOTP requires both your current password and a
									current 6-digit authenticator code.
								</p>
								<div>
									<label
										for="disable-totp-pass"
										class="mb-1 block text-xs font-medium text-stone-700 dark:text-stone-300"
									>
										Current Password
									</label>
									<input
										id="disable-totp-pass"
										type="password"
										bind:value={totpDisablePassword}
										required
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 text-sm dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
								<div>
									<label
										for="disable-totp-code"
										class="mb-1 block text-xs font-medium text-stone-700 dark:text-stone-300"
									>
										Current TOTP Code
									</label>
									<input
										id="disable-totp-code"
										type="text"
										inputmode="numeric"
										maxlength="6"
										bind:value={totpDisableCode}
										required
										placeholder="123456"
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono text-sm dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
								<button
									type="submit"
									disabled={totpBusy}
									class="rounded-md border border-red-300 px-4 py-2 text-sm font-medium text-red-800 hover:bg-red-50 disabled:opacity-50 dark:border-red-800 dark:text-red-300 dark:hover:bg-red-950/40"
								>
									{totpBusy ? 'Disabling...' : 'Disable TOTP'}
								</button>
							</form>
						{:else if totpInit}
							<div class="space-y-4">
								<div
									class="mx-auto w-fit rounded-md border border-stone-200 bg-white p-2"
								>
									{@html totpInit.qr_svg}
								</div>
								<code
									class="block rounded border border-stone-200 bg-stone-50 p-2 font-mono text-xs break-all select-all dark:border-stone-800 dark:bg-stone-950"
								>
									{totpInit.secret}
								</code>
								<form onsubmit={handleEnableTOTP} class="space-y-3">
									<div>
										<label
											for="enable-totp-code"
											class="mb-1 block text-xs font-medium text-stone-700 dark:text-stone-300"
										>
											6-Digit Verification Code
										</label>
										<input
											id="enable-totp-code"
											type="text"
											inputmode="numeric"
											maxlength="6"
											bind:value={totpEnableCode}
											required
											placeholder="123456"
											class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono text-sm dark:border-stone-700 dark:bg-stone-950"
										/>
									</div>
									<button
										type="submit"
										disabled={totpBusy || totpEnableCode.trim().length !== 6}
										class="rounded-md bg-brand-primary px-4 py-2 text-sm font-medium text-brand-btn-text hover:bg-brand-hover disabled:opacity-50"
									>
										{totpBusy ? 'Verifying...' : 'Enable TOTP'}
									</button>
								</form>
							</div>
						{:else}
							<form onsubmit={handleInitTOTP} class="space-y-3.5">
								<div>
									<label
										for="init-totp-pass"
										class="mb-1 block text-xs font-medium text-stone-700 dark:text-stone-300"
									>
										Confirm Current Password
									</label>
									<input
										id="init-totp-pass"
										type="password"
										bind:value={totpInitPassword}
										required
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 text-sm dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
								<button
									type="submit"
									disabled={totpBusy}
									class="rounded-md bg-brand-primary px-4 py-2 text-sm font-medium text-brand-btn-text hover:bg-brand-hover disabled:opacity-50"
								>
									{totpBusy ? 'Generating...' : 'Set Up TOTP'}
								</button>
							</form>
						{/if}
					</section>
				</div>
			{:else if tab === 'hook-domain'}
				<section
					class="max-w-xl space-y-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm dark:border-stone-800 dark:bg-stone-900"
				>
					<h2 class="text-base font-semibold">Reserved Hook Domain</h2>
					<HookDomainForm onSaved={() => loadAll()} />
				</section>
			{:else if tab === 'github'}
				<GitHubSettings onSwitchToHookDomain={() => switchTab('hook-domain')} />
			{/if}
		</div>
	</div>
</div>
