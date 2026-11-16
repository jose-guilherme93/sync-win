<script lang="ts">
  import { onDestroy } from 'svelte'
  import {
    addTelemetryPoint,
    deviceHistoryStore,
    setHistoryFromAPI,
    type ChartPoint
  } from '../../lib/telemetry-store'
  import {
    LIVE_MAX_MS,
    LIVE_POLL_MS,
    LIVE_RENEW_EVERY,
    LIVE_SEED_WINDOW,
    LIVE_WINDOW_MS,
    liveTail,
    requestLiveLease
  } from '../../lib/telemetry-live'
  import { swapPercent, memoryPercent, type Device, type HardwareStats } from '../../lib/types'
  import { formatBytes, formatDuration, formatRelative, formatRate, severityFor } from '../../lib/format'
  import { selectedRange, rangeById, DEFAULT_RANGE, type RangeId } from '../../lib/timerange'
  import { pointValues, seriesStats } from '../../lib/telemetry-history'
  import { apiFetch, apiURL } from '../../lib/api'
  import { generateInsights, THRESHOLDS } from '../../lib/insights'
  import {
    fetchTelemetrySeries,
    mergeDelta,
    newestTimestampMs,
    recentWindowCount
  } from '../../lib/telemetry-series'
  import SeriesPlot, { type PlotSeries } from '../ui/SeriesPlot.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import Skeleton from '../ui/Skeleton.svelte'

  export let device: Device
  export let authHeaders: Record<string, string> = {}

  // Live points from the shared store. These are what the Live window uses,
  // because the store updates every poll and is fresher than any fetch.
  let liveHistory: ChartPoint[] = []
  let liveDeviceId = ''
  let unsubHistory: (() => void) | null = null

  function subscribeLiveHistory(id: string) {
    if (!id || id === liveDeviceId) return
    liveDeviceId = id
    unsubHistory?.()
    liveHistory = []
    unsubHistory = deviceHistoryStore(id).subscribe((value) => (liveHistory = value))
  }
  $: subscribeLiveHistory(device.id)
  onDestroy(() => unsubHistory?.())

  // History for the longer ranges. The series endpoint owns the fetch: the
  // first request pulls the whole window, the 30 s refetch asks only for the
  // delta (`since` the newest point already plotted). Until the server ships
  // the endpoint, fetchTelemetrySeries falls back to history-v2 behind that
  // one function, so this screen never breaks in the meantime.
  let serverHistory: ChartPoint[] = []
  let loadingHistory = false
  let historyError = ''
  // The exact window the server answered for, so the x axis can end where the
  // requested range ends instead of where the last bucket happens to fall.
  let historyWindow: { from: number; to: number } | null = null

  let range: RangeId = DEFAULT_RANGE
  const unsubRange = selectedRange.subscribe((value) => (range = value))
  onDestroy(unsubRange)
  // When Live was entered. The chart opens seeded with the last minute that is
  // already in hand (memory first, then the tail of the 30m series) and fills
  // from the right with samples that arrive afterwards.
  // `liveStart` opens the drawable window one minute back so the seed is
  // visible at once; `liveEnteredAt` is when the operator clicked, and only it
  // drives the 15-minute ceiling.
  let liveStart = 0
  let liveEnteredAt = 0
  // The device detail the live poll returns. The fleet poll stops while Live is
  // open, so without this the chart moved and the vitals strip stayed frozen.
  let liveHardware: HardwareStats | null = null
  let historyRequest = 0
  let historyTimer: ReturnType<typeof setInterval> | null = null

  // Live machinery. One interval drives everything: the 1 s single-device
  // telemetry poll, the 45 s lease renewals (every LIVE_RENEW_EVERY ticks) and
  // the 15-minute ceiling, so there is never an orphan timer to clean up.
  let liveTimer: ReturnType<typeof setInterval> | null = null
  let liveTicks = 0
  let unmounting = false

  // Right-to-left reveal, once per load and never on a poll. The window is
  // already in hand; only its presentation animates, driven by the display's
  // refresh rate inside SeriesPlot. `revealFrom` is the fraction that is on
  // screen from the first frame (the newest slice), and `revealId` restarts the
  // animation only when a new range is loaded.
  let revealFrom: number | null = null
  let revealId = 0

  async function loadHistory(delta: boolean) {
    if (unmounting) return
    const option = rangeById(range)
    if (option.id === 'live') {
      // Live is a stream, not a query. The window is seeded on entry by
      // startLive; nothing is fetched into serverHistory here.
      serverHistory = []
      historyWindow = null
      revealFrom = null
      return
    }

    const request = ++historyRequest
    if (!delta) {
      loadingHistory = true
      historyError = ''
    }
    try {
      const since = delta ? newestTimestampMs(serverHistory) ?? undefined : undefined
      const result = await fetchTelemetrySeries(device.id, authHeaders, {
        window: option.id,
        windowMs: option.ms,
        since
      })
      if (request !== historyRequest) return
      historyWindow = result.window
      if (delta && serverHistory.length > 0) {
        // A poll appends to what is drawn. It must never replace the series or
        // replay the reveal: the line only extends at the right edge.
        serverHistory = mergeDelta(serverHistory, result.points)
      } else {
        serverHistory = result.points
        const total = result.points.length
        revealFrom = total > 0 ? recentWindowCount(total) / total : null
        revealId += 1
      }
    } catch (err) {
      // Keep whatever we had; a failed fetch must not blank the chart.
      if (request === historyRequest) {
        if (!delta) {
          serverHistory = []
          revealFrom = null
          historyError = err instanceof Error ? err.message : 'history unavailable'
        }
      }
    } finally {
      if (request === historyRequest && !delta) loadingHistory = false
    }
  }

  function scheduleHistory(id: RangeId) {
    if (unmounting) return
    if (historyTimer) {
      clearInterval(historyTimer)
      historyTimer = null
    }
    const option = rangeById(id)
    if (option.id === 'live') {
      serverHistory = []
      historyWindow = null
      revealFrom = null
      startLive()
      return
    }
    stopLive()
    void loadHistory(false)
    historyTimer = setInterval(() => void loadHistory(true), option.refreshMs)
  }

  // Entering Live. The window is seeded before anything else so the chart
  // never opens empty: the last minute already in the shared store, or — when
  // the store has nothing — the tail of the 30m series endpoint. The lease is
  // best effort; a 404/503 costs the acceleration, never the screen.
  let liveLeaseDevice = ''
  function startLive() {
    if (liveTimer && liveLeaseDevice === device.id) {
      liveStart = Date.now() - LIVE_WINDOW_MS
      liveEnteredAt = Date.now()
      liveTicks = 0
      return
    }
    // A device switch mid-live releases the old lease before opening the new
    // one; the server would otherwise keep the old device accelerated until
    // its own ceiling.
    stopLive()
    liveLeaseDevice = device.id
    liveStart = Date.now() - LIVE_WINDOW_MS
    liveEnteredAt = Date.now()
    liveTicks = 0
    seedLiveWindow()
    void requestLiveLease(device.id, authHeaders, true)
    liveTimer = setInterval(() => void livePollTick(), LIVE_POLL_MS)
  }

  // Nothing in memory yet: seed from the server's recent history. Points the
  // poll collected while the request was in flight are preserved by
  // setHistoryFromAPI, so the seed never overwrites fresher samples.
  function seedLiveWindow() {
    if (liveHistory.length > 0) return
    const id = device.id
    void (async () => {
      try {
        const result = await fetchTelemetrySeries(id, authHeaders, {
          window: LIVE_SEED_WINDOW,
          windowMs: 30 * 60_000
        })
        if (unmounting || rangeById(range).id !== 'live') return
        const tail = liveTail(result.points, LIVE_WINDOW_MS)
        if (tail.length > 0) setHistoryFromAPI(id, tail)
      } catch {
        // Live still draws from its own poll; the seed is a courtesy.
      }
    })()
  }

  async function livePollTick() {
    if (!isLive || document.hidden) return
    liveTicks += 1
    if (liveEnteredAt > 0 && Date.now() - liveEnteredAt >= LIVE_MAX_MS) {
      exitLive()
      return
    }
    if (liveTicks % LIVE_RENEW_EVERY === 0) {
      void requestLiveLease(device.id, authHeaders, true)
    }
    try {
      // One small request per second for one device: the detail carries the
      // current hardware sample, which feeds both the chart (one point per new
      // collected_at) and the vitals strip. /telemetry is the agent's upload
      // endpoint (POST only), so it is never the read path.
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/detail`), { headers: authHeaders })
      if (!response.ok) return
      const payload = await response.json()
      if (unmounting || rangeById(range).id !== 'live') return
      const hardware = payload?.hardware
      if (hardware && typeof hardware === 'object' && !Array.isArray(hardware)) {
        liveHardware = hardware
        addTelemetryPoint(device.id, hardware)
      }
    } catch {
      // A failed tick keeps the window as it is.
    }
  }

  function releaseLiveLease() {
    if (!liveLeaseDevice) return
    const id = liveLeaseDevice
    liveLeaseDevice = ''
    void requestLiveLease(id, authHeaders, false)
  }

  function stopLive() {
    if (liveTimer) {
      clearInterval(liveTimer)
      liveTimer = null
    }
    releaseLiveLease()
  }

  // Live ends itself — ceiling reached, tab hidden, or the screen going away —
  // by closing the lease and falling back to the 1h history view. A range the
  // user picked by hand is never overridden by anything but these exits.
  function exitLive() {
    stopLive()
    selectedRange.set(DEFAULT_RANGE)
  }

  function onVisibilityChange() {
    if (document.hidden && isLive) exitLive()
  }
  document.addEventListener('visibilitychange', onVisibilityChange)

  // Keyed by source, not by object identity: the device object is replaced on
  // every poll, and re-running this restarted the fetch, blanked the series and
  // replayed the reveal every few seconds.
  let historySource = ''
  $: if (device.id && range) {
    const source = `${device.id}|${range}`
    if (source !== historySource) {
      historySource = source
      scheduleHistory(range)
    }
  }

  // Leaving the device screen while Live is open must end the lease, stop the
  // poll and fall back to 1h — no timers may survive the component.
  onDestroy(() => {
    const wasLive = rangeById(range).id === 'live'
    unmounting = true
    document.removeEventListener('visibilitychange', onVisibilityChange)
    // stopLive closes the lease, so an unmount never leaves the agent
    // accelerated past the screen that asked for it.
    stopLive()
    if (wasLive) selectedRange.set(DEFAULT_RANGE)
    if (historyTimer) {
      clearInterval(historyTimer)
      historyTimer = null
    }
  })

  // Only a change of range starts a new live session (see scheduleHistory);
  // startLive stamps `liveEnteredAt`, which drives the 15-minute ceiling.

  $: isLive = rangeById(range).id === 'live'
  // The live chart is capped at the trailing minute. The axis ends at the
  // newest sample, so the line always touches the right edge and the history
  // grows leftward; without the cap the store would keep every sample of the
  // session and the minute-wide axis would squash all of it. The seed from
  // startLive lands here too: samples older than the newest one by more than
  // the window are dropped, the recent ones are drawn at once.
  $: livePoints = (() => {
    if (liveHistory.length === 0) return []
    const newest = Date.parse(liveHistory[liveHistory.length - 1].timestamp)
    if (!Number.isFinite(newest)) return liveHistory
    // The last minute of samples, anchored on the newest one. At the agent's
    // ten-second cadence that is six points across a sixty-second axis.
    return liveHistory.filter((point) => {
      const at = Date.parse(point.timestamp)
      return Number.isFinite(at) && at >= newest - LIVE_WINDOW_MS
    })
  })()
  // The whole window is plotted; the reveal is a clip, not a slice, so the
  // data never changes shape underneath the animation.
  $: points = isLive ? livePoints : serverHistory
  $: chartWindowEnd = isLive
    ? (livePoints.length > 0 ? Date.parse(livePoints[livePoints.length - 1].timestamp) : null)
    : (historyWindow?.to ?? null)
  $: timestamps = points.map((point) => point.timestamp)
  $: rangeLabel = rangeById(range).label
  // A counter while the window is still filling; nothing once it is complete.
  // A number that never changes is noise on a screen that is meant to be read
  // at a glance.
  $: sampleHint = ''

  // Charts use the aligned arrays (gaps stay null); cards and sparklines use
  // the compact arrays.
  $: cpuGap = pointValues(points, 'cpu')
  $: memGap = pointValues(points, 'memory')
  $: tempGap = pointValues(points, 'temp')
  $: rxGap = pointValues(points, 'netRx')
  $: txGap = pointValues(points, 'netTx')
  $: readGap = pointValues(points, 'diskRead')
  $: writeGap = pointValues(points, 'diskWrite')

  let showCpu = true
  let showMem = true
  let showTemp = true
  let showDown = true
  let showUp = true
  let showRead = true
  let showWrite = true

  $: tempAvailable = tempGap.some((value) => value != null)
  $: performanceSeries = [
    ...(showCpu ? [{ label: 'CPU', values: cpuGap, color: 'var(--chart-1)' }] : []),
    ...(showMem ? [{ label: 'Memory', values: memGap, color: 'var(--chart-2)' }] : []),
    ...(showTemp && tempAvailable
      ? [{
          label: 'Temp',
          values: tempGap,
          color: 'var(--chart-3)',
          axis: 'right' as const,
          format: (value: number) => `${value.toFixed(0)}°C`
        }]
      : [])
  ] as PlotSeries[]

  $: networkSeries = [
    ...(showDown ? [{ label: 'Down', values: rxGap, color: 'var(--chart-2)', format: formatRate }] : []),
    ...(showUp ? [{ label: 'Up', values: txGap, color: 'var(--chart-5)', format: formatRate }] : [])
  ] as PlotSeries[]

  $: diskIoSeries = [
    ...(showRead ? [{ label: 'Read', values: readGap, color: 'var(--chart-1)', format: formatRate }] : []),
    ...(showWrite ? [{ label: 'Write', values: writeGap, color: 'var(--chart-3)', format: formatRate }] : [])
  ] as PlotSeries[]

  // Vitals strip: the current reading of each headline metric, with the
  // window's spread as the subline.
  $: hw = isLive && liveHardware ? liveHardware : device.hardware
  $: memPct = memoryPercent(hw)
  $: diskParts = (hw?.disk_partitions ?? []).filter((part) => part.total_bytes > 0)
  // The root partition, or the largest one when a container reports the host
  // disk under a bind mount instead of "/".
  $: rootDisk = diskParts.find((part) => part.mount === '/')
    ?? [...diskParts].sort((a, b) => b.total_bytes - a.total_bytes)[0]
    ?? null
  $: cpuStats = seriesStats(cpuGap.filter((value): value is number => value != null))
  $: netRx = hw?.net_rx_rate ?? 0
  $: netTx = hw?.net_tx_rate ?? 0

  $: cores = hw?.cpu_core_usage ?? []
  $: busiest = cores.length > 0 ? Math.max(...cores) : null
  $: loads = (hw?.load_average ?? '').trim().split(/\s+/).map(Number).filter(Number.isFinite)
  $: load1 = loads[0] ?? null
  $: load5 = loads[1] ?? null
  $: load15 = loads[2] ?? null
  $: loadPerCore = load1 != null && cores.length > 0 ? load1 / cores.length : null

  $: disks = (hw?.disk_partitions ?? [])
    .filter((part) => part.total_bytes > 0)
    .sort((a, b) => b.used_percent - a.used_percent)
  $: ifaces = (hw?.network_ifaces ?? [])
    .filter((iface) => (iface.rx_rate || 0) > 0 || (iface.tx_rate || 0) > 0 || iface.rx_errors > 0 || iface.tx_errors > 0)
    .slice(0, 4)

  $: procsCpu = (hw?.top_cpu_processes ?? []).slice(0, 5)
  $: procsMem = (hw?.top_mem_processes ?? []).slice(0, 5)
  $: insights = generateInsights(hw)
  $: critical = insights.filter((insight) => insight.type === 'crit').length
  $: warnings = insights.filter((insight) => insight.type === 'warn').length
  $: clockMhz = hw?.cpu_clock_mhz ?? 0
  $: clock = clockMhz >= 1000 ? `${(clockMhz / 1000).toFixed(2)} GHz` : clockMhz > 0 ? `${clockMhz.toFixed(0)} MHz` : '—'
  $: swapTotal = hw?.swap_total_bytes ?? 0
  $: temp = hw?.cpu_temperature ?? 0
</script>

<section class="overview">
  {#if !hw}
    <Skeleton variant="cards" rows={3} />
  {:else}
    <div class="status-strip">
      <span>up {formatDuration(hw.uptime_seconds)}</span>
      <span>sampled {formatRelative(hw.collected_at)}</span>
      <span>
        agent {hw.agent_version || '—'}
        {#if hw.agent_cpu_usage != null}· {hw.agent_cpu_usage.toFixed(1)}% CPU{/if}
        {#if hw.agent_memory_bytes}· {formatBytes(hw.agent_memory_bytes)} RAM{/if}
      </span>
      {#if insights.length > 0}
        <span class="strip-alert">
          <SeverityBadge severity={insights[0].type} />
          {critical} critical · {warnings} warning{warnings === 1 ? '' : 's'}
        </span>
      {/if}
    </div>

    <section class="vitals" data-testid="vitals" aria-label="Current values">
      <div class="vital">
        <span class="v-label">CPU</span>
        <span class="v-num">{(hw.cpu_usage_percent || 0).toFixed(0)}<small>%</small></span>
        <span class="v-bar"><i style="width: {Math.min(hw.cpu_usage_percent || 0, 100)}%"></i></span>
        <span class="v-sub">
          {cpuStats ? `${cpuStats.min.toFixed(0)}–${cpuStats.max.toFixed(0)} · ` : ''}{rangeLabel}
        </span>
      </div>
      <div class="vital">
        <span class="v-label">Memory</span>
        <span class="v-num">{memPct.toFixed(0)}<small>%</small></span>
        <span class="v-bar"><i style="width: {Math.min(memPct, 100)}%"></i></span>
        <span class="v-sub">{formatBytes(hw.memory_used_bytes)} / {formatBytes(hw.memory_total_bytes)}</span>
      </div>
      <div class="vital">
        <span class="v-label">Disk /</span>
        <span class="v-num">{rootDisk ? rootDisk.used_percent.toFixed(0) : '—'}<small>%</small></span>
        <span class="v-bar"><i style="width: {rootDisk ? Math.min(rootDisk.used_percent, 100) : 0}%"></i></span>
        <span class="v-sub">
          {rootDisk ? `${formatBytes(rootDisk.used_bytes)} / ${formatBytes(rootDisk.total_bytes)}` : ''}
        </span>
      </div>
      <div class="vital">
        <span class="v-label">Temp</span>
        <span class="v-num">{temp > 0 ? temp.toFixed(0) : '—'}<small>°C</small></span>
        <span class="v-bar"><i style="width: {temp > 0 ? Math.min(temp, 100) : 0}%"></i></span>
        <span class="v-sub">{temp > 0 ? `limit ${THRESHOLDS.tempCrit}` : ''}</span>
      </div>
      <div class="vital">
        <span class="v-label">Load</span>
        <span class="v-num">{load1 != null ? load1.toFixed(2) : '—'}</span>
        <span class="v-bar"><i style="width: {loadPerCore != null ? Math.min(loadPerCore * 100, 100) : 0}%"></i></span>
        <span class="v-sub">
          {loadPerCore != null ? `${loadPerCore.toFixed(2)} / core · ` : ''}{cores.length} cores
        </span>
      </div>
      <div class="vital">
        <span class="v-label">Network</span>
        <span class="v-num">{formatBytes(netRx)}<small>/s</small></span>
        <span class="v-bar"><i style="width: {Math.min(((netRx + netTx) / 125_000_000) * 100, 100)}%"></i></span>
        <span class="v-sub">tx {formatRate(netTx)}</span>
      </div>
    </section>

    <article class="card chart-card">
      <header class="card-head">
        <div class="head-title">
          <h2>Performance</h2>
          {#if sampleHint}<span class="hint">{rangeLabel} · {sampleHint}</span>{/if}
        </div>
        <div class="toggles">
          <button class:on={showCpu} on:click={() => (showCpu = !showCpu)}><i style="background: var(--chart-1)"></i>CPU</button>
          <button class:on={showMem} on:click={() => (showMem = !showMem)}><i style="background: var(--chart-2)"></i>Memory</button>
          <button class:on={showTemp} disabled={!tempAvailable} on:click={() => (showTemp = !showTemp)}><i style="background: var(--chart-3)"></i>Temp</button>
        </div>
      </header>
      {#if historyError}<p class="chart-error" role="alert">History unavailable: {historyError}</p>{/if}
      <SeriesPlot
        labels={timestamps}
        series={performanceSeries}
        leftUnit="%"
        rightUnit="°C"
        leftMax={100}
        rightMax={100}
        height={300}
        windowMs={rangeById(range).ms}
        windowEnd={chartWindowEnd}
        loading={loadingHistory && points.length === 0}
        emptyMessage={isLive ? 'Waiting…' : 'No samples.'}
        revealFrom={revealFrom}
        revealId={revealId}
      />
    </article>

    <div class="duo">
      <article class="card chart-card">
        <header class="card-head">
          <div class="head-title"><h2>Network</h2><span class="hint">received / sent</span></div>
          <div class="toggles">
            <button class:on={showDown} on:click={() => (showDown = !showDown)}><i style="background: var(--chart-2)"></i>Down</button>
            <button class:on={showUp} on:click={() => (showUp = !showUp)}><i style="background: var(--chart-5)"></i>Up</button>
          </div>
        </header>
        <SeriesPlot
          labels={timestamps}
          series={networkSeries}
          leftFormat={formatRate}
          height={180}
          windowMs={rangeById(range).ms}
          windowEnd={chartWindowEnd}
          loading={loadingHistory && points.length === 0}
          emptyMessage="No samples."
        />
      </article>

      <article class="card chart-card">
        <header class="card-head">
          <div class="head-title"><h2>Disk I/O</h2><span class="hint">read / write</span></div>
          <div class="toggles">
            <button class:on={showRead} on:click={() => (showRead = !showRead)}><i style="background: var(--chart-1)"></i>Read</button>
            <button class:on={showWrite} on:click={() => (showWrite = !showWrite)}><i style="background: var(--chart-3)"></i>Write</button>
          </div>
        </header>
        <SeriesPlot
          labels={timestamps}
          series={diskIoSeries}
          leftFormat={formatRate}
          height={180}
          windowMs={rangeById(range).ms}
          windowEnd={chartWindowEnd}
          loading={loadingHistory && points.length === 0}
          emptyMessage="No samples."
        />
      </article>
    </div>

    <div class="duo">
      <article class="card">
        <header class="card-head">
          <h2>Load &amp; cores</h2>
          <span class="hint">{cores.length > 0 ? `${cores.length} cores` : ''}</span>
        </header>
        <div class="load-row">
          <span>1m <b>{load1 != null ? load1.toFixed(2) : '—'}</b></span>
          <span>5m <b>{load5 != null ? load5.toFixed(2) : '—'}</b></span>
          <span>15m <b>{load15 != null ? load15.toFixed(2) : '—'}</b></span>
          <span class="per-core" class:text-warn={loadPerCore != null && loadPerCore >= 0.7} class:text-crit={loadPerCore != null && loadPerCore >= 1}>
            {loadPerCore != null ? loadPerCore.toFixed(2) : '—'} / core
          </span>
        </div>
        {#if cores.length === 0}
          <p class="muted">No per-core data.</p>
        {:else}
          <div class="core-grid">
            {#each cores as usage, index (index)}
              <div class="core-row">
                <span class="core-id">CPU{index}</span>
                <span class="core-bar"><i style="width: {Math.min(usage, 100)}%"></i></span>
                <b class="core-value" class:text-warn={usage >= 70 && usage < 90} class:text-crit={usage >= 90}>{usage.toFixed(0)}%</b>
              </div>
            {/each}
          </div>
          {#if busiest != null}<p class="muted">Busiest core: {busiest.toFixed(0)}%</p>{/if}
        {/if}
      </article>

      <article class="card">
        <header class="card-head">
          <h2>Top processes</h2>
          <span class="hint">cpu / rss</span>
        </header>
        <div class="proc-cols">
          <div>
            <h3 class="proc-title">cpu</h3>
            {#if procsCpu.length === 0}
              <p class="muted">Not reported.</p>
            {:else}
              <ul class="proc-list">
                {#each procsCpu as proc (proc.pid)}
                  <li>
                    <span class="proc-name" title={proc.name}>{proc.name}</span>
                    <span class="proc-value">{proc.cpu_percent.toFixed(1)}%</span>
                    <span class="proc-sub">{formatBytes(proc.mem_rss_bytes)}</span>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
          <div>
            <h3 class="proc-title">rss</h3>
            {#if procsMem.length === 0}
              <p class="muted">Not reported.</p>
            {:else}
              <ul class="proc-list">
                {#each procsMem as proc (proc.pid)}
                  <li>
                    <span class="proc-name" title={proc.name}>{proc.name}</span>
                    <span class="proc-value">{formatBytes(proc.mem_rss_bytes)}</span>
                    <span class="proc-sub">{proc.cpu_percent.toFixed(1)}% cpu</span>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        </div>
      </article>
    </div>

    <div class="duo">
      <article class="card">
        <header class="card-head">
          <h2>Storage</h2>
          <span class="hint">{disks.length} partition{disks.length === 1 ? '' : 's'}</span>
        </header>
        {#if disks.length === 0}
          <p class="muted">No partition data reported.</p>
        {:else}
          {#each disks as disk (disk.mount)}
            <div class="disk-row">
              <div class="disk-head">
                <span class="mono">{disk.mount}</span>
                <b class:text-warn={severityFor(disk.used_percent) === 'warn'} class:text-crit={severityFor(disk.used_percent) === 'crit'}>
                  {disk.used_percent.toFixed(0)}%
                </b>
              </div>
              <span class="disk-bar"><i style="width: {Math.min(disk.used_percent, 100)}%"></i></span>
              <span class="disk-sub">{formatBytes(disk.used_bytes)} of {formatBytes(disk.total_bytes)} · {formatBytes(disk.free_bytes)} free</span>
            </div>
          {/each}
        {/if}
      </article>

      <article class="card">
        <header class="card-head">
          <h2>Interfaces</h2>
          <span class="hint">{ifaces.length} active</span>
        </header>
        {#if ifaces.length === 0}
          <p class="muted">No active interface with traffic or errors.</p>
        {:else}
          {#each ifaces as iface (iface.name)}
            <div class="iface-row">
              <span class="mono iface-name">{iface.name}</span>
              <span class="iface-rates">↓ {formatRate(iface.rx_rate || 0)} · ↑ {formatRate(iface.tx_rate || 0)}</span>
              {#if (iface.rx_errors || 0) + (iface.tx_errors || 0) > 0}
                <span class="iface-err">{iface.rx_errors + iface.tx_errors} err</span>
              {/if}
              {#if iface.addresses?.length}
                <span class="iface-addr">{iface.addresses.slice(0, 2).join(' · ')}</span>
              {/if}
            </div>
          {/each}
        {/if}
      </article>
    </div>

    <article class="card mt">
      <header class="card-head"><h2>System</h2><span class="hint">reported</span></header>
      <dl class="facts">
        <div><dt>OS</dt><dd>{hw.operating_system || '—'}</dd></div>
        <div><dt>Kernel</dt><dd>{hw.kernel_version || '—'}</dd></div>
        <div><dt>Arch</dt><dd>{hw.architecture || '—'}</dd></div>
        <div><dt>Clock</dt><dd>{clock}</dd></div>
        <div><dt>Swap</dt><dd>{swapTotal > 0 ? `${swapPercent(hw).toFixed(0)}% of ${formatBytes(swapTotal)}` : 'not configured'}</dd></div>
        <div><dt>Power</dt><dd>{hw.power_watts != null && hw.power_watts > 0 ? `${hw.power_watts.toFixed(1)} W` : '—'}</dd></div>
        <div><dt>Battery</dt><dd>{hw.battery_percent != null && hw.battery_percent > 0 ? `${hw.battery_percent.toFixed(0)}%${hw.battery_status ? ` (${hw.battery_status})` : ''}` : '—'}</dd></div>
        <div><dt>Last sample</dt><dd>{formatRelative(hw.collected_at)}</dd></div>
      </dl>
    </article>
  {/if}
</section>

<style>
  .overview { display: grid; gap: 0.75rem; }

  .status-strip {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.3rem 1rem;
    color: var(--text-faint);
    font-size: 0.72rem;
  }
  .strip-alert { display: inline-flex; align-items: center; gap: 0.35rem; color: var(--text-muted); }

  /* Vitals strip: one card, hairline gaps, label + number + unit + bar. */
  .vitals {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 1px;
    background: var(--border);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }
  @media (min-width: 620px) { .vitals { grid-template-columns: repeat(3, 1fr); } }
  @media (min-width: 920px) { .vitals { grid-template-columns: repeat(6, 1fr); } }
  .vital { background: var(--card); padding: 0.7rem 0.9rem 0.75rem; display: grid; gap: 0.25rem; align-content: start; min-width: 0; }
  .v-label { color: var(--text-faint); font-size: 0.68rem; }
  .v-num {
    color: var(--text-bright);
    font: 500 1.28rem/1.1 var(--mono);
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .v-num small { color: var(--text-muted); font-size: 0.7rem; font-weight: 400; margin-left: 3px; }
  .v-bar { height: 3px; background: var(--card-inset); border-radius: 2px; overflow: hidden; }
  .v-bar i { display: block; height: 100%; background: var(--text-faint); border-radius: 2px; }
  .v-sub { color: var(--text-faint); font: 0.66rem var(--mono); font-variant-numeric: tabular-nums; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .card {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.85rem 0.9rem;
    min-width: 0;
  }

  .card-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.6rem;
    margin-bottom: 0.55rem;
  }
  .head-title { display: flex; align-items: baseline; gap: 0.5rem; min-width: 0; flex-wrap: wrap; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .hint { color: var(--text-faint); font-size: 0.7rem; font-variant-numeric: tabular-nums; }
  .muted { margin: 0.35rem 0 0; color: var(--text-faint); font-size: 0.74rem; line-height: 1.45; }

  /* Series toggles are neutral; the series dot carries the colour. */
  .toggles { display: flex; gap: 0.25rem; flex: none; }
  .toggles button {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.2rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-faint);
    font-size: 0.68rem;
    cursor: pointer;
  }
  .toggles button:hover { color: var(--text-muted); }
  .toggles button.on { border-color: var(--border-strong); color: var(--text); }
  .toggles button:disabled { opacity: 0.4; cursor: default; }
  .toggles i { width: 7px; height: 7px; border-radius: 2px; flex: none; }

  .chart-error { margin: 0 0 0.4rem; color: var(--crit); font-size: 0.72rem; }

  .duo {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 0.75rem;
    align-items: stretch;
  }
  .mt { margin-top: 0.75rem; }

  .load-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem 1rem;
    color: var(--text-faint);
    font-size: 0.7rem;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid var(--border);
  }
  .load-row b { color: var(--text); font: 500 0.78rem var(--mono); font-variant-numeric: tabular-nums; }
  .load-row .per-core { margin-left: auto; font-variant-numeric: tabular-nums; }

  .core-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
    gap: 0.25rem 1rem;
    margin-top: 0.55rem;
  }
  .core-row {
    display: grid;
    grid-template-columns: 2.3rem minmax(30px, 1fr) 2.2rem;
    align-items: center;
    gap: 0.4rem;
    font: 0.66rem var(--mono);
    font-variant-numeric: tabular-nums;
  }
  .core-id { color: var(--text-faint); }
  .core-bar, .disk-bar {
    display: block;
    height: 4px;
    background: var(--card-inset);
    border-radius: 2px;
    overflow: hidden;
  }
  .core-bar i, .disk-bar i {
    display: block;
    height: 100%;
    background: var(--text-faint);
  }
  .core-value { text-align: right; color: var(--text-muted); font-weight: 400; font-variant-numeric: tabular-nums; }

  .disk-row { display: grid; gap: 0.2rem; padding: 0.4rem 0 0.45rem; border-bottom: 1px solid var(--border); }
  .disk-row:last-of-type { border-bottom: 0; }
  .disk-head { display: flex; justify-content: space-between; gap: 0.5rem; font-size: 0.7rem; }
  .disk-head .mono { color: var(--text); }
  .disk-head b { color: var(--text); font: 500 0.7rem var(--mono); font-variant-numeric: tabular-nums; }
  .disk-sub { color: var(--text-faint); font: 0.64rem var(--mono); font-variant-numeric: tabular-nums; }

  .iface-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0.15rem 0.75rem;
    padding: 0.4rem 0;
    border-bottom: 1px solid var(--border);
    font-size: 0.7rem;
  }
  .iface-row:last-child { border-bottom: 0; }
  .iface-name { color: var(--text); font-weight: 600; }
  .iface-rates { color: var(--text-muted); font-variant-numeric: tabular-nums; }
  .iface-err { color: var(--warn); font-size: 0.68rem; font-variant-numeric: tabular-nums; }
  .iface-addr { grid-column: 1 / -1; color: var(--text-faint); font-size: 0.66rem; }

  .proc-cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 0.75rem; }
  .proc-title { margin: 0 0 0.25rem; color: var(--text-faint); font-size: 0.66rem; font-weight: 500; }
  .proc-list { list-style: none; margin: 0; padding: 0; }
  .proc-list li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0.1rem 0.5rem;
    padding: 0.24rem 0;
    border-bottom: 1px solid var(--border);
    font-size: 0.7rem;
  }
  .proc-list li:last-child { border-bottom: 0; }
  .proc-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-muted); }
  .proc-value { color: var(--text); font: 0.7rem var(--mono); font-variant-numeric: tabular-nums; }
  .proc-sub { grid-column: 1 / -1; color: var(--text-faint); font: 0.62rem var(--mono); font-variant-numeric: tabular-nums; }

  .facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 0.5rem 1.1rem; margin: 0; }
  .facts div { display: grid; gap: 0.05rem; }
  .facts dt { color: var(--text-faint); font-size: 0.64rem; }
  .facts dd { margin: 0; color: var(--text-muted); font: 0.74rem var(--mono); font-variant-numeric: tabular-nums; }

  .mono { font-family: var(--mono); }
  .text-warn { color: var(--warn) !important; }
  .text-crit { color: var(--crit) !important; }
</style>
