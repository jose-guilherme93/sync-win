<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import EmptyState from '../ui/EmptyState.svelte'
  import {
    PAGE_SIZE,
    TIME_RANGES,
    UNKNOWN_SOURCE,
    buildQuery,
    displayTime,
    fullTimestamp,
    matchesFilter,
    sinceFor,
    toClipboardText,
    type DeviceLogEntry,
    type DeviceLogPage,
    type LevelFilter,
    type TimeRange
  } from '../../lib/device-logs'

  export let deviceId: string
  export let authHeaders: Record<string, string> = {}

  let entries: DeviceLogEntry[] = []
  let total = 0
  let counts: Record<string, number> = { error: 0, warn: 0, info: 0 }
  let sources: string[] = []

  let level: LevelFilter = 'all'
  let source = ''
  let search = ''
  let range: TimeRange = 'all'
  let offset = 0

  let loading = true
  let loadingMore = false
  let loaded = false
  let error = ''

  let follow = true
  let wrap = false
  let copied = false
  let copiedTimer: ReturnType<typeof setTimeout> | null = null
  let searchTimer: ReturnType<typeof setTimeout> | null = null

  // The agent uploads a fresh journal sample roughly every 60s, so polling at
  // that cadence keeps the view current without hammering the API. Slower than
  // the shell's device poll because this screen is rarely left open for long.
  const REFRESH_MS = 60_000
  let refreshTimer: ReturnType<typeof setInterval> | null = null

  // Guards against a slow first request overwriting the result of a later one.
  let requestSeq = 0

  let viewport: HTMLDivElement | null = null

  $: levelCounts = {
    all: total,
    error: counts.error || 0,
    warn: counts.warn || 0,
    info: counts.info || 0
  }

  $: hasMore = offset + entries.length < total
  $: filtered = entries.length > 0 && entries.filter((e) => matchesFilter(e, level, source, search)).length

  async function fetchPage(nextOffset: number, append: boolean) {
    if (!deviceId) return
    if (append) loadingMore = true
    else loading = true
    error = ''

    const seq = ++requestSeq
    try {
      const params = buildQuery({
        level,
        source,
        search,
        since: sinceFor(range),
        limit: PAGE_SIZE,
        offset: nextOffset
      })
      const response = await apiFetch(apiURL(`/api/devices/${deviceId}/logs?${params}`), { headers: authHeaders })
      if (!response.ok) throw new Error(`Could not load device logs (${response.status})`)
      const payload: DeviceLogPage = await response.json()
      // A filter change while this request was in flight invalidates it.
      if (seq !== requestSeq) return

      const incoming = Array.isArray(payload.entries) ? payload.entries : []
      entries = append ? dedupe([...entries, ...incoming]) : incoming
      total = payload.total ?? entries.length
      counts = payload.counts || { error: 0, warn: 0, info: 0 }
      sources = Array.isArray(payload.sources) ? payload.sources : []
      offset = nextOffset
      if (append) requestAnimationFrame(scrollToEnd)
    } catch (e) {
      if (seq !== requestSeq) return
      error = e instanceof Error ? e.message : 'Could not load device logs'
      if (!append) {
        entries = []
        total = 0
      }
    } finally {
      if (seq === requestSeq) {
        loading = false
        loadingMore = false
        loaded = true
      }
    }
  }

  // The server page is already ordered and unique, but a poll that lands while
  // the user has scrolled back can return a window overlapping what is loaded.
  function dedupe(list: DeviceLogEntry[]): DeviceLogEntry[] {
    const seen = new Set<number>()
    return list.filter((e) => {
      if (seen.has(e.id)) return false
      seen.add(e.id)
      return true
    })
  }

  function reload() {
    offset = 0
    void fetchPage(0, false)
  }

  function loadMore() {
    void fetchPage(entries.length, true)
  }

  function setLevel(next: LevelFilter) {
    if (level === next) return
    level = next
    reload()
  }

  function setRange(next: TimeRange) {
    if (range === next) return
    range = next
    reload()
  }

  function onSourceChange(event: Event) {
    const next = (event.currentTarget as HTMLSelectElement).value
    if (next === source) return
    source = next
    reload()
  }

  function onSearchInput() {
    if (searchTimer) clearTimeout(searchTimer)
    searchTimer = setTimeout(() => {
      searchTimer = null
      reload()
    }, 300)
  }

  function clearFilters() {
    level = 'all'
    source = ''
    search = ''
    range = 'all'
    reload()
  }

  function scrollToEnd() {
    if (viewport) viewport.scrollTop = viewport.scrollHeight
  }

  // Autoscroll follows new lines only while the user is already at the bottom,
  // so scrolling up to read something is not yanked away by the next poll.
  function onScroll() {
    if (!viewport) return
    const distance = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight
    follow = distance < 40
  }

  async function copyAll() {
    try {
      await navigator.clipboard.writeText(toClipboardText(entries))
      copied = true
      if (copiedTimer) clearTimeout(copiedTimer)
      copiedTimer = setTimeout(() => {
        copiedTimer = null
        copied = false
      }, 1600)
    } catch {
      copied = false
    }
  }

  function sourceOf(entry: DeviceLogEntry): string {
    return entry.source || UNKNOWN_SOURCE
  }

  function rowClass(entry: DeviceLogEntry): string {
    const parts = [`level-${entry.level}`]
    // Dim rows the active filters would exclude rather than hiding them, so the
    // surrounding context of a matching line stays readable.
    if (level !== 'all' || source || search.trim()) {
      if (!matchesFilter(entry, level, source, search)) parts.push('muted-row')
    }
    return parts.join(' ')
  }

  $: if (deviceId) {
    level = 'all'
    source = ''
    search = ''
    range = 'all'
    offset = 0
    loaded = false
    follow = true
    void fetchPage(0, false)
    if (refreshTimer) clearInterval(refreshTimer)
    refreshTimer = setInterval(() => void fetchPage(0, false), REFRESH_MS)
  }

  onMount(() => {
    const onKey = (event: KeyboardEvent) => {
      // Ignore keys aimed at the search box.
      const target = event.target as HTMLElement | null
      const typing = target && (target.tagName === 'INPUT' || target.tagName === 'SELECT')
      if (typing) return
      if (event.key === 'f') {
        event.preventDefault()
        document.getElementById('device-log-search')?.focus()
      } else if (event.key === 'r') {
        event.preventDefault()
        reload()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  onDestroy(() => {
    if (refreshTimer) clearInterval(refreshTimer)
    if (searchTimer) clearTimeout(searchTimer)
    if (copiedTimer) clearTimeout(copiedTimer)
  })
</script>

<section class="logs-screen">
  <article class="card">
    <header class="card-head">
      <div class="titles">
        <h2>Device logs</h2>
        <p class="sub">
          {#if loading && !loaded}
            Loading…
          {:else if total === 0}
            No entries stored yet
          {:else}
            {total} stored {total === 1 ? 'entry' : 'entries'}
            {#if entries.length < total}· showing {entries.length}{/if}
          {/if}
        </p>
      </div>

      <div class="tools">
        <input
          id="device-log-search"
          type="search"
          bind:value={search}
          on:input={onSearchInput}
          placeholder="Search messages…"
          aria-label="Search log messages"
        />

        {#if sources.length > 0}
          <select aria-label="Filter by source" value={source} on:change={onSourceChange}>
            <option value="">All sources ({sources.length})</option>
            {#each sources as name (name)}
              <option value={name}>{name}</option>
            {/each}
          </select>
        {/if}

        <div class="ranges" role="group" aria-label="Time range">
          <button class:active={range === 'all'} on:click={() => setRange('all')}>All</button>
          {#each TIME_RANGES as r (r.id)}
            <button class:active={range === r.id} on:click={() => setRange(r.id)}>{r.label}</button>
          {/each}
        </div>

        <button class="ghost" on:click={copyAll} disabled={entries.length === 0} title="Copy the loaded lines">
          {copied ? 'Copied' : 'Copy'}
        </button>
        <button class="ghost" on:click={reload} disabled={loading} title="Refresh (r)">
          {loading && loaded ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>
    </header>

    <div class="levels" role="group" aria-label="Filter by severity">
      {#each ['all', 'error', 'warn', 'info'] as const as id (id)}
        <button class="chip chip-{id}" class:active={level === id} on:click={() => setLevel(id)}>
          <span class="dot" aria-hidden="true"></span>
          {id}
          <span class="num">{levelCounts[id]}</span>
        </button>
      {/each}
      <span class="spacer"></span>
      <label class="toggle">
        <input type="checkbox" bind:checked={wrap} />
        Wrap
      </label>
      <label class="toggle">
        <input type="checkbox" bind:checked={follow} />
        Follow
      </label>
    </div>

    {#if error}
      <div class="error-banner" role="alert">
        {error}
        <button class="retry" on:click={reload}>Retry</button>
      </div>
    {/if}

    {#if loading && !loaded}
      <p class="loading">Loading device logs…</p>
    {:else if error && entries.length === 0}
      <EmptyState
        icon="⚠"
        title="Could not load device logs"
        message={error}
        actionLabel="Retry"
        onAction={reload}
      />
    {:else if !loaded}
      <EmptyState
        icon="📄"
        title="Not collected yet"
        message="This device has not uploaded a journal sample yet. The agent samples the system journal about once a minute."
        actionLabel="Refresh"
        onAction={reload}
      />
    {:else if entries.length === 0 && level === 'all' && !source && !search.trim() && range === 'all'}
      <EmptyState
        icon="📄"
        title="No device logs stored"
        message="The agent has not reported any journal lines for this device. Lines are kept for a week; older ones are cleaned up automatically."
        actionLabel="Refresh"
        onAction={reload}
      />
    {:else if entries.length === 0}
      <EmptyState
        icon="🔍"
        title="No matching entries"
        message="No stored line matches the current filters. Try a wider time range or clear the filters."
        actionLabel="Clear filters"
        onAction={clearFilters}
      />
    {:else}
      <div
        class="viewport"
        bind:this={viewport}
        on:scroll={onScroll}
        role="log"
        aria-label="Device log entries"
        aria-live="polite"
      >
        <ol class="rows" class:wrap>
          {#each entries as entry (entry.id)}
            <li class={rowClass(entry)}>
              <span class="time mono" title={fullTimestamp(entry.ts)}>{displayTime(entry.ts)}</span>
              <span class="level">{entry.level}</span>
              <span class="source mono" title={sourceOf(entry)}>{sourceOf(entry)}</span>
              <span class="message">{entry.message}</span>
            </li>
          {/each}
        </ol>
      </div>

      <footer class="foot">
        <span class="status">
          {#if loading}
            Refreshing…
          {:else if hasMore}
            Showing the newest {entries.length} of {total}
          {:else}
            All {entries.length} {entries.length === 1 ? 'entry' : 'entries'} loaded
          {/if}
          {#if filtered && filtered < entries.length}
            · {filtered} match the filters
          {/if}
        </span>
        <span class="actions">
          {#if hasMore}
            <button class="ghost" on:click={loadMore} disabled={loadingMore}>
              {loadingMore ? 'Loading…' : `Load ${Math.min(PAGE_SIZE, total - entries.length)} older`}
            </button>
          {/if}
          {#if !follow}
            <button class="ghost" on:click={() => { follow = true; scrollToEnd() }}>Jump to latest</button>
          {/if}
        </span>
      </footer>
    {/if}
  </article>
</section>

<style>
  .logs-screen { display: grid; gap: 0.85rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }

  .card-head {
    display: flex; align-items: flex-start; justify-content: space-between;
    gap: 0.75rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border);
    flex-wrap: wrap;
  }
  .titles h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .sub { margin: 0.15rem 0 0; font-size: 0.72rem; color: var(--text-faint); }

  .tools { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
  .tools input[type='search'], .tools select {
    padding: 0.35rem 0.55rem; background: var(--card-inset); border: 1px solid var(--border);
    border-radius: var(--radius-sm); color: var(--text); font: inherit; font-size: 0.75rem;
  }
  .tools input[type='search'] { min-width: 12rem; }
  .tools input[type='search']:focus, .tools select:focus { outline: none; border-color: var(--accent-border); }

  .ranges { display: flex; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
  .ranges button {
    padding: 0.3rem 0.45rem; background: transparent; border: 0; border-right: 1px solid var(--border);
    color: var(--text-faint); font: inherit; font-size: 0.71rem; cursor: pointer;
  }
  .ranges button:last-child { border-right: 0; }
  .ranges button:hover { color: var(--text); }
  .ranges button.active { background: var(--accent-dim); color: var(--accent); }

  .ghost {
    padding: 0.3rem 0.55rem; background: transparent; border: 1px solid var(--border);
    border-radius: var(--radius-sm); color: var(--text-muted); font: inherit;
    font-size: 0.72rem; cursor: pointer;
  }
  .ghost:hover:not(:disabled) { border-color: var(--accent-border); color: var(--text); }
  .ghost:disabled { opacity: 0.5; cursor: default; }

  .levels {
    display: flex; align-items: center; gap: 0.4rem; padding: 0.5rem 0.9rem;
    border-bottom: 1px solid var(--border); flex-wrap: wrap;
  }
  .chip {
    display: inline-flex; align-items: center; gap: 0.3rem;
    padding: 0.22rem 0.5rem; background: transparent; border: 1px solid var(--border);
    border-radius: 999px; color: var(--text-muted); font: inherit; font-size: 0.71rem;
    cursor: pointer; text-transform: capitalize;
  }
  .chip:hover { border-color: var(--accent-border); }
  .chip.active { background: var(--accent-dim); border-color: var(--accent-border); color: var(--text); }
  .chip .dot { width: 0.45rem; height: 0.45rem; border-radius: 50%; background: var(--text-faint); }
  .chip-error .dot { background: var(--crit); }
  .chip-warn .dot { background: var(--warn); }
  .chip-info .dot { background: var(--accent); }
  .chip .num { color: var(--text-faint); font-variant-numeric: tabular-nums; }
  .chip.active .num { color: var(--text-muted); }
  .spacer { flex: 1; }
  .toggle { display: flex; align-items: center; gap: 0.25rem; color: var(--text-faint); font-size: 0.71rem; cursor: pointer; }

  .error-banner {
    display: flex; align-items: center; gap: 0.6rem; padding: 0.5rem 0.75rem;
    border-bottom: 1px solid var(--border);
    background: var(--crit-dim); color: var(--crit); font-size: 0.76rem;
  }
  .retry {
    margin-left: auto; padding: 0.2rem 0.5rem; background: transparent;
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    color: var(--text); font: inherit; font-size: 0.72rem; cursor: pointer;
  }
  .retry:hover { border-color: var(--accent-border); }

  .loading { margin: 0; padding: 0.9rem; color: var(--text-faint); font-size: 0.78rem; }

  .viewport { max-height: 60vh; overflow-y: auto; overflow-x: hidden; }
  .rows { list-style: none; margin: 0; padding: 0; font-size: 0.76rem; }

  .rows li {
    display: grid;
    grid-template-columns: 6.5rem 3.5rem minmax(6rem, 10rem) minmax(0, 1fr);
    gap: 0.6rem; padding: 0.3rem 0.9rem; border-bottom: 1px solid var(--border);
    align-items: baseline;
  }
  .rows li:last-child { border-bottom: 0; }
  .time { color: var(--text-faint); font-size: 0.71rem; white-space: nowrap; }
  .level { font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.03em; color: var(--text-faint); }
  .source {
    color: var(--text-muted); font-size: 0.71rem;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .message { color: var(--text); word-break: break-word; }

  .level-error .level { color: var(--crit); }
  .level-warn .level { color: var(--warn); }
  .level-info .level { color: var(--text-faint); }
  .level-error { background: var(--crit-dim); }

  .rows:not(.wrap) .message { white-space: pre; overflow-x: hidden; text-overflow: ellipsis; }

  .muted-row { opacity: 0.38; }

  .foot {
    display: flex; align-items: center; justify-content: space-between; gap: 0.6rem;
    padding: 0.5rem 0.9rem; border-top: 1px solid var(--border); flex-wrap: wrap;
  }
  .status { color: var(--text-faint); font-size: 0.72rem; }
  .actions { display: flex; align-items: center; gap: 0.45rem; }

  @media (max-width: 800px) {
    .viewport { max-height: none; }
    .rows li { grid-template-columns: 5.5rem 3rem minmax(0, 1fr); }
    .source { display: none; }
    .tools input[type='search'] { min-width: 0; width: 100%; }
  }
</style>