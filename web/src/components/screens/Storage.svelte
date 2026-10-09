<script lang="ts">
  import { formatBytes, formatRate, severityFor } from '../../lib/format'
  import type { Device } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import MiniSparkline from '../ui/MiniSparkline.svelte'

  export let device: Device

  const MAX_SAMPLES = 60
  let readSeries: number[] = []
  let writeSeries: number[] = []
  let lastStamp = ''

  $: hw = device.hardware
  $: disks = (hw?.disk_partitions ?? [])
    .filter((p) => p.total_bytes > 0)
    .sort((a, b) => b.total_bytes - a.total_bytes)

  $: totalBytes = disks.reduce((n, d) => n + d.total_bytes, 0)
  $: usedBytes = disks.reduce((n, d) => n + d.used_bytes, 0)
  $: freeBytes = disks.reduce((n, d) => n + d.free_bytes, 0)
  $: usedPercent = totalBytes > 0 ? (usedBytes / totalBytes) * 100 : 0
  $: critCount = disks.filter((d) => severityFor(d.used_percent) === 'crit').length
  $: warnCount = disks.filter((d) => severityFor(d.used_percent) === 'warn').length

  $: if (hw?.collected_at && hw.collected_at !== lastStamp) {
    lastStamp = hw.collected_at
    readSeries = push(readSeries, hw.disk_read_rate || 0)
    writeSeries = push(writeSeries, hw.disk_write_rate || 0)
  }

  function push(series: number[], value: number): number[] {
    const out = [...series, value]
    return out.length > MAX_SAMPLES ? out.slice(out.length - MAX_SAMPLES) : out
  }

  function severityLabel(percent: number): 'crit' | 'warn' | 'info' {
    const sev = severityFor(percent)
    return sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : 'info'
  }
</script>

<section class="storage">
  {#if !hw}
    <EmptyState icon="💾" title="No telemetry yet" message="The agent has not reported disk metrics for this device." />
  {:else}
    <div class="summary">
      <article class="stat">
        <span class="label">Disk read</span>
        <span class="value">{formatRate(hw.disk_read_rate || 0)}</span>
        <MiniSparkline series={readSeries} color="var(--accent)" height={26} label="Disk read rate" />
      </article>
      <article class="stat">
        <span class="label">Disk write</span>
        <span class="value">{formatRate(hw.disk_write_rate || 0)}</span>
        <MiniSparkline series={writeSeries} color="var(--series-2)" height={26} label="Disk write rate" />
      </article>
      <article class="stat">
        <span class="label">Capacity</span>
        <span class="value">{formatBytes(totalBytes)}</span>
        <span class="sub">{formatBytes(usedBytes)} used · {formatBytes(freeBytes)} free</span>
      </article>
      <article class="stat">
        <span class="label">Usage</span>
        <span class="value" class:text-warn={warnCount > 0} class:text-crit={critCount > 0}>{usedPercent.toFixed(0)}<small>%</small></span>
        <span class="sub">{disks.length} filesystems{#if critCount > 0} · {critCount} critical{:else if warnCount > 0} · {warnCount} near full{/if}</span>
      </article>
    </div>

    <article class="card">
      <header class="card-head">
        <h2>Filesystems</h2>
        <span class="hint">sorted by size · {formatBytes(usedBytes)} of {formatBytes(totalBytes)} used</span>
      </header>
      {#if disks.length === 0}
        <EmptyState icon="💾" title="No partitions reported" message="The agent did not report any filesystem with a non-zero size." />
      {:else}
        <div class="parts">
          {#each disks as disk (disk.mount)}
            {@const sev = severityFor(disk.used_percent)}
            <div class="part">
              <div class="part-head">
                <div class="part-id">
                  <span class="mount mono">{disk.mount}</span>
                  {#if disk.device}<span class="dev mono" title={disk.device}>{disk.device}</span>{/if}
                </div>
                <div class="part-right">
                  {#if sev !== 'ok'}<SeverityBadge severity={severityLabel(disk.used_percent)} />{/if}
                  <span class="pct" class:text-warn={sev === 'warn'} class:text-crit={sev === 'crit'}>
                    {disk.used_percent.toFixed(0)}%
                  </span>
                </div>
              </div>
              <div class="bar"><i class={sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : ''} style="width: {Math.min(disk.used_percent, 100)}%"></i></div>
              <div class="part-sub">
                <span>{formatBytes(disk.used_bytes)} of {formatBytes(disk.total_bytes)}</span>
                <span>{formatBytes(disk.free_bytes)} free</span>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </article>

    <p class="footnote">
      SMART drive health, inode usage and per-disk I/O are not collected by the agent yet, so those
      rows are omitted rather than fabricated.
    </p>
  {/if}
</section>

<style>
  .storage { display: grid; gap: 0.85rem; }

  .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 0.6rem; }
  .stat {
    display: grid; gap: 0.25rem; align-content: start;
    padding: 0.8rem 0.9rem; background: var(--card);
    border: 1px solid var(--border); border-radius: var(--radius);
  }
  .stat .value { font-size: 1.45rem; }
  .stat .value small { font-size: 0.8rem; color: var(--text-muted); font-weight: 400; }
  .stat .sub { color: var(--text-faint); font-size: 0.72rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .hint { color: var(--text-faint); font-size: 0.7rem; }

  .parts { display: grid; gap: 0.9rem; padding: 0.9rem; }
  .part { display: grid; gap: 0.3rem; }
  .part-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; }
  .part-id { display: flex; align-items: baseline; gap: 0.5rem; min-width: 0; }
  .mount { color: var(--text-bright); font-size: 0.85rem; }
  .dev { color: var(--text-faint); font-size: 0.7rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .part-right { display: flex; align-items: center; gap: 0.5rem; }
  .pct { font-size: 0.95rem; font-weight: 600; font-variant-numeric: tabular-nums; color: var(--text-bright); }

  .bar { height: 9px; border-radius: var(--radius-pill); background: rgba(148, 163, 184, 0.15); overflow: hidden; }
  .bar i { display: block; height: 100%; background: var(--accent); border-radius: inherit; transition: width var(--t); }
  .bar i.warn { background: var(--warn); }
  .bar i.crit { background: var(--crit); }

  .part-sub { display: flex; justify-content: space-between; gap: 0.5rem; color: var(--text-faint); font-size: 0.7rem; font-variant-numeric: tabular-nums; }

  .footnote { margin: 0; color: var(--text-faint); font-size: 0.7rem; line-height: 1.5; }
</style>
