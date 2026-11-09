<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, APIError } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';

	let password = $state('');
	let totp = $state('');
	let loading = $state(false);
	let errorMessage = $state('');
	let retryCountdown = $state(0);
	let countdownInterval: any = null;

	onMount(async () => {
		try {
			// If already authenticated, redirect to dashboard
			await api.getStatus();
			goto('/');
		} catch {
			// Not authenticated, stay on login
		}
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		if (retryCountdown > 0) return;
		if (!password) {
			errorMessage = 'Password is required.';
			return;
		}

		loading = true;
		errorMessage = '';

		try {
			await api.login(password, totp);
			toast.success('Logged in successfully');
			window.location.href = '/';
		} catch (err: any) {
			if (err instanceof APIError) {
				errorMessage = err.message;
				if (err.status === 429 && err.retryAfter) {
					startCountdown(err.retryAfter);
				}
			} else {
				errorMessage = err.message || 'Login failed';
			}
		} finally {
			loading = false;
		}
	}

	function startCountdown(seconds: number) {
		retryCountdown = seconds;
		clearInterval(countdownInterval);
		countdownInterval = setInterval(() => {
			retryCountdown -= 1;
			if (retryCountdown <= 0) {
				clearInterval(countdownInterval);
				errorMessage = '';
			}
		}, 1000);
	}
</script>

<svelte:head>
	<title>Login - dermaga</title>
</svelte:head>

<div class="w-full max-w-md p-8 bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 shadow-2xl">
	<div class="text-center mb-8">
		<div class="w-12 h-12 rounded-xl bg-indigo-600 flex items-center justify-center text-white font-bold text-2xl mx-auto mb-3 shadow-md shadow-indigo-600/20">
			<svg class="w-7 h-7 text-white" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
				<path d="M4 18h16M7 14l5-9 5 9M12 5v9"/>
			</svg>
		</div>
		<h1 class="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">Welcome to dermaga</h1>
		<p class="text-xs text-zinc-500 dark:text-zinc-400 mt-1">Authenticate to access your PaaS panel</p>
	</div>

	{#if errorMessage}
		<div class="mb-5 p-3 rounded-lg bg-rose-50 dark:bg-rose-950/60 border border-rose-200 dark:border-rose-900/60 text-xs text-rose-700 dark:text-rose-300 flex items-start space-x-2">
			<svg class="w-4 h-4 text-rose-500 shrink-0 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
			</svg>
			<div class="flex-1">
				<span>{errorMessage}</span>
				{#if retryCountdown > 0}
					<p class="font-semibold mt-1">Please wait {retryCountdown}s before retrying.</p>
				{/if}
			</div>
		</div>
	{/if}

	<form onsubmit={handleSubmit} class="space-y-4">
		<div>
			<label for="password" class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1.5 uppercase tracking-wider">
				Master Admin Password
			</label>
			<input
				id="password"
				type="password"
				bind:value={password}
				disabled={loading || retryCountdown > 0}
				required
				autocomplete="current-password"
				placeholder="Enter your master password"
				class="w-full px-3.5 py-2.5 rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-sm text-zinc-900 dark:text-zinc-100 placeholder-zinc-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent transition-all disabled:opacity-50"
			/>
		</div>

		<div>
			<div class="flex items-center justify-between mb-1.5">
				<label for="totp" class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 uppercase tracking-wider">
					TOTP Code <span class="text-zinc-400 font-normal lowercase">(if enabled)</span>
				</label>
			</div>
			<input
				id="totp"
				type="text"
				inputmode="numeric"
				pattern="[0-9]*"
				maxlength="6"
				bind:value={totp}
				disabled={loading || retryCountdown > 0}
				autocomplete="one-time-code"
				placeholder="6-digit authenticator code"
				class="w-full px-3.5 py-2.5 rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-sm font-mono text-zinc-900 dark:text-zinc-100 placeholder-zinc-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent transition-all disabled:opacity-50 tracking-widest text-center"
			/>
		</div>

		<button
			type="submit"
			disabled={loading || retryCountdown > 0}
			class="w-full mt-2 py-2.5 px-4 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white font-semibold text-sm shadow-md shadow-indigo-600/20 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 dark:focus:ring-offset-zinc-900 transition-all disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center space-x-2"
		>
			{#if loading}
				<svg class="animate-spin -ml-1 mr-2 h-4 w-4 text-white" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
				</svg>
				<span>Verifying credentials...</span>
			{:else if retryCountdown > 0}
				<span>Rate limited ({retryCountdown}s)</span>
			{:else}
				<span>Unlock Panel</span>
			{/if}
		</button>
	</form>

	<div class="mt-6 pt-4 border-t border-zinc-100 dark:border-zinc-800/80 text-center text-[11px] text-zinc-400">
		Protected by Argon2id & RFC 6238 TOTP
	</div>
</div>
