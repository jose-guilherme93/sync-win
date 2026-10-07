<script lang="ts">
  import { deviceHistoryStore, type ChartPoint } from '../../lib/telemetry-store'
  import MiniSparkline from '../ui/MiniSparkline.svelte'

  // One metric's sparkline for one fleet row. Split into its own component
  // because each row needs its own subscription to the telemetry store, and
  // doing that inline in a table would mean N subscriptions managed by the
  // parent.
  //
  // `metric` is explicit so the CPU trend and the memory trend can live in
  // their own table columns under their own headers. Previously both series
  // were rendered side by side inside the CPU cell with no label, which read as
  // two anonymous graphs.
  export let deviceId: string
  export let metric: 'cpu' | 'memory' = 'cpu'

  let points: ChartPoint[] = []
  const unsub = deviceHistoryStore(deviceId).subscribe((value) => (points = value))

  $: series = points.map((p) => (metric === 'memory' ? p.memory : p.cpu))
  $: color = metric === 'memory' ? 'var(--series-1)' : 'var(--accent)'
  $: caption = metric === 'memory' ? 'RAM trend' : 'CPU trend'
</script>

<div class="spark-cell">
  <MiniSparkline
    {series}
    {color}
    height={22}
    label={`${caption} over the last ${series.length} sample${series.length === 1 ? '' : 's'}`}
  />
  <span class="spark-caption">{caption}</span>
</div>

<style>
  .spark-cell {
    width: 84px;
    display: grid;
    gap: 0.1rem;
  }

  .spark-caption {
    color: var(--text-faint);
    font-size: 0.6rem;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    white-space: nowrap;
  }
</style>
