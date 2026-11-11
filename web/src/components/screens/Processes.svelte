<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import EmptyState from '../ui/EmptyState.svelte'
  import ConfirmDialog from '../ui/ConfirmDialog.svelte'
  import TimeSeriesChart from '../ui/TimeSeriesChart.svelte'
  import Icon from '../ui/Icon.svelte'
  import {
    EMPTY_FILTER,
    SAVED_VIEWS,
    SIGNALS,
    SORT_KEYS,
    buildHeatmap,
    buildTable,
    canSignal,
    eventMarkers,
    filterRows,
    formatCpuPercent,
    formatUptime,
    historyWindowMs,
    ioText,
    parseProcessSnapshot,
    signalBlockReason,
    sortRows,
    type HistoryRange,
    type ProcessFilter,
    type SortKey,
    type SortDir,
    type TableEntry
  } from '../../lib/processes'
  import type { Device, ProcessHistory, ProcessRow, ProcessSnapshot, ProcessState, ProcessStats } from '../../lib/types'
  import {
    fetchProcessHistory,
    fetchProcessSnapshot,
    ioniceProcess,
    reniceProcess,
    signalProcess
  } from '../../lib/processes-client'
  import { formatBytes } from '../../lib/format'
  import { timeDomain as timeDomainFn } from '../../lib/chart'

  export let device: Device | undefined = undefined
  export let deviceId = ''
  export let authHeaders: Record<string, string> = {}

  $: id = device?.id ?? deviceId
  $: agentVersion = device?.hardware?.agent_version ?? ''

  // ---- view state ----
  let view: 'live' | 'history' = 'live'
  let snapshot: ProcessSnapshot | null = null
  let loading = true
  let loaded = false
  let error = ''

  let history: ProcessHistory | null = null
  let historyLoading = false
  let historyError = ''
  let historyRange: HistoryRange = '24h'

  // ---- table state ----
  let mode: 'flat' | 'tree' = 'flat'
  let sortKey: SortKey = 'cpu'
  let sortDir: SortDir = 'desc'
  let density: 'comfy' | 'compact' = 'comfy'
  let expanded = new Set<number>()
  let selected: ProcessRow | null = null
  let drawerOpen = false

  let search = ''
  let states: ProcessState[] = []
  let hideKernel = true
  let listeningOnly = false
  let windowNames: string[] | null = null
  let heatmapSelection: string | null = null

  // ---- modal ----
  let killTarget: ProcessRow | null = null
  let killConfirmText = ''
  let killBusy = false
  let killBlockedReason: string | null = null
  let actionError = ''

  let refreshTimer: ReturnType<typeof setInterval> | null = null
  let requestSeq = 0

  $: filter = {
    search,
    states,
    hideKernel,
    listeningOnly,
    windowNames
  } as ProcessFilter

  $: stats = snapshot?.stats ?? null
  $: rows = snapshot?.processes ?? []
  $: filteredRows = filterRows(rows, filter)
  $: tableEntries = buildTable(filteredRows, { mode, sortKey, sortDir, expanded })
  $: ioAvailable = snapshot?.io_available ?? false
  $: signalsEnabled = snapshot?.signals_enabled ?? false

  function toggleState(state: ProcessState) {
    if (states.includes(state)) states = states.filter((s) => s !== state)
    else states = [...states, state]
  }

  function toggleSort(key: SortKey) {
    if (sortKey === key) sortDir = sortDir === 'asc' ? 'desc' : 'asc'
    else {
      sortKey = key
      sortDir = key === 'name' ? 'asc' : 'desc'
    }
  }

  function toggleExpand(pid: number) {
    const next = new Set(expanded)
    if (next.has(pid)) next.delete(pid)
    else next.add(pid)
    expanded = next
  }

  function select(row: ProcessRow) {
    selected = row
    drawerOpen = true
  }

  function closeDrawer() {
    drawerOpen = false
    selected = null
  }

  function applyView(id: string) {
    const view = SAVED_VIEWS.find((v) => v.id === id)
    if (!view) return
    if (view.filter.search !== undefined) search = view.filter.search
    if (view.filter.states) states = view.filter.states
    if (view.filter.hideKernel !== undefined) hideKernel = view.filter.hideKernel
    if (view.filter.listeningOnly !== undefined) listeningOnly = view.filter.listeningOnly
    windowNames = null
    heatmapSelection = null
  }

  async function load() {
    if (!id) return
    const seq = ++requestSeq
    loading = true
    error = ''
    try {
      const snap = await fetchProcessSnapshot(id, { headers: authHeaders })
      if (seq !== requestSeq) return
      snapshot = snap
    } catch (e) {
      if (seq !== requestSeq) return
      error = e instanceof Error ? e.message : 'Could not load processes'
      snapshot = null
    } finally {
      if (seq === requestSeq) {
        loading = false
        loaded = true
      }
    }
  }

  async function loadHistory() {
    if (!id) return
    historyLoading = true
    historyError = ''
    try {
      history = await fetchProcessHistory(id, historyRange, { headers: authHeaders })
    } catch (e) {
      historyError = e instanceof Error ? e.message : 'Could not load history'
      history = null
    } finally {
      historyLoading = false
    }
  }

  async function doSignal(row: ProcessRow, signal: (typeof SIGNALS)[number]) {
    if (!snapshot) return
    actionError = ''
    try {
      await signalProcess(id, row.pid, signal, row.start_time_unix, { headers: authHeaders })
    } catch (e) {
      actionError = e instanceof Error ? e.message : 'signal failed'
    }
    await load()
  }

  function openKill(row: ProcessRow) {
    const reason = signalBlockReason(row, snapshot!)
    killTarget = row
    killBlockedReason = reason
    killConfirmText = ''
    killBusy = false
  }

  function closeKill() {
    killTarget = null
    killBlockedReason = null
    killConfirmText = ''
  }

  async function confirmKill() {
    if (!killTarget || killBlockedReason) return
    if (killConfirmText.trim() !== String(killTarget.pid)) return
    killBusy = true
    actionError = ''
    try {
      await signalProcess(id, killTarget.pid, 'KILL', killTarget.start_time_unix, { headers: authHeaders })
    } catch (e) {
      actionError = e instanceof Error ? e.message : 'signal failed'
    } finally {
      killBusy = false
      closeKill()
      await load()
    }
  }

  async function doRenice(row: ProcessRow, nice: number) {
    try {
      await reniceProcess(id, row.pid, nice, { headers: authHeaders })
    } catch (e) {
      actionError = e instanceof Error ? e.message : 'renice failed'
    }
    await load()
  }

  async function doIonice(row: ProcessRow, ioClass: number, level: number) {
    try {
      await ioniceProcess(id, row.pid, ioClass, level, { headers: authHeaders })
    } catch (e) {
      actionError = e instanceof Error ? e.message : 'ionice failed'
    }
    await load()
  }

  // ---- history derived ----
  $: heatmap = history ? buildHeatmap(history.buckets) : null
  $: historyLabels = history
    ? Array.from(
        new Set(history.series.flatMap((s) => s.points.map((p) => new Date(p[0] * 1000).toISOString())))
      ).sort()
    : []
  $: historySeries = history
    ? history.series.map((s) => {
        const byTime = new Map(s.points.map((p) => [p[0], p[1]]))
        return {
          label: s.name,
          values: historyLabels.map((label) => {
            const t = Math.floor(Date.parse(label) / 1000)
            return byTime.has(t) ? byTime.get(t)! : null
          }),
          color: 'var(--series-1)'
        }
      })
    : []
  $: historyWindowEnd = historyLabels.length
    ? Math.max(...historyLabels.map((l) => Date.parse(l)))
    : null
  $: historyMarkers = history && historyLabels.length
    ? eventMarkers(
        history.events,
        (timeDomainMs(historyLabels, historyWindowMs(historyRange), historyWindowEnd)?.from) ?? 0,
        (timeDomainMs(historyLabels, historyWindowMs(historyRange), historyWindowEnd)?.to) ?? 0
      )
    : []

  function timeDomainMs(labels: string[], windowMs: number, windowEnd: number | null) {
    return timeDomainFn(labels, windowMs, windowEnd)
  }

  function selectHeatmap(name: string) {
    if (heatmapSelection === name) {
      heatmapSelection = null
      windowNames = null
    } else {
      heatmapSelection = name
      windowNames = [name]
    }
  }

  $: if (id) {
    // initial
  }

  onMount(() => {
    if (refreshTimer) clearInterval(refreshTimer)
    refreshTimer = setInterval(() => void load(), 30_000)
  })

  // react to id changes
  $: if (id) {
    // trigger load when id changes (guarded by seq)
    void load()
  }

  $: if (view === 'history' && id) {
    void loadHistory()
  }

  $: if (historyRange && view === 'history') {
    void loadHistory()
  }

  onDestroy(() => {
    if (refreshTimer) clearInterval(refreshTimer)
  })
