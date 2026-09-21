<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { deviceHistoryStore, type ChartPoint } from '../lib/telemetry-store'

  export let device: { id: string }

  let canvas: HTMLCanvasElement
  let history: ChartPoint[] = []
  let unsubscribe: (() => void) | null = null
  let raf = 0

  const HEIGHT = 72
  const COLORS = {
    cpu: '#2dd4bf',
    mem: '#3b82f6',
    net: '#fbbf24'
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

    ctx.strokeStyle = 'rgba(148, 163, 184, 0.12)'
    ctx.lineWidth = 1
    ctx.beginPath()
    ctx.moveTo(0, HEIGHT / 2)
    ctx.lineTo(cssWidth, HEIGHT / 2)
    ctx.stroke()

    if (history.length < 2) return

    const pad = 3
    const plotHeight = HEIGHT - pad * 2

    let netMax = 1
    for (const p of history) {
      if (p.netRx > netMax) netMax = p.netRx
      if (p.netTx > netMax) netMax = p.netTx
    }

    drawSeries(ctx, history.map((p) => p.cpu), 100, COLORS.cpu, cssWidth, pad, plotHeight)
    drawSeries(ctx, history.map((p) => p.memory), 100, COLORS.mem, cssWidth, pad, plotHeight)
    drawSeries(ctx, history.map((p) => p.netRx), netMax, COLORS.net, cssWidth, pad, plotHeight, 0.7)
  }

  function drawSeries(
    ctx: CanvasRenderingContext2D,
    values: number[],
    max: number,
    color: string,
    width: number,
    pad: number,
    plotHeight: number,
    alpha = 1
  ) {
    if (values.length < 2) return
    const stepX = width / (values.length - 1)
    const scaleY = max > 0 ? plotHeight / max : 0

    ctx.beginPath()
    ctx.strokeStyle = color
    ctx.globalAlpha = alpha
    ctx.lineWidth = 1.5
    ctx.lineJoin = 'round'
    for (let i = 0; i < values.length; i++) {
      const x = i * stepX
      const clamped = Math.max(0, Math.min(values[i], max))
      const y = pad + plotHeight - clamped * scaleY
      if (i === 0) ctx.moveTo(x, y)
      else ctx.lineTo(x, y)
    }
    ctx.stroke()
    ctx.globalAlpha = 1
  }
</script>

<div class="sparkline">
  <canvas bind:this={canvas}></canvas>
</div>

<style>
  .sparkline {
    width: 100%;
    height: 72px;
  }

  canvas {
    display: block;
    width: 100%;
    height: 100%;
  }
</style>
