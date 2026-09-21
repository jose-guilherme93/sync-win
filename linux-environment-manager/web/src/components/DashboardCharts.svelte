<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { Chart, LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler, Tooltip, Legend, type Chart as ChartType } from 'chart.js'
  import { getDeviceHistory, setHistoryFromAPI, type ChartPoint } from '../lib/telemetry-store'

  Chart.register(LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler, Tooltip, Legend)

  type Device = {
    id: string
    hostname: string
    user_id: string
    status: string
    preference_count: number
    app_count: number
    saves_count: number
    saves_size_bytes: number
    last_sync_at: string
    last_error: string
    last_error_at: string
    hardware?: {
      cpu_usage_percent: number
      memory_used_bytes: number
      memory_total_bytes: number
      memory_percent: number
      disk_read_rate: number
      disk_write_rate: number
      uptime_seconds: number
      power_watts: number
      cpu_temperature: number
      network_ifaces?: { name: string; rx_rate: number; tx_rate: number; rx_bytes: number; tx_bytes: number }[]
    }
  }

  export let device: Device
  export let authHeaders: Record<string, string> = {}

  const serverBase = `${window.location.protocol}//${window.location.hostname}:8080`
  const MAX_POINTS = 60

  let cpuCanvas: HTMLCanvasElement
  let memCanvas: HTMLCanvasElement
  let netCanvas: HTMLCanvasElement

  let cpuChart: ChartType | null = null
  let memChart: ChartType | null = null
  let netChart: ChartType | null = null

  let history: ChartPoint[] = []
  let destroyed = false
  let lastDeviceId = ''
  let building = false

  $: if (device && device.id !== lastDeviceId) {
    lastDeviceId = device.id
    destroyCharts()
    history = []
    void loadInitialHistory()
  }

  $: if (device && !destroyed) {
    const storeHistory = getDeviceHistory(device.id)
    if (storeHistory.length > 0) {
      history = storeHistory.slice(-MAX_POINTS)
      if (cpuChart) updateCharts()
      else if (history.length > 0) buildCharts()
    }
  }

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
        padding: 6,
        titleFont: { size: 10 },
        bodyFont: { size: 9 }
      }
    },
    scales: {
      x: { display: false },
      y: {
        display: true,
        beginAtZero: true,
        grid: { color: 'rgba(148, 163, 184, 0.06)' },
        ticks: { color: '#475569', font: { size: 9 }, maxTicksLimit: 4 }
      }
    }
  }

  function formatRate(bytesPerSec: number) {
    if (bytesPerSec < 1024) return `${bytesPerSec.toFixed(0)} B/s`
    if (bytesPerSec < 1048576) return `${(bytesPerSec / 1024).toFixed(1)} KB/s`
    return `${(bytesPerSec / 1048576).toFixed(1)} MB/s`
  }

  function memoryPercent(h?: Device['hardware']) {
    if (!h || !h.memory_total_bytes) return 0
    return (h.memory_used_bytes / h.memory_total_bytes) * 100
  }

  function totalNetRxRate(h?: Device['hardware']) {
    if (!h?.network_ifaces) return 0
    let total = 0
    for (const iface of h.network_ifaces) {
      if (iface.name === 'lo') continue
      total += iface.rx_rate ?? 0
    }
    return total
  }

  function totalNetTxRate(h?: Device['hardware']) {
    if (!h?.network_ifaces) return 0
    let total = 0
    for (const iface of h.network_ifaces) {
      if (iface.name === 'lo') continue
      total += iface.tx_rate ?? 0
    }
    return total
  }

  async function loadInitialHistory() {
    try {
      const response = await fetch(`${serverBase}/api/devices/${device.id}/telemetry/history?limit=${MAX_POINTS}`, {
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
        history = points.slice(-MAX_POINTS)
      }
    } catch {
      // keep empty history
    }
  }

  function labels(): string[] {
    return history.map((p) => {
      const d = new Date(p.timestamp)
      return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}:${d.getSeconds().toString().padStart(2, '0')}`
    })
  }

  function destroyCharts() {
    for (const c of [cpuChart, memChart, netChart]) {
      c?.destroy()
    }
    cpuChart = null
    memChart = null
    netChart = null
  }

  function buildCharts() {
    if (destroyed || building) return
    building = true
    try {
      destroyCharts()
      if (!cpuCanvas || !memCanvas || !netCanvas || history.length === 0) return
      if (!cpuCanvas.isConnected || !memCanvas.isConnected || !netCanvas.isConnected) return

      const lbls = labels()

      cpuChart = new Chart(cpuCanvas, {
      type: 'line',
      data: {
        labels: lbls,
        datasets: [{
          data: history.map((p) => p.cpu),
          borderColor: '#2dd4bf',
          backgroundColor: 'rgba(45, 212, 191, 0.12)',
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
          backgroundColor: 'rgba(59, 130, 246, 0.12)',
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
            label: 'RX',
            data: history.map((p) => p.netRx),
            borderColor: '#2dd4bf',
            backgroundColor: 'rgba(45, 212, 191, 0.08)',
            fill: true,
            borderWidth: 1.5,
            pointRadius: 0,
            tension: 0.3
          },
          {
            label: 'TX',
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
          legend: { display: true, labels: { color: '#64748b', font: { size: 7 }, boxWidth: 6, padding: 3 } },
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
              callback: (value: number) => formatRate(value)
            }
          }
        }
      }
    })
    } finally {
      building = false
    }
  }

  function updateCharts() {
    if (destroyed) return
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
  }

  onMount(async () => {
    await loadInitialHistory()
    if (device && !destroyed) buildCharts()
  })

  onDestroy(() => {
    destroyed = true
    destroyCharts()
  })
</script>

<div class="dashboard-charts">
  {#if device.hardware}
    <div class="chart-col">
      <span class="chart-label">CPU {device.hardware.cpu_usage_percent?.toFixed(0) || 0}%</span>
      <div class="chart-wrap"><canvas bind:this={cpuCanvas}></canvas></div>
    </div>
    <div class="chart-col">
      <span class="chart-label">RAM {memoryPercent(device.hardware).toFixed(0)}%</span>
      <div class="chart-wrap"><canvas bind:this={memCanvas}></canvas></div>
    </div>
    <div class="chart-col">
      <span class="chart-label">NET &darr;{formatRate(totalNetRxRate(device.hardware))} &uarr;{formatRate(totalNetTxRate(device.hardware))}</span>
      <div class="chart-wrap"><canvas bind:this={netCanvas}></canvas></div>
    </div>
  {:else}
    <p class="no-data">No hardware data</p>
  {/if}
</div>

<style>
  .dashboard-charts {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 0.4rem;
  }

  .chart-col {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
  }

  .chart-label {
    font-size: 0.62rem;
    color: #94a3b8;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .chart-wrap {
    height: 80px;
    position: relative;
  }

  .no-data {
    margin: 0;
    color: #64748b;
    font-size: 0.65rem;
    text-align: center;
    padding: 0.3rem;
  }

  @media (max-width: 600px) {
    .dashboard-charts {
      grid-template-columns: 1fr;
    }
  }
</style>
