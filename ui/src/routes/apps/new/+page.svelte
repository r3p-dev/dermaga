<script lang="ts">
	import {
		cancelDeployment,
		createApp,
		getGitHubStatus,
		getSession,
		inspectRepo,
		listRepoBranches,
		streamBuildLogs
	} from '#lib/api.js';
	import DomainDNSChecker from '#lib/components/DomainDNSChecker.svelte';
	import EnvEditor from '#lib/components/EnvEditor.svelte';
	import type {
		EnvVarInput,
		GitHubStatusResponse,
		RepoInspectResponse
	} from '#lib/types/api.js';
	import { onDestroy, onMount } from 'svelte';

	let step = $state<1 | 2 | 3 | 4>(1);
	let loading = $state(true);
	let ghStatus = $state<GitHubStatusResponse | null>(null);
	let loadError = $state<string | null>(null);

	// Step 1: Repo & Branch
	let selectedRepo = $state('');
	let appName = $state('');
	let branches = $state<string[]>([]);
	let branch = $state('');
	let loadingBranches = $state(false);

	// Step 2: Build & Port
	let inspecting = $state(false);
	let inspection = $state<RepoInspectResponse | null>(null);
	let inspectedKey = $state('');
	let inspectError = $state<string | null>(null);
	let containerfilePath = $state('Containerfile');
	let buildContext = $state('.');
	let port = $state<string>('');

	// Step 3: Env, Health & Domain
	let envItems = $state<EnvVarInput[]>([]);
	let healthType = $state<'http' | 'cmd' | 'none'>('http');
	let healthPath = $state('/health');
	let healthCmd = $state('');
	let domain = $state('');

	// Step 4: Deploy & Live Log
	let submitting = $state(false);
	let formError = $state<string | null>(null);
	let fieldErrors = $state<Record<string, string>>({});
	let deploymentId = $state<string | null>(null);
	let deployStatus = $state<string>('');
	let deployError = $state<string | null>(null);
	let buildLogs = $state('');
	let cancelling = $state(false);
	let stopStream: (() => void) | null = null;

	const steps = [
		{ num: 1 as const, label: '1. Repo & Branch' },
		{ num: 2 as const, label: '2. Build & Port' },
		{ num: 3 as const, label: '3. Env, Health & Domain' },
		{ num: 4 as const, label: '4. Deploy' }
	];

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
			ghStatus = await getGitHubStatus();
			if (ghStatus.repositories?.length > 0) {
				await selectRepository(ghStatus.repositories[0].full_name, ghStatus.repositories[0].default_branch);
			}
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Failed to load wizard';
		} finally {
			loading = false;
		}
	});

	onDestroy(() => {
		stopStream?.();
	});

	function deriveAppName(repoFull: string): string {
		const parts = repoFull.split('/');
		const raw = (parts[1] || parts[0] || '').toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/^-+|-+$/g, '');
		return raw.slice(0, 32);
	}

	async function selectRepository(fullName: string, defaultBranch?: string) {
		selectedRepo = fullName;
		if (!appName) {
			appName = deriveAppName(fullName);
		}
		branch = defaultBranch || 'main';
		const [owner, repo] = fullName.split('/');
		if (!owner || !repo) return;
		loadingBranches = true;
		try {
			const res = await listRepoBranches(owner, repo);
			branches = res.branches || [];
			if (branches.length > 0 && !branches.includes(branch)) {
				branch = branches[0];
			}
		} catch {
			branches = branch ? [branch] : [];
		} finally {
			loadingBranches = false;
		}
	}

	async function runInspection() {
		const [owner, repo] = selectedRepo.split('/');
		if (!owner || !repo) return;
		inspecting = true;
		inspectError = null;
		try {
			inspection = await inspectRepo(owner, repo, branch);
			inspectedKey = `${selectedRepo}@${branch}`;
			if (inspection.has_containerfile) {
				containerfilePath = inspection.containerfile;
				buildContext = inspection.build_context || '.';
				if (inspection.suggested_port > 0 && !port) {
					port = String(inspection.suggested_port);
				}
			}
		} catch (err) {
			inspectError = err instanceof Error ? err.message : 'Failed to inspect repository';
		} finally {
			inspecting = false;
		}
	}

	async function goToStep(target: 1 | 2 | 3 | 4) {
		formError = null;
		if (target === 2 && step === 1) {
			step = 2;
			if (inspectedKey !== `${selectedRepo}@${branch}`) {
				await runInspection();
			}
			return;
		}
		step = target;
	}

	function mapFieldStep(fields: Record<string, string>): 1 | 2 | 3 {
		if (fields.name || fields.repo_full_name || fields.branch) return 1;
		if (fields.port || fields.containerfile_path || fields.build_context) return 2;
		return 3;
	}

	async function handleDeploy() {
		submitting = true;
		formError = null;
		fieldErrors = {};
		deployError = null;
		buildLogs = '';

		const parsedPort = Number.parseInt(port, 10) || 0;
		const cleanedEnv = envItems.filter((item) => item.key.trim() !== '');

		const res = await createApp({
			name: appName.trim(),
			repo_full_name: selectedRepo,
			branch: branch.trim(),
			containerfile_path: containerfilePath.trim(),
			build_context: buildContext.trim(),
			port: parsedPort,
			health_type: healthType,
			health_path: healthPath.trim(),
			health_cmd: healthCmd.trim(),
			health_timeout_sec: 30,
			health_consecutive: 3,
			domain: domain.trim(),
			env: cleanedEnv
		});

		submitting = false;
		if (!res.ok || !res.data) {
			formError = res.error || 'Failed to create app';
			fieldErrors = res.fields || {};
			if (Object.keys(fieldErrors).length > 0) {
				step = mapFieldStep(fieldErrors);
			}
			return;
		}

		const created = res.data;
		deploymentId = created.deployment.id;
		deployStatus = created.deployment.status_label;
		const appId = created.app.id;

		stopStream?.();
		stopStream = streamBuildLogs(created.deployment.id, {
			onLog: (chunk) => {
				buildLogs += chunk;
			},
			onStatus: (label) => {
				deployStatus = label;
			},
			onDone: (errMsg) => {
				if (errMsg) {
					deployError = errMsg;
				}
				if (deployStatus === 'live') {
					// Keep live URL visible on Step 4, with link to App page.
					createdAppId = appId;
				}
			},
			onError: () => {
				if (deployStatus !== 'live' && deployStatus !== 'failed' && deployStatus !== 'cancelled') {
					deployError = 'Log stream disconnected. Check the app page for current status.';
				}
			}
		});
		createdAppId = appId;
	}

	let createdAppId = $state<string | null>(null);

	async function handleCancel() {
		if (!deploymentId) return;
		cancelling = true;
		try {
			await cancelDeployment(deploymentId);
			deployStatus = 'cancelled';
		} catch (err) {
			deployError = err instanceof Error ? err.message : 'Failed to cancel deployment';
		} finally {
			cancelling = false;
		}
	}
