<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import EmptyState from '../ui/EmptyState.svelte'
  import type { Device } from '../../lib/types'

  export let devices: Device[] = []
  export let authHeaders: Record<string, string> = {}

  type LogEntry = {
    id: number
    timestamp: string
    level: string
    category: string
    event: string
    message?: string
    device_id?: string
    request_id?: string
    duration_ms?: number
    status?: number
    metadata?: Record<string, unknown>
    redacted?: boolean
  }

  type LogPage = { entries: LogEntry[]; total: number }

  type Settings = {
    level: string
    console_level: string
    retention_days: number
    levels: string[]
    categories: string[]
  }

  const PAGE_SIZE = 50
  const LIVE_MS = 5000
  const SEARCH_DEBOUNCE_MS = 350

  // Time windows the operator can pick. `ms: 0` means no lower bound, which is
  // what an unfiltered query wants.
  const RANGES: { id: string; label: string; ms: number }[] = [
    { id: 'all', label: 'All time', ms: 0 },
    { id: '15m', label: 'Last 15 min', ms: 15 * 60_000 },
    { id: '1h', label: 'Last hour', ms: 60 * 60_000 },
    { id: '6h', label: 'Last 6 hours', ms: 6 * 60 * 60_000 },
    { id: '24h', label: 'Last 24 hours', ms: 24 * 60 * 60_000 },
    { id: '7d', label: 'Last 7 days', ms: 7 * 24 * 60 * 60_000 }
  ]

  let settings: Settings | null = null
  let settingsError = ''
  let savingSettings = false
  let savedNote = ''

  let level = ''
  let category = ''
  let deviceId = ''
  let search = ''
  let range = 'all'

  let entries: LogEntry[] = []
  let total = 0
  let errors24h = 0
  let byLevel: Record<string, number> = {}
  let offset = 0

  let loading = true
  let loadingMore = false
  let error = ''
  let expanded: number | null = null

  let live = false
  let liveTimer: ReturnType<typeof setInterval> | null = null
  let searchTimer: ReturnType<typeof setTimeout> | null = null
  // Guards against a slow request overwriting the result of a later one.
  let requestSeq = 0

  $: hasMore = offset < total
  $: levels = settings?.levels ?? []
  $: categories = settings?.categories ?? []

  function fromFor(id: string): string {
    const window = RANGES.find((r) => r.id === id)
    if (!window || window.ms === 0) return ''
    return new Date(Date.now() - window.ms).toISOString()
  }

  function buildQuery(nextOffset: number): string {
    const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(nextOffset) })
    if (level) params.set('level', level)
    if (category) params.set('category', category)
    if (deviceId) params.set('device_id', deviceId)
    if (search.trim()) params.set('search', search.trim())
    const from = fromFor(range)
    if (from) params.set('from', from)
    return params.toString()
  }

  async function fetchPage(append: boolean) {
    const seq = ++requestSeq
    if (append) loadingMore = true
    else loading = true
    error = ''
    try {
      const res = await apiFetch(`${apiURL('/api/logs')}?${buildQuery(append ? offset : 0)}`, {
        headers: authHeaders
      })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
      const page: LogPage = await res.json()
      if (seq !== requestSeq) return
      const rows = page.entries ?? []
      entries = append ? [...entries, ...rows] : rows
      total = page.total ?? entries.length
      offset = entries.length
      if (!append) expanded = null
    } catch (e) {
      if (seq === requestSeq) error = e instanceof Error ? e.message : 'Could not load logs'
    } finally {
      if (seq === requestSeq) {
        loading = false
        loadingMore = false
      }
    }
  }

  async function loadStats() {
    try {
      const res = await apiFetch(apiURL('/api/logs/stats'), { headers: authHeaders })
      if (!res.ok) return
      const data = await res.json()
      byLevel = data.by_level ?? {}
      errors24h = data.errors_24h ?? 0
    } catch {
      // Stats are a summary, not the payload. Their absence must not blank the
      // entries that did load.
    }
  }

  function reload() {
    void fetchPage(false)
    void loadStats()
  }

  async function loadSettings() {
    settingsError = ''
    try {
      const res = await apiFetch(apiURL('/api/log-settings'), { headers: authHeaders })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
      settings = await res.json()
    } catch (e) {
      settingsError = e instanceof Error ? e.message : 'Could not load settings'
    }
  }

  async function saveSettings() {
    if (!settings) return
    savingSettings = true
    settingsError = ''
    savedNote = ''
    try {
      const res = await apiFetch(apiURL('/api/log-settings'), {
        method: 'PUT',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({
          level: settings.level,
          console_level: settings.console_level,
          retention_days: Number(settings.retention_days)
        })
      })
      if (!res.ok) {
        const payload = await res.json().catch(() => ({}))
        throw new Error(payload.error || `request failed (${res.status})`)
      }
      settings = await res.json()
      savedNote = 'Saved. The new level applies immediately.'
    } catch (e) {
      settingsError = e instanceof Error ? e.message : 'Could not save settings'
    } finally {
      savingSettings = false
    }
  }

  function onSearchInput() {
    if (searchTimer) clearTimeout(searchTimer)
    searchTimer = setTimeout(() => {
      searchTimer = null
      reload()
    }, SEARCH_DEBOUNCE_MS)
  }

  function toggleLive() {
    live = !live
    if (live) {
      liveTimer = setInterval(reload, LIVE_MS)
    } else if (liveTimer) {
      clearInterval(liveTimer)
      liveTimer = null
    }
  }

  function formatTime(ts: string): string {
    const d = new Date(ts)
    if (Number.isNaN(d.getTime())) return ts
    return `${d.toLocaleDateString('en-CA')} ${d.toLocaleTimeString('en-GB', { hour12: false })}`
  }

  function metadataText(meta: unknown): string {
    if (meta == null) return ''
    if (typeof meta === 'string') return meta
    try {
      return JSON.stringify(meta)
    } catch {
      return ''
    }
  }

  onMount(() => {
    void loadSettings()
    reload()
  })

  onDestroy(() => {
    if (liveTimer) clearInterval(liveTimer)
    if (searchTimer) clearTimeout(searchTimer)
  })
