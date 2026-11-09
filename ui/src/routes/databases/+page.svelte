<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';
	import type { DatabaseView } from '$lib/types/api';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Modal from '$lib/components/Modal.svelte';

	let databases = $state<DatabaseView[]>([]);
	let loading = $state(true);
	let createModalOpen = $state(false);

	// Create form fields
	let newDbName = $state('');
	let newDbEngine = $state<'postgres' | 'mariadb' | 'valkey'>('postgres');
	let newDbVersion = $state('');
	let creating = $state(false);

	onMount(async () => {
		await loadDatabases();
	});

	async function loadDatabases() {
		loading = true;
		try {
			databases = await api.listDatabases();
		} catch (err: any) {
			toast.error(err.message || 'Failed to load databases');
		} finally {
			loading = false;
		}
	}

	async function handleCreate(e: Event) {
		e.preventDefault();
		if (!newDbName.trim()) {
			toast.error('Database name is required');
			return;
		}

		creating = true;
		try {
			await api.createDatabase({
				name: newDbName.trim(),
				engine: newDbEngine,
				version: newDbVersion.trim() || undefined
			});
			toast.success(`Database ${newDbName} provisioned successfully!`);
			createModalOpen = false;
			newDbName = '';
			newDbVersion = '';
			await loadDatabases();
		} catch (err: any) {
			toast.error(err.message || 'Failed to provision database');
		} finally {
			creating = false;
		}
	}

	async function handleRestart(name: string) {
		try {
			await api.restartDatabase(name);
			toast.success(`Database ${name} restarted`);
			await loadDatabases();
		} catch (err: any) {
			toast.error(err.message || 'Failed to restart database');
		}
	}

	async function handleBackup(name: string) {
		try {
			const snap = await api.backupDatabase(name);
			toast.success(`Snapshot ${snap.short_id} created for ${name}`);
		} catch (err: any) {
			toast.error(err.message || 'Failed to backup database');
		}
	}
</script>

<svelte:head>
	<title>Databases - dermaga</title>
</svelte:head>

