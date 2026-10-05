<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { deviceHistoryStore, type ChartPoint } from '../lib/telemetry-store'

  export let device: { id: string }

  let canvas: HTMLCanvasElement
  let history: ChartPoint[] = []
  let unsubscribe: (() => void) | null = null
  let raf = 0

  const HEIGHT = 72
  const PAD_LEFT = 24
  const PAD_RIGHT = 4
  const PAD_TOP = 7
  const AXIS_BOTTOM = 12
  const PLOT_HEIGHT = HEIGHT - PAD_TOP - AXIS_BOTTOM

  const COLORS = {
    cpu: '#2dd4bf',
    mem: '#3b82f6',
    net: '#fbbf24'
  }

  // Each series keeps its own scale and the legend says which is which. CPU and
  // memory are bounded percentages sharing one 0-100 axis; network is a rate
  // whose ceiling follows the data. Drawing all three on one 0-100 axis made
  // them impossible to compare and gave no hint of what they measured.
  type Series = {
    key: string
    label: string
    color: string
    unit: string
    values: number[]
    max: number
    dashed?: boolean
  }

  $: series = buildSeries(history)

  // Round a peak up to a readable ceiling so the legend prints a round number.
  function niceMax(value: number): number {
    if (value <= 0) return 1024
    const magnitude = Math.pow(10, Math.floor(Math.log10(value)))
    const normalized = value / magnitude
    const step = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10
    return step * magnitude
  }

  function buildSeries(points: ChartPoint[]): Series[] {
    let netPeak = 0
    for (const p of points) {
      if (p.netRx > netPeak) netPeak = p.netRx
      if (p.netTx > netPeak) netPeak = p.netTx
    }
    return [
      {
        key: 'cpu',
        label: 'CPU',
        color: COLORS.cpu,
        unit: '%',
        values: points.map((p) => p.cpu),
        max: 100
      },
      {
        key: 'memory',
        label: 'RAM',
        color: COLORS.mem,
        unit: '%',
        values: points.map((p) => p.memory),
        max: 100
      },
      {
        key: 'netRx',
        label: 'Net down',
        color: COLORS.net,
        unit: '/s',
        values: points.map((p) => p.netRx),
        max: niceMax(netPeak),
        dashed: true
      }
    ]
  }

  function formatRate(value: number): string {
    if (value <= 0) return '0 B/s'
    const units = ['B', 'KB', 'MB', 'GB']
    const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
    return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}/s`
  }

  onMount(() => {
    // device.id is stable for a keyed card, so a single subscription is enough.
    unsubscribe = deviceHistoryStore(device.id).subscribe((points) => {
      history = points
      scheduleDraw()
    })
  })

  onDestroy(() => {
    unsubscribe?.()
    if (raf) cancelAnimationFrame(raf)
  })

  function scheduleDraw() {
    if (raf) return
    raf = requestAnimationFrame(() => {
      raf = 0
      draw()
    })
  }

  function draw() {
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const cssWidth = canvas.clientWidth || canvas.parentElement?.clientWidth || 240
    const dpr = Math.min(window.devicePixelRatio || 1, 2)
    const pixelWidth = Math.max(1, Math.floor(cssWidth * dpr))
    const pixelHeight = Math.floor(HEIGHT * dpr)
    if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
      canvas.width = pixelWidth
      canvas.height = pixelHeight
    }

    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.clearRect(0, 0, cssWidth, HEIGHT)

    const plotWidth = cssWidth - PAD_LEFT - PAD_RIGHT
    if (plotWidth <= 0) return

    // The shared 0-100 percentage axis, drawn first so the series sit on top.
    drawPercentAxis(ctx, plotWidth)

    if (history.length < 2) return

    drawSeries(ctx, series[0], plotWidth)
    drawSeries(ctx, series[1], plotWidth)
    // Network is normalised to its own ceiling and drawn dashed so it is never
    // mistaken for a percentage.
    drawSeries(ctx, series[2], plotWidth)
  }

  function drawPercentAxis(ctx: CanvasRenderingContext2D, plotWidth: number) {
    ctx.strokeStyle = 'rgba(148, 163, 184, 0.16)'
    ctx.lineWidth = 1
    ctx.fillStyle = 'rgba(148, 163, 184, 0.7)'
    ctx.font = '8px system-ui, sans-serif'
    ctx.textAlign = 'right'
    ctx.textBaseline = 'middle'

    for (const percent of [0, 50, 100]) {
      const y = Math.round(PAD_TOP + PLOT_HEIGHT - (percent / 100) * PLOT_HEIGHT) + 0.5
      ctx.beginPath()
      ctx.moveTo(PAD_LEFT, y)
      ctx.lineTo(PAD_LEFT + plotWidth, y)
      ctx.stroke()
      ctx.fillText(String(percent), PAD_LEFT - 3, y)
    }
  }

  function drawSeries(ctx: CanvasRenderingContext2D, series: Series, plotWidth: number) {
    const values = series.values
    if (values.length < 2) return

    const stepX = plotWidth / (values.length - 1)
    const scaleY = PLOT_HEIGHT / series.max

    ctx.save()
    ctx.beginPath()
    ctx.rect(PAD_LEFT, PAD_TOP, plotWidth, PLOT_HEIGHT)
    ctx.clip()

    ctx.beginPath()
    ctx.strokeStyle = series.color
    ctx.lineWidth = 1.5
    ctx.lineJoin = 'round'
    if (series.dashed) ctx.setLineDash([3, 2])

    for (let i = 0; i < values.length; i++) {
      const x = PAD_LEFT + i * stepX
      const clamped = Math.max(0, Math.min(values[i], series.max))
      const y = PAD_TOP + PLOT_HEIGHT - clamped * scaleY
      if (i === 0) ctx.moveTo(x, y)
      else ctx.lineTo(x, y)
    }
    ctx.stroke()
    ctx.restore()
  }
</script>

<div class="sparkline">
  <canvas bind:this={canvas}></canvas>
  <!-- The legend names every series and prints the network ceiling, which the
       canvas alone cannot convey. -->
  <ul class="spark-legend">
    {#each series as s (s.key)}
      <li style="--legend-color: {s.color}">
        <span class="legend-swatch" class:dashed={s.dashed}></span>
        <span class="legend-label">{s.label}</span>
        <span class="legend-scale">{s.key === 'netRx' ? formatRate(s.max) : `0-100${s.unit}`}</span>
      </li>
    {/each}
  </ul>
</div>

<style>
  .sparkline {
    width: 100%;
  }

  canvas {
    display: block;
    width: 100%;
    height: 72px;
  }

  .spark-legend {
    display: flex;
    flex-wrap: wrap;
    gap: 0.15rem 0.5rem;
    margin: 0.25rem 0 0;
    padding: 0;
    list-style: none;
  }

  .spark-legend li {
    display: flex;
    align-items: center;
    gap: 0.2rem;
  }

  .legend-swatch {
    width: 9px;
    height: 2px;
    border-radius: 1px;
    background: var(--legend-color);
  }

  .legend-swatch.dashed {
    background: repeating-linear-gradient(
      to right,
      var(--legend-color) 0 3px,
      transparent 3px 5px
    );
  }

  .legend-label {
    color: #cbd5e1;
    font-size: 0.56rem;
  }

  .legend-scale {
    color: #64748b;
    font-size: 0.52rem;
  }
</style>
