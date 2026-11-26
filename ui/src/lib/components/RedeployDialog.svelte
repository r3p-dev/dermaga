<script lang="ts">
	// Redeploy: the branch's latest commit or a chosen commit. Shows the memory check before the
	// deploy starts (owner decision: refused when MemAvailable < the app's memory limit).
	import { getMemoryCheck, redeploy } from '#lib/api.js'
	import type { DeploymentInfo, MemoryCheckResponse } from '#lib/types/api.js'

	let {
		appId,
		appName,
		branch,
		open = $bindable(false),
		mode = $bindable<'latest' | 'commit'>('latest'),
		onstarted
	}: {
		appId: string
		appName: string
		branch: string
		open: boolean
		mode: 'latest' | 'commit'
		onstarted: (dep: DeploymentInfo) => void
	} = $props()

	let dialog: HTMLDialogElement
	let commit = $state('')
	let memory = $state<MemoryCheckResponse | null>(null)
	let memoryErr = $state<string | null>(null)
	let commitErr = $state<string | null>(null)
	let submitErr = $state<string | null>(null)
	let busy = $state(false)

	$effect(() => {
		if (open && !dialog.open) {
			commit = ''
			memory = null
			memoryErr = null
			commitErr = null
			submitErr = null
			dialog.showModal()
			getMemoryCheck(appId)
				.then((m) => (memory = m))
				.catch((e) => (memoryErr = e instanceof Error ? e.message : String(e)))
		} else if (!open && dialog.open) {
			dialog.close()
		}
	})

	async function start() {
		busy = true
		commitErr = null
		submitErr = null
		const res = await redeploy(appId, mode === 'commit' ? commit : undefined)
		busy = false
		if (!res.ok || !res.data) {
			commitErr = res.fields?.commit ?? null
			submitErr = commitErr ? null : (res.error ?? 'Failed to start the deploy')
			return
		}
		open = false
		onstarted(res.data)
	}

	function mib(bytes: number): string {
		return `${Math.floor(bytes / 1048576)} MiB`
	}
</script>

<dialog
	bind:this={dialog}
	onclose={() => (open = false)}
	aria-labelledby="redeploy-title"
	class="m-auto w-[calc(100%-2rem)] max-w-lg rounded-xl border border-stone-200 bg-white p-0 text-stone-900 shadow-xl backdrop:bg-stone-950/60 dark:border-stone-800 dark:bg-stone-900 dark:text-stone-100"
>
	{#if open}
		<form
			class="space-y-4 p-5 text-sm"
			onsubmit={(e) => {
				e.preventDefault()
				start()
			}}
		>
			<h2 id="redeploy-title" class="text-base font-semibold">
				Deploy {appName}
			</h2>
			<fieldset class="space-y-2 text-xs">
				<legend class="sr-only">What to deploy</legend>
				<label class="flex items-center gap-2">
					<input type="radio" bind:group={mode} value="latest" />
					Latest commit on <span class="font-mono">{branch}</span>
				</label>
				<label class="flex items-center gap-2">
					<input type="radio" bind:group={mode} value="commit" />
					A specific commit
				</label>
				{#if mode === 'commit'}
					<div class="space-y-1 pl-6">
						<label for="redeploy-commit" class="block font-medium">
							Commit SHA
						</label>
						<input
							id="redeploy-commit"
							bind:value={commit}
							required
							autocomplete="off"
							spellcheck="false"
							aria-invalid={commitErr ? 'true' : undefined}
							aria-describedby="redeploy-commit-help"
							class="w-full rounded-md border border-stone-300 bg-white px-3 py-1.5 font-mono dark:border-stone-700 dark:bg-stone-950"
						/>
						<p
							id="redeploy-commit-help"
							class={commitErr
								? 'text-red-700 dark:text-red-400'
								: 'text-stone-600 dark:text-stone-400'}
						>
							{commitErr ?? '7 to 40 characters, as shown on GitHub.'}
						</p>
					</div>
				{/if}
			</fieldset>

			{#if memoryErr}
				<p role="alert" class="text-xs text-red-700 dark:text-red-400">
					{memoryErr}
				</p>
			{:else if !memory}
				<p class="text-xs text-stone-600 dark:text-stone-400">
					Checking free memory...
				</p>
			{:else if memory.ok}
				<p class="text-xs text-stone-600 dark:text-stone-400">
					Free memory: {mib(memory.available_bytes)}; the new release starts
					next to the live one with up to {memory.memory_limit}.
				</p>
			{:else}
				<p
					role="alert"
					class="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
				>
					Not enough free memory: {mib(memory.available_bytes)} available, the new
					release needs {mib(memory.needed_bytes)} next to the live one. Free memory
					or lower the app's memory limit in Settings first.
				</p>
			{/if}

			{#if submitErr}
				<p role="alert" class="text-xs text-red-700 dark:text-red-400">
					{submitErr}
				</p>
			{/if}

			<div class="flex justify-end gap-2 pt-1 text-xs">
				<button
					type="button"
					onclick={() => (open = false)}
					class="rounded-md border border-stone-300 px-3 py-1.5 transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:hover:bg-stone-800"
				>
					Cancel
				</button>
				<button
					type="submit"
					disabled={busy || !memory || !memory.ok}
					class="rounded-lg bg-brand-primary px-4 py-1.5 font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:outline-none disabled:opacity-50"
				>
					{busy ? 'Queueing...' : 'Deploy'}
				</button>
			</div>
		</form>
	{/if}
</dialog>
