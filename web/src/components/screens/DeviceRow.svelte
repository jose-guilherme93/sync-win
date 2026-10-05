<script lang="ts">
  import { deviceHistoryStore, type ChartPoint } from '../../lib/telemetry-store'
  import MiniSparkline from '../ui/MiniSparkline.svelte'

  // One row's sparklines. Split into its own component because each row needs
  // its own subscription to the telemetry store, and doing that inline in a
  // table would mean N subscriptions managed by the parent.
  export let deviceId: string

  let points: ChartPoint[] = []
  const unsub = deviceHistoryStore(deviceId).subscribe((value) => (points = value))

  $: cpuSeries = points.map((p) => p.cpu)
  $: memSeries = points.map((p) => p.memory)
</script>

<div class="spark-cell">
  <MiniSparkline series={cpuSeries} color="var(--accent)" height={22} />
</div>
<div class="spark-cell">
  <MiniSparkline series={memSeries} color="var(--series-1)" height={22} />
</div>

<style>
  .spark-cell {
    width: 72px;
    min-height: 22px;
  }
</style>
