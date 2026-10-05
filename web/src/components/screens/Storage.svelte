<script lang="ts">
  import { formatBytes, formatRate, severityFor } from '../../lib/format'
  import type { Device } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'

  export let device: Device

  $: hw = device.hardware
  $: disks = (hw?.disk_partitions ?? [])
    .filter((p) => p.total_bytes > 0)
    .sort((a, b) => b.total_bytes - a.total_bytes)

  function severityLabel(percent: number) {
    const sev = severityFor(percent)
    return sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : 'info'
  }
</script>

<section class="storage">
  {#if !hw}
    <p class="muted pad">No telemetry yet.</p>
  {:else}
    <div class="io-row">
      <article class="card io">
        <span class="label">Disk read</span>
        <span class="value">{formatRate(hw.disk_read_rate || 0)}</span>
        <span class="sub">{formatBytes(hw.disk_read_bytes || 0)} since boot</span>
      </article>
      <article class="card io">
        <span class="label">Disk write</span>
        <span class="value">{formatRate(hw.disk_write_rate || 0)}</span>
        <span class="sub">{formatBytes(hw.disk_write_bytes || 0)} since boot</span>
      </article>
      <article class="card io">
        <span class="label">Partitions</span>
        <span class="value">{disks.length}</span>
      </article>
      <article class="card io">
        <span class="label">Capacity</span>
        <span class="value">{formatBytes(disks.reduce((n, d) => n + d.total_bytes, 0))}</span>
        <span class="sub">{formatBytes(disks.reduce((n, d) => n + d.used_bytes, 0))} used</span>
      </article>
    </div>

    <article class="card">
      <header class="card-head"><h2>Partitions</h2></header>
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
        <p class="footnote">
          SMART health is not collected by the agent yet, so no drive-health row is shown rather
          than a fabricated one.
        </p>
      {/if}
    </article>
  {/if}
</section>

<style>
  .storage { display: grid; gap: 0.85rem; }

  .io-row { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 0.6rem; }

  .io { display: grid; gap: 0.15rem; padding: 0.75rem 0.85rem; }
  .io .value { font-size: 1.4rem; }
  .io .sub { color: var(--text-faint); font-size: 0.72rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); }
  .card-head { padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }

  .muted { color: var(--text-faint); font-size: 0.8rem; }
  .pad { padding: 1rem 0.9rem; }

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

  .footnote { margin: 0; padding: 0 0.9rem 0.8rem; color: var(--text-faint); font-size: 0.7rem; line-height: 1.5; }
</style>
