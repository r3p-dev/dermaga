<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, streamSSE } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';
	import type { AppView, GitHubSettings, DatabaseView } from '$lib/types/api';
	import type { Release } from '$lib/types/apps';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Terminal from '$lib/components/Terminal.svelte';
	import Modal from '$lib/components/Modal.svelte';

	const appName = $derived(page.params.name ?? '');

	type Tab = 'overview' | 'deployments' | 'logs' | 'env' | 'domains' | 'databases' | 'settings' | 'danger';
	let activeTab = $state<Tab>('overview');

	let app = $state<AppView | null>(null);
	let loading = $state(true);

	// Releases & logs
	let selectedRelease = $state<Release | null>(null);
	let buildLogLines = $state<string[]>([]);
	let stopBuildLogSSE: (() => void) | null = null;
	let logModalOpen = $state(false);

	// Runtime logs
	let runtimeLogLines = $state<string[]>([]);
	let stopRuntimeLogSSE: (() => void) | null = null;
	let runtimeFollowing = $state(true);

	// Environment variables
	let envKeys = $state<string[]>([]);
	let envValues = $state<Record<string, string>>({});
	let envRevealed = $state(false);
	let newEnvKey = $state('');
	let newEnvValue = $state('');
	let envEdits = $state<Record<string, string>>({});
	let envDeletions = $state<Set<string>>(new Set());

	// Databases & Linking
	let allDatabases = $state<DatabaseView[]>([]);
	let linkModalOpen = $state(false);
	let selectedDbToLink = $state('');
	let linkEnvMapping = $state('DATABASE_URL');

	// Danger zone
	let deleteModalOpen = $state(false);
	let purgeVolumes = $state(false);
	let confirmDeleteName = $state('');

	// GitHub details modal
	let githubModalOpen = $state(false);
	let githubSettings = $state<GitHubSettings | null>(null);

	onMount(async () => {
		const tabParam = page.url.searchParams.get('tab') as Tab;
		if (tabParam) activeTab = tabParam;

		await loadApp();
		await loadDatabases();
	});

	onDestroy(() => {
		if (stopBuildLogSSE) stopBuildLogSSE();
		if (stopRuntimeLogSSE) stopRuntimeLogSSE();
	});

	$effect(() => {
		if (activeTab === 'logs') {
			startRuntimeLogs();
		} else {
			if (stopRuntimeLogSSE) {
				stopRuntimeLogSSE();
				stopRuntimeLogSSE = null;
			}
		}

		if (activeTab === 'env') {
			loadEnv();
		}
	});

	async function loadApp() {
		loading = true;
		try {
			app = await api.getApp(appName);
			githubSettings = app.github || null;
		} catch (err: any) {
			toast.error(err.message || 'Failed to load application');
		} finally {
			loading = false;
		}
	}

	async function loadDatabases() {
		try {
			allDatabases = await api.listDatabases();
		} catch {
			// ignore
		}
	}

	async function handleDeploy() {
		try {
			const rel = await api.deployApp(appName);
			toast.success(`Deploy queued: release ${rel.rel8}`);
			await loadApp();
			viewBuildLog(rel);
		} catch (err: any) {
			toast.error(err.message || 'Deploy failed');
		}
	}

	async function handleRollback(toRel8: string) {
		try {
			const rel = await api.rollbackApp(appName, toRel8);
			toast.success(`Rollback queued to release ${toRel8}`);
			await loadApp();
			viewBuildLog(rel);
		} catch (err: any) {
			toast.error(err.message || 'Rollback failed');
		}
	}

	function viewBuildLog(rel: Release) {
		selectedRelease = rel;
		buildLogLines = [];
		logModalOpen = true;

		if (stopBuildLogSSE) stopBuildLogSSE();
		const url = `/api/apps/${encodeURIComponent(appName)}/releases/${encodeURIComponent(rel.rel8)}/log?follow=1`;
		stopBuildLogSSE = streamSSE(
			url,
			(line) => {
				buildLogLines = [...buildLogLines, line];
			},
			(finalStatus) => {
				if (finalStatus) {
					loadApp();
				}
			}
		);
	}

	function startRuntimeLogs() {
		if (stopRuntimeLogSSE) stopRuntimeLogSSE();
		runtimeLogLines = [];
		const url = `/api/apps/${encodeURIComponent(appName)}/logs?follow=1&lines=100`;
		stopRuntimeLogSSE = streamSSE(
			url,
			(line) => {
				runtimeLogLines = [...runtimeLogLines, line];
			},
			undefined,
			() => {
				// reconnect or paused
			}
		);
	}

	// Environment operations
	async function loadEnv() {
		try {
			const res = await api.getEnv(appName, envRevealed);
			if (envRevealed && typeof res === 'object' && !Array.isArray(res)) {
				envValues = res;
				envKeys = Object.keys(res).sort();
			} else if (Array.isArray(res)) {
				envKeys = res;
			}
		} catch (err: any) {
			toast.error(err.message || 'Failed to load environment variables');
		}
	}

	async function toggleRevealEnv() {
		envRevealed = !envRevealed;
		await loadEnv();
	}

	function addEnvVar() {
		if (!newEnvKey.trim()) return;
		const k = newEnvKey.trim().toUpperCase();
		envEdits[k] = newEnvValue;
		envDeletions.delete(k);
		if (!envKeys.includes(k)) envKeys = [...envKeys, k].sort();
		newEnvKey = '';
		newEnvValue = '';
	}

	function deleteEnvVar(key: string) {
		envDeletions.add(key);
		delete envEdits[key];
		envKeys = envKeys.filter((k) => k !== key);
	}

	async function saveEnv() {
		try {
			await api.updateEnv(appName, envEdits, Array.from(envDeletions));
			toast.success('Environment variables updated');
			envEdits = {};
			envDeletions.clear();
			await loadEnv();
		} catch (err: any) {
			toast.error(err.message || 'Failed to update environment variables');
		}
	}

	// Database operations
	async function linkDb() {
		if (!selectedDbToLink) return;
		try {
			const envMap: Record<string, string> = {};
			if (linkEnvMapping) {
				envMap['DATABASE_URL'] = linkEnvMapping;
			}
			await api.linkDatabase(selectedDbToLink, appName, envMap);
			toast.success(`Database ${selectedDbToLink} linked to ${appName}`);
			linkModalOpen = false;
			await loadApp();
		} catch (err: any) {
			toast.error(err.message || 'Failed to link database');
		}
	}

	async function unlinkDb(dbName: string) {
		try {
			await api.unlinkDatabase(dbName, appName);
			toast.success(`Database ${dbName} unlinked from ${appName}`);
			await loadApp();
		} catch (err: any) {
			toast.error(err.message || 'Failed to unlink database');
		}
	}

	// Delete app
	async function confirmDeleteApp() {
		if (confirmDeleteName !== appName) {
			toast.error(`Please type "${appName}" to confirm`);
			return;
		}

		try {
			await api.deleteApp(appName, purgeVolumes);
			toast.success(`Application ${appName} deleted`);
			goto('/');
		} catch (err: any) {
			toast.error(err.message || 'Failed to delete application');
		}
	}

	async function revealWebhookDetails() {
		try {
			githubSettings = await api.revealWebhook(appName);
			githubModalOpen = true;
		} catch (err: any) {
			toast.error(err.message || 'Failed to retrieve webhook settings');
		}
	}
