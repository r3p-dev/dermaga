<script lang="ts">
	import Modal from './Modal.svelte';

	let { open = $bindable(false) }: { open: boolean } = $props();

	const shortcuts = [
		{ keys: ['?'], description: 'Open keyboard shortcuts' },
		{ keys: ['g', 'h'], description: 'Go to Dashboard' },
		{ keys: ['g', 'a'], description: 'Go to Apps list' },
		{ keys: ['g', 'd'], description: 'Go to Databases' },
		{ keys: ['g', 'b'], description: 'Go to Backups' },
		{ keys: ['g', 's'], description: 'Go to Settings' },
		{ keys: ['g', 'l'], description: 'Go to Audit Log' },
		{ keys: ['n', 'a'], description: 'Create New App' },
		{ keys: ['n', 'd'], description: 'Create New Database' },
		{ keys: ['t'], description: 'Toggle Dark / Light theme' },
		{ keys: ['Esc'], description: 'Close modal / dropdown' }
	];
</script>

<Modal {open} title="Keyboard Navigation Shortcuts" onclose={() => (open = false)}>
	<div class="space-y-3">
		<p class="text-xs text-zinc-500 dark:text-zinc-400">
			Press key sequences in quick succession to navigate anywhere in the dashboard without touching your mouse.
		</p>
		<div class="divide-y divide-zinc-100 dark:divide-zinc-800">
			{#each shortcuts as s}
				<div class="flex items-center justify-between py-2 text-sm">
					<span class="text-zinc-700 dark:text-zinc-300">{s.description}</span>
					<div class="flex items-center space-x-1">
						{#each s.keys as k}
							<kbd
								class="px-2 py-1 text-xs font-semibold text-zinc-800 dark:text-zinc-200 bg-zinc-100 dark:bg-zinc-800 border border-zinc-300 dark:border-zinc-700 rounded shadow-xs"
							>
								{k}
							</kbd>
						{/each}
					</div>
				</div>
			{/each}
		</div>
	</div>
</Modal>
