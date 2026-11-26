<script lang="ts">
	import '../app.css'
	import { page } from '$app/state'
	import { getSession, logout } from '#lib/api.js'
	import type { SessionInfo } from '#lib/types/api.js'
	import { onMount } from 'svelte'

	let { children } = $props()

	let session = $state<SessionInfo | null>(null)
	let loading = $state(true)
	let isDark = $state(false)

	let pathname = $derived(page.url.pathname)
	let isDashboardActive = $derived(pathname === '/')
	let isAppsActive = $derived(
		pathname === '/apps' || pathname.startsWith('/apps/')
	)
	let isActivityActive = $derived(
		pathname === '/activity' || pathname === '/audit-logs'
	)

	async function refreshSession() {
		try {
			session = await getSession()
			if (session.setup_required && page.url.pathname !== '/setup') {
				window.location.href = '/setup'
				return
			}
		} catch {
			session = null
		} finally {
			loading = false
		}
	}

	function toggleTheme() {
		isDark = !isDark
		if (isDark) {
			document.documentElement.classList.add('dark')
			localStorage.setItem('theme', 'dark')
		} else {
			document.documentElement.classList.remove('dark')
			localStorage.setItem('theme', 'light')
		}
	}

	async function handleLogout() {
		try {
			await logout()
			session = null
			window.location.href = '/login'
		} catch (err) {
			console.error('Logout error:', err)
		}
	}

	onMount(() => {
		isDark = document.documentElement.classList.contains('dark')

		const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
		const handleMediaChange = (e: MediaQueryListEvent) => {
			if (!localStorage.getItem('theme')) {
				isDark = e.matches
				if (e.matches) {
					document.documentElement.classList.add('dark')
				} else {
					document.documentElement.classList.remove('dark')
				}
			}
		}
		mediaQuery.addEventListener('change', handleMediaChange)

		refreshSession()

		return () => mediaQuery.removeEventListener('change', handleMediaChange)
	})
</script>

