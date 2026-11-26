<script lang="ts">
	import { page } from '$app/state'
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
	} from '#lib/api.js'
	import EnvEditor from '#lib/components/EnvEditor.svelte'
	import PageHeader from '#lib/components/PageHeader.svelte'
	import StatusBadge from '#lib/components/StatusBadge.svelte'
	import SubNav, { type SubNavItem } from '#lib/components/SubNav.svelte'
	import type {
		AppDetailResponse,
		DeploymentInfo,
		EnvVarInput
	} from '#lib/types/api.js'
	import { onDestroy, onMount } from 'svelte'

	type TabId =
		'overview' | 'deployments' | 'logs' | 'environment' | 'settings' | 'danger'
	const tabs: SubNavItem[] = [
		{ id: 'overview', label: 'Overview' },
		{ id: 'deployments', label: 'Deployments' },
		{ id: 'logs', label: 'Logs' },
		{ id: 'environment', label: 'Environment' },
		{ id: 'settings', label: 'Settings' },
		{ id: 'danger', label: 'Danger zone', danger: true }
	]

	let activeTab = $state<TabId>('overview')
	let app = $state<AppDetailResponse | null>(null)
	let loading = $state(true)
	let error = $state<string | null>(null)
	let actionMsg = $state<string | null>(null)
	let actionErr = $state<string | null>(null)

	// Deployments & build log drawer
	let selectedDep = $state<DeploymentInfo | null>(null)
	let liveBuildLogs = $state('')
	let stopBuildStream: (() => void) | null = null
	let deploying = $state(false)

	// Runtime logs
	let runtimeLines = $state(200)
	let runtimeFollow = $state(true)
	let runtimeLogText = $state('')
	let runtimeErr = $state<string | null>(null)
	let stopRuntimeStream: (() => void) | null = null

	// Environment
	let revealedValues = $state<Record<string, string>>({})
	let editingEnv = $state(false)
	let envDraft = $state<EnvVarInput[]>([])
	let savingEnv = $state(false)
	let envChangedNotice = $state(false)

	// Settings
	let sBranch = $state('')
	let sContainerfile = $state('')
	let sBuildContext = $state('.')
	let sPort = $state(8080)
	let sHealthType = $state<'http' | 'cmd' | 'none'>('http')
	let sHealthPath = $state('/health')
	let sHealthCmd = $state('')
	let sHealthTimeout = $state(30)
	let sHealthConsecutive = $state(3)
	let sMemory = $state('512m')
	let sCPUs = $state('1.0')
	let sPids = $state(128)
	let sReadOnly = $state(false)
	let sCaps = $state('')
	let savingSettings = $state(false)

	// Danger zone
	let confirmName = $state('')
	let deleting = $state(false)

	let appId = $derived(page.params.id ?? '')

	onMount(async () => {
		try {
			const sess = await getSession()
			if (sess.setup_required) {
				window.location.href = '/setup'
				return
			}
			if (!sess.authenticated) {
				window.location.href = '/login'
				return
			}
			await loadApp()
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load app'
		} finally {
			loading = false
		}
	})

	onDestroy(() => {
		stopBuildStream?.()
		stopRuntimeStream?.()
	})

	async function loadApp() {
		const res = await getApp(appId)
		app = res
		sBranch = res.branch
		sContainerfile = res.containerfile_path
		sBuildContext = res.build_context
		sPort = res.port
		sHealthType = (res.health_type as 'http' | 'cmd' | 'none') || 'http'
		sHealthPath = res.health_path || '/health'
		sHealthCmd = res.health_cmd || ''
		sHealthTimeout = res.health_timeout_sec
		sHealthConsecutive = res.health_consecutive
		sMemory = res.memory_limit
		sCPUs = res.cpu_limit
		sPids = res.pids_limit
		sReadOnly = res.read_only_root
		sCaps = (res.add_capabilities || []).join(', ')
	}

	function selectTab(id: TabId) {
		activeTab = id
		actionMsg = null
		actionErr = null
		if (id === 'logs') {
			startRuntimeLogStream()
		} else {
			stopRuntimeStream?.()
			stopRuntimeStream = null
		}
	}

	function formatDuration(start: string, end?: string): string {
		if (!start || !end) return 'running'
		const ms = new Date(end).getTime() - new Date(start).getTime()
		if (Number.isNaN(ms) || ms < 0) return '-'
		const s = Math.round(ms / 1000)
		return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`
	}

	function openBuildLog(dep: DeploymentInfo) {
		selectedDep = dep
		liveBuildLogs = dep.build_logs || ''
		stopBuildStream?.()
		const terminal = [
			'live',
			'failed',
			'cancelled',
			'replaced by a newer deploy'
		].includes(dep.status_label)
		if (!terminal) {
			liveBuildLogs = ''
			stopBuildStream = streamBuildLogs(dep.id, {
				onLog: (chunk) => {
					liveBuildLogs += chunk
				},
				onStatus: (label) => {
					if (selectedDep && selectedDep.id === dep.id) {
						selectedDep = { ...selectedDep, status_label: label }
					}
				},
				onDone: async () => {
					await loadApp()
				}
			})
		}
	}

	async function handleTriggerDeploy() {
		if (!app) return
		deploying = true
		actionErr = null
		actionMsg = null
		try {
			const dep = await triggerDeploy(app.id)
			envChangedNotice = false
			await loadApp()
			activeTab = 'deployments'
			openBuildLog(dep)
		} catch (err) {
			actionErr = err instanceof Error ? err.message : 'Failed to start deploy'
		} finally {
			deploying = false
		}
	}

	async function handleCancelDep(depId: string) {
		actionErr = null
		try {
			await cancelDeployment(depId)
			await loadApp()
			actionMsg = 'Deployment cancelled.'
		} catch (err) {
			actionErr =
				err instanceof Error ? err.message : 'Failed to cancel deployment'
		}
	}

	function startRuntimeLogStream() {
		if (!app) return
		stopRuntimeStream?.()
		runtimeLogText = ''
		runtimeErr = null
		stopRuntimeStream = streamRuntimeLogs(app.id, runtimeLines, runtimeFollow, {
			onLog: (line) => {
				runtimeLogText += line + '\n'
			},
			onError: (msg) => {
				runtimeErr = msg
			}
		})
	}

	async function handleReveal(key: string, scope: 'runtime' | 'build_time') {
		if (!app) return
		const id = `${key}:${scope}`
		if (revealedValues[id] !== undefined) {
			const copy = { ...revealedValues }
			delete copy[id]
			revealedValues = copy
			return
		}
		try {
			const res = await revealAppEnv(app.id, { key, scope })
			revealedValues = { ...revealedValues, [id]: res.value }
		} catch (err) {
			actionErr =
				err instanceof Error ? err.message : 'Failed to reveal variable'
		}
	}

	async function startEditEnv() {
		if (!app) return
		actionErr = null
		const rows: EnvVarInput[] = []
		for (const m of app.env) {
			const scope = m.runtime ? 'runtime' : 'build_time'
			const res = await revealAppEnv(app.id, { key: m.key, scope })
			rows.push({
				key: m.key,
				value: res.value,
				runtime: m.runtime,
				build_time: m.build_time
			})
		}
		envDraft = rows
		editingEnv = true
	}

	async function handleSaveEnv() {
		if (!app) return
		savingEnv = true
		actionErr = null
		actionMsg = null
		try {
			const cleaned = envDraft.filter((r) => r.key.trim() !== '')
			await updateAppEnv(app.id, { env: cleaned })
			editingEnv = false
			revealedValues = {}
			envChangedNotice = true
			await loadApp()
			actionMsg =
				'Environment variables saved. Changes take effect on the next deploy.'
		} catch (err) {
			actionErr =
				err instanceof Error ? err.message : 'Failed to save environment'
		} finally {
			savingEnv = false
		}
	}

	async function handleSaveSettings(e: SubmitEvent) {
		e.preventDefault()
		if (!app) return
		savingSettings = true
		actionErr = null
		actionMsg = null
		try {
			const caps = sCaps
				.split(',')
				.map((c) => c.trim())
				.filter(Boolean)
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
			})
			await loadApp()
			actionMsg = 'Settings saved. Deploy to apply changes.'
		} catch (err) {
			actionErr = err instanceof Error ? err.message : 'Failed to save settings'
		} finally {
			savingSettings = false
		}
	}

	async function handleDelete(e: SubmitEvent) {
		e.preventDefault()
		if (!app) return
		deleting = true
		actionErr = null
		try {
			await deleteApp(app.id, { confirm_name: confirmName.trim() })
			window.location.href = '/'
		} catch (err) {
			actionErr = err instanceof Error ? err.message : 'Failed to delete app'
			deleting = false
		}
	}
</script>

<svelte:head>
	<title>{app ? app.name : 'App'} &bull; dermaga</title>
</svelte:head>

<div class="space-y-6">
	{#if error}
		<div
			role="alert"
			class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
		>
			{error}
		</div>
	{:else if loading || !app}
		<div
			class="h-48 animate-pulse rounded-xl bg-stone-100 dark:bg-stone-800/50"
		></div>
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
						pulse={latest.status_label === 'queued' ||
							latest.status_label === 'building' ||
							latest.status_label === 'starting' ||
							latest.status_label === 'healthy'}
					/>
				{/if}
				<button
					type="button"
					onclick={handleTriggerDeploy}
					disabled={deploying}
					class="w-fit rounded-lg bg-brand-primary px-4 py-2 text-xs font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:outline-none disabled:opacity-50"
				>
					{deploying ? 'Queueing...' : 'Deploy'}
				</button>
			{/snippet}
		</PageHeader>

		<div class="flex flex-col items-start gap-6 md:flex-row">
			<SubNav
				items={tabs}
				active={activeTab}
				onSelect={(id) => selectTab(id as TabId)}
				ariaLabel="App sections"
			/>

			<div class="w-full min-w-0 flex-1 space-y-4">
				{#if actionMsg}
					<div
						role="status"
						class="rounded-lg border border-emerald-200 bg-emerald-50 p-3 text-xs text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300"
					>
						{actionMsg}
					</div>
				{/if}
				{#if actionErr}
					<div
						role="alert"
						class="rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
					>
						{actionErr}
					</div>
				{/if}

				{#if activeTab === 'overview'}
					<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
						<div
							class="space-y-2 rounded-xl border border-stone-200 bg-white p-5 text-xs dark:border-stone-800 dark:bg-stone-900"
						>
							<h2 class="text-sm font-semibold">Live Endpoint</h2>
							<p>
								Primary domain:
								<a
									href="https://{app.primary_domain}"
									target="_blank"
									rel="noopener noreferrer"
									class="font-mono text-brand-text underline"
								>
									https://{app.primary_domain}
								</a>
							</p>
							<p class="text-stone-600 dark:text-stone-400">
								Container port: <span class="font-mono">{app.port}</span>
							</p>
							<p class="text-stone-600 dark:text-stone-400">
								Health check: <span class="font-mono">
									{app.health_type}{app.health_type === 'http'
										? ` ${app.health_path}`
										: ''}
								</span>
							</p>
						</div>
						<div
							class="space-y-2 rounded-xl border border-stone-200 bg-white p-5 text-xs dark:border-stone-800 dark:bg-stone-900"
						>
							<h2 class="text-sm font-semibold">Latest Deployment</h2>
							{#if app.deployments.length === 0}
								<p class="text-stone-600 dark:text-stone-400">
									No deployments yet.
								</p>
							{:else}
								{@const latest = app.deployments[0]}
								<p>
									Release <span class="font-mono font-semibold">
										r{latest.release_number}
									</span>
									&mdash;
									<span class="font-mono font-semibold">
										{latest.status_label}
									</span>
								</p>
								<p class="truncate text-stone-600 dark:text-stone-400">
									{latest.commit_message}
								</p>
								{#if latest.error_message}
									<p class="text-red-700 dark:text-red-400">
										{latest.error_message}
									</p>
								{/if}
							{/if}
						</div>
					</div>
				{:else if activeTab === 'deployments'}
					<div class="space-y-4">
						{#if app.deployments.length === 0}
							<p class="text-xs text-stone-600 dark:text-stone-400">
								No deployments recorded yet.
							</p>
						{:else}
							<div class="space-y-2.5">
								{#each app.deployments as dep}
									<div
										class="flex flex-col justify-between gap-3 rounded-lg border border-stone-200 bg-white p-4 text-xs sm:flex-row sm:items-center dark:border-stone-800 dark:bg-stone-900"
									>
										<div class="min-w-0 space-y-1">
											<div class="flex flex-wrap items-center gap-2">
												<span class="font-mono font-semibold tabular-nums">
													r{dep.release_number}
												</span>
												<StatusBadge label={dep.status_label} />
												<span class="text-stone-500 dark:text-stone-400">
													{dep.trigger}
												</span>
												<span class="text-stone-500 dark:text-stone-400">
													{formatDuration(dep.started_at, dep.finished_at)}
												</span>
											</div>
											<p
												class="truncate font-mono text-stone-600 dark:text-stone-400"
											>
												{dep.commit_hash ? dep.commit_hash.slice(0, 7) : 'HEAD'} &mdash;
												{dep.commit_message}
											</p>
											{#if dep.error_message}
												<p class="text-red-700 dark:text-red-400">
													{dep.error_message}
												</p>
											{/if}
										</div>
										<div class="flex shrink-0 items-center gap-2">
											{#if ['queued', 'building', 'starting', 'healthy'].includes(dep.status_label)}
												<button
													type="button"
													onclick={() => handleCancelDep(dep.id)}
													class="rounded-md border border-red-300 px-3 py-1.5 text-red-700 transition hover:bg-red-50 dark:border-red-800 dark:text-red-300 dark:hover:bg-red-950/40"
												>
													Cancel
												</button>
											{/if}
											<button
												type="button"
												onclick={() => openBuildLog(dep)}
												class="rounded-md border border-stone-300 px-3 py-1.5 transition hover:bg-stone-100 dark:border-stone-700 dark:hover:bg-stone-800"
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
									<span class="font-semibold">
										Build log &mdash; r{selectedDep.release_number} ({selectedDep.status_label})
									</span>
									<button
										type="button"
										onclick={() => {
											selectedDep = null
											stopBuildStream?.()
										}}
										class="text-stone-500 hover:underline"
									>
										Close
									</button>
								</div>
								<pre
									class="max-h-96 overflow-x-auto rounded-lg bg-stone-950 p-4 font-mono text-xs whitespace-pre-wrap text-stone-100">{liveBuildLogs ||
										'No build log output.'}</pre>
							</div>
						{/if}
					</div>
				{:else if activeTab === 'logs'}
					<div class="space-y-3">
						<div
							class="flex flex-wrap items-center justify-between gap-3 text-xs"
						>
							<div class="flex items-center gap-3">
								<label class="inline-flex items-center gap-1.5">
									<span>Lines:</span>
									<select
										bind:value={runtimeLines}
										onchange={startRuntimeLogStream}
										class="rounded border border-stone-300 bg-white px-2 py-1 dark:border-stone-700 dark:bg-stone-950"
									>
										<option value={100}>100</option>
										<option value={200}>200</option>
										<option value={500}>500</option>
									</select>
								</label>
								<label class="inline-flex cursor-pointer items-center gap-1.5">
									<input
										type="checkbox"
										bind:checked={runtimeFollow}
										onchange={startRuntimeLogStream}
									/>
									<span>Follow live</span>
								</label>
							</div>
							<button
								type="button"
								onclick={startRuntimeLogStream}
								class="rounded-md border border-stone-300 px-3 py-1.5 transition hover:bg-stone-100 dark:border-stone-700 dark:hover:bg-stone-800"
							>
								Reconnect
							</button>
						</div>
						{#if runtimeErr}
							<div
								role="alert"
								class="rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
							>
								{runtimeErr}
							</div>
						{/if}
						<pre
							class="max-h-112 overflow-x-auto rounded-lg bg-stone-950 p-4 font-mono text-xs whitespace-pre-wrap text-stone-100">{runtimeLogText ||
								'Waiting for runtime logs...'}</pre>
					</div>
				{:else if activeTab === 'environment'}
					<div
						class="space-y-4 rounded-xl border border-stone-200 bg-white p-5 dark:border-stone-800 dark:bg-stone-900"
					>
						<div class="flex flex-wrap items-center justify-between gap-2">
							<div>
								<h2 class="text-sm font-semibold">Environment Variables</h2>
								<p class="text-xs text-stone-600 dark:text-stone-400">
									Values are encrypted at rest and hidden by default. Reveals
									are recorded in the audit log.
								</p>
							</div>
							{#if !editingEnv}
								<button
									type="button"
									onclick={startEditEnv}
									class="rounded-md border border-stone-300 px-3 py-1.5 text-xs font-medium transition hover:bg-stone-100 dark:border-stone-700 dark:hover:bg-stone-800"
								>
									Edit variables
								</button>
							{/if}
						</div>

						{#if envChangedNotice}
							<div
								role="status"
								class="flex flex-wrap items-center justify-between gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
							>
								<span>
									Environment updated. Changes take effect on the next deploy.
								</span>
								<button
									type="button"
									onclick={handleTriggerDeploy}
									class="rounded bg-brand-primary px-3 py-1 font-medium text-brand-btn-text hover:bg-brand-hover"
								>
									Redeploy now
								</button>
							</div>
						{/if}

						{#if editingEnv}
							<EnvEditor bind:items={envDraft} disabled={savingEnv} />
							<div class="flex justify-end gap-2 pt-2">
								<button
									type="button"
									onclick={() => (editingEnv = false)}
									class="rounded-md border border-stone-300 px-3 py-1.5 text-xs dark:border-stone-700"
								>
									Cancel
								</button>
								<button
									type="button"
									onclick={handleSaveEnv}
									disabled={savingEnv}
									class="rounded-md bg-brand-primary px-4 py-1.5 text-xs font-medium text-brand-btn-text hover:bg-brand-hover disabled:opacity-50"
								>
									{savingEnv ? 'Saving...' : 'Save environment'}
								</button>
							</div>
						{:else if app.env.length === 0}
							<p class="text-xs text-stone-600 dark:text-stone-400">
								No environment variables configured.
							</p>
						{:else}
							<div class="space-y-2">
								{#each app.env as item}
									{@const scope = item.runtime ? 'runtime' : 'build_time'}
									{@const revKey = `${item.key}:${scope}`}
									<div
										class="flex flex-col justify-between gap-2 rounded-lg border border-stone-200 p-3 text-xs sm:flex-row sm:items-center dark:border-stone-800"
									>
										<div class="min-w-0 space-y-1">
											<div class="flex flex-wrap items-center gap-2">
												<span class="font-mono font-semibold">{item.key}</span>
												{#if item.runtime}
													<span
														class="rounded bg-stone-100 px-1.5 py-0.5 text-[10px] dark:bg-stone-800"
													>
														Runtime
													</span>
												{/if}
												{#if item.build_time}
													<span
														class="rounded bg-amber-100 px-1.5 py-0.5 text-[10px] text-amber-800 dark:bg-amber-900/50 dark:text-amber-300"
													>
														Build time (--build-arg)
													</span>
												{/if}
											</div>
											<p
												class="font-mono break-all text-stone-600 dark:text-stone-400"
											>
												{revealedValues[revKey] !== undefined
													? revealedValues[revKey]
													: '••••••••••••'}
											</p>
										</div>
										<button
											type="button"
											onclick={() => handleReveal(item.key, scope)}
											class="w-fit shrink-0 rounded-md border border-stone-300 px-3 py-1.5 hover:bg-stone-100 dark:border-stone-700 dark:hover:bg-stone-800"
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
						<div
							class="space-y-4 rounded-xl border border-stone-200 bg-white p-5 text-xs dark:border-stone-800 dark:bg-stone-900"
						>
							<h2 class="text-sm font-semibold">Health Check (APP-5)</h2>
							<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
								<div>
									<label for="set-htype" class="mb-1 block font-medium">
										Type
									</label>
									<select
										id="set-htype"
										bind:value={sHealthType}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 dark:border-stone-700 dark:bg-stone-950"
									>
										<option value="http">HTTP path</option>
										<option value="cmd">Container command</option>
										<option value="none">None</option>
									</select>
								</div>
								<div>
									<label for="set-port" class="mb-1 block font-medium">
										Container Port
									</label>
									<input
										id="set-port"
										type="number"
										min="1"
										max="65535"
										bind:value={sPort}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
								{#if sHealthType === 'http'}
									<div>
										<label for="set-hpath" class="mb-1 block font-medium">
											HTTP Path
										</label>
										<input
											id="set-hpath"
											type="text"
											bind:value={sHealthPath}
											class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
										/>
									</div>
								{:else if sHealthType === 'cmd'}
									<div>
										<label for="set-hcmd" class="mb-1 block font-medium">
											Command
										</label>
										<input
											id="set-hcmd"
											type="text"
											bind:value={sHealthCmd}
											class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
										/>
									</div>
								{/if}
								<div>
									<label for="set-htimeout" class="mb-1 block font-medium">
										Start Timeout (s)
									</label>
									<input
										id="set-htimeout"
										type="number"
										min="1"
										bind:value={sHealthTimeout}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
								<div>
									<label for="set-hconsec" class="mb-1 block font-medium">
										Consecutive Successes
									</label>
									<input
										id="set-hconsec"
										type="number"
										min="1"
										bind:value={sHealthConsecutive}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
							</div>
						</div>

						<div
							class="space-y-4 rounded-xl border border-stone-200 bg-white p-5 text-xs dark:border-stone-800 dark:bg-stone-900"
						>
							<h2 class="text-sm font-semibold">
								Resources &amp; Security (APP-6)
							</h2>
							<div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
								<div>
									<label for="set-mem" class="mb-1 block font-medium">
										Memory Limit
									</label>
									<input
										id="set-mem"
										type="text"
										bind:value={sMemory}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
								<div>
									<label for="set-cpu" class="mb-1 block font-medium">
										CPU Limit
									</label>
									<input
										id="set-cpu"
										type="text"
										bind:value={sCPUs}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
								<div>
									<label for="set-pids" class="mb-1 block font-medium">
										PIDs Limit
									</label>
									<input
										id="set-pids"
										type="number"
										min="1"
										bind:value={sPids}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
							</div>
							<div class="flex flex-wrap items-center gap-4">
								<label class="inline-flex cursor-pointer items-center gap-2">
									<input type="checkbox" bind:checked={sReadOnly} />
									<span>Read-only root filesystem</span>
								</label>
							</div>
							<div>
								<label for="set-caps" class="mb-1 block font-medium">
									Extra Capabilities (comma-separated, default none)
								</label>
								<input
									id="set-caps"
									type="text"
									bind:value={sCaps}
									placeholder="NET_BIND_SERVICE"
									class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
								/>
							</div>
						</div>

						<div
							class="space-y-4 rounded-xl border border-stone-200 bg-white p-5 text-xs dark:border-stone-800 dark:bg-stone-900"
						>
							<h2 class="text-sm font-semibold">Build (APP-7)</h2>
							<div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
								<div>
									<label for="set-branch" class="mb-1 block font-medium">
										Branch
									</label>
									<input
										id="set-branch"
										type="text"
										bind:value={sBranch}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
								<div>
									<label for="set-cf" class="mb-1 block font-medium">
										Containerfile Path
									</label>
									<input
										id="set-cf"
										type="text"
										bind:value={sContainerfile}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
								<div>
									<label for="set-ctx" class="mb-1 block font-medium">
										Build Context
									</label>
									<input
										id="set-ctx"
										type="text"
										bind:value={sBuildContext}
										class="w-full rounded-md border border-stone-300 bg-white px-3 py-2 font-mono dark:border-stone-700 dark:bg-stone-950"
									/>
								</div>
							</div>
						</div>

						<div class="flex justify-end">
							<button
								type="submit"
								disabled={savingSettings}
								class="rounded-md bg-brand-primary px-4 py-2 text-xs font-medium text-brand-btn-text hover:bg-brand-hover disabled:opacity-50"
							>
								{savingSettings ? 'Saving...' : 'Save settings'}
							</button>
						</div>
					</form>
				{:else if activeTab === 'danger'}
					<form
						onsubmit={handleDelete}
						class="space-y-4 rounded-xl border border-red-200 bg-white p-5 text-xs dark:border-red-900 dark:bg-stone-900"
					>
						<div>
							<h2 class="text-sm font-semibold text-red-700 dark:text-red-400">
								Remove App (APP-9)
							</h2>
							<p class="mt-1 text-stone-600 dark:text-stone-400">
								Permanently removes containers, images, Quadlet units, Traefik
								routes, env files, and the Git mirror for <code
									class="font-mono font-semibold"
								>
									{app.name}
								</code>.
							</p>
						</div>
						<div>
							<label for="confirm-delete" class="mb-1 block font-medium">
								Type <code class="font-mono font-semibold">{app.name}</code> to confirm
							</label>
							<input
								id="confirm-delete"
								type="text"
								bind:value={confirmName}
								placeholder={app.name}
								class="w-full rounded-md border border-red-300 bg-white px-3 py-2 font-mono focus:ring-2 focus:ring-red-500 focus:outline-none sm:w-80 dark:border-red-800 dark:bg-stone-950"
							/>
						</div>
						<button
							type="submit"
							disabled={deleting || confirmName.trim() !== app.name}
							class="rounded-md bg-red-600 px-4 py-2 font-medium text-white transition hover:bg-red-700 disabled:opacity-50"
						>
							{deleting ? 'Removing...' : 'Remove app'}
						</button>
					</form>
				{/if}
			</div>
		</div>
	{/if}
</div>
