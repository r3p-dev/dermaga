<script lang="ts">
	import {
		getGitHubStatus,
		startGitHubManifest,
		syncGitHub,
		disconnectGitHub
	} from '#lib/api.js';
	import type { GitHubStatusResponse } from '#lib/types/api.js';
	import { onMount } from 'svelte';

	interface Props {
		onSwitchToHookDomain?: () => void;
	}

	let { onSwitchToHookDomain }: Props = $props();

	let status = $state<GitHubStatusResponse | null>(null);
	let loading = $state(true);
	let busy = $state(false);
	let syncing = $state(false);
	let disconnecting = $state(false);
	let error = $state<string | null>(null);
	let message = $state<string | null>(null);
	let confirmText = $state('');
	let deletedAppURL = $state<string | null>(null);

	async function loadStatus() {
		loading = true;
		error = null;
		try {
			status = await getGitHubStatus();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load GitHub settings';
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		loadStatus();
	});

	async function handleConnect() {
		busy = true;
		error = null;
		deletedAppURL = null;
		try {
			const res = await startGitHubManifest();
			const form = document.createElement('form');
			form.method = 'POST';
			form.action = res.action_url;
			form.style.display = 'none';

			const input = document.createElement('input');
			input.type = 'hidden';
			input.name = 'manifest';
			input.value = res.manifest_json;

			form.appendChild(input);
			document.body.appendChild(form);
			form.submit();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to start GitHub App creation';
			busy = false;
		}
	}

	async function handleSync() {
		syncing = true;
		error = null;
		message = null;
		try {
			const res = await syncGitHub();
			if (res.error) {
				error = res.error;
			} else {
				status = res;
				message = 'Installations and repositories refreshed from GitHub.';
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to refresh from GitHub';
		} finally {
			syncing = false;
		}
	}

	async function handleDisconnect(e: SubmitEvent) {
		e.preventDefault();
		disconnecting = true;
		error = null;
		message = null;
		try {
			const res = await disconnectGitHub({ confirm: confirmText.trim() });
			if (res.success) {
				confirmText = '';
				deletedAppURL = res.delete_app_url || 'https://github.com/settings/apps';
				await loadStatus();
			} else {
				error = res.error || 'Failed to disconnect GitHub';
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Network error';
		} finally {
			disconnecting = false;
		}
	}
</script>

{#if loading}
	<div class="text-sm text-stone-500 py-4">Loading GitHub connection...</div>
{:else if status}
	<div class="space-y-6">
		{#if error}
			<div role="alert" class="p-3 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs">
				{error}
			</div>
		{/if}
		{#if message}
			<div class="p-3 rounded-lg bg-emerald-50 dark:bg-emerald-950/40 border border-emerald-200 dark:border-emerald-800 text-emerald-800 dark:text-emerald-300 text-xs">
				{message}
			</div>
		{/if}
		{#if deletedAppURL}
			<div class="p-4 rounded-xl bg-amber-50 dark:bg-amber-950/40 border border-amber-300 dark:border-amber-800 text-amber-950 dark:text-amber-200 text-xs space-y-2">
				<p class="font-semibold">GitHub App disconnected from this server.</p>
				<p>
					Stored keys and secrets have been deleted from dermaga. Existing live apps keep serving their current release,
					and new deploys are paused until GitHub is connected again.
				</p>
				<p>
					To also delete the App on GitHub, open
					<a href={deletedAppURL} target="_blank" rel="noopener noreferrer" class="underline font-medium">
						GitHub App Advanced Settings
					</a>
					and choose <strong>Delete GitHub App</strong>.
				</p>
			</div>
		{/if}

		{#if !status.connected}
			<section class="max-w-2xl bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-5">
				<div>
					<h2 class="text-base font-semibold">Connect GitHub</h2>
					<p class="text-xs text-stone-600 dark:text-stone-400 mt-1">
						Create a private GitHub App owned by your account so dermaga can read selected repositories and receive push webhooks.
					</p>
				</div>

				{#if !status.hook_domain}
					<div class="p-4 rounded-lg bg-amber-50 dark:bg-amber-950/40 border border-amber-300 dark:border-amber-800 text-amber-950 dark:text-amber-200 text-xs space-y-2.5">
						<p class="font-medium">Hook domain required first</p>
						<p>
							Set a reserved hook domain before connecting GitHub so the App can be created with its webhook URL from the start.
						</p>
						{#if onSwitchToHookDomain}
							<button
								type="button"
								onclick={onSwitchToHookDomain}
								class="px-3 py-1.5 rounded-md bg-amber-900 text-white dark:bg-amber-200 dark:text-stone-900 text-xs font-medium"
							>
								Configure Hook Domain
							</button>
						{:else}
							<a href="/settings?tab=hook-domain" class="inline-block underline font-medium">Go to Settings &rarr; Hook Domain</a>
						{/if}
					</div>
				{:else}
					<div class="rounded-lg bg-stone-50 dark:bg-stone-950 border border-stone-200 dark:border-stone-800 p-4 space-y-3 text-xs">
						<p class="font-medium text-stone-800 dark:text-stone-200">What happens next on GitHub:</p>
						<ol class="list-decimal list-inside space-y-1.5 text-stone-600 dark:text-stone-400">
							<li>
								<strong>Create private App:</strong> Confirm creating a private GitHub App with read-only
								<code class="font-mono">Contents</code> and <code class="font-mono">Metadata</code> permissions and
								<code class="font-mono">push</code> webhooks to
								<code class="font-mono">https://{status.hook_domain}/hooks/github-app</code>.
							</li>
							<li>
								<strong>Install on your account or organization:</strong> Choose which repositories dermaga may access.
							</li>
							<li>
								<strong>Return to dermaga:</strong> You are redirected back to <code class="font-mono">http://localhost:7070</code>
								with your repositories ready to deploy.
							</li>
						</ol>
					</div>

					<button
						type="button"
						onclick={handleConnect}
						disabled={busy}
						class="py-2 px-4 rounded-md bg-brand-primary hover:bg-brand-hover text-brand-btn-text text-sm font-medium transition disabled:opacity-50"
					>
						{busy ? 'Redirecting to GitHub...' : 'Connect GitHub'}
					</button>
				{/if}
			</section>
		{:else if status.app}
			<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
				<section class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-4">
					<div class="flex items-start justify-between gap-3">
						<div>
							<h2 class="text-base font-semibold">{status.app.name || status.app.slug}</h2>
							<p class="text-xs text-stone-600 dark:text-stone-400 mt-0.5">
								App ID <span class="font-mono">{status.app.id}</span> &bull; Slug <span class="font-mono">{status.app.slug}</span>
							</p>
						</div>
						<span class="px-2 py-0.5 rounded-full text-xs font-medium bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300">
							Connected
						</span>
					</div>

					<dl class="space-y-2 text-xs border-t border-stone-200 dark:border-stone-800 pt-3">
						<div class="flex flex-wrap justify-between gap-2">
							<dt class="text-stone-600 dark:text-stone-400">Webhook endpoint</dt>
							<dd class="font-mono text-stone-900 dark:text-stone-100 break-all">https://{status.hook_domain}/hooks/github-app</dd>
						</div>
						<div class="flex flex-wrap justify-between gap-2">
							<dt class="text-stone-600 dark:text-stone-400">Last delivery</dt>
							<dd class="text-stone-900 dark:text-stone-100">
								{#if status.last_delivery}
									<span class="font-mono">{status.last_delivery.event}</span> ({status.last_delivery.status}) &bull; {status.last_delivery.received_at}
								{:else}
									No webhook deliveries received yet
								{/if}
							</dd>
						</div>
					</dl>

					<div class="flex flex-wrap gap-2 pt-1">
						<a
							href={status.app.install_url}
							target="_blank"
							rel="noopener noreferrer"
							class="px-3 py-1.5 rounded-md bg-brand-primary hover:bg-brand-hover text-brand-btn-text text-xs font-medium transition"
						>
							Manage Repositories on GitHub
						</a>
						<button
							type="button"
							onclick={handleSync}
							disabled={syncing}
							class="px-3 py-1.5 rounded-md bg-stone-100 dark:bg-stone-800 hover:bg-stone-200 dark:hover:bg-stone-700 text-xs font-medium transition disabled:opacity-50"
						>
							{syncing ? 'Refreshing...' : 'Refresh'}
						</button>
						{#if status.app.html_url}
							<a
								href={status.app.html_url}
								target="_blank"
								rel="noopener noreferrer"
								class="px-3 py-1.5 rounded-md border border-stone-300 dark:border-stone-700 text-xs font-medium hover:bg-stone-50 dark:hover:bg-stone-800 transition"
							>
								View App on GitHub
							</a>
						{/if}
					</div>
				</section>

				<section class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-4">
					<h2 class="text-base font-semibold">Installations</h2>
					{#if status.installations.length === 0}
						<div class="text-xs text-stone-600 dark:text-stone-400 space-y-2">
							<p>This GitHub App is not installed on any account or organization yet.</p>
							<a
								href={status.app.install_url}
								target="_blank"
								rel="noopener noreferrer"
								class="inline-block underline font-medium text-brand-text dark:text-brand-primary"
							>
								Install on GitHub
							</a>
						</div>
					{:else}
						<div class="divide-y divide-stone-200 dark:divide-stone-800">
							{#each status.installations as inst}
								<div class="py-2.5 flex items-center justify-between gap-3 text-xs">
									<div>
										<div class="font-medium text-stone-900 dark:text-stone-100">{inst.account_login}</div>
										<div class="text-stone-600 dark:text-stone-400">
											{inst.account_type} &bull; {inst.repository_selection === 'all' ? 'All repositories' : 'Selected repositories'}
										</div>
									</div>
									<div class="flex items-center gap-2">
										{#if inst.suspended_at}
											<span class="px-2 py-0.5 rounded-full bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-300">
												Suspended
											</span>
										{/if}
										{#if inst.html_url}
											<a href={inst.html_url} target="_blank" rel="noopener noreferrer" class="underline text-stone-700 dark:text-stone-300">
												Configure
											</a>
										{/if}
									</div>
								</div>
							{/each}
						</div>
					{/if}
				</section>
			</div>

			<section class="bg-white dark:bg-stone-900 border border-stone-200 dark:border-stone-800 rounded-xl p-6 shadow-sm space-y-4">
				<div class="flex items-center justify-between gap-3">
					<h2 class="text-base font-semibold">Accessible Repositories ({status.repositories.length})</h2>
				</div>

				{#if status.repositories.length === 0}
					<div class="text-xs text-stone-600 dark:text-stone-400 space-y-2">
						<p>No repositories are visible to the GitHub App yet.</p>
						<p>
							Grant access on GitHub using
							<a href={status.app.install_url} target="_blank" rel="noopener noreferrer" class="underline font-medium">
								Manage Repositories on GitHub
							</a>, then click <strong>Refresh</strong>.
						</p>
					</div>
				{:else}
					<div class="hidden md:block overflow-x-auto">
						<table class="w-full text-left text-xs">
							<thead>
								<tr class="border-b border-stone-200 dark:border-stone-800 text-stone-600 dark:text-stone-400">
									<th class="py-2 pr-4 font-medium">Repository</th>
									<th class="py-2 pr-4 font-medium">Visibility</th>
									<th class="py-2 pr-4 font-medium">Default Branch</th>
									<th class="py-2 font-medium text-right">GitHub</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-stone-200 dark:divide-stone-800">
								{#each status.repositories as repo}
									<tr>
										<td class="py-2.5 pr-4 font-mono text-stone-900 dark:text-stone-100">{repo.full_name}</td>
										<td class="py-2.5 pr-4 text-stone-600 dark:text-stone-400">{repo.private ? 'Private' : 'Public'}</td>
										<td class="py-2.5 pr-4 font-mono text-stone-600 dark:text-stone-400">{repo.default_branch}</td>
										<td class="py-2.5 text-right">
											{#if repo.html_url}
												<a href={repo.html_url} target="_blank" rel="noopener noreferrer" class="underline text-stone-700 dark:text-stone-300">Open</a>
											{/if}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>

					<div class="md:hidden divide-y divide-stone-200 dark:divide-stone-800">
						{#each status.repositories as repo}
							<div class="py-3 space-y-1 text-xs">
								<div class="flex items-center justify-between gap-2">
									<span class="font-mono font-medium text-stone-900 dark:text-stone-100 break-all">{repo.full_name}</span>
									<span class="px-2 py-0.5 rounded bg-stone-100 dark:bg-stone-800 text-stone-700 dark:text-stone-300 shrink-0">
										{repo.private ? 'Private' : 'Public'}
									</span>
								</div>
								<div class="flex items-center justify-between text-stone-600 dark:text-stone-400">
									<span>Branch: <code class="font-mono">{repo.default_branch}</code></span>
									{#if repo.html_url}
										<a href={repo.html_url} target="_blank" rel="noopener noreferrer" class="underline">Open</a>
									{/if}
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</section>

			<section class="max-w-2xl bg-white dark:bg-stone-900 border border-red-200 dark:border-red-900 rounded-xl p-6 shadow-sm space-y-3">
				<h2 class="text-base font-semibold text-red-700 dark:text-red-400">Disconnect GitHub</h2>
				<p class="text-xs text-stone-600 dark:text-stone-400">
					Removes the stored GitHub App private key, webhook secret, and repository cache from this server.
					Running apps keep serving their live release; new deploys are refused until GitHub is connected again.
				</p>
				<form onsubmit={handleDisconnect} class="space-y-3">
					<div>
						<label for="confirm-disconnect" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1">
							Type <code class="font-mono">disconnect</code> to confirm
						</label>
						<input
							id="confirm-disconnect"
							type="text"
							bind:value={confirmText}
							placeholder="disconnect"
							class="w-full max-w-xs px-3 py-2 text-sm rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950"
						/>
					</div>
					<button
						type="submit"
						disabled={disconnecting || confirmText.trim() !== 'disconnect'}
						class="py-2 px-4 rounded-md bg-red-600 hover:bg-red-700 text-white text-sm font-medium transition disabled:opacity-50"
					>
						{disconnecting ? 'Disconnecting...' : 'Disconnect GitHub'}
					</button>
				</form>
			</section>
		{/if}
	</div>
{/if}