</script>

<section class="procs">
  {#if actionError}
    <div class="error-banner" role="alert">{actionError}<button class="retry" on:click={() => (actionError = '')}>×</button></div>
  {/if}

  <!-- subnav -->
  <div class="subnav">
    <span class="segmented" role="group" aria-label="View">
      <button class:active={view === 'live'} on:click={() => (view = 'live')}>Live</button>
      <button class:active={view === 'history'} on:click={() => (view = 'history')}>History</button>
    </span>
  </div>

  {#if loading && !loaded}
    <p class="loading">Loading processes…</p>
  {:else if error && !snapshot}
    <div class="error-banner" role="alert">{error}<button class="retry" on:click={load}>Retry</button></div>
  {:else if !snapshot?.collected}
    <EmptyState
      icon="cog"
      title="Not collected"
      message={agentVersion ? `Agent ${agentVersion} did not report processes.` : 'Agent did not report processes.'}
      actionLabel="Refresh"
      onAction={load}
    />
  {:else}
    {#if view === 'live'}
      <!-- KPI strip -->
      {#if stats}
        <div class="kpis">
          <div class="kpi"><span class="k">Processes</span><span class="v">{stats.total}</span></div>
          <div class="kpi"><span class="k">Threads</span><span class="v">{stats.threads}</span></div>
          <div class="kpi"><span class="k">Zombies</span><span class="v" class:warn={stats.zombie > 0}>{stats.zombie}</span></div>
          <div class="kpi"><span class="k">Running</span><span class="v">{stats.running} / {stats.blocked}</span></div>
          <div class="kpi"><span class="k">Load</span><span class="v">{stats.load1.toFixed(2)}</span></div>
          <div class="kpi"><span class="k">CPU psi</span><span class="v">{stats.psi_cpu_some_avg10.toFixed(1)}%</span></div>
          <div class="kpi"><span class="k">OOM 24h</span><span class="v" class:crit={stats.oom_kills_24h > 0}>{stats.oom_kills_24h}</span></div>
        </div>
      {/if}

      <!-- toolbar -->
      <div class="card">
        <div class="toolbar">
          <div class="trow">
            <input type="search" bind:value={search} placeholder="Search" aria-label="Search processes" />
            <span class="gap"></span>
            <span class="count-line">Views</span>
            {#each SAVED_VIEWS as v (v.id)}
              <button class="chip" on:click={() => applyView(v.id)}>{v.label}</button>
            {/each}
            <span class="gap"></span>
            <button class="btn" on:click={() => (mode = mode === 'flat' ? 'tree' : 'flat')}>
              {mode === 'flat' ? 'Tree' : 'Flat'}
            </button>
            <button class="btn" on:click={() => (density = density === 'comfy' ? 'compact' : 'comfy')}>
              {density === 'comfy' ? 'Compact' : 'Comfy'}
            </button>
          </div>
          <div class="trow">
            <span class="count-line">State</span>
            {#each ['R','S','D','Z','T','I'] as s (s)}
              <button
                class="chip"
                class:active={states.includes(s as ProcessState)}
                on:click={() => toggleState(s as ProcessState)}
              >{s}</button>
            {/each}
            <button class="chip" class:active={listeningOnly} on:click={() => (listeningOnly = !listeningOnly)}>listening</button>
            <button class="chip" class:active={hideKernel} on:click={() => (hideKernel = !hideKernel)}>kernel</button>
            <span class="gap"></span>
            <span class="count-line">Sort</span>
            <select bind:value={sortKey} aria-label="Sort">
              {#each SORT_KEYS as k (k)}
                <option value={k}>{k}</option>
              {/each}
            </select>
            <button class="btn" on:click={() => (sortDir = sortDir === 'asc' ? 'desc' : 'asc')}>{sortDir}</button>
          </div>
        </div>
      </div>

      <!-- table -->
      <div class="card">
        <div class="table-scroll" class:density-c={density === 'compact'}>
          <table>
            <thead>
              <tr>
                <th></th>
                <th class="sortable" on:click={() => toggleSort('pid')}>PID{sortKey === 'pid' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</th>
                <th class="sortable" on:click={() => toggleSort('name')}>Name{sortKey === 'name' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</th>
                <th>State</th>
                <th class="r sortable" on:click={() => toggleSort('cpu')}>CPU{sortKey === 'cpu' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</th>
                <th class="r sortable" on:click={() => toggleSort('rss')}>MEM{sortKey === 'rss' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</th>
                <th>IO</th>
                <th class="r sortable" on:click={() => toggleSort('threads')}>THR{sortKey === 'threads' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</th>
                <th class="r sortable" on:click={() => toggleSort('uptime')}>Up{sortKey === 'uptime' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {#each tableEntries as entry (entry.row.pid)}
                <tr
                  class="row"
                  class:sel={selected?.pid === entry.row.pid}
                  on:click={() => select(entry.row)}
                >
                  <td>
                    {#if entry.hasChildren}
                      <button class="exp" on:click|stopPropagation={() => toggleExpand(entry.row.pid)}>
                        {entry.expanded ? '▾' : '▸'}
                      </button>
                    {:else}
                      <span class="exp leaf"></span>
                    {/if}
                  </td>
                  <td class="mono pid" style:padding-left={`${entry.depth * 1}rem`}>{entry.row.pid}</td>
                  <td>
                    <div class="namecell">
                      <span class="pname" title={entry.row.cmd || entry.row.name}>{entry.row.name}</span>
                    </div>
                    <div class="pcmd" title={entry.row.cmd}>{entry.row.cmd}</div>
                  </td>
                  <td><span class="statebadge state-{entry.row.state}">{entry.row.state}</span></td>
                  <td class="r">
                    <span class="cpuval" class:hot={entry.row.cpu_percent >= 50}>{formatCpuPercent(entry.row.cpu_percent)}</span>
                  </td>
                  <td class="r">
                    {formatBytes(entry.row.rss_bytes)}
                    <span class="memsmall">{stats && stats.total ? ((entry.row.rss_bytes / (device?.hardware?.memory_total_bytes || 1)) * 100).toFixed(1) : ''}%</span>
                  </td>
                  <td><span class="ioval">{ioText(entry.row, ioAvailable)}</span></td>
                  <td class="r">{entry.row.threads}</td>
                  <td class="r">{formatUptime(entry.row.uptime_seconds)}</td>
                  <td>
                    <div class="qa">
                      <button class="qab" title="TERM" on:click|stopPropagation={() => doSignal(entry.row, 'TERM')}>T</button>
                      <button class="qab crit" title="KILL" on:click|stopPropagation={() => openKill(entry.row)}>K</button>
                      <button class="qab" title="STOP" on:click|stopPropagation={() => doSignal(entry.row, 'STOP')}>S</button>
                      <button class="qab" title="CONT" on:click|stopPropagation={() => doSignal(entry.row, 'CONT')}>C</button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
          {#if filteredRows.length === 0}
            <p class="count-line pad">No processes match.</p>
          {/if}
        </div>
      </div>
    {:else}
      <!-- History -->
      <div class="card">
        <div class="card-head">
          <h2>History</h2>
          <div class="tools">
            {#each ['1h','24h','7d'] as r (r)}
              <button class="btn" class:active={historyRange === r} on:click={() => (historyRange = r as HistoryRange)}>{r}</button>
            {/each}
          </div>
        </div>
        {#if historyLoading && !history}
          <p class="loading">Loading history…</p>
        {:else if historyError}
          <div class="error-banner" role="alert">{historyError}<button class="retry" on:click={loadHistory}>Retry</button></div>
        {:else if history}
          <!-- heatmap -->
          {#if heatmap && heatmap.names.length > 0}
            <div class="heatmap">
              {#each heatmap.rows as row (row.name)}
                <div class="heatmap-row">
                  <button class="heatmap-name" on:click={() => selectHeatmap(row.name)}>{row.name}</button>
                  {#each row.cells as cell, i (i)}
                    <span
                      class="heatmap-cell"
                      style:opacity={cell == null ? 0.08 : Math.max(0.15, (cell / (heatmap.max || 1)))}
                      title={cell == null ? 'absent' : `${cell.toFixed(1)}%`}
                    ></span>
                  {/each}
                </div>
              {/each}
            </div>
          {:else}
            <p class="loading">No history yet.</p>
          {/if}

          <!-- chart -->
          <div class="chart-wrap">
            <TimeSeriesChart
              labels={historyLabels}
              series={historySeries}
              windowMs={historyWindowMs(historyRange)}
              windowEnd={historyWindowEnd}
              height={220}
            />
            {#if historyMarkers.length > 0}
              <div class="markers" aria-hidden="true">
                {#each historyMarkers as m (m.event.t + m.event.pid)}
                  <span class="marker" style:left={`${m.left}%`} title={`${m.event.type} ${m.event.name}`}></span>
                {/each}
              </div>
            {/if}
          </div>

          <!-- events -->
          <div class="events">
            <h3>Events</h3>
            {#each history.events.slice(-50).reverse() as e (e.t + e.pid)}
              <div class="event-row">
                <span class="event-type">{e.type}</span>
                <span class="mono">{e.name}</span>
                <span class="faint">{e.pid}</span>
                {#if e.exit_code !== undefined}<span class="faint">exit {e.exit_code}</span>{/if}
              </div>
            {/each}
            {#if history.events.length === 0}
              <p class="loading">No events.</p>
            {/if}
          </div>
        {:else}
          <p class="loading">No history.</p>
        {/if}
      </div>
    {/if}
  {/if}

  <!-- drawer -->
  {#if drawerOpen && selected}
    <div class="backdrop" role="presentation" on:click={closeDrawer}></div>
    <aside class="drawer" aria-label="Process details">
      <header class="drawer-head">
        <div class="t">
          <div class="name">{selected.name}</div>
          <div class="sub">{selected.pid} · ppid {selected.ppid}</div>
        </div>
        <button class="close" on:click={closeDrawer} aria-label="Close"><Icon name="x" size={13} /></button>
      </header>
      <div class="cmdblock"><code>{selected.cmd || selected.name}</code></div>
      <dl class="facts">
        <div><dt>User</dt><dd>{selected.user} ({selected.uid})</dd></div>
        <div><dt>State</dt><dd>{selected.state} · {selected.threads} thr</dd></div>
        <div><dt>CPU</dt><dd>{formatCpuPercent(selected.cpu_percent)} · {selected.cpu_time_seconds.toFixed(1)}s</dd></div>
        <div><dt>RSS</dt><dd>{formatBytes(selected.rss_bytes)} · VSZ {formatBytes(selected.vsz_bytes)}</dd></div>
        <div><dt>Swap</dt><dd>{formatBytes(selected.swap_bytes)}</dd></div>
        <div><dt>IO</dt><dd>{ioText(selected, ioAvailable)}</dd></div>
        <div><dt>FDs</dt><dd>{selected.fd_count}</dd></div>
        <div><dt>OOM score</dt><dd>{selected.oom_score}</dd></div>
        <div><dt>Nice/Prio</dt><dd>{selected.nice} / {selected.prio}</dd></div>
        <div><dt>Uptime</dt><dd>{formatUptime(selected.uptime_seconds)}</dd></div>
        <div><dt>Unit</dt><dd class="mono">{selected.unit || '—'}</dd></div>
        <div><dt>Container</dt><dd class="mono">{selected.container || '—'}</dd></div>
        <div><dt>Cgroup</dt><dd class="mono small">{selected.cgroup || '—'}</dd></div>
        <div><dt>Ports</dt><dd>{selected.ports.length ? selected.ports.map((p: { proto: string; port: number }) => `${p.proto} ${p.port}`).join(', ') : '—'}</dd></div>
      </dl>
      <div class="actions">
        <button class="btn" disabled={!canSignal(selected, snapshot!)} on:click={() => doSignal(selected!, 'TERM')}>TERM</button>
        <button class="btn crit" disabled={!canSignal(selected, snapshot!)} on:click={() => openKill(selected!)}>KILL</button>
        <button class="btn" disabled={!canSignal(selected, snapshot!)} on:click={() => doSignal(selected!, 'STOP')}>STOP</button>
        <button class="btn" disabled={!canSignal(selected, snapshot!)} on:click={() => doSignal(selected!, 'CONT')}>CONT</button>
      </div>
      {#if !canSignal(selected, snapshot!)}
        <p class="blocked">{signalBlockReason(selected, snapshot!)}</p>
      {/if}
      {#if signalsEnabled}
        <div class="renice">
          <label>Nice <input type="number" min="-20" max="19" value={selected.nice} on:change={(e) => doRenice(selected!, Number((e.currentTarget as HTMLInputElement).value))} /></label>
          <label>Ionice class <input type="number" min="1" max="3" value={2} on:change={(e) => doIonice(selected!, Number((e.currentTarget as HTMLInputElement).value), 4)} /></label>
        </div>
      {/if}
    </aside>
  {/if}

  <!-- kill modal -->
  {#if killTarget}
    <div class="backdrop" role="presentation">
      <div class="dialog" role="alertdialog" aria-modal="true">
        <h3>Kill {killTarget.name}?</h3>
        {#if killBlockedReason}
          <p class="blocked">Blocked: {killBlockedReason}</p>
        {:else}
          <p>Type {killTarget.pid} to confirm.</p>
          <input bind:value={killConfirmText} placeholder={String(killTarget.pid)} />
        {/if}
        <div class="actions">
          <button class="btn" on:click={closeKill}>Cancel</button>
          <button
            class="btn crit"
            disabled={!!killBlockedReason || killConfirmText.trim() !== String(killTarget.pid) || killBusy}
            on:click={confirmKill}
          >Kill</button>
        </div>
      </div>
    </div>
  {/if}
</section>

<style>
  .procs { display: grid; gap: 0.85rem; }
  .subnav { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; }
  .segmented { display: inline-flex; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
  .segmented button { padding: 0.28rem 0.5rem; border: 0; border-right: 1px solid var(--border); background: transparent; color: var(--text-faint); font-size: 0.71rem; font-weight: 600; cursor: pointer; }
  .segmented button:last-child { border-right: 0; }
  .segmented button.active { background: var(--accent-dim); color: var(--accent); }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .toolbar { padding: 0.55rem 0.75rem; display: grid; gap: 0.45rem; }
  .trow { display: flex; align-items: center; gap: 0.45rem; flex-wrap: wrap; }
  .gap { flex: 1; }
  .count-line { color: var(--text-faint); font-size: 0.68rem; }
  .btn { padding: 0.3rem 0.55rem; background: transparent; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-muted); font: inherit; font-size: 0.72rem; cursor: pointer; }
  .btn:hover:not(:disabled) { border-color: var(--accent-border); color: var(--text); }
  .btn:disabled { opacity: 0.5; cursor: default; }
  .btn.crit { border-color: rgba(248,113,113,0.45); color: var(--crit); }
  .btn.active { background: var(--accent-dim); color: var(--accent); border-color: var(--accent-border); }
  .chip { padding: 0.2rem 0.5rem; background: transparent; border: 1px solid var(--border); border-radius: 999px; color: var(--text-muted); font: inherit; font-size: 0.71rem; cursor: pointer; }
  .chip:hover { border-color: var(--accent-border); }
  .chip.active { background: var(--accent-dim); border-color: var(--accent-border); color: var(--text); }
  .kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(112px, 1fr)); gap: 0.6rem; }
  .kpi { display: grid; gap: 0.15rem; padding: 0.65rem 0.8rem; background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); }
  .kpi .k { color: var(--text-muted); font-size: 10.5px; font-weight: 600; text-transform: uppercase; }
  .kpi .v { color: var(--text-bright); font-size: 1.3rem; font-variant-numeric: tabular-nums; }
  .kpi .v.warn { color: var(--warn); }
  .kpi .v.crit { color: var(--crit); }
  .tools { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
  .table-scroll { overflow-x: auto; }
  .table-scroll.density-c td { height: 27px; }
  table { width: 100%; border-collapse: collapse; font-size: 11.5px; }
  th { text-align: left; padding: 0.45rem 0.55rem; color: var(--text-faint); font-size: 9.5px; font-weight: 700; text-transform: uppercase; border-bottom: 1px solid var(--border); white-space: nowrap; user-select: none; }
  th.sortable { cursor: pointer; }
  td { padding: 0 0.55rem; border-bottom: 1px solid var(--border); color: var(--text); vertical-align: middle; white-space: nowrap; height: 32px; }
  tr.row { cursor: pointer; }
  tr.row:hover { background: var(--card-hover); }
  tr.row.sel { background: var(--accent-dim); box-shadow: inset 2px 0 0 var(--accent); }
  .r { text-align: right; font-variant-numeric: tabular-nums; }
  .mono { font-family: var(--mono); }
  .pid { color: var(--text-bright); }
  .namecell { display: flex; align-items: center; gap: 0.4rem; max-width: 20rem; }
  .pname { color: var(--text-bright); font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .pcmd { font-family: var(--mono); color: var(--text-faint); font-size: 10px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 18rem; }
  .statebadge { display: inline-flex; align-items: center; justify-content: center; min-width: 1.05rem; padding: 0.05rem 0.3rem; border-radius: 4px; font-size: 9.5px; font-weight: 700; font-family: var(--mono); }
  .state-R { background: var(--accent-dim); color: var(--accent); }
  .state-S { background: var(--card-inset); color: var(--text-muted); }
  .state-D { background: var(--warn-dim); color: var(--warn); }
  .state-Z { background: var(--crit-dim); color: var(--crit); }
  .state-T { background: var(--card-inset); color: var(--text-faint); }
  .state-I { background: var(--card-inset); color: var(--text-faint); }
  .cpuval { font-variant-numeric: tabular-nums; font-weight: 600; }
  .cpuval.hot { color: var(--warn); }
  .ioval { font-family: var(--mono); font-size: 10.5px; color: var(--text-muted); }
  .memsmall { display: block; color: var(--text-faint); font-size: 9.5px; }
  .exp { width: 14px; height: 14px; border: 0; background: transparent; color: var(--text-faint); cursor: pointer; padding: 0; display: inline-flex; align-items: center; justify-content: center; border-radius: 4px; }
  .exp.leaf { visibility: hidden; }
  .qa { display: flex; gap: 0.25rem; justify-content: flex-end; }
  .qab { width: 22px; height: 22px; display: inline-flex; align-items: center; justify-content: center; border: 1px solid var(--border); border-radius: 6px; background: var(--card-inset); color: var(--text-muted); cursor: pointer; padding: 0; font-size: 10px; }
  .qab.crit:hover { color: var(--crit); border-color: rgba(248,113,113,0.5); }
  .loading { margin: 0; padding: 0.9rem; color: var(--text-faint); font-size: 0.78rem; }
  .error-banner { display: flex; align-items: center; gap: 0.6rem; padding: 0.5rem 0.75rem; background: var(--crit-dim); color: var(--crit); font-size: 0.76rem; border-radius: var(--radius-sm); }
  .retry { margin-left: auto; padding: 0.2rem 0.5rem; background: transparent; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font: inherit; font-size: 0.72rem; cursor: pointer; }
  .pad { padding: 0.9rem; }
  .backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.5); z-index: 40; }
  .drawer { position: fixed; top: 0; right: 0; height: 100vh; width: min(460px, 94vw); background: var(--bg-elevated); border-left: 1px solid var(--border); box-shadow: var(--shadow-pop); z-index: 41; overflow-y: auto; padding: 0.9rem; }
  .drawer-head { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; }
  .drawer-head .name { font-size: 0.95rem; font-weight: 600; color: var(--text-bright); }
  .drawer-head .sub { font-family: var(--mono); color: var(--text-faint); font-size: 0.7rem; }
  .close { border: 1px solid var(--border); background: transparent; color: var(--text-muted); cursor: pointer; border-radius: var(--radius-sm); padding: 0.25rem; }
  .cmdblock { background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 0.45rem 0.55rem; margin: 0.5rem 0; }
  .cmdblock code { font-family: var(--mono); font-size: 0.68rem; color: var(--text-muted); word-break: break-all; }
  .facts { display: grid; gap: 0.4rem; margin: 0.8rem 0; }
  .facts > div { display: grid; grid-template-columns: 5rem 1fr; gap: 0.5rem; align-items: baseline; }
  .facts dt { color: var(--text-faint); font-size: 0.7rem; text-transform: uppercase; }
  .facts dd { margin: 0; color: var(--text); font-size: 0.8rem; }
  .facts .mono { font-family: var(--mono); }
  .facts .small { font-size: 0.68rem; }
  .actions { display: flex; gap: 0.5rem; flex-wrap: wrap; margin-top: 0.5rem; }
  .blocked { color: var(--crit); font-size: 0.75rem; }
  .renice { display: flex; gap: 0.75rem; margin-top: 0.5rem; }
  .renice label { display: flex; align-items: center; gap: 0.3rem; color: var(--text-faint); font-size: 0.72rem; }
  .renice input { width: 4rem; }
  .heatmap { display: grid; gap: 0.25rem; padding: 0.5rem 0.75rem; }
  .heatmap-row { display: flex; align-items: center; gap: 0.4rem; }
  .heatmap-name { border: 0; background: transparent; color: var(--text-muted); font: inherit; font-size: 0.72rem; cursor: pointer; width: 6rem; text-align: left; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .heatmap-cell { width: 14px; height: 14px; border-radius: 3px; background: var(--accent); display: inline-block; }
  .chart-wrap { position: relative; padding: 0.5rem 0.75rem; }
  .markers { position: absolute; top: 0.5rem; left: 0.75rem; right: 0.75rem; height: 220px; pointer-events: none; }
  .marker { position: absolute; top: 0; width: 2px; height: 100%; background: var(--crit); opacity: 0.6; }
  .events { padding: 0.5rem 0.75rem; display: grid; gap: 0.25rem; max-height: 24vh; overflow-y: auto; }
  .events h3 { margin: 0; font-size: 0.75rem; color: var(--text-faint); text-transform: uppercase; }
  .event-row { display: flex; gap: 0.6rem; font-size: 0.75rem; align-items: baseline; }
  .event-type { color: var(--accent); font-size: 0.7rem; text-transform: uppercase; }
  .faint { color: var(--text-faint); }
  .small { font-size: 0.7rem; }
  .dialog { background: var(--bg-elevated); border: 1px solid var(--border); border-radius: var(--radius); padding: 1rem; width: min(420px, 90vw); display: grid; gap: 0.75rem; z-index: 41; position: relative; }
  .dialog input { padding: 0.45rem 0.6rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font-family: var(--mono); }
  .dialog h3 { margin: 0; color: var(--text-bright); font-size: 1rem; }
</style>