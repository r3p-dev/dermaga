<script lang="ts">
	// Domains tab (owner-approved mockup A, rows): roles, DNS check with the record to create, and
	// certificate status per domain. Every change sends the whole set; the server applies the route
	// through Traefik before it stores it, and keeps the previous domains when Traefik refuses.
	import {
		checkDomainsAgain,
		getDomains,
		updateDomains,
		type DomainsResult
	} from '#lib/api.js'
	import DomainDNSChecker from '#lib/components/DomainDNSChecker.svelte'
	import type {
		DomainInput,
		DomainsResponse,
		DomainView
	} from '#lib/types/api.js'
	import { onMount } from 'svelte'

	let { appId }: { appId: string } = $props()

	let data = $state<DomainsResponse | null>(null)
	let loadErr = $state<string | null>(null)
	let busy = $state<string | null>(null) // what is being saved, shown while Traefik checks it
	let actionErr = $state<string | null>(null)
	let actionMsg = $state<string | null>(null)

	let prefix = $state('')
	let strip = $state(false)
	let prefixErr = $state<string | null>(null)

	let dialog: HTMLDialogElement
	let newDomain = $state('')
	let newRole = $state<'alias' | 'redirect'>('alias')
	let addPair = $state(false)
	let newErr = $state<string | null>(null)

	onMount(load)

	async function load() {
		try {
			apply(await getDomains(appId))
		} catch (err) {
			loadErr = err instanceof Error ? err.message : 'Failed to load domains'
		}
	}

	function apply(res: DomainsResponse) {
		data = res
		prefix = res.path_prefix
		strip = res.strip_prefix
		loadErr = null
	}

	// The www ↔ apex counterpart of a domain, offered as a redirect when it is not listed yet.
	let pair = $derived.by(() => {
		const d = newDomain.trim().toLowerCase().replace(/\.$/, '')
		if (!d.includes('.')) return ''
		const other = d.startsWith('www.') ? d.slice(4) : `www.${d}`
		if (!other.includes('.') || data?.domains.some((x) => x.domain === other))
			return ''
		return other
	})

	function inputs(): DomainInput[] {
		return (data?.domains ?? []).map((d) => ({
			domain: d.domain,
			role: d.role
		}))
	}

	async function save(
		set: DomainInput[],
		what: string,
		done: string
	): Promise<DomainsResult> {
		busy = what
		actionErr = null
		actionMsg = null
		const res = await updateDomains(appId, {
			domains: set,
			path_prefix: prefix.trim(),
			strip_prefix: strip
		})
		busy = null
		if (res.ok) {
			apply(res.data)
			actionMsg = done
		}
		return res
	}

	async function makePrimary(d: DomainView) {
		const set = inputs().map((x) => ({
			domain: x.domain,
			role:
				x.domain === d.domain
					? 'primary'
					: x.role === 'primary'
						? 'alias'
						: x.role
		}))
		const res = await save(
			set,
			`Making ${d.domain} the primary domain`,
			`${d.domain} is now the primary domain.`
		)
		if (!res.ok) actionErr = res.error
	}

	async function remove(d: DomainView) {
		const res = await save(
			inputs().filter((x) => x.domain !== d.domain),
			`Removing ${d.domain}`,
			`${d.domain} was removed.`
		)
		if (!res.ok) actionErr = res.error
	}

	async function savePrefix(e: SubmitEvent) {
		e.preventDefault()
		prefixErr = null
		const res = await save(
			inputs(),
			'Saving the path prefix',
			'Path prefix saved.'
		)
		if (!res.ok) {
			if (res.fields?.path_prefix) prefixErr = res.fields.path_prefix
			else actionErr = res.error
		}
	}

	function openAdd() {
		newDomain = ''
		newRole = 'alias'
		addPair = false
		newErr = null
		dialog.showModal()
	}

	async function add(e: SubmitEvent) {
		e.preventDefault()
		newErr = null
		const set = inputs()
		const index = set.length
		set.push({ domain: newDomain.trim(), role: newRole })
		const withPair = addPair && pair !== ''
		if (withPair) set.push({ domain: pair, role: 'redirect' })
		const res = await save(
			set,
			`Adding ${newDomain.trim()}`,
			`${newDomain.trim()} was added.`
		)
		if (res.ok) {
			dialog.close()
			return
		}
		newErr =
			res.fields?.[`domains.${index}`] ??
			(withPair ? res.fields?.[`domains.${index + 1}`] : undefined) ??
			res.fields?.domains ??
			res.error
	}

	async function checkAgain() {
		busy = 'Checking DNS and certificates again'
		actionErr = null
		actionMsg = null
		const res = await checkDomainsAgain(appId)
		busy = null
		if (res.ok) {
			apply(res.data)
			actionMsg = 'Checked again.'
		} else actionErr = res.error
	}

	const roleLabel: Record<string, string> = {
		primary: 'Primary',
		alias: 'Alias',
		redirect: 'Redirects to primary'
	}

	function day(iso?: string): string {
		return iso
			? new Date(iso).toLocaleDateString('en-GB', {
					day: 'numeric',
					month: 'short',
					year: 'numeric'
				})
			: ''
	}

	function clock(iso?: string): string {
		return iso
			? new Date(iso).toLocaleTimeString('en-GB', {
					hour: '2-digit',
					minute: '2-digit'
				})
			: ''
	}
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between gap-3">
		<h2 class="text-sm font-semibold">Domains</h2>
		<button
			type="button"
			onclick={openAdd}
			disabled={!data || busy !== null}
			class="rounded-lg bg-brand-primary px-3.5 py-1.5 text-xs font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:outline-none disabled:opacity-50"
		>
			+ Add domain
		</button>
	</div>

	{#if busy}
		<p role="status" class="text-xs text-stone-600 dark:text-stone-400">
			{busy}... checking that Traefik serves the new route.
		</p>
	{/if}
	{#if actionErr}
		<p
			role="alert"
			class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
		>
			{actionErr}
		</p>
	{/if}
	{#if actionMsg && !busy}
		<p role="status" class="text-xs text-emerald-800 dark:text-emerald-300">
			{actionMsg}
		</p>
	{/if}

	{#if loadErr}
		<p role="alert" class="text-xs text-red-700 dark:text-red-400">
			{loadErr}
		</p>
	{:else if !data}
		<p class="text-xs text-stone-600 dark:text-stone-400">Loading domains...</p>
	{:else}
		{#if !data.live}
			<p class="text-xs text-stone-600 dark:text-stone-400">
				Nothing is served yet. These domains go live with the first successful
				deploy.
			</p>
		{/if}

		<div class="space-y-2.5">
			{#each data.domains as d (d.domain)}
				{@const cert = d.certificate}
				<div
					class="space-y-2 rounded-lg border border-stone-200 bg-white p-4 text-xs dark:border-stone-800 dark:bg-stone-900"
				>
					<div
						class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between"
					>
						<div class="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1">
							<span class="font-mono font-semibold break-all">{d.domain}</span>
							<span class="text-stone-600 dark:text-stone-400">
								{roleLabel[d.role] ?? d.role}
							</span>
							{#if d.dns_verified}
								<span class="text-emerald-800 dark:text-emerald-300">
									✓ DNS points here
								</span>
							{:else}
								<span class="text-amber-800 dark:text-amber-300">
									✗ DNS does not point here yet
								</span>
							{/if}
							{#if cert.status === 'active'}
								<span
									class={cert.expiring_soon
										? 'text-amber-800 dark:text-amber-300'
										: 'text-emerald-800 dark:text-emerald-300'}
								>
									● HTTPS active · until {day(cert.expires_at)}
								</span>
							{:else if cert.status === 'waiting_dns'}
								<span class="text-stone-600 dark:text-stone-400">
									○ Certificate waits for DNS
								</span>
							{:else if cert.status === 'requesting'}
								<span class="text-stone-600 dark:text-stone-400">
									◌ Requesting certificate
								</span>
							{:else if cert.status === 'failed'}
								<span class="font-medium text-red-700 dark:text-red-400">
									✗ Certificate failed
								</span>
							{/if}
						</div>
						{#if d.role !== 'primary'}
							<details class="relative shrink-0 self-start sm:self-auto">
								<summary
									aria-label="Actions for {d.domain}"
									class="cursor-pointer list-none rounded-md border border-stone-300 px-2.5 py-1 font-medium transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:hover:bg-stone-800"
								>
									Actions <span aria-hidden="true">▾</span>
								</summary>
								<div
									class="absolute right-0 z-10 mt-1 w-48 rounded-lg border border-stone-200 bg-white p-1 shadow-lg dark:border-stone-700 dark:bg-stone-900"
								>
									<button
										type="button"
										disabled={busy !== null}
										onclick={(e) => {
											e.currentTarget
												.closest('details')
												?.removeAttribute('open')
											makePrimary(d)
										}}
										class="block w-full rounded-md px-3 py-2 text-left hover:bg-stone-100 focus:bg-stone-100 focus:outline-none disabled:opacity-50 dark:hover:bg-stone-800 dark:focus:bg-stone-800"
									>
										Make primary
									</button>
									<button
										type="button"
										disabled={busy !== null}
										onclick={(e) => {
											e.currentTarget
												.closest('details')
												?.removeAttribute('open')
											remove(d)
										}}
										class="block w-full rounded-md px-3 py-2 text-left text-red-700 hover:bg-stone-100 focus:bg-stone-100 focus:outline-none disabled:opacity-50 dark:text-red-400 dark:hover:bg-stone-800 dark:focus:bg-stone-800"
									>
										Remove
									</button>
								</div>
							</details>
						{/if}
					</div>

					{#if cert.status === 'active' && cert.expiring_soon}
						<p class="text-amber-800 dark:text-amber-300">
							This certificate is close to expiry and has not been renewed.
							Check that DNS still points here and that port 80 is reachable.
						</p>
					{/if}

					{#if !d.dns_verified && d.dns}
						<div
							class="flex flex-col gap-2 rounded-md bg-amber-50 p-3 text-amber-900 sm:flex-row sm:items-center sm:justify-between dark:bg-amber-950/40 dark:text-amber-200"
						>
							<div class="space-y-1">
								<p>
									Create this record at your DNS provider:
									<code class="font-mono font-semibold">A</code>
									<code class="font-mono">{d.domain}</code>
									→
									<code class="font-mono font-semibold">
										{d.dns.server_ip || data.server_ip}
									</code>
								</p>
								{#if d.dns.aaaa_mismatch && d.dns.warning}
									<p>{d.dns.warning}</p>
								{/if}
								<p class="text-amber-800 dark:text-amber-300">
									dermaga checks again every 5 minutes and requests the
									certificate once the record is right.
								</p>
							</div>
							<button
								type="button"
								onclick={checkAgain}
								disabled={busy !== null}
								class="shrink-0 self-start rounded-md border border-amber-300 px-3 py-1.5 font-medium transition hover:bg-amber-100 focus:ring-2 focus:ring-brand-ring focus:outline-none disabled:opacity-50 sm:self-auto dark:border-amber-700 dark:hover:bg-amber-900/40"
							>
								Check again
							</button>
						</div>
					{/if}

					{#if cert.status === 'failed'}
						<div
							class="flex flex-col gap-2 rounded-md bg-red-50 p-3 text-red-900 sm:flex-row sm:items-start sm:justify-between dark:bg-red-950/40 dark:text-red-200"
						>
							<div class="min-w-0 space-y-1">
								<p class="font-medium">{cert.reason}</p>
								<p>{cert.fix}</p>
								<p>
									This domain is not served until the certificate works. Next
									automatic try: {clock(cert.next_retry_at)}.
								</p>
								{#if cert.detail}
									<details>
										<summary
											class="cursor-pointer focus:ring-2 focus:ring-brand-ring focus:outline-none"
										>
											Details from the certificate authority
										</summary>
										<p class="mt-1 font-mono break-all">{cert.detail}</p>
									</details>
								{/if}
							</div>
							<button
								type="button"
								onclick={checkAgain}
								disabled={busy !== null}
								class="shrink-0 self-start rounded-md border border-red-300 px-3 py-1.5 font-medium transition hover:bg-red-100 focus:ring-2 focus:ring-brand-ring focus:outline-none disabled:opacity-50 dark:border-red-700 dark:hover:bg-red-900/40"
							>
								Check again
							</button>
						</div>
					{/if}
				</div>
			{/each}
		</div>

		<form
			onsubmit={savePrefix}
			class="space-y-3 rounded-xl border border-stone-200 bg-white p-5 text-xs dark:border-stone-800 dark:bg-stone-900"
		>
			<h3 class="text-sm font-semibold">Path prefix</h3>
			<p class="text-stone-600 dark:text-stone-400">
				Optional. Serve the app only under a path such as /blog on every domain
				above. Leave empty to serve the whole domain.
			</p>
			<div class="flex flex-col gap-3 sm:flex-row sm:items-end">
				<div class="sm:w-64">
					<label for="dom-prefix" class="mb-1 block font-medium">Prefix</label>
					<input
						id="dom-prefix"
						type="text"
						bind:value={prefix}
						placeholder="/blog"
						aria-invalid={prefixErr ? 'true' : undefined}
						aria-describedby={prefixErr ? 'dom-prefix-err' : undefined}
						class="w-full rounded-md border bg-white px-3 py-2 font-mono focus:ring-2 focus:ring-brand-ring focus:outline-none dark:bg-stone-950 {prefixErr
							? 'border-red-500'
							: 'border-stone-300 dark:border-stone-700'}"
					/>
				</div>
				<label class="flex items-center gap-2 py-2">
					<input type="checkbox" bind:checked={strip} class="size-4" />
					Remove the prefix before forwarding to the app
				</label>
				<button
					type="submit"
					disabled={busy !== null}
					class="self-start rounded-lg bg-brand-primary px-4 py-2 font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:outline-none disabled:opacity-50 sm:self-auto"
				>
					Save
				</button>
			</div>
			{#if prefixErr}
				<p id="dom-prefix-err" class="text-red-700 dark:text-red-400">
					{prefixErr}
				</p>
			{/if}
		</form>
	{/if}
</div>

<dialog
	bind:this={dialog}
	aria-labelledby="add-domain-title"
	class="m-auto w-[calc(100%-2rem)] max-w-lg rounded-xl border border-stone-200 bg-white p-0 text-stone-900 shadow-xl backdrop:bg-stone-950/60 dark:border-stone-800 dark:bg-stone-900 dark:text-stone-100"
>
	<form onsubmit={add} class="space-y-4 p-5 text-sm">
		<h2 id="add-domain-title" class="text-base font-semibold">Add a domain</h2>
		<DomainDNSChecker
			bind:domain={newDomain}
			inputId="add-domain-input"
			label="Domain"
			fieldError={newErr}
			pendingNote="You can save it now; the certificate is requested once the record is right."
		/>
		<fieldset class="space-y-2 text-xs">
			<legend class="mb-1 font-medium">Role</legend>
			<label class="flex items-center gap-2">
				<input type="radio" bind:group={newRole} value="alias" class="size-4" />
				Alias: serves the app like the primary domain
			</label>
			<label class="flex items-center gap-2">
				<input
					type="radio"
					bind:group={newRole}
					value="redirect"
					class="size-4"
				/>
				Redirect: sends visitors to the primary domain (301)
			</label>
		</fieldset>
		{#if pair}
			<label class="flex items-start gap-2 text-xs">
				<input
					type="checkbox"
					bind:checked={addPair}
					class="mt-0.5 size-4 shrink-0"
				/>
				<span>
					Also add <span class="font-mono break-all">{pair}</span> and redirect it
					to the primary domain
				</span>
			</label>
		{/if}
		<div class="flex justify-end gap-2 text-xs">
			<button
				type="button"
				onclick={() => dialog.close()}
				class="rounded-lg border border-stone-300 px-4 py-1.5 font-medium transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:hover:bg-stone-800"
			>
				Cancel
			</button>
			<button
				type="submit"
				disabled={busy !== null || !newDomain.trim()}
				class="rounded-lg bg-brand-primary px-4 py-1.5 font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:outline-none disabled:opacity-50"
			>
				{busy ? 'Saving...' : 'Save'}
			</button>
		</div>
	</form>
</dialog>
