<script lang="ts">
	import { login, getSession } from '#lib/api.js';
	import { onMount } from 'svelte';

	let password = $state('');
	let totpCode = $state('');
	let totpRequired = $state(false);
	let loading = $state(false);
	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			const s = await getSession();
			if (s.setup_required) {
				window.location.href = '/setup';
				return;
			}
			if (s.authenticated) {
				window.location.href = '/';
			}
		} catch (err) {
			console.error(err);
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!password) return;

		loading = true;
		error = null;

		try {
			const res = await login({
				password,
				...(totpRequired ? { totp_code: totpCode.trim() } : {})
			});
			if (res.success) {
				window.location.href = '/';
			} else {
				if (res.totp_required) {
					totpRequired = true;
				}
				error = res.error || 'Authentication failed';
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Network error';
		} finally {
			loading = false;
		}
	}
</script>

<svelte:head>
	<title>Login &bull; dermaga</title>
</svelte:head>

<div class="max-w-md mx-auto mt-12 md:mt-20">
	<div class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 md:p-8 shadow-sm">
		<div class="mb-6 text-center">
			<img src="/logo.svg" alt="dermaga logo" class="w-12 h-12 mx-auto mb-3 object-contain" />
			<h1 class="text-xl font-semibold tracking-tight">Admin Login</h1>
			<p class="text-xs text-stone-500 dark:text-stone-400 mt-1">Enter your password to unlock the dermaga panel</p>
		</div>

		{#if error}
			<div role="alert" class="mb-5 p-3 rounded-md bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-300 text-xs font-medium">
				{error}
			</div>
		{/if}

		<form onsubmit={handleSubmit} class="space-y-4">
			<div>
				<label for="admin-password" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1.5">
					Admin Password
				</label>
				<input
					id="admin-password"
					type="password"
					bind:value={password}
					placeholder="••••••••••••"
					required
					class="w-full px-3 py-2 text-sm rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring focus:border-transparent transition"
				/>
			</div>

			{#if totpRequired}
				<div>
					<label for="totp-code" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1.5">
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
						class="w-full px-3 py-2 text-sm font-mono tracking-widest rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring focus:border-transparent transition"
					/>
				</div>
			{/if}

			<button
				type="submit"
				disabled={loading || !password || (totpRequired && totpCode.trim().length !== 6)}
				class="w-full py-2 px-4 rounded-md bg-brand-primary hover:bg-brand-hover active:bg-brand-active disabled:opacity-50 text-brand-btn-text font-medium text-sm transition shadow-sm flex items-center justify-center gap-2 focus:outline-none focus:ring-2 focus:ring-brand-ring focus:ring-offset-2 dark:focus:ring-offset-stone-900"
			>
				{#if loading}
					<span class="inline-block w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin"></span>
					Verifying...
				{:else}
					Sign In
				{/if}
			</button>
		</form>

		<div class="mt-6 pt-6 border-t border-stone-100 dark:border-stone-800 text-center">
			<p class="text-xs text-stone-500 dark:text-stone-400">
				Forgotten password? Run <code class="text-xs font-mono bg-stone-100 dark:bg-stone-800 text-stone-700 dark:text-stone-300 px-1 py-0.5 rounded">dermaga admin set-password</code> on the server (SRV-1).
			</p>
		</div>
	</div>
</div>
