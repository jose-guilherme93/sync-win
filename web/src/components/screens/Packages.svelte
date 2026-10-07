<script lang="ts">
  import { onMount } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import { formatRelative } from '../../lib/format'
  import type { AppInfo } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'
  import Skeleton from '../ui/Skeleton.svelte'

  export let deviceId: string
  export let apps: AppInfo[] = []
  export let authHeaders: Record<string, string> = {}

  type PendingUpdate = { source: string; name: string; current_version?: string; new_version: string }
  type UpdateReport = { status: string; checked_at?: string; message?: string; updates: PendingUpdate[] }

  let query = ''
  let source = 'all'
  let loading = false
  let error = ''
  let loaded = false

  let updateReport: UpdateReport | null = null
  let updatesLoading = false
  let updatesError = ''

  // The app inventory is not part of /api/devices or /api/devices/{id}/detail:
  // both omit it because it can be thousands of entries, and the summary carries
  // only app_count. The screen therefore fetches its own list, the same way the
  // Services screen does, rather than reading a field that is never populated.
  onMount(() => {
    void load()
    void loadUpdates()
  })

  async function load() {
    if (!deviceId) {
      loaded = true
      return
    }
    loading = true
    error = ''
    try {
      const res = await apiFetch(apiURL(`/api/devices/${deviceId}/apps`), { headers: authHeaders })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
      const payload = await res.json()
      apps = Array.isArray(payload) ? payload : []
    } catch (e: any) {
      error = e?.message || 'Failed to load the package inventory'
      apps = []
    } finally {
      loading = false
      loaded = true
    }
  }

  // Pending updates come from a read-only package-manager query the agent runs
  // on its own schedule. The three non-`ready` states are distinct on purpose:
  // "not reported yet" is not the same as "no supported package manager", which
  // is not the same as a failed check.
  async function loadUpdates() {
    if (!deviceId) return
    updatesLoading = true
    updatesError = ''
    try {
      const res = await apiFetch(apiURL(`/api/devices/${deviceId}/updates`), { headers: authHeaders })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
      const payload = await res.json()
      if (payload && typeof payload === 'object' && !Array.isArray(payload)) {
        if (!Array.isArray(payload.updates)) payload.updates = []
        updateReport = payload
      } else {
        updateReport = null
      }
    } catch (e: any) {
      updatesError = e?.message || 'Failed to load pending updates'
      updateReport = null
    } finally {
      updatesLoading = false
    }
  }

  $: sources = Array.from(new Set(apps.map((a) => a.source))).sort()

  $: filtered = apps.filter((app) => {
    if (source !== 'all' && app.source !== source) return false
    if (!query) return true
    const needle = query.toLowerCase()
    return `${app.name} ${app.version} ${app.path || ''}`.toLowerCase().includes(needle)
  })

  $: grouped = filtered.reduce<Record<string, AppInfo[]>>((acc, app) => {
    ;(acc[app.source] ||= []).push(app)
    return acc
  }, {})

  $: visible = Object.entries(grouped).sort(([a], [b]) => a.localeCompare(b))
</script>

