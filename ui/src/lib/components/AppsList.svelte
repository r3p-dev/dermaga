<script lang="ts">
  import type { AppSummary } from "#lib/types/api.js";
  import StatusBadge from "./StatusBadge.svelte";

  let {
    apps,
    loading = false,
    error = null,
  }: {
    apps: AppSummary[];
    loading?: boolean;
    error?: string | null;
  } = $props();

  function isInFlight(label?: string): boolean {
    return (
      label === "queued" ||
      label === "building" ||
      label === "starting" ||
      label === "healthy"
    );
  }
</script>

<section class="space-y-4">
  <div class="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
    <div>
      <h2
        class="text-base font-semibold tracking-tight text-stone-900 dark:text-stone-100"
      >
        Applications
      </h2>
      <p class="text-xs text-stone-600 dark:text-stone-400">
        Stateless apps deployed from your connected GitHub App
      </p>
    </div>
    <a
      href="/apps/new"
      class="inline-flex w-fit items-center justify-center gap-1.5 rounded-lg bg-brand-primary px-3.5 py-2 text-xs font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:outline-none"
    >
      New app
    </a>
  </div>

  {#if error}
    <div
      role="alert"
      class="rounded-xl border border-red-200 bg-red-50 p-4 text-xs text-red-800 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
    >
      {error}
    </div>
  {:else if loading}
    <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
      {#each [1, 2] as _}
        <div
          class="h-32 animate-pulse rounded-xl bg-stone-100 dark:bg-stone-800/50"
        ></div>
      {/each}
    </div>
  {:else if apps.length === 0}
    <div
      class="space-y-3 rounded-xl border border-dashed border-stone-300 bg-white p-8 text-center dark:border-stone-700 dark:bg-stone-900"
    >
      <div
        class="mx-auto flex size-10 items-center justify-center rounded-lg bg-stone-100 text-stone-600 dark:bg-stone-800 dark:text-stone-400"
        aria-hidden="true"
      >
        <svg
          class="size-5"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"
          />
        </svg>
      </div>
      <p class="text-sm font-medium text-stone-900 dark:text-stone-100">
        No applications yet
      </p>
      <p class="mx-auto max-w-md text-xs text-stone-600 dark:text-stone-400">
        Deploy a repository with a Containerfile or Dockerfile using the guided
        wizard.
      </p>
      <a
        href="/apps/new"
        class="inline-flex items-center rounded-lg bg-brand-primary px-4 py-2 text-xs font-medium text-brand-btn-text shadow-sm transition hover:bg-brand-hover focus:ring-2 focus:ring-brand-ring focus:outline-none"
      >
        Create your first app
      </a>
    </div>
  {:else}
    <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
      {#each apps as app}
        <a
          href="/apps/{app.id}"
          class="group block space-y-3 rounded-xl border border-stone-200 bg-white p-5 shadow-sm transition hover:border-brand-primary/70 hover:shadow focus:ring-2 focus:ring-brand-ring focus:outline-none dark:border-stone-800 dark:bg-stone-900"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <h3
                class="truncate text-base font-semibold text-stone-900 transition group-hover:text-brand-text dark:text-stone-100"
              >
                {app.name}
              </h3>
              <p
                class="mt-0.5 truncate font-mono text-xs text-stone-600 dark:text-stone-400"
              >
                {app.repo_full_name} ({app.branch})
              </p>
            </div>
            {#if app.last_deployment}
              <div class="shrink-0">
                <StatusBadge
                  label={app.last_deployment.status_label}
                  pulse={isInFlight(app.last_deployment.status_label)}
                />
              </div>
            {/if}
          </div>
          <div
            class="flex flex-wrap items-center justify-between gap-2 border-t border-stone-100 pt-3 text-xs text-stone-600 dark:border-stone-800 dark:text-stone-400"
          >
            <span class="truncate font-mono">
              {app.primary_domain || "No domain"}
            </span>
            <div class="flex shrink-0 items-center gap-2">
              {#if app.last_deployment}
                <span class="font-mono tabular-nums">
                  r{app.last_deployment.release_number}
                </span>
              {/if}
              <span
                class="text-stone-400 transition group-hover:text-brand-text"
                aria-hidden="true"
              >
                &rarr;
              </span>
            </div>
          </div>
        </a>
      {/each}
    </div>
  {/if}
</section>
