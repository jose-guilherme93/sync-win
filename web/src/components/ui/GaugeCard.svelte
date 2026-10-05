<script lang="ts">
  import { clampPercent, severityFor, tempSeverity, type Severity } from '../../lib/format'

  // A semicircular gauge (270° arc) for the one headline number on a card.
  // The arc colour follows the same severity thresholds as every other meter,
  // so "amber" means the same thing here as on a horizontal bar.
  export let value: number | null = null
  export let label = ''
  export let unit = '%'
  export let min: number | null = null
  export let max: number | null = null
  export let mode: 'percent' | 'temperature' = 'percent'
  // For temperature the arc is scaled against a plausible ceiling rather than
  // 100%, because 47°C would otherwise fill less than half the dial and read as
  // "fine" even at 80°C. Both jitter scales stay inside their physical range.
  export let scaleMax = 100
  export let decimals = 0
  // A reading of 0 is meaningful for CPU but not for a missing sensor. When
  // `present` is false the gauge drops to an empty "no reading" state rather
  // than drawing an encouraging green zero.
  export let present = true

  const STROKE = 12
  const RADIUS = 54
  const SIZE = 140
  const ARC_DEGREES = 270
  // Start at the bottom-left and sweep clockwise the long way round, leaving a
  // gap at the bottom so the scale reads like a dial rather than a donut.
  const START_ROTATION = 135

  $: circumference = 2 * Math.PI * RADIUS
  $: arcLength = circumference * (ARC_DEGREES / 360)

  $: pct = mode === 'percent'
    ? clampPercent(value)
    : clampPercent((((value || 0) / scaleMax) * 100))
  $: severity = mode === 'temperature' ? tempSeverity(value) : severityFor(value)
  $: hasReading = present && value != null && Number.isFinite(value)

  // Remaining portion of the 270° arc, expressed as a dash offset.
  $: dash = hasReading ? arcLength * (pct / 100) : 0

  $: display = hasReading ? Number(value).toFixed(decimals) : '—'

  const COLOR: Record<Severity, string> = {
    ok: 'var(--accent)',
    warn: 'var(--warn)',
    crit: 'var(--crit)'
  }

  $: stroke = hasReading ? COLOR[severity] : 'var(--border-strong)'
</script>

<div class="gauge" role="img" aria-label={`${label || 'Gauge'}: ${hasReading ? `${display}${unit}` : 'no reading'}`}>
  <svg viewBox="0 0 {SIZE} {SIZE}" width={SIZE} height={SIZE}>
    <g transform="rotate({START_ROTATION} {SIZE / 2} {SIZE / 2})">
      <circle
        class="track"
        cx={SIZE / 2}
        cy={SIZE / 2}
        r={RADIUS}
        fill="none"
        stroke-width={STROKE}
        stroke-linecap="round"
        stroke-dasharray="{arcLength} {circumference}"
      />
      <circle
        class="fill"
        cx={SIZE / 2}
        cy={SIZE / 2}
        r={RADIUS}
        fill="none"
        stroke-width={STROKE}
        stroke-linecap="round"
        stroke={stroke}
        stroke-dasharray="{dash} {circumference}"
      />
    </g>
  </svg>

  <div class="readout">
    <span class="number" class:text-ok={severity === 'ok' && hasReading} class:text-warn={severity === 'warn'} class:text-crit={severity === 'crit'}>
      {display}<small>{hasReading ? unit : ''}</small>
    </span>
    {#if label}<span class="label">{label}</span>{/if}
  </div>

  {#if min != null || max != null}
    <div class="range">
      <span>↓ {min != null ? min.toFixed(decimals) : '—'}</span>
      <span>↑ {max != null ? max.toFixed(decimals) : '—'}</span>
    </div>
  {/if}
</div>

<style>
  .gauge {
    position: relative;
    display: grid;
    justify-items: center;
    gap: 0.35rem;
  }

  svg {
    display: block;
  }

  .track {
    stroke: var(--border-strong);
  }

  .fill {
    transition: stroke-dasharray var(--t), stroke var(--t);
  }

  .readout {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 140px;
    display: grid;
    align-content: center;
    justify-items: center;
    gap: 0.15rem;
    pointer-events: none;
  }

  .number {
    color: var(--text-bright);
    font-size: 2rem;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    line-height: 1;
  }

  .number small {
    font-size: 1rem;
    color: var(--text-muted);
    margin-left: 0.1rem;
  }

  .readout .label {
    margin-top: 0.15rem;
  }

  .range {
    display: flex;
    justify-content: space-between;
    gap: 1.5rem;
    color: var(--text-muted);
    font-size: 0.7rem;
    font-variant-numeric: tabular-nums;
  }
</style>
