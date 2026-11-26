<script lang="ts">
	import { page } from '$app/state';
	import {
		cancelDeployment,
		deleteApp,
		getApp,
		getSession,
		revealAppEnv,
		streamBuildLogs,
		streamRuntimeLogs,
		triggerDeploy,
		updateAppEnv,
		updateAppSettings
	} from '#lib/api.js';
	import EnvEditor from '#lib/components/EnvEditor.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import StatusBadge from '#lib/components/StatusBadge.svelte';
	import SubNav, { type SubNavItem } from '#lib/components/SubNav.svelte';
	import type { AppDetailResponse, DeploymentInfo, EnvVarInput } from '#lib/types/api.js';
	import { onDestroy, onMount } from 'svelte';

	type TabId = 'overview' | 'deployments' | 'logs' | 'environment' | 'settings' | 'danger';
	const tabs: SubNavItem[] = [
		{ id: 'overview', label: 'Overview' },
		{ id: 'deployments', label: 'Deployments' },
		{ id: 'logs', label: 'Logs' },
		{ id: 'environment', label: 'Environment' },
		{ id: 'settings', label: 'Settings' },
		{ id: 'danger', label: 'Danger zone', danger: true }
	];

	let activeTab = $state<TabId>('overview');
	let app = $state<AppDetailResponse | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let actionMsg = $state<string | null>(null);
	let actionErr = $state<string | null>(null);

	// Deployments & build log drawer
	let selectedDep = $state<DeploymentInfo | null>(null);
	let liveBuildLogs = $state('');
	let stopBuildStream: (() => void) | null = null;
	let deploying = $state(false);

	// Runtime logs
	let runtimeLines = $state(200);
	let runtimeFollow = $state(true);
	let runtimeLogText = $state('');
	let runtimeErr = $state<string | null>(null);
	let stopRuntimeStream: (() => void) | null = null;

	// Environment
	let revealedValues = $state<Record<string, string>>({});
	let editingEnv = $state(false);
	let envDraft = $state<EnvVarInput[]>([]);
	let savingEnv = $state(false);
	let envChangedNotice = $state(false);

	// Settings
	let sBranch = $state('');
	let sContainerfile = $state('');
	let sBuildContext = $state('.');
	let sPort = $state(8080);
	let sHealthType = $state<'http' | 'cmd' | 'none'>('http');
	let sHealthPath = $state('/health');
	let sHealthCmd = $state('');
	let sHealthTimeout = $state(30);
	let sHealthConsecutive = $state(3);
	let sMemory = $state('512m');
	let sCPUs = $state('1.0');
	let sPids = $state(128);
	let sReadOnly = $state(false);
	let sCaps = $state('');
	let savingSettings = $state(false);

	// Danger zone
	let confirmName = $state('');
	let deleting = $state(false);

	let appId = $derived(page.params.id ?? '');

	onMount(async () => {
		try {
			const sess = await getSession();
			if (sess.setup_required) {
				window.location.href = '/setup';
				return;
			}
			if (!sess.authenticated) {
				window.location.href = '/login';
				return;
			}
			await loadApp();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load app';
		} finally {
			loading = false;
		}
	});

	onDestroy(() => {
		stopBuildStream?.();
		stopRuntimeStream?.();
	});

	async function loadApp() {
		const res = await getApp(appId);
		app = res;
		sBranch = res.branch;
		sContainerfile = res.containerfile_path;
		sBuildContext = res.build_context;
		sPort = res.port;
		sHealthType = (res.health_type as 'http' | 'cmd' | 'none') || 'http';
		sHealthPath = res.health_path || '/health';
		sHealthCmd = res.health_cmd || '';
		sHealthTimeout = res.health_timeout_sec;
		sHealthConsecutive = res.health_consecutive;
		sMemory = res.memory_limit;
		sCPUs = res.cpu_limit;
		sPids = res.pids_limit;
		sReadOnly = res.read_only_root;
		sCaps = (res.add_capabilities || []).join(', ');
	}

	function selectTab(id: TabId) {
		activeTab = id;
		actionMsg = null;
		actionErr = null;
		if (id === 'logs') {
			startRuntimeLogStream();
		} else {
			stopRuntimeStream?.();
			stopRuntimeStream = null;
		}
	}

	function formatDuration(start: string, end?: string): string {
		if (!start || !end) return 'running';
		const ms = new Date(end).getTime() - new Date(start).getTime();
		if (Number.isNaN(ms) || ms < 0) return '-';
		const s = Math.round(ms / 1000);
		return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
	}

	function openBuildLog(dep: DeploymentInfo) {
		selectedDep = dep;
		liveBuildLogs = dep.build_logs || '';
		stopBuildStream?.();
		const terminal = ['live', 'failed', 'cancelled', 'replaced by a newer deploy'].includes(dep.status_label);
		if (!terminal) {
			liveBuildLogs = '';
			stopBuildStream = streamBuildLogs(dep.id, {
				onLog: (chunk) => {
					liveBuildLogs += chunk;
				},
				onStatus: (label) => {
					if (selectedDep && selectedDep.id === dep.id) {
						selectedDep = { ...selectedDep, status_label: label };
					}
				},
				onDone: async () => {
					await loadApp();
				}
			});
		}
	}

	async function handleTriggerDeploy() {
		if (!app) return;
		deploying = true;
		actionErr = null;
		actionMsg = null;
		try {
			const dep = await triggerDeploy(app.id);
			envChangedNotice = false;
			await loadApp();
			activeTab = 'deployments';
			openBuildLog(dep);
		} catch (err) {
			actionErr = err instanceof Error ? err.message : 'Failed to start deploy';
		} finally {
			deploying = false;
		}
	}

	async function handleCancelDep(depId: string) {
		actionErr = null;
		try {
			await cancelDeployment(depId);
			await loadApp();
			actionMsg = 'Deployment cancelled.';
		} catch (err) {
			actionErr = err instanceof Error ? err.message : 'Failed to cancel deployment';
		}
	}

	function startRuntimeLogStream() {
		if (!app) return;
		stopRuntimeStream?.();
		runtimeLogText = '';
		runtimeErr = null;
		stopRuntimeStream = streamRuntimeLogs(app.id, runtimeLines, runtimeFollow, {
			onLog: (line) => {
				runtimeLogText += line + '\n';
			},
			onError: (msg) => {
				runtimeErr = msg;
			}
		});
	}

	async function handleReveal(key: string, scope: 'runtime' | 'build_time') {
		if (!app) return;
		const id = `${key}:${scope}`;
		if (revealedValues[id] !== undefined) {
			const copy = { ...revealedValues };
			delete copy[id];
			revealedValues = copy;
			return;
		}
		try {
			const res = await revealAppEnv(app.id, { key, scope });
			revealedValues = { ...revealedValues, [id]: res.value };
		} catch (err) {
			actionErr = err instanceof Error ? err.message : 'Failed to reveal variable';
		}
	}

	async function startEditEnv() {
		if (!app) return;
		actionErr = null;
		const rows: EnvVarInput[] = [];
		for (const m of app.env) {
			const scope = m.runtime ? 'runtime' : 'build_time';
			const res = await revealAppEnv(app.id, { key: m.key, scope });
			rows.push({
				key: m.key,
				value: res.value,
				runtime: m.runtime,
				build_time: m.build_time
			});
		}
		envDraft = rows;
		editingEnv = true;
	}

	async function handleSaveEnv() {
		if (!app) return;
		savingEnv = true;
		actionErr = null;
		actionMsg = null;
		try {
			const cleaned = envDraft.filter((r) => r.key.trim() !== '');
			await updateAppEnv(app.id, { env: cleaned });
			editingEnv = false;
			revealedValues = {};
			envChangedNotice = true;
			await loadApp();
			actionMsg = 'Environment variables saved. Changes take effect on the next deploy.';
		} catch (err) {
			actionErr = err instanceof Error ? err.message : 'Failed to save environment';
		} finally {
			savingEnv = false;
		}
	}

	async function handleSaveSettings(e: SubmitEvent) {
		e.preventDefault();
		if (!app) return;
		savingSettings = true;
		actionErr = null;
		actionMsg = null;
		try {
			const caps = sCaps
				.split(',')
				.map((c) => c.trim())
				.filter(Boolean);
			await updateAppSettings(app.id, {
				branch: sBranch.trim(),
				containerfile_path: sContainerfile.trim(),
				build_context: sBuildContext.trim(),
				port: Number(sPort),
				health_type: sHealthType,
				health_path: sHealthPath.trim(),
				health_cmd: sHealthCmd.trim(),
				health_timeout_sec: Number(sHealthTimeout),
				health_consecutive: Number(sHealthConsecutive),
				memory_limit: sMemory.trim(),
				cpu_limit: sCPUs.trim(),
				pids_limit: Number(sPids),
				read_only_root: sReadOnly,
				add_capabilities: caps
			});
			await loadApp();
			actionMsg = 'Settings saved. Deploy to apply changes.';
		} catch (err) {
			actionErr = err instanceof Error ? err.message : 'Failed to save settings';
		} finally {
			savingSettings = false;
		}
	}

	async function handleDelete(e: SubmitEvent) {
		e.preventDefault();
		if (!app) return;
		deleting = true;
		actionErr = null;
		try {
			await deleteApp(app.id, { confirm_name: confirmName.trim() });
			window.location.href = '/';
		} catch (err) {
			actionErr = err instanceof Error ? err.message : 'Failed to delete app';
			deleting = false;
		}
	}
