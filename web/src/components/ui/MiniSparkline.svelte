<script lang="ts">
  import { onMount } from 'svelte'
  import { resolveCssColor } from '../../lib/color'

  // A minimal canvas sparkline that takes plain numbers. The existing
  // components/Sparkline.svelte is bound to the shared device telemetry store
  // and weaves three series together with a legend; that is the right tool for
  // a device card, but telemetry cards need to plot an arbitrary series (a
  // single sensor, a disk's history) without touching the store. This is that
  // smaller tool.
  export let series: number[] = []
  export let color = 'var(--accent)'
  export let height = 28
  export let fill = true
  export let label = ''

  let canvas: HTMLCanvasElement | null = null

  function draw() {
    if (!canvas || series.length < 2) return
    const dpr = window.devicePixelRatio || 1
    const width = canvas.clientWidth || 120
    const h = height
    canvas.width = Math.round(width * dpr)
    canvas.height = Math.round(h * dpr)

    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.clearRect(0, 0, width, h)

    // Resolve "var(--…)" to a concrete colour: the canvas API ignores custom
    // properties and would otherwise keep the context's default black.
    const stroke = resolveCssColor(color)

    let lo = Infinity
    let hi = -Infinity
    for (const v of series) {
      if (!Number.isFinite(v)) continue
      if (v < lo) lo = v
      if (v > hi) hi = v
    }
    if (!Number.isFinite(lo)) return
    // A dead-flat series has zero range; give it a hair of padding so the line
    // lands mid-canvas instead of on the edge or dividing by zero.
    const range = hi - lo || 1
    const pad = 3
    const stepX = width / (series.length - 1)

    const y = (v: number) => pad + (1 - (v - lo) / range) * (h - pad * 2)

    ctx.beginPath()
    series.forEach((v, i) => {
      const x = i * stepX
      const yy = y(Number.isFinite(v) ? v : lo)
      if (i === 0) ctx.moveTo(x, yy)
      else ctx.lineTo(x, yy)
    })

    if (fill) {
      ctx.save()
      ctx.lineTo(width, h)
      ctx.lineTo(0, h)
      ctx.closePath()
      ctx.globalAlpha = 0.12
      ctx.fillStyle = stroke
      ctx.fill()
      ctx.restore()
    }

    ctx.beginPath()
    series.forEach((v, i) => {
      const x = i * stepX
      const yy = y(Number.isFinite(v) ? v : lo)
      if (i === 0) ctx.moveTo(x, yy)
      else ctx.lineTo(x, yy)
    })
    ctx.strokeStyle = stroke
    ctx.lineWidth = 1.5
    ctx.lineJoin = 'round'
    ctx.stroke()
  }

  $: series, color, draw()
  onMount(draw)
</script>

<canvas bind:this={canvas} style="height: {height}px" role={label ? 'img' : undefined} aria-label={label || undefined}></canvas>

<style>
  canvas {
    display: block;
    width: 100%;
  }
</style>
