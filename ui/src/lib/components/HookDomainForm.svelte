<script lang="ts">
	import {
		getHookDomain,
		checkHookDomainDNS,
		saveHookDomain
	} from '#lib/api.js'
	import type { CertificateInfo, HookDomainDNSInfo } from '#lib/types/api.js'
	import { onMount } from 'svelte'

	let {
		onSaved,
		onSkip
	}: {
		onSaved?: (domain: string) => void
		onSkip?: () => void
	} = $props()

	let domain = $state('')
	let serverIP = $state('')
	let dnsInfo = $state<HookDomainDNSInfo | null>(null)
	let certificate = $state<CertificateInfo | null>(null)
	let loading = $state(true)
	let checking = $state(false)
	let saving = $state(false)
	let error = $state<string | null>(null)
	let successMsg = $state<string | null>(null)

	onMount(async () => {
		try {
			const res = await getHookDomain()
			domain = res.hook_domain || ''
			serverIP = res.server_ip || ''
			dnsInfo = res.dns_check || null
			certificate = res.certificate || null
		} catch (err) {
			error =
				err instanceof Error
					? err.message
					: 'Failed to load hook domain settings'
		} finally {
			loading = false
		}
	})

	async function handleCheckDNS() {
		if (!domain.trim()) return
		checking = true
		error = null
		successMsg = null
		try {
			const res = await checkHookDomainDNS({ domain: domain.trim() })
			if (res.error) {
				error = res.error
				dnsInfo = null
			} else {
				dnsInfo = res
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'DNS check failed'
		} finally {
			checking = false
		}
	}

	async function handleSave(e: SubmitEvent) {
		e.preventDefault()
		if (!domain.trim()) return
		saving = true
		error = null
		successMsg = null
		try {
			const res = await saveHookDomain({ domain: domain.trim() })
			if (res.error) {
				error = res.error
			} else {
				domain = res.hook_domain
				serverIP = res.server_ip
				dnsInfo = res.dns_check || null
				certificate = res.certificate || null
				successMsg = `Saved ${res.hook_domain} and updated Traefik routing.`
				onSaved?.(res.hook_domain)
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to save hook domain'
		} finally {
			saving = false
		}
	}
</script>

<div class="space-y-4">
	<p class="text-xs leading-relaxed text-stone-600 dark:text-stone-400">
		GitHub sends push webhooks to a single dedicated host (<code
			class="rounded bg-stone-100 px-1 py-0.5 font-mono dark:bg-stone-800"
		>
			/hooks/github-app
		</code>). This domain is reserved for webhooks and cannot be assigned to an
		app.
	</p>

	{#if serverIP}
		<div
			class="rounded-md border border-stone-200 bg-stone-50 p-3 text-xs text-stone-700 dark:border-stone-800 dark:bg-stone-950 dark:text-stone-300"
		>
			Expected DNS record: <code class="font-mono font-semibold">
				{domain.trim() || 'hooks.example.com'}
			</code>
			&rarr; <code class="font-mono font-semibold">A {serverIP}</code>
		</div>
	{/if}

	{#if error}
		<div
			role="alert"
			class="rounded-md border border-red-200 bg-red-50 p-3 text-xs font-medium text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
		>
			{error}
		</div>
	{/if}

	{#if successMsg}
		<div
			role="status"
			class="rounded-md border border-emerald-200 bg-emerald-50 p-3 text-xs font-medium text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300"
		>
			{successMsg}
		</div>
	{/if}

	{#if loading}
		<div class="py-6 text-center text-xs text-stone-600 dark:text-stone-400">
			Loading hook domain...
		</div>
	{:else}
		<form onsubmit={handleSave} class="space-y-4">
			<div>
				<label
					for="hook-domain-input"
					class="mb-1.5 block text-xs font-medium text-stone-700 dark:text-stone-300"
				>
					Hook Domain
				</label>
				<div class="flex flex-col gap-2 sm:flex-row">
					<input
						id="hook-domain-input"
						type="text"
						bind:value={domain}
						placeholder="hooks.example.com"
						required
						class="flex-1 rounded-md border border-stone-300 bg-white px-3 py-2 font-mono text-sm text-stone-900 transition focus:border-transparent focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:bg-stone-950 dark:text-stone-100"
					/>
					<button
						type="button"
						onclick={handleCheckDNS}
						disabled={checking || !domain.trim()}
						class="rounded-md border border-stone-300 px-3.5 py-2 text-xs font-medium text-stone-700 transition hover:bg-stone-100 disabled:opacity-50 dark:border-stone-700 dark:text-stone-300 dark:hover:bg-stone-800"
					>
						{checking ? 'Checking...' : 'Check DNS'}
					</button>
				</div>
			</div>

			{#if dnsInfo}
				<div
					class="space-y-1.5 rounded-md border p-3 text-xs {dnsInfo.warning
						? 'border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200'
						: 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300'}"
				>
					<div class="font-medium">
						{dnsInfo.warning
							? dnsInfo.warning
							: `DNS matches this server (${dnsInfo.server_ip}).`}
					</div>
					<div class="font-mono text-[11px] opacity-90">
						A: {dnsInfo.a_records?.length
							? dnsInfo.a_records.join(', ')
							: 'none'} &bull; AAAA: {dnsInfo.aaaa_records?.length
							? dnsInfo.aaaa_records.join(', ')
							: 'none'}
					</div>
				</div>
			{/if}

			{#if certificate}
				<p class="text-xs text-stone-700 dark:text-stone-300">
					{#if certificate.status === 'active'}
						HTTPS certificate active until {new Date(
							certificate.expires_at ?? ''
						).toLocaleDateString('en-GB', {
							day: 'numeric',
							month: 'short',
							year: 'numeric'
						})}.
						{#if certificate.expiring_soon}
							<span class="text-amber-800 dark:text-amber-300">
								It is close to expiry and has not been renewed.
							</span>
						{/if}
					{:else}
						No HTTPS certificate yet. Traefik requests it once DNS points to
						this server.
					{/if}
				</p>
			{/if}

			<div class="flex flex-col gap-2.5 sm:flex-row">
				<button
					type="submit"
					disabled={saving || !domain.trim()}
					class="flex-1 rounded-md bg-brand-primary px-4 py-2 text-sm font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:ring-offset-2 focus:outline-none active:bg-brand-active disabled:opacity-50 dark:focus:ring-offset-stone-900"
				>
					{saving ? 'Saving...' : 'Save Hook Domain'}
				</button>
				{#if onSkip}
					<button
						type="button"
						onclick={onSkip}
						disabled={saving}
						class="rounded-md border border-stone-300 px-4 py-2 text-sm font-medium text-stone-700 transition hover:bg-stone-100 disabled:opacity-50 dark:border-stone-700 dark:text-stone-300 dark:hover:bg-stone-800"
					>
						Skip for now
					</button>
				{/if}
			</div>
		</form>
	{/if}
</div>