<div class="space-y-6">
	<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
		<div>
			<div class="flex items-center space-x-2 text-xs text-zinc-500 mb-1">
				<a href="/" class="hover:text-zinc-800 dark:hover:text-zinc-200">Dashboard</a>
				<span>/</span>
				<span>Databases</span>
			</div>
			<h1 class="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">
				Managed Databases
			</h1>
			<p class="text-xs text-zinc-500 dark:text-zinc-400 mt-0.5">
				Rootless Podman Quadlet container engines isolated on private backend networks.
			</p>
		</div>

		<div class="flex items-center space-x-2.5">
			<button
				type="button"
				onclick={loadDatabases}
				aria-label="Refresh databases"
				class="p-2 text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-lg shadow-2xs"
			>
				<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
				</svg>
			</button>
			<button
				type="button"
				onclick={() => (createModalOpen = true)}
				class="inline-flex items-center space-x-1.5 px-3.5 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold shadow-xs"
			>
				<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
				</svg>
				<span>New Database</span>
			</button>
		</div>
	</div>

	<!-- Databases Grid -->
	{#if databases.length === 0}
		<div class="text-center py-12 rounded-2xl border border-dashed border-zinc-300 dark:border-zinc-800 bg-white dark:bg-zinc-900/30">
			<svg class="mx-auto h-10 w-10 text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4" />
			</svg>
			<h3 class="mt-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100">No databases provisioned</h3>
			<p class="mt-1 text-xs text-zinc-500">Launch a dedicated PostgreSQL, MariaDB, or Valkey instance.</p>
			<div class="mt-4">
				<button
					type="button"
					onclick={() => (createModalOpen = true)}
					class="px-3 py-1.5 text-xs font-semibold rounded-lg text-white bg-indigo-600 hover:bg-indigo-500"
				>
					Provision Database
				</button>
			</div>
		</div>
	{:else}
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
			{#each databases as db}
				<div class="rounded-2xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5 shadow-2xs flex flex-col justify-between space-y-4">
					<div>
						<div class="flex items-start justify-between">
							<div class="flex items-center space-x-3">
								<div class="w-10 h-10 rounded-xl bg-zinc-100 dark:bg-zinc-800 flex items-center justify-center font-bold text-sm uppercase text-zinc-800 dark:text-zinc-200">
									{db.engine.slice(0, 2)}
								</div>
								<div>
									<a
										href="/databases/{db.name}"
										class="font-bold text-base text-zinc-900 dark:text-zinc-100 hover:text-indigo-600 transition-colors"
									>
										{db.name}
									</a>
									<p class="text-xs text-zinc-500 font-mono">
										{db.engine} {db.version}
									</p>
								</div>
							</div>
							<StatusBadge status={db.status} />
						</div>

						<div class="mt-4 pt-3 border-t border-zinc-100 dark:border-zinc-800 space-y-2 text-xs text-zinc-500">
							<div class="flex items-center justify-between">
								<span>Port</span>
								<span class="font-mono text-zinc-800 dark:text-zinc-200">:{db.port}</span>
							</div>
							<div class="flex items-center justify-between">
								<span>Volume</span>
								<span class="font-mono text-zinc-800 dark:text-zinc-200 truncate max-w-[150px]">{db.volume}</span>
							</div>
							<div class="flex items-center justify-between">
								<span>Linked Apps</span>
								<span class="font-semibold text-zinc-800 dark:text-zinc-200">
									{db.linkedApps?.length || 0} apps
								</span>
							</div>
						</div>
					</div>

					<div class="pt-3 border-t border-zinc-100 dark:border-zinc-800 flex items-center justify-between gap-2">
						<a
							href="/databases/{db.name}"
							class="px-2.5 py-1.5 rounded-lg border border-zinc-200 dark:border-zinc-700 text-xs font-semibold text-zinc-700 dark:text-zinc-300 hover:bg-zinc-50 dark:hover:bg-zinc-800"
						>
							Connection & Info
						</a>
						<div class="flex items-center space-x-1.5">
							<button
								type="button"
								onclick={() => handleRestart(db.name)}
								class="p-1.5 rounded-lg text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200 hover:bg-zinc-100 dark:hover:bg-zinc-800"
								title="Restart database"
							>
								<svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
								</svg>
							</button>
							<button
								type="button"
								onclick={() => handleBackup(db.name)}
								class="px-2.5 py-1.5 rounded-lg bg-indigo-50 dark:bg-indigo-950/60 text-indigo-600 dark:text-indigo-400 hover:bg-indigo-100 text-xs font-semibold"
							>
								Backup
							</button>
						</div>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<!-- Create Database Modal -->
<Modal open={createModalOpen} title="Provision Managed Database" onclose={() => (createModalOpen = false)}>
	<form onsubmit={handleCreate} class="space-y-4 text-xs">
		<div>
			<label for="newDbName" class="block font-semibold mb-1 text-zinc-700 dark:text-zinc-300">
				Database Name <span class="text-rose-500">*</span>
			</label>
			<input
				id="newDbName"
				type="text"
				bind:value={newDbName}
				required
				placeholder="e.g. main-db"
				class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 font-mono"
			/>
		</div>

		<div>
			<label for="newDbEngine" class="block font-semibold mb-1 text-zinc-700 dark:text-zinc-300">
				Database Engine <span class="text-rose-500">*</span>
			</label>
			<select
				id="newDbEngine"
				bind:value={newDbEngine}
				class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-950"
			>
				<option value="postgres">PostgreSQL 18 (default: postgres:18)</option>
				<option value="mariadb">MariaDB 11.8 (default: mariadb:11.8)</option>
				<option value="valkey">Valkey 9 (default: valkey/valkey:9)</option>
			</select>
		</div>

		<div>
			<label for="newDbVersion" class="block font-semibold mb-1 text-zinc-700 dark:text-zinc-300">
				Version Tag <span class="text-zinc-400 font-normal">(optional override)</span>
			</label>
			<input
				id="newDbVersion"
				type="text"
				bind:value={newDbVersion}
				placeholder="leave blank for pinned default"
				class="w-full px-3 py-2 text-sm rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 font-mono"
			/>
		</div>

		<div class="pt-2 flex justify-end space-x-2">
			<button
				type="button"
				onclick={() => (createModalOpen = false)}
				class="px-4 py-2 rounded-lg border text-xs font-semibold"
			>
				Cancel
			</button>
			<button
				type="submit"
				disabled={creating}
				class="px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold disabled:opacity-50"
			>
				{creating ? 'Provisioning...' : 'Provision Database'}
			</button>
		</div>
	</form>
</Modal>