{#if error}
  <div class="error-banner" role="alert">
    {error}
    <button class="retry" on:click={load}>Retry</button>
  </div>
{/if}

<section class="packages">
  <article class="card">
    <header class="card-head">
      <h2>Installed software</h2>
      <span class="muted">{apps.length} package{apps.length !== 1 ? 's' : ''}</span>
    </header>

    <div class="toolbar">
      <input type="search" bind:value={query} placeholder="Filter by name or version" aria-label="Filter packages" />
      <select bind:value={source} aria-label="Filter by source">
        <option value="all">All sources</option>
        {#each sources as s (s)}<option value={s}>{s}</option>{/each}
      </select>
    </div>

    {#if !loaded}
      <Skeleton variant="lines" rows={6} />
    {:else if apps.length === 0}
      <EmptyState
        icon="📦"
        title="No packages reported"
        message="The agent enumerates packages from a fixed set of sources (pacman, apt, flatpak, AUR, AppImage). A machine with none of those configured reports nothing here."
      />
    {:else if visible.length === 0}
      <EmptyState icon="🔍" title="No match" message="Adjust the filter to see packages." />
    {:else}
      <div class="groups">
        {#each visible as [group, items] (group)}
          <div class="group">
            <h3>{group}<span>{items.length}</span></h3>
            <ul>
              {#each items.slice(0, 300) as app (app.name + app.version + (app.path || ''))}
                <li>
                  <span class="name" title={app.name}>{app.name}</span>
                  <span class="version mono">{app.version}</span>
                </li>
              {/each}
            </ul>
            {#if items.length > 300}
              <p class="more">+{items.length - 300} more in this source</p>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </article>

  <article class="card">
    <header class="card-head">
      <h2>Pending updates</h2>
      {#if updateReport?.status === 'ready' && !updatesLoading}
        <span class="muted">{updateReport.updates.length} update{updateReport.updates.length !== 1 ? 's' : ''}</span>
      {/if}
    </header>

    {#if updatesLoading}
      <Skeleton variant="lines" rows={3} />
    {:else if updatesError}
      <div class="error-banner" role="alert">
        {updatesError}
        <button class="retry" on:click={loadUpdates}>Retry</button>
      </div>
    {:else if !updateReport || updateReport.status === 'not_reported'}
      <EmptyState
        icon="⬆"
        title="Not checked yet"
        message="The agent checks for available updates on its own schedule. Nothing has been reported for this device yet."
      />
    {:else if updateReport.status === 'unsupported'}
      <EmptyState
        icon="⬆"
        title="No supported package manager"
        message={updateReport.message || 'This device has none of the supported package managers (apt, flatpak, pacman, AUR).'}
      />
    {:else if updateReport.status === 'error'}
      <div class="error-banner" role="alert">Update check failed: {updateReport.message || 'unknown error'}</div>
    {:else if updateReport.updates.length === 0}
      <EmptyState icon="✅" title="Up to date" message="No pending package updates were found." />
    {:else}
      <ul class="updates">
        {#each updateReport.updates as update (update.source + update.name)}
          <li>
            <span class="source">{update.source}</span>
            <span class="name" title={update.name}>{update.name}</span>
            <span class="version mono">{update.current_version ? `${update.current_version} → ` : ''}{update.new_version}</span>
          </li>
        {/each}
      </ul>
      {#if updateReport.checked_at}<p class="checked">Checked {formatRelative(updateReport.checked_at)}</p>{/if}
    {/if}
  </article>
</section>

<style>
  .packages { display: grid; gap: 0.85rem; }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }

  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .muted { color: var(--text-faint); font-size: 0.74rem; }

  .toolbar { display: flex; gap: 0.5rem; padding: 0.7rem 0.9rem; }
  .toolbar input, .toolbar select {
    padding: 0.4rem 0.6rem; background: var(--card-inset); border: 1px solid var(--border);
    border-radius: var(--radius-sm); color: var(--text); font: inherit; font-size: 0.78rem;
  }
  .toolbar input { flex: 1; }
  .toolbar input:focus, .toolbar select:focus { outline: none; border-color: var(--accent-border); }

  .groups { max-height: 34rem; overflow-y: auto; }

  .group h3 {
    display: flex; align-items: center; justify-content: space-between;
    margin: 0; padding: 0.5rem 0.9rem; background: var(--card-inset);
    color: var(--text-muted); font-size: 0.68rem; font-weight: 700;
    letter-spacing: 0.07em; text-transform: uppercase;
    position: sticky; top: 0;
  }
  .group h3 span { color: var(--text-faint); font-weight: 400; }

  .group ul { list-style: none; margin: 0; padding: 0; }
  .group li {
    display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem;
    padding: 0.35rem 0.9rem; border-bottom: 1px solid var(--border); font-size: 0.78rem;
  }
  .name { color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .version { color: var(--text-faint); white-space: nowrap; }
  .more { margin: 0; padding: 0.4rem 0.9rem; color: var(--text-faint); font-size: 0.7rem; }

  .updates { list-style: none; margin: 0; padding: 0; max-height: 22rem; overflow-y: auto; }
  .updates li {
    display: grid; grid-template-columns: auto 1fr auto; gap: 0.6rem; align-items: baseline;
    padding: 0.35rem 0.9rem; border-bottom: 1px solid var(--border); font-size: 0.78rem;
  }
  .updates li:last-child { border-bottom: 0; }
  .source {
    color: var(--text-faint); font-size: 0.68rem; text-transform: uppercase;
    letter-spacing: 0.06em; min-width: 4.5rem;
  }
  .checked { margin: 0; padding: 0.4rem 0.9rem; color: var(--text-faint); font-size: 0.7rem; }
  .error-banner { display: flex; align-items: center; gap: 0.6rem; padding: 0.6rem 0.9rem; color: var(--crit); font-size: 0.78rem; }
  .retry {
    padding: 0.25rem 0.6rem; border: 1px solid var(--border-strong); border-radius: var(--radius-sm);
    background: transparent; color: var(--text); font-size: 0.72rem; cursor: pointer;
  }
  .retry:hover { background: var(--card-hover); }
</style>
