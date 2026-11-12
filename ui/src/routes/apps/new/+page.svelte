<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';
	import type { AppView, GitHubSettings } from '$lib/types/api';
	import type { App as AppSpec } from '$lib/types/spec';

	let step = $state<1 | 2>(1);
	let loading = $state(false);

	// Form inputs
	let name = $state('');
	let repo = $state('');
	let branch = $state('main');
	let containerfile = $state('Containerfile');
	let port = $state(3000);
	let healthPath = $state('/');
	let domain = $state('');
	let strategy = $state('zero-downtime');
	let autoDeploy = $state(true);

	// Created app result with GitHub setup keys & webhooks
	let createdApp = $state<AppView | null>(null);
	let ghSettings = $state<GitHubSettings | null>(null);

	// Suggestions based on common repos (e.g. portfolio)
	function applySuggestions(repoName: string) {
		const lower = repoName.toLowerCase();
		if (lower.includes('portfolio')) {
			if (!name) name = 'portfolio';
			if (!domain) domain = 'portfolio.vm.local';
			port = 3000;
			containerfile = 'Containerfile';
			healthPath = '/healthz';
			toast.info(
				'Auto-detected stack configuration for portfolio (Astro / Node port 3000, /healthz)'
			);
		}
	}

	async function handleCreate(e: Event) {
		e.preventDefault();
		if (!name.trim()) {
			toast.error('App name is required');
			return;
		}

		loading = true;
		try {
			const spec: AppSpec = {
				source: {
					type: 'git',
					repo: repo.trim(),
					branch: branch.trim() || 'main',
					containerfile: containerfile.trim() || 'Containerfile',
					autoDeploy: autoDeploy
				},
				port: Number(port) || 3000,
				health: {
					path: healthPath.trim() || '/',
					timeoutSec: 10
				},
				strategy: strategy
			};

			const domains = domain.trim() ? [{ host: domain.trim(), primary: true }] : [];
			const app = await api.createApp({
				name: name.trim(),
				domains,
				spec
			});

			createdApp = app;
			ghSettings = app.github || null;
			step = 2;
			toast.success(`Application ${name} created successfully!`);
		} catch (err: any) {
			toast.error(err.message || 'Failed to create application');
		} finally {
			loading = false;
		}
	}

	async function triggerFirstDeploy() {
		if (!createdApp) return;
		loading = true;
		try {
			await api.deployApp(createdApp.name);
			toast.success(`Initial deploy queued for ${createdApp.name}!`);
			goto(`/apps/${encodeURIComponent(createdApp.name)}?tab=deployments`);
		} catch (err: any) {
			toast.error(err.message || 'Failed to trigger deploy');
		} finally {
			loading = false;
		}
	}

	function copyToClipboard(text: string, label: string) {
		navigator.clipboard.writeText(text).then(() => {
			toast.success(`${label} copied to clipboard!`);
		});
	}
</script>

<svelte:head>
	<title>New Application - dermaga</title>
</svelte:head>

