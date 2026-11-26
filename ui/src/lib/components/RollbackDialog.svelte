<script lang="ts">
	// Rollback confirmation (DEP-4), owner-approved mockup A: explains what the rollback restores and
	// lists env variable names that differ from the current settings (values are never sent).
	import { getRollbackPreview, rollback } from '#lib/api.js'
	import { timeAgo } from '#lib/time.js'
	import type { DeploymentInfo, RollbackPreview } from '#lib/types/api.js'

	let {
		appId,
		appName,
		target = $bindable(null),
		onstarted
	}: {
		appId: string
		appName: string
		target: DeploymentInfo | null
		onstarted: (dep: DeploymentInfo) => void
	} = $props()

	let dialog: HTMLDialogElement
	let preview = $state<RollbackPreview | null>(null)
	let loadErr = $state<string | null>(null)
	let submitErr = $state<string | null>(null)
	let busy = $state(false)

	const signs: Record<string, string> = {
		added: '+',
		removed: '−',
		changed: '~'
	}
	const words: Record<string, string> = {
		added: 'added since',
		removed: 'removed since',
		changed: 'value changed since'
	}

	let changedNames = $derived(
		(preview?.env_changes ?? [])
			.filter((c) => c.change === 'changed')
			.map((c) => c.name)
	)

	$effect(() => {
		if (target && !dialog.open) {
			const id = target.id
			preview = null
			loadErr = null
			submitErr = null
			dialog.showModal()
			getRollbackPreview(appId, id)
				.then((p) => (preview = p))
				.catch((e) => (loadErr = e instanceof Error ? e.message : String(e)))
		} else if (!target && dialog.open) {
			dialog.close()
		}
	})

	async function confirm() {
		if (!target) return
		busy = true
		submitErr = null
		const res = await rollback(appId, target.id)
		busy = false
		if (!res.ok || !res.data) {
			submitErr = res.error ?? 'Failed to start the rollback'
			return
		}
		target = null
		onstarted(res.data)
	}

	function mib(bytes: number): string {
		return `${Math.floor(bytes / 1048576)} MiB`
	}
</script>

<dialog
	bind:this={dialog}
	onclose={() => (target = null)}
	aria-labelledby="rollback-title"
	class="m-auto w-[calc(100%-2rem)] max-w-lg rounded-xl border border-stone-200 bg-white p-0 text-stone-900 shadow-xl backdrop:bg-stone-950/60 dark:border-stone-800 dark:bg-stone-900 dark:text-stone-100"
>
	{#if target}
		{@const rel = `r${target.release_number}`}
		<div class="space-y-4 p-5 text-sm">
			<h2 id="rollback-title" class="text-base font-semibold">
				Roll back {appName} to {rel}?
			</h2>
			<p class="font-mono text-xs text-stone-600 dark:text-stone-400">
				{rel} &middot; {target.commit_hash
					? target.commit_hash.slice(0, 7)
					: 'HEAD'}
				{target.commit_message} &middot; built {timeAgo(target.started_at)}
			</p>

			<ul
				class="list-disc space-y-1 pl-5 text-xs text-stone-700 dark:text-stone-300"
			>
				<li>Uses {rel}'s image (no rebuild), usually &lt; 10 s.</li>
				<li>
					Runtime env goes back to what {rel} had. Your Environment tab stays as it
					is.
				</li>
				<li>Domains, headers and middleware stay current.</li>
				<li>Build-time variables are baked into {rel}.</li>
			</ul>

			{#if loadErr}
				<p role="alert" class="text-xs text-red-700 dark:text-red-400">
					{loadErr}
				</p>
			{:else if !preview}
				<p class="text-xs text-stone-600 dark:text-stone-400">
					Comparing environments...
				</p>
			{:else}
				<div class="space-y-1.5">
					<h3 class="text-xs font-semibold">
						Env differences from the current settings:
					</h3>
					{#if preview.env_changes.length === 0}
						<p class="text-xs text-stone-600 dark:text-stone-400">
							None: {rel} used the same runtime variables.
						</p>
					{:else}
						<ul class="space-y-0.5 font-mono text-xs">
							{#each preview.env_changes as c (c.name)}
								<li>
									<span aria-hidden="true">{signs[c.change]}</span>
									<span class="font-semibold">{c.name}</span>
									<span class="font-sans text-stone-600 dark:text-stone-400">
										{words[c.change]}
										{rel}
									</span>
								</li>
							{/each}
						</ul>
					{/if}
				</div>
				{#if changedNames.length > 0}
					<p
						role="note"
						class="rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
					>
						{changedNames.join(', ')} changed after {rel}. If the old value was
						revoked, {rel} may fail its health check; then the live release keeps
						serving.
					</p>
				{/if}
				{#if !preview.memory.ok}
					<p
						role="alert"
						class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
					>
						Not enough free memory: {mib(preview.memory.available_bytes)} available,
						{rel} needs {mib(preview.memory.needed_bytes)} next to the live release.
						Free memory or lower its limit first.
					</p>
				{/if}
			{/if}

			{#if submitErr}
				<p role="alert" class="text-xs text-red-700 dark:text-red-400">
					{submitErr}
				</p>
			{/if}

			<div class="flex justify-end gap-2 pt-1 text-xs">
				<button
					type="button"
					onclick={() => (target = null)}
					class="rounded-md border border-stone-300 px-3 py-1.5 transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:hover:bg-stone-800"
				>
					Cancel
				</button>
				<button
					type="button"
					onclick={confirm}
					disabled={busy || !preview || !preview.memory.ok}
					class="rounded-lg bg-brand-primary px-4 py-1.5 font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:outline-none disabled:opacity-50"
				>
					{busy ? 'Starting...' : `Roll back to ${rel}`}
				</button>
			</div>
		</div>
	{/if}
</dialog>
