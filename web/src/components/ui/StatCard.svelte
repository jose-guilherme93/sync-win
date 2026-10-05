<script lang="ts">
  import MiniSparkline from './MiniSparkline.svelte'

  // The workhorse card: a label, a big value, an optional min/max line and an
  // optional sparkline. Every telemetry card on the overview is one of these,
  // which is what makes the grid scannable.
  export let label = ''
  export let value: string | number | null = null
  export let unit = ''
  export let sub = ''
  // Period min/max, rendered as the ↓/↑ line under the value.
  export let min: number | null = null
  export let max: number | null = null
  export let decimals = 0
  export let tone: 'default' | 'ok' | 'warn' | 'crit' = 'default'
  // A series to draw as a sparkline. Kept as plain numbers so the card works
  // with any metric, not just the ones the shared device store knows about.
  export let series: number[] = []
  export let seriesColor = 'var(--accent)'
  // When the metric is unavailable (no sensor, no data yet), pass present=false
  // so the card shows an em dash instead of a misleading zero.
  export let present = true

  $: hasValue = present && value != null && value !== ''
  $: shown = hasValue ? value : '—'
  $: hasRange = min != null || max != null
  // A single point cannot draw a line, so skip the sparkline rather than show a
  // flat dot that looks like a stuck metric.
  $: canPlot = series.length >= 2
</script>

<div class="stat-card {tone}">
  <span class="label">{label}</span>

  <div class="value-row">
    <span class="value">{shown}{#if hasValue && unit}<small>{unit}</small>{/if}</span>
    {#if hasRange}
      <span class="range">
        {#if min != null}<span title="Minimum in period">↓ {min.toFixed(decimals)}</span>{/if}
        {#if max != null}<span title="Maximum in period">↑ {max.toFixed(decimals)}</span>{/if}
      </span>
    {/if}
  </div>

  {#if sub}<span class="sub">{sub}</span>{/if}

  {#if series.length > 0}
    <div class="spark" style="--spark: {seriesColor}">
      {#if canPlot}
        <MiniSparkline {series} color={seriesColor} height={28} />
      {:else}
        <div class="spark-empty">collecting...</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .stat-card {
    display: grid;
    gap: 0.3rem;
    align-content: start;
    padding: 0.8rem 0.9rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    min-width: 0;
  }

  .value-row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .value {
    color: var(--text-bright);
    font-size: 1.5rem;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    line-height: 1.1;
  }

  .value small {
    font-size: 0.85rem;
    color: var(--text-muted);
    margin-left: 0.15rem;
    font-weight: 400;
  }

  .range {
    display: flex;
    gap: 0.5rem;
    color: var(--text-muted);
    font-size: 0.7rem;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .sub {
    color: var(--text-muted);
    font-size: 0.74rem;
    font-variant-numeric: tabular-nums;
  }

  .spark {
    margin-top: 0.1rem;
    min-height: 28px;
  }

  .spark-empty {
    height: 28px;
    display: grid;
    place-items: center;
    color: var(--text-faint);
    font-size: 0.68rem;
  }

  .ok .value { color: var(--ok); }
  .warn .value { color: var(--warn); }
  .crit .value { color: var(--crit); }
</style>
