<script lang="ts">
	import { page } from '$app/state';

	let { open = false, onclose }: { open?: boolean; onclose?: () => void } = $props();

	const currentPath = $derived(page.url.pathname);

	const links = [
		{
			href: '/',
			label: 'Dashboard',
			exact: true,
			icon: 'M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6'
		},
		{
			href: '/apps/new',
			label: 'New App',
			icon: 'M12 4v16m8-8H4'
		},
		{
			href: '/databases',
			label: 'Databases',
			icon: 'M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4'
		},
		{
			href: '/backups',
			label: 'Backups',
			icon: 'M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4'
		},
		{
			href: '/settings',
			label: 'Settings',
			icon: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z'
		},
		{
			href: '/audit',
			label: 'Audit Log',
			icon: 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z'
		}
	];

	function isActive(href: string, exact?: boolean): boolean {
		if (exact) return currentPath === href;
		return currentPath.startsWith(href);
	}
</script>

{#if open}
	<button
		type="button"
		class="fixed inset-0 z-40 bg-zinc-950/50 backdrop-blur-xs md:hidden border-none"
		onclick={() => onclose?.()}
		aria-label="Close sidebar backdrop"
	></button>
{/if}

<aside
	class="fixed md:sticky top-16 z-40 md:z-0 h-[calc(100vh-4rem)] w-64 shrink-0 border-r border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900/60 p-4 transition-transform duration-200 ease-in-out md:translate-x-0 {open
		? 'translate-x-0'
		: '-translate-x-full'}"
>
	<nav class="space-y-1">
		{#each links as link}
			{@const active = isActive(link.href, link.exact)}
			<a
				href={link.href}
				onclick={() => onclose?.()}
				class="flex items-center space-x-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors
				{active
					? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-950/60 dark:text-indigo-300 font-semibold shadow-xs'
					: 'text-zinc-600 dark:text-zinc-400 hover:bg-zinc-100 dark:hover:bg-zinc-800/80 hover:text-zinc-900 dark:hover:text-zinc-100'}"
			>
				<svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d={link.icon} />
				</svg>
				<span>{link.label}</span>
			</a>
		{/each}
	</nav>

	<div
		class="mt-8 pt-4 border-t border-zinc-200 dark:border-zinc-800 text-xs text-zinc-400 dark:text-zinc-500 px-3 space-y-1"
	>
		<p class="font-medium text-zinc-500 dark:text-zinc-400">Security Notice</p>
		<p class="leading-relaxed">
			Panel reachable only via SSH tunnel to 127.0.0.1:7070. Never exposed to public internet.
		</p>
	</div>
</aside>
