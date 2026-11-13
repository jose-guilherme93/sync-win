<script module lang="ts">
  // One SVG telemetry plot, drawn straight from the approved prototype: fixed
  // axis rulers, gridlines, real timestamps on a linear axis that never
  // stretches, series strokes with non-scaling width and no other colour on
  // the card. Loading state is the 40 px mountain stroke, never a skeleton.
  export type PlotSeries = {
    label: string
    values: (number | null)[]
    color: string
    axis?: 'left' | 'right'
    format?: (value: number) => string
  }
</script>

<script lang="ts">
  import { onDestroy } from 'svelte'
  import { prefersReducedMotion } from '../../lib/telemetry-series'
  import { seriesStats } from '../../lib/telemetry-history'
  import {
    buildTimeTicks,
    formatTimeTick,
    niceMax,
    parseTimestamp,
    timeDomain,
    type TimeDomain
  } from '../../lib/chart'

  // Legend and ruler share one formatting rule so a number never changes shape
  // between the axis and the statistics row.
  function rulerValue(
    entry: { format?: (value: number) => string; axis?: 'left' | 'right' },
    value: number
  ): string {
    if (entry.format) return entry.format(value)
    return `${value.toFixed(0)}${entry.axis === 'right' ? rightUnit : leftUnit}`
  }

  export let labels: string[] = []
  export let series: PlotSeries[] = []
  export let height = 240
  // Fixed span of the x axis, so a sparse window stays mostly empty instead of
  // being stretched edge to edge (the project rule for time axes).
  export let windowMs: number | null = null
  export let windowEnd: number | null = null
  export let leftUnit = ''
  export let rightUnit = ''
  export let leftMax: number | null = null
  export let rightMax: number | null = null
  export let leftFormat: ((value: number) => string) | null = null
  export let rightFormat: ((value: number) => string) | null = null
  // While the first fetch is in flight the plot is unknown, not empty: the
  // mountain stroke stands in, with the axis and the card title still visible.
  export let loading = false
  export let emptyMessage = 'No samples in this window yet.'
  // Right-to-left reveal, run once per load: 0..1 is the fraction already on
  // screen at the first frame. `revealId` restarts it; a poll never does.
  export let revealFrom: number | null = null
  export let revealId = 0

  // viewBox width. Height maps to the prop so the vertical scale is in real
  // pixels; preserveAspectRatio="none" only stretches the time axis.
  const W = 1000

  // The reveal animates a clip, not the data: the paths are built once and the
  // window is uncovered from the right at the display's refresh rate. Adding a
  // point to an array every few hundred milliseconds is what made it look like
  // five frames per second.
  const REVEAL_MS = 900
  let revealP = 1
  let revealRaf = 0
  let revealFor = -1

  function easeOut(t: number): number {
    return 1 - Math.pow(1 - t, 3)
  }

  function runReveal(from: number) {
    cancelAnimationFrame(revealRaf)
    if (from >= 1 || prefersReducedMotion()) {
      revealP = 1
      return
    }
    const started = performance.now()
    revealP = from
    const step = (now: number) => {
      const t = Math.min(1, (now - started) / REVEAL_MS)
      revealP = from + (1 - from) * easeOut(t)
      if (t < 1) revealRaf = requestAnimationFrame(step)
    }
    revealRaf = requestAnimationFrame(step)
  }

  onDestroy(() => cancelAnimationFrame(revealRaf))

  type LegendEntry = { series: PlotSeries; stats: NonNullable<ReturnType<typeof seriesStats>> }
  type BuiltPath = { series: PlotSeries; stroke: string; area: string; headTop: number | null }

  function finite(value: unknown): value is number {
    return typeof value === 'number' && Number.isFinite(value)
  }

  $: visible = series.filter((entry) => entry.values.some(finite))
  $: hasData = visible.some((entry) => entry.values.filter(finite).length >= 2)
  $: hasRight = visible.some((entry) => entry.axis === 'right')
  $: legend = visible
    .map((entry) => ({ series: entry, stats: seriesStats(entry.values.filter(finite)) }))
    .filter((entry): entry is LegendEntry => entry.stats != null)
  $: if (hasData && revealFrom != null && revealId !== revealFor) {
    revealFor = revealId
    runReveal(revealFrom)
  }
  $: domain = timeDomain(labels, windowMs, windowEnd)
  $: span = domain ? domain.to - domain.from : 0

  // Fixed ruler scale: explicit when the caller knows the bound (percent),
  // otherwise the smallest nice step that covers the plotted data.
  $: leftValues = series
    .filter((entry) => (entry.axis ?? 'left') === 'left')
    .flatMap((entry) => entry.values.filter(finite))
  $: rightValues = series.filter((entry) => entry.axis === 'right').flatMap((entry) => entry.values.filter(finite))
  $: leftScale = leftMax ?? (leftValues.length > 0 ? niceMax(Math.max(...leftValues)) : null)
  $: rightScale = rightMax ?? (rightValues.length > 0 ? niceMax(Math.max(...rightValues)) : null)
  $: leftRuler = leftScale != null ? [leftScale, leftScale / 2, 0] : []
  $: rightRuler = rightScale != null ? [rightScale, rightScale / 2, 0] : []

  function rulerLabel(axis: 'left' | 'right', value: number): string {
    const format = axis === 'right' ? rightFormat : leftFormat
    if (format) return format(value)
    return `${value.toFixed(0)}${axis === 'right' ? rightUnit : leftUnit}`
  }

  // Points are placed at their real timestamp inside the fixed window; a null
  // value breaks the line instead of pulling the neighbours across the gap.
  function buildPaths(
    labels: string[],
    series: PlotSeries[],
    domain: TimeDomain,
    scale: number,
    plotHeight: number
  ): BuiltPath[] {
    const { from, to } = domain
    const width = Math.max(to - from, 1)
    return series.map((entry) => {
      const segments: { x: number; y: number }[][] = []
      let current: { x: number; y: number }[] | null = null
      let previous = -2
      labels.forEach((label, index) => {
        const at = parseTimestamp(label)
        const value = entry.values[index]
        if (!Number.isFinite(at) || !finite(value) || at < from - 1 || at > to + 1) {
          current = null
          previous = -2
          return
        }
        const point = {
          x: ((at - from) / width) * W,
          y: plotHeight - (Math.min(Math.max(value, 0), scale) / scale) * plotHeight
        }
        if (!current || index !== previous + 1) {
          current = [point]
          segments.push(current)
        } else {
          current.push(point)
        }
        previous = index
      })

      const r = (value: number) => Math.round(value * 10) / 10
      const line = (segment: { x: number; y: number }[]) =>
        'M' + segment.map((point) => `${r(point.x)},${r(point.y)}`).join(' L')
      const stroke = segments.map(line).join(' ')
      const area = segments
        .filter((segment) => segment.length >= 2)
        .map(
          (segment) =>
            `${line(segment)} L${r(segment[segment.length - 1].x)},${plotHeight} L${r(segment[0].x)},${plotHeight} Z`
        )
        .join(' ')
      // Marker on the newest sample of the series (the live edge).
      let headTop: number | null = null
      for (let index = labels.length - 1; index >= 0; index--) {
        const value = entry.values[index]
        if (!finite(value)) continue
        const at = parseTimestamp(labels[index])
        if (!Number.isFinite(at) || at < from - 1 || at > to + 1) continue
        headTop = (1 - Math.min(Math.max(value, 0), scale) / scale) * 100
        break
      }
      return { series: entry, stroke, area, headTop }
    })
  }

  $: leftSeries = visible.filter((entry) => (entry.axis ?? 'left') === 'left')
  $: leftPaths = domain && leftScale != null ? buildPaths(labels, leftSeries, domain, leftScale, height) : []
  $: rightPaths = domain && rightScale != null ? buildPaths(labels, visible.filter((entry) => entry.axis === 'right'), domain, rightScale, height) : []
  $: head = leftPaths.find((path) => path.headTop != null)

  $: ticks = domain ? buildTimeTicks(domain.from, domain.to) : []
