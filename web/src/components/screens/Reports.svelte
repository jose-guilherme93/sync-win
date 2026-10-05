<script lang="ts">
  import { computedStatus, deviceLabel, memoryPercent, type Device } from '../../lib/types'
  import { formatBytes, formatDuration, formatRelative } from '../../lib/format'
  import { generateInsights, THRESHOLDS } from '../../lib/insights'

  // A read-only fleet report. There is no reporting endpoint yet, so this
  // summarises what the current device payload already carries rather than
  // inventing history.
  export let devices: Device[] = []
  export let onSelect: (id: string) => void

  $: rows = devices.map((device) => ({
    device,
    status: computedStatus(device),
    insights: generateInsights(device.hardware),
    files: device.preference_count || 0,
    apps: device.app_count || 0,
    saves: device.saves_count || 0,
    saveBytes: device.saves_size_bytes || 0
  }))

  $: totals = {
    devices: rows.length,
    online: rows.filter((r) => r.status === 'online').length,
    files: rows.reduce((n, r) => n + r.files, 0),
    apps: rows.reduce((n, r) => n + r.apps, 0),
    saves: rows.reduce((n, r) => n + r.saves, 0),
    saveBytes: rows.reduce((n, r) => n + r.saveBytes, 0),
    alerts: rows.reduce((n, r) => n + r.insights.length, 0),
    disks: rows.flatMap((r) => (r.device.hardware?.disk_partitions ?? []).map((p) => ({ device: r.device, part: p })))
      .sort((a, b) => b.part.used_percent - a.part.used_percent)
      .slice(0, 8)
  }

  // A device only counts as up if it reported recently; uptime alone would
  // include machines that went dark hours ago.
  $: longestUp = [...rows]
    .filter((r) => r.device.hardware && r.status === 'online')
    .sort((a, b) => (b.device.hardware!.uptime_seconds || 0) - (a.device.hardware!.uptime_seconds || 0))
    .slice(0, 5)
</script>

<section class="reports">
  <div class="kpis">
    <article class="kpi"><span class="label">Devices</span><span class="value">{totals.devices}</span><span class="sub">{totals.online} online</span></article>
    <article class="kpi"><span class="label">Synced files</span><span class="value">{totals.files}</span></article>
    <article class="kpi"><span class="label">Packages</span><span class="value">{totals.apps}</span></article>
    <article class="kpi"><span class="label">Saves</span><span class="value">{totals.saves}</span><span class="sub">{formatBytes(totals.saveBytes)}</span></article>
    <article class="kpi"><span class="label">Open alerts</span><span class="value">{totals.alerts}</span></article>
  </div>

  <div class="two-col">
    <article class="card">
      <header class="card-head"><h2>Fullest disks</h2></header>
      {#if totals.disks.length === 0}
        <p class="muted pad">No partition data.</p>
      {:else}
        <ul class="list">
          {#each totals.disks as item (item.device.id + item.part.mount)}
            <li>
              <button on:click={() => onSelect(item.device.id)}>
                <span class="mono mount">{item.part.mount}</span>
                <span class="host">{deviceLabel(item.device)}</span>
                <span class="pct" class:text-warn={item.part.used_percent > THRESHOLDS.diskWarn && item.part.used_percent <= THRESHOLDS.diskCrit} class:text-crit={item.part.used_percent > THRESHOLDS.diskCrit}>
                  {item.part.used_percent.toFixed(0)}%
                </span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </article>

    <article class="card">
      <header class="card-head"><h2>Longest uptime (online)</h2></header>
      {#if longestUp.length === 0}
        <p class="muted pad">No online devices.</p>
      {:else}
        <ul class="list">
          {#each longestUp as row (row.device.id)}
            <li>
              <button on:click={() => onSelect(row.device.id)}>
                <span class="host">{deviceLabel(row.device)}</span>
                <span class="mono">{formatDuration(row.device.hardware!.uptime_seconds)}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </article>
  </div>

  <article class="card">
    <header class="card-head"><h2>Inventory</h2></header>
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Host</th><th>Status</th><th class="num">CPU</th><th class="num">RAM</th>
            <th class="num">Files</th><th class="num">Packages</th><th class="num">Saves</th><th class="num">Last seen</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as row (row.device.id)}
            <tr on:click={() => onSelect(row.device.id)} tabindex="0" on:keydown={(e) => e.key === 'Enter' && onSelect(row.device.id)}>
              <td class="host-cell">{deviceLabel(row.device)}</td>
              <td>{row.status}</td>
              <td class="num">{row.device.hardware ? `${row.device.hardware.cpu_usage_percent.toFixed(0)}%` : '—'}</td>
              <td class="num">{row.device.hardware ? `${memoryPercent(row.device.hardware).toFixed(0)}%` : '—'}</td>
              <td class="num">{row.files}</td>
              <td class="num">{row.apps}</td>
              <td class="num">{row.saves}</td>
              <td class="num" title={row.device.last_seen_at}>{formatRelative(row.device.last_seen_at)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </article>
</section>

<style>
  .reports { display: grid; gap: 0.85rem; }
  .kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 0.6rem; }
  .kpi { display: grid; gap: 0.15rem; padding: 0.75rem 0.85rem; background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); }
  .kpi .value { font-size: 1.6rem; }
  .kpi .sub { color: var(--text-faint); font-size: 0.72rem; }

  .two-col { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 0.75rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }

  .muted { color: var(--text-faint); font-size: 0.78rem; }
  .pad { padding: 1rem 0.9rem; }

  .list { list-style: none; margin: 0; padding: 0; }
  .list li { border-bottom: 1px solid var(--border); }
  .list li:last-child { border-bottom: 0; }
  .list button {
    display: flex; align-items: center; gap: 0.6rem; width: 100%;
    padding: 0.5rem 0.9rem; border: 0; background: transparent;
    color: inherit; font: inherit; font-size: 0.78rem; text-align: left; cursor: pointer;
  }
  .list button:hover { background: var(--card-hover); }
  .mount { color: var(--text); min-width: 4rem; }
  .host { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-muted); }
  .pct { font-variant-numeric: tabular-nums; font-weight: 600; }

  .table-scroll { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 0.78rem; }
  th { text-align: left; padding: 0.5rem 0.7rem; color: var(--text-faint); font-size: 10px; font-weight: 700; letter-spacing: 0.07em; text-transform: uppercase; border-bottom: 1px solid var(--border); white-space: nowrap; }
  td { padding: 0.5rem 0.7rem; border-bottom: 1px solid var(--border); color: var(--text); }
  tbody tr { cursor: pointer; }
  tbody tr:hover { background: var(--card-hover); }
  .num { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
  .host-cell { font-weight: 600; color: var(--text-bright); }
</style>
