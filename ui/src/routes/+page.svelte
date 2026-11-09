<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';
	import type { AppView, DatabaseView, Status } from '$lib/types/api';
	import type { Check } from '$lib/types/doctor';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Modal from '$lib/components/Modal.svelte';

	let status = $state<Status | null>(null);
	let apps = $state<AppView[]>([]);
	let databases = $state<DatabaseView[]>([]);
	let checks = $state<Check[]>([]);
	let loading = $state(true);
	let searchFilter = $state('');
	let doctorModalOpen = $state(false);

	onMount(async () => {
		await loadData();
	});

	async function loadData() {
		loading = true;
		try {
			const [st, appList, dbList, chkList] = await Promise.all([
				api.getStatus(),
				api.listApps(),
				api.listDatabases(),
				api.getDoctor()
			]);
			status = st;
			apps = appList;
			databases = dbList;
			checks = chkList;
		} catch (err: any) {
			toast.error(err.message || 'Failed to load dashboard data');
		} finally {
			loading = false;
		}
	}

	const filteredApps = $derived(
		apps.filter((a) => {
			const q = searchFilter.toLowerCase();
			return (
				a.name.toLowerCase().includes(q) ||
				a.domains.some((d) => d.host.toLowerCase().includes(q))
			);
		})
	);

	const failedChecks = $derived(checks.filter((c) => c.status === 'fail'));
	const warnChecks = $derived(checks.filter((c) => c.status === 'warn'));
	const doctorHealth = $derived(
		failedChecks.length > 0 ? 'fail' : warnChecks.length > 0 ? 'warn' : 'ok'
	);

	async function triggerDeploy(appName: string) {
		try {
			await api.deployApp(appName);
			toast.success(`Deploy queued for ${appName}`);
			await loadData();
		} catch (err: any) {
			toast.error(err.message || `Failed to deploy ${appName}`);
		}
	}
</script>

<svelte:head>
	<title>Dashboard - dermaga</title>
</svelte:head>