</script>

<svelte:head>
	<title>{app ? app.name : 'App'} &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
	{#if error}
		<div role="alert" class="p-4 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-sm">
			{error}
		</div>
	{:else if loading || !app}
		<div class="h-48 rounded-xl bg-stone-100 dark:bg-stone-800/50 animate-pulse"></div>
	{:else}
		<PageHeader
			title={app.name}
			subtitle="{app.repo_full_name} ({app.branch}) &bull; {app.primary_domain}"
			breadcrumbs={[{ label: 'Dashboard', href: '/' }, { label: app.name }]}
		>
			{#snippet actions()}
				{#if app && app.deployments.length > 0}
					{@const latest = app.deployments[0]}
					<StatusBadge
						label={latest.status_label}
						pulse={latest.status_label === 'queued' || latest.status_label === 'building' || latest.status_label === 'starting' || latest.status_label === 'healthy'}
					/>
				{/if}
				<button
					type="button"
					onclick={handleTriggerDeploy}
					disabled={deploying}
					class="px-4 py-2 text-xs font-medium rounded-lg bg-brand-primary text-brand-btn-text hover:bg-brand-hover disabled:opacity-50 focus:outline-none focus:ring-2 focus:ring-brand-ring transition w-fit shadow-sm"
				>
					{deploying ? 'Queueing...' : 'Deploy'}
				</button>
			{/snippet}
		</PageHeader>

		<div class="flex flex-col md:flex-row gap-6 items-start">
			<SubNav
				items={tabs}
				active={activeTab}
				onSelect={(id) => selectTab(id as TabId)}
				ariaLabel="App sections"
			/>

			<div class="flex-1 min-w-0 w-full space-y-4">
				{#if actionMsg}
					<div role="status" class="p-3 rounded-lg bg-emerald-50 dark:bg-emerald-950/40 border border-emerald-200 dark:border-emerald-800 text-emerald-800 dark:text-emerald-300 text-xs">
						{actionMsg}
					</div>
				{/if}
				{#if actionErr}
					<div role="alert" class="p-3 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">
						{actionErr}
					</div>
				{/if}

		{#if activeTab === 'overview'}
			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div class="rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-5 space-y-2 text-xs">
					<h2 class="text-sm font-semibold">Live Endpoint</h2>
					<p>
						Primary domain:
						<a href="https://{app.primary_domain}" target="_blank" rel="noopener noreferrer" class="font-mono text-brand-text underline">
							https://{app.primary_domain}
						</a>
					</p>
					<p class="text-stone-600 dark:text-stone-400">Container port: <span class="font-mono">{app.port}</span></p>
					<p class="text-stone-600 dark:text-stone-400">
						Health check: <span class="font-mono">{app.health_type}{app.health_type === 'http' ? ` ${app.health_path}` : ''}</span>
					</p>
				</div>
				<div class="rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-5 space-y-2 text-xs">
					<h2 class="text-sm font-semibold">Latest Deployment</h2>
					{#if app.deployments.length === 0}
						<p class="text-stone-600 dark:text-stone-400">No deployments yet.</p>
					{:else}
						{@const latest = app.deployments[0]}
						<p>
							Release <span class="font-mono font-semibold">r{latest.release_number}</span> &mdash;
							<span class="font-mono font-semibold">{latest.status_label}</span>
						</p>
						<p class="text-stone-600 dark:text-stone-400 truncate">{latest.commit_message}</p>
						{#if latest.error_message}
							<p class="text-red-700 dark:text-red-400">{latest.error_message}</p>
						{/if}
					{/if}
				</div>
			</div>
		{:else if activeTab === 'deployments'}
			<div class="space-y-4">
				{#if app.deployments.length === 0}
					<p class="text-xs text-stone-600 dark:text-stone-400">No deployments recorded yet.</p>
				{:else}
					<div class="space-y-2.5">
						{#each app.deployments as dep}
							<div class="rounded-lg border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs">
								<div class="min-w-0 space-y-1">
									<div class="flex flex-wrap items-center gap-2">
										<span class="font-mono font-semibold tabular-nums">r{dep.release_number}</span>
										<StatusBadge label={dep.status_label} />
										<span class="text-stone-500 dark:text-stone-400">{dep.trigger}</span>
										<span class="text-stone-500 dark:text-stone-400">{formatDuration(dep.started_at, dep.finished_at)}</span>
									</div>
									<p class="font-mono text-stone-600 dark:text-stone-400 truncate">
										{dep.commit_hash ? dep.commit_hash.slice(0, 7) : 'HEAD'} &mdash; {dep.commit_message}
									</p>
									{#if dep.error_message}
										<p class="text-red-700 dark:text-red-400">{dep.error_message}</p>
									{/if}
								</div>
								<div class="flex items-center gap-2 shrink-0">
									{#if ['queued', 'building', 'starting', 'healthy'].includes(dep.status_label)}
										<button
											type="button"
											onclick={() => handleCancelDep(dep.id)}
											class="px-3 py-1.5 rounded-md border border-red-300 dark:border-red-800 text-red-700 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-950/40 transition"
										>
											Cancel
										</button>
									{/if}
									<button
										type="button"
										onclick={() => openBuildLog(dep)}
										class="px-3 py-1.5 rounded-md border border-stone-300 dark:border-stone-700 hover:bg-stone-100 dark:hover:bg-stone-800 transition"
									>
										View log
									</button>
								</div>
							</div>
						{/each}
					</div>
				{/if}

				{#if selectedDep}
					<div class="space-y-2 pt-2">
						<div class="flex items-center justify-between text-xs">
							<span class="font-semibold">Build log &mdash; r{selectedDep.release_number} ({selectedDep.status_label})</span>
							<button type="button" onclick={() => { selectedDep = null; stopBuildStream?.(); }} class="text-stone-500 hover:underline">
								Close
							</button>
						</div>
						<pre class="p-4 rounded-lg bg-stone-950 text-stone-100 font-mono text-xs overflow-x-auto max-h-96 whitespace-pre-wrap">{liveBuildLogs || 'No build log output.'}</pre>
					</div>
				{/if}
			</div>
		{:else if activeTab === 'logs'}
			<div class="space-y-3">
				<div class="flex flex-wrap items-center justify-between gap-3 text-xs">
					<div class="flex items-center gap-3">
						<label class="inline-flex items-center gap-1.5">
							<span>Lines:</span>
							<select bind:value={runtimeLines} onchange={startRuntimeLogStream} class="px-2 py-1 rounded border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950">
								<option value={100}>100</option>
								<option value={200}>200</option>
								<option value={500}>500</option>
							</select>
						</label>
						<label class="inline-flex items-center gap-1.5 cursor-pointer">
							<input type="checkbox" bind:checked={runtimeFollow} onchange={startRuntimeLogStream} />
							<span>Follow live</span>
						</label>
					</div>
					<button
						type="button"
						onclick={startRuntimeLogStream}
						class="px-3 py-1.5 rounded-md border border-stone-300 dark:border-stone-700 hover:bg-stone-100 dark:hover:bg-stone-800 transition"
					>
						Reconnect
					</button>
				</div>
				{#if runtimeErr}
					<div role="alert" class="p-3 rounded-md bg-amber-50 dark:bg-amber-950/40 border border-amber-200 dark:border-amber-800 text-amber-900 dark:text-amber-200 text-xs">
						{runtimeErr}
					</div>
				{/if}
				<pre class="p-4 rounded-lg bg-stone-950 text-stone-100 font-mono text-xs overflow-x-auto max-h-[28rem] whitespace-pre-wrap">{runtimeLogText || 'Waiting for runtime logs...'}</pre>
			</div>
		{:else if activeTab === 'environment'}
			<div class="rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-5 space-y-4">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<div>
						<h2 class="text-sm font-semibold">Environment Variables</h2>
						<p class="text-xs text-stone-600 dark:text-stone-400">Values are encrypted at rest and hidden by default. Reveals are recorded in the audit log.</p>
					</div>
					{#if !editingEnv}
						<button
							type="button"
							onclick={startEditEnv}
							class="px-3 py-1.5 text-xs font-medium rounded-md border border-stone-300 dark:border-stone-700 hover:bg-stone-100 dark:hover:bg-stone-800 transition"
						>
							Edit variables
						</button>
					{/if}
				</div>

				{#if envChangedNotice}
					<div role="status" class="p-3 rounded-md bg-amber-50 dark:bg-amber-950/40 border border-amber-200 dark:border-amber-800 text-amber-900 dark:text-amber-200 text-xs flex flex-wrap items-center justify-between gap-2">
						<span>Environment updated. Changes take effect on the next deploy.</span>
						<button type="button" onclick={handleTriggerDeploy} class="px-3 py-1 rounded bg-brand-primary text-brand-btn-text font-medium hover:bg-brand-hover">
							Redeploy now
						</button>
					</div>
				{/if}

				{#if editingEnv}
					<EnvEditor bind:items={envDraft} disabled={savingEnv} />
					<div class="flex justify-end gap-2 pt-2">
						<button type="button" onclick={() => (editingEnv = false)} class="px-3 py-1.5 text-xs rounded-md border border-stone-300 dark:border-stone-700">
							Cancel
						</button>
						<button type="button" onclick={handleSaveEnv} disabled={savingEnv} class="px-4 py-1.5 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover disabled:opacity-50">
							{savingEnv ? 'Saving...' : 'Save environment'}
						</button>
					</div>
				{:else if app.env.length === 0}
					<p class="text-xs text-stone-600 dark:text-stone-400">No environment variables configured.</p>
				{:else}
					<div class="space-y-2">
						{#each app.env as item}
							{@const scope = item.runtime ? 'runtime' : 'build_time'}
							{@const revKey = `${item.key}:${scope}`}
							<div class="p-3 rounded-lg border border-stone-200 dark:border-stone-800 flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-xs">
								<div class="space-y-1 min-w-0">
									<div class="flex flex-wrap items-center gap-2">
										<span class="font-mono font-semibold">{item.key}</span>
										{#if item.runtime}
											<span class="px-1.5 py-0.5 rounded text-[10px] bg-stone-100 dark:bg-stone-800">Runtime</span>
										{/if}
										{#if item.build_time}
											<span class="px-1.5 py-0.5 rounded text-[10px] bg-amber-100 dark:bg-amber-900/50 text-amber-800 dark:text-amber-300">Build time (--build-arg)</span>
										{/if}
									</div>
									<p class="font-mono text-stone-600 dark:text-stone-400 break-all">
										{revealedValues[revKey] !== undefined ? revealedValues[revKey] : '••••••••••••'}
									</p>
								</div>
								<button
									type="button"
									onclick={() => handleReveal(item.key, scope)}
									class="px-3 py-1.5 rounded-md border border-stone-300 dark:border-stone-700 hover:bg-stone-100 dark:hover:bg-stone-800 w-fit shrink-0"
								>
									{revealedValues[revKey] !== undefined ? 'Hide' : 'Reveal'}
								</button>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{:else if activeTab === 'settings'}
			<form onsubmit={handleSaveSettings} class="space-y-5">
				<div class="rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-5 space-y-4 text-xs">
					<h2 class="text-sm font-semibold">Health Check (APP-5)</h2>
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
						<div>
							<label for="set-htype" class="block font-medium mb-1">Type</label>
							<select id="set-htype" bind:value={sHealthType} class="w-full px-3 py-2 rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950">
								<option value="http">HTTP path</option>
								<option value="cmd">Container command</option>
								<option value="none">None</option>
							</select>
						</div>
						<div>
							<label for="set-port" class="block font-medium mb-1">Container Port</label>
							<input id="set-port" type="number" min="1" max="65535" bind:value={sPort} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
						{#if sHealthType === 'http'}
							<div>
								<label for="set-hpath" class="block font-medium mb-1">HTTP Path</label>
								<input id="set-hpath" type="text" bind:value={sHealthPath} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
							</div>
						{:else if sHealthType === 'cmd'}
							<div>
								<label for="set-hcmd" class="block font-medium mb-1">Command</label>
								<input id="set-hcmd" type="text" bind:value={sHealthCmd} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
							</div>
						{/if}
						<div>
							<label for="set-htimeout" class="block font-medium mb-1">Start Timeout (s)</label>
							<input id="set-htimeout" type="number" min="1" bind:value={sHealthTimeout} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
						<div>
							<label for="set-hconsec" class="block font-medium mb-1">Consecutive Successes</label>
							<input id="set-hconsec" type="number" min="1" bind:value={sHealthConsecutive} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
					</div>
				</div>

				<div class="rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-5 space-y-4 text-xs">
					<h2 class="text-sm font-semibold">Resources &amp; Security (APP-6)</h2>
					<div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
						<div>
							<label for="set-mem" class="block font-medium mb-1">Memory Limit</label>
							<input id="set-mem" type="text" bind:value={sMemory} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
						<div>
							<label for="set-cpu" class="block font-medium mb-1">CPU Limit</label>
							<input id="set-cpu" type="text" bind:value={sCPUs} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
						<div>
							<label for="set-pids" class="block font-medium mb-1">PIDs Limit</label>
							<input id="set-pids" type="number" min="1" bind:value={sPids} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
					</div>
					<div class="flex flex-wrap items-center gap-4">
						<label class="inline-flex items-center gap-2 cursor-pointer">
							<input type="checkbox" bind:checked={sReadOnly} />
							<span>Read-only root filesystem</span>
						</label>
					</div>
					<div>
						<label for="set-caps" class="block font-medium mb-1">Extra Capabilities (comma-separated, default none)</label>
						<input id="set-caps" type="text" bind:value={sCaps} placeholder="NET_BIND_SERVICE" class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
					</div>
				</div>

				<div class="rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-5 space-y-4 text-xs">
					<h2 class="text-sm font-semibold">Build (APP-7)</h2>
					<div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
						<div>
							<label for="set-branch" class="block font-medium mb-1">Branch</label>
							<input id="set-branch" type="text" bind:value={sBranch} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
						<div>
							<label for="set-cf" class="block font-medium mb-1">Containerfile Path</label>
							<input id="set-cf" type="text" bind:value={sContainerfile} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
						<div>
							<label for="set-ctx" class="block font-medium mb-1">Build Context</label>
							<input id="set-ctx" type="text" bind:value={sBuildContext} class="w-full px-3 py-2 font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950" />
						</div>
					</div>
				</div>

				<div class="flex justify-end">
					<button type="submit" disabled={savingSettings} class="px-4 py-2 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover disabled:opacity-50">
						{savingSettings ? 'Saving...' : 'Save settings'}
					</button>
				</div>
			</form>
		{:else if activeTab === 'danger'}
			<form onsubmit={handleDelete} class="rounded-xl border border-red-200 dark:border-red-900 bg-white dark:bg-stone-900 p-5 space-y-4 text-xs">
				<div>
					<h2 class="text-sm font-semibold text-red-700 dark:text-red-400">Remove App (APP-9)</h2>
					<p class="text-stone-600 dark:text-stone-400 mt-1">
						Permanently removes containers, images, Quadlet units, Traefik routes, env files, and the Git mirror for <code class="font-mono font-semibold">{app.name}</code>.
					</p>
				</div>
				<div>
					<label for="confirm-delete" class="block font-medium mb-1">
						Type <code class="font-mono font-semibold">{app.name}</code> to confirm
					</label>
					<input
						id="confirm-delete"
						type="text"
						bind:value={confirmName}
						placeholder={app.name}
						class="w-full sm:w-80 px-3 py-2 font-mono rounded-md border border-red-300 dark:border-red-800 bg-white dark:bg-stone-950 focus:outline-none focus:ring-2 focus:ring-red-500"
					/>
				</div>
				<button
					type="submit"
					disabled={deleting || confirmName.trim() !== app.name}
					class="px-4 py-2 rounded-md bg-red-600 text-white font-medium hover:bg-red-700 disabled:opacity-50 transition"
				>
					{deleting ? 'Removing...' : 'Remove app'}
				</button>
			</form>
		{/if}
			</div>
		</div>
	{/if}
</div>