</script>

<div class="chart-block" data-points={labels.length}>
  {#if legend.length > 0}
    <div class="legend" aria-label="Series statistics">
      {#each legend as entry (entry.series.label)}
        <span class="legend-item">
          <i class="dot" style="background: {entry.series.color}"></i>
          <span class="name">{entry.series.label}</span>
          <b class="last" title="Last sample in the window">{rulerValue(entry.series, entry.stats.last)}</b>
          <span class="stats">
            <span title="Average">ø {rulerValue(entry.series, entry.stats.avg)}</span>
            <span class="sep">·</span>
            <span title="Minimum">↓ {rulerValue(entry.series, entry.stats.min)}</span>
            <span class="sep">·</span>
            <span title="Maximum">↑ {rulerValue(entry.series, entry.stats.max)}</span>
          </span>
        </span>
      {/each}
    </div>
  {/if}

  <div class="plot" style:height={`${height}px`}>
    <div class="yax" aria-hidden="true">
      {#each leftRuler as value (value)}<span>{rulerLabel('left', value)}</span>{/each}
    </div>

    <div class="canvas">
      {#if leftScale != null}
        {#each leftRuler as value, index (value)}
          <div
            class="gl"
            class:base={index === leftRuler.length - 1}
            style:top={`${(1 - value / leftScale) * 100}%`}
          ></div>
        {/each}
      {:else}
        <div class="gl base" style="top: 100%"></div>
      {/if}

      {#if hasData && domain}
        <svg
          class="plot-svg"
          viewBox="0 0 {W} {height}"
          preserveAspectRatio="none"
          aria-hidden="true"
          style:clip-path={revealP < 1 ? `inset(0 0 0 ${(1 - revealP) * 100}%)` : null}
        >
          {#each leftPaths as path (path.series.label)}
            {#if path.area}<path d={path.area} fill={path.series.color} fill-opacity="0.12" />{/if}
          {/each}
          {#each rightPaths as path (path.series.label)}
            {#if path.area}<path d={path.area} fill={path.series.color} fill-opacity="0.1" />{/if}
          {/each}
          {#each rightPaths as path (path.series.label)}
            <path
              d={path.stroke}
              fill="none"
              stroke={path.series.color}
              stroke-width="1.4"
              vector-effect="non-scaling-stroke"
            />
          {/each}
          {#each leftPaths as path (path.series.label)}
            <path
              d={path.stroke}
              fill="none"
              stroke={path.series.color}
              stroke-width="1.8"
              vector-effect="non-scaling-stroke"
            />
          {/each}
        </svg>
        {#if head?.headTop != null}
          <i
            class="head-dot"
            style:background={head.series.color}
            style:top={`${head.headTop}%`}
            style:opacity={revealP < 1 ? 0 : 1}
          ></i>
        {/if}
      {/if}

      {#if !hasData}
        <div class="placeholder">
          {#if loading}
            <svg class="mtn" width="40" height="40" viewBox="0 0 48 48" aria-hidden="true" data-testid="series-loader">
              <polyline points="4,38 13,24 19,30 27,12 33,22 44,7" pathLength="1" />
            </svg>
          {:else}
            <p class="empty">{emptyMessage}</p>
          {/if}
        </div>
      {/if}
    </div>

    {#if hasRight}
      <div class="yax r" aria-hidden="true">
        {#each rightRuler as value (value)}<span>{rulerLabel('right', value)}</span>{/each}
      </div>
    {/if}
  </div>

  <div class="xax" class:rpad={hasRight}>
    {#each ticks as tick (tick)}
      <span>{formatTimeTick(tick, span)}</span>
    {/each}
  </div>
</div>

<style>
  .chart-block { display: grid; gap: 0.45rem; min-width: 0; }

  .legend { display: flex; flex-wrap: wrap; gap: 0.35rem 1rem; align-items: baseline; }
  .legend-item {
    display: inline-flex; align-items: baseline; gap: 0.32rem;
    min-width: 0; font-size: 0.72rem; color: var(--text-muted); white-space: nowrap;
  }
  .dot { width: 8px; height: 8px; border-radius: 50%; flex: none; transform: translateY(-1px); }
  .name { color: var(--text-muted); }
  .last, .stats { color: var(--text-faint); font-variant-numeric: tabular-nums; }
  .last { color: var(--text-bright); }
  .sep { padding: 0 0.1rem; }

  .plot { display: flex; align-items: stretch; }
  .yax {
    width: 36px; flex: none; display: flex; flex-direction: column; justify-content: space-between;
    text-align: right; color: var(--text-faint); font: 0.62rem/1 var(--mono);
    font-variant-numeric: tabular-nums; padding: 0 6px 0 0;
  }
  .yax.r { text-align: left; padding: 0 0 0 6px; }
  .yax span { transform: translateY(-3px); }
  .yax span:last-child { transform: translateY(3px); }

  .canvas { flex: 1; position: relative; min-width: 0; }
  .gl { position: absolute; left: 0; right: 0; height: 1px; background: var(--chart-grid); }
  .gl.base { background: var(--chart-baseline); }
  .plot-svg { display: block; width: 100%; height: 100%; overflow: visible; will-change: clip-path; }
  .head-dot {
    position: absolute; right: -3px; width: 7px; height: 7px; border-radius: 50%;
    transform: translate(50%, -50%);
  }

  .placeholder { position: absolute; inset: 0; display: grid; place-items: center; }

  /* Mountain loader: one muted stroke, stopped under reduced motion. */
  .mtn polyline {
    fill: none; stroke: var(--text-muted); stroke-width: 2.6;
    stroke-linecap: round; stroke-linejoin: round;
    stroke-dasharray: 0.42 0.58; animation: mtn 1.8s linear infinite;
  }
  @keyframes mtn { from { stroke-dashoffset: 1; } to { stroke-dashoffset: 0; } }

  .empty { margin: 0; padding: 1rem; color: var(--text-faint); font-size: 0.78rem; text-align: center; }

  .xax {
    display: flex; justify-content: space-between; padding: 6px 0 0 36px;
    color: var(--text-faint); font: 0.62rem var(--mono); font-variant-numeric: tabular-nums;
  }
  .xax.rpad { padding-right: 36px; }

  @media (prefers-reduced-motion: reduce) {
    .mtn polyline { animation: none; stroke-dasharray: none; }
  }
</style>