</script>

<svelte:head>
	<title>New App &bull; dermaga</title>
</svelte:head>

<div class="max-w-3xl mx-auto space-y-6">
	<div class="flex items-center justify-between gap-4 pb-4 border-b border-stone-200 dark:border-stone-800">
		<div>
			<h1 class="text-2xl font-bold tracking-tight">New App</h1>
			<p class="text-xs text-stone-600 dark:text-stone-400 mt-1">Deploy a GitHub repository to your server</p>
		</div>
		<a href="/" class="text-xs text-stone-600 dark:text-stone-400 hover:underline">Back to Dashboard</a>
	</div>

	{#if loadError}
		<div role="alert" class="p-4 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">
			{loadError}
		</div>
	{:else if loading}
		<div class="h-48 rounded-xl bg-stone-100 dark:bg-stone-800/50 animate-pulse"></div>
	{:else if !ghStatus?.connected}
		<div class="rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-6 space-y-3">
			<h2 class="text-base font-semibold">Connect GitHub first</h2>
			<p class="text-xs text-stone-600 dark:text-stone-400">
				You need a connected GitHub App before creating an app.
			</p>
			<a
				href="/settings?tab=github"
				class="inline-flex items-center px-3.5 py-2 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover focus:outline-none focus:ring-2 focus:ring-brand-ring transition"
			>
				Go to Settings &rarr; GitHub
			</a>
		</div>
	{:else if (ghStatus.repositories || []).length === 0}
		<div class="rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-6 space-y-3">
			<h2 class="text-base font-semibold">No repositories available</h2>
			<p class="text-xs text-stone-600 dark:text-stone-400">
				Your GitHub App cannot see any repositories yet. Grant access to at least one repository on GitHub, then sync.
			</p>
			<div class="flex flex-wrap gap-2">
				{#if ghStatus.app?.slug}
					<a
						href="https://github.com/apps/{ghStatus.app.slug}/installations/new"
						target="_blank"
						rel="noopener noreferrer"
						class="inline-flex items-center px-3.5 py-2 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover transition"
					>
						Configure repositories on GitHub
					</a>
				{/if}
				<a
					href="/settings?tab=github"
					class="inline-flex items-center px-3.5 py-2 text-xs font-medium rounded-md border border-stone-300 dark:border-stone-700 text-stone-800 dark:text-stone-200 hover:bg-stone-100 dark:hover:bg-stone-800 transition"
				>
					Open Settings &rarr; GitHub
				</a>
			</div>
		</div>
	{:else}
		<!-- Stepper Indicator -->
		<nav aria-label="Wizard steps" class="flex items-center gap-2 overflow-x-auto pb-1">
			{#each steps as s}
				<button
					type="button"
					disabled={deploymentId !== null}
					onclick={() => goToStep(s.num)}
					class="shrink-0 px-3 py-1.5 rounded-md text-xs font-medium border transition focus:outline-none focus:ring-2 focus:ring-brand-ring {step === s.num
						? 'bg-brand-primary text-brand-btn-text border-brand-primary'
						: 'bg-white dark:bg-stone-900 text-stone-700 dark:text-stone-300 border-stone-200 dark:border-stone-800 hover:bg-stone-50 dark:hover:bg-stone-800'}"
				>
					{s.label}
				</button>
			{/each}
		</nav>

		{#if formError}
			<div role="alert" class="p-3 rounded-md bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs font-medium">
				{formError}
			</div>
		{/if}

		<div class="rounded-xl border border-stone-200 dark:border-stone-800 bg-white dark:bg-stone-900 p-6 shadow-sm space-y-5">
			{#if step === 1}
				<div class="space-y-4">
					<div>
						<label for="wiz-repo" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1.5">
							Repository
						</label>
						<select
							id="wiz-repo"
							value={selectedRepo}
							onchange={(e) => {
								const full = (e.currentTarget as HTMLSelectElement).value;
								const found = ghStatus?.repositories.find((r) => r.full_name === full);
								selectRepository(full, found?.default_branch);
							}}
							class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
						>
							{#each ghStatus.repositories as r}
								<option value={r.full_name}>{r.full_name}</option>
							{/each}
						</select>
						{#if fieldErrors.repo_full_name}
							<p class="text-xs text-red-700 dark:text-red-400 mt-1">{fieldErrors.repo_full_name}</p>
						{/if}
					</div>

					<div>
						<label for="wiz-branch" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1.5">
							Branch {loadingBranches ? '(loading...)' : ''}
						</label>
						{#if branches.length > 0}
							<select
								id="wiz-branch"
								bind:value={branch}
								class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
							>
								{#each branches as b}
									<option value={b}>{b}</option>
								{/each}
							</select>
						{:else}
							<input
								id="wiz-branch"
								type="text"
								bind:value={branch}
								class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
							/>
						{/if}
						{#if fieldErrors.branch}
							<p class="text-xs text-red-700 dark:text-red-400 mt-1">{fieldErrors.branch}</p>
						{/if}
					</div>

					<div>
						<label for="wiz-name" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1.5">
							App Name
						</label>
						<input
							id="wiz-name"
							type="text"
							bind:value={appName}
							placeholder="my-app"
							class="w-full px-3 py-2 text-sm font-mono rounded-md border {fieldErrors.name ? 'border-red-500' : 'border-stone-300 dark:border-stone-700'} bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
						/>
						<p class="text-[11px] text-stone-500 dark:text-stone-400 mt-1">
							Lowercase letters, digits, and hyphens (2–32 chars). Used for container and route names.
						</p>
						{#if fieldErrors.name}
							<p class="text-xs text-red-700 dark:text-red-400 mt-1">{fieldErrors.name}</p>
						{/if}
					</div>
				</div>

				<div class="flex justify-end pt-2">
					<button
						type="button"
						onclick={() => goToStep(2)}
						disabled={!selectedRepo || !branch.trim() || !appName.trim()}
						class="px-4 py-2 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover disabled:opacity-50 focus:outline-none focus:ring-2 focus:ring-brand-ring transition"
					>
						Next: Build &amp; Port
					</button>
				</div>
			{:else if step === 2}
				<div class="space-y-4">
					{#if inspecting}
						<div class="p-4 rounded-md bg-stone-50 dark:bg-stone-950 border border-stone-200 dark:border-stone-800 text-xs text-stone-600 dark:text-stone-400">
							Inspecting repository tree via GitHub API...
						</div>
					{:else if inspectError}
						<div role="alert" class="p-3 rounded-md bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">
							{inspectError}
						</div>
					{:else if inspection}
						{#if inspection.has_submodules || inspection.has_lfs}
							<div role="alert" class="p-3 rounded-md bg-amber-50 dark:bg-amber-950/40 border border-amber-200 dark:border-amber-800 text-amber-900 dark:text-amber-200 text-xs">
								<strong>Unsupported Git feature detected:</strong>
								{inspection.has_submodules ? ' Git submodules (.gitmodules)' : ''}
								{inspection.has_submodules && inspection.has_lfs ? ' and' : ''}
								{inspection.has_lfs ? ' Git LFS (filter=lfs)' : ''}
								{' '}are not supported in v1.0.0; the build may fail or be incomplete.
							</div>
						{/if}

						{#if !inspection.has_containerfile}
							<div class="p-4 rounded-lg bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 space-y-3 text-xs text-amber-950 dark:text-amber-200">
								<p class="font-semibold">No Containerfile or Dockerfile found</p>
								<p>
									Add a non-root <code class="font-mono">Containerfile</code> to your repository and commit it, or specify a custom path below.
								</p>
								<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
									<div class="p-2.5 rounded bg-white/80 dark:bg-stone-950/80 border border-amber-200/80 dark:border-amber-900 font-mono text-[11px] overflow-x-auto">
										<div class="font-sans font-semibold mb-1">Go (port 8080)</div>
										<pre>FROM golang:1.27-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /app .
FROM alpine:3.21
RUN adduser -D app
USER app
COPY --from=build /app /app
EXPOSE 8080
CMD ["/app"]</pre>
									</div>
									<div class="p-2.5 rounded bg-white/80 dark:bg-stone-950/80 border border-amber-200/80 dark:border-amber-900 font-mono text-[11px] overflow-x-auto">
										<div class="font-sans font-semibold mb-1">Static (port 8080)</div>
										<pre>FROM alpine:3.21
RUN adduser -D app
WORKDIR /srv
COPY . .
USER app
EXPOSE 8080
CMD ["httpd","-f","-p","8080"]</pre>
									</div>
								</div>
							</div>
						{/if}
					{/if}

					<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
						<div>
							<label for="wiz-cf" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1.5">
								Containerfile Path
							</label>
							<input
								id="wiz-cf"
								type="text"
								bind:value={containerfilePath}
								class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
							/>
							{#if fieldErrors.containerfile_path}
								<p class="text-xs text-red-700 dark:text-red-400 mt-1">{fieldErrors.containerfile_path}</p>
							{/if}
						</div>
						<div>
							<label for="wiz-ctx" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1.5">
								Build Context
							</label>
							<input
								id="wiz-ctx"
								type="text"
								bind:value={buildContext}
								class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
							/>
							{#if fieldErrors.build_context}
								<p class="text-xs text-red-700 dark:text-red-400 mt-1">{fieldErrors.build_context}</p>
							{/if}
						</div>
					</div>

					<div>
						<label for="wiz-port" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1.5">
							Container HTTP Port
						</label>
						<input
							id="wiz-port"
							type="number"
							min="1"
							max="65535"
							bind:value={port}
							placeholder="e.g. 3000, 8080, 80"
							class="w-full px-3 py-2 text-sm font-mono rounded-md border {fieldErrors.port ? 'border-red-500' : 'border-stone-300 dark:border-stone-700'} bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
						/>
						<p class="text-[11px] text-stone-500 dark:text-stone-400 mt-1">
							The TCP port your app listens on inside the container (common: Bun/Node 3000, Go 8080, nginx 80).
						</p>
						{#if fieldErrors.port}
							<p class="text-xs text-red-700 dark:text-red-400 mt-1">{fieldErrors.port}</p>
						{/if}
					</div>
				</div>

				<div class="flex justify-between pt-2">
					<button
						type="button"
						onclick={() => goToStep(1)}
						class="px-4 py-2 text-xs font-medium rounded-md border border-stone-300 dark:border-stone-700 text-stone-800 dark:text-stone-200 hover:bg-stone-100 dark:hover:bg-stone-800 transition"
					>
						Back
					</button>
					<button
						type="button"
						onclick={() => goToStep(3)}
						disabled={!port || Number(port) < 1 || Number(port) > 65535}
						class="px-4 py-2 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover disabled:opacity-50 focus:outline-none focus:ring-2 focus:ring-brand-ring transition"
					>
						Next: Env, Health &amp; Domain
					</button>
				</div>
			{:else if step === 3}
				<div class="space-y-6">
					<div class="space-y-2">
						<h2 class="text-sm font-semibold">Health Check</h2>
						<p class="text-xs text-stone-600 dark:text-stone-400">
							Verifies the container is ready before routing public traffic.
						</p>
						<div class="flex flex-wrap gap-3 text-xs">
							<label class="inline-flex items-center gap-1.5 cursor-pointer">
								<input type="radio" bind:group={healthType} value="http" />
								<span>HTTP path (recommended)</span>
							</label>
							<label class="inline-flex items-center gap-1.5 cursor-pointer">
								<input type="radio" bind:group={healthType} value="cmd" />
								<span>Container command</span>
							</label>
							<label class="inline-flex items-center gap-1.5 cursor-pointer">
								<input type="radio" bind:group={healthType} value="none" />
								<span>None</span>
							</label>
						</div>

						{#if healthType === 'http'}
							<div class="pt-1">
								<label for="wiz-hpath" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">
									Health Check Path
								</label>
								<input
									id="wiz-hpath"
									type="text"
									bind:value={healthPath}
									placeholder="/health"
									class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
								/>
								{#if fieldErrors.health_path}
									<p class="text-xs text-red-700 dark:text-red-400 mt-1">{fieldErrors.health_path}</p>
								{/if}
							</div>
						{:else if healthType === 'cmd'}
							<div class="pt-1">
								<label for="wiz-hcmd" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">
									Health Check Command
								</label>
								<input
									id="wiz-hcmd"
									type="text"
									bind:value={healthCmd}
									placeholder="wget -qO- http://127.0.0.1:8080/health"
									class="w-full px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring"
								/>
								{#if fieldErrors.health_cmd}
									<p class="text-xs text-red-700 dark:text-red-400 mt-1">{fieldErrors.health_cmd}</p>
								{/if}
							</div>
						{:else}
							<div role="note" class="p-3 rounded-md bg-amber-50 dark:bg-amber-950/40 border border-amber-200 dark:border-amber-800 text-amber-900 dark:text-amber-200 text-xs">
								Without a health check, zero downtime cannot be guaranteed (uses a 5-second stability window instead).
							</div>
						{/if}
					</div>

					<DomainDNSChecker bind:domain fieldError={fieldErrors.domain || null} />

					<div class="space-y-2 pt-2 border-t border-stone-200 dark:border-stone-800">
						<h2 class="text-sm font-semibold">Environment Variables</h2>
						<EnvEditor bind:items={envItems} />
						{#if fieldErrors.env}
							<p class="text-xs text-red-700 dark:text-red-400">{fieldErrors.env}</p>
						{/if}
					</div>
				</div>

				<div class="flex justify-between pt-2">
					<button
						type="button"
						onclick={() => goToStep(2)}
						class="px-4 py-2 text-xs font-medium rounded-md border border-stone-300 dark:border-stone-700 text-stone-800 dark:text-stone-200 hover:bg-stone-100 dark:hover:bg-stone-800 transition"
					>
						Back
					</button>
					<button
						type="button"
						onclick={() => goToStep(4)}
						disabled={!domain.trim()}
						class="px-4 py-2 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover disabled:opacity-50 focus:outline-none focus:ring-2 focus:ring-brand-ring transition"
					>
						Next: Review &amp; Deploy
					</button>
				</div>
			{:else if step === 4}
				<div class="space-y-4">
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-3 p-4 rounded-lg bg-stone-50 dark:bg-stone-950 border border-stone-200 dark:border-stone-800 text-xs">
						<div><span class="text-stone-500 dark:text-stone-400">App:</span> <span class="font-mono font-semibold">{appName}</span></div>
						<div><span class="text-stone-500 dark:text-stone-400">Source:</span> <span class="font-mono">{selectedRepo}@{branch}</span></div>
						<div><span class="text-stone-500 dark:text-stone-400">Containerfile:</span> <span class="font-mono">{containerfilePath}</span></div>
						<div><span class="text-stone-500 dark:text-stone-400">Port / Health:</span> <span class="font-mono">{port} / {healthType}</span></div>
						<div class="sm:col-span-2"><span class="text-stone-500 dark:text-stone-400">Domain:</span> <span class="font-mono font-semibold">{domain}</span></div>
					</div>

					{#if !deploymentId}
						<div class="flex justify-between">
							<button
								type="button"
								onclick={() => goToStep(3)}
								disabled={submitting}
								class="px-4 py-2 text-xs font-medium rounded-md border border-stone-300 dark:border-stone-700 text-stone-800 dark:text-stone-200 hover:bg-stone-100 dark:hover:bg-stone-800 transition"
							>
								Back
							</button>
							<button
								type="button"
								onclick={handleDeploy}
								disabled={submitting}
								class="px-5 py-2 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover disabled:opacity-50 focus:outline-none focus:ring-2 focus:ring-brand-ring transition"
							>
								{submitting ? 'Starting deploy...' : 'Deploy app'}
							</button>
						</div>
					{:else}
						<div class="flex flex-wrap items-center justify-between gap-2">
							<div class="flex items-center gap-2 text-xs">
								<span class="font-medium">Status:</span>
								<span class="px-2 py-0.5 rounded-full font-mono text-[11px] border border-stone-300 dark:border-stone-700">
									{deployStatus || 'queued'}
								</span>
							</div>
							<div class="flex items-center gap-2">
								{#if deployStatus === 'queued' || deployStatus === 'building' || deployStatus === 'starting' || deployStatus === 'healthy'}
									<button
										type="button"
										onclick={handleCancel}
										disabled={cancelling}
										class="px-3 py-1.5 text-xs font-medium rounded-md border border-red-300 dark:border-red-800 text-red-700 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-950/40 transition"
									>
										{cancelling ? 'Cancelling...' : 'Cancel build'}
									</button>
								{/if}
								{#if createdAppId}
									<a
										href="/apps/{createdAppId}"
										class="px-3 py-1.5 text-xs font-medium rounded-md bg-brand-primary text-brand-btn-text hover:bg-brand-hover transition"
									>
										Open App Page
									</a>
								{/if}
							</div>
						</div>

						{#if deployStatus === 'live'}
							<div role="status" class="p-3 rounded-md bg-emerald-50 dark:bg-emerald-950/40 border border-emerald-200 dark:border-emerald-800 text-emerald-800 dark:text-emerald-300 text-xs">
								Deployment is live at <a href="https://{domain}" target="_blank" rel="noopener noreferrer" class="font-mono font-semibold underline">https://{domain}</a>.
							</div>
						{/if}

						{#if deployError}
							<div role="alert" class="p-3 rounded-md bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">
								<p class="font-medium">Deploy did not succeed: {deployError}</p>
								<p class="mt-1">Fix the issue in your repository or app settings, then trigger a new deploy from the App page.</p>
							</div>
						{/if}

						<pre class="p-4 rounded-lg bg-stone-950 text-stone-100 font-mono text-xs overflow-x-auto max-h-96 whitespace-pre-wrap">{buildLogs || 'Waiting for build output...'}</pre>
					{/if}
				</div>
			{/if}
		</div>
	{/if}
</div>
