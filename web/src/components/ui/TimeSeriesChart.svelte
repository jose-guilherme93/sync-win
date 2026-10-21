<script module lang="ts">
  export type ChartSeries = {
    label: string
    values: (number | null)[]
    color: string
    axis?: 'left' | 'right'
    format?: (value: number) => string
  }
</script>

<script lang="ts">
  import { afterUpdate, onMount, onDestroy } from 'svelte'
  import {
    Chart,
    LineController,
    LineElement,
    PointElement,
    LinearScale,
    CategoryScale,
    Tooltip,
    Legend,
    type ChartConfiguration
  } from 'chart.js'
  import { resolveCssColor } from '../../lib/color'
  import { seriesStats } from '../../lib/telemetry-history'

  // One reusable line chart for the device screens. Chart.js is already a
  // dependency (SystemMetrics uses it); using it here buys axes, ticks and a
  // crosshair tooltip that the hand-drawn SVG history chart never had.
  Chart.register(LineController, LineElement, PointElement, LinearScale, CategoryScale, Tooltip, Legend)

  export let labels: string[] = []
  export let series: ChartSeries[] = []
  export let leftUnit = '%'
  export let rightUnit = ''
  export let leftMax: number | null = null
  export let rightMax: number | null = null
  export let leftFormat: ((value: number) => string) | null = null
  export let rightFormat: ((value: number) => string) | null = null
  export let height = 240
  export let emptyMessage = 'No samples in this window yet.'

  type LegendEntry = { series: ChartSeries; stats: NonNullable<ReturnType<typeof seriesStats>> }

  let canvas: HTMLCanvasElement
  let chart: Chart | null = null

  $: visible = series.filter((s) => s.values.some((value) => value != null))
  $: hasData = visible.some((s) => s.values.filter((value) => value != null).length >= 2)
  $: hasRight = visible.some((s) => s.axis === 'right')
  $: legend = visible
    .map((s) => ({ series: s, stats: seriesStats(s.values.filter((value): value is number => value != null)) }))
    .filter((entry): entry is LegendEntry => entry.stats != null)
  $: multiDay = labels.length > 1
    && (Date.parse(labels[labels.length - 1]) - Date.parse(labels[0])) > 24 * 60 * 60 * 1000

  function fmt(entry: ChartSeries, value: number): string {
    if (entry.format) return entry.format(value)
    return `${value.toFixed(0)}${entry.axis === 'right' ? rightUnit : leftUnit}`
  }

  function axisTick(axis: 'left' | 'right', value: number): string {
    const format = axis === 'right' ? rightFormat : leftFormat
    if (format) return format(value)
    return `${value.toFixed(0)}${axis === 'right' ? rightUnit : leftUnit}`
  }

  function timeLabel(iso: string): string {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return iso
    const time = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
    if (!multiDay) return time
    return `${date.toLocaleDateString(undefined, { day: '2-digit', month: '2-digit' })} ${time}`
  }

  function withAlpha(color: string, alpha: number): string {
    const hex = /^#([0-9a-f]{6})$/i.exec(color)
    if (!hex) return color
    const value = parseInt(hex[1], 16)
    return `rgba(${(value >> 16) & 255}, ${(value >> 8) & 255}, ${value & 255}, ${alpha})`
  }

  function buildConfig(): ChartConfiguration<'line'> {
    const datasets = visible.map((entry) => {
      const color = resolveCssColor(entry.color)
      return {
        label: entry.label,
        data: entry.values,
        borderColor: color,
        backgroundColor: entry.axis === 'right' ? 'transparent' : withAlpha(color, 0.08),
        borderWidth: 1.5,
        pointRadius: 0,
        pointHitRadius: 10,
        spanGaps: true,
        tension: 0.25,
        yAxisID: entry.axis === 'right' ? 'y1' : 'y',
        // Kept for the tooltip, which needs the series' own unit/format.
        __series: entry
      }
    })

    return {
      type: 'line',
      data: { labels, datasets: datasets as any },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        interaction: { intersect: false, mode: 'index' },
        plugins: {
          legend: { display: false },
          tooltip: {
            backgroundColor: 'rgba(15, 18, 22, 0.96)',
            titleColor: '#e7e9ee',
            bodyColor: '#c7cbd4',
            borderColor: 'rgba(148, 163, 184, 0.25)',
            borderWidth: 1,
            padding: 9,
            titleFont: { size: 11 },
            bodyFont: { size: 11 },
            callbacks: {
              title: (items) => labels[items[0]?.dataIndex] ?? '',
              label: (item) => {
                const value = item.parsed.y
                if (value == null) return ''
                const source = (item.dataset as any).__series as ChartSeries
                return `${item.dataset.label}: ${fmt(source, value)}`
              }
            }
          }
        },
        scales: {
          x: {
            display: true,
            grid: { display: false },
            border: { color: 'rgba(148, 163, 184, 0.18)' },
            ticks: {
              color: '#9398a6',
              font: { size: 10 },
              maxTicksLimit: 8,
              maxRotation: 0,
              callback: (_value, index) => timeLabel(labels[index] || '')
            }
          },
          y: {
            position: 'left',
            beginAtZero: true,
            suggestedMax: leftMax ?? undefined,
            grid: { color: 'rgba(148, 163, 184, 0.10)' },
            border: { display: false },
            ticks: {
              color: '#9398a6',
              font: { size: 10 },
              maxTicksLimit: 5,
              callback: (value) => axisTick('left', Number(value))
            }
          },
          ...(hasRight
            ? {
                y1: {
                  position: 'right',
                  beginAtZero: true,
                  suggestedMax: rightMax ?? undefined,
                  grid: { drawOnChartArea: false },
                  border: { display: false },
                  ticks: {
                    color: '#9398a6',
                    font: { size: 10 },
                    maxTicksLimit: 5,
                    callback: (value) => axisTick('right', Number(value))
                  }
                }
              }
            : {})
        }
      }
    }
  }

  function syncChart() {
    if (!canvas) return
    if (!hasData) {
      chart?.destroy()
      chart = null
      return
    }
    const config = buildConfig()
    if (!chart) {
      chart = new Chart(canvas, config)
    } else {
      chart.data = config.data
      chart.options = config.options as any
      chart.update('none')
    }
  }

  // afterUpdate runs for every prop change and bind:this assignment, which is
  // what makes the chart follow new data on all instances (a hand-written
  // dependency list only reliably caught the first chart).
  afterUpdate(() => {
    if (canvas) syncChart()
  })

  onMount(syncChart)
  onDestroy(() => {
    chart?.destroy()
    chart = null
  })
