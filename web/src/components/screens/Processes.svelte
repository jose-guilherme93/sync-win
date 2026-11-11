<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte'
  import EmptyState from '../ui/EmptyState.svelte'
  import TimeSeriesChart from '../ui/TimeSeriesChart.svelte'
  import Icon from '../ui/Icon.svelte'
  import ProcessDetail from '../processes/ProcessDetail.svelte'
  import {
    EVENT_LABELS,
    HEAT_CELL_WIDTH,
    HEAT_COLUMN_SECONDS,
    HISTORY_RANGES,
    SAVED_VIEWS,
    STATE_LABELS,
    buildHeatmap,
    buildTable,
    busiestCell,
    bucketRangeLabel,
    filterOptions,
    filterRows,
    formatClock,
    formatCoreMin,
    formatCpuPercent,
    formatShare,
    formatUptime,
    groupRows,
    heatCellColor,
    collapseEvents,
    heatTicks,
    historyWindowMs,
    ioText,
    memoryShare,
    signalBlockReason,
    stateCounts,
    treeEntries,
    type HistoryRange,
    type ProcessFilter,
    type ProcessGroup,
    type SortDir,
    type SortKey,
    type TableEntry
  } from '../../lib/processes'
  import type { Device, ProcessHistory, ProcessInfo, ProcessRow, ProcessSnapshot, ProcessSignal, ProcessState } from '../../lib/types'
  import {
    fetchProcessHistory,
    fetchProcessSnapshot,
    ioniceProcess,
    reniceProcess,
    signalProcess
  } from '../../lib/processes-client'
  import { formatBytes } from '../../lib/format'

  export let device: Device | undefined = undefined
  export let deviceId = ''
  export let authHeaders: Record<string, string> = {}

  $: id = device?.id ?? deviceId

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
  let historyDefaultSet = false

  let heatHistory: ProcessHistory | null = null
  let heatLoading = false
  let heatError = ''

  // ---- layout state ----
  type GroupMode = 'flat' | 'tree' | 'unit' | 'container'
  let group: GroupMode = 'flat'
  let layout: 'table' | 'tree' = 'table'
  let sortKey: SortKey = 'cpu'
  let sortDir: SortDir = 'desc'
  // Compact is the default: the list is the point of this screen, and a comfortable
  // row costs a third of the visible ones. The toggle still offers the roomier one.
  let density: 'comfy' | 'compact' = 'compact'
  let expanded = new Set<number>()
  let collapsed = new Set<number>()
  let selected: ProcessRow | null = null
  let drawerOpen = false

  let search = ''
  let searchOpen = false
  let searchBox: HTMLInputElement | undefined = undefined
  let viewsOpen = false
  let states: ProcessState[] = []
  let hideKernel = true
  let listeningOnly = false
  let user = ''
  let unit = ''
  let container = ''
  let windowNames: string[] | null = null
  let windowRange: { from: number; to: number } | null = null
  let windowCount = 0
  let activeView: string | null = null

  // ---- modal ----
  let killTarget: ProcessRow | null = null
  let killConfirmText = ''
  let killBusy = false
  let killBlockedReason: string | null = null
  let actionError = ''

  let refreshTimer: ReturnType<typeof setInterval> | null = null
  let requestSeq = 0
  let hmwrap: HTMLElement | undefined = undefined
  let hmScroll = 0
  let hmViewport = 0

  const STATE_CHIPS: ProcessState[] = ['R', 'S', 'D', 'Z', 'T']
  const STATE_DOT: Record<ProcessState, string> = {
    R: 'var(--accent)',
    S: '#4b5563',
    D: 'var(--warn)',
    Z: 'var(--crit)',
    T: 'var(--text-faint)',
    I: 'var(--text-faint)'
  }
  const GROUP_BUTTONS: { id: GroupMode; label: string; title: string }[] = [
    { id: 'flat', label: 'Flat', title: 'One flat list' },
    { id: 'tree', label: 'Process tree', title: 'Children under their parent' },
    { id: 'unit', label: 'By unit', title: 'Grouped by systemd unit' },
    { id: 'container', label: 'By container', title: 'Grouped by container' }
  ]

  $: filter = {
    search,
    states,
    hideKernel,
    listeningOnly,
    user,
    unit,
    container,
    windowNames
  } as ProcessFilter

  $: stats = snapshot?.stats ?? null
  $: hw = device?.hardware
  $: processSummary = hw?.process_summary ?? null
  $: reported = mergeReported(hw)
  // An agent that predates the full list reports only the top/problem lists.
  // Those rows still deserve a table; every field it never sent stays blank
  // instead of being invented.
  $: legacyMode = !snapshot?.collected && reported.length > 0
  $: rows = legacyMode ? reported.map(toLegacyRow) : (snapshot?.processes ?? [])
  $: totalMemory = hw?.memory_total_bytes ?? 0

  const STATE_LETTERS = new Set(['R', 'S', 'D', 'Z', 'T', 'I'])
  function toLegacyRow(p: ProcessInfo): ProcessRow {
    const state = (p.state && STATE_LETTERS.has(p.state) ? p.state : '') as ProcessState
    return {
      pid: p.pid,
      ppid: p.ppid ?? 0,
      name: p.name,
      cmd: '',
      user: p.user ?? '',
      uid: 0,
      state,
      threads: 0,
      rss_bytes: p.mem_rss_bytes,
      vsz_bytes: p.mem_virt_bytes ?? 0,
      swap_bytes: 0,
      cpu_percent: p.cpu_percent,
      cpu_time_seconds: 0,
      start_time_unix: 0,
      uptime_seconds: 0,
      io_read_bps: 0,
      io_write_bps: 0,
      fd_count: 0,
      oom_score: -1,
      nice: 0,
      prio: 0,
      cgroup: '',
      unit: '',
      container: '',
      ports: [],
      kernel: false
    }
  }
  function mergeReported(hardware: Device['hardware']): ProcessInfo[] {
    if (!hardware) return []
    const byPid = new Map<number, ProcessInfo>()
    for (const list of [hardware.top_cpu_processes, hardware.top_mem_processes, hardware.stopped_processes, hardware.zombie_processes]) {
      for (const proc of list ?? []) {
        const existing = byPid.get(proc.pid)
        byPid.set(proc.pid, existing ? { ...existing, ...proc } : proc)
      }
    }
    return [...byPid.values()]
  }

  $: options = filterOptions(rows)
  $: counts = stateCounts(rows, filter)
  $: filteredRows = filterRows(rows, filter)
  // Two expansion models, because they start from opposite ends: the flat
  // table hides children until a row is opened, the tree group shows the whole
  // tree until a subtree is collapsed.
  $: treeExpanded = new Set(rows.filter((row) => !collapsed.has(row.pid)).map((row) => row.pid))
  $: tableEntries = buildTable(filteredRows, {
    mode: group === 'tree' ? 'tree' : 'flat',
    sortKey,
    sortDir,
    expanded: group === 'tree' ? treeExpanded : expanded
  })
  $: groups =
    group === 'unit' || group === 'container' ? groupRows(filteredRows, group) : []
  $: treeRows = treeEntries(filteredRows, 'cpu', 'desc')
  $: ioAvailable = snapshot?.io_available ?? false
  $: signalsEnabled = snapshot?.signals_enabled ?? false

  // One list of lines for every table mode, so a row is drawn once whether it
  // sits under a group header or directly in the table.
  type TableLine = { kind: 'group'; group: ProcessGroup } | { kind: 'row'; entry: TableEntry }
  $: tableLines = ((): TableLine[] => {
    if (group === 'unit' || group === 'container') {
      return groups.flatMap((entry) => [
        { kind: 'group' as const, group: entry },
        ...entry.rows.map((row) => ({
          kind: 'row' as const,
          entry: { row, depth: 0, hasChildren: false, expanded: false }
        }))
      ])
    }
    return tableEntries.map((entry) => ({ kind: 'row' as const, entry }))
  })()

  // Keep the inspected row pointing at the live sample of the same pid, so a
  // refresh does not freeze the inspector on a stale object.
  $: if (selected && rows.length) {
    const fresh = rows.find((row) => row.pid === selected!.pid)
    if (fresh && fresh !== selected) selected = fresh
  }

  // The inspector is persistent, so arriving at the tree layout with nothing
  // selected would draw an empty half of the screen. Start at the busiest root.
  $: if (layout === 'tree' && !selected && treeRows.length > 0) selected = treeRows[0].row

  // The history is folded by name, so a row can stand for several pids (nginx
  // workers, the agent and its helper). The label carries the pid of the
  // busiest of them — the one that produced the peak — with the lowest pid as a
  // tie-break so the same sample always names the same process.
  $: pidByName = (() => {
    const byName = new Map<string, number>()
    const cpu = new Map<string, number>()
    for (const row of rows) {
      const previous = cpu.get(row.name)
      if (previous == null || row.cpu_percent > previous || (row.cpu_percent === previous && row.pid < byName.get(row.name)!)) {
        cpu.set(row.name, row.cpu_percent)
        byName.set(row.name, row.pid)
      }
    }
    return byName
  })()

  $: heatmap = heatHistory ? buildHeatmap(heatHistory.buckets, { pids: pidByName }) : null
  $: hotCell = heatmap ? busiestCell(heatmap) : null
  $: allTicks = heatmap ? rulerTicks(heatmap) : []
  $: visibleTicks =
    hmViewport === 0
      ? allTicks
      : allTicks.filter((tick) => tick.left + 170 >= hmScroll + 2 && tick.left + 170 <= hmScroll + hmViewport - 46)

  function sparklinePoints(row: ProcessRow): number[] | null {
    if (!history) return null
    const series = history.series.find((ser) => ser.name === row.name)
    if (!series || series.points.length < 2) return null
    const nums = series.points.map((pt) => pt.cpu).filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
    return nums.length >= 2 ? nums : null
  }

  // 44 x 16, the size of the sparkline cell in the table.
  function sparkPath(points: number[]): string {
    const finite = points.filter((v) => Number.isFinite(v))
    const max = Math.max(1, ...finite)
    const step = 44 / Math.max(1, points.length - 1)
    let d = ''
    let started = false
    points.forEach((v, i) => {
      if (!Number.isFinite(v)) {
        started = false
        return
      }
      const yy = 14 - 12 * (v / max)
      d += (started ? ' L' : 'M') + (i * step).toFixed(1) + ' ' + yy.toFixed(1)
      started = true
    })
    return d
  }

  // The same line closed against the baseline: a faint area under the curve
  // reads as a plot even when the series is flat at zero.
  function sparkArea(points: number[]): string {
    const line = sparkPath(points)
    return line ? `${line} L44 15 L0 15 Z` : ''
  }

  function eventSeverity(type: string): string {
    if (type === 'oom_killed') return 'sevcrit'
    if (type === 'exited') return 'sevwarn'
    return 'sevinfo'
  }

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
    if (group === 'tree') {
      const next = new Set(collapsed)
      if (next.has(pid)) next.delete(pid)
      else next.add(pid)
      collapsed = next
      return
    }
    const next = new Set(expanded)
    if (next.has(pid)) next.delete(pid)
    else next.add(pid)
    expanded = next
  }

  function select(row: ProcessRow) {
    selected = row
  }

  function inspect(row: ProcessRow) {
    selected = row
    drawerOpen = true
  }

  function closeDrawer() {
    drawerOpen = false
  }

  function applyView(id: string) {
    const saved = SAVED_VIEWS.find((v) => v.id === id)
    if (!saved) return
    if (activeView === id) {
      // A chip that is already on turns the view off again, instead of leaving
      // a preset applied that no chip claims.
      activeView = null
      search = ''
      states = []
      hideKernel = true
      listeningOnly = false
      user = ''
      unit = ''
      container = ''
      windowNames = null
      windowRange = null
      windowCount = 0
      return
    }
    activeView = id
    search = saved.filter.search ?? ''
    states = saved.filter.states ?? []
    hideKernel = saved.filter.hideKernel ?? true
    listeningOnly = saved.filter.listeningOnly ?? false
    user = ''
    unit = ''
    container = ''
    windowNames = null
    windowRange = null
    windowCount = 0
    if (saved.sort) {
      sortKey = saved.sort.key
      sortDir = saved.sort.dir
    }
  }

  function clearWindow() {
    windowNames = null
    windowRange = null
  }

  // ---- search popover ----
  // The query is a corner tool, not a row of the toolbar: it opens on demand,
  // filters as it is typed, and leaves a dot on the icon so an active filter is
  // never invisible.
  async function openSearch() {
    searchOpen = true
    await tick()
    searchBox?.focus()
    searchBox?.select()
  }

  function closeSearch() {
    searchOpen = false
  }

  function toggleSearch() {
    if (searchOpen) closeSearch()
    else void openSearch()
  }

  function onSearchKey(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault()
      closeSearch()
    } else if (event.key === 'Enter') {
      // Enter keeps the query and gives the list back its keys.
      event.preventDefault()
      closeSearch()
    }
  }

  function clearSearch() {
    search = ''
    activeView = null
    void openSearch()
  }

  function onGlobalKey(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      if (viewsOpen) viewsOpen = false
      return
    }
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
      if (view !== 'live') return
      event.preventDefault()
      if (searchOpen) searchBox?.focus()
      else void openSearch()
    }
  }

  $: activeViewLabel = SAVED_VIEWS.find((saved) => saved.id === activeView)?.label ?? 'Views'

  function openWindow(index: number) {
    if (!heatmap) return
    const column = heatmap.columns[index]
    if (!column) return
    const names = heatmap.rows.filter((row) => row.cells[index] != null).map((row) => row.name)
    if (names.length === 0) return
    windowNames = names
    // A name can cover several pids (nginx workers, the agent and its helper);
    // the banner counts the rows it will actually show.
    windowCount = rows.filter((row) => names.includes(row.name)).length
    windowRange = { from: column.from, to: column.to }
    view = 'live'
  }

  // One ruler tick every four hours, plus "now" at the right edge. A tick that
  // would run into the right edge is dropped rather than drawn over "now".
  function rulerTicks(map: { columns: { from: number; to: number }[] }): {
    left: number
    label: string
    now: boolean
  }[] {
    const width = map.columns.length * HEAT_CELL_WIDTH
    const ticks = heatTicks(map.columns)
      .filter((tick) => tick.left < width - 110)
      .map((tick) => ({ left: tick.left, label: tick.label, now: false }))
    ticks.push({ left: width - 26, label: 'now', now: true })
    return ticks
  }

  function onHeatScroll() {
    hmScroll = hmwrap?.scrollLeft ?? 0
    hmViewport = hmwrap?.clientWidth ?? 0
  }

  // The grid is wider than the card and its right edge is "now". The action
  // runs per mount, so leaving History and coming back anchors on the newest
  // bars again instead of opening on 23 empty hours.
  function anchorRight(node: HTMLElement) {
    requestAnimationFrame(() => {
      node.scrollLeft = node.scrollWidth
      onHeatScroll()
    })
  }

  function sampleClock(at: string | undefined): string {
    const date = at ? new Date(at) : new Date()
    if (Number.isNaN(date.getTime())) return formatClock(Date.now() / 1000)
    return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
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

  let historyFetchedAt = 0

  async function loadHistory(force = false) {
    if (!id) return
    if (!force && history && Date.now() - historyFetchedAt < 30_000) return
    historyLoading = true
    historyError = ''
    try {
      history = await fetchProcessHistory(id, historyRange, { headers: authHeaders })
      historyFetchedAt = Date.now()
      if (!historyDefaultSet && historyRange === '24h' && history?.buckets?.length) {
        historyDefaultSet = true
        const newest = Math.max(...history.buckets.map((b) => b.t))
        if (Number.isFinite(newest) && Date.now() / 1000 - newest < 7200) {
          historyRange = '1h'
          void loadHistory(true)
          return
        }
      } else if (!historyDefaultSet) {
        historyDefaultSet = true
      }
    } catch (e) {
      historyError = e instanceof Error ? e.message : 'Could not load history'
      history = null
      historyDefaultSet = true
    } finally {
      historyLoading = false
    }
  }

  // The heatmap is always the same 24 hours, whatever the spike chart is set
  // to: a grid whose width moved with the range would make two days
  // incomparable. This is also why it has its own fetch.
  async function loadHeatmap() {
    if (!id) return
    heatLoading = true
    heatError = ''
    try {
      heatHistory = await fetchProcessHistory(id, '24h', { headers: authHeaders })
    } catch (e) {
      heatError = e instanceof Error ? e.message : 'Could not load history'
      heatHistory = null
    } finally {
      heatLoading = false
    }
  }

  async function doSignal(row: ProcessRow, signal: ProcessSignal) {
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
    killTarget = row
    killBlockedReason = snapshot ? signalBlockReason(row, snapshot) : null
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

  // ---- spike chart ----
  $: historyLabels = history
    ? Array.from(new Set(history.series.flatMap((s) => s.points.map((p) => new Date(p.t * 1000).toISOString())))).sort()
    : []
  // Only the busiest handful stay on the plot: twelve overlapping lines with the
  // same weight explain nothing. The server already sorts by peak.
  $: historySeries = history
    ? history.series.slice(0, 6).map((s, i) => {
        const byTime = new Map(s.points.map((p) => [p.t, p.cpu]))
        return {
          label: s.name,
          values: historyLabels.map((label) => {
            const t = Math.floor(Date.parse(label) / 1000)
            return byTime.has(t) ? byTime.get(t)! : null
          }),
          color: `var(--chart-${(i % 5) + 1})`
        }
      })
    : []
  // Two aggregate lines beat six flat ones: on a real host the IO of the top
  // processes is near zero most of the time, and per-process lines at that
  // scale are six overlapping baselines. Sum of the same top names, direction
  // kept apart, answers "was the box moving bytes".
  $: historyIOSeries = (() => {
    if (!history || historyLabels.length === 0) return []
    const top = history.series.slice(0, 6)
    const sumAt = (key: 'ioRead' | 'ioWrite') =>
      historyLabels.map((label) => {
        const t = Math.floor(Date.parse(label) / 1000)
        let total: number | null = null
        for (const series of top) {
          const point = series.points.find((p) => p.t === t)
          const value = point ? point[key] : null
          if (value == null) continue
          total = (total ?? 0) + value
        }
        return total == null ? null : total / 1024
      })
    const read = sumAt('ioRead')
    const write = sumAt('ioWrite')
    const out: { label: string; values: (number | null)[]; color: string }[] = []
    if (read.some((v) => v != null)) out.push({ label: 'read KB/s', values: read, color: 'var(--chart-1)' })
    if (write.some((v) => v != null)) out.push({ label: 'write KB/s', values: write, color: 'var(--chart-3)' })
    return out
  })()
  $: historyWindowEnd = historyLabels.length ? Math.max(...historyLabels.map((l) => Date.parse(l))) : null
  $: windowLabel = windowRange ? bucketRangeLabel(windowRange.from, windowRange.to - windowRange.from) : ''

  $: if (id) void load()

  $: if (view === 'history' && id) void loadHistory()

  $: if (view === 'history' && id) void loadHeatmap()

  function setHistoryRange(next: HistoryRange) {
    historyRange = next
    void loadHistory(true)
  }

  onMount(() => {
    if (refreshTimer) clearInterval(refreshTimer)
    refreshTimer = setInterval(() => void load(), 30_000)
    // The inspector's timeline and the table's sparklines read this history;
    // fetching it only when the History tab opens left both blank on a first
    // visit to the tree layout.
    void loadHistory()
    window.addEventListener('keydown', onGlobalKey)
  })

  onDestroy(() => {
    if (refreshTimer) clearInterval(refreshTimer)
    window.removeEventListener('keydown', onGlobalKey)
  })
</script>

<section class="procs" class:fill={view === 'live' && !!snapshot}>
  {#if actionError}
    <div class="error-banner" role="alert">{actionError}<button class="retry" on:click={() => (actionError = '')}>×</button></div>
  {/if}

  <!-- The view switch rides inside the live toolbar. History has no toolbar (its
       controls are per-panel), so it keeps the row to itself. -->
  {#snippet viewSwitch()}
    <span class="segmented" role="group" aria-label="View">
      <button class:active={view === 'live'} on:click={() => (view = 'live')}>Live</button>
      <button class:active={view === 'history'} on:click={() => (view = 'history')}>History</button>
    </span>
  {/snippet}

  {#if view === 'history'}
    <div class="subnav">{@render viewSwitch()}</div>
  {/if}

  {#if hw && (!stats || legacyMode)}
    <div class="status-strip">
      {#if processSummary}
        <span>{reported.length} reported of {processSummary.total} processes</span>
      {:else}
        <span class="warn-text">Counts are the reported subset; this agent predates the full process tally.</span>
      {/if}
    </div>
  {/if}

  {#if loading && !loaded}
    <p class="loading">Loading processes…</p>
  {:else if error && !snapshot}
    <div class="error-banner" role="alert">{error}<button class="retry" on:click={load}>Retry</button></div>
  {:else if !snapshot?.collected && !legacyMode}
    <EmptyState
      icon="cog"
      title="Not collected"
      message="This agent did not report processes."
      actionLabel="Refresh"
      onAction={load}
    />
  {:else if view === 'live'}
    <!-- One panel: toolbar, summary, list. Everything above the list is one
         compact row, so the rows own the viewport. -->
    <div class="card panel">
      <div class="toolbar">
        <div class="tgroup filters">
        {@render viewSwitch()}
        <span class="vsep"></span>
        {#each STATE_CHIPS as state (state)}
          <button
            class="chip"
            class:active={states.includes(state)}
            title={STATE_LABELS[state]}
            on:click={() => toggleState(state)}
          ><span class="dot" style:background={STATE_DOT[state]}></span>{state}<span class="n">{counts[state]}</span></button>
        {/each}
        <button class="chip" class:active={listeningOnly} on:click={() => (listeningOnly = !listeningOnly)} title="Listening sockets only">
          <Icon name="pulse" size={10} />listening
        </button>
        <button class="chip" class:active={hideKernel} on:click={() => (hideKernel = !hideKernel)} title={hideKernel ? 'Kernel threads are hidden' : 'Kernel threads are shown'}>
          kernel
        </button>
        {#if options.users.length}
          <select bind:value={user} aria-label="All users">
            <option value="">All users</option>
            {#each options.users as value (value)}<option value={value}>{value}</option>{/each}
          </select>
        {/if}
        {#if options.units.length}
          <select bind:value={unit} aria-label="All units">
            <option value="">All units</option>
            {#each options.units as value (value)}<option value={value}>{value}</option>{/each}
          </select>
        {/if}
        {#if options.containers.length}
          <select bind:value={container} aria-label="All containers">
            <option value="">All containers</option>
            {#each options.containers as value (value)}<option value={value}>{value}</option>{/each}
          </select>
        {/if}
        {#if windowNames}
          <span class="chip on wchip" title="Filtered to a heatmap window">
            <Icon name="pulse" size={10} />{windowLabel}<span class="n">{windowCount}</span>
            <button class="chipx" aria-label="Clear window" on:click={clearWindow}><Icon name="x" size={9} /></button>
          </span>
        {/if}
        </div>
        <div class="tgroup actions">
        <span class="segmented" role="group" aria-label="Group">
          {#each GROUP_BUTTONS as option (option.id)}
            <button class:active={group === option.id} title={option.title} on:click={() => (group = option.id)}>{option.label}</button>
          {/each}
        </span>
        <span class="segmented" role="group" aria-label="Layout">
          <button class:active={layout === 'table'} on:click={() => (layout = 'table')}>Pro table</button>
          <button class:active={layout === 'tree'} on:click={() => (layout = 'tree')}>Tree + inspector</button>
        </span>
        <button
          class="iconbtn"
          title={density === 'compact' ? 'Comfy rows' : 'Compact rows'}
          aria-label={density === 'compact' ? 'Comfy rows' : 'Compact rows'}
          on:click={() => (density = density === 'comfy' ? 'compact' : 'comfy')}
        ><Icon name="list" size={13} /></button>
        <div class="menuwrap" role="presentation" on:click|stopPropagation>
          <button class="btn" class:on={activeView !== null} aria-haspopup="menu" aria-expanded={viewsOpen} on:click={() => (viewsOpen = !viewsOpen)}>
            {activeViewLabel}<Icon name="chevron" size={10} />
          </button>
          {#if viewsOpen}
            <div class="vmenu" role="menu" aria-label="Saved views">
              {#each SAVED_VIEWS as saved (saved.id)}
                <button
                  role="menuitemradio"
                  aria-checked={activeView === saved.id}
                  class:on={activeView === saved.id}
                  on:click={() => { applyView(saved.id); viewsOpen = false }}
                >{saved.label}</button>
              {/each}
            </div>
          {/if}
        </div>
        <span class="livepill" title="Live sample from the last refresh"><i></i>live {sampleClock(snapshot?.collected_at)}</span>
        <div class="searchwrap" role="presentation" on:click|stopPropagation>
          <button
            class="iconbtn sbtn"
            class:on={!!search}
            aria-label="Search"
            aria-expanded={searchOpen}
            title="Search · Ctrl K"
            on:click={toggleSearch}
          >
            <Icon name="search" size={13} />
            {#if search}<i class="smark"></i>{/if}
          </button>
          {#if searchOpen}
            <div class="spop">
              <input
                bind:this={searchBox}
                bind:value={search}
                type="text"
                aria-label="Search processes"
                placeholder="name · cmd · pid · user"
                autocomplete="off"
                spellcheck="false"
                on:keydown={onSearchKey}
                on:input={() => (activeView = null)}
              />
              {#if search}
                <button class="chipx" aria-label="Clear search" on:click={clearSearch}><Icon name="x" size={10} /></button>
              {/if}
            </div>
          {/if}
        </div>
        </div>
      </div>

      {#if stats}
        <div class="kpi-bar compact" aria-label="Process summary">
          <div class="sitem"><span class="k">Processes</span><span class="v">{stats.total}</span><span class="s">{filteredRows.length} shown</span></div>
          <div class="sitem"><span class="k">Threads</span><span class="v">{stats.threads}</span></div>
          <div class="sitem"><span class="k">Zombies</span><span class="v" class:warn={stats.zombie > 0}>{stats.zombie}</span></div>
          <div class="sitem"><span class="k">Running</span><span class="v">{stats.running}</span><span class="s">{stats.blocked} blocked</span></div>
          <div class="sitem" title="Load average · 1m / 5m / 15m">
            <span class="k">Load</span><span class="v">{stats.load1.toFixed(2)}</span><span class="s">{stats.load5.toFixed(2)} · {stats.load15.toFixed(2)}</span>
          </div>
          <div class="sitem" title="CPU pressure · some avg10">
            <span class="k">CPU pressure</span><span class="v">{stats.psi_cpu_some_avg10.toFixed(1)}%</span><span class="s">mem {stats.psi_mem_some_avg10.toFixed(1)}%</span>
          </div>
          <div class="sitem"><span class="k">OOM 24h</span><span class="v" class:crit={stats.oom_kills_24h > 0}>{stats.oom_kills_24h < 0 ? '—' : stats.oom_kills_24h}</span></div>
        </div>
      {:else if processSummary}
        <!-- Older agent: the whole-machine tally exists, the per-state detail does not. -->
        <div class="kpi-bar compact" aria-label="Process summary">
          <div class="sitem"><span class="k">Processes</span><span class="v">{processSummary.total}</span><span class="s">{reported.length} shown</span></div>
          <div class="sitem"><span class="k">Running</span><span class="v">{processSummary.running}</span></div>
          <div class="sitem"><span class="k">Sleeping</span><span class="v">{processSummary.sleeping}</span></div>
          <div class="sitem"><span class="k">Disk sleep</span><span class="v" class:warn={processSummary.disk_sleep > 0}>{processSummary.disk_sleep}</span></div>
          <div class="sitem"><span class="k">Stopped</span><span class="v" class:warn={processSummary.stopped > 0}>{processSummary.stopped}</span></div>
          <div class="sitem"><span class="k">Zombies</span><span class="v" class:warn={processSummary.zombie > 0}>{processSummary.zombie}</span></div>
          <div class="sitem"><span class="k">Threads</span><span class="v">{processSummary.threads}</span></div>
        </div>
      {/if}

      {#if layout === 'table'}
        <!-- process table -->
        <div class="pane">
          <div class="tablewrap" class:comfy={density === 'comfy'}>
            <table>
              <thead>
                <tr>
                  <th class="w-pid r sortable"><button type="button" on:click={() => toggleSort('pid')}>PID{sortKey === 'pid' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</button></th>
                  <th class="sortable"><button type="button" on:click={() => toggleSort('name')}>Name{sortKey === 'name' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</button></th>
                  <th class="w-user">User</th>
                  <th class="w-st">St</th>
                  <th class="w-cpu r sortable"><button type="button" on:click={() => toggleSort('cpu')}>CPU%{sortKey === 'cpu' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</button></th>
                  <th class="w-mem r sortable"><button type="button" on:click={() => toggleSort('rss')}>MEM{sortKey === 'rss' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</button></th>
                  <th class="w-io r sortable"><button type="button" on:click={() => toggleSort('io')}>IO r / w{sortKey === 'io' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</button></th>
                  <th class="w-thr r sortable"><button type="button" on:click={() => toggleSort('threads')}>THR{sortKey === 'threads' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</button></th>
                  <th class="w-up r sortable"><button type="button" on:click={() => toggleSort('uptime')}>Uptime{sortKey === 'uptime' ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}</button></th>
                  <th class="w-unit">Unit / ctnr</th>
                  <th class="w-qa"></th>
                </tr>
              </thead>
              <tbody>
                {#each tableLines as line (line.kind === 'group' ? `g:${line.group.label}` : `r:${line.entry.row.pid}`)}
                  {#if line.kind === 'group'}
                    <tr class="grp">
                      <td colspan="11">
                        <div class="grphead">
                          <span class="gname">{line.group.label}</span>
                          <span class="gstat">{line.group.rows.length} process{line.group.rows.length === 1 ? '' : 'es'} · Σ CPU {line.group.cpu.toFixed(1)}% · Σ RSS {formatBytes(line.group.rss)}</span>
                        </div>
                      </td>
                    </tr>
                  {:else}
                    {@const entry = line.entry}
                    {@const row = entry.row}
                    {@const share = memoryShare(row.rss_bytes, totalMemory || 1)}
                    <tr class="row" class:sel={selected?.pid === row.pid} on:click={() => inspect(row)}>
                      <td class="r"><span class="pid">{row.pid}</span><span class="ppid">·{row.ppid}</span></td>
                      <td>
                        <div class="namecell" style:padding-left={`${Math.min(entry.depth, 4) * 0.9}rem`}>
                          {#if entry.hasChildren}
                            <button class="exp" on:click|stopPropagation={() => toggleExpand(row.pid)} aria-label={entry.expanded ? 'Collapse' : 'Expand'}>
                              {entry.expanded ? '▾' : '▸'}
                            </button>
                          {:else}
                            <span class="exp leaf"></span>
                          {/if}
                          <div class="nwrap">
                            <span class="pname" title={row.name}>{row.name}</span>
                            {#if row.cmd}<span class="pcmd" title={row.cmd}>{row.cmd}</span>{/if}
                          </div>
                        </div>
                      </td>
                      <td class="mono user">{row.user || '—'}</td>
                      <td><span class="statebadge state-{row.state}">{row.state}</span></td>
                      <td class="r">
                        <div class="cpuwrap">
                          <span class="cpuval" class:hot={row.cpu_percent >= 80}>{formatCpuPercent(row.cpu_percent)}</span>
                          {#if sparklinePoints(row)}
                            <svg class="spark" width="44" height="16" viewBox="0 0 44 16" aria-hidden="true">
                              <path d={sparkArea(sparklinePoints(row)!)} fill="rgba(96, 165, 250, 0.18)" stroke="none" />
                              <path d={sparkPath(sparklinePoints(row)!)} fill="none" stroke="var(--series-1)" stroke-width="1.2" stroke-linejoin="round" />
                            </svg>
                          {/if}
                        </div>
                      </td>
                      <td class="r">
                        <span class="num">{formatBytes(row.rss_bytes)}</span>
                        <span class="membar" title="{share.toFixed(1)}% of memory"><i style="width:{Math.min(100, Math.max(2, share * 1.8))}%"></i></span>
                        <span class="memsmall num" title={row.swap_bytes ? `swap ${formatBytes(row.swap_bytes)}` : undefined}>{share.toFixed(1)}%</span>
                      </td>
                      <td class="r"><span class="ioval">{ioText(row, ioAvailable)}</span></td>
                      <td class="r num">{row.threads || '—'}</td>
                      <td class="r num">{formatUptime(row.uptime_seconds)}</td>
                      <td>
                        {#if row.container}
                          <span class="unitchip ctnr" title="Container: {row.container}">{row.container}</span>
                        {:else if row.unit}
                          <span class="unitchip" title={row.unit}>{row.unit}</span>
                        {:else}
                          <span class="unitchip" style="border-style:dashed" title="no systemd unit">no unit</span>
                        {/if}
                      </td>
                      <td class="qacell">
                        <span class="qa">
                          <button class="qab" title="TERM" on:click|stopPropagation={() => doSignal(row, 'TERM')}>T</button>
                          <button class="qab crit" title="KILL" on:click|stopPropagation={() => openKill(row)}>K</button>
                          <button class="qab" title="STOP" on:click|stopPropagation={() => doSignal(row, 'STOP')}>S</button>
                          <button class="qab" title="CONT" on:click|stopPropagation={() => doSignal(row, 'CONT')}>C</button>
                        </span>
                      </td>
                    </tr>
                  {/if}
                {/each}
              </tbody>
            </table>
            {#if filteredRows.length === 0}
              <p class="count-line pad">No processes match.</p>
            {/if}
          </div>
        </div>
      {:else}
        <!-- tree + inspector -->
        <div class="split2">
          <div class="pane-card">
            <div class="pane-head">
              <h2>Process tree</h2>
              <span class="hint">{treeRows.length} shown · click to inspect</span>
            </div>
            <div class="treewrap">
              <div class="treegrid">
                <div class="tline thead" aria-hidden="true">
                  <span class="tn">Process</span>
                  <span class="tpid">PID</span>
                  <span class="tcpu">CPU</span>
                  <span class="tmem">MEM</span>
                  <span class="tthr">THR</span>
                  <span class="tio">IO r/w</span>
                  <span class="tup">UP</span>
                </div>
                {#each treeRows as entry (entry.row.pid)}
                  <button
                    class="tline pnode"
                    class:sel={selected?.pid === entry.row.pid}
                    on:click={() => select(entry.row)}
                  >
                    <span class="tn" style:padding-left={`${0.5 + Math.min(entry.depth, 8) * 0.85}rem`}>
                      <span class="sdot sdot-{entry.row.state}" title={STATE_LABELS[entry.row.state]}></span>
                      <span class="n" title={entry.row.name}>{entry.row.name}</span>
                    </span>
                    <span class="tpid">{entry.row.pid}</span>
                    <span class="tcpu" class:hot={entry.row.cpu_percent >= 80}>{formatCpuPercent(entry.row.cpu_percent)}</span>
                    <span class="tmem">{formatBytes(entry.row.rss_bytes)}</span>
                    <span class="tthr">{entry.row.threads || '—'}</span>
                    <span class="tio">{ioText(entry.row, ioAvailable)}</span>
                    <span class="tup">{formatUptime(entry.row.uptime_seconds)}</span>
                  </button>
                {/each}
                {#if treeRows.length === 0}
                  <p class="empty-state">No matching processes</p>
                {/if}
              </div>
            </div>
          </div>
          <div class="pane-card">
            <div class="pane-head"><h2>Inspector</h2>{#if selected}<span class="hint">{selected.name} · {selected.pid}</span>{/if}</div>
            <div class="inspector">
              {#if selected}
                <ProcessDetail
                  row={selected}
                  rows={rows}
                  history={history}
                  historyRange={historyRange}
                  ioAvailable={ioAvailable}
                  signalsEnabled={signalsEnabled}
                  onSelect={select}
                  onRangeChange={setHistoryRange}
                  onSignal={doSignal}
                  onKill={openKill}
                  onRenice={doRenice}
                  onIonice={doIonice}
                />
              {:else}
                <div class="empty"><div class="et">No process selected</div></div>
              {/if}
            </div>
          </div>
        </div>
      {/if}
    </div>

  {:else}
    <!-- History -->
    {#if heatLoading && !heatHistory && historyLoading && !history}
      <p class="loading">Loading history…</p>
    {:else}
      <div class="histgrid">
        <!-- left: lifecycle events -->
        <div class="card">
          <div class="card-head"><h2>Lifecycle events · 24 h</h2><span class="hint">started / exited / OOM-killed</span></div>
          {#if historyError}
            <div class="error-banner" role="alert">{historyError}<button class="retry" on:click={() => loadHistory(true)}>Retry</button></div>
          {:else if history && history.events.length > 0}
            <div class="tlwrap">
              <ul class="tl">
                {#each collapseEvents(history.events).slice(-40).reverse() as event (event.t + ':' + event.pid + ':' + event.type)}
                  <li>
                    <span class="t">{formatClock(event.t)}</span>
                    <span class="ic"><span class="mkdot" style:background={event.type === 'oom_killed' ? 'var(--crit)' : event.type === 'exited' ? 'var(--warn)' : 'var(--text-muted)'}></span></span>
                    <span class="txt {eventSeverity(event.type)}"><b>{event.name}</b> <span class="mono faint">pid {event.pid}</span> · {EVENT_LABELS[event.type]}{event.exit_code != null ? ` · exit ${event.exit_code}` : ''}{#if event.count > 1}<span class="cnt">×{event.count}</span>{/if}</span>
                  </li>
                {/each}
              </ul>
            </div>
          {:else}
            <p class="empty-state">no events in 24 h</p>
          {/if}
        </div>

        <div class="histright">
          <!-- heatmap -->
          <div class="card">
            <div class="card-head">
              <h2>Top CPU over time</h2>
              <span class="hint hm-peak">
                {#if hotCell}
                  busiest cell <b>{formatShare(hotCell.value)}%</b> · {hotCell.name} · {bucketRangeLabel(hotCell.from, hotCell.to - hotCell.from)}
                {:else}
                  collecting
                {/if}
              </span>
            </div>
            {#if heatError}
              <div class="error-banner" role="alert">{heatError}<button class="retry" on:click={loadHeatmap}>Retry</button></div>
            {:else if heatmap && heatmap.rows.length > 0}
              {@const hm = heatmap}
              <div class="hmwrap" use:anchorRight bind:this={hmwrap} on:scroll={onHeatScroll}>
                <div class="hm" style:min-width={`${170 + hm.columns.length * HEAT_CELL_WIDTH + 160}px`}>
                  <div class="hmticks">
                    {#each visibleTicks as tick (tick.label + tick.left)}
                      <span class="hmtick" class:now={tick.now} style:left={`${170 + tick.left}px`}>{tick.label}</span>
                    {/each}
                  </div>
                  {#each hm.rows as row (row.name)}
                    <div class="hmrow">
                      <span class="hmlabel" title={row.pid ? `${row.name} · ${row.pid}` : row.name}>
                        <span class="hn" title={row.name}>{row.name}</span>
                        {#if row.pid}<span class="hp">{row.pid}</span>{/if}
                      </span>
                      {#each row.cells as cell, index (index)}
                        <button
                          type="button"
                          tabindex="-1"
                          class="hmc"
                          class:empty={cell == null}
                          style:background={cell == null ? undefined : heatCellColor(cell, heatmap.max)}
                          title={cell == null
                            ? `${row.name} · not alive · ${bucketRangeLabel(hm.columns[index].from, HEAT_COLUMN_SECONDS)}`
                            : `${formatShare(cell)}% · ${bucketRangeLabel(hm.columns[index].from, HEAT_COLUMN_SECONDS)}`}
                          aria-label={`${row.name} ${bucketRangeLabel(hm.columns[index].from, HEAT_COLUMN_SECONDS)}`}
                          on:click={() => openWindow(index)}
                        ></button>
                      {/each}
                      <span class="hmsum">Σ {formatCoreMin(row.coreMin)} core-min · peak {formatShare(row.peak)}%</span>
                    </div>
                  {/each}
                </div>
              </div>
              <div class="hmlegend">
                <span class="sw"><i style="background:rgba(96,165,250,.15)"></i>quiet</span>
                <span class="sw"><i style="background:rgba(96,165,250,.85)"></i>busy</span>
                <span class="sw"><i style="background:var(--card-inset);border:1px solid var(--border)"></i>process not alive</span>
                <span class="hmnote">cell = avg CPU · colour = peak of the window · {hm.columns.length} × 5-min buckets · click opens Live</span>
              </div>
            {:else}
              <p class="empty-state">collecting</p>
            {/if}
          </div>

          <!-- spike investigation -->
          <div class="card">
            <div class="card-head">
              <h2>Spike investigation</h2>
              <span class="hint">CPU and IO · top processes</span>
              <span class="segmented" role="group" aria-label="History range">
                {#each HISTORY_RANGES as range (range)}
                  <button class:active={historyRange === range} on:click={() => setHistoryRange(range)}>{range}</button>
                {/each}
              </span>
            </div>
            <div class="chart-wrap">
              <TimeSeriesChart
                labels={historyLabels}
                series={historySeries}
                windowMs={historyWindowMs(historyRange)}
                windowEnd={historyWindowEnd}
                height={180}
                compactLegend={true}
                loading={historyLoading && !history}
              />
              {#if historySeries.length === 0}
                <p class="empty-state">collecting</p>
              {/if}
            </div>
            <div class="chart-wrap io-panel">
              <TimeSeriesChart
                labels={historyLabels}
                series={historyIOSeries}
                leftUnit="KB/s"
                windowMs={historyWindowMs(historyRange)}
                windowEnd={historyWindowEnd}
                height={130}
                compactLegend={true}
                loading={historyLoading && !history}
                emptyMessage="IO is not recorded over time."
              />
            </div>
          </div>
        </div>
      </div>
    {/if}
  {/if}

  <!-- drawer (table layout) -->
  {#if drawerOpen && selected}
    <div class="backdrop" role="presentation" on:click={closeDrawer}></div>
    <aside class="drawer" aria-label="Process details">
      <ProcessDetail
        row={selected}
        rows={rows}
        history={history}
        historyRange={historyRange}
        ioAvailable={ioAvailable}
        signalsEnabled={signalsEnabled}
        showHeader
        showClose
        onClose={closeDrawer}
        onSelect={select}
        onRangeChange={setHistoryRange}
        onSignal={doSignal}
        onKill={openKill}
        onRenice={doRenice}
        onIonice={doIonice}
      />
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
  /* The screen is a column: everything above the list is chrome, the list takes
     the rest of the viewport. fill is only on in the live view — History is a
     page that scrolls. */
  .procs { display: grid; gap: 0.6rem; align-content: start; }
  .procs.fill { display: flex; flex-direction: column; height: 100%; min-height: 0; gap: 0.5rem; }
  .procs.fill > .panel { flex: 1; min-height: 0; }
  .subnav { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; }
  .segmented { display: inline-flex; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; flex: none; }
  .segmented button { padding: 0.14rem 0.36rem; background: transparent; border: 0; border-right: 1px solid var(--border); color: var(--text-faint); font-size: 10.5px; font-weight: 600; cursor: pointer; white-space: nowrap; }
  .segmented button:last-child { border-right: 0; }
  .segmented button:hover { color: var(--text); }
  .segmented button.active { background: var(--accent-dim); color: var(--accent); }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; box-shadow: var(--shadow-card); }
  /* The live panel is one card: toolbar / summary / list. It cannot clip, or the
     search and saved-view popovers would be cut off, so the list does its own
     rounding. */
  .card.panel { display: grid; grid-template-rows: auto auto minmax(0, 1fr); min-height: 0; overflow: visible; }
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; padding: 0.65rem 0.9rem; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .hint { color: var(--text-faint); font-size: 0.68rem; }
  .toolbar { display: flex; align-items: center; gap: 0.2rem 0.35rem; padding: 0.28rem 0.35rem; flex-wrap: wrap; position: relative; z-index: 6; border-bottom: 1px solid var(--border); }
  /* Two clusters: filters on the left, view controls and the corner tools on the
     right. They stay together as a block when the row wraps, so the second line
     never looks like three orphaned buttons. */
  .tgroup { display: flex; align-items: center; gap: 0.2rem; flex-wrap: wrap; min-width: 0; }
  .tgroup.actions { margin-left: auto; justify-content: flex-end; }
  .vsep { width: 1px; height: 16px; background: var(--border); flex: none; margin: 0 0.15rem; }
  .count-line { color: var(--text-faint); font-size: 0.68rem; }
  .btn { display: inline-flex; align-items: center; gap: 0.25rem; padding: 0.16rem 0.42rem; background: transparent; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-muted); font: inherit; font-size: 10.5px; cursor: pointer; transition: color var(--t-fast), border-color var(--t-fast); white-space: nowrap; }
  .btn:hover:not(:disabled) { border-color: var(--accent-border); color: var(--text); }
  .btn:disabled { opacity: 0.45; cursor: default; }
  .btn.crit { border-color: rgba(248, 113, 113, 0.45); color: var(--crit); }
  .btn.on { border-color: var(--accent-border); color: var(--accent); }
  .iconbtn { display: inline-flex; align-items: center; justify-content: center; width: 23px; height: 22px; padding: 0; background: transparent; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-muted); cursor: pointer; flex: none; position: relative; }
  .iconbtn:hover { color: var(--text); border-color: var(--border-strong); }
  .iconbtn.on { color: var(--accent); border-color: var(--accent-border); background: var(--accent-dim); }
  .chip { display: inline-flex; align-items: center; gap: 0.16rem; padding: 0.1rem 0.32rem; background: transparent; border: 1px solid var(--border); border-radius: 999px; color: var(--text-muted); font: inherit; font-size: 10.5px; cursor: pointer; white-space: nowrap; }
  .chip:hover { border-color: var(--accent-border); }
  .chip.active, .chip.on { background: var(--accent-dim); border-color: var(--accent-border); color: var(--text); }
  .chip .dot { width: 0.4rem; height: 0.4rem; border-radius: 50%; background: var(--text-faint); flex: none; }
  .chip .n { color: var(--text-faint); font-variant-numeric: tabular-nums; }
  .chip.active .n { color: var(--accent); }
  .wchip { color: var(--text); gap: 0.3rem; }
  .chipx { display: inline-flex; align-items: center; justify-content: center; width: 14px; height: 14px; padding: 0; border: 0; border-radius: 50%; background: rgba(148, 163, 184, 0.16); color: inherit; cursor: pointer; }
  .chipx:hover { background: rgba(148, 163, 184, 0.3); }
  .toolbar select { max-width: 6.5rem; padding: 0.1rem 0.25rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-muted); font: inherit; font-size: 10.5px; }
  .menuwrap, .searchwrap { position: relative; flex: none; }
  .vmenu { position: absolute; top: calc(100% + 4px); right: 0; z-index: 30; display: grid; min-width: 8rem; padding: 0.2rem; background: var(--bg-elevated); border: 1px solid var(--border-strong); border-radius: var(--radius-sm); box-shadow: var(--shadow-pop); }
  .vmenu button { padding: 0.28rem 0.45rem; background: transparent; border: 0; border-radius: 6px; color: var(--text-muted); font: inherit; font-size: 0.72rem; text-align: left; cursor: pointer; }
  .vmenu button:hover { background: var(--card-hover); color: var(--text); }
  .vmenu button.on { color: var(--accent); }
  .spop { position: absolute; top: calc(100% + 5px); right: 0; z-index: 30; display: flex; align-items: center; gap: 0.25rem; padding: 0.25rem; background: var(--bg-elevated); border: 1px solid var(--border-strong); border-radius: var(--radius-sm); box-shadow: var(--shadow-pop); }
  .spop input { width: 14rem; padding: 0.22rem 0.45rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: 6px; color: var(--text); font: inherit; font-size: 0.72rem; }
  .spop input:focus { outline: none; border-color: var(--accent-border); }
  .smark { position: absolute; top: 2px; right: 2px; width: 5px; height: 5px; border-radius: 50%; background: var(--accent); }
  .livepill { display: inline-flex; align-items: center; gap: 0.3rem; padding: 0.1rem 0.45rem; border-radius: 999px; border: 1px solid var(--accent-border); background: var(--accent-dim); color: var(--accent); font-size: 10px; font-weight: 700; font-variant-numeric: tabular-nums; white-space: nowrap; }
  .livepill i { width: 0.36rem; height: 0.36rem; border-radius: 50%; background: var(--accent); animation: pulse 2s ease infinite; }
  @keyframes pulse { 0%, 100% { opacity: 1 } 50% { opacity: 0.35 } }
  /* Summary: one line of label + value. Nothing here is taller than 56px, and no
     item carries a caption that is not a number. */
  .kpi-bar { display: flex; align-items: center; flex-wrap: wrap; border-bottom: 1px solid var(--border); }
  .kpi-bar.compact .sitem { display: flex; align-items: baseline; gap: 0.35rem; padding: 0.26rem 0.6rem; min-width: 0; white-space: nowrap; }
  .kpi-bar .sitem + .sitem { border-left: 1px solid var(--border); }
  .kpi-bar .k { color: var(--text-faint); font-size: 9px; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; }
  .kpi-bar .v { color: var(--text-bright); font-size: 0.82rem; font-weight: 600; font-variant-numeric: tabular-nums; }
  .kpi-bar .v.warn { color: var(--warn); }
  .kpi-bar .v.crit { color: var(--crit); }
  .kpi-bar .s { color: var(--text-faint); font-size: 9.5px; font-variant-numeric: tabular-nums; }
  .pane { min-height: 0; overflow: hidden; border-radius: 0 0 var(--radius) var(--radius); }
  .tablewrap { height: 100%; min-height: 0; overflow: auto; }
  /* Fixed layout: every column keeps its share and the name truncates, so no
     column is ever pushed out of the card at 1480. */
  table { width: 100%; border-collapse: separate; border-spacing: 0; table-layout: fixed; font-size: 11.5px; }
  thead th { position: sticky; top: 0; z-index: 2; background: var(--card); text-align: left; padding: 0.28rem 0.5rem; color: var(--text-faint); font-size: 9px; font-weight: 700; letter-spacing: 0.07em; text-transform: uppercase; border-bottom: 1px solid var(--border); white-space: nowrap; user-select: none; }
  thead th.r { text-align: right; }
  thead th.sortable { cursor: pointer; }
  thead th.sortable button { background: transparent; border: 0; padding: 0; margin: 0; font: inherit; color: inherit; letter-spacing: inherit; text-transform: inherit; cursor: pointer; }
  thead th.sortable button:hover { color: var(--text); }
  .w-pid { width: 4.9rem; }
  .w-user { width: 5.2rem; }
  .w-st { width: 2.1rem; }
  .w-cpu { width: 7.4rem; }
  .w-mem { width: 8.6rem; }
  .w-io { width: 7.9rem; }
  .w-thr { width: 3rem; }
  .w-up { width: 3.9rem; }
  .w-unit { width: 7.6rem; }
  .w-qa { width: 5.4rem; }
  td { padding: 0 0.5rem; border-bottom: 1px solid var(--border); color: var(--text); vertical-align: middle; white-space: nowrap; overflow: hidden; height: 27px; text-overflow: ellipsis; }
  .cpuwrap { display: flex; align-items: center; justify-content: flex-end; gap: 0.25rem; }
  .tablewrap.comfy td { height: 36px; }
  tr.row { cursor: pointer; transition: background var(--t-fast); }
  tr.row:hover { background: var(--card-hover); }
  tr.row.sel { background: var(--accent-dim); box-shadow: inset 2px 0 0 var(--accent); }
  tr.grp { cursor: default; }
  tr.grp:hover { background: transparent; }
  .grphead { display: flex; align-items: center; gap: 0.6rem; padding: 0.35rem 0.55rem; background: var(--card-inset); border-bottom: 1px solid var(--border); font-size: 10.5px; }
  .grphead .gname { font-family: var(--mono); color: var(--text); font-weight: 600; }
  .grphead .gstat { color: var(--text-faint); }
  .num { font-variant-numeric: tabular-nums; }
  .r { text-align: right; font-variant-numeric: tabular-nums; }
  .mono { font-family: var(--mono); }
  .user { color: var(--text-muted); font-size: 10.5px; }
  .pid { font-family: var(--mono); color: var(--text-bright); font-size: 11px; }
  .ppid { font-family: var(--mono); color: var(--text-faint); font-size: 10px; margin-left: 0.3rem; }
  .namecell { display: flex; align-items: center; gap: 0.4rem; min-width: 0; }
  .nwrap { display: flex; align-items: baseline; gap: 0.4rem; min-width: 0; flex: 1 1 auto; }
  .pname { color: var(--text-bright); font-weight: 500; white-space: nowrap; flex: 0 0 auto; max-width: 11rem; overflow: hidden; text-overflow: ellipsis; }
  .pcmd { font-family: var(--mono); color: var(--text-faint); font-size: 9.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; flex: 1 1 auto; }
  .exp { width: 14px; height: 14px; flex: none; border: 0; background: transparent; color: var(--text-faint); cursor: pointer; padding: 0; display: inline-flex; align-items: center; justify-content: center; border-radius: 4px; font-size: 9px; }
  .exp:hover { color: var(--text); background: var(--card-inset); }
  .exp.leaf { visibility: hidden; }
  .statebadge { display: inline-flex; align-items: center; justify-content: center; min-width: 1.05rem; padding: 0.05rem 0.3rem; border-radius: 4px; font-size: 9.5px; font-weight: 700; font-family: var(--mono); }
  .state-R { background: var(--accent-dim); color: var(--accent); }
  .state-S { background: var(--card-inset); color: var(--text-muted); }
  .state-D { background: var(--warn-dim); color: var(--warn); }
  .state-Z { background: var(--crit-dim); color: var(--crit); }
  .state-T, .state-I { background: var(--card-inset); color: var(--text-faint); }
  .cpuval { font-variant-numeric: tabular-nums; font-weight: 600; }
  .cpuval.hot { color: var(--warn); }
  .spark { display: block; width: 44px; height: 16px; flex: none; }
  .membar { display: inline-block; width: 28px; height: 4px; border-radius: 999px; background: rgba(148, 163, 184, 0.15); overflow: hidden; vertical-align: middle; margin: 0 0.26rem; }
  .membar i { display: block; height: 100%; background: var(--series-4, #22d3ee); border-radius: inherit; }
  .memsmall { color: var(--text-faint); font-size: 9.5px; }
  .ioval { font-family: var(--mono); font-size: 10px; color: var(--text-muted); }
  .unitchip { display: inline-block; max-width: 100%; overflow: hidden; text-overflow: ellipsis; padding: 0.05rem 0.4rem; border: 1px solid var(--border); border-radius: 999px; font-family: var(--mono); font-size: 9.5px; color: var(--text-muted); vertical-align: middle; }
  .unitchip.ctnr { border-color: rgba(96, 165, 250, 0.35); color: var(--series-1, #60a5fa); }
  .qacell { padding-right: 0.4rem; }
  .qa { display: flex; gap: 0.18rem; justify-content: flex-end; opacity: 0; transition: opacity var(--t-fast); }
  tr.row:hover .qa, tr.row.sel .qa { opacity: 1; }
  @media (hover: none) { .qa { opacity: 0.75 } }
  .qab { width: 18px; height: 18px; display: inline-flex; align-items: center; justify-content: center; border: 1px solid var(--border); border-radius: 5px; background: var(--card-inset); color: var(--text-muted); cursor: pointer; padding: 0; font-size: 9px; }
  .qab:hover { color: var(--text); border-color: var(--border-strong); }
  .qab.crit:hover { color: var(--crit); border-color: rgba(248, 113, 113, 0.5); }
  /* Tree layout: the same panel fills the viewport, split into the tree and the
     persistent inspector. Both panes scroll inside their own card. */
  .split2 { display: grid; grid-template-columns: minmax(0, 1.12fr) minmax(0, 1fr); gap: 0.5rem; min-height: 0; }
  @media (max-width: 960px) { .split2 { grid-template-columns: 1fr; } }
  .pane-card { display: grid; grid-template-rows: auto minmax(0, 1fr); min-height: 0; background: var(--card); overflow: hidden; }
  .pane-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; min-height: 26px; padding: 0.2rem 0.6rem; border-bottom: 1px solid var(--border); }
  .pane-head h2 { margin: 0; font-size: 0.74rem; font-weight: 600; color: var(--text); }
  .treewrap { overflow: auto; min-height: 0; }
  /* One column template for the header and every row, so the added THR/IO/UP
     columns line up without a table. */
  .treegrid { --cols: minmax(0, 1fr) 3rem 3.2rem 4.4rem 2rem 6.6rem 2.8rem; }
  .tline { display: grid; grid-template-columns: var(--cols); align-items: center; gap: 0.25rem; width: 100%; height: 24px; padding: 0 0.5rem; border: 0; border-bottom: 1px solid var(--border); background: transparent; color: var(--text); text-align: left; font: inherit; font-size: 10.5px; font-weight: 400; }
  .tline.thead { position: sticky; top: 0; z-index: 2; height: 22px; background: var(--card); color: var(--text-faint); font-size: 9px; font-weight: 700; letter-spacing: 0.07em; text-transform: uppercase; }
  button.tline { cursor: pointer; }
  button.tline:hover { background: var(--card-hover); }
  button.tline.sel { background: var(--accent-dim); box-shadow: inset 2px 0 0 var(--accent); }
  .tline .tn { display: flex; align-items: center; gap: 0.3rem; min-width: 0; }
  .tline .n { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-bright); }
  .tline .tpid, .tline .tio { font-family: var(--mono); font-size: 9.5px; color: var(--text-faint); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: right; }
  .tline.thead .tpid, .tline.thead .tio { font-family: inherit; font-size: 9px; color: inherit; letter-spacing: inherit; text-align: right; }
  .tline .tcpu, .tline .tmem, .tline .tthr, .tline .tup { font-variant-numeric: tabular-nums; color: var(--text-muted); text-align: right; }
  .tline.thead .tcpu, .tline.thead .tmem, .tline.thead .tthr, .tline.thead .tup { color: inherit; }
  .tline .tcpu.hot { color: var(--warn); }
  .sdot { width: 0.45rem; height: 0.45rem; border-radius: 50%; flex: none; }
  .sdot-R { background: var(--accent); }
  .sdot-S { background: #4b5563; }
  .sdot-D { background: var(--warn); }
  .sdot-Z { background: var(--crit); }
  .sdot-T, .sdot-I { background: var(--text-faint); }
  .inspector { padding: 0.6rem 0.8rem 0.9rem; overflow: auto; min-height: 0; }

  .empty { padding: 1.6rem 1rem; text-align: center; }
  .empty .et { color: var(--text); font-weight: 600; font-size: 0.8rem; }
  .empty-state { margin: 0; padding: 0.9rem; color: var(--text-faint); font-size: 0.78rem; }
  .loading { margin: 0; padding: 0.9rem; color: var(--text-faint); font-size: 0.78rem; }
  .error-banner { display: flex; align-items: center; gap: 0.6rem; padding: 0.5rem 0.75rem; background: var(--crit-dim); color: var(--crit); font-size: 0.76rem; border-radius: var(--radius-sm); }
  .retry { margin-left: auto; padding: 0.2rem 0.5rem; background: transparent; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font: inherit; font-size: 0.72rem; cursor: pointer; }
  .pad { padding: 0.9rem; }
  .status-strip { color: var(--text-faint); font-size: 0.7rem; }
  .warn-text { color: var(--warn); }

  .backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); z-index: 40; }
  .drawer { position: fixed; top: 0; right: 0; height: 100vh; width: min(460px, 94vw); background: var(--bg-elevated); border-left: 1px solid var(--border); box-shadow: var(--shadow-pop); z-index: 41; overflow-y: auto; padding: 0.9rem; animation: slide-in 180ms ease; }
  @keyframes slide-in { from { transform: translateX(16px); opacity: 0 } to { transform: none; opacity: 1 } }
  .actions { display: flex; gap: 0.5rem; flex-wrap: wrap; }
  .blocked { color: var(--crit); font-size: 0.75rem; margin: 0; }
  .dialog { background: var(--bg-elevated); border: 1px solid var(--border); border-radius: var(--radius); padding: 1rem; width: min(420px, 90vw); display: grid; gap: 0.75rem; z-index: 41; position: relative; }
  .dialog input { padding: 0.45rem 0.6rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font-family: var(--mono); }
  .dialog h3 { margin: 0; color: var(--text-bright); font-size: 1rem; }

  /* ---- history ---- */
  .histgrid { display: grid; grid-template-columns: minmax(280px, 360px) minmax(0, 1fr); gap: 0.85rem; align-items: start; }
  @media (max-width: 960px) { .histgrid { grid-template-columns: 1fr; } }
  .histright { display: grid; gap: 0.85rem; min-width: 0; }
  .tl { list-style: none; margin: 0; padding: 0; }
  .tlwrap { max-height: 62vh; overflow-y: auto; }
  .tl li { display: grid; grid-template-columns: auto auto 1fr; gap: 0.55rem; padding: 0.4rem 0.9rem; border-bottom: 1px solid var(--border); align-items: baseline; }
  .tl .t { font-family: var(--mono); font-size: 0.68rem; color: var(--text-faint); white-space: nowrap; }
  .tl .ic { width: 1.5rem; display: flex; justify-content: center; }
  .tl .txt { font-size: 0.74rem; line-height: 1.5; color: var(--text-muted); }
  .tl .txt b { color: var(--text-bright); font-weight: 600; }
  .tl .faint { color: var(--text-faint); }
  .sevcrit { color: var(--crit); }
  .sevwarn { color: var(--warn); }
  .sevinfo { color: var(--text-muted); }
  .mkdot { width: 0.5rem; height: 0.5rem; border-radius: 50%; }
  .hmwrap { overflow-x: auto; padding: 0.2rem 0.6rem 0.7rem; }
  .hm { display: block; }
  .hmticks { display: block; height: 16px; position: relative; }
  .hmtick { position: absolute; top: 0; font-size: 10px; color: var(--text-muted); font-family: var(--mono); white-space: nowrap; }
  .hmtick.now { color: var(--text-faint); }
  .hmrow { display: flex; align-items: center; gap: 0; height: 21px; }
  .hmlabel { position: sticky; left: 0; z-index: 2; width: 170px; flex: none; background: var(--card); padding-right: 0.5rem; display: flex; align-items: center; gap: 0.35rem; font-size: 0.68rem; color: var(--text-muted); height: 21px; }
  .hmlabel .hn { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 7.6rem; }
  .hmlabel .hp { font-family: var(--mono); color: var(--text-faint); font-size: 0.62rem; }
  .hmc { width: 3px; height: 16px; margin-right: 1px; border-radius: 1px; flex: none; cursor: pointer; padding: 0; border: 0; }
  .hmc.empty { background: var(--card-inset); }
  /* The summary closes the row, after the bars, exactly where the prototype
     puts it. It is not sticky: pinned over the right edge it would cover the
     newest columns, which are the ones worth reading. */
  .hmsum { flex: none; margin-left: auto; padding-left: 0.6rem; display: flex; align-items: center; justify-content: flex-end; height: 21px; line-height: 21px; font-family: var(--mono); font-size: 0.62rem; color: var(--text-faint); white-space: nowrap; }
  .hmlegend { display: flex; align-items: center; gap: 0.5rem; padding: 0.5rem 0.9rem 0.7rem; font-size: 0.66rem; color: var(--text-faint); flex-wrap: wrap; }
  .hmlegend .sw { display: flex; align-items: center; gap: 0.2rem; }
  .hmlegend .sw i { width: 10px; height: 10px; border-radius: 2px; display: inline-block; }
  .hmlegend .hmnote { color: var(--text-faint); }
  .hm-peak { font-size: 0.72rem; color: var(--text-muted); font-variant-numeric: tabular-nums; }
  .chart-wrap { position: relative; padding: 0.5rem 0.75rem; isolation: isolate; }
  .chart-wrap.io-panel { border-top: 1px solid var(--border); }
</style>