<div class="space-y-6">
	<!-- Top Bar / Header -->
	<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">Overview</h1>
			<p class="text-xs text-zinc-500 dark:text-zinc-400 mt-0.5">
				Manage applications, databases, and platform infrastructure
			</p>
		</div>
		<div class="flex items-center space-x-2.5">
			<button
				type="button"
				onclick={loadData}
				class="p-2 text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-lg hover:bg-zinc-50 dark:hover:bg-zinc-800/80 transition-colors shadow-2xs"
				title="Refresh data"
			>
				<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
				</svg>
			</button>
			<a
				href="/apps/new"
				class="inline-flex items-center space-x-1.5 px-3.5 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold shadow-xs transition-colors"
			>
				<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
				</svg>
				<span>New App</span>
			</a>
		</div>
	</div>

	<!-- System Overview Metrics Grid -->
	<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
		<!-- Host Doctor Card -->
		<div
			class="p-4 rounded-xl border bg-white dark:bg-zinc-900/60 transition-all shadow-2xs cursor-pointer hover:border-zinc-400 dark:hover:border-zinc-700
			{doctorHealth === 'ok'
				? 'border-emerald-200 dark:border-emerald-900/50'
				: doctorHealth === 'warn'
					? 'border-amber-200 dark:border-amber-900/50'
					: 'border-rose-200 dark:border-rose-900/50'}"
			onclick={() => (doctorModalOpen = true)}
			onkeydown={(e) => e.key === 'Enter' && (doctorModalOpen = true)}
			role="button"
			tabindex="0"
		>
			<div class="flex items-center justify-between text-xs font-semibold text-zinc-500 dark:text-zinc-400">
				<span>SYSTEM HEALTH</span>
				<StatusBadge status={doctorHealth === 'ok' ? 'all green' : doctorHealth} />
			</div>
			<div class="mt-2 text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 flex items-baseline">
				{checks.length} checks
				{#if failedChecks.length > 0}
					<span class="ml-2 text-xs text-rose-500 font-normal">({failedChecks.length} failed)</span>
				{:else if warnChecks.length > 0}
					<span class="ml-2 text-xs text-amber-500 font-normal">({warnChecks.length} warnings)</span>
				{/if}
			</div>
			<div class="mt-1 text-[11px] text-zinc-500 flex items-center space-x-1">
				<span>Click to inspect doctor report</span>
				<svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
				</svg>
			</div>
		</div>

		<!-- Apps Metric -->
		<div class="p-4 rounded-xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900/60 shadow-2xs">
			<div class="flex items-center justify-between text-xs font-semibold text-zinc-500 dark:text-zinc-400">
				<span>APPLICATIONS</span>
				<svg class="w-4 h-4 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
				</svg>
			</div>
			<div class="mt-2 text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">
				{apps.length}
			</div>
			<div class="mt-1 text-[11px] text-zinc-500">
				{apps.filter((a) => a.live).length} active, {apps.filter((a) => !a.live).length} undeployed
			</div>
		</div>

		<!-- Databases Metric -->
		<div class="p-4 rounded-xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900/60 shadow-2xs">
			<div class="flex items-center justify-between text-xs font-semibold text-zinc-500 dark:text-zinc-400">
				<span>MANAGED DATABASES</span>
				<svg class="w-4 h-4 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4" />
				</svg>
			</div>
			<div class="mt-2 text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">
				{databases.length}
			</div>
			<div class="mt-1 text-[11px] text-zinc-500">
				{databases.filter((d) => d.status === 'active').length} active containers
			</div>
		</div>

		<!-- Daemon Memory & Uptime -->
		<div class="p-4 rounded-xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900/60 shadow-2xs">
			<div class="flex items-center justify-between text-xs font-semibold text-zinc-500 dark:text-zinc-400">
				<span>DAEMON RSS</span>
				<span class="text-xs font-mono text-emerald-600 dark:text-emerald-400 font-bold">&lt; 30 MB budget</span>
			</div>
			<div class="mt-2 text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">
				{status ? (status.rssKB / 1024).toFixed(1) : '0'} MB
			</div>
			<div class="mt-1 text-[11px] text-zinc-500">
				PID {status?.pid || '-'} • Go {status?.goVersion || '-'}
			</div>
		</div>
	</div>

	<!-- Applications Section -->
	<div class="space-y-4">
		<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
			<h2 class="text-lg font-bold tracking-tight text-zinc-900 dark:text-zinc-100">Applications</h2>
			<div class="relative w-full sm:w-64">
				<input
					type="text"
					bind:value={searchFilter}
					placeholder="Search apps or domains..."
					class="w-full px-3 py-1.5 pl-8 text-xs rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-zinc-900 dark:text-zinc-100 placeholder-zinc-400 focus:outline-none focus:ring-1 focus:ring-indigo-500"
				/>
				<svg class="w-3.5 h-3.5 text-zinc-400 absolute left-2.5 top-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
				</svg>
			</div>
		</div>

		{#if filteredApps.length === 0}
			<div class="text-center py-12 rounded-xl border border-dashed border-zinc-300 dark:border-zinc-800 bg-white dark:bg-zinc-900/30">
				<svg class="mx-auto h-10 w-10 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
				</svg>
				<h3 class="mt-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100">No applications found</h3>
				<p class="mt-1 text-xs text-zinc-500">Deploy your first containerized Git app or Docker image.</p>
				<div class="mt-4">
					<a
						href="/apps/new"
						class="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg text-white bg-indigo-600 hover:bg-indigo-500 shadow-xs"
					>
						Create application
					</a>
				</div>
			</div>
		{:else}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each filteredApps as app}
					{@const primaryDomain = app.domains.find((d) => d.primary)?.host || app.domains[0]?.host}
					<div
						class="rounded-xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5 shadow-2xs hover:border-zinc-300 dark:hover:border-zinc-700 transition-all flex flex-col justify-between"
					>
						<div>
							<div class="flex items-start justify-between">
								<div>
									<a
										href="/apps/{app.name}"
										class="font-bold text-base text-zinc-900 dark:text-zinc-100 hover:text-indigo-600 dark:hover:text-indigo-400 transition-colors"
									>
										{app.name}
									</a>
									{#if primaryDomain}
										<div class="mt-0.5">
											<a
												href="https://{primaryDomain}"
												target="_blank"
												rel="noopener noreferrer"
												class="text-xs text-zinc-500 hover:text-indigo-600 dark:hover:text-indigo-400 inline-flex items-center space-x-1"
											>
												<span>{primaryDomain}</span>
												<svg class="w-3 h-3 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
													<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
												</svg>
											</a>
										</div>
									{/if}
								</div>
								<StatusBadge status={app.live ? 'live' : 'inactive'} />
							</div>

							<div class="mt-4 pt-3 border-t border-zinc-100 dark:border-zinc-800/80 space-y-1.5 text-xs text-zinc-500">
								<div class="flex items-center justify-between">
									<span>Strategy</span>
									<span class="font-mono text-zinc-700 dark:text-zinc-300">{app.strategy}</span>
								</div>
								<div class="flex items-center justify-between">
									<span>Source</span>
									<span class="font-mono text-zinc-700 dark:text-zinc-300 truncate max-w-[160px]">
										{app.spec.source.type === 'git'
											? app.spec.source.repo || 'git'
											: app.spec.source.image || 'image'}
									</span>
								</div>
								{#if app.live}
									<div class="flex items-center justify-between">
										<span>Release</span>
										<span class="font-mono text-indigo-600 dark:text-indigo-400 font-semibold">
											{app.live.rel8}
										</span>
									</div>
									{#if app.live.message}
										<div class="text-[11px] text-zinc-400 truncate pt-1 italic">
											"{app.live.message}"
										</div>
									{/if}
								{/if}
							</div>
						</div>

						<div class="mt-5 pt-3 border-t border-zinc-100 dark:border-zinc-800/80 flex items-center justify-between gap-2">
							<a
								href="/apps/{app.name}"
								class="px-2.5 py-1.5 rounded-lg border border-zinc-200 dark:border-zinc-800 text-xs font-medium text-zinc-700 dark:text-zinc-300 hover:bg-zinc-50 dark:hover:bg-zinc-800 transition-colors"
							>
								Inspect
							</a>
							<button
								type="button"
								onclick={() => triggerDeploy(app.name)}
								class="px-2.5 py-1.5 rounded-lg bg-indigo-50 dark:bg-indigo-950/60 text-indigo-600 dark:text-indigo-400 hover:bg-indigo-100 dark:hover:bg-indigo-900/60 text-xs font-semibold transition-colors"
							>
								Redeploy
							</button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>

	<!-- Managed Databases Section -->
	<div class="space-y-4 pt-4 border-t border-zinc-200 dark:border-zinc-800">
		<div class="flex items-center justify-between">
			<h2 class="text-lg font-bold tracking-tight text-zinc-900 dark:text-zinc-100">Databases</h2>
			<a
				href="/databases"
				class="text-xs font-semibold text-indigo-600 dark:text-indigo-400 hover:underline inline-flex items-center space-x-1"
			>
				<span>View all databases</span>
				<svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
				</svg>
			</a>
		</div>

		{#if databases.length === 0}
			<div class="text-center py-8 rounded-xl border border-dashed border-zinc-300 dark:border-zinc-800 bg-white dark:bg-zinc-900/30">
				<p class="text-xs text-zinc-500">No managed databases provisioned yet.</p>
				<a href="/databases" class="mt-2 inline-block text-xs font-semibold text-indigo-600 dark:text-indigo-400">
					Create PostgreSQL, MariaDB, or Valkey
				</a>
			</div>
		{:else}
			<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each databases as db}
					<a
						href="/databases/{db.name}"
						class="rounded-xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4 shadow-2xs hover:border-zinc-300 dark:hover:border-zinc-700 transition-all flex items-center justify-between group"
					>
						<div class="flex items-center space-x-3">
							<div class="w-9 h-9 rounded-lg bg-zinc-100 dark:bg-zinc-800 flex items-center justify-center font-bold text-xs uppercase text-zinc-700 dark:text-zinc-300">
								{db.engine.slice(0, 2)}
							</div>
							<div>
								<h3 class="font-bold text-sm text-zinc-900 dark:text-zinc-100 group-hover:text-indigo-600 dark:group-hover:text-indigo-400 transition-colors">
									{db.name}
								</h3>
								<p class="text-xs text-zinc-500 font-mono">
									{db.engine} {db.version} • :{db.port}
								</p>
							</div>
						</div>
						<StatusBadge status={db.status} />
					</a>
				{/each}
			</div>
		{/if}
	</div>
</div>

<!-- Doctor Health Checks Modal -->
<Modal open={doctorModalOpen} title="Host Health & Environment Doctor" onclose={() => (doctorModalOpen = false)}>
	<div class="space-y-4">
		<p class="text-xs text-zinc-500 leading-relaxed">
			Diagnostics performed by the Dermaga engine verifying host privileges, user lingering, subUID allocations, systemd sockets, and container network isolation.
		</p>

		<div class="divide-y divide-zinc-100 dark:divide-zinc-800 rounded-lg border border-zinc-200 dark:border-zinc-800 overflow-hidden text-xs">
			{#each checks as c}
				<div class="p-3 flex items-start justify-between bg-white dark:bg-zinc-950">
					<div class="space-y-0.5 max-w-[70%]">
						<span class="font-bold text-zinc-800 dark:text-zinc-200 uppercase tracking-wider">{c.name}</span>
						<p class="text-zinc-500 break-words">{c.detail}</p>
					</div>
					<StatusBadge status={c.status} />
				</div>
			{/each}
		</div>
	</div>
</Modal>
