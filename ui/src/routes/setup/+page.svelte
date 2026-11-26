<script lang="ts">
	import {
		getSession,
		verifySetupToken,
		setSetupPassword,
		initSetupTOTP,
		verifySetupTOTP,
		skipSetupTOTP
	} from '#lib/api.js'
	import type { SetupTOTPInitResponse } from '#lib/types/api.js'
	import HookDomainForm from '#lib/components/HookDomainForm.svelte'
	import { onMount } from 'svelte'

	type Step = 'loading' | 'token' | 'password' | 'totp' | 'hook-domain'

	let step = $state<Step>('loading')
	let tokenInput = $state('')
	let password = $state('')
	let confirmPassword = $state('')
	let totpCode = $state('')
	let totpInit = $state<SetupTOTPInitResponse | null>(null)
	let copiedField = $state<'secret' | 'uri' | null>(null)
	let busy = $state(false)
	let error = $state<string | null>(null)

	function passwordStrength(pw: string): {
		score: number
		label: string
		color: string
	} {
		if (!pw)
			return {
				score: 0,
				label: 'Enter at least 8 characters',
				color: 'bg-stone-200 dark:bg-stone-700'
			}
		let score = 0
		if (pw.length >= 8) score++
		if (pw.length >= 12) score++
		if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) score++
		if (/[0-9]/.test(pw) || /[^a-zA-Z0-9]/.test(pw)) score++

		if (pw.length < 8)
			return {
				score: 1,
				label: 'Too short (minimum 8 characters)',
				color: 'bg-red-600'
			}
		if (score <= 2)
			return {
				score: 2,
				label: 'Fair — longer passphrases are stronger',
				color: 'bg-amber-600'
			}
		if (score === 3) return { score: 3, label: 'Good', color: 'bg-emerald-600' }
		return { score: 4, label: 'Strong', color: 'bg-emerald-600' }
	}

	let strength = $derived(passwordStrength(password))

	async function loadTOTP() {
		busy = true
		error = null
		try {
			totpInit = await initSetupTOTP()
			step = 'totp'
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to initialize TOTP'
			step = 'totp'
		} finally {
			busy = false
		}
	}

	onMount(async () => {
		try {
			const params = new URLSearchParams(window.location.search)
			const urlToken = params.get('token')?.trim() ?? ''
			if (urlToken) {
				// Strip the secret token from the address bar immediately.
				history.replaceState({}, '', '/setup')
			}

			const session = await getSession()
			if (session.authenticated) {
				if (session.setup_state === 'admin_created') {
					step = 'hook-domain'
					return
				}
				window.location.href = '/'
				return
			}
			if (!session.setup_required && !session.setup_authenticated) {
				window.location.href = '/login'
				return
			}

			if (session.setup_authenticated) {
				if (session.setup_required) {
					step = 'password'
				} else {
					await loadTOTP()
				}
				return
			}

			if (urlToken) {
				busy = true
				const res = await verifySetupToken({ token: urlToken })
				busy = false
				if (res.success) {
					step = 'password'
				} else {
					error = res.error || 'Invalid or expired setup token.'
					step = 'token'
				}
				return
			}

			step = 'token'
		} catch (err) {
			error =
				err instanceof Error ? err.message : 'Failed to check setup status'
			step = 'token'
		}
	})

	async function handleTokenSubmit(e: SubmitEvent) {
		e.preventDefault()
		if (!tokenInput.trim()) return
		busy = true
		error = null
		try {
			const res = await verifySetupToken({ token: tokenInput.trim() })
			if (res.success) {
				tokenInput = ''
				step = 'password'
			} else {
				error = res.error || 'Invalid setup token.'
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Network error'
		} finally {
			busy = false
		}
	}

	async function handlePasswordSubmit(e: SubmitEvent) {
		e.preventDefault()
		if (password.length < 8) {
			error = 'Password must be at least 8 characters long.'
			return
		}
		if (password !== confirmPassword) {
			error = 'Passwords do not match.'
			return
		}

		busy = true
		error = null
		try {
			const res = await setSetupPassword({ password })
			if (res.success) {
				password = ''
				confirmPassword = ''
				await loadTOTP()
			} else {
				error = res.error || 'Failed to save password.'
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Network error'
		} finally {
			busy = false
		}
	}

	async function handleTOTPVerify(e: SubmitEvent) {
		e.preventDefault()
		if (!totpCode.trim()) return
		busy = true
		error = null
		try {
			const res = await verifySetupTOTP({ code: totpCode.trim() })
			if (res.success) {
				step = 'hook-domain'
			} else {
				error = res.error || 'Invalid verification code.'
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Network error'
		} finally {
			busy = false
		}
	}

	async function handleTOTPSkip() {
		busy = true
		error = null
		try {
			const res = await skipSetupTOTP()
			if (res.success) {
				step = 'hook-domain'
			} else {
				error = res.error || 'Failed to skip TOTP.'
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Network error'
		} finally {
			busy = false
		}
	}

	async function copyText(field: 'secret' | 'uri', value: string) {
		try {
			await navigator.clipboard.writeText(value)
			copiedField = field
			setTimeout(() => {
				if (copiedField === field) copiedField = null
			}, 2000)
		} catch {
			// Clipboard API may be unavailable in headless contexts; user can still select text.
		}
	}
</script>

<svelte:head>
	<title>First-Run Setup &bull; dermaga</title>
</svelte:head>

<div class="mx-auto mt-6 max-w-lg md:mt-12">
	<div
		class="rounded-xl border border-stone-200 bg-white p-6 shadow-sm md:p-8 dark:border-stone-800 dark:bg-stone-900"
	>
		<div class="mb-6 text-center">
			<img
				src="/logo.svg"
				alt="dermaga logo"
				class="mx-auto mb-3 size-11 object-contain"
			/>
			<h1 class="text-xl font-semibold tracking-tight">First-Run Setup</h1>
			<p class="mt-1 text-xs text-stone-600 dark:text-stone-400">
				Secure your server before accessing the panel
			</p>
		</div>

		{#if step === 'password' || step === 'totp' || step === 'hook-domain'}
			<ol
				class="mb-6 grid grid-cols-3 gap-2 text-xs font-medium"
				aria-label="Setup steps"
			>
				<li
					class="rounded-md border px-2.5 py-2 text-center {step === 'password'
						? 'border-brand-primary bg-brand-primary/10 text-brand-text'
						: 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300'}"
				>
					1. Password
				</li>
				<li
					class="rounded-md border px-2.5 py-2 text-center {step === 'totp'
						? 'border-brand-primary bg-brand-primary/10 text-brand-text'
						: step === 'hook-domain'
							? 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300'
							: 'border-stone-200 text-stone-600 dark:border-stone-800 dark:text-stone-400'}"
				>
					2. Two-Factor Auth
				</li>
				<li
					class="rounded-md border px-2.5 py-2 text-center {step ===
					'hook-domain'
						? 'border-brand-primary bg-brand-primary/10 text-brand-text'
						: 'border-stone-200 text-stone-600 dark:border-stone-800 dark:text-stone-400'}"
				>
					3. Hook Domain
				</li>
			</ol>
		{/if}

		{#if error}
			<div
				role="alert"
				class="mb-5 rounded-md border border-red-200 bg-red-50 p-3 text-xs font-medium text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
			>
				{error}
			</div>
		{/if}

		{#if step === 'loading'}
			<div class="py-10 text-center text-sm text-stone-600 dark:text-stone-400">
				Checking setup state...
			</div>
		{:else if step === 'token'}
			<div
				class="mb-5 rounded-md border border-stone-200 bg-stone-50 p-3.5 text-xs leading-relaxed text-stone-700 dark:border-stone-800 dark:bg-stone-950 dark:text-stone-300"
			>
				Open the link from the installer, or run <code
					class="rounded bg-stone-200/70 px-1 py-0.5 font-mono dark:bg-stone-800"
				>
					dermaga panel
				</code> on the server for a fresh link.
			</div>

			<form onsubmit={handleTokenSubmit} class="space-y-4">
				<div>
					<label
						for="setup-token"
						class="mb-1.5 block text-xs font-medium text-stone-700 dark:text-stone-300"
					>
						Setup Token
					</label>
					<input
						id="setup-token"
						type="text"
						bind:value={tokenInput}
						placeholder="Paste the setup token from your terminal"
						autocomplete="off"
						required
						class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono text-sm text-stone-900 transition focus:border-transparent focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-950 dark:text-stone-100"
					/>
				</div>

				<button
					type="submit"
					disabled={busy || !tokenInput.trim()}
					class="w-full rounded-md bg-brand-primary px-4 py-2 text-sm font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:ring-offset-2 focus:outline-none active:bg-brand-active disabled:opacity-50 dark:focus:ring-offset-stone-900"
				>
					{busy ? 'Verifying...' : 'Continue'}
				</button>
			</form>
		{:else if step === 'password'}
			<form onsubmit={handlePasswordSubmit} class="space-y-4">
				<div>
					<label
						for="setup-password"
						class="mb-1.5 block text-xs font-medium text-stone-700 dark:text-stone-300"
					>
						Admin Password
					</label>
					<input
						id="setup-password"
						type="password"
						bind:value={password}
						placeholder="Minimum 8 characters"
						autocomplete="new-password"
						required
						minlength="8"
						class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 text-sm text-stone-900 transition focus:border-transparent focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-950 dark:text-stone-100"
					/>

					<div class="mt-2 space-y-1">
						<div class="grid grid-cols-4 gap-1.5" aria-hidden="true">
							{#each [1, 2, 3, 4] as segment}
								<div
									class="h-1.5 rounded-full {strength.score >= segment
										? strength.color
										: 'bg-stone-200 dark:bg-stone-800'}"
								></div>
							{/each}
						</div>
						<p class="text-xs text-stone-600 dark:text-stone-400">
							Strength: {strength.label}
						</p>
					</div>
				</div>

				<div>
					<label
						for="setup-password-confirm"
						class="mb-1.5 block text-xs font-medium text-stone-700 dark:text-stone-300"
					>
						Confirm Password
					</label>
					<input
						id="setup-password-confirm"
						type="password"
						bind:value={confirmPassword}
						placeholder="Re-enter your password"
						autocomplete="new-password"
						required
						minlength="8"
						class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 text-sm text-stone-900 transition focus:border-transparent focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-950 dark:text-stone-100"
					/>
				</div>

				<button
					type="submit"
					disabled={busy || password.length < 8 || password !== confirmPassword}
					class="w-full rounded-md bg-brand-primary px-4 py-2 text-sm font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:ring-offset-2 focus:outline-none active:bg-brand-active disabled:opacity-50 dark:focus:ring-offset-stone-900"
				>
					{busy ? 'Saving Password...' : 'Save Password & Continue'}
				</button>
			</form>
		{:else if step === 'totp'}
			<div class="space-y-5">
				<p class="text-xs leading-relaxed text-stone-600 dark:text-stone-400">
					Scan this QR code with your authenticator app (such as 1Password,
					Bitwarden, or Aegis), then enter the 6-digit code below. You can skip
					this for now, though dermaga will warn until TOTP is enabled.
				</p>

				{#if totpInit}
					<div
						class="flex flex-col items-center gap-3 rounded-lg border border-stone-200 bg-stone-50 p-4 dark:border-stone-800 dark:bg-stone-950"
					>
						<div class="rounded-md border border-stone-200 bg-white p-2">
							{@html totpInit.qr_svg}
						</div>

						<div class="w-full space-y-2">
							<div class="flex items-center justify-between gap-2 text-xs">
								<span class="font-medium text-stone-700 dark:text-stone-300">
									Manual key
								</span>
								<button
									type="button"
									onclick={() => copyText('secret', totpInit!.secret)}
									class="text-xs font-medium text-brand-text hover:underline"
								>
									{copiedField === 'secret' ? 'Copied' : 'Copy key'}
								</button>
							</div>
							<code
								class="block w-full rounded border border-stone-200 bg-white p-2 font-mono text-xs break-all select-all dark:border-stone-800 dark:bg-stone-900"
							>
								{totpInit.secret}
							</code>

							<div class="flex items-center justify-between gap-2 pt-1 text-xs">
								<span class="font-medium text-stone-700 dark:text-stone-300">
									otpauth:// URI
								</span>
								<button
									type="button"
									onclick={() => copyText('uri', totpInit!.otpauth_uri)}
									class="text-xs font-medium text-brand-text hover:underline"
								>
									{copiedField === 'uri' ? 'Copied' : 'Copy URI'}
								</button>
							</div>
							<code
								class="block w-full rounded border border-stone-200 bg-white p-2 font-mono text-[11px] break-all text-stone-700 select-all dark:border-stone-800 dark:bg-stone-900 dark:text-stone-300"
							>
								{totpInit.otpauth_uri}
							</code>
						</div>
					</div>
				{/if}

				<form onsubmit={handleTOTPVerify} class="space-y-4">
					<div>
						<label
							for="setup-totp-code"
							class="mb-1.5 block text-xs font-medium text-stone-700 dark:text-stone-300"
						>
							6-Digit Verification Code
						</label>
						<input
							id="setup-totp-code"
							type="text"
							inputmode="numeric"
							autocomplete="one-time-code"
							pattern="[0-9]*"
							maxlength="6"
							bind:value={totpCode}
							placeholder="123456"
							required
							class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono text-sm tracking-widest text-stone-900 transition focus:border-transparent focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-950 dark:text-stone-100"
						/>
					</div>

					<div class="flex flex-col gap-2.5 sm:flex-row">
						<button
							type="submit"
							disabled={busy || totpCode.trim().length !== 6}
							class="flex-1 rounded-md bg-brand-primary px-4 py-2 text-sm font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:ring-offset-2 focus:outline-none active:bg-brand-active disabled:opacity-50 dark:focus:ring-offset-stone-900"
						>
							{busy ? 'Verifying...' : 'Verify & Complete Setup'}
						</button>
						<button
							type="button"
							onclick={handleTOTPSkip}
							disabled={busy}
							class="rounded-md border border-stone-300 px-4 py-2 text-sm font-medium text-stone-700 transition hover:bg-stone-100 disabled:opacity-50 dark:border-stone-700 dark:text-stone-300 dark:hover:bg-stone-800"
						>
							Skip for now
						</button>
					</div>
				</form>
			</div>
		{:else if step === 'hook-domain'}
			<HookDomainForm
				onSaved={() => {
					window.location.href = '/'
				}}
				onSkip={() => {
					window.location.href = '/'
				}}
			/>
		{/if}
	</div>
</div>
