<script lang="ts">
	import { checkDomainDNS } from '#lib/api.js';
	import type { DomainCheckResponse } from '#lib/types/api.js';

	let {
		domain = $bindable(''),
		fieldError = null,
		inputId = 'app-domain-input',
		label = 'Primary Domain'
	}: {
		domain: string;
		fieldError?: string | null;
		inputId?: string;
		label?: string;
	} = $props();

	let checking = $state(false);
	let dnsError = $state<string | null>(null);
	let dnsResult = $state<DomainCheckResponse | null>(null);

	async function handleCheck() {
		if (!domain.trim()) return;
		checking = true;
		dnsError = null;
		try {
			const res = await checkDomainDNS({ domain: domain.trim() });
			if (res.error) {
				dnsError = res.error;
				dnsResult = null;
			} else {
				dnsResult = res;
			}
		} catch (err) {
			dnsError = err instanceof Error ? err.message : 'DNS check failed';
		} finally {
			checking = false;
		}
	}
</script>

<div class="space-y-2">
	<label for={inputId} class="block text-xs font-medium text-stone-700 dark:text-stone-300">
		{label}
	</label>
	<div class="flex flex-col sm:flex-row gap-2">
		<input
			id={inputId}
			type="text"
			bind:value={domain}
			placeholder="app.example.com"
			class="flex-1 px-3 py-2 text-sm font-mono rounded-md border {fieldError ? 'border-red-500 dark:border-red-500' : 'border-stone-300 dark:border-stone-700'} bg-white dark:bg-stone-950 text-stone-900 dark:text-stone-100 focus:outline-none focus:ring-2 focus:ring-brand-ring transition"
		/>
		<button
			type="button"
			onclick={handleCheck}
			disabled={checking || !domain.trim()}
			class="px-3.5 py-2 text-xs font-medium rounded-md border border-stone-300 dark:border-stone-700 text-stone-800 dark:text-stone-200 hover:bg-stone-100 dark:hover:bg-stone-800 disabled:opacity-50 focus:outline-none focus:ring-2 focus:ring-brand-ring transition"
		>
			{checking ? 'Checking...' : 'Check DNS'}
		</button>
	</div>

	{#if fieldError}
		<p class="text-xs text-red-700 dark:text-red-400 font-medium">{fieldError}</p>
	{/if}

	{#if dnsError}
		<p class="text-xs text-red-700 dark:text-red-400">{dnsError}</p>
	{/if}

	{#if dnsResult}
		<div
			class="p-3 rounded-md text-xs border {dnsResult.matches
				? 'bg-emerald-50 dark:bg-emerald-950/40 border-emerald-200 dark:border-emerald-800 text-emerald-800 dark:text-emerald-300'
				: 'bg-amber-50 dark:bg-amber-950/40 border-amber-200 dark:border-amber-800 text-amber-900 dark:text-amber-200'}"
		>
			{#if dnsResult.matches}
				DNS matches this server (<code class="font-mono">{dnsResult.expected_ip}</code>). Traefik will issue a certificate automatically.
			{:else}
				<p class="font-medium">DNS does not point to this server yet.</p>
				<p class="mt-1">
					Create an <code class="font-mono font-semibold">A</code> record for <code class="font-mono">{dnsResult.domain}</code> pointing to <code class="font-mono font-semibold">{dnsResult.expected_ip || 'your server IP'}</code>. You can still deploy now while DNS propagates.
				</p>
			{/if}
		</div>
	{/if}
</div>
