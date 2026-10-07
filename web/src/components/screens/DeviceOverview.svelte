<script lang="ts">
  import { onDestroy } from 'svelte'
  import { deviceHistoryStore, type ChartPoint } from '../../lib/telemetry-store'
  import { apiFetch, apiURL } from '../../lib/api'
  import { memoryPercent, swapPercent, type Device } from '../../lib/types'
  import { formatBytes, formatDuration, formatRate, severityFor } from '../../lib/format'
  import GaugeCard from '../ui/GaugeCard.svelte'
  import StatCard from '../ui/StatCard.svelte'
  import EmptyState from '../ui/EmptyState.svelte'
  import Skeleton from '../ui/Skeleton.svelte'

  export let device: Device
  export let authHeaders: Record<string, string> = {}

  // Live points from the shared store. These are what the 5-minute view uses,
  // because the store updates every poll and is fresher than any fetch.
  let liveHistory: ChartPoint[] = []
  const unsub = deviceHistoryStore(device.id).subscribe((value) => (liveHistory = value))
  onDestroy(unsub)

  // History pulled from the server for the longer ranges. The store only holds
  // a short live window (MAX_POINTS), which is why the period selector used to
  // show the same two minutes whatever you picked.
  let serverHistory: ChartPoint[] = []
  let loadingHistory = false
  let historyError = ''

  // Maps a series to an SVG polyline across a 600x160 box. Both axes are fixed
  // so series with different natural ranges stay comparable; temperature is
  // scaled against 100°C to match the gauge.
  function polyline(values: number[], scaleMax: number): string {
    if (values.length < 2) return ''
    const stepX = 600 / (values.length - 1)
    return values
      .map((v, i) => {
        const clamped = Math.max(0, Math.min(v, scaleMax))
        const y = 160 - (clamped / scaleMax) * 160
        return `${(i * stepX).toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
  }

  const PERIODS = [
    { id: '5m', label: '5 min', ms: 5 * 60_000, live: true },
    { id: '1h', label: '1 hour', ms: 60 * 60_000, live: false },
    { id: '6h', label: '6 hours', ms: 6 * 60 * 60_000, live: false },
    { id: '24h', label: '24 hours', ms: 24 * 60 * 60_000, live: false },
    { id: 'day', label: 'Day', ms: 24 * 60 * 60_000, live: false }
  ]
  let period = '1h'
  let historyRequest = 0

  // history-v2 returns either downsampled aggregates (cpu_avg, mem_avg, …) or
  // raw payloads, depending on the window. Normalising here keeps the chart
  // oblivious to which one the server chose.
  function normalizePoint(raw: any): ChartPoint | null {
    if (!raw) return null
    if (raw.cpu_avg != null || raw.mem_avg != null) {
      return {
        timestamp: raw.timestamp || '',
        cpu: raw.cpu_avg ?? 0,
        memory: raw.mem_avg ?? 0,
        netRx: raw.net_rx_avg ?? 0,
        netTx: raw.net_tx_avg ?? 0,
        temp: raw.temp_avg ?? raw.temp_max ?? null
      }
    }
    let hw = raw.payload ?? raw
    if (typeof hw === 'string') {
      try { hw = JSON.parse(hw) } catch { return null }
    }
    if (!hw || typeof hw !== 'object') return null
    const total = hw.memory_total_bytes || 0
    return {
      timestamp: raw.timestamp || raw.received_at || '',
      cpu: hw.cpu_usage_percent || 0,
      memory: total ? ((hw.memory_used_bytes || 0) / total) * 100 : 0,
      netRx: hw.net_rx_rate || 0,
      netTx: hw.net_tx_rate || 0,
      temp: hw.cpu_temperature ?? null
    }
  }

  async function loadHistory() {
    const option = PERIODS.find((p) => p.id === period)
    if (!option) return

    // The live window needs no request: it is already in memory and updates on
    // every poll.
    if (option.live) {
      serverHistory = []
      return
    }

    loadingHistory = true
    historyError = ''
    const request = ++historyRequest
    try {
      const to = new Date()
      const from = new Date(to.getTime() - option.ms)
      if (period === 'day') from.setHours(0, 0, 0, 0)
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
      const payload = await response.json()
      const points: ChartPoint[] = Array.isArray(payload?.points)
        ? payload.points.map(normalizePoint).filter((p: ChartPoint | null): p is ChartPoint => p != null)
        : []
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

  // Refetch whenever the device or the selected range changes. The 5-minute
  // option leaves this as a no-op.
  $: if (device.id && period) void loadHistory()

  $: windowed = PERIODS.find((p) => p.id === period)?.live ? liveHistory : serverHistory

  // windowed must be passed in explicitly: Svelte's reactive analysis does not
  // see a dependency that is only read inside a called function, so
  // `seriesOf('cpu')` would be evaluated once (while windowed was still empty)
  // and never re-run. That left the History chart and the overview sparklines
  // permanently blank.
  function seriesOf(values: ChartPoint[], key: 'cpu' | 'memory' | 'temp' | 'netRx' | 'netTx'): number[] {
    return values.map((p) => (p[key] as number) ?? 0).filter((v) => Number.isFinite(v))
  }

  function minOf(values: number[]): number | null {
    const real = values.filter((v) => Number.isFinite(v))
    return real.length ? Math.min(...real) : null
  }

  function maxOf(values: number[]): number | null {
    const real = values.filter((v) => Number.isFinite(v))
    return real.length ? Math.max(...real) : null
  }

  $: hw = device.hardware
  $: cores = hw?.cpu_core_usage ?? []
  $: disks = (hw?.disk_partitions ?? []).filter((p) => p.total_bytes > 0).sort((a, b) => b.total_bytes - a.total_bytes)
  $: ifaces = (hw?.network_ifaces ?? [])
    .filter((i) => (i.rx_rate || 0) > 0 || (i.tx_rate || 0) > 0 || i.rx_errors > 0 || i.tx_errors > 0)
    .slice(0, 4)

  $: cpuSeries = seriesOf(windowed, 'cpu')
  $: memSeries = seriesOf(windowed, 'memory')
  $: tempSeries = seriesOf(windowed, 'temp')
  $: rxSeries = seriesOf(windowed, 'netRx')
  $: txSeries = seriesOf(windowed, 'netTx')

  // Agent impact is the footprint of the SyncWin agent itself, which the user
  // cares about because it is the one process this project is responsible for.
  $: agentCpu = hw?.agent_cpu_usage ?? null
  $: agentMemPct = hw?.agent_memory_bytes && hw?.memory_total_bytes
    ? (hw.agent_memory_bytes / hw.memory_total_bytes) * 100
    : null
</script>

<section class="overview">
  {#if !hw}
    <Skeleton variant="cards" rows={3} />
  {:else}
    <!-- Primary gauges: the three numbers a hardware dashboard exists to show. -->
    <div class="row gauges">
      <article class="card gauge-card">
        <GaugeCard
          value={hw.cpu_usage_percent}
          label="CPU"
          unit="%"
          min={minOf(cpuSeries)}
          max={maxOf(cpuSeries)}
        />
        <div class="gauge-meta">
          <span>Load <b>{hw.load_average || '—'}</b></span>
          <span>Cores <b>{cores.length || '—'}</b></span>
        </div>
      </article>

      <article class="card gauge-card">
        <GaugeCard
          value={hw.cpu_temperature ?? null}
          label="Temperature"
          unit="°C"
          mode="temperature"
          scaleMax={100}
          present={hw.cpu_temperature != null}
          min={minOf(tempSeries)}
          max={maxOf(tempSeries)}
        />
        {#if hw.gpu_temperature_celsius != null}
          <div class="gauge-meta"><span>GPU <b>{hw.gpu_temperature_celsius.toFixed(0)}°C</b></span></div>
        {/if}
      </article>

      <article class="card gauge-card">
        <GaugeCard
          value={memoryPercent(hw)}
          label="Memory"
          unit="%"
          min={minOf(memSeries)}
          max={maxOf(memSeries)}
        />
        <div class="gauge-meta">
          <span><b>{formatBytes(hw.memory_used_bytes)}</b> of {formatBytes(hw.memory_total_bytes)}</span>
        </div>
      </article>
    </div>

    <!-- Secondary metrics with inline sparklines. -->
    <div class="row stats">
      <StatCard
        label="CPU usage"
        value={hw.cpu_usage_percent.toFixed(1)}
        unit="%"
        series={cpuSeries}
        min={minOf(cpuSeries)}
        max={maxOf(cpuSeries)}
      />
      <StatCard
        label="Memory"
        value={memoryPercent(hw).toFixed(0)}
        unit="%"
        sub={`${formatBytes(hw.memory_used_bytes)} / ${formatBytes(hw.memory_total_bytes)}`}
        series={memSeries}
        seriesColor="var(--series-1)"
        min={minOf(memSeries)}
        max={maxOf(memSeries)}
      />
      <StatCard
        label="Swap"
        value={hw.swap_total_bytes ? swapPercent(hw).toFixed(0) : null}
        unit="%"
        present={Boolean(hw.swap_total_bytes)}
        sub={hw.swap_total_bytes ? `${formatBytes(hw.swap_used_bytes || 0)} / ${formatBytes(hw.swap_total_bytes)}` : 'not configured'}
      />
      <StatCard
        label="Net down"
        value={formatRate(hw.net_rx_rate || 0)}
        series={rxSeries}
        min={minOf(rxSeries)}
        max={maxOf(rxSeries)}
      />
      <StatCard
        label="Net up"
        value={formatRate(hw.net_tx_rate || 0)}
        series={txSeries}
        seriesColor="var(--series-5)"
        min={minOf(txSeries)}
        max={maxOf(txSeries)}
      />
      <StatCard label="Power" value={(hw.power_watts || 0).toFixed(1)} unit="W" />
      <StatCard label="Uptime" value={formatDuration(hw.uptime_seconds)} />
      <StatCard
        label="Agent impact"
        value={agentCpu != null ? agentCpu.toFixed(1) : null}
        unit="%"
        present={agentCpu != null}
        sub={hw.agent_memory_bytes ? `${formatBytes(hw.agent_memory_bytes)} RAM` : ''}
        tone={agentCpu != null && agentCpu > 5 ? 'warn' : 'default'}
      />
    </div>

    <!-- Load per core/thread, which is how a spike localises to one core. -->
    {#if cores.length > 1}
      <article class="card">
        <header class="card-head">
          <h2>Load per thread</h2>
          <span class="muted">{cores.length} cores</span>
        </header>
        <div class="thread-bars" role="img" aria-label="CPU usage per core">
          {#each cores as value, index (index)}
            <div
              class="thread-bar"
              class:hot={value >= 85}
              class:warm={value >= 60 && value < 85}
              style="height: {Math.max(value, 3)}%"
              title="Core {index}: {value.toFixed(0)}%"
            ></div>
          {/each}
        </div>
      </article>
    {/if}

    <div class="two-col">
      <!-- Storage -->
      <article class="card">
        <header class="card-head">
          <h2>Storage</h2>
        </header>
        {#if disks.length === 0}
          <p class="muted pad">No partition data reported.</p>
        {:else}
          <div class="bars">
            {#each disks as disk (disk.mount)}
              {@const sev = severityFor(disk.used_percent)}
              <div class="bar-row">
                <div class="bar-top">
                  <span class="bar-label mono">{disk.mount}</span>
                  <span class="bar-value" class:text-warn={sev === 'warn'} class:text-crit={sev === 'crit'}>
                    {disk.used_percent.toFixed(0)}%
                  </span>
                </div>
                <div class="bar">
                  <i class={sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : ''} style="width: {Math.min(disk.used_percent, 100)}%"></i>
                </div>
                <span class="bar-sub">{formatBytes(disk.used_bytes)} of {formatBytes(disk.total_bytes)}</span>
              </div>
            {/each}
          </div>
        {/if}
      </article>

      <!-- Network -->
      <article class="card">
        <header class="card-head">
          <h2>Network</h2>
        </header>
        {#if ifaces.length === 0}
          <p class="muted pad">No active interfaces.</p>
        {:else}
          <ul class="ifaces">
            {#each ifaces as iface (iface.name)}
              <li>
                <span class="iface-name mono">{iface.name}</span>
                <span class="iface-rates">
                  <span class="down">↓ {formatRate(iface.rx_rate || 0)}</span>
                  <span class="up">↑ {formatRate(iface.tx_rate || 0)}</span>
                </span>
                {#if iface.rx_errors > 0 || iface.tx_errors > 0}
                  <span class="text-warn">{iface.rx_errors + iface.tx_errors} err</span>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </article>
    </div>

    <!-- History -->
    <article class="card">
      <header class="card-head">
        <h2>History</h2>
        <div class="head-right">
          {#if loadingHistory}<span class="hint" role="status">loading…</span>{/if}
          {#if historyError}<span class="hint error" role="alert">{historyError}</span>{/if}
          <div class="periods" role="group" aria-label="History range">
            {#each PERIODS as option (option.id)}
              <button class:active={period === option.id} on:click={() => (period = option.id)}>{option.label}</button>
            {/each}
          </div>
        </div>
      </header>

      {#if loadingHistory && windowed.length < 2}
        <Skeleton variant="lines" rows={4} />
      {:else if windowed.length < 2}
        <EmptyState
          icon="📈"
          title={historyError ? 'History unavailable' : 'Collecting data…'}
          message={historyError
            ? 'The server did not return history for this range. The live 5-minute view may still have data.'
            : PERIODS.find((p) => p.id === period)?.live
              ? 'The history chart appears once at least two live samples have been collected.'
              : 'No server-side samples exist for this range yet. Telemetry is downsampled in the background, so longer ranges fill in over time.'}
        />
      {:else}
        <div class="chart-wrap">
          <svg class="chart" viewBox="0 0 600 160" preserveAspectRatio="none" role="img" aria-label="CPU, memory and temperature history">
            <!-- Horizontal guides at 25/50/75% -->
            {#each [0.25, 0.5, 0.75] as g (g)}
              <line x1="0" x2="600" y1={160 * g} y2={160 * g} class="grid" />
            {/each}
            <polyline points={polyline(cpuSeries, 100)} class="line cpu" />
            <polyline points={polyline(memSeries, 100)} class="line mem" />
            {#if tempSeries.length >= 2}
              <polyline points={polyline(tempSeries, 100)} class="line temp" />
            {/if}
          </svg>
        </div>
        <div class="legend">
          <span><i class="swatch cpu"></i> CPU {hw.cpu_usage_percent.toFixed(1)}%</span>
          <span><i class="swatch mem"></i> Memory {memoryPercent(hw).toFixed(0)}%</span>
          {#if hw.cpu_temperature != null}
            <span><i class="swatch temp"></i> Temp {hw.cpu_temperature.toFixed(0)}°C</span>
          {/if}
          <span class="legend-count">{windowed.length} samples</span>
        </div>
      {/if}
    </article>
  {/if}
</section>

<style>
  .overview {
    display: grid;
    gap: 0.85rem;
  }

  .row {
    display: grid;
    gap: 0.75rem;
  }

  .gauges {
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  }

  .stats {
    grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  }

  .two-col {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 0.75rem;
  }

  .card {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.9rem;
    min-width: 0;
  }

  .gauge-card {
    display: grid;
    justify-items: center;
    gap: 0.5rem;
  }

  .gauge-meta {
    display: flex;
    gap: 1rem;
    justify-content: center;
    flex-wrap: wrap;
    color: var(--text-muted);
    font-size: 0.75rem;
  }

  .gauge-meta b {
    color: var(--text);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }

  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    margin-bottom: 0.6rem;
  }

  .card-head h2 {
    margin: 0;
    font-size: 0.82rem;
    font-weight: 600;
    color: var(--text);
  }

  .muted { color: var(--text-faint); font-size: 0.75rem; }
  .pad { padding: 1rem 0; text-align: center; }

  .thread-bars {
    display: flex;
    align-items: flex-end;
    gap: 3px;
    height: 56px;
    padding: 4px;
    border-radius: var(--radius-sm);
    background: var(--card-inset);
  }

  .thread-bar {
    flex: 1 1 0;
    min-width: 3px;
    max-width: 16px;
    border-radius: 2px;
    background: var(--accent);
    transition: height var(--t);
  }

  .thread-bar.warm { background: var(--warn); }
  .thread-bar.hot { background: var(--crit); }

  .bars { display: grid; gap: 0.7rem; }

  .bar-row { display: grid; gap: 0.25rem; }

  .bar-top {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 0.5rem;
  }

  .bar-label { color: var(--text); }
  .bar-value { color: var(--text-bright); font-size: 0.85rem; font-variant-numeric: tabular-nums; font-weight: 600; }

  .bar {
    height: 8px;
    border-radius: var(--radius-pill);
    background: rgba(148, 163, 184, 0.15);
    overflow: hidden;
  }

  .bar i {
    display: block;
    height: 100%;
    border-radius: inherit;
    background: var(--accent);
    transition: width var(--t);
  }

  .bar i.warn { background: var(--warn); }
  .bar i.crit { background: var(--crit); }

  .bar-sub { color: var(--text-faint); font-size: 0.7rem; font-variant-numeric: tabular-nums; }

  .ifaces { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.5rem; }

  .ifaces li {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .iface-name { color: var(--text); }
  .iface-rates { display: flex; gap: 0.6rem; font-size: 0.76rem; font-variant-numeric: tabular-nums; }
  .iface-rates .down { color: var(--accent); }
  .iface-rates .up { color: var(--series-5); }

  .periods { display: flex; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }

  .periods button {
    padding: 0.28rem 0.5rem;
    border: 0;
    background: transparent;
    color: var(--text-faint);
    font-size: 0.68rem;
    font-weight: 600;
    cursor: pointer;
    transition: background var(--t-fast), color var(--t-fast);
  }

  .periods button:hover { background: var(--card-hover); color: var(--text); }
  .periods button.active { background: var(--accent-dim); color: var(--accent); }

  .head-right { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; justify-content: flex-end; }
  .hint { color: var(--text-faint); font-size: 0.7rem; }
  .hint.error { color: var(--crit); }
  .legend-count { margin-left: auto; color: var(--text-faint); }

  .chart-wrap { width: 100%; height: 170px; }
  .chart { width: 100%; height: 100%; }

  .grid { stroke: rgba(148, 163, 184, 0.12); stroke-width: 1; stroke-dasharray: 3 4; }

  .line { fill: none; stroke-width: 1.6; }
  .line.cpu { stroke: var(--accent); }
  .line.mem { stroke: var(--series-1); }
  .line.temp { stroke: var(--crit); }

  .legend {
    display: flex;
    gap: 1rem;
    flex-wrap: wrap;
    margin-top: 0.5rem;
    color: var(--text-muted);
    font-size: 0.72rem;
    font-variant-numeric: tabular-nums;
  }

  .legend span { display: inline-flex; align-items: center; gap: 0.35rem; }
  .swatch { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
  .swatch.cpu { background: var(--accent); }
  .swatch.mem { background: var(--series-1); }
  .swatch.temp { background: var(--crit); }
</style>