</script>

<div class="chart-block">
  {#if legend.length > 0}
    <div class="legend" aria-label="Series statistics">
      {#each legend as entry (entry.series.label)}
        <span class="legend-item">
          <i class="dot" style="background: {entry.series.color}"></i>
          <span class="name">{entry.series.label}</span>
          <b class="last" title="Last sample in the window">{fmt(entry.series, entry.stats.last)}</b>
          <span class="stats">
            <span title="Average">ø {fmt(entry.series, entry.stats.avg)}</span>
            <span class="sep">·</span>
            <span title="Minimum">↓ {fmt(entry.series, entry.stats.min)}</span>
            <span class="sep">·</span>
            <span title="Maximum">↑ {fmt(entry.series, entry.stats.max)}</span>
          </span>
        </span>
      {/each}
    </div>
  {/if}

  <div class="canvas-wrap" style:height={`${height}px`}>
    <canvas bind:this={canvas} class:hidden={!hasData}></canvas>
    {#if !hasData}
      <p class="empty">{emptyMessage}</p>
    {/if}
  </div>
</div>

<style>
  .chart-block { display: grid; gap: 0.45rem; min-width: 0; }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem 1rem;
    align-items: baseline;
  }

  .legend-item {
    display: inline-flex;
    align-items: baseline;
    gap: 0.3rem;
    min-width: 0;
    font-size: 0.7rem;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex: none;
    transform: translateY(-1px);
  }

  .name { color: var(--text-muted); }
  .last { color: var(--text-bright); font-variant-numeric: tabular-nums; }
  .stats { color: var(--text-faint); font-variant-numeric: tabular-nums; }
  .sep { padding: 0 0.1rem; }

  .canvas-wrap { position: relative; min-width: 0; }
  canvas { width: 100% !important; height: 100% !important; }
  canvas.hidden { visibility: hidden; }

  .empty {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    margin: 0;
    padding: 1rem;
    color: var(--text-faint);
    font-size: 0.78rem;
    text-align: center;
  }
</style>
