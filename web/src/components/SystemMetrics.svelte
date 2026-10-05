<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import {
    Chart,
    LineController,
    LineElement,
    PointElement,
    LinearScale,
    CategoryScale,
    Filler,
    Tooltip,
    Legend
  } from 'chart.js'
  import { setHistoryFromAPI, addTelemetryPoint, deviceHistoryStore, type ChartPoint } from '../lib/telemetry-store'
  import { apiFetch, apiURL, serverBase } from '../lib/api'
  import type { Device as SharedDevice, HardwareStats as SharedHardwareStats } from '../lib/types'

  Chart.register(LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler, Tooltip, Legend)

  type DeviceLog = { timestamp: string; level: string; source: string; message: string }

  // Shapes come from lib/types so a Device passed from the shell type-checks
  // here. This view additionally reads the raw log array the detail payload
  // carries, which the shared Device type does not model.
  type HardwareStats = SharedHardwareStats & { logs?: DeviceLog[] }
  type Device = SharedDevice & { logs?: DeviceLog[] }

  export let device: Device
  export let authHeaders: Record<string, string> = {}

  const REFRESH_OPTIONS = [1000, 5000, 10000, 30000, 60000] as const

  let refreshInterval = 5000
  let history: ChartPoint[] = []
  let loadingHistory = false
  let chartError = ''
  let logFilter = 'all'
  let loadingLogs = false

  let cpuCanvas: HTMLCanvasElement
  let memCanvas: HTMLCanvasElement
  let netCanvas: HTMLCanvasElement
  let tempCanvas: HTMLCanvasElement

  let cpuChart: Chart | null = null
  let memChart: Chart | null = null
  let netChart: Chart | null = null
  let tempChart: Chart | null = null

  const MAX_POINTS = 120

  const chartDefaults = {
    responsive: true,
    maintainAspectRatio: false,
    animation: false as const,
    interaction: { intersect: false, mode: 'index' as const },
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: 'rgba(15, 23, 42, 0.92)',
        titleColor: '#e2e8f0',
        bodyColor: '#cbd5e1',
        borderColor: 'rgba(148, 163, 184, 0.2)',
        borderWidth: 1,
        padding: 8,
        titleFont: { size: 11 },
        bodyFont: { size: 10 }
      }
    },
    scales: {
      x: {
        display: true,
        grid: { color: 'rgba(148, 163, 184, 0.08)' },
        ticks: { color: '#64748b', font: { size: 9 }, maxTicksLimit: 8, maxRotation: 0 }
      },
      y: {
        display: true,
        beginAtZero: true,
        grid: { color: 'rgba(148, 163, 184, 0.08)' },
        ticks: { color: '#64748b', font: { size: 9 }, maxTicksLimit: 5 }
      }
    }
  }

  function labels(): string[] {
    return history.map((p) => {
      const d = new Date(p.timestamp)
      return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}:${d.getSeconds().toString().padStart(2, '0')}`
    })
  }

  function destroyCharts() {
    for (const c of [cpuChart, memChart, netChart, tempChart]) {
      c?.destroy()
    }
    cpuChart = null
    memChart = null
    netChart = null
    tempChart = null
  }

  // A chart needs at least two samples to draw a meaningful line. With fewer,
  // Chart.js renders a flat line pinned to the axis, which reads as "this
  // metric is stuck at zero" rather than "we have no data yet".
  $: hasSeries = history.length >= 2

  function buildCharts() {
    destroyCharts()
    if (!hasSeries) {
      chartError = ''
      return
    }
    if (!cpuCanvas || !memCanvas || !netCanvas || !tempCanvas) {
      chartError = 'Canvas elements not ready'
      return
    }
    const lbls = labels()

    cpuChart = new Chart(cpuCanvas, {
      type: 'line',
      data: {
        labels: lbls,
        datasets: [{
          data: history.map((p) => p.cpu),
          borderColor: '#2dd4bf',
          backgroundColor: 'rgba(45, 212, 191, 0.1)',
          fill: true,
          borderWidth: 1.5,
          pointRadius: 0,
          tension: 0.3
        }]
      },
      options: { ...chartDefaults, scales: { ...chartDefaults.scales, y: { ...chartDefaults.scales.y, max: 100 } } }
    })

    memChart = new Chart(memCanvas, {
      type: 'line',
      data: {
        labels: lbls,
        datasets: [{
          data: history.map((p) => p.memory),
          borderColor: '#3b82f6',
          backgroundColor: 'rgba(59, 130, 246, 0.1)',
          fill: true,
          borderWidth: 1.5,
          pointRadius: 0,
          tension: 0.3
        }]
      },
      options: { ...chartDefaults, scales: { ...chartDefaults.scales, y: { ...chartDefaults.scales.y, max: 100 } } }
    })

    netChart = new Chart(netCanvas, {
      type: 'line',
      data: {
        labels: lbls,
        datasets: [
          {
            label: 'Download (RX)',
            data: history.map((p) => p.netRx),
            borderColor: '#2dd4bf',
            backgroundColor: 'rgba(45, 212, 191, 0.08)',
            fill: true,
            borderWidth: 1.5,
            pointRadius: 0,
            tension: 0.3
          },
          {
            label: 'Upload (TX)',
            data: history.map((p) => p.netTx),
            borderColor: '#fbbf24',
            backgroundColor: 'rgba(251, 191, 36, 0.08)',
            fill: true,
            borderWidth: 1.5,
            pointRadius: 0,
            tension: 0.3
          }
        ]
      },
      options: {
        ...chartDefaults,
        plugins: {
          ...chartDefaults.plugins,
          legend: { display: true, labels: { color: '#94a3b8', font: { size: 9 }, boxWidth: 10, padding: 6 } },
          tooltip: {
            ...chartDefaults.plugins.tooltip,
            callbacks: {
              label: (context) => `${context.dataset.label}: ${formatRate(Number(context.raw) || 0)}`
            }
          }
        },
        scales: {
          ...chartDefaults.scales,
          y: {
            ...chartDefaults.scales.y,
            ticks: {
              ...chartDefaults.scales.y.ticks,
              callback: (value) => formatRate(Number(value) || 0)
            }
          }
        }
      }
    })

    tempChart = new Chart(tempCanvas, {
      type: 'line',
      data: {
        labels: lbls,
        datasets: [{
          data: history.map((p) => p.temp),
          borderColor: '#f87171',
          backgroundColor: 'rgba(248, 113, 113, 0.1)',
          fill: true,
          borderWidth: 1.5,
          pointRadius: 0,
          tension: 0.3
        }]
      },
      options: chartDefaults
    })
    chartError = ''
  }

  function updateCharts() {
    if (!hasSeries) {
      destroyCharts()
      return
    }
    const lbls = labels()
    if (cpuChart) {
      cpuChart.data.labels = lbls
      cpuChart.data.datasets[0].data = history.map((p) => p.cpu)
      cpuChart.update('none')
    }
    if (memChart) {
      memChart.data.labels = lbls
      memChart.data.datasets[0].data = history.map((p) => p.memory)
      memChart.update('none')
    }
    if (netChart) {
      netChart.data.labels = lbls
      netChart.data.datasets[0].data = history.map((p) => p.netRx)
      netChart.data.datasets[1].data = history.map((p) => p.netTx)
      netChart.update('none')
    }
    if (tempChart) {
      tempChart.data.labels = lbls
      tempChart.data.datasets[0].data = history.map((p) => p.temp)
      tempChart.update('none')
    }
  }

  function transformPayload(raw: any): ChartPoint {
    const hw = typeof raw === 'string' ? (JSON.parse(raw) || {}) : (raw || {})
    const memTotal = hw.memory_total_bytes || 1
    const memPct = hw.memory_used_bytes ? (hw.memory_used_bytes / memTotal) * 100 : 0

    return {
      timestamp: '',
      cpu: hw.cpu_usage_percent || 0,
      memory: memPct,
      // The agent totals the real interfaces; summing them again here would
      // reintroduce the container double counting it filters out.
      netRx: hw.net_rx_rate || 0,
      netTx: hw.net_tx_rate || 0,
      temp: hw.cpu_temperature || null
    }
  }

  async function loadInitialHistory() {
    loadingHistory = true
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/telemetry/history?limit=${MAX_POINTS}`), {
        headers: authHeaders
      })
      if (!response.ok) throw new Error(`${response.status}`)
      const payload = await response.json()
      if (Array.isArray(payload)) {
        const points = payload.map((point: any) => ({
          timestamp: point.timestamp || point.received_at,
          cpu: point.cpu_usage_percent || point.cpu || 0,
          memory: point.memory_percent || point.memory || 0,
          netRx: point.net_rx || point.netRx || 0,
          netTx: point.net_tx || point.netTx || 0,
          temp: point.cpu_temperature || point.temp || null
        }))
        setHistoryFromAPI(device.id, points)
      }
      chartError = ''
    } catch (e) {
      chartError = e instanceof Error ? e.message : 'fetch failed'
    } finally {
      loadingHistory = false
    }
  }

  // Keep charts in sync with the shared telemetry store through a real
  // subscription instead of length-only polling, and coalesce rebuilds so we
  // never stack setTimeout calls.
  let historyUnsub: (() => void) | null = null
  let subscribedDeviceId = ''
  let buildTimer: ReturnType<typeof setTimeout> | null = null

  function scheduleBuild() {
    if (buildTimer) return
    buildTimer = setTimeout(() => {
      buildTimer = null
      buildCharts()
    }, 50)
  }

  $: if (device.id && device.id !== subscribedDeviceId) {
    historyUnsub?.()
    subscribedDeviceId = device.id
    historyUnsub = deviceHistoryStore(device.id).subscribe((points) => {
      history = points.slice(-MAX_POINTS)
      // Rebuilding on a drop to zero or one point is what keeps the empty state
      // honest: a device that stops reporting loses its charts instead of
      // freezing on a stale line.
      if (cpuChart) updateCharts()
      else if (!loadingHistory) scheduleBuild()
    })
  }

  let localLogs: DeviceLog[] | null = null

  async function refreshLogs() {
    loadingLogs = true
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/detail`), {
        headers: authHeaders
      })
      if (!response.ok) throw new Error(`${response.status}`)
      const data = await response.json()
      if (data?.hardware?.logs) {
        localLogs = data.hardware.logs
      }
    } catch {
      // keep previous logs
    } finally {
      loadingLogs = false
    }
  }

  async function pollTick() {
    if (device.hardware) {
      addTelemetryPoint(device.id, device.hardware)
    }
  }

  function setRefresh(ms: number) {
    refreshInterval = ms
  }

  function memoryPercent(h?: HardwareStats) {
    if (!h || !h.memory_total_bytes) return 0
    return (h.memory_used_bytes / h.memory_total_bytes) * 100
  }

  function formatBytes(value: number) {
    if (!Number.isFinite(value) || value <= 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
    return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
  }

  function formatRate(value: number) {
    return `${formatBytes(value)}/s`
  }

  function formatDuration(value: number) {
    const days = Math.floor(value / 86400)
    const hours = Math.floor((value % 86400) / 3600)
    const minutes = Math.floor((value % 3600) / 60)
    return days > 0 ? `${days}d ${hours}h` : `${hours}h ${minutes}m`
  }

  function meterClass(percent: number) {
    if (percent >= 85) return 'crit'
    if (percent >= 60) return 'warn'
    return ''
  }

  function generateInsights(h?: HardwareStats) {
    if (!h) return []
    const insights: { text: string; type: 'info' | 'warn' | 'crit' }[] = []
    const cpu = h.cpu_usage_percent || 0
    const mem = memoryPercent(h)
    const agentCpu = h.agent_cpu_usage || 0

    if (cpu > 90) insights.push({ text: `CPU critically high (${cpu.toFixed(0)}%) - check top processes`, type: 'crit' })
    else if (cpu > 70) insights.push({ text: `CPU moderately loaded (${cpu.toFixed(0)}%)`, type: 'warn' })
    else if (cpu < 3 && (h.uptime_seconds || 0) > 300) insights.push({ text: 'System appears idle', type: 'info' })

    if (mem > 90) insights.push({ text: `Memory nearly exhausted (${mem.toFixed(0)}%) - consider closing apps`, type: 'crit' })
    else if (mem > 75) insights.push({ text: `Memory usage is elevated (${mem.toFixed(0)}%)`, type: 'warn' })

    if (h.swap_total_bytes && h.swap_total_bytes > 0) {
      const swapPct = ((h.swap_used_bytes || 0) / h.swap_total_bytes) * 100
      if (swapPct > 50) insights.push({ text: `Heavy swap usage (${swapPct.toFixed(0)}%) - performance may be affected`, type: 'warn' })
    }

    if (h.cpu_temperature && h.cpu_temperature > 85) insights.push({ text: `CPU temperature critically high (${h.cpu_temperature.toFixed(0)}C)`, type: 'crit' })
    else if (h.cpu_temperature && h.cpu_temperature > 70) insights.push({ text: `CPU temperature elevated (${h.cpu_temperature.toFixed(0)}C)`, type: 'warn' })

    if (agentCpu > 5) insights.push({ text: `Agent using ${agentCpu.toFixed(1)}% CPU - higher than expected`, type: 'warn' })
    else if (agentCpu > 0) insights.push({ text: `Agent impact: ${agentCpu.toFixed(1)}% CPU - low`, type: 'info' })

    if (h.battery_percent != null && h.battery_percent > 0 && h.battery_percent < 15 && h.battery_status !== 'charging') {
      insights.push({ text: `Battery critically low (${h.battery_percent.toFixed(0)}%)`, type: 'crit' })
    }

    if (h.disk_partitions) {
      for (const p of h.disk_partitions) {
        if (p.used_percent > 90) insights.push({ text: `Partition ${p.mount} is ${p.used_percent.toFixed(0)}% full`, type: 'crit' })
        else if (p.used_percent > 80) insights.push({ text: `Partition ${p.mount} is ${p.used_percent.toFixed(0)}% full`, type: 'warn' })
      }
    }

    if (h.network_ifaces) {
      for (const iface of h.network_ifaces) {
        if (iface.rx_errors > 0 || iface.tx_errors > 0) {
          insights.push({ text: `Network errors on ${iface.name}: RX ${iface.rx_errors}, TX ${iface.tx_errors}`, type: 'warn' })
        }
      }
    }

    if (h.kernel_version) {
      const kernelParts = h.kernel_version.split('.')
      if (kernelParts.length >= 2) {
        const major = parseInt(kernelParts[0])
        const minor = parseInt(kernelParts[1])
        if (major < 5 || (major === 5 && minor < 15)) {
          insights.push({ text: `Kernel ${h.kernel_version} is outdated - consider updating for security patches`, type: 'warn' })
        }
      }
    }

    if (h.agent_version) {
      const verParts = h.agent_version.split('.').map(Number)
      if (verParts.length >= 2 && (verParts[0] < 1 || (verParts[0] === 1 && verParts[1] < 0))) {
        insights.push({ text: `Agent v${h.agent_version} may be outdated - check for updates`, type: 'info' })
      }
    }

    if (h.logs && h.logs.length > 0) {
      let failedLogins = 0
      let segfaults = 0
      let oomKills = 0
      let permissionDenied = 0
      for (const log of h.logs) {
        const msg = log.message.toLowerCase()
        if (msg.includes('failed password') || msg.includes('authentication failure')) failedLogins++
        if (msg.includes('segfault') || msg.includes('segmentation fault')) segfaults++
        if (msg.includes('out of memory') || msg.includes('oom-killer') || msg.includes('oom_reaper')) oomKills++
        if (msg.includes('permission denied')) permissionDenied++
      }
      if (failedLogins > 3) insights.push({ text: `${failedLogins} failed login attempts detected - possible brute force`, type: 'crit' })
      else if (failedLogins > 0) insights.push({ text: `${failedLogins} failed login attempt(s) detected`, type: 'warn' })
      if (segfaults > 0) insights.push({ text: `${segfaults} segmentation fault(s) - unstable software detected`, type: 'warn' })
      if (oomKills > 0) insights.push({ text: `${oomKills} OOM kill(s) - system ran out of memory`, type: 'crit' })
      if (permissionDenied > 5) insights.push({ text: `${permissionDenied} permission denied events - check access controls`, type: 'warn' })
    }

    return insights
  }

  $: hw = device.hardware
  $: insights = generateInsights(hw)

  onMount(() => {
    void loadInitialHistory().then(() => {
      if (history.length > 0 && !cpuChart) {
        scheduleBuild()
      }
    })
  })

  onDestroy(() => {
    historyUnsub?.()
    if (buildTimer) clearTimeout(buildTimer)
    destroyCharts()
  })
</script>

<div class="system-metrics">
  {#if hw}
    <div class="agent-impact-bar">
      <span class="impact-title">Agent Impact</span>
      <div class="impact-bars">
        <div class="impact-item">
          <span>CPU {hw.agent_cpu_usage != null ? `${hw.agent_cpu_usage.toFixed(1)}%` : '—'}</span>
          <div class="impact-track"><i class={meterClass(hw.agent_cpu_usage || 0)} style="width: {Math.min(hw.agent_cpu_usage || 0, 100)}%"></i></div>
        </div>
        <div class="impact-item">
          <span>RAM {hw.agent_memory_bytes ? formatBytes(hw.agent_memory_bytes) : '—'}</span>
          <div class="impact-track"><i class={meterClass(hw.memory_total_bytes ? ((hw.agent_memory_bytes || 0) / hw.memory_total_bytes) * 100 : 0)} style="width: {Math.min(hw.memory_total_bytes ? ((hw.agent_memory_bytes || 0) / hw.memory_total_bytes) * 100 : 0, 100)}%"></i></div>
        </div>
        <div class="impact-item">
          <span>Disk {formatRate((hw.disk_read_rate || 0) + (hw.disk_write_rate || 0))}</span>
          <div class="impact-track"><i style="width: {Math.min(((hw.disk_read_rate || 0) + (hw.disk_write_rate || 0)) / 524288, 100)}%"></i></div>
        </div>
      </div>
    </div>

    <div class="refresh-bar">
      <span class="refresh-label">Refresh</span>
      <div class="refresh-options">
        {#each REFRESH_OPTIONS as ms (ms)}
          <button
            class="refresh-btn"
            class:active={refreshInterval === ms}
            on:click={() => setRefresh(ms)}
          >
            {ms >= 60000 ? `${ms / 60000}m` : `${ms / 1000}s`}
          </button>
        {/each}
      </div>
      {#if loadingHistory}<span class="loading-hint">loading...</span>{/if}
      {#if chartError}<span class="error-hint">{chartError}</span>{/if}
    </div>

    <div class="charts-grid">
      <div class="chart-box">
        <span class="chart-title">CPU %</span>
        <div class="chart-wrap">
          {#if hasSeries}
            <canvas bind:this={cpuCanvas}></canvas>
          {:else}
            <div class="chart-pending" role="status">collecting data...</div>
          {/if}
        </div>
      </div>
      <div class="chart-box">
        <span class="chart-title">Memory %</span>
        <div class="chart-wrap">
          {#if hasSeries}
            <canvas bind:this={memCanvas}></canvas>
          {:else}
            <div class="chart-pending" role="status">collecting data...</div>
          {/if}
        </div>
      </div>
      <div class="chart-box">
        <span class="chart-title">Network (bytes)</span>
        <div class="chart-wrap">
          {#if hasSeries}
            <canvas bind:this={netCanvas}></canvas>
          {:else}
            <div class="chart-pending" role="status">collecting data...</div>
          {/if}
        </div>
      </div>
      <div class="chart-box">
        <span class="chart-title">Temperature (C)</span>
        <div class="chart-wrap">
          {#if hasSeries}
            <canvas bind:this={tempCanvas}></canvas>
          {:else}
            <div class="chart-pending" role="status">collecting data...</div>
          {/if}
        </div>
      </div>
    </div>

    {#if hw.disk_partitions && hw.disk_partitions.length > 0}
      <section class="section-block">
        <h3>Disk Partitions</h3>
        <div class="disk-grid">
          {#each hw.disk_partitions as part (part.mount)}
            <div class="disk-item">
              <div class="disk-header">
                <span class="disk-mount">{part.mount}</span>
                <span class="disk-pct">{part.used_percent.toFixed(1)}%</span>
              </div>
              <div class="disk-bar"><i class={meterClass(part.used_percent)} style="width: {Math.min(part.used_percent, 100)}%"></i></div>
              <div class="disk-info">
                <span>{formatBytes(part.used_bytes)} / {formatBytes(part.total_bytes)}</span>
              </div>
            </div>
          {/each}
        </div>
      </section>
    {/if}

    {#if hw.top_cpu_processes && hw.top_cpu_processes.length > 0}
      <section class="section-block">
        <h3>Top Processes (by CPU)</h3>
        <div class="process-table-wrap">
          <table class="process-table">
            <thead>
              <tr><th>Process</th><th>PID</th><th>CPU %</th><th>RAM</th></tr>
            </thead>
            <tbody>
              {#each hw.top_cpu_processes as proc (proc.pid)}
                <tr>
                  <td class="proc-name">{proc.name}</td>
                  <td>{proc.pid}</td>
                  <td>{proc.cpu_percent.toFixed(1)}%</td>
                  <td>{formatBytes(proc.mem_rss_bytes)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </section>
    {/if}

    {#if hw.network_ifaces && hw.network_ifaces.length > 0}
      <section class="section-block">
        <h3>Network Interfaces</h3>
        <div class="net-grid">
          {#each hw.network_ifaces as iface (iface.name)}
            <div class="net-item">
              <span class="net-name">{iface.name}</span>
              <div class="net-counters">
                <span>&darr; {formatRate(iface.rx_rate || 0)} <small>({formatBytes(iface.rx_bytes)})</small></span>
                <span>&uarr; {formatRate(iface.tx_rate || 0)} <small>({formatBytes(iface.tx_bytes)})</small></span>
                {#if iface.rx_errors > 0 || iface.tx_errors > 0}
                  <span class="net-errors">ERR {iface.rx_errors}/{iface.tx_errors}</span>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      </section>
    {/if}

    <section class="section-block">
      <h3>System Info</h3>
      <dl class="info-grid">
        <div><dt>OS</dt><dd>{hw.operating_system || 'Linux'}</dd></div>
        <div><dt>Kernel</dt><dd>{hw.kernel_version || '—'}</dd></div>
        <div><dt>CPU</dt><dd>{hw.cpu_model || '—'}</dd></div>
        <div><dt>Uptime</dt><dd>{formatDuration(hw.uptime_seconds || 0)}</dd></div>
        <div><dt>Load</dt><dd>{hw.load_average || '—'}</dd></div>
        {#if hw.battery_percent != null && hw.battery_percent > 0}
          <div><dt>Battery</dt><dd>{hw.battery_percent.toFixed(0)}% {hw.battery_status === 'charging' ? '(charging)' : hw.battery_status === 'discharging' ? '' : '(full)'}</dd></div>
        {/if}
        {#if hw.architecture}
          <div><dt>Arch</dt><dd>{hw.architecture}</dd></div>
        {/if}
        {#if hw.desktop_environment}
          <div><dt>DE</dt><dd>{hw.desktop_environment}</dd></div>
        {/if}
        {#if hw.locale}
          <div><dt>Locale</dt><dd>{hw.locale}</dd></div>
        {/if}
        {#if hw.timezone}
          <div><dt>Timezone</dt><dd>{hw.timezone}</dd></div>
        {/if}
        {#if hw.agent_version}
          <div><dt>Agent</dt><dd>v{hw.agent_version}</dd></div>
        {/if}
        <div><dt>Power</dt><dd>{(hw.power_watts || 0).toFixed(1)} W</dd></div>
        {#if hw.swap_total_bytes && hw.swap_total_bytes > 0}
          <div><dt>Swap</dt><dd>{formatBytes(hw.swap_used_bytes || 0)} / {formatBytes(hw.swap_total_bytes)}</dd></div>
        {/if}
        {#if hw.memory_buffers_bytes}
          <div><dt>Buffers</dt><dd>{formatBytes(hw.memory_buffers_bytes)}</dd></div>
        {/if}
        {#if hw.memory_cached_bytes}
          <div><dt>Cached</dt><dd>{formatBytes(hw.memory_cached_bytes)}</dd></div>
        {/if}
      </dl>
    </section>

    {#if insights.length > 0}
      <section class="section-block insights-block">
        <h3>Insights & Security</h3>
        <ul class="insights-list">
          {#each insights as insight (insight.text)}
            <li class="insight-{insight.type}">{insight.text}</li>
          {/each}
        </ul>
      </section>
    {/if}
  {:else}
    <p class="no-hardware">Waiting for agent hardware report...</p>
  {/if}
</div>

<style>
  .system-metrics { display: grid; gap: 0.75rem; }

  .agent-impact-bar {
    padding: 0.6rem 0.7rem;
    border: 1px solid rgba(45, 212, 191, 0.18);
    border-radius: 9px;
    background: rgba(8, 47, 73, 0.22);
  }
  .impact-title { display: block; margin-bottom: 0.4rem; color: #2dd4bf; font-size: 0.68rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; }
  .impact-bars { display: flex; gap: 1rem; }
  .impact-item { flex: 1; display: flex; flex-direction: column; gap: 0.2rem; }
  .impact-item span { font-size: 0.7rem; color: #cbd5e1; }
  .impact-track { height: 4px; border-radius: 2px; background: rgba(148, 163, 184, 0.15); overflow: hidden; }
  .impact-track i { display: block; height: 100%; border-radius: inherit; background: #2dd4bf; transition: width 0.4s ease; }
  .impact-track i.warn { background: #fbbf24; }
  .impact-track i.crit { background: #f87171; }

  .refresh-bar { display: flex; align-items: center; gap: 0.5rem; }
  .refresh-label { font-size: 0.68rem; color: #94a3b8; text-transform: uppercase; letter-spacing: 0.06em; }
  .refresh-options { display: flex; gap: 0.25rem; }
  .refresh-btn {
    padding: 0.2rem 0.5rem;
    font-size: 0.65rem;
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-radius: 5px;
    background: transparent;
    color: #94a3b8;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .refresh-btn:hover { border-color: rgba(45, 212, 191, 0.3); color: #e2e8f0; }
  .refresh-btn.active { border-color: #2dd4bf; color: #2dd4bf; background: rgba(45, 212, 191, 0.08); }
  .loading-hint { font-size: 0.6rem; color: #64748b; margin-left: auto; }
  .error-hint { font-size: 0.6rem; color: #f87171; margin-left: auto; }

  /* auto-fit instead of a fixed 2 columns: a 1fr 1fr grid forces each chart
     into half the width regardless of viewport, which is what clipped the
     network and temperature plots. */
  .charts-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 0.5rem; }
  .chart-box {
    padding: 0.5rem;
    border: 1px solid rgba(148, 163, 184, 0.1);
    border-radius: 8px;
    background: rgba(15, 23, 42, 0.3);
  }
  .chart-title { display: block; font-size: 0.65rem; color: #94a3b8; text-transform: uppercase; letter-spacing: 0.06em; margin-bottom: 0.3rem; }
  /* A fixed height clipped taller canvases. min-height plus an aspect ratio lets
     the plot grow with the container and still reserve room for the axis. */
  .chart-wrap { position: relative; min-height: 170px; height: 170px; }
  @media (max-width: 640px) { .chart-wrap { min-height: 150px; height: 150px; } }
  .chart-pending {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    color: #64748b;
    font-size: 0.72rem;
  }

  .section-block {
    padding: 0.6rem 0.7rem;
    border: 1px solid rgba(148, 163, 184, 0.1);
    border-radius: 8px;
    background: rgba(15, 23, 42, 0.25);
  }
  .section-block h3 { margin: 0 0 0.5rem; font-size: 0.72rem; color: #2dd4bf; text-transform: uppercase; letter-spacing: 0.06em; font-weight: 600; }

  .disk-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 0.4rem; }
  .disk-item { padding: 0.4rem; border: 1px solid rgba(148, 163, 184, 0.08); border-radius: 6px; background: rgba(15, 23, 42, 0.3); }
  .disk-header { display: flex; justify-content: space-between; margin-bottom: 0.2rem; }
  .disk-mount { font-size: 0.7rem; color: #e2e8f0; font-weight: 500; }
  .disk-pct { font-size: 0.7rem; color: #94a3b8; }
  .disk-bar { height: 4px; border-radius: 2px; background: rgba(148, 163, 184, 0.12); overflow: hidden; margin-bottom: 0.2rem; }
  .disk-bar i { display: block; height: 100%; border-radius: inherit; background: #2dd4bf; transition: width 0.4s ease; }
  .disk-bar i.warn { background: #fbbf24; }
  .disk-bar i.crit { background: #f87171; }
  .disk-info span { font-size: 0.6rem; color: #64748b; }

  .process-table-wrap { overflow-x: auto; }
  .process-table { width: 100%; border-collapse: collapse; font-size: 0.7rem; }
  .process-table th { text-align: left; padding: 0.3rem 0.5rem; color: #94a3b8; font-weight: 500; border-bottom: 1px solid rgba(148, 163, 184, 0.1); }
  .process-table td { padding: 0.25rem 0.5rem; color: #cbd5e1; border-bottom: 1px solid rgba(148, 163, 184, 0.05); }
  .proc-name { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .net-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 0.4rem; }
  .net-item { display: flex; justify-content: space-between; align-items: center; padding: 0.4rem 0.5rem; border: 1px solid rgba(148, 163, 184, 0.08); border-radius: 6px; background: rgba(15, 23, 42, 0.3); }
  .net-name { font-size: 0.7rem; color: #e2e8f0; font-weight: 500; }
  .net-counters { display: flex; gap: 0.7rem; font-size: 0.65rem; color: #94a3b8; }
  .net-errors { color: #f87171; font-weight: 600; }

  .info-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 0.3rem; margin: 0; }
  .info-grid > div { display: flex; gap: 0.4rem; padding: 0.2rem 0; }
  .info-grid dt { font-size: 0.65rem; color: #64748b; min-width: 50px; }
  .info-grid dd { font-size: 0.65rem; color: #cbd5e1; margin: 0; }

  .insights-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
  .insights-list li { font-size: 0.7rem; padding: 0.35rem 0.5rem; border-radius: 5px; border-left: 3px solid; }
  .insight-info { color: #94a3b8; border-color: #3b82f6; background: rgba(59, 130, 246, 0.06); }
  .insight-warn { color: #fbbf24; border-color: #fbbf24; background: rgba(251, 191, 36, 0.06); }
  .insight-crit { color: #f87171; border-color: #f87171; background: rgba(248, 113, 113, 0.06); }

  .logs-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem; }
  .logs-header h3 { margin: 0; }
  .logs-controls { display: flex; align-items: center; gap: 0.5rem; }
  .log-filters { display: flex; gap: 0.2rem; }
  .log-filter-btn {
    padding: 0.15rem 0.45rem;
    font-size: 0.6rem;
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-radius: 4px;
    background: transparent;
    color: #94a3b8;
    cursor: pointer;
    transition: all 0.15s ease;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }
  .log-filter-btn:hover { border-color: rgba(45, 212, 191, 0.3); color: #e2e8f0; }
  .log-filter-btn.active { border-color: #2dd4bf; color: #2dd4bf; background: rgba(45, 212, 191, 0.08); }
  .filter-count {
    font-size: 0.5rem;
    padding: 0.05rem 0.2rem;
    border-radius: 3px;
    background: rgba(148, 163, 184, 0.12);
    color: #64748b;
  }
  .log-filter-btn.active .filter-count { background: rgba(45, 212, 191, 0.15); color: #2dd4bf; }
  .log-refresh-btn {
    width: 1.6rem;
    height: 1.6rem;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.8rem;
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-radius: 4px;
    background: transparent;
    color: #94a3b8;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .log-refresh-btn:hover:not(:disabled) { border-color: #2dd4bf; color: #2dd4bf; background: rgba(45, 212, 191, 0.08); }
  .log-refresh-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .logs-footer { margin-top: 0.35rem; font-size: 0.55rem; color: #64748b; text-align: right; }
  .logs-empty { padding: 1rem; text-align: center; color: #64748b; font-size: 0.7rem; }

  .logs-wrap { max-height: 400px; overflow-y: auto; display: grid; gap: 0.15rem; }
  .log-entry { display: flex; gap: 0.5rem; font-size: 0.62rem; padding: 0.2rem 0.35rem; border-radius: 3px; font-family: 'SF Mono', 'Cascadia Code', monospace; }
  .log-entry:hover { background: rgba(148, 163, 184, 0.06); }
  .log-time { color: #64748b; white-space: nowrap; min-width: 60px; }
  .log-src { color: #94a3b8; white-space: nowrap; min-width: 80px; max-width: 120px; overflow: hidden; text-overflow: ellipsis; }
  .log-msg { color: #cbd5e1; word-break: break-word; }
  .log-error .log-msg { color: #fca5a5; }
  .log-warn .log-msg { color: #fcd34d; }

  .no-hardware { margin: 0; color: #64748b; font-size: 0.8rem; text-align: center; padding: 2rem; }

  @media (max-width: 700px) {
    .charts-grid { grid-template-columns: 1fr; }
    .impact-bars { flex-direction: column; }
  }
</style>