</script>

<svelte:head>
	<title>{appName} - dermaga</title>
</svelte:head>

<div class="space-y-6">
	<!-- Breadcrumbs and Header -->
	<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
		<div>
			<div class="flex items-center space-x-2 text-xs text-zinc-500 mb-1">
				<a href="/" class="hover:text-zinc-800 dark:hover:text-zinc-200">Dashboard</a>
				<span>/</span>
				<span>{appName}</span>
			</div>
			<div class="flex items-center space-x-3">
				<h1 class="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 font-mono">
					{appName}
				</h1>
				<StatusBadge status={app?.live ? 'live' : 'inactive'} size="md" />
			</div>
		</div>

		<div class="flex items-center space-x-2.5">
			<button
				type="button"
				onclick={revealWebhookDetails}
				class="px-3 py-2 text-xs font-semibold rounded-lg border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 hover:bg-zinc-50 dark:hover:bg-zinc-800 text-zinc-700 dark:text-zinc-300 shadow-2xs flex items-center space-x-1.5"
			>
				<svg class="w-3.5 h-3.5 text-zinc-500" fill="currentColor" viewBox="0 0 24 24">
					<path fill-rule="evenodd" clip-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.53 1.032 1.53 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"/>
				</svg>
				<span>GitHub Setup</span>
			</button>

			<button
				type="button"
				onclick={handleDeploy}
				class="px-3.5 py-2 text-xs font-semibold rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white shadow-xs flex items-center space-x-1.5"
			>
				<svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
				</svg>
				<span>Trigger Deploy</span>
			</button>
		</div>
	</div>

	<!-- Navigation Tabs -->
	<div class="border-b border-zinc-200 dark:border-zinc-800">
		<nav class="flex space-x-6 overflow-x-auto" aria-label="Tabs">
			{#each [
				{ id: 'overview', label: 'Overview' },
				{ id: 'deployments', label: 'Deployments' },
				{ id: 'logs', label: 'Runtime Logs' },
				{ id: 'env', label: 'Environment' },
				{ id: 'domains', label: 'Domains & Proxy' },
				{ id: 'databases', label: 'Databases' },
				{ id: 'settings', label: 'Settings' },
				{ id: 'danger', label: 'Danger Zone' }
			] as tab}
				<button
					type="button"
					onclick={() => (activeTab = tab.id as Tab)}
					class="py-3 px-1 border-b-2 font-medium text-xs whitespace-nowrap transition-colors
					{activeTab === tab.id
						? 'border-indigo-600 text-indigo-600 dark:text-indigo-400 font-bold'
						: 'border-transparent text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300 hover:border-zinc-300'}"
				>
					{tab.label}
				</button>
			{/each}
		</nav>
	</div>

	<!-- Tab: Overview -->
	{#if activeTab === 'overview'}
		{#if app}
			<div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
				<!-- App Info Card -->
				<div class="lg:col-span-2 space-y-6">
					<div class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 p-6 shadow-2xs space-y-4">
						<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
							Deployment Status
						</h2>

						{#if app.live}
							<div class="p-4 rounded-xl bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 space-y-3 font-mono text-xs">
								<div class="flex items-center justify-between">
									<span class="text-zinc-500">Live Release</span>
									<span class="font-bold text-indigo-600 dark:text-indigo-400">{app.live.rel8}</span>
								</div>
								{#if app.live.sha}
									<div class="flex items-center justify-between">
										<span class="text-zinc-500">Commit SHA</span>
										<span>{app.live.sha.slice(0, 10)}</span>
									</div>
								{/if}
								{#if app.live.trigger}
									<div class="flex items-center justify-between">
										<span class="text-zinc-500">Trigger</span>
										<span>{app.live.trigger}</span>
									</div>
								{/if}
								{#if app.live.message}
									<div class="pt-2 border-t border-zinc-200 dark:border-zinc-800 text-zinc-600 dark:text-zinc-400 italic">
										"{app.live.message}"
									</div>
								{/if}
							</div>
						{:else}
							<div class="text-center py-6 text-xs text-zinc-500">
								No live release running. Click <strong>Trigger Deploy</strong> to build and launch.
							</div>
						{/if}

						<div class="grid grid-cols-2 sm:grid-cols-3 gap-4 pt-2 text-xs">
							<div>
								<span class="block text-zinc-400 text-[11px]">Strategy</span>
								<span class="font-semibold font-mono">{app.strategy}</span>
							</div>
							<div>
								<span class="block text-zinc-400 text-[11px]">Internal Port</span>
								<span class="font-semibold font-mono">{app.spec.port}</span>
							</div>
							<div>
								<span class="block text-zinc-400 text-[11px]">Health Check</span>
								<span class="font-semibold font-mono">{app.spec.health?.path || '/'}</span>
							</div>
						</div>
					</div>
				</div>

				<!-- Domains & Webhook Quick Links -->
				<div class="space-y-6">
					<div class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 p-6 shadow-2xs space-y-3">
						<h3 class="text-xs font-bold uppercase tracking-wider text-zinc-500">Active Domains</h3>
						{#if app.domains.length === 0}
							<p class="text-xs text-zinc-400">No domains assigned.</p>
						{:else}
							<div class="space-y-2">
								{#each app.domains as d}
									<a
										href="https://{d.host}"
										target="_blank"
										rel="noopener noreferrer"
										class="flex items-center justify-between p-2.5 rounded-lg border border-zinc-200 dark:border-zinc-800 hover:border-indigo-400 text-xs font-mono group"
									>
										<span class="text-zinc-800 dark:text-zinc-200 group-hover:text-indigo-600">{d.host}</span>
										{#if d.primary}
											<span class="text-[10px] bg-indigo-50 dark:bg-indigo-950 text-indigo-600 dark:text-indigo-400 px-1.5 py-0.5 rounded font-sans">Primary</span>
										{/if}
									</a>
								{/each}
							</div>
						{/if}
					</div>
				</div>
			</div>
		{/if}

	<!-- Tab: Deployments -->
	{:else if activeTab === 'deployments'}
		<div class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 shadow-2xs overflow-hidden">
			<div class="p-4 border-b border-zinc-200 dark:border-zinc-800 flex items-center justify-between">
				<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">Release History</h2>
				<span class="text-xs text-zinc-500">Retaining recent releases for instant rollback</span>
			</div>

			{#if !app?.releases || app.releases.length === 0}
				<div class="text-center py-12 text-xs text-zinc-500">No deployment releases yet.</div>
			{:else}
				<div class="divide-y divide-zinc-100 dark:divide-zinc-800 text-xs">
					{#each app.releases as rel}
						<div class="p-4 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 hover:bg-zinc-50/50 dark:hover:bg-zinc-800/40">
							<div class="flex items-start space-x-3">
								<StatusBadge status={rel.status} />
								<div>
									<div class="flex items-center space-x-2 font-mono">
										<span class="font-bold text-zinc-900 dark:text-zinc-100">{rel.rel8}</span>
										{#if rel.sha}
											<span class="text-zinc-400">• {rel.sha.slice(0, 8)}</span>
										{/if}
										{#if rel.trigger}
											<span class="text-zinc-400">via {rel.trigger}</span>
										{/if}
									</div>
									{#if rel.message}
										<p class="text-zinc-600 dark:text-zinc-400 mt-0.5 italic">{rel.message}</p>
									{/if}
									{#if rel.error}
										<p class="text-rose-500 text-[11px] mt-0.5">{rel.error}</p>
									{/if}
								</div>
							</div>

							<div class="flex items-center space-x-2">
								<button
									type="button"
									onclick={() => viewBuildLog(rel)}
									class="px-2.5 py-1.5 rounded-lg border border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-700 font-medium"
								>
									View Log
								</button>
								{#if rel.status !== 'live' && rel.status !== 'building'}
									<button
										type="button"
										onclick={() => handleRollback(rel.rel8)}
										class="px-2.5 py-1.5 rounded-lg bg-amber-50 dark:bg-amber-950/60 text-amber-700 dark:text-amber-300 hover:bg-amber-100 dark:hover:bg-amber-900/60 font-semibold"
									>
										Rollback To
									</button>
								{/if}
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>

	<!-- Tab: Runtime Logs -->
	{:else if activeTab === 'logs'}
		<div class="space-y-3">
			<div class="flex items-center justify-between text-xs text-zinc-500">
				<span>Streaming container journal via Server-Sent Events (SSE)</span>
				<div class="flex items-center space-x-2">
					<button
						type="button"
						onclick={() => (runtimeFollowing ? (stopRuntimeLogSSE?.(), runtimeFollowing = false) : startRuntimeLogs())}
						class="px-2.5 py-1 rounded border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900"
					>
						{runtimeFollowing ? 'Pause Stream' : 'Resume Stream'}
					</button>
				</div>
			</div>
			<Terminal
				lines={runtimeLogLines}
				title="{appName} (runtime logs)"
				follow={runtimeFollowing}
				maxHeight="580px"
				emptyText="No container runtime logs yet. Trigger a deploy to view logs."
			/>
		</div>

	<!-- Tab: Environment Variables -->
	{:else if activeTab === 'env'}
		<div class="space-y-6">
			<div class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 p-6 shadow-2xs space-y-4">
				<div class="flex items-center justify-between">
					<div>
						<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
							Environment Variables
						</h2>
						<p class="text-xs text-zinc-500">Encrypted in SQLite with AES-256-GCM. Injected into rootless Quadlet containers.</p>
					</div>
					<button
						type="button"
						onclick={toggleRevealEnv}
						class="px-3 py-1.5 rounded-lg border border-zinc-200 dark:border-zinc-700 text-xs font-semibold hover:bg-zinc-50 dark:hover:bg-zinc-800"
					>
						{envRevealed ? 'Hide Values' : 'Reveal Values'}
					</button>
				</div>

				<!-- Add Variable Form -->
				<div class="p-4 rounded-xl bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 flex flex-col sm:flex-row items-center gap-3">
					<input
						type="text"
						bind:value={newEnvKey}
						placeholder="KEY (e.g. API_SECRET)"
						class="w-full sm:w-1/3 px-3 py-1.5 text-xs font-mono rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 uppercase"
					/>
					<input
						type="text"
						bind:value={newEnvValue}
						placeholder="VALUE"
						class="w-full sm:flex-1 px-3 py-1.5 text-xs font-mono rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900"
					/>
					<button
						type="button"
						onclick={addEnvVar}
						class="w-full sm:w-auto px-4 py-1.5 rounded bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold"
					>
						Add
					</button>
				</div>

				<!-- Current Variables Table -->
				<div class="divide-y divide-zinc-100 dark:divide-zinc-800 text-xs">
					{#each envKeys as key}
						<div class="py-2.5 flex items-center justify-between">
							<span class="font-mono font-bold text-zinc-800 dark:text-zinc-200">{key}</span>
							<div class="flex items-center space-x-3">
								<span class="font-mono text-zinc-500 max-w-xs truncate">
									{envEdits[key] !== undefined
										? envEdits[key]
										: envRevealed
											? envValues[key] || ''
											: '••••••••'}
								</span>
								<button
									type="button"
									onclick={() => deleteEnvVar(key)}
									class="text-rose-500 hover:text-rose-700"
								>
									Delete
								</button>
							</div>
						</div>
					{/each}
				</div>

				<div class="pt-4 border-t border-zinc-100 dark:border-zinc-800 flex justify-end">
					<button
						type="button"
						onclick={saveEnv}
						class="px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold"
					>
						Save Changes
					</button>
				</div>
			</div>
		</div>

	<!-- Tab: Databases -->
	{:else if activeTab === 'databases'}
		<div class="space-y-6">
			<div class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 p-6 shadow-2xs space-y-4">
				<div class="flex items-center justify-between">
					<div>
						<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
							Linked Databases
						</h2>
						<p class="text-xs text-zinc-500">Databases attached on isolated Quadlet networks with automated connection injection.</p>
					</div>
					<button
						type="button"
						onclick={() => (linkModalOpen = true)}
						class="px-3 py-1.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold"
					>
						Link Database
					</button>
				</div>

				<!-- Linked databases listing -->
				{#if allDatabases.filter((d) => d.linkedApps.includes(appName)).length === 0}
					<div class="text-center py-8 text-xs text-zinc-500">
						No databases linked to this application.
					</div>
				{:else}
					<div class="divide-y divide-zinc-100 dark:divide-zinc-800 text-xs">
						{#each allDatabases.filter((d) => d.linkedApps.includes(appName)) as db}
							<div class="py-3 flex items-center justify-between">
								<div class="flex items-center space-x-3">
									<span class="font-bold text-sm text-zinc-900 dark:text-zinc-100">{db.name}</span>
									<span class="font-mono text-zinc-500">{db.engine} {db.version}</span>
								</div>
								<button
									type="button"
									onclick={() => unlinkDb(db.name)}
									class="px-3 py-1 rounded border border-rose-200 dark:border-rose-900/50 text-rose-600 hover:bg-rose-50 text-xs font-medium"
								>
									Unlink
								</button>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		</div>

	<!-- Tab: Danger Zone -->
	{:else if activeTab === 'danger'}
		<div class="bg-rose-50/50 dark:bg-rose-950/20 rounded-2xl border border-rose-200 dark:border-rose-900/50 p-6 space-y-4">
			<h2 class="text-sm font-bold text-rose-700 dark:text-rose-400 uppercase tracking-wider">
				Danger Zone
			</h2>
			<p class="text-xs text-zinc-600 dark:text-zinc-400 leading-relaxed">
				Deleting this application will immediately stop and tear down running containers, unregister Traefik routing rules, and delete git bare mirrors.
			</p>
			<button
				type="button"
				onclick={() => (deleteModalOpen = true)}
				class="px-4 py-2 rounded-lg bg-rose-600 hover:bg-rose-500 text-white text-xs font-semibold shadow-xs"
			>
				Delete Application
			</button>
		</div>
	{/if}
</div>

<!-- Build Log SSE Modal -->
<Modal open={logModalOpen} title="Build Logs - {selectedRelease?.rel8}" onclose={() => (logModalOpen = false)}>
	<Terminal
		lines={buildLogLines}
		title="Release {selectedRelease?.rel8}"
		follow={true}
		maxHeight="500px"
		emptyText="Streaming build output..."
	/>
</Modal>

<!-- GitHub Settings Modal -->
<Modal open={githubModalOpen} title="GitHub Repository Integration" onclose={() => (githubModalOpen = false)}>
	{#if githubSettings}
		<div class="space-y-4 text-xs font-mono">
			<div>
				<span class="block text-zinc-500 font-sans font-semibold mb-1">Deploy Key</span>
				<textarea readonly rows="3" class="w-full p-2 bg-zinc-950 border border-zinc-800 rounded text-zinc-300">{githubSettings.deployKey}</textarea>
			</div>
			<div>
				<span class="block text-zinc-500 font-sans font-semibold mb-1">Webhook URL</span>
				<input readonly type="text" value={githubSettings.webhookUrl} class="w-full p-2 bg-zinc-950 border border-zinc-800 rounded text-zinc-300" />
			</div>
			{#if githubSettings.webhookSecret}
				<div>
					<span class="block text-zinc-500 font-sans font-semibold mb-1">Webhook Secret</span>
					<input readonly type="text" value={githubSettings.webhookSecret} class="w-full p-2 bg-zinc-950 border border-zinc-800 rounded text-zinc-300" />
				</div>
			{/if}
		</div>
	{/if}
</Modal>

<!-- Link Database Modal -->
<Modal open={linkModalOpen} title="Link Database to {appName}" onclose={() => (linkModalOpen = false)}>
	<div class="space-y-4 text-xs">
		<div>
			<label for="dbSelect" class="block font-semibold mb-1">Select Database</label>
			<select id="dbSelect" bind:value={selectedDbToLink} class="w-full p-2 border rounded bg-white dark:bg-zinc-900">
				<option value="">-- Choose database --</option>
				{#each allDatabases as d}
					<option value={d.name}>{d.name} ({d.engine})</option>
				{/each}
			</select>
		</div>
		<div>
			<label for="envMap" class="block font-semibold mb-1">Env Variable Name</label>
			<input id="envMap" type="text" bind:value={linkEnvMapping} class="w-full p-2 border rounded font-mono bg-white dark:bg-zinc-900" />
		</div>
		<button type="button" onclick={linkDb} class="w-full py-2 bg-indigo-600 text-white rounded font-semibold">
			Confirm Link & Redeploy
		</button>
	</div>
</Modal>

<!-- Delete Confirmation Modal -->
<Modal open={deleteModalOpen} title="Delete Application {appName}" onclose={() => (deleteModalOpen = false)}>
	<div class="space-y-4 text-xs">
		<p class="text-zinc-600 dark:text-zinc-400">
			This action cannot be undone. To confirm, please type <strong>{appName}</strong> below:
		</p>
		<input
			type="text"
			bind:value={confirmDeleteName}
			placeholder={appName}
			class="w-full p-2 border border-rose-300 rounded font-mono"
		/>
		<label class="flex items-center space-x-2">
			<input type="checkbox" bind:checked={purgeVolumes} />
			<span>Purge persistent volumes as well</span>
		</label>
		<button
			type="button"
			onclick={confirmDeleteApp}
			class="w-full py-2 bg-rose-600 hover:bg-rose-500 text-white font-semibold rounded"
		>
			Permanently Delete App
		</button>
	</div>
</Modal>
