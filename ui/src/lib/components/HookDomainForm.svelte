<script lang="ts">
	import { getHookDomain, checkHookDomainDNS, saveHookDomain } from '#lib/api.js';
	import type { HookDomainDNSInfo } from '#lib/types/api.js';
	import { onMount } from 'svelte';

	let {
		onSaved,
		onSkip
	}: {
		onSaved?: (domain: string) => void;
		onSkip?: () => void;
	} = $props();

	let domain = $state('');
	let serverIP = $state('');
	let dnsInfo = $state<HookDomainDNSInfo | null>(null);
	let loading = $state(true);
	let checking = $state(false);
	let saving = $state(false);
	let error = $state<string | null>(null);
	let successMsg = $state<string | null>(null);

	onMount(async () => {
		try {
			const res = await getHookDomain();
			domain = res.hook_domain || '';
			serverIP = res.server_ip || '';
			dnsInfo = res.dns_check || null;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load hook domain settings';
		} finally {
			loading = false;
		}
	});

	async function handleCheckDNS() {
		if (!domain.trim()) return;
		checking = true;
		error = null;
		successMsg = null;
		try {
			const res = await checkHookDomainDNS({ domain: domain.trim() });
			if (res.error) {
				error = res.error;
				dnsInfo = null;
			} else {
				dnsInfo = res;
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'DNS check failed';
		} finally {
			checking = false;
		}
	}

	async function handleSave(e: SubmitEvent) {
		e.preventDefault();
		if (!domain.trim()) return;
		saving = true;
		error = null;
		successMsg = null;
		try {
			const res = await saveHookDomain({ domain: domain.trim() });
			if (res.error) {
				error = res.error;
			} else {
				domain = res.hook_domain;
				serverIP = res.server_ip;
				dnsInfo = res.dns_check || null;
				successMsg = `Saved ${res.hook_domain} and updated Traefik routing.`;
				onSaved?.(res.hook_domain);
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to save hook domain';
		} finally {
			saving = false;
		}
	}
</script>

<div class="space-y-4">
	<p class="text-xs text-stone-600 dark:text-stone-400 leading-relaxed">
		GitHub sends push webhooks to a single dedicated host (<code class="font-mono bg-stone-100 dark:bg-stone-800 px-1 py-0.5 rounded">/hooks/github-app</code>). This domain is reserved for webhooks and cannot be assigned to an app.
	</p>

	{#if serverIP}
		<div class="p-3 rounded-md bg-stone-50 dark:bg-stone-950 border border-stone-200 dark:border-stone-800 text-xs text-stone-700 dark:text-stone-300">
			Expected DNS record: <code class="font-mono font-semibold">{domain.trim() || 'hooks.example.com'}</code> &rarr; <code class="font-mono font-semibold">A {serverIP}</code>
		</div>
	{/if}

	{#if error}
		<div role="alert" class="p-3 rounded-md bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-300 text-xs font-medium">
			{error}
		</div>
	{/if}

	{#if successMsg}
		<div role="status" class="p-3 rounded-md bg-emerald-50 dark:bg-emerald-950/40 border border-emerald-200 dark:border-emerald-800 text-emerald-800 dark:text-emerald-300 text-xs font-medium">
			{successMsg}
		</div>
	{/if}

	{#if loading}
		<div class="py-6 text-center text-xs text-stone-600 dark:text-stone-400">Loading hook domain...</div>
	{:else}
		<form onsubmit={handleSave} class="space-y-4">
			<div>
				<label for="hook-domain-input" class="block text-xs font-medium text-stone-700 dark:text-stone-300 mb-1.5">
					Hook Domain
				</label>
				<div class="flex flex-col sm:flex-row gap-2">
					<input
						id="hook-domain-input"
						type="text"
						bind:value={domain}
						placeholder="hooks.example.com"
						required
						class="flex-1 px-3 py-2 text-sm font-mono rounded-md border border-stone-300 dark:border-stone-700 bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring focus:border-transparent transition"
					/>
					<button
						type="button"
						onclick={handleCheckDNS}
						disabled={checking || !domain.trim()}
						class="px-3.5 py-2 rounded-md border border-stone-300 dark:border-stone-700 text-stone-700 dark:text-stone-300 hover:bg-stone-100 dark:hover:bg-stone-800 disabled:opacity-50 text-xs font-medium transition"
					>
						{checking ? 'Checking...' : 'Check DNS'}
					</button>
				</div>
			</div>

			{#if dnsInfo}
				<div class="p-3 rounded-md border text-xs space-y-1.5 {dnsInfo.warning
					? 'bg-amber-50 dark:bg-amber-950/40 border-amber-300 dark:border-amber-800 text-amber-900 dark:text-amber-200'
					: 'bg-emerald-50 dark:bg-emerald-950/40 border-emerald-200 dark:border-emerald-800 text-emerald-800 dark:text-emerald-300'}">
					<div class="font-medium">
						{dnsInfo.warning ? dnsInfo.warning : `DNS matches this server (${dnsInfo.server_ip}).`}
					</div>
					<div class="font-mono text-[11px] opacity-90">
						A: {dnsInfo.a_records?.length ? dnsInfo.a_records.join(', ') : 'none'} &bull; AAAA: {dnsInfo.aaaa_records?.length ? dnsInfo.aaaa_records.join(', ') : 'none'}
					</div>
				</div>
			{/if}

			<div class="flex flex-col sm:flex-row gap-2.5">
				<button
					type="submit"
					disabled={saving || !domain.trim()}
					class="flex-1 py-2 px-4 rounded-md bg-brand-primary hover:bg-brand-hover active:bg-brand-active disabled:opacity-50 text-brand-btn-text font-medium text-sm transition shadow-sm focus:outline-none focus:ring-2 focus:ring-brand-ring focus:ring-offset-2 dark:focus:ring-offset-stone-900"
				>
					{saving ? 'Saving...' : 'Save Hook Domain'}
				</button>
				{#if onSkip}
					<button
						type="button"
						onclick={onSkip}
						disabled={saving}
						class="py-2 px-4 rounded-md border border-stone-300 dark:border-stone-700 text-stone-700 dark:text-stone-300 hover:bg-stone-100 dark:hover:bg-stone-800 disabled:opacity-50 font-medium text-sm transition"
					>
						Skip for now
					</button>
				{/if}
			</div>
		</form>
	{/if}
</div>
