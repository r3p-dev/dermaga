<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';
	import type { Status, Settings as AppSettings, NotificationSettingsView } from '$lib/types/api';

	let status = $state<Status | null>(null);
	let settings = $state<AppSettings | null>(null);
	let loading = $state(true);

	let hookHost = $state('');
	let savingHook = $state(false);

	let syncingHosts = $state(false);
	let knownHostsResult = $state<{ file: string; fingerprints: string[] } | null>(null);

	// Notification Settings (M9)
	let notifyProvider = $state('');
	let notifyTarget = $state('');
	let notifyToken = $state('');
	let notifyEvents = $state('deploy.failed,backup.failed,disk.warning');
	let notifyConfigured = $state(false);
	let savingNotify = $state(false);
	let testingNotify = $state(false);
	let testFeedback = $state<{ success: boolean; detail: string } | null>(null);

	onMount(async () => {
		await loadData();
	});

	async function loadData() {
		loading = true;
		try {
			const [st, sett, notif] = await Promise.all([
				api.getStatus(),
				api.getSettings(),
				api.getNotificationSettings().catch(() => null)
			]);
			status = st;
			settings = sett;
			hookHost = sett.hookHost || '';
			if (notif) {
				notifyProvider = notif.provider || '';
				notifyTarget = notif.target || '';
				notifyConfigured = notif.configured;
				notifyEvents = notif.events || 'deploy.failed,backup.failed,disk.warning';
			}
		} catch (err: any) {
			toast.error(err.message || 'Failed to load settings');
		} finally {
			loading = false;
		}
	}

	async function saveHookHost() {
		savingHook = true;
		try {
			await api.updateSettings({ hookHost: hookHost.trim() });
			toast.success('Hook hostname updated and Traefik dynamic configuration re-rendered');
			await loadData();
		} catch (err: any) {
			toast.error(err.message || 'Failed to update hook host');
		} finally {
			savingHook = false;
		}
	}

	async function saveNotificationSettings() {
		savingNotify = true;
		testFeedback = null;
		try {
			await api.updateNotificationSettings({
				provider: notifyProvider,
				target: notifyTarget.trim(),
				token: notifyToken.trim(),
				events: notifyEvents.trim()
			});
			notifyToken = '';
			toast.success('Notification settings saved securely');
			await loadData();
		} catch (err: any) {
			toast.error(err.message || 'Failed to save notification settings');
		} finally {
			savingNotify = false;
		}
	}

	async function sendTestNotification() {
		testingNotify = true;
		testFeedback = null;
		try {
			const res = await api.testNotification({
				provider: notifyProvider,
				target: notifyTarget.trim(),
				token: notifyToken.trim()
			});
			testFeedback = res;
			if (res.success) {
				toast.success(res.detail || 'Test notification sent!');
			} else {
				toast.error(res.detail || 'Test notification failed');
			}
		} catch (err: any) {
			testFeedback = { success: false, detail: err.message || 'Failed to send test alert' };
			toast.error(err.message || 'Failed to send test alert');
		} finally {
			testingNotify = false;
		}
	}

	async function syncGitHubKnownHosts() {
		syncingHosts = true;
		try {
			const res = await api.syncKnownHosts();
			knownHostsResult = res;
			toast.success('GitHub host keys verified and saved to known_hosts');
		} catch (err: any) {
			toast.error(err.message || 'Failed to sync GitHub known hosts');
		} finally {
			syncingHosts = false;
		}
	}

	function copyToClipboard(text: string, label: string) {
		navigator.clipboard.writeText(text).then(() => {
			toast.success(`${label} copied to clipboard`);
		});
	}
</script>

<svelte:head>
	<title>Settings - dermaga</title>
</svelte:head>

