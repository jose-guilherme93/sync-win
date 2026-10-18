<script lang="ts">
  import { formatBytes, severityFor, clampPercent } from '../../lib/format'
  import type { Device } from '../../lib/types'
  import { memoryPercent, swapPercent } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import MiniSparkline from '../ui/MiniSparkline.svelte'

  export let device: Device

  const MAX_SAMPLES = 60
  let memSeries: number[] = []
  let lastStamp = ''
  let seriesDeviceId = ''

  // The shell reuses this component when another device is selected. Without
  // this reset the sparkline would mix samples from two devices.
  function resetSeries(id: string) {
    if (!id || id === seriesDeviceId) return
    seriesDeviceId = id
    memSeries = []
    lastStamp = ''
  }
  $: resetSeries(device.id)

  $: hw = device.hardware
  $: total = hw?.memory_total_bytes ?? 0
  $: used = hw?.memory_used_bytes ?? 0
  $: buffers = hw?.memory_buffers_bytes ?? 0
  $: cached = hw?.memory_cached_bytes ?? 0
  // The agent defines used = total - free - buffers - cached, so free is the
  // remainder and "reclaimable" is everything the kernel can hand back without
  // swapping. This is the breakdown a memory-pressure read actually needs.
  $: free = Math.max(0, total - used - buffers - cached)
  $: reclaimable = buffers + cached
  $: usedPct = memoryPercent(hw)
  $: swapUsed = hw?.swap_used_bytes ?? 0
  $: swapTotal = hw?.swap_total_bytes ?? 0
  $: swapPct = swapPercent(hw)
  $: procs = (hw?.top_mem_processes ?? []).slice(0, 10)
  $: procsRss = procs.reduce((n, p) => n + (p.mem_rss_bytes || 0), 0)

  $: if (hw?.collected_at && hw.collected_at !== lastStamp) {
    lastStamp = hw.collected_at
    memSeries = push(memSeries, memoryPercent(hw))
  }

  function push(series: number[], value: number): number[] {
    const out = [...series, value]
    return out.length > MAX_SAMPLES ? out.slice(out.length - MAX_SAMPLES) : out
  }

  function pct(part: number): number {
    return total > 0 ? (part / total) * 100 : 0
  }

  function sevLabel(sev: string): 'info' | 'warn' | 'crit' {
    return sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : 'info'
  }
</script>