{#snippet themeIcon()}
	{#if isDark}
		<svg
			class="size-4 shrink-0"
			fill="none"
			stroke="currentColor"
			viewBox="0 0 24 24"
			aria-hidden="true"
		>
			<path
				stroke-linecap="round"
				stroke-linejoin="round"
				stroke-width="2"
				d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z"
			/>
		</svg>
	{:else}
		<svg
			class="size-4 shrink-0"
			fill="none"
			stroke="currentColor"
			viewBox="0 0 24 24"
			aria-hidden="true"
		>
			<path
				stroke-linecap="round"
				stroke-linejoin="round"
				stroke-width="2"
				d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"
			/>
		</svg>
	{/if}
{/snippet}

<div
	class="flex min-h-screen flex-col {session?.authenticated
		? 'md:flex-row'
		: ''} bg-stone-50 font-sans text-stone-900 dark:bg-stone-950 dark:text-stone-100"
>
	{#if session?.authenticated}
		<!-- Desktop persistent left sidebar (no top utility bar on md+) -->
		<aside
			aria-label="Primary sidebar"
			class="hidden border-r border-stone-200 bg-white md:sticky md:top-0 md:flex md:h-screen md:w-60 md:shrink-0 md:flex-col dark:border-stone-800 dark:bg-stone-900"
		>
			<div
				class="flex h-16 items-center border-b border-stone-200 px-4 dark:border-stone-800"
			>
				<a
					href="/"
					class="flex items-center gap-2.5 rounded-md text-lg font-semibold tracking-tight transition hover:opacity-80 focus:ring-2 focus:ring-brand-ring focus:outline-none"
				>
					<img src="/logo.svg" alt="dermaga" class="size-7 object-contain" />
					<span>dermaga</span>
				</a>
			</div>

			<nav
				aria-label="Main navigation"
				class="flex-1 space-y-1 overflow-y-auto px-3 py-4 text-sm font-medium"
			>
				<a
					href="/"
					class="flex items-center gap-2.5 rounded-lg px-3 py-2 transition focus:ring-2 focus:ring-brand-ring focus:outline-none {isDashboardActive
						? 'border border-brand-badge-border bg-brand-badge-bg text-brand-badge-text'
						: 'border border-transparent text-stone-700 hover:bg-stone-100 dark:text-stone-300 dark:hover:bg-stone-800'}"
				>
					<svg
						class="size-4 shrink-0"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
						aria-hidden="true"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z"
						/>
					</svg>
					<span>Dashboard</span>
				</a>
				<a
					href="/apps"
					class="flex items-center gap-2.5 rounded-lg px-3 py-2 transition focus:ring-2 focus:ring-brand-ring focus:outline-none {isAppsActive
						? 'border border-brand-badge-border bg-brand-badge-bg text-brand-badge-text'
						: 'border border-transparent text-stone-700 hover:bg-stone-100 dark:text-stone-300 dark:hover:bg-stone-800'}"
				>
					<svg
						class="size-4 shrink-0"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
						aria-hidden="true"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"
						/>
					</svg>
					<span>Apps</span>
				</a>
				<a
					href="/activity"
					class="flex items-center gap-2.5 rounded-lg px-3 py-2 transition focus:ring-2 focus:ring-brand-ring focus:outline-none {isActivityActive
						? 'border border-brand-badge-border bg-brand-badge-bg text-brand-badge-text'
						: 'border border-transparent text-stone-700 hover:bg-stone-100 dark:text-stone-300 dark:hover:bg-stone-800'}"
				>
					<svg
						class="size-4 shrink-0"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
						aria-hidden="true"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
						/>
					</svg>
					<span>Activity</span>
				</a>
				<a
					href="/settings"
					class="flex items-center gap-2.5 rounded-lg px-3 py-2 transition focus:ring-2 focus:ring-brand-ring focus:outline-none {pathname.startsWith(
						'/settings'
					)
						? 'border border-brand-badge-border bg-brand-badge-bg text-brand-badge-text'
						: 'border border-transparent text-stone-700 hover:bg-stone-100 dark:text-stone-300 dark:hover:bg-stone-800'}"
				>
					<svg
						class="size-4 shrink-0"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
						aria-hidden="true"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
						/>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
						/>
					</svg>
					<span>Settings</span>
				</a>
			</nav>

			<!-- Bottom pinned controls -->
			<div
				class="mt-auto space-y-1 border-t border-stone-200 p-3 text-xs font-medium dark:border-stone-800"
			>
				<button
					type="button"
					onclick={toggleTheme}
					aria-label="{isDark ? 'Light mode' : 'Dark mode'} (Toggle theme)"
					class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-stone-700 transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:text-stone-300 dark:hover:bg-stone-800"
				>
					{@render themeIcon()}
					<span>{isDark ? 'Light mode' : 'Dark mode'}</span>
				</button>
				<button
					type="button"
					onclick={handleLogout}
					class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-stone-700 transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:text-stone-300 dark:hover:bg-stone-800"
				>
					<svg
						class="size-4 shrink-0"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
						aria-hidden="true"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1"
						/>
					</svg>
					<span>Log out</span>
				</button>
			</div>
		</aside>

		<!-- Narrow screen: logo, theme toggle and log out on top; page links below -->
		<header
			class="sticky top-0 z-40 border-b border-stone-200 bg-white md:hidden dark:border-stone-800 dark:bg-stone-900"
		>
			<div class="flex h-12 items-center justify-between px-4">
				<a
					href="/"
					class="flex items-center gap-2 text-base font-semibold tracking-tight"
				>
					<img src="/logo.svg" alt="dermaga" class="size-6 object-contain" />
					<span>dermaga</span>
				</a>
				<div class="flex shrink-0 items-center gap-1.5 text-xs font-medium">
					<button
						type="button"
						onclick={toggleTheme}
						aria-label="Toggle theme"
						class="rounded-md p-1.5 text-stone-600 transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:text-stone-400 dark:hover:bg-stone-800"
					>
						{@render themeIcon()}
					</button>
					<button
						type="button"
						onclick={handleLogout}
						class="rounded-md border border-stone-200 px-2.5 py-1.5 text-stone-700 transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-700 dark:text-stone-300 dark:hover:bg-stone-800"
					>
						Log out
					</button>
				</div>
			</div>
			<nav
				aria-label="Mobile navigation"
				class="flex items-center gap-2 overflow-x-auto border-t border-stone-200 px-3 py-2 text-xs font-medium dark:border-stone-800"
			>
				<div class="flex shrink-0 items-center gap-1">
					<a
						href="/"
						class="rounded-md px-2.5 py-1.5 transition focus:ring-2 focus:ring-brand-ring focus:outline-none {isDashboardActive
							? 'border border-brand-badge-border bg-brand-badge-bg text-brand-badge-text'
							: 'text-stone-600 dark:text-stone-400'}"
					>
						Dashboard
					</a>
					<a
						href="/apps"
						class="rounded-md px-2.5 py-1.5 transition focus:ring-2 focus:ring-brand-ring focus:outline-none {isAppsActive
							? 'border border-brand-badge-border bg-brand-badge-bg text-brand-badge-text'
							: 'text-stone-600 dark:text-stone-400'}"
					>
						Apps
					</a>
					<a
						href="/activity"
						class="rounded-md px-2.5 py-1.5 transition focus:ring-2 focus:ring-brand-ring focus:outline-none {isActivityActive
							? 'border border-brand-badge-border bg-brand-badge-bg text-brand-badge-text'
							: 'text-stone-600 dark:text-stone-400'}"
					>
						Activity
					</a>
					<a
						href="/settings"
						class="rounded-md px-2.5 py-1.5 transition focus:ring-2 focus:ring-brand-ring focus:outline-none {pathname.startsWith(
							'/settings'
						)
							? 'border border-brand-badge-border bg-brand-badge-bg text-brand-badge-text'
							: 'text-stone-600 dark:text-stone-400'}"
					>
						Settings
					</a>
				</div>
			</nav>
		</header>
	{:else}
		<!-- Minimal header for unauthenticated / setup views -->
		<header
			class="sticky top-0 z-40 border-b border-stone-200 bg-white dark:border-stone-800 dark:bg-stone-900"
		>
			<div
				class="mx-auto flex h-14 max-w-5xl items-center justify-between px-4 md:px-6"
			>
				<a
					href="/"
					class="flex items-center gap-2.5 text-lg font-semibold tracking-tight transition hover:opacity-80"
				>
					<img src="/logo.svg" alt="dermaga" class="size-7 object-contain" />
					<span>dermaga</span>
				</a>
				<button
					type="button"
					onclick={toggleTheme}
					aria-label="Toggle theme"
					class="rounded-md p-2 text-stone-600 transition hover:bg-stone-100 focus:ring-2 focus:ring-brand-ring focus:outline-none dark:text-stone-400 dark:hover:bg-stone-800"
				>
					{@render themeIcon()}
				</button>
			</div>
		</header>
	{/if}

	<div class="flex min-w-0 flex-1 flex-col">
		<main class="w-full flex-1 px-4 py-6 md:px-6 lg:px-8">
			{#if loading}
				<div
					class="flex h-64 items-center justify-center text-sm text-stone-500 dark:text-stone-400"
				>
					Loading dermaga...
				</div>
			{:else}
				{#if session?.authenticated}
					<div class="mb-6 space-y-3">
						{#if !session.totp_enabled}
							<div
								role="alert"
								class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-300 bg-amber-50 p-3.5 text-xs font-medium text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
							>
								<span>
									Two-factor authentication is disabled. Enable TOTP in Settings
									to protect your server.
								</span>
								<a
									href="/settings?tab=security"
									class="font-semibold underline hover:no-underline"
								>
									Configure TOTP
								</a>
							</div>
						{/if}
						{#if session.setup_state === 'admin_created'}
							<div
								role="status"
								class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-stone-300 bg-stone-100 p-3.5 text-xs text-stone-800 dark:border-stone-700 dark:bg-stone-900 dark:text-stone-200"
							>
								<span>
									Setup incomplete: hook domain is not configured yet (required
									before connecting GitHub).
								</span>
								<a
									href="/settings?tab=hook-domain"
									class="font-semibold text-brand-text hover:underline"
								>
									Configure hook domain
								</a>
							</div>
						{/if}
					</div>
				{/if}
				{@render children()}
			{/if}
		</main>
	</div>
</div>
