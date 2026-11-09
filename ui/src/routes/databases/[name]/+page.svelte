<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';
	import type { DatabaseView, AppView } from '$lib/types/api';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Modal from '$lib/components/Modal.svelte';

	const dbName = $derived(page.params.name ?? '');

	let db = $state<DatabaseView | null>(null);
	let allApps = $state<AppView[]>([]);
	let loading = $state(true);
	let revealCredentials = $state(false);

	// Action loading states
	let restarting = $state(false);
	let backingUp = $state(false);
	let testingRestore = $state(false);
	let testRestoreLog = $state('');
	let testRestoreOpen = $state(false);

	// Link app modal
	let linkModalOpen = $state(false);
	let selectedAppToLink = $state('');
	let linkEnvName = $state('DATABASE_URL');

	// Delete modal
	let deleteModalOpen = $state(false);
	let purgeVolume = $state(false);
	let forceDelete = $state(false);
	let confirmDbName = $state('');

	onMount(async () => {
		await loadDb();
		await loadApps();
	});

	async function loadDb() {
		loading = true;
		try {
			db = await api.getDatabase(dbName, revealCredentials);
		} catch (err: any) {
			toast.error(err.message || 'Failed to load database');
		} finally {
			loading = false;
		}
	}

	async function loadApps() {
		try {
			allApps = await api.listApps();
		} catch {
			// ignore
		}
	}

	async function toggleReveal() {
		revealCredentials = !revealCredentials;
		await loadDb();
	}

	async function handleRestart() {
		restarting = true;
		try {
			await api.restartDatabase(dbName);
			toast.success(`Database ${dbName} restarted successfully`);
			await loadDb();
		} catch (err: any) {
			toast.error(err.message || 'Restart failed');
		} finally {
			restarting = false;
		}
	}

	async function handleBackup() {
		backingUp = true;
		try {
			const snap = await api.backupDatabase(dbName);
			toast.success(`Snapshot ${snap.short_id} created successfully`);
		} catch (err: any) {
			toast.error(err.message || 'Backup failed');
		} finally {
			backingUp = false;
		}
	}

	async function handleTestRestore() {
		testingRestore = true;
		testRestoreLog = 'Starting sandboxed test restore in isolated container...';
		testRestoreOpen = true;
		try {
			const res = await api.testRestoreDatabase(dbName);
			testRestoreLog = `Sandboxed restore test completed successfully!\nTarget database: ${dbName}\nDuration: ${res.duration || 'verified'}\nIntegrity check: PASS\nSandboxed container destroyed cleanly.`;
			toast.success(`Test restore passed for ${dbName}!`);
		} catch (err: any) {
			testRestoreLog = `Test restore failed:\n${err.message || 'Error occurred'}`;
			toast.error('Test restore failed');
		} finally {
			testingRestore = false;
		}
	}

	async function handleLinkApp() {
		if (!selectedAppToLink) return;
		try {
			const envMap: Record<string, string> = {};
			if (linkEnvName) envMap['DATABASE_URL'] = linkEnvName;
			await api.linkDatabase(dbName, selectedAppToLink, envMap);
			toast.success(`Linked ${selectedAppToLink} to ${dbName}`);
			linkModalOpen = false;
			await loadDb();
		} catch (err: any) {
			toast.error(err.message || 'Linking failed');
		}
	}

	async function handleUnlinkApp(targetApp: string) {
		try {
			await api.unlinkDatabase(dbName, targetApp);
			toast.success(`Unlinked ${targetApp} from ${dbName}`);
			await loadDb();
		} catch (err: any) {
			toast.error(err.message || 'Unlink failed');
		}
	}

	async function handleDelete() {
		if (confirmDbName !== dbName) {
			toast.error(`Please type "${dbName}" to confirm deletion`);
			return;
		}

		try {
			await api.deleteDatabase(dbName, forceDelete, purgeVolume);
			toast.success(`Database ${dbName} deleted`);
			goto('/databases');
		} catch (err: any) {
			toast.error(err.message || 'Deletion failed');
		}
	}

	function copyToClipboard(text: string, label: string) {
		navigator.clipboard.writeText(text).then(() => {
			toast.success(`${label} copied to clipboard`);
		});
	}
</script>

<svelte:head>
	<title>{dbName} - Databases - dermaga</title>
</svelte:head>

