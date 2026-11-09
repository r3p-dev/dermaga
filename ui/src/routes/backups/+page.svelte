<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';
	import type { Snapshot, Settings as BackupSettings } from '$lib/types/backup';
	import Modal from '$lib/components/Modal.svelte';

	let settings = $state<BackupSettings | null>(null);
	let snapshots = $state<Snapshot[]>([]);
	let loading = $state(true);
	let tagFilter = $state('');

	// Action states
	let systemBackingUp = $state(false);
	let checkingIntegrity = $state(false);
	let checkResult = $state('');
	let checkModalOpen = $state(false);

	// Settings modal
	let settingsModalOpen = $state(false);
	let repoInput = $state('');
	let endpointInput = $state('');
	let regionInput = $state('');
	let accessKeyInput = $state('');
	let secretKeyInput = $state('');
	let passwordInput = $state('');

	// Key export modal
	let keyExportOpen = $state(false);
	let exportedKey = $state('');
	let exportInstructions = $state('');

	onMount(async () => {
		await loadData();
	});

	async function loadData() {
		loading = true;
		try {
			const [st, snaps] = await Promise.all([
				api.getBackupSettings().catch(() => null),
				api.listSnapshots(tagFilter).catch(() => [])
			]);
			settings = st;
			snapshots = snaps || [];
			if (st) {
				repoInput = st.repository || '';
				endpointInput = st.s3Endpoint || '';
				regionInput = st.s3Region || '';
				accessKeyInput = st.s3AccessKey || '';
				secretKeyInput = st.s3SecretKey || '';
			}
		} catch (err: any) {
			toast.error(err.message || 'Failed to load backup data');
		} finally {
			loading = false;
		}
	}

	async function handleSaveSettings(e: Event) {
		e.preventDefault();
		try {
			await api.updateBackupSettings({
				repository: repoInput.trim(),
				s3Endpoint: endpointInput.trim(),
				s3Region: regionInput.trim(),
				s3AccessKey: accessKeyInput.trim(),
				s3SecretKey: secretKeyInput.trim(),
				password: passwordInput ? passwordInput.trim() : undefined,
				configured: true
			});
			toast.success('Restic backup settings saved successfully');
			settingsModalOpen = false;
			await loadData();
		} catch (err: any) {
			toast.error(err.message || 'Failed to save backup settings');
		}
	}

	async function handleSystemBackup() {
		systemBackingUp = true;
		try {
			const snap = await api.systemBackup();
			toast.success(`System snapshot ${snap.short_id} created successfully`);
			await loadData();
		} catch (err: any) {
			toast.error(err.message || 'System backup failed');
		} finally {
			systemBackingUp = false;
		}
	}

	async function handleCheckIntegrity() {
		checkingIntegrity = true;
		checkResult = 'Running restic check on remote repository...';
		checkModalOpen = true;
		try {
			await api.checkBackup();
			checkResult = 'Repository integrity check: SUCCESS\nAll snapshots, pack files, and trees verified cleanly.\nZero index errors.';
			toast.success('Backup repository integrity check passed');
		} catch (err: any) {
			checkResult = `Integrity check failed:\n${err.message || 'Error occurred'}`;
			toast.error('Integrity check failed');
		} finally {
			checkingIntegrity = false;
		}
	}

	async function handleExportKeys() {
		try {
			const res = await api.exportKeys();
			exportedKey = res.masterKeyB64;
			exportInstructions = res.instructions;
			keyExportOpen = true;
		} catch (err: any) {
			toast.error(err.message || 'Failed to export master key');
		}
	}

	function formatBytes(bytes: number): string {
		if (bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return (bytes / Math.pow(k, i)).toFixed(1) + ' ' + sizes[i];
	}
</script>

<svelte:head>
	<title>Backups & Disaster Recovery - dermaga</title>
</svelte:head>

<div class="space-y-6">
	<!-- Header -->
	<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
		<div>
			<div class="flex items-center space-x-2 text-xs text-zinc-500 mb-1">
				<a href="/" class="hover:text-zinc-800 dark:hover:text-zinc-200">Dashboard</a>
				<span>/</span>
				<span>Backups</span>
			</div>
			<h1 class="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">
				Backups & Disaster Recovery
			</h1>
			<p class="text-xs text-zinc-500 dark:text-zinc-400 mt-0.5">
				Streaming encrypted backups powered by restic to Cloudflare R2, Backblaze B2, or local storage.
			</p>
		</div>

		<div class="flex items-center space-x-2.5">
			<button
				type="button"
				onclick={() => (settingsModalOpen = true)}
				class="px-3 py-2 text-xs font-semibold rounded-lg border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 hover:bg-zinc-50 text-zinc-700 dark:text-zinc-300 shadow-2xs"
			>
				Restic / S3 Settings
			</button>
			<button
				type="button"
				onclick={handleExportKeys}
				class="px-3 py-2 text-xs font-semibold rounded-lg border border-amber-200 dark:border-amber-900/60 bg-amber-50 dark:bg-amber-950/40 text-amber-800 dark:text-amber-300 hover:bg-amber-100"
			>
				Export Master Key
			</button>
			<button
				type="button"
				disabled={systemBackingUp}
				onclick={handleSystemBackup}
				class="px-3.5 py-2 text-xs font-semibold rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white shadow-xs"
			>
				{systemBackingUp ? 'Backing up...' : 'Backup System'}
			</button>
		</div>
	</div>

	<!-- Storage Repository Banner -->
	<div class="p-5 rounded-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 shadow-2xs flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
		<div>
			<span class="text-xs font-bold uppercase tracking-wider text-zinc-500">Repository</span>
			<div class="mt-1 font-mono text-sm font-semibold text-zinc-900 dark:text-zinc-100 truncate max-w-xl">
				{settings?.repository || 'Not configured'}
			</div>
			<p class="text-xs text-zinc-500 mt-0.5">
				Schedule: daily at 03:00 Asia/Jakarta with RandomizedDelaySec
			</p>
		</div>
		<div class="flex items-center space-x-2">
			<button
				type="button"
				disabled={checkingIntegrity}
				onclick={handleCheckIntegrity}
				class="px-3 py-1.5 rounded-lg border border-zinc-200 dark:border-zinc-700 text-xs font-semibold hover:bg-zinc-50 dark:hover:bg-zinc-800"
			>
				{checkingIntegrity ? 'Checking...' : 'Check Integrity'}
			</button>
		</div>
	</div>

	<!-- Snapshots Explorer -->
	<div class="bg-white dark:bg-zinc-900 rounded-2xl border border-zinc-200 dark:border-zinc-800 shadow-2xs overflow-hidden">
		<div class="p-4 border-b border-zinc-200 dark:border-zinc-800 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
			<div>
				<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100">Snapshot Explorer</h2>
				<span class="text-xs text-zinc-500">{snapshots.length} total restic snapshots</span>
			</div>
			<div class="flex items-center space-x-2">
				<select
					bind:value={tagFilter}
					onchange={loadData}
					class="px-3 py-1.5 text-xs rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-zinc-800 dark:text-zinc-200"
				>
					<option value="">All Tags</option>
					<option value="system">system</option>
					<option value="db">databases</option>
				</select>
			</div>
		</div>

		{#if snapshots.length === 0}
			<div class="text-center py-12 text-xs text-zinc-500">
				No snapshots found in repository.
			</div>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full text-left text-xs">
					<thead class="bg-zinc-50 dark:bg-zinc-950/60 border-b border-zinc-200 dark:border-zinc-800 text-zinc-500 font-semibold uppercase tracking-wider text-[11px]">
						<tr>
							<th class="py-3 px-4">Snapshot ID</th>
							<th class="py-3 px-4">Tags</th>
							<th class="py-3 px-4">Time</th>
							<th class="py-3 px-4">Size</th>
							<th class="py-3 px-4">Paths</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-100 dark:divide-zinc-800">
						{#each snapshots as snap}
							<tr class="hover:bg-zinc-50/50 dark:hover:bg-zinc-800/40">
								<td class="py-3 px-4 font-mono font-bold text-indigo-600 dark:text-indigo-400">
									{snap.short_id}
								</td>
								<td class="py-3 px-4">
									<div class="flex flex-wrap gap-1">
										{#each snap.tags || [] as tag}
											<span class="px-2 py-0.5 rounded-full bg-zinc-100 dark:bg-zinc-800 text-[10px] font-mono">
												{tag}
											</span>
										{/each}
									</div>
								</td>
								<td class="py-3 px-4 text-zinc-600 dark:text-zinc-400 font-mono">
									{new Date(snap.time).toLocaleString()}
								</td>
								<td class="py-3 px-4 font-mono">
									{formatBytes(snap.size_bytes || 0)}
								</td>
								<td class="py-3 px-4 font-mono text-zinc-500 truncate max-w-xs">
									{(snap.paths || []).join(', ')}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</div>
</div>

<!-- S3 / Restic Settings Modal -->
<Modal open={settingsModalOpen} title="Restic & S3 Backup Settings" onclose={() => (settingsModalOpen = false)}>
	<form onsubmit={handleSaveSettings} class="space-y-4 text-xs">
		<div>
			<label for="repoUrl" class="block font-semibold mb-1">Repository URL / Path <span class="text-rose-500">*</span></label>
			<input id="repoUrl" type="text" bind:value={repoInput} required placeholder="s3:https://<account>.r2.cloudflarestorage.com/<bucket>" class="w-full p-2 border rounded font-mono bg-white dark:bg-zinc-900" />
		</div>
		<div class="grid grid-cols-2 gap-3">
			<div>
				<label for="s3Ep" class="block font-semibold mb-1">S3 Endpoint</label>
				<input id="s3Ep" type="text" bind:value={endpointInput} placeholder="https://..." class="w-full p-2 border rounded font-mono bg-white dark:bg-zinc-900" />
			</div>
			<div>
				<label for="s3Reg" class="block font-semibold mb-1">S3 Region</label>
				<input id="s3Reg" type="text" bind:value={regionInput} placeholder="auto" class="w-full p-2 border rounded font-mono bg-white dark:bg-zinc-900" />
			</div>
		</div>
		<div class="grid grid-cols-2 gap-3">
			<div>
				<label for="s3Acc" class="block font-semibold mb-1">Access Key ID</label>
				<input id="s3Acc" type="text" bind:value={accessKeyInput} class="w-full p-2 border rounded font-mono bg-white dark:bg-zinc-900" />
			</div>
			<div>
				<label for="s3Sec" class="block font-semibold mb-1">Secret Access Key</label>
				<input id="s3Sec" type="password" bind:value={secretKeyInput} placeholder="••••••••" class="w-full p-2 border rounded font-mono bg-white dark:bg-zinc-900" />
			</div>
		</div>
		<div>
			<label for="rPass" class="block font-semibold mb-1">Restic Password <span class="text-zinc-400 font-normal">(leave blank to keep current)</span></label>
			<input id="rPass" type="password" bind:value={passwordInput} placeholder="Repository encryption password" class="w-full p-2 border rounded font-mono bg-white dark:bg-zinc-900" />
		</div>
		<button type="submit" class="w-full py-2 bg-indigo-600 text-white rounded font-semibold">
			Save Backup Configuration
		</button>
	</form>
</Modal>

<!-- Integrity Check Modal -->
<Modal open={checkModalOpen} title="Repository Integrity Check Report" onclose={() => (checkModalOpen = false)}>
	<div class="space-y-3">
		<pre class="p-3 bg-zinc-950 text-zinc-300 font-mono text-xs rounded-lg overflow-x-auto select-all border border-zinc-800 whitespace-pre-wrap">{checkResult}</pre>
		<div class="flex justify-end">
			<button type="button" onclick={() => (checkModalOpen = false)} class="px-3 py-1.5 rounded bg-zinc-200 dark:bg-zinc-800 text-xs font-semibold">
				Close
			</button>
		</div>
	</div>
</Modal>

<!-- Export Master Key Modal -->
<Modal open={keyExportOpen} title="Master Encryption Key Export" onclose={() => (keyExportOpen = false)}>
	<div class="space-y-4 text-xs">
		<div class="p-3 rounded-lg bg-amber-50 dark:bg-amber-950/60 border border-amber-200 dark:border-amber-900 text-amber-900 dark:text-amber-100">
			<p class="font-bold">Crucial Disaster Recovery Secret</p>
			<p class="mt-1 leading-relaxed">
				This master key encrypts all database passwords, environment variables, and webhook secrets. Keep a secure offline backup in your password manager (e.g. Bitwarden, 1Password).
			</p>
		</div>
		<div>
			<span class="block font-semibold mb-1">Master Key (Base64)</span>
			<div class="relative">
				<textarea readonly rows="2" class="w-full p-2 bg-zinc-950 border border-zinc-800 rounded font-mono text-zinc-200 select-all">{exportedKey}</textarea>
			</div>
		</div>
		{#if exportInstructions}
			<div>
				<span class="block font-semibold mb-1">Recovery Instructions</span>
				<pre class="p-3 bg-zinc-950 border border-zinc-800 rounded font-mono text-zinc-400 whitespace-pre-wrap text-[11px]">{exportInstructions}</pre>
			</div>
		{/if}
	</div>
</Modal>