</script>

<section class="app-settings">
  <header class="page-head">
    <div>
      <p class="eyebrow">Administration</p>
      <h1>Application logs</h1>
      <p class="muted">
        Every structured event the server records — HTTP, auth, devices, commands and sync — plus the
        thresholds that decide what is kept.
      </p>
    </div>
    <div class="head-actions">
      <button class:active={live} class="ghost" on:click={toggleLive}>
        {live ? 'Live · on' : 'Live · off'}
      </button>
      <button class="ghost" on:click={reload} disabled={loading}>Refresh</button>
    </div>
  </header>

  <div class="stat-row">
    <div class="stat">
      <span class="k">Total entries</span>
      <strong>{total}</strong>
    </div>
    <div class="stat">
      <span class="k">Errors · 24h</span>
      <strong class:crit={errors24h > 0}>{errors24h}</strong>
    </div>
    <div class="stat">
      <span class="k">By level</span>
      <span class="chips">
        {#each Object.entries(byLevel) as [lvl, count] (lvl)}
          <span class="chip level-{lvl.toLowerCase()}">{lvl} {count}</span>
        {/each}
        {#if Object.keys(byLevel).length === 0}<span class="muted">—</span>{/if}
      </span>
    </div>
  </div>

  <div class="filters">
    <label>
      <span class="k">Level</span>
      <select bind:value={level} on:change={reload}>
        <option value="">All</option>
        {#each levels as lvl (lvl)}<option value={lvl}>{lvl}</option>{/each}
      </select>
    </label>
    <label>
      <span class="k">Category</span>
      <select bind:value={category} on:change={reload}>
        <option value="">All</option>
        {#each categories as cat (cat)}<option value={cat}>{cat}</option>{/each}
      </select>
    </label>
    <label>
      <span class="k">Device</span>
      <select bind:value={deviceId} on:change={reload}>
        <option value="">All</option>
        {#each devices as d (d.id)}<option value={d.id}>{d.hostname || d.id}</option>{/each}
      </select>
    </label>
    <label>
      <span class="k">Window</span>
      <select bind:value={range} on:change={reload}>
        {#each RANGES as r (r.id)}<option value={r.id}>{r.label}</option>{/each}
      </select>
    </label>
    <label class="grow">
      <span class="k">Search</span>
      <input type="search" bind:value={search} on:input={onSearchInput} placeholder="message or event…" />
    </label>
  </div>

  {#if error}
    <div class="banner" role="alert">
      {error}
      <button class="inline" on:click={reload}>Retry</button>
    </div>
  {/if}

  {#if loading && entries.length === 0}
    <div class="muted pad">Loading…</div>
  {:else if entries.length === 0}
    <EmptyState icon="📋" title="No log entries" message="Nothing matches these filters. Widen the window or clear the search." />
  {:else}
    <ul class="log-list">
      {#each entries as entry (entry.id)}
        <li class:open={expanded === entry.id}>
          <button class="log-row" on:click={() => (expanded = expanded === entry.id ? null : entry.id)}>
            <span class="ts">{formatTime(entry.timestamp)}</span>
            <span class="level level-{entry.level.toLowerCase()}">{entry.level}</span>
            <span class="cat">{entry.category}</span>
            <span class="event">{entry.event}</span>
            {#if entry.device_id}<span class="device">{entry.device_id}</span>{/if}
            {#if entry.redacted}<span class="tag">redacted</span>{/if}
            <span class="msg">{entry.message}</span>
          </button>
          {#if expanded === entry.id}
            <div class="detail">
              <dl>
                <div><dt>Timestamp</dt><dd>{entry.timestamp}</dd></div>
                {#if entry.request_id}<div><dt>Request</dt><dd>{entry.request_id}</dd></div>{/if}
                {#if entry.duration_ms}<div><dt>Duration</dt><dd>{entry.duration_ms} ms</dd></div>{/if}
                {#if entry.status}<div><dt>Status</dt><dd>{entry.status}</dd></div>{/if}
                {#if entry.device_id}<div><dt>Device</dt><dd>{entry.device_id}</dd></div>{/if}
              </dl>
              {#if metadataText(entry.metadata)}
                <pre class="meta">{metadataText(entry.metadata)}</pre>
              {/if}
            </div>
          {/if}
        </li>
      {/each}
    </ul>

    {#if hasMore}
      <div class="pad center">
        <button class="ghost" on:click={() => fetchPage(true)} disabled={loadingMore}>
          {loadingMore ? 'Loading…' : `Load more (${entries.length} of ${total})`}
        </button>
      </div>
    {/if}
  {/if}

  <section class="settings-card">
    <header>
      <h2>Log settings</h2>
      <p class="muted">
        Application-wide. The level applies to the running logger immediately; the retention window
        applies at the next cleanup cycle (daily).
      </p>
    </header>

    {#if settingsError}<p class="error-inline" role="alert">{settingsError}</p>{/if}
    {#if savedNote}<p class="ok-inline">{savedNote}</p>{/if}

    {#if !settings}
      <p class="muted">Loading settings…</p>
    {:else}
      <div class="settings-grid">
        <div class="field">
          <label>
            <span class="k">Persistence level</span>
            <select bind:value={settings.level}>
              {#each settings.levels as lvl (lvl)}<option value={lvl}>{lvl}</option>{/each}
            </select>
          </label>
          <small class="muted">Entries at this level and above are stored.</small>
        </div>
        <div class="field">
          <label>
            <span class="k">Console level</span>
            <select bind:value={settings.console_level}>
              {#each settings.levels as lvl (lvl)}<option value={lvl}>{lvl}</option>{/each}
            </select>
          </label>
          <small class="muted">Entries at this level and above are printed to the container log.</small>
        </div>
        <div class="field">
          <label>
            <span class="k">Retention (days)</span>
            <input type="number" min="1" max="3650" bind:value={settings.retention_days} />
          </label>
          <small class="muted">Audit and fatal entries are never deleted.</small>
        </div>
      </div>
      <div class="settings-actions">
        <button class="primary" on:click={saveSettings} disabled={savingSettings}>
          {savingSettings ? 'Saving…' : 'Save settings'}
        </button>
      </div>
    {/if}
  </section>
</section>

<style>
  .app-settings {
    display: grid;
    gap: 1rem;
    max-width: 1100px;
  }

  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
  }

  .page-head h1 {
    margin: 0.1rem 0 0.25rem;
    font-size: 1.25rem;
    color: var(--text-bright);
  }

  .eyebrow {
    margin: 0;
    color: var(--text-faint);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
  }

  .muted {
    color: var(--text-muted);
    font-size: 0.82rem;
    margin: 0;
  }

  .head-actions {
    display: flex;
    gap: 0.4rem;
    flex-shrink: 0;
  }

  .stat-row {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 0.6rem;
  }

  .stat {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.6rem 0.75rem;
    display: grid;
    gap: 0.25rem;
  }

  .k {
    color: var(--text-faint);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .stat strong {
    font-size: 1.15rem;
    color: var(--text-bright);
    font-variant-numeric: tabular-nums;
  }

  .stat strong.crit {
    color: var(--danger, #ef5350);
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.3rem;
  }

  .chip {
    font-size: 0.7rem;
    padding: 0.05rem 0.4rem;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--card-hover);
    color: var(--text-muted);
  }

  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: 0.6rem;
    align-items: flex-end;
    padding: 0.7rem;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .filters label {
    display: grid;
    gap: 0.2rem;
  }

  .filters label.grow {
    flex: 1;
    min-width: 200px;
  }

  select,
  input {
    background: var(--card);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.35rem 0.5rem;
    font-size: 0.8rem;
  }

  .banner {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.5rem 0.7rem;
    border: 1px solid var(--danger, #ef5350);
    color: var(--text);
    border-radius: var(--radius-sm);
    background: var(--card);
    font-size: 0.82rem;
  }

  .pad {
    padding: 0.6rem 0;
  }

  .center {
    text-align: center;
  }

  .log-list {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }

  .log-list li + li {
    border-top: 1px solid var(--border);
  }

  .log-row {
    display: grid;
    grid-template-columns: 150px 62px 90px 170px auto 1fr;
    gap: 0.5rem;
    width: 100%;
    align-items: baseline;
    padding: 0.4rem 0.6rem;
    background: transparent;
    border: 0;
    text-align: left;
    cursor: pointer;
    font-size: 0.78rem;
    color: var(--text);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }

  .log-row:hover {
    background: var(--card-hover);
  }

  .ts {
    color: var(--text-faint);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .level {
    font-weight: 700;
  }

  .level-trace,
  .level-debug {
    color: var(--text-faint);
  }
  .level-info {
    color: var(--text-muted);
  }
  .level-notice {
    color: var(--accent);
  }
  .level-warn {
    color: #e0a030;
  }
  .level-error,
  .level-fatal {
    color: var(--danger, #ef5350);
  }
  .level-audit {
    color: #8ab4f8;
  }

  .cat {
    color: var(--text-muted);
  }

  .event {
    color: var(--text);
  }

  .device,
  .tag {
    color: var(--text-faint);
    font-size: 0.7rem;
  }

  .tag {
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0 0.35rem;
  }

  .msg {
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .detail {
    padding: 0.5rem 0.7rem 0.7rem;
    background: var(--bg-elevated);
  }

  .detail dl {
    display: flex;
    flex-wrap: wrap;
    gap: 0.8rem;
    margin: 0 0 0.4rem;
    font-size: 0.75rem;
  }

  .detail dt {
    color: var(--text-faint);
    text-transform: uppercase;
    font-size: 9px;
    letter-spacing: 0.08em;
  }

  .detail dd {
    margin: 0;
    color: var(--text);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }

  .meta {
    margin: 0;
    padding: 0.5rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-muted);
    font-size: 0.72rem;
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .settings-card {
    display: grid;
    gap: 0.75rem;
    padding: 0.9rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .settings-card h2 {
    margin: 0 0 0.2rem;
    font-size: 0.95rem;
    color: var(--text-bright);
  }

  .settings-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 0.75rem;
  }

  .settings-grid .field {
    display: grid;
    gap: 0.25rem;
  }

  .settings-grid label {
    display: grid;
    gap: 0.25rem;
  }

  .settings-grid small {
    font-size: 0.72rem;
  }

  .settings-actions {
    display: flex;
    justify-content: flex-end;
  }

  .error-inline {
    margin: 0;
    color: var(--danger, #ef5350);
    font-size: 0.8rem;
  }

  .ok-inline {
    margin: 0;
    color: var(--accent);
    font-size: 0.8rem;
  }

  .ghost,
  .primary,
  .inline {
    border: 1px solid var(--border);
    background: var(--card);
    color: var(--text);
    border-radius: 6px;
    padding: 0.35rem 0.6rem;
    font-size: 0.8rem;
    cursor: pointer;
  }

  .ghost.active {
    border-color: var(--accent);
    color: var(--accent);
  }

  .primary {
    border-color: var(--accent);
    color: var(--accent);
  }

  .inline {
    padding: 0.15rem 0.45rem;
    font-size: 0.75rem;
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