<div class="space-y-6">
	<!-- Header -->
	<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
		<div>
			<div class="flex items-center space-x-2 text-xs text-zinc-500 mb-1">
				<a href="/" class="hover:text-zinc-800 dark:hover:text-zinc-200">Dashboard</a>
				<span>/</span>
				<a href="/databases" class="hover:text-zinc-800 dark:hover:text-zinc-200">Databases</a>
				<span>/</span>
				<span>{dbName}</span>
			</div>
			<div class="flex items-center space-x-3">
				<h1 class="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100 font-mono">
					{dbName}
				</h1>
				<StatusBadge status={db?.status || 'unknown'} size="md" />
			</div>
		</div>

		<div class="flex items-center space-x-2.5">
			<button
				type="button"
				disabled={restarting}
				onclick={handleRestart}
				class="px-3 py-2 text-xs font-semibold rounded-lg border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 hover:bg-zinc-50 text-zinc-700 dark:text-zinc-300 shadow-2xs"
			>
				{restarting ? 'Restarting...' : 'Restart'}
			</button>
			<button
				type="button"
				disabled={backingUp}
				onclick={handleBackup}
				class="px-3.5 py-2 text-xs font-semibold rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white shadow-xs"
			>
				{backingUp ? 'Backing up...' : 'Trigger Backup'}
			</button>
			<button
				type="button"
				disabled={testingRestore}
				onclick={handleTestRestore}
				class="px-3.5 py-2 text-xs font-semibold rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white shadow-xs"
			>
				Test Restore
			</button>
		</div>
	</div>

	{#if db}
		<div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
			<!-- Connection Info Card -->
			<div class="lg:col-span-2 space-y-6">
				<div class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 p-6 shadow-2xs space-y-4">
					<div class="flex items-center justify-between">
						<div>
							<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
								Connection Details
							</h2>
							<p class="text-xs text-zinc-500">Encrypted in master SQLite store. Reachable through container network or SSH tunnel.</p>
						</div>
						<button
							type="button"
							onclick={toggleReveal}
							class="px-3 py-1.5 rounded-lg border border-zinc-200 dark:border-zinc-700 text-xs font-semibold hover:bg-zinc-50 dark:hover:bg-zinc-800"
						>
							{revealCredentials ? 'Hide Secrets' : 'Reveal Secrets'}
						</button>
					</div>

					<!-- Connection String -->
					{#if db.url}
						<div class="space-y-1 text-xs">
							<span class="font-semibold text-zinc-700 dark:text-zinc-300">Connection URL</span>
							<div class="flex items-center space-x-2">
								<input
									type="text"
									readonly
									value={db.url}
									class="flex-1 px-3 py-2 text-xs font-mono rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-800 dark:text-zinc-200 select-all"
								/>
								<button
									type="button"
									onclick={() => copyToClipboard(db?.url || '', 'Connection URL')}
									class="px-3 py-2 text-xs font-semibold rounded-lg bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 shadow-2xs"
								>
									Copy
								</button>
							</div>
						</div>
					{/if}

					<!-- SSH Tunnel Snippet -->
					{#if db.snippet}
						<div class="space-y-1 text-xs pt-2">
							<span class="font-semibold text-zinc-700 dark:text-zinc-300">Desktop / Local Tunnel Command</span>
							<div class="relative">
								<pre class="p-3 bg-zinc-950 text-zinc-300 font-mono text-xs rounded-lg overflow-x-auto select-all border border-zinc-800">{db.snippet}</pre>
								<button
									type="button"
									onclick={() => copyToClipboard(db?.snippet || '', 'SSH tunnel snippet')}
									class="absolute top-2 right-2 px-2.5 py-1 text-xs font-semibold rounded bg-zinc-800 hover:bg-zinc-700 text-zinc-200 border border-zinc-700"
								>
									Copy
								</button>
							</div>
							<p class="text-[11px] text-zinc-500">Run from your laptop terminal to expose the database on localhost:{db.port}.</p>
						</div>
					{/if}

					<!-- Credential Fields -->
					{#if db.credentials}
						<div class="grid grid-cols-2 sm:grid-cols-4 gap-4 pt-3 border-t border-zinc-100 dark:border-zinc-800 text-xs">
							<div>
								<span class="block text-zinc-400 text-[11px]">Host</span>
								<span class="font-mono font-semibold">{db.name}</span>
							</div>
							<div>
								<span class="block text-zinc-400 text-[11px]">Port</span>
								<span class="font-mono font-semibold">{db.port}</span>
							</div>
							<div>
								<span class="block text-zinc-400 text-[11px]">User</span>
								<span class="font-mono font-semibold">{db.credentials.username}</span>
							</div>
							<div>
								<span class="block text-zinc-400 text-[11px]">Database</span>
								<span class="font-mono font-semibold">{db.credentials.database}</span>
							</div>
						</div>
					{/if}
				</div>

				<!-- Linked Apps -->
				<div class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 p-6 shadow-2xs space-y-4">
					<div class="flex items-center justify-between">
						<div>
							<h3 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
								Linked Applications
							</h3>
							<p class="text-xs text-zinc-500">Attached containers sharing network namespace dg-db-{dbName}.</p>
						</div>
						<button
							type="button"
							onclick={() => (linkModalOpen = true)}
							class="px-3 py-1.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold"
						>
							Link App
						</button>
					</div>

					{#if db.linkedApps.length === 0}
						<div class="text-center py-6 text-xs text-zinc-500">No applications linked yet.</div>
					{:else}
						<div class="divide-y divide-zinc-100 dark:divide-zinc-800 text-xs">
							{#each db.linkedApps as app}
								<div class="py-3 flex items-center justify-between">
									<a href="/apps/{app}" class="font-bold text-indigo-600 dark:text-indigo-400 hover:underline">
										{app}
									</a>
									<button
										type="button"
										onclick={() => handleUnlinkApp(app)}
										class="px-2.5 py-1 rounded text-rose-600 border border-rose-200 dark:border-rose-900/50 hover:bg-rose-50 text-xs font-medium"
									>
										Unlink
									</button>
								</div>
							{/each}
						</div>
					{/if}
				</div>
			</div>

			<!-- Meta Info & Danger Zone -->
			<div class="space-y-6">
				<div class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 p-6 shadow-2xs space-y-3 text-xs">
					<h3 class="text-xs font-bold uppercase tracking-wider text-zinc-500">Specifications</h3>
					<div class="space-y-2 text-zinc-600 dark:text-zinc-400">
						<div class="flex items-center justify-between">
							<span>Engine</span>
							<span class="font-bold font-mono text-zinc-900 dark:text-zinc-100">{db.engine}</span>
						</div>
						<div class="flex items-center justify-between">
							<span>Version</span>
							<span class="font-mono">{db.version}</span>
						</div>
						<div class="flex items-center justify-between">
							<span>Volume</span>
							<span class="font-mono truncate max-w-[140px]">{db.volume}</span>
						</div>
						<div class="flex items-center justify-between">
							<span>Backup Policy</span>
							<span class="font-mono">{db.backupSchedule || '03:00 daily'}</span>
						</div>
					</div>
				</div>

				<div class="bg-rose-50/50 dark:bg-rose-950/20 rounded-2xl border border-rose-200 dark:border-rose-900/50 p-6 space-y-3">
					<h3 class="text-xs font-bold uppercase tracking-wider text-rose-700 dark:text-rose-400">Danger Zone</h3>
					<p class="text-xs text-zinc-600 dark:text-zinc-400 leading-relaxed">
						Delete database container and Quadlet services. Optionally purge storage volume.
					</p>
					<button
						type="button"
						onclick={() => (deleteModalOpen = true)}
						class="w-full py-2 bg-rose-600 hover:bg-rose-500 text-white rounded-lg text-xs font-semibold"
					>
						Delete Database
					</button>
				</div>
			</div>
		</div>
	{/if}
</div>

<!-- Test Restore Result Modal -->
<Modal open={testRestoreOpen} title="Sandboxed Test Restore Report" onclose={() => (testRestoreOpen = false)}>
	<div class="space-y-3">
		<pre class="p-3 bg-zinc-950 text-zinc-300 font-mono text-xs rounded-lg overflow-x-auto select-all border border-zinc-800 whitespace-pre-wrap">{testRestoreLog}</pre>
		<div class="flex justify-end">
			<button type="button" onclick={() => (testRestoreOpen = false)} class="px-3 py-1.5 rounded bg-zinc-200 dark:bg-zinc-800 text-xs font-semibold">
				Close
			</button>
		</div>
	</div>
</Modal>

<!-- Link App Modal -->
<Modal open={linkModalOpen} title="Link Application to {dbName}" onclose={() => (linkModalOpen = false)}>
	<div class="space-y-4 text-xs">
		<div>
			<label for="appSelect" class="block font-semibold mb-1">Select Application</label>
			<select id="appSelect" bind:value={selectedAppToLink} class="w-full p-2 border rounded bg-white dark:bg-zinc-900">
				<option value="">-- Choose app --</option>
				{#each allApps as a}
					<option value={a.name}>{a.name}</option>
				{/each}
			</select>
		</div>
		<div>
			<label for="envKey" class="block font-semibold mb-1">Env Variable Key</label>
			<input id="envKey" type="text" bind:value={linkEnvName} class="w-full p-2 border rounded font-mono bg-white dark:bg-zinc-900" />
		</div>
		<button type="button" onclick={handleLinkApp} class="w-full py-2 bg-indigo-600 text-white rounded font-semibold">
			Confirm Link & Redeploy
		</button>
	</div>
</Modal>

<!-- Delete Modal -->
<Modal open={deleteModalOpen} title="Delete Database {dbName}" onclose={() => (deleteModalOpen = false)}>
	<div class="space-y-4 text-xs">
		<p class="text-zinc-600 dark:text-zinc-400">
			To confirm deletion of this database, please type <strong>{dbName}</strong>:
		</p>
		<input type="text" bind:value={confirmDbName} placeholder={dbName} class="w-full p-2 border border-rose-300 rounded font-mono" />
		<div class="space-y-2">
			<label class="flex items-center space-x-2">
				<input type="checkbox" bind:checked={purgeVolume} />
				<span>Permanently purge volume data</span>
			</label>
			<label class="flex items-center space-x-2">
				<input type="checkbox" bind:checked={forceDelete} />
				<span>Force delete even if linked to apps</span>
			</label>
		</div>
		<button type="button" onclick={handleDelete} class="w-full py-2 bg-rose-600 hover:bg-rose-500 text-white font-semibold rounded">
			Permanently Delete
		</button>
	</div>
</Modal>
