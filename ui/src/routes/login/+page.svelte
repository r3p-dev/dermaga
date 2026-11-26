<script lang="ts">
	import { login, getSession } from '#lib/api.js'
	import { onMount } from 'svelte'

	let password = $state('')
	let totpCode = $state('')
	let totpRequired = $state(false)
	let loading = $state(false)
	let error = $state<string | null>(null)

	onMount(async () => {
		try {
			const s = await getSession()
			if (s.setup_required) {
				window.location.href = '/setup'
				return
			}
			if (s.authenticated) {
				window.location.href = '/'
			}
		} catch (err) {
			console.error(err)
		}
	})

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault()
		if (!password) return

		loading = true
		error = null

		try {
			const res = await login({
				password,
				...(totpRequired ? { totp_code: totpCode.trim() } : {})
			})
			if (res.success) {
				window.location.href = '/'
			} else {
				if (res.totp_required) {
					totpRequired = true
				}
				error = res.error || 'Authentication failed'
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Network error'
		} finally {
			loading = false
		}
	}
</script>

<svelte:head>
	<title>Login &bull; dermaga</title>
</svelte:head>

<div class="mx-auto mt-12 max-w-md md:mt-20">
	<div
		class="rounded-xl border border-stone-200 bg-white p-6 shadow-sm md:p-8 dark:border-stone-800 dark:bg-stone-900"
	>
		<div class="mb-6 text-center">
			<img
				src="/logo.svg"
				alt="dermaga logo"
				class="mx-auto mb-3 size-12 object-contain"
			/>
			<h1 class="text-xl font-semibold tracking-tight">Admin Login</h1>
			<p class="mt-1 text-xs text-stone-500 dark:text-stone-400">
				Enter your password to unlock the dermaga panel
			</p>
		</div>

		{#if error}
			<div
				role="alert"
				class="mb-5 rounded-md border border-red-200 bg-red-50 p-3 text-xs font-medium text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
			>
				{error}
			</div>
		{/if}

		<form onsubmit={handleSubmit} class="space-y-4">
			<div>
				<label
					for="admin-password"
					class="mb-1.5 block text-xs font-medium text-stone-700 dark:text-stone-300"
				>
					Admin Password
				</label>
				<input
					id="admin-password"
					type="password"
					bind:value={password}
					placeholder="••••••••••••"
					required
					class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 text-sm text-stone-900 transition focus:border-transparent focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-950 dark:text-stone-100"
				/>
			</div>

			{#if totpRequired}
				<div>
					<label
						for="totp-code"
						class="mb-1.5 block text-xs font-medium text-stone-700 dark:text-stone-300"
					>
						Authenticator Code (TOTP)
					</label>
					<input
						id="totp-code"
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
			{/if}

			<button
				type="submit"
				disabled={loading ||
					!password ||
					(totpRequired && totpCode.trim().length !== 6)}
				class="flex w-full items-center justify-center gap-2 rounded-md bg-brand-primary px-4 py-2 text-sm font-medium text-brand-btn-text shadow-sm transition-colors hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:ring-offset-2 focus:outline-none active:bg-brand-active disabled:opacity-50 dark:focus:ring-offset-stone-900"
			>
				{#if loading}
					<span
						class="inline-block size-4 animate-spin rounded-full border-2 border-white/30 border-t-white"
					></span>
					Verifying...
				{:else}
					Sign In
				{/if}
			</button>
		</form>

		<div
			class="mt-6 border-t border-stone-100 pt-6 text-center dark:border-stone-800"
		>
			<p class="text-xs text-stone-500 dark:text-stone-400">
				Forgotten password? Run <code
					class="rounded bg-stone-100 px-1 py-0.5 font-mono text-xs text-stone-700 dark:bg-stone-800 dark:text-stone-300"
				>
					dermaga admin set-password
				</code> on the server (SRV-1).
			</p>
		</div>
	</div>
</div>