<section class="mem">
  {#if !hw}
    <EmptyState icon="🧠" title="No telemetry yet" message="The agent has not reported memory metrics for this device." />
  {:else}
    <div class="summary">
      <article class="stat">
        <span class="label">Memory used</span>
        <span class="value" class:text-warn={severityFor(usedPct) === 'warn'} class:text-crit={severityFor(usedPct) === 'crit'}>
          {usedPct.toFixed(0)}<small>%</small>
        </span>
        <span class="sub">{formatBytes(used)} of {formatBytes(total)}</span>
      </article>
      <article class="stat">
        <span class="label">Reclaimable</span>
        <span class="value">{formatBytes(reclaimable)}</span>
        <span class="sub">{formatBytes(buffers)} buffers · {formatBytes(cached)} cached</span>
      </article>
      <article class="stat">
        <span class="label">Free</span>
        <span class="value">{formatBytes(free)}</span>
        <span class="sub">{pct(free).toFixed(0)}% of RAM untouched</span>
      </article>
      <article class="stat">
        <span class="label">Swap</span>
        <span class="value" class:text-warn={severityFor(swapPct) === 'warn'} class:text-crit={severityFor(swapPct) === 'crit'}>
          {swapTotal > 0 ? `${swapPct.toFixed(0)}%` : '—'}
        </span>
        <span class="sub">{swapTotal > 0 ? `${formatBytes(swapUsed)} of ${formatBytes(swapTotal)}` : 'no swap configured'}</span>
      </article>
    </div>

    <article class="card">
      <header class="card-head">
        <h2>Memory breakdown</h2>
        <span class="hint">{formatBytes(total)} total</span>
      </header>
      <div class="stack-wrap">
        <div class="stack" role="img" aria-label="Memory composition">
          <i class="seg used" style="width: {pct(used)}%"></i>
          <i class="seg buffers" style="width: {pct(buffers)}%"></i>
          <i class="seg cached" style="width: {pct(cached)}%"></i>
          <i class="seg free" style="width: {pct(free)}%"></i>
        </div>
        <div class="legend">
          <span><i class="dot used"></i>Used {formatBytes(used)} ({pct(used).toFixed(0)}%)</span>
          <span><i class="dot buffers"></i>Buffers {formatBytes(buffers)} ({pct(buffers).toFixed(1)}%)</span>
          <span><i class="dot cached"></i>Cached {formatBytes(cached)} ({pct(cached).toFixed(1)}%)</span>
          <span><i class="dot free"></i>Free {formatBytes(free)} ({pct(free).toFixed(0)}%)</span>
        </div>
      </div>
      <dl class="kv">
        <div><dt>Total</dt><dd>{formatBytes(total)}</dd></div>
        <div><dt>Used</dt><dd>{formatBytes(used)}</dd></div>
        <div><dt>Buffers</dt><dd>{formatBytes(buffers)}</dd></div>
        <div><dt>Cached</dt><dd>{formatBytes(cached)}</dd></div>
        <div><dt>Free</dt><dd>{formatBytes(free)}</dd></div>
        <div><dt>Reclaimable (buffers + cache)</dt><dd>{formatBytes(reclaimable)}</dd></div>
        <div><dt>Swap used</dt><dd>{swapTotal > 0 ? `${formatBytes(swapUsed)} of ${formatBytes(swapTotal)}` : '—'}</dd></div>
      </dl>
      {#if swapTotal > 0 && swapPct >= 25}
        <p class="warn-note">
          <SeverityBadge severity={sevLabel(severityFor(swapPct))} />
          Swap in active use ({swapPct.toFixed(0)}%) — resident pressure, not just a cold cache.
        </p>
      {/if}
    </article>

    <article class="card">
      <header class="card-head">
        <h2>Top processes by memory</h2>
        <span class="hint">{procs.length} shown · {formatBytes(procsRss)} RSS combined</span>
      </header>
      {#if procs.length === 0}
        <EmptyState icon="📋" title="No process snapshot" message="The agent did not report top memory processes." />
      {:else}
        <table class="procs">
          <thead><tr><th>PID</th><th>Process</th><th class="num">RSS</th><th class="num">Share</th></tr></thead>
          <tbody>
            {#each procs as p (p.pid)}
              <tr>
                <td class="mono dim">{p.pid}</td>
                <td class="mono">{p.name}</td>
                <td class="num">{formatBytes(p.mem_rss_bytes)}</td>
                <td class="num dim">{total > 0 ? `${((p.mem_rss_bytes / total) * 100).toFixed(1)}%` : '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </article>

    <article class="card agent">
      <header class="card-head"><h2>Agent overhead</h2></header>
      <div class="agent-row">
        <span>sync-win-agent</span>
        <span class="mono">{formatBytes(hw.agent_memory_bytes ?? 0)} RSS · {(hw.agent_cpu_usage ?? 0).toFixed(2)}% CPU</span>
      </div>
      <div class="spark"><MiniSparkline series={memSeries} color="var(--series-4)" height={30} label="Memory used history" /></div>
    </article>

    <p class="footnote">
      Slab, dirty/writeback, per-NUMA nodes, huge pages and OOM-kill counts are not collected by the
      agent yet, so those rows are omitted rather than fabricated.
    </p>
  {/if}
</section>

<style>
  .mem { display: grid; gap: 0.85rem; }

  .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 0.6rem; }
  .stat {
    display: grid; gap: 0.25rem; align-content: start;
    padding: 0.8rem 0.9rem; background: var(--card);
    border: 1px solid var(--border); border-radius: var(--radius);
  }
  .stat .label { color: var(--text-faint); font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.04em; }
  .stat .value { font-size: 1.45rem; font-weight: 600; color: var(--text-bright); font-variant-numeric: tabular-nums; }
  .stat .value small { font-size: 0.8rem; color: var(--text-muted); font-weight: 400; }
  .stat .sub { color: var(--text-faint); font-size: 0.72rem; }
  .text-warn { color: var(--warn); }
  .text-crit { color: var(--crit); }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .hint { color: var(--text-faint); font-size: 0.7rem; }

  .stack-wrap { display: grid; gap: 0.6rem; padding: 0.9rem; }
  .stack { display: flex; height: 22px; border-radius: var(--radius-sm); overflow: hidden; background: rgba(148, 163, 184, 0.12); }
  .seg { display: block; height: 100%; transition: width var(--t); }
  .seg.used { background: var(--accent); }
  .seg.buffers { background: var(--series-2); }
  .seg.cached { background: var(--series-3); }
  .seg.free { background: rgba(148, 163, 184, 0.28); }
  .legend { display: flex; flex-wrap: wrap; gap: 0.4rem 1rem; color: var(--text-muted); font-size: 0.72rem; }
  .legend span { display: inline-flex; align-items: center; gap: 0.35rem; }
  .dot { width: 9px; height: 9px; border-radius: 2px; display: inline-block; }
  .dot.used { background: var(--accent); }
  .dot.buffers { background: var(--series-2); }
  .dot.cached { background: var(--series-3); }
  .dot.free { background: rgba(148, 163, 184, 0.28); }

  .kv { display: grid; gap: 0.4rem; margin: 0; padding: 0 0.9rem 0.9rem; }
  .kv div { display: flex; justify-content: space-between; gap: 0.6rem; align-items: baseline; border-bottom: 1px solid var(--border); padding-bottom: 0.4rem; }
  .kv div:last-child { border-bottom: none; }
  .kv dt { color: var(--text-faint); font-size: 0.74rem; }
  .kv dd { margin: 0; color: var(--text); font-size: 0.78rem; font-variant-numeric: tabular-nums; }

  .warn-note { display: flex; align-items: center; gap: 0.4rem; margin: 0; padding: 0 0.9rem 0.9rem; color: var(--warn); font-size: 0.72rem; }

  .procs { width: 100%; border-collapse: collapse; }
  .procs th { text-align: left; padding: 0.5rem 0.9rem; color: var(--text-faint); font-size: 0.68rem; font-weight: 500; text-transform: uppercase; letter-spacing: 0.04em; border-bottom: 1px solid var(--border); }
  .procs td { padding: 0.42rem 0.9rem; border-bottom: 1px solid var(--border); color: var(--text); font-size: 0.78rem; }
  .procs tr:last-child td { border-bottom: none; }
  .procs .num { text-align: right; font-variant-numeric: tabular-nums; }
  .dim { color: var(--text-muted); }

  .agent-row { display: flex; justify-content: space-between; gap: 0.6rem; padding: 0.8rem 0.9rem 0.4rem; color: var(--text-muted); font-size: 0.78rem; }
  .spark { padding: 0 0.9rem 0.9rem; }

  .mono { font-family: var(--mono); }
  .footnote { margin: 0; color: var(--text-faint); font-size: 0.7rem; line-height: 1.5; }
</style>
