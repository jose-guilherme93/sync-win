<script lang="ts">
  import { onDestroy } from 'svelte'
  import { deviceHistoryStore, type ChartPoint } from '../../lib/telemetry-store'
  import { apiFetch, apiURL } from '../../lib/api'
  import { memoryPercent, swapPercent, type Device, type DiskPartition } from '../../lib/types'
  import { formatBytes, formatDuration, formatRate, formatRelative, severityFor } from '../../lib/format'
  import { selectedRange, rangeById, DEFAULT_RANGE, type RangeId } from '../../lib/timerange'
  import { normalizeHistory, pointValues, seriesStats, seriesValues } from '../../lib/telemetry-history'
  import { generateInsights } from '../../lib/insights'
  import StatCard from '../ui/StatCard.svelte'
  import TimeSeriesChart, { type ChartSeries } from '../ui/TimeSeriesChart.svelte'
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

  // History for the longer ranges. The store only holds a short live window,
  // so the selected range owns its own fetch.
  let serverHistory: ChartPoint[] = []
  let loadingHistory = false
  let historyError = ''

  let range: RangeId = DEFAULT_RANGE
  const unsubRange = selectedRange.subscribe((value) => (range = value))
  onDestroy(unsubRange)
  let historyRequest = 0
  let historyTimer: ReturnType<typeof setInterval> | null = null

  async function loadHistory() {
    const option = rangeById(range)
    if (option.id === 'live') {
      serverHistory = []
      return
    }

    loadingHistory = true
    historyError = ''
    const request = ++historyRequest
    try {
      const to = new Date()
      const from = new Date(to.getTime() - option.ms)
      const query = new URLSearchParams({
        from: from.toISOString(),
        to: to.toISOString(),
        resolution: 'auto'
      })
      const response = await apiFetch(
        apiURL(`/api/devices/${device.id}/telemetry/history-v2?${query.toString()}`),
        { headers: authHeaders }
      )
      if (!response.ok) throw new Error(`request failed (${response.status})`)
      const points = normalizeHistory(await response.json())
      if (request === historyRequest) serverHistory = points
    } catch (err) {
      // Keep whatever we had; a failed fetch must not blank the chart.
      if (request === historyRequest) {
        serverHistory = []
        historyError = err instanceof Error ? err.message : 'history unavailable'
      }
    } finally {
      if (request === historyRequest) loadingHistory = false
    }
  }

  function scheduleHistory(id: RangeId) {
    if (historyTimer) {
      clearInterval(historyTimer)
      historyTimer = null
    }
    void loadHistory()
    const option = rangeById(id)
    if (option.id !== 'live') {
      historyTimer = setInterval(() => void loadHistory(), option.refreshMs)
    }
  }

  $: if (device.id && range) scheduleHistory(range)

  onDestroy(() => {
    if (historyTimer) clearInterval(historyTimer)
  })

  $: isLive = rangeById(range).id === 'live'
  $: points = isLive ? liveHistory : serverHistory
  $: timestamps = points.map((point) => point.timestamp)

  // Charts use the aligned arrays (gaps stay null); cards and sparklines use
  // the compact arrays.
  $: cpuGap = pointValues(points, 'cpu')
  $: memGap = pointValues(points, 'memory')
  $: tempGap = pointValues(points, 'temp')
  $: rxGap = pointValues(points, 'netRx')
  $: txGap = pointValues(points, 'netTx')
  $: readGap = pointValues(points, 'diskRead')
  $: writeGap = pointValues(points, 'diskWrite')

  $: cpuSeries = seriesValues(points, 'cpu')
  $: memSeries = seriesValues(points, 'memory')
  $: tempSeries = seriesValues(points, 'temp')
  $: diskSeries = seriesValues(points, 'disk')

  $: cpuStats = seriesStats(cpuSeries)
  $: memStats = seriesStats(memSeries)
  $: tempStats = seriesStats(tempSeries)
  $: diskStats = seriesStats(diskSeries)

  let showCpu = true
  let showMem = true
  let showTemp = true

  $: tempAvailable = tempGap.some((value) => value != null)
  $: performanceSeries = [
    ...(showCpu ? [{ label: 'CPU', values: cpuGap, color: 'var(--accent)' }] : []),
    ...(showMem ? [{ label: 'Memory', values: memGap, color: 'var(--series-1)' }] : []),
    ...(showTemp && tempAvailable
      ? [{
          label: 'Temp',
          values: tempGap,
          color: 'var(--series-3)',
          axis: 'right' as const,
          format: (value: number) => `${value.toFixed(0)}°C`
        }]
      : [])
  ] as ChartSeries[]

  $: networkSeries = [
    { label: 'Down', values: rxGap, color: 'var(--accent)', format: formatRate },
    { label: 'Up', values: txGap, color: 'var(--series-5)', format: formatRate }
  ] as ChartSeries[]

  $: diskIoSeries = [
    { label: 'Read', values: readGap, color: 'var(--series-2)', format: formatRate },
    { label: 'Write', values: writeGap, color: 'var(--series-4)', format: formatRate }
  ] as ChartSeries[]

  // Hardware facts.
  $: hw = device.hardware
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
  $: primary = disks.find((part) => part.mount === '/')
    ?? disks.reduce<DiskPartition | null>((largest, part) =>
      !largest || part.total_bytes > largest.total_bytes ? part : largest, null)

  $: ifaces = (hw?.network_ifaces ?? [])
    .filter((iface) => (iface.rx_rate || 0) > 0 || (iface.tx_rate || 0) > 0 || iface.rx_errors > 0 || iface.tx_errors > 0)
    .slice(0, 4)

  $: procsCpu = (hw?.top_cpu_processes ?? []).slice(0, 5)
  $: procsMem = (hw?.top_mem_processes ?? []).slice(0, 5)
  $: insights = generateInsights(hw)
  $: critical = insights.filter((insight) => insight.type === 'crit').length
  $: warnings = insights.filter((insight) => insight.type === 'warn').length
  $: temp = hw?.cpu_temperature != null && hw.cpu_temperature > 0 ? hw.cpu_temperature : null
  $: clockMhz = hw?.cpu_clock_mhz ?? 0
  $: clock = clockMhz >= 1000 ? `${(clockMhz / 1000).toFixed(2)} GHz` : clockMhz > 0 ? `${clockMhz.toFixed(0)} MHz` : '—'
  $: swapTotal = hw?.swap_total_bytes ?? 0

  function sevClass(percent: number): string {
    const sev = severityFor(percent)
    return sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : ''
  }
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

    <div class="vitals">
      <div data-vital="cpu">
        <StatCard
          label="CPU"
          value={hw.cpu_usage_percent.toFixed(0)}
          unit="%"
          sub={load1 != null ? `load ${load1.toFixed(2)} · ${cores.length} cores` : `${cores.length} cores`}
          avg={cpuStats?.avg ?? null}
          min={cpuStats?.min ?? null}
          max={cpuStats?.max ?? null}
          series={cpuSeries}
          seriesColor="var(--accent)"
        />
      </div>
      <div data-vital="memory">
        <StatCard
          label="Memory"
          value={memoryPercent(hw).toFixed(0)}
          unit="%"
          sub={`${formatBytes(hw.memory_used_bytes)} / ${formatBytes(hw.memory_total_bytes)}`}
          avg={memStats?.avg ?? null}
          min={memStats?.min ?? null}
          max={memStats?.max ?? null}
          series={memSeries}
          seriesColor="var(--series-1)"
        />
      </div>
      <div data-vital="disk">
        <StatCard
          label="Disk"
          value={primary ? primary.used_percent.toFixed(0) : null}
          unit="%"
          present={primary != null}
          sub={primary ? `${formatBytes(primary.used_bytes)} / ${formatBytes(primary.total_bytes)}` : 'no partition reported'}
          avg={diskStats?.avg ?? null}
          min={diskStats?.min ?? null}
          max={diskStats?.max ?? null}
          series={diskSeries}
          seriesColor="var(--series-2)"
        />
      </div>
      <div data-vital="temperature">
        <StatCard
          label="Temperature"
          value={temp != null ? temp.toFixed(0) : null}
          unit="°C"
          present={temp != null}
          sub={hw.gpu_temperature_celsius != null && hw.gpu_temperature_celsius > 0 ? `GPU ${hw.gpu_temperature_celsius.toFixed(0)}°C` : hw.cpu_clock_mhz ? clock : 'no sensor'}
          avg={tempStats?.avg ?? null}
          min={tempStats?.min ?? null}
          max={tempStats?.max ?? null}
          series={tempSeries}
          seriesColor="var(--series-3)"
        />
      </div>
    </div>

    <article class="card chart-card">
      <header class="card-head">
        <div class="head-title">
          <h2>Performance</h2>
          <span class="hint">{rangeById(range).label} · {points.length} samples{loadingHistory && points.length === 0 ? ' · loading' : ''}</span>
        </div>
        <div class="toggles">
          <button class:on={showCpu} on:click={() => (showCpu = !showCpu)}>CPU</button>
          <button class:on={showMem} on:click={() => (showMem = !showMem)}>Memory</button>
          <button class:on={showTemp} disabled={!tempAvailable} on:click={() => (showTemp = !showTemp)}>Temp</button>
        </div>
      </header>
      {#if historyError}<p class="chart-error" role="alert">History unavailable: {historyError}</p>{/if}
      <TimeSeriesChart
        labels={timestamps}
        series={performanceSeries}
        leftUnit="%"
        rightUnit="°C"
        leftMax={100}
        rightMax={100}
        height={360}
        emptyMessage={isLive
          ? 'The chart appears once at least two live samples have been collected.'
          : 'No server-side samples exist for this window yet.'}
      />
    </article>

    <div class="two-col">
      <article class="card chart-card">
        <header class="card-head">
          <div class="head-title"><h2>Network</h2><span class="hint">received / sent</span></div>
        </header>
        <TimeSeriesChart
          labels={timestamps}
          series={networkSeries}
          leftUnit=""
          leftFormat={formatRate}
          height={240}
          emptyMessage="No network samples in this window."
        />
      </article>

      <article class="card chart-card">
        <header class="card-head">
          <div class="head-title"><h2>Disk I/O</h2><span class="hint">read / write</span></div>
        </header>
        <TimeSeriesChart
          labels={timestamps}
          series={diskIoSeries}
          leftUnit=""
          leftFormat={formatRate}
          height={240}
          emptyMessage={isLive
            ? 'Disk I/O appears once two live samples have been collected.'
            : 'Disk I/O history is only kept in the live window; longer ranges aggregate CPU, memory, network and temperature.'}
        />
      </article>
    </div>

    <div class="system-grid">
      <article class="card">
        <header class="card-head">
          <h2>Load &amp; cores</h2>
          <span class="hint">{cores.length > 0 ? `${cores.length} cores` : 'not reported'}</span>
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
          <p class="muted">Per-core usage was not included in the last telemetry payload.</p>
        {:else}
          <div class="core-grid">
            {#each cores as usage, index (index)}
              <div class="core-row">
                <span class="core-id">CPU{index}</span>
                <span class="core-bar"><i class={sevClass(usage)} style="width: {Math.min(usage, 100)}%"></i></span>
                <b class="core-value" class:text-warn={usage >= 70 && usage < 90} class:text-crit={usage >= 90}>{usage.toFixed(0)}%</b>
              </div>
            {/each}
          </div>
          {#if busiest != null}<p class="muted">Busiest core: {busiest.toFixed(0)}%</p>{/if}
        {/if}
      </article>

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
              <span class="disk-bar"><i class={sevClass(disk.used_percent)} style="width: {Math.min(disk.used_percent, 100)}%"></i></span>
              <span class="disk-sub">{formatBytes(disk.used_bytes)} of {formatBytes(disk.total_bytes)} · {formatBytes(disk.free_bytes)} free</span>
            </div>
          {/each}
        {/if}
      </article>

      <article class="card">
        <header class="card-head">
          <h2>Network interfaces</h2>
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

      <article class="card">
        <header class="card-head">
          <h2>Top processes</h2>
          <span class="hint">CPU / memory</span>
        </header>
        <div class="proc-cols">
          <div>
            <h3 class="proc-title">By CPU</h3>
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
            <h3 class="proc-title">By memory</h3>
            {#if procsMem.length === 0}
              <p class="muted">Not reported.</p>
            {:else}
              <ul class="proc-list">
                {#each procsMem as proc (proc.pid)}
                  <li>
                    <span class="proc-name" title={proc.name}>{proc.name}</span>
                    <span class="proc-value">{formatBytes(proc.mem_rss_bytes)}</span>
                    <span class="proc-sub">{proc.cpu_percent.toFixed(1)}% CPU</span>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        </div>
      </article>

      <article class="card">
        <header class="card-head"><h2>System</h2><span class="hint">reported values</span></header>
        <dl class="facts">
          <div><dt>OS</dt><dd>{hw.operating_system || '—'}</dd></div>
          <div><dt>Kernel</dt><dd>{hw.kernel_version || '—'}</dd></div>
          <div><dt>Architecture</dt><dd>{hw.architecture || '—'}</dd></div>
          <div><dt>Clock</dt><dd>{clock}</dd></div>
          <div><dt>Core voltage</dt><dd>{hw.core_voltage_v ? `${hw.core_voltage_v.toFixed(3)} V` : '—'}</dd></div>
          <div><dt>Swap</dt><dd>{swapTotal > 0 ? `${swapPercent(hw).toFixed(0)}% of ${formatBytes(swapTotal)}` : 'not configured'}</dd></div>
          <div><dt>Power</dt><dd>{hw.power_watts != null && hw.power_watts > 0 ? `${hw.power_watts.toFixed(1)} W` : '—'}</dd></div>
          <div><dt>Battery</dt><dd>{hw.battery_percent != null && hw.battery_percent > 0 ? `${hw.battery_percent.toFixed(0)}%${hw.battery_status ? ` (${hw.battery_status})` : ''}` : '—'}</dd></div>
          <div><dt>GPU temp</dt><dd>{hw.gpu_temperature_celsius != null && hw.gpu_temperature_celsius > 0 ? `${hw.gpu_temperature_celsius.toFixed(0)}°C` : '—'}</dd></div>
          <div><dt>Last sample</dt><dd>{formatRelative(hw.collected_at)}</dd></div>
        </dl>
      </article>
    </div>
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

  .vitals {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 0.75rem;
  }

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
  .hint { color: var(--text-faint); font-size: 0.7rem; }
  .muted { margin: 0.35rem 0 0; color: var(--text-faint); font-size: 0.74rem; line-height: 1.45; }

  .toggles { display: flex; gap: 0.25rem; flex: none; }
  .toggles button {
    padding: 0.2rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-faint);
    font-size: 0.68rem;
    cursor: pointer;
  }
  .toggles button.on {
    border-color: var(--accent-border);
    background: var(--accent-dim);
    color: var(--accent);
  }
  .toggles button:disabled { opacity: 0.4; cursor: default; }

  .chart-error { margin: 0 0 0.4rem; color: var(--crit); font-size: 0.72rem; }

  .two-col {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
    gap: 0.75rem;
  }

  .system-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
    gap: 0.75rem;
    align-items: start;
  }
  /* The facts card closes the grid instead of leaving an empty half-row. */
  .system-grid > .card:last-child { grid-column: 1 / -1; }

  .load-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem 1rem;
    color: var(--text-faint);
    font-size: 0.72rem;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid var(--border);
  }
  .load-row b { color: var(--text); font-variant-numeric: tabular-nums; }
  .load-row .per-core { margin-left: auto; }

  .core-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 0.3rem 0.9rem;
    margin-top: 0.55rem;
  }
  .core-row {
    display: grid;
    grid-template-columns: 2.6rem minmax(30px, 1fr) 2.2rem;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.68rem;
  }
  .core-id { color: var(--text-faint); }
  .core-bar, .disk-bar {
    display: block;
    height: 0.45rem;
    background: var(--card-inset);
    overflow: hidden;
  }
  .core-bar i, .disk-bar i {
    display: block;
    height: 100%;
    background: var(--accent);
  }
  .core-bar i.warn, .disk-bar i.warn { background: var(--warn); }
  .core-bar i.crit, .disk-bar i.crit { background: var(--crit); }
  .core-value { text-align: right; color: var(--text-muted); font-variant-numeric: tabular-nums; }

  .disk-row { display: grid; gap: 0.2rem; padding: 0.4rem 0 0.45rem; border-bottom: 1px solid var(--border); }
  .disk-row:last-of-type { border-bottom: 0; }
  .disk-head { display: flex; justify-content: space-between; gap: 0.5rem; font-size: 0.72rem; }
  .disk-head b { color: var(--text); font-variant-numeric: tabular-nums; }
  .disk-sub { color: var(--text-faint); font-size: 0.68rem; }

  .iface-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0.15rem 0.75rem;
    padding: 0.4rem 0;
    border-bottom: 1px solid var(--border);
    font-size: 0.72rem;
  }
  .iface-row:last-child { border-bottom: 0; }
  .iface-name { color: var(--text); font-weight: 600; }
  .iface-rates { color: var(--text-muted); font-variant-numeric: tabular-nums; }
  .iface-err { color: var(--warn); font-size: 0.68rem; }
  .iface-addr { grid-column: 1 / -1; color: var(--text-faint); font-size: 0.66rem; }

  .proc-cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 0.75rem; }
  .proc-title {
    margin: 0 0 0.25rem;
    color: var(--text-faint);
    font-size: 0.64rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
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
  .proc-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); }
  .proc-value { color: var(--text-bright); font-variant-numeric: tabular-nums; }
  .proc-sub { grid-column: 1 / -1; color: var(--text-faint); font-size: 0.64rem; }

  .facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 0.4rem 0.9rem; margin: 0; }
  .facts div { display: grid; gap: 0.05rem; }
  .facts dt { color: var(--text-faint); font-size: 0.64rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .facts dd { margin: 0; color: var(--text-muted); font-size: 0.72rem; }

  .mono { font-family: var(--mono); }
  .text-warn { color: var(--warn) !important; }
  .text-crit { color: var(--crit) !important; }

  @media (max-width: 1100px) {
    .vitals { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }

  @media (max-width: 560px) {
    .vitals { grid-template-columns: 1fr; }
  }
</style>