<div class="max-w-3xl mx-auto space-y-6">
	<div>
		<div class="flex items-center space-x-2 text-xs text-zinc-500 mb-1">
			<a href="/" class="hover:text-zinc-800 dark:hover:text-zinc-200">Dashboard</a>
			<span>/</span>
			<span>New App</span>
		</div>
		<h1 class="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">
			Create Application
		</h1>
		<p class="text-xs text-zinc-500 dark:text-zinc-400 mt-0.5">
			Connect a Git repository, configure health and domains, and deploy zero-downtime containers.
		</p>
	</div>

	<!-- Step Indicators -->
	<div class="flex items-center space-x-4 border-b border-zinc-200 dark:border-zinc-800 pb-4">
		<div
			class="flex items-center space-x-2 {step === 1
				? 'text-indigo-600 dark:text-indigo-400 font-bold'
				: 'text-zinc-400 font-medium'} text-xs"
		>
			<span
				class="w-6 h-6 rounded-full flex items-center justify-center border {step === 1
					? 'border-indigo-600 bg-indigo-50 dark:bg-indigo-950/60'
					: 'border-zinc-300 dark:border-zinc-700'}">1</span
			>
			<span>Configuration & Stack</span>
		</div>
		<div class="w-8 border-t border-zinc-200 dark:border-zinc-800"></div>
		<div
			class="flex items-center space-x-2 {step === 2
				? 'text-indigo-600 dark:text-indigo-400 font-bold'
				: 'text-zinc-400 font-medium'} text-xs"
		>
			<span
				class="w-6 h-6 rounded-full flex items-center justify-center border {step === 2
					? 'border-indigo-600 bg-indigo-50 dark:bg-indigo-950/60'
					: 'border-zinc-300 dark:border-zinc-700'}">2</span
			>
			<span>Deploy Key & Webhooks</span>
		</div>
	</div>

	{#if step === 1}
		<form
			onsubmit={handleCreate}
			class="space-y-6 bg-white dark:bg-zinc-900 p-6 rounded-2xl border border-zinc-200 dark:border-zinc-800 shadow-2xs"
		>
			<!-- Repository & Name -->
			<div class="space-y-4">
				<h2
					class="text-sm font-bold uppercase tracking-wider text-zinc-600 dark:text-zinc-400 border-b border-zinc-100 dark:border-zinc-800/80 pb-2"
				>
					Git Source
				</h2>

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<div>
						<label
							for="repo"
							class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1"
						>
							Repository URL / Slug <span class="text-rose-500">*</span>
						</label>
						<input
							id="repo"
							type="text"
							bind:value={repo}
							oninput={() => applySuggestions(repo)}
							required
							placeholder="e.g. r3p-dev/portfolio"
							class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 placeholder-zinc-400 focus:outline-none focus:ring-2 focus:ring-indigo-500"
						/>
						<p class="text-[11px] text-zinc-500 mt-1">GitHub repository name or URL.</p>
					</div>

					<div>
						<label
							for="appName"
							class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1"
						>
							App Name <span class="text-rose-500">*</span>
						</label>
						<input
							id="appName"
							type="text"
							bind:value={name}
							required
							placeholder="e.g. portfolio"
							class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 placeholder-zinc-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 font-mono"
						/>
						<p class="text-[11px] text-zinc-500 mt-1">
							Unique identifier (lowercase letters, digits, hyphens).
						</p>
					</div>
				</div>

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-2">
					<div>
						<label
							for="branch"
							class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1"
						>
							Target Branch
						</label>
						<input
							id="branch"
							type="text"
							bind:value={branch}
							placeholder="main"
							class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 font-mono"
						/>
					</div>

					<div>
						<label
							for="containerfile"
							class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1"
						>
							Containerfile / Dockerfile
						</label>
						<input
							id="containerfile"
							type="text"
							bind:value={containerfile}
							placeholder="Containerfile"
							class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 font-mono"
						/>
					</div>
				</div>
			</div>

			<!-- Routing & Health -->
			<div class="space-y-4 pt-4 border-t border-zinc-200 dark:border-zinc-800">
				<h2
					class="text-sm font-bold uppercase tracking-wider text-zinc-600 dark:text-zinc-400 border-b border-zinc-100 dark:border-zinc-800/80 pb-2"
				>
					Routing & Health Checks
				</h2>

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<div>
						<label
							for="domain"
							class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1"
						>
							Primary Domain
						</label>
						<input
							id="domain"
							type="text"
							bind:value={domain}
							placeholder="e.g. portfolio.vm.local"
							class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 placeholder-zinc-400 focus:outline-none focus:ring-2 focus:ring-indigo-500 font-mono"
						/>
						<p class="text-[11px] text-zinc-500 mt-1">Routed by Traefik dynamic reverse proxy.</p>
					</div>

					<div>
						<label
							for="port"
							class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1"
						>
							Internal Container Port <span class="text-rose-500">*</span>
						</label>
						<input
							id="port"
							type="number"
							bind:value={port}
							required
							min="1"
							max="65535"
							class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 font-mono"
						/>
						<p class="text-[11px] text-zinc-500 mt-1">
							Port exposed by your web service (e.g. 3000, 8080).
						</p>
					</div>
				</div>

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-2">
					<div>
						<label
							for="healthPath"
							class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1"
						>
							Health Check Path
						</label>
						<input
							id="healthPath"
							type="text"
							bind:value={healthPath}
							placeholder="/"
							class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 font-mono"
						/>
						<p class="text-[11px] text-zinc-500 mt-1">
							Polled during zero-downtime blue-green switches.
						</p>
					</div>

					<div>
						<label
							for="strategy"
							class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1"
						>
							Deployment Strategy
						</label>
						<select
							id="strategy"
							bind:value={strategy}
							class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100"
						>
							<option value="zero-downtime">zero-downtime (Blue-Green)</option>
							<option value="recreate">recreate (In-place restart, for shared volumes)</option>
						</select>
					</div>
				</div>

				<div class="pt-2">
					<label
						class="flex items-center space-x-2 text-xs font-medium text-zinc-700 dark:text-zinc-300 cursor-pointer"
					>
						<input
							type="checkbox"
							bind:checked={autoDeploy}
							class="rounded text-indigo-600 focus:ring-indigo-500 h-4 w-4"
						/>
						<span>Auto-deploy commits on GitHub webhook push</span>
					</label>
				</div>
			</div>

			<div class="flex justify-end pt-4 border-t border-zinc-200 dark:border-zinc-800">
				<button
					type="submit"
					disabled={loading}
					class="px-5 py-2.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white font-semibold text-sm shadow-xs transition-colors flex items-center space-x-2 disabled:opacity-50"
				>
					{#if loading}
						<svg class="animate-spin -ml-1 mr-2 h-4 w-4 text-white" fill="none" viewBox="0 0 24 24">
							<circle
								class="opacity-25"
								cx="12"
								cy="12"
								r="10"
								stroke="currentColor"
								stroke-width="4"
							></circle>
							<path
								class="opacity-75"
								fill="currentColor"
								d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
							></path>
						</svg>
						<span>Creating Application...</span>
					{:else}
						<span>Continue to GitHub Setup &rarr;</span>
					{/if}
				</button>
			</div>
		</form>
	{:else}
		<!-- Step 2: Deploy Key & Webhook setup screen -->
		<div
			class="space-y-6 bg-white dark:bg-zinc-900 p-6 rounded-2xl border border-zinc-200 dark:border-zinc-800 shadow-2xs"
		>
			<div
				class="p-4 rounded-xl bg-emerald-50 dark:bg-emerald-950/60 border border-emerald-200 dark:border-emerald-900 text-xs text-emerald-900 dark:text-emerald-100 flex items-start space-x-3"
			>
				<svg
					class="w-5 h-5 text-emerald-600 dark:text-emerald-400 shrink-0"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
					/>
				</svg>
				<div>
					<p class="font-bold text-sm">Application record created on Dermaga!</p>
					<p class="mt-1 leading-relaxed">
						Complete GitHub integration by adding the generated deploy key and webhook URL below.
						Once added, click <strong>Trigger Initial Deploy</strong>.
					</p>
				</div>
			</div>

			{#if ghSettings}
				<!-- Deploy Key Section -->
				<div class="space-y-3">
					<div class="flex items-center justify-between">
						<div>
							<h3 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">1. SSH Deploy Key</h3>
							<p class="text-xs text-zinc-500">Add to your repository with read-only access.</p>
						</div>
						<a
							href={ghSettings.deployKeysPage}
							target="_blank"
							rel="noopener noreferrer"
							class="text-xs font-semibold text-indigo-600 dark:text-indigo-400 hover:underline flex items-center space-x-1"
						>
							<span>Open GitHub Deploy Keys</span>
							<svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
									d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"
								/>
							</svg>
						</a>
					</div>

					<div class="relative">
						<textarea
							readonly
							rows="3"
							class="w-full p-3 font-mono text-xs rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-800 dark:text-zinc-200 select-all"
							>{ghSettings.deployKey}</textarea
						>
						<button
							type="button"
							onclick={() => copyToClipboard(ghSettings?.deployKey || '', 'Deploy key')}
							class="absolute top-2 right-2 px-2.5 py-1 text-xs font-semibold rounded bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 shadow-2xs hover:bg-zinc-100 dark:hover:bg-zinc-700"
						>
							Copy Key
						</button>
					</div>
					{#if ghSettings.deployKeyFingerprint}
						<p class="text-[11px] font-mono text-zinc-500">
							SHA-256 fingerprint: {ghSettings.deployKeyFingerprint}
						</p>
					{/if}
				</div>

				<!-- Webhook Section -->
				<div class="space-y-3 pt-4 border-t border-zinc-200 dark:border-zinc-800">
					<div class="flex items-center justify-between">
						<div>
							<h3 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">2. GitHub Webhook</h3>
							<p class="text-xs text-zinc-500">Configures automated builds on commit pushes.</p>
						</div>
						<a
							href={ghSettings.webhooksPage}
							target="_blank"
							rel="noopener noreferrer"
							class="text-xs font-semibold text-indigo-600 dark:text-indigo-400 hover:underline flex items-center space-x-1"
						>
							<span>Open GitHub Webhooks</span>
							<svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
									d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"
								/>
							</svg>
						</a>
					</div>

					<div class="space-y-2">
						<div>
							<span class="block text-xs font-medium text-zinc-500 mb-1">Payload URL</span>
							<div class="flex items-center space-x-2">
								<input
									type="text"
									readonly
									value={ghSettings.webhookUrl}
									class="flex-1 px-3 py-1.5 text-xs font-mono rounded border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-800 dark:text-zinc-200 select-all"
								/>
								<button
									type="button"
									onclick={() => copyToClipboard(ghSettings?.webhookUrl || '', 'Webhook URL')}
									class="px-2.5 py-1.5 text-xs font-semibold rounded bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 shadow-2xs hover:bg-zinc-100"
								>
									Copy URL
								</button>
							</div>
						</div>

						{#if ghSettings.webhookSecret}
							<div>
								<span class="block text-xs font-medium text-zinc-500 mb-1"
									>Secret (HMAC-SHA256)</span
								>
								<div class="flex items-center space-x-2">
									<input
										type="text"
										readonly
										value={ghSettings.webhookSecret}
										class="flex-1 px-3 py-1.5 text-xs font-mono rounded border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-800 dark:text-zinc-200 select-all"
									/>
									<button
										type="button"
										onclick={() =>
											copyToClipboard(ghSettings?.webhookSecret || '', 'Webhook Secret')}
										class="px-2.5 py-1.5 text-xs font-semibold rounded bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 shadow-2xs hover:bg-zinc-100"
									>
										Copy Secret
									</button>
								</div>
							</div>
						{/if}
						<p class="text-[11px] text-zinc-500 pt-1">
							Set Content type to <code>application/json</code>, and select "Just the push event".
						</p>
					</div>
				</div>
			{/if}

			<!-- Action buttons -->
			<div
				class="flex items-center justify-between pt-6 border-t border-zinc-200 dark:border-zinc-800"
			>
				<a
					href="/apps/{createdApp?.name}"
					class="px-4 py-2 rounded-lg border border-zinc-200 dark:border-zinc-700 text-xs font-medium text-zinc-700 dark:text-zinc-300 hover:bg-zinc-50 dark:hover:bg-zinc-800"
				>
					Skip to App Overview
				</a>

				<button
					type="button"
					disabled={loading}
					onclick={triggerFirstDeploy}
					class="px-5 py-2.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold text-sm shadow-xs transition-colors flex items-center space-x-2 disabled:opacity-50"
				>
					{#if loading}
						<svg class="animate-spin -ml-1 mr-2 h-4 w-4 text-white" fill="none" viewBox="0 0 24 24">
							<circle
								class="opacity-25"
								cx="12"
								cy="12"
								r="10"
								stroke="currentColor"
								stroke-width="4"
							></circle>
							<path
								class="opacity-75"
								fill="currentColor"
								d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
							></path>
						</svg>
						<span>Queuing Deploy...</span>
					{:else}
						<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"
							/>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
							/>
						</svg>
						<span>Deploy Key Added — Trigger Initial Deploy</span>
					{/if}
				</button>
			</div>
		</div>
	{/if}
</div>
