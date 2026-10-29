<script module lang="ts">
  export type ChartSeries = {
    label: string
    values: (number | null)[]
    color: string
    axis?: 'left' | 'right'
    format?: (value: number) => string
    // Series sharing a stack id are drawn as one stacked area (the network
    // screen uses this so the top of the stack is the total traffic).
    stack?: string
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
    Filler,
    Tooltip,
    Legend,
    type ChartConfiguration
  } from 'chart.js'
  import { resolveCssColor } from '../../lib/color'
  import { seriesStats } from '../../lib/telemetry-history'
  import {
    buildTimeTicks,
    formatTimeTick,
    formatTooltipTime,
    parseTimestamp,
    timeDomain,
    withAlpha
  } from '../../lib/chart'

  // One reusable plot for every telemetry screen. A linear x axis places each
  // sample at its real timestamp inside a fixed window, so the line grows from
  // the left edge as data arrives instead of being stretched across the card
  // (the category axis this replaced did exactly that). Monotone interpolation
  // removes the spline overshoot that used to invent peaks at the start of a
  // series.
  Chart.register(LineController, LineElement, PointElement, LinearScale, Filler, Tooltip, Legend)

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
  // Fixed span of the x axis. Without it a sparse window would still stretch
  // its few samples edge to edge, because the axis would fit the data.
  export let windowMs: number | null = null
  // Optional right edge of the window (the "to" of a history fetch). Live
  // charts omit it and anchor on the newest sample.
  export let windowEnd: number | null = null

  type LegendEntry = { series: ChartSeries; stats: NonNullable<ReturnType<typeof seriesStats>> }

  let canvas: HTMLCanvasElement
  let chart: Chart | null = null

  $: visible = series.filter((s) => s.values.some((value) => value != null))
  $: hasData = visible.some((s) => s.values.filter((value) => value != null).length >= 2)
  $: hasRight = visible.some((s) => s.axis === 'right')
  $: legend = visible
    .map((s) => ({ series: s, stats: seriesStats(s.values.filter((value): value is number => value != null)) }))
    .filter((entry): entry is LegendEntry => entry.stats != null)
  $: domain = timeDomain(labels, windowMs, windowEnd)

  function fmt(entry: ChartSeries, value: number): string {
    if (entry.format) return entry.format(value)
    return `${value.toFixed(0)}${entry.axis === 'right' ? rightUnit : leftUnit}`
  }

  function axisTick(axis: 'left' | 'right', value: number): string {
    const format = axis === 'right' ? rightFormat : leftFormat
    if (format) return format(value)
    return `${value.toFixed(0)}${axis === 'right' ? rightUnit : leftUnit}`
  }

  // A vertical gradient, like Beszel's filled areas: readable near the line,
  // nearly transparent at the baseline so overlapping series stay distinct.
  function areaFill(color: string) {
    return (context: any) => {
      const { chart: instance } = context
      const area = instance?.chartArea
      if (!area) return withAlpha(color, 0.16)
      const gradient = instance.ctx.createLinearGradient(0, area.top, 0, area.bottom)
      gradient.addColorStop(0, withAlpha(color, 0.32))
      gradient.addColorStop(1, withAlpha(color, 0.02))
      return gradient
    }
  }

  function chartPoints(values: (number | null)[]) {
    const points: { x: number; y: number | null }[] = []
    labels.forEach((label, index) => {
      const x = parseTimestamp(label)
      if (Number.isFinite(x)) points.push({ x, y: values[index] ?? null })
    })
    return points
  }

  function buildConfig(): ChartConfiguration<'line'> {
    const span = domain ? domain.to - domain.from : 0
    const datasets = visible.map((entry) => {
      const color = resolveCssColor(entry.color)
      return {
        label: entry.label,
        data: chartPoints(entry.values),
        borderColor: color,
        backgroundColor: areaFill(color),
        fill: true,
        borderWidth: 1.6,
        pointRadius: 0,
        pointHoverRadius: 2.5,
        pointHoverBorderWidth: 0,
        pointHitRadius: 12,
        spanGaps: false,
        cubicInterpolationMode: 'monotone' as const,
        stack: entry.stack,
        yAxisID: entry.axis === 'right' ? 'y1' : 'y',
        // Kept for the tooltip, which needs the series' own unit/format.
        __series: entry
      }
    })

    return {
      type: 'line',
      data: { datasets: datasets as any },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        interaction: { intersect: false, mode: 'index' },
        layout: { padding: { top: 4, right: 2 } },
        plugins: {
          legend: { display: false },
          tooltip: {
            backgroundColor: resolveCssColor('var(--chart-tooltip)', 'rgba(20, 22, 27, 0.96)'),
            titleColor: '#e7e9ee',
            bodyColor: '#c7cbd4',
            borderColor: resolveCssColor('var(--chart-tooltip-border)', 'rgba(148, 163, 184, 0.2)'),
            borderWidth: 1,
            padding: 10,
            cornerRadius: 8,
            boxWidth: 8,
            boxHeight: 8,
            usePointStyle: true,
            titleFont: { size: 11, weight: 600 },
            bodyFont: { size: 11 },
            callbacks: {
              title: (items) => formatTooltipTime(items[0]?.parsed.x ?? 0),
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
            type: 'linear',
            min: domain?.from,
            max: domain?.to,
            display: true,
            grid: { display: false },
            border: { color: resolveCssColor('var(--chart-baseline)', 'rgba(148, 163, 184, 0.18)') },
            afterBuildTicks: (axis) => {
              if (!domain) return
              axis.ticks = buildTimeTicks(domain.from, domain.to).map((value) => ({ value }))
            },
            ticks: {
              color: resolveCssColor('var(--chart-axis)', '#8b8f9a'),
              font: { size: 10 },
              maxRotation: 0,
              autoSkip: true,
              padding: 6,
              callback: (value) => formatTimeTick(Number(value), span)
            }
          },
          y: {
            position: 'left',
            beginAtZero: true,
            suggestedMax: leftMax ?? undefined,
            grid: { color: resolveCssColor('var(--chart-grid)', 'rgba(148, 163, 184, 0.1)') },
            border: { display: false },
            ticks: {
              color: resolveCssColor('var(--chart-axis)', '#8b8f9a'),
              font: { size: 10 },
              maxTicksLimit: 5,
              padding: 6,
              callback: (value) => axisTick('left', Number(value))
            }
          },
          ...(hasRight
            ? {
                y1: {
                  position: 'right' as const,
                  beginAtZero: true,
                  suggestedMax: rightMax ?? undefined,
                  grid: { drawOnChartArea: false },
                  border: { display: false },
                  ticks: {
                    color: resolveCssColor('var(--chart-axis)', '#8b8f9a'),
                    font: { size: 10 },
                    maxTicksLimit: 5,
                    padding: 6,
                    callback: (value: string | number) => axisTick('right', Number(value))
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
    gap: 0.32rem;
    min-width: 0;
    font-size: 0.72rem;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .dot {
    width: 8px;
    height: 8px;
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