<div class="max-w-4xl space-y-6">
	<div>
		<div class="flex items-center space-x-2 text-xs text-zinc-500 mb-1">
			<a href="/" class="hover:text-zinc-800 dark:hover:text-zinc-200">Dashboard</a>
			<span>/</span>
			<span>Settings</span>
		</div>
		<h1 class="text-2xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">
			Platform Settings
		</h1>
		<p class="text-xs text-zinc-500 dark:text-zinc-400 mt-0.5">
			Manage system security, notifications, hook relay domains, and remote SSH tunnel routing.
		</p>
	</div>

	<!-- Alert Notifications Configuration (M9) -->
	<div class="p-6 rounded-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 shadow-2xs space-y-5">
		<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
			<div>
				<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
					Alert Notifications
				</h2>
				<p class="text-xs text-zinc-500 mt-0.5 leading-relaxed">
					Deliver real-time alerts for deployment failures, automated backup failures, and disk saturation.
				</p>
			</div>
			<div>
				{#if notifyProvider && notifyConfigured}
					<span class="inline-flex items-center px-2 py-0.5 rounded text-[11px] font-medium bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-400">
						Active ({notifyProvider})
					</span>
				{:else}
					<span class="inline-flex items-center px-2 py-0.5 rounded text-[11px] font-medium bg-zinc-100 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-400">
						Disabled
					</span>
				{/if}
			</div>
		</div>

		<div class="space-y-4">
			<!-- Channel Provider Selection -->
			<div>
				<label for="notify-provider" class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1">
					Notification Channel Provider
				</label>
				<select
					id="notify-provider"
					bind:value={notifyProvider}
					class="w-full sm:w-64 px-3 py-2 text-xs rounded-lg border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 focus:outline-none focus:ring-1 focus:ring-indigo-500"
				>
					<option value="">Disabled</option>
					<option value="ntfy">ntfy.sh (or self-hosted)</option>
					<option value="telegram">Telegram Bot</option>
					<option value="discord">Discord Webhook</option>
				</select>
			</div>

			{#if notifyProvider}
				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<!-- Target Field -->
					<div>
						<label for="notify-target" class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1">
							{#if notifyProvider === 'ntfy'}
								Topic Name or URL
							{:else if notifyProvider === 'telegram'}
								Telegram Chat ID
							{:else}
								Discord Webhook URL
							{/if}
						</label>
						<input
							id="notify-target"
							type="text"
							bind:value={notifyTarget}
							placeholder={notifyProvider === 'ntfy'
								? 'e.g. my-alerts or https://ntfy.sh/my-topic'
								: notifyProvider === 'telegram'
									? 'e.g. -100123456789 or 12345678'
									: 'https://discord.com/api/webhooks/...'}
							class="w-full px-3 py-2 text-xs font-mono rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100"
						/>
						<p class="text-[11px] text-zinc-400 mt-1">
							{#if notifyProvider === 'ntfy'}
								Public ntfy.sh topic name or full self-hosted URL.
							{:else if notifyProvider === 'telegram'}
								Target user ID or group chat ID (invite bot to group first).
							{:else}
								Full Discord incoming webhook URL.
							{/if}
						</p>
					</div>

					<!-- Token / Secret Field (except Discord) -->
					{#if notifyProvider === 'telegram' || notifyProvider === 'ntfy'}
						<div>
							<label for="notify-token" class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1">
								{#if notifyProvider === 'telegram'}
									Bot API Token
								{:else}
									Bearer Auth Token (optional)
								{/if}
							</label>
							<input
								id="notify-token"
								type="password"
								bind:value={notifyToken}
								placeholder={notifyConfigured ? '•••••••••••••••• (leave empty to keep)' : 'Enter secret token'}
								class="w-full px-3 py-2 text-xs font-mono rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100"
							/>
							<p class="text-[11px] text-zinc-400 mt-1">
								Encrypted at rest in SQLite using AES-256-GCM.
							</p>
						</div>
					{/if}
				</div>

				<!-- Trigger Events -->
				<div>
					<label for="notify-events" class="block text-xs font-semibold text-zinc-700 dark:text-zinc-300 mb-1">
						Active Event Triggers (comma-separated)
					</label>
					<input
						id="notify-events"
						type="text"
						bind:value={notifyEvents}
						placeholder="deploy.failed,backup.failed,disk.warning"
						class="w-full sm:w-96 px-3 py-2 text-xs font-mono rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100"
					/>
					<p class="text-[11px] text-zinc-400 mt-1">
						Supported: <code>deploy.failed</code>, <code>backup.failed</code>, <code>disk.warning</code> (triggered at &gt; 85% disk usage).
					</p>
				</div>
			{/if}

			{#if testFeedback}
				<div
					class="p-3 rounded-lg text-xs font-mono {testFeedback.success
						? 'bg-emerald-50 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300 border border-emerald-200 dark:border-emerald-900'
						: 'bg-rose-50 text-rose-800 dark:bg-rose-950/60 dark:text-rose-300 border border-rose-200 dark:border-rose-900'}"
				>
					{testFeedback.detail}
				</div>
			{/if}

			<div class="flex items-center gap-3 pt-2">
				<button
					type="button"
					disabled={savingNotify}
					onclick={saveNotificationSettings}
					class="px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-xs font-semibold shadow-xs disabled:opacity-50"
				>
					{savingNotify ? 'Saving...' : 'Save Settings'}
				</button>
				{#if notifyProvider}
					<button
						type="button"
						disabled={testingNotify}
						onclick={sendTestNotification}
						class="px-4 py-2 bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-zinc-800 dark:text-zinc-200 rounded-lg text-xs font-semibold shadow-xs disabled:opacity-50"
					>
						{testingNotify ? 'Dispatching...' : 'Send Test Notification'}
					</button>
				{/if}
			</div>
		</div>
	</div>

	<!-- Hook Hostname Configuration -->
	<div class="p-6 rounded-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 shadow-2xs space-y-4">
		<div>
			<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
				GitHub Inbound Webhook Hostname
			</h2>
			<p class="text-xs text-zinc-500 mt-0.5 leading-relaxed">
				The public domain routed by Traefik to the unprivileged Unix hook relay socket (<code>/hooks/github/...</code>).
			</p>
		</div>

		<div class="flex flex-col sm:flex-row items-center gap-3">
			<input
				type="text"
				bind:value={hookHost}
				placeholder="e.g. hook.example.com"
				class="w-full sm:flex-1 px-3 py-2 text-xs font-mono rounded-lg border border-zinc-300 dark:border-zinc-700 bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100"
			/>
			<button
				type="button"
				disabled={savingHook}
				onclick={saveHookHost}
				class="w-full sm:w-auto px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-xs font-semibold shadow-xs disabled:opacity-50"
			>
				{savingHook ? 'Saving...' : 'Save Hostname'}
			</button>
		</div>
	</div>

	<!-- Admin Security & TOTP Status -->
	<div class="p-6 rounded-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 shadow-2xs space-y-4">
		<div>
			<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
				Admin Authentication & Credentials
			</h2>
			<p class="text-xs text-zinc-500 mt-0.5">
				To uphold strict security invariants (BRIEF §4.2), credential mutations are accepted exclusively via the local 0600 Unix socket.
			</p>
		</div>

		<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
			<div class="p-4 rounded-xl border border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 space-y-2">
				<div class="flex items-center justify-between text-xs font-semibold">
					<span>Master Password</span>
					<span class="text-emerald-600 dark:text-emerald-400">Set</span>
				</div>
				<p class="text-xs text-zinc-500">To rotate your admin password, run via SSH:</p>
				<div class="flex items-center justify-between p-2 rounded bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 font-mono text-[11px]">
					<span>dermaga admin set-password</span>
					<button
						type="button"
						onclick={() => copyToClipboard('dermaga admin set-password', 'Command')}
						class="text-indigo-600 dark:text-indigo-400 font-sans font-semibold hover:underline"
					>
						Copy
					</button>
				</div>
			</div>

			<div class="p-4 rounded-xl border border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 space-y-2">
				<div class="flex items-center justify-between text-xs font-semibold">
					<span>TOTP 2FA Authentication</span>
					<span class={status?.totpEnabled ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-500'}>
						{status?.totpEnabled ? 'Enabled' : 'Disabled'}
					</span>
				</div>
				<p class="text-xs text-zinc-500">To enable or disable TOTP 2FA:</p>
				<div class="flex items-center justify-between p-2 rounded bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 font-mono text-[11px]">
					<span>dermaga admin totp enable</span>
					<button
						type="button"
						onclick={() => copyToClipboard('dermaga admin totp enable', 'Command')}
						class="text-indigo-600 dark:text-indigo-400 font-sans font-semibold hover:underline"
					>
						Copy
					</button>
				</div>
			</div>
		</div>
	</div>

	<!-- GitHub Host Keys Verification -->
	<div class="p-6 rounded-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 shadow-2xs space-y-4">
		<div class="flex items-center justify-between">
			<div>
				<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
					GitHub Host Keys (pinned known_hosts)
				</h2>
				<p class="text-xs text-zinc-500 mt-0.5">
					Fetches official SSH host keys directly from api.github.com/meta to prevent MITM during git clone.
				</p>
			</div>
			<button
				type="button"
				disabled={syncingHosts}
				onclick={syncGitHubKnownHosts}
				class="px-3.5 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-xs font-semibold shadow-xs disabled:opacity-50"
			>
				{syncingHosts ? 'Syncing...' : 'Sync Known Hosts'}
			</button>
		</div>

		{#if knownHostsResult}
			<div class="p-3 bg-zinc-950 rounded-lg border border-zinc-800 font-mono text-xs text-zinc-300 space-y-1">
				<p class="text-zinc-400 text-[11px]">Saved to {knownHostsResult.file}:</p>
				{#each knownHostsResult.fingerprints as fp}
					<p class="text-emerald-400">{fp}</p>
				{/each}
			</div>
		{/if}
	</div>

	<!-- SSH Tunnel Connection Snippet -->
	<div class="p-6 rounded-2xl bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 shadow-2xs space-y-3">
		<h2 class="text-sm font-bold text-zinc-900 dark:text-zinc-100 uppercase tracking-wider">
			Localhost SSH Tunnel Setup
		</h2>
		<p class="text-xs text-zinc-500 leading-relaxed">
			Because the web panel is bound strictly to <code>127.0.0.1:7070</code> on the host, configure your laptop's <code>~/.ssh/config</code> for one-click access:
		</p>
		<pre class="p-3 bg-zinc-950 text-zinc-300 rounded-lg font-mono text-xs overflow-x-auto select-all border border-zinc-800">Host dermaga-panel
    HostName &lt;your-server-ip&gt;
    Port 4222
    User server
    LocalForward 7070 127.0.0.1:7070
    SessionType none
    ExitOnForwardFailure yes</pre>
		<p class="text-[11px] text-zinc-500">
			Then run <code>ssh dermaga-panel</code> and navigate to <strong>http://localhost:7070</strong> in your browser.
		</p>
	</div>
</div>
