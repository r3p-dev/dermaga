<script lang="ts">
	let {
		label,
		pulse = false
	}: {
		label: string
		pulse?: boolean
	} = $props()

	let tone = $derived.by(() => {
		if (
			label === 'live' ||
			label === 'running' ||
			label === 'healthy' ||
			label === 'connected' ||
			label === 'enabled' ||
			label.startsWith('Daemon Active')
		) {
			return {
				badge:
					'bg-emerald-50 dark:bg-emerald-950/50 text-emerald-800 dark:text-emerald-300 border-emerald-200 dark:border-emerald-800',
				dot: 'bg-emerald-600 dark:bg-emerald-400'
			}
		}
		switch (label) {
			case 'failed':
			case 'error':
			case 'disconnected':
				return {
					badge:
						'bg-red-50 dark:bg-red-950/50 text-red-800 dark:text-red-300 border-red-200 dark:border-red-800',
					dot: 'bg-red-600 dark:bg-red-400'
				}
			case 'cancelled':
			case 'inactive':
			case 'replaced by a newer deploy':
				return {
					badge:
						'bg-stone-100 dark:bg-stone-800 text-stone-700 dark:text-stone-300 border-stone-300 dark:border-stone-700',
					dot: 'bg-stone-500 dark:bg-stone-400'
				}
			default:
				return {
					badge:
						'bg-amber-50 dark:bg-amber-950/50 text-amber-900 dark:text-amber-300 border-amber-200 dark:border-amber-800',
					dot: 'bg-amber-600 dark:bg-amber-400'
				}
		}
	})
</script>

<span
	class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[11px] font-medium {tone.badge}"
>
	<span
		class="size-1.5 rounded-full {tone.dot} {pulse ? 'animate-pulse' : ''}"
		aria-hidden="true"
	></span>
	<span>{label}</span>
</span>
