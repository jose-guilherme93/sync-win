<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte'
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
  import { getCachedHistory, setCachedHistory } from '../lib/telemetry-cache'

  Chart.register(LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler, Tooltip, Legend)

  export let device: { id: string; hostname: string }
  export let open = false

  const dispatch = createEventDispatcher()
  const serverBase = ''

  type ChartPoint = { time: string; cpu: number; memory: number; netRx: number; netTx: number; temp: number; power: number }

  let history: ChartPoint[] = []
  let loading = true
  let error = ''
  let resolution = 'auto'

  let period = '1d'
  const periods = [
    { label: '1H', value: '1h', hours: 1 },
    { label: '6H', value: '6h', hours: 6 },
    { label: '1D', value: '1d', hours: 24 },
    { label: '1W', value: '1w', hours: 168 },
    { label: '1M', value: '1m', hours: 720 },
  ]

  let cpuCanvas: HTMLCanvasElement
  let memCanvas: HTMLCanvasElement
  let netCanvas: HTMLCanvasElement
  let tempCanvas: HTMLCanvasElement
  let powerCanvas: HTMLCanvasElement
  let cpuChart: Chart
  let memChart: Chart
  let netChart: Chart
  let tempChart: Chart
  let powerChart: Chart

  function getAuthHeaders(): Record<string, string> {
    const token = localStorage.getItem('auth_token') || sessionStorage.getItem('auth_token')
    return token ? { Authorization: `Bearer ${token}` } : {}
  }

  function getTimeRange(): { from: string; to: string } {
    const to = new Date()
    const p = periods.find((x) => x.value === period) || periods[2]
    const from = new Date(to.getTime() - p.hours * 60 * 60 * 1000)
    return { from: from.toISOString(), to: to.toISOString() }
  }

  async function fetchHistory() {
    loading = true
    error = ''
    try {
      const { from, to } = getTimeRange()
      const cached = await getCachedHistory(device.id, resolution, from, to)
      if (cached && cached.length > 0) {
        history = mapPoints(cached)
        resolution = cached[0]?.resolution || resolution
        loading = false
        return
      }

      const params = new URLSearchParams({ from, to, resolution })
      const response = await fetch(`${serverBase}/api/devices/${device.id}/telemetry/history-v2?${params}`, {
        headers: getAuthHeaders()
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const data = await response.json()
      history = mapPoints(data.points || [])
      resolution = data.resolution || 'auto'

      await setCachedHistory(device.id, resolution, from, to, data.points || [])
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load history'
    } finally {
      loading = false
      setTimeout(buildCharts, 50)
    }
  }

  function mapPoints(points: any[]): ChartPoint[] {
    return points.map((p: any) => {
      const rx = p.net_rx_avg || 0
      const tx = p.net_tx_avg || 0
      return {
        time: p.timestamp,
        cpu: p.cpu_avg || 0,
        memory: p.mem_avg || 0,
        netRx: rx,
        netTx: tx,
        temp: p.temp_avg || 0,
        power: p.power_avg || 0
      }
    })
  }

  function buildCharts() {
    if (!cpuCanvas || history.length === 0) return
    const labels = history.map((p) => {
      const d = new Date(p.time)
      return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
    })

    const makeLine = (label: string, color: string, data: number[]) => ({
      label,
      borderColor: color,
      backgroundColor: color + '18',
      borderWidth: 1.5,
      pointRadius: 0,
      tension: 0.3,
      fill: false,
      data
    })

    const commonOptions = {
      responsive: true,
      maintainAspectRatio: false,
      plugins: { legend: { display: false }, tooltip: { mode: 'index' as const, intersect: false } },
      scales: {
        x: { display: false },
        y: { display: true, grid: { color: 'rgba(255,255,255,0.06)' }, ticks: { color: '#888', font: { size: 10 } } }
      },
      animation: { duration: 300 }
    }

    cpuChart?.destroy()
    memChart?.destroy()
    netChart?.destroy()
    tempChart?.destroy()
    powerChart?.destroy()

    cpuChart = new Chart(cpuCanvas, {
      type: 'line',
      data: { labels, datasets: [makeLine('CPU %', '#3b82f6', history.map((p) => p.cpu))] },
      options: { ...commonOptions, scales: { ...commonOptions.scales, y: { ...commonOptions.scales.y, min: 0, max: 100 } } }
    })

    memChart = new Chart(memCanvas, {
      type: 'line',
      data: { labels, datasets: [makeLine('Memory %', '#8b5cf6', history.map((p) => p.memory))] },
      options: { ...commonOptions, scales: { ...commonOptions.scales, y: { ...commonOptions.scales.y, min: 0, max: 100 } } }
    })

    netChart = new Chart(netCanvas, {
      type: 'line',
      data: { labels, datasets: [makeLine('RX KB/s', '#10b981', history.map((p) => p.netRx / 1024)), makeLine('TX KB/s', '#f59e0b', history.map((p) => p.netTx / 1024))] },
      options: { ...commonOptions, scales: { ...commonOptions.scales, y: { ...commonOptions.scales.y, min: 0 } }, plugins: { ...commonOptions.plugins, legend: { display: true, labels: { color: '#aaa', boxWidth: 10, font: { size: 10 } } } } }
    })

    tempChart = new Chart(tempCanvas, {
      type: 'line',
      data: { labels, datasets: [makeLine('Temp °C', '#ef4444', history.map((p) => p.temp))] },
      options: { ...commonOptions, scales: { ...commonOptions.scales, y: { ...commonOptions.scales.y, min: 0 } } }
    })

    powerChart = new Chart(powerCanvas, {
      type: 'line',
      data: { labels, datasets: [makeLine('Watts', '#f97316', history.map((p) => p.power))] },
      options: { ...commonOptions, scales: { ...commonOptions.scales, y: { ...commonOptions.scales.y, min: 0 } } }
    })
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function close() {
    open = false
    dispatch('close')
  }

  $: if (open && period) {
    fetchHistory()
  }

  onMount(() => {
    document.addEventListener('keydown', handleKeydown)
    return () => document.removeEventListener('keydown', handleKeydown)
  })
</script>

{#if open}
  <div class="modal-overlay" on:click|self={close}>
    <div class="modal history-modal">
      <div class="modal-header">
        <h3>History — {device.hostname}</h3>
        <button class="close-btn" on:click={close}>✕</button>
      </div>
      <div class="modal-body">
        <div class="period-tabs">
          {#each periods as p}
            <button class="tab-btn" class:active={period === p.value} on:click={() => (period = p.value)}>
              {p.label}
            </button>
          {/each}
          {#if resolution !== 'auto'}
            <span class="resolution-badge">resolution: {resolution}</span>
          {/if}
        </div>

        {#if loading}
          <div class="chart-loading">Loading history...</div>
        {:else if error}
          <div class="chart-error">{error}</div>
        {:else if history.length === 0}
          <div class="chart-empty">No telemetry data for this period.</div>
        {:else}
          <div class="chart-grid">
            <div class="chart-cell">
              <span class="chart-label">CPU</span>
              <canvas bind:this={cpuCanvas}></canvas>
            </div>
            <div class="chart-cell">
              <span class="chart-label">Memory</span>
              <canvas bind:this={memCanvas}></canvas>
            </div>
            <div class="chart-cell">
              <span class="chart-label">Network</span>
              <canvas bind:this={netCanvas}></canvas>
            </div>
            <div class="chart-cell">
              <span class="chart-label">Temperature</span>
              <canvas bind:this={tempCanvas}></canvas>
            </div>
            <div class="chart-cell">
              <span class="chart-label">Power</span>
              <canvas bind:this={powerCanvas}></canvas>
            </div>
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-overlay {
    position: fixed; inset: 0; z-index: 1000;
    background: rgba(0, 0, 0, 0.6);
    display: flex; align-items: center; justify-content: center;
  }
  .modal {
    background: var(--surface, #1e1e2e);
    border: 1px solid var(--border, #333);
    border-radius: 12px;
    width: 95vw; max-width: 1100px;
    max-height: 90vh;
    display: flex; flex-direction: column;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
  }
  .modal-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 16px 20px;
    border-bottom: 1px solid var(--border, #333);
  }
  .modal-header h3 { margin: 0; font-size: 16px; color: var(--text, #eee); }
  .close-btn {
    background: none; border: none; color: #888; font-size: 18px;
    cursor: pointer; padding: 4px 8px;
  }
  .close-btn:hover { color: #fff; }
  .modal-body { padding: 16px 20px; overflow-y: auto; flex: 1; }

  .period-tabs {
    display: flex; gap: 4px; margin-bottom: 16px; flex-wrap: wrap; align-items: center;
  }
  .tab-btn {
    background: var(--surface-alt, #2a2a3e);
    border: 1px solid var(--border, #333);
    color: #aaa; padding: 6px 14px; border-radius: 6px;
    cursor: pointer; font-size: 12px; font-weight: 500;
  }
  .tab-btn:hover { background: #333; color: #fff; }
  .tab-btn.active { background: var(--primary, #3b82f6); color: #fff; border-color: var(--primary, #3b82f6); }
  .resolution-badge {
    margin-left: 12px; font-size: 11px; color: #888;
    background: var(--surface-alt, #2a2a3e); padding: 4px 8px; border-radius: 4px;
  }

  .chart-loading, .chart-error, .chart-empty {
    padding: 40px; text-align: center; color: #888; font-size: 13px;
  }
  .chart-error { color: #ef4444; }

  .chart-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  .chart-cell {
    background: var(--surface-alt, #2a2a3e);
    border: 1px solid var(--border, #333);
    border-radius: 8px;
    padding: 10px 12px;
    position: relative;
  }
  .chart-label {
    font-size: 11px; color: #888; font-weight: 600; text-transform: uppercase;
    letter-spacing: 0.5px; margin-bottom: 6px; display: block;
  }
  canvas { width: 100% !important; height: 120px !important; }

  @media (max-width: 600px) {
    .chart-grid { grid-template-columns: 1fr; }
  }
</style>
