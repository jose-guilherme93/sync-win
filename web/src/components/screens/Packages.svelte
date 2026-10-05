<script lang="ts">
  import type { AppInfo } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'
  import Skeleton from '../ui/Skeleton.svelte'

  export let apps: AppInfo[] = []
  export let loading = false

  let query = ''
  let source = 'all'

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

    {#if loading}
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
    <header class="card-head"><h2>Pending updates</h2></header>
    <EmptyState
      icon="⬆"
      title="Not collected yet"
      message="Checking for available updates requires running the package manager, which the agent does not do. A GET /api/devices/&#123;id&#125;/updates endpoint is needed before this list can be filled."
    />
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
</style>
