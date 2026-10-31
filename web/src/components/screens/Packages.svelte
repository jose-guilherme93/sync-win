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

  // The pending-updates card is driven by four distinct states the agent can
  // report, plus the request itself failing. Naming them once keeps the headline
  // stat and the card body from disagreeing, which they did when the card fell
  // through to "Not checked yet" while the chip said otherwise.
  $: pendingCount = updateReport?.status === 'ready' ? updateReport.updates.length : null
  $: updatesState = !updateReport
    ? 'not checked yet'
    : updateReport.status === 'ready'
      ? updateReport.updates.length === 0
        ? 'up to date'
        : 'updates available'
      : updateReport.status === 'unsupported'
        ? 'no supported package manager'
        : updateReport.status === 'error'
          ? 'check failed'
          : 'not checked yet'

  // Versions are absent for a whole source when the agent could not read them —
  // `apt-mark showmanual` prints names only, so older agents report none — so the
  // column states that instead of leaving a blank slot that reads as a bug.
  $: missingVersions = apps.filter((a) => !a.version).length

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
    <span>{error}</span>
    <button class="retry" on:click={load}>Retry</button>
  </div>
{/if}

<section class="packages">
  <div class="summary">
    <article class="stat">
      <span class="label">Installed</span>
      <span class="value">{apps.length}</span>
      <span class="sub">
        {sources.length} source{sources.length === 1 ? '' : 's'}{missingVersions > 0 ? ` · ${missingVersions} without a version` : ''}
      </span>
    </article>
    <article class="stat">
      <span class="label">Pending updates</span>
      <span class="value" class:warn={pendingCount !== null && pendingCount > 0}>{pendingCount ?? '—'}</span>
      <span class="sub">{updatesState}</span>
    </article>
  </div>

  <article class="card">
    <header class="card-head">
      <h2>Installed software</h2>
      <span class="muted">{filtered.length === apps.length ? `${apps.length} packages` : `${filtered.length} of ${apps.length}`}</span>
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
        icon="package"
        title="No packages reported"
        message="The agent enumerates packages from a fixed set of sources (pacman, apt, flatpak, AUR, AppImage). A machine with none of those configured reports nothing here."
      />
    {:else if visible.length === 0}
      <EmptyState icon="search" title="No match" message="Adjust the filter to see packages." />
    {:else}
      <div class="groups">
        {#each visible as [group, items] (group)}
          <div class="group">
            <h3>{group}<span>{items.length}</span></h3>
            <ul>
              {#each items.slice(0, 300) as app (app.name + app.version + (app.path || ''))}
                <li>
                  <span class="name" title={app.name}>{app.name}</span>
                  <span class="version mono" title={app.version ? `version ${app.version}` : 'The agent did not report a version for this package'}>
                    {app.version || '—'}
                  </span>
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
      <div class="inline-error" role="alert">
        <span>{updatesError}</span>
        <button class="retry" on:click={loadUpdates}>Retry</button>
      </div>
    {:else if !updateReport || updateReport.status === 'not_reported'}
      <EmptyState
        icon="upload"
        title="Not checked yet"
        message="The agent checks for available updates on its own schedule. Nothing has been reported for this device yet."
      />
    {:else if updateReport.status === 'unsupported'}
      <EmptyState
        icon="upload"
        title="No supported package manager"
        message={updateReport.message || 'This device has none of the supported package managers (apt, flatpak, pacman, AUR).'}
      />
    {:else if updateReport.status === 'error'}
      <div class="inline-error" role="alert">
        <span>Update check failed: {updateReport.message || 'unknown error'}</span>
      </div>
    {:else if updateReport.updates.length === 0}
      <EmptyState icon="check" title="Up to date" message="No pending package updates were found." />
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

  /* Same headline row the other device screens open with, so a screen that has
     counts to show shows them before the list does. */
  .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 0.6rem; }
  .stat {
    display: grid; gap: 0.25rem; align-content: start;
    padding: 0.8rem 0.9rem; background: var(--card);
    border: 1px solid var(--border); border-radius: var(--radius);
  }
  .stat .value { font-size: 1.45rem; }
  .stat .value.warn { color: var(--warn); }
  .stat .sub { color: var(--text-faint); font-size: 0.72rem; }

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

  .groups { max-height: 62vh; overflow-y: auto; }

  .group h3 {
    display: flex; align-items: center; justify-content: space-between;
    margin: 0; padding: 0.5rem 0.9rem; background: var(--card-inset);
    color: var(--text-muted); font-size: 0.68rem; font-weight: 700;
    letter-spacing: 0.07em; text-transform: uppercase;
    position: sticky; top: 0;
  }
  .group h3 span { color: var(--text-faint); font-weight: 400; }

  .group ul { list-style: none; margin: 0; padding: 0; }
  /* Name and version are two columns, not a spaced-out pair: the version has to
     line up down the list to be scannable, and a missing one has to hold its
     place instead of collapsing the row. */
  .group li {
    display: grid; grid-template-columns: minmax(0, 1fr) auto;
    gap: 0.75rem; align-items: baseline;
    padding: 0.35rem 0.9rem; border-bottom: 1px solid var(--border); font-size: 0.78rem;
  }
  .group li:last-child { border-bottom: 0; }
  .name { color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .version { color: var(--text-faint); white-space: nowrap; text-align: right; font-variant-numeric: tabular-nums; }
  .more { margin: 0; padding: 0.4rem 0.9rem; color: var(--text-faint); font-size: 0.7rem; }

  .updates { list-style: none; margin: 0; padding: 0; max-height: 22rem; overflow-y: auto; }
  .updates li {
    display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 0.6rem; align-items: baseline;
    padding: 0.35rem 0.9rem; border-bottom: 1px solid var(--border); font-size: 0.78rem;
  }
  .updates li:last-child { border-bottom: 0; }
  .updates .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .updates .version { text-align: right; }
  .source {
    color: var(--text-faint); font-size: 0.68rem; text-transform: uppercase;
    letter-spacing: 0.06em; min-width: 4.5rem;
  }
  .checked { margin: 0; padding: 0.4rem 0.9rem; color: var(--text-faint); font-size: 0.7rem; }

  /* A failure is a block with its own edges, like the empty states it sits
     beside, rather than a red strip bleeding across the card. */
  .error-banner, .inline-error {
    display: flex; align-items: flex-start; gap: 0.6rem;
    margin: 0.8rem; padding: 0.7rem 0.85rem;
    background: var(--crit-dim); border: 1px solid rgba(248, 113, 113, 0.35);
    border-radius: var(--radius-sm); color: var(--crit); font-size: 0.78rem;
    line-height: 1.45;
  }
  .error-banner { justify-content: space-between; align-items: center; }
  .error-banner span, .inline-error span { overflow-wrap: anywhere; }
  .retry {
    flex: none; padding: 0.25rem 0.6rem; border: 1px solid rgba(248, 113, 113, 0.45);
    border-radius: var(--radius-sm);
    background: transparent; color: var(--crit); font: inherit; font-size: 0.72rem; cursor: pointer;
  }
  .retry:hover { background: rgba(248, 113, 113, 0.16); }
</style>
