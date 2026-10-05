<script lang="ts">
  import { computedStatus, deviceLabel, deviceTags, memoryPercent, type Device } from '../../lib/types'
  import { formatDuration, formatRelative } from '../../lib/format'
  import { generateInsights } from '../../lib/insights'
  import StatusDot from '../ui/StatusDot.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import DeviceRow from './DeviceRow.svelte'

  export let devices: Device[] = []
  export let onSelect: (id: string) => void
  export let onAdd: () => void

  type Row = { device: Device; status: string; insights: ReturnType<typeof generateInsights> }

  // Critical devices sort first: the whole point of the fleet view is to make
  // the thing that is on fire the first thing you see.
  $: rows = devices
    .map((device) => ({ device, status: computedStatus(device), insights: generateInsights(device.hardware) }))
    .sort((a, b) => {
      const rank = (r: Row) => {
        if (r.insights.some((i) => i.type === 'crit')) return 0
        if (r.status === 'error') return 1
        if (r.status === 'offline') return 2
        if (r.status === 'stale') return 3
        if (r.insights.length > 0) return 4
        return 5
      }
      return rank(a) - rank(b) || deviceLabel(a.device).localeCompare(deviceLabel(b.device))
    })

  // Derived from the data itself rather than a hardcoded version: the newest
  // agent seen in the fleet is the baseline, so "outdated" keeps working
  // without a release having to update a constant here.
  $: newestAgent = devices
    .map((d) => d.hardware?.agent_version)
    .filter((v): v is string => Boolean(v))
    .sort(compareVersions)
    .pop() || ''

  function compareVersions(a: string, b: string) {
    const pa = a.split('.').map((n) => parseInt(n, 10) || 0)
    const pb = b.split('.').map((n) => parseInt(n, 10) || 0)
    for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
      const diff = (pa[i] || 0) - (pb[i] || 0)
      if (diff !== 0) return diff
    }
    return 0
  }

  $: summary = {
    total: devices.length,
    online: rows.filter((r) => r.status === 'online').length,
    offline: rows.filter((r) => r.status === 'offline').length,
    critical: rows.reduce((n, r) => n + r.insights.filter((i) => i.type === 'crit').length, 0),
    warnings: rows.reduce((n, r) => n + r.insights.filter((i) => i.type === 'warn').length, 0),
    disksNearFull: rows.filter((r) => (r.device.hardware?.disk_partitions ?? []).some((p) => p.used_percent > 90)).length,
    outdatedAgents: newestAgent
      ? rows.filter((r) => r.device.hardware?.agent_version && r.device.hardware.agent_version !== newestAgent).length
      : 0,
    withoutLynis: rows.filter((r) => r.device.hardware && r.device.hardware.lynis_available === false).length
  }

  // Flattened alert feed: every non-ok insight from every device, worst first.
  $: alerts = rows
    .flatMap((r) => r.insights.map((insight) => ({ device: r.device, insight })))
    .sort((a, b) => (a.insight.type === 'crit' ? -1 : 1))
    .slice(0, 8)
</script>

<section class="home">
  <div class="kpis" aria-label="Fleet summary">
    <article class="kpi">
      <span class="label">Devices</span>
      <span class="value">{summary.total}</span>
      <span class="sub"><b class="text-ok">{summary.online}</b> online · <b class="text-muted">{summary.offline}</b> offline</span>
    </article>
    <article class="kpi" class:crit={summary.critical > 0}>
      <span class="label">Critical alerts</span>
      <span class="value">{summary.critical}</span>
      <span class="sub">{summary.warnings} warning{summary.warnings !== 1 ? 's' : ''}</span>
    </article>
    <article class="kpi" class:warn={summary.disksNearFull > 0}>
      <span class="label">Disks &gt; 90%</span>
      <span class="value">{summary.disksNearFull}</span>
      <span class="sub">device{summary.disksNearFull !== 1 ? 's' : ''} affected</span>
    </article>
    <article class="kpi" class:warn={summary.outdatedAgents > 0}>
      <span class="label">Outdated agents</span>
      <span class="value">{summary.outdatedAgents}</span>
      <span class="sub">{newestAgent ? `latest ${newestAgent}` : 'no data'}</span>
    </article>
    <article class="kpi" class:warn={summary.withoutLynis > 0}>
      <span class="label">Without Lynis</span>
      <span class="value">{summary.withoutLynis}</span>
      <span class="sub">security audit missing</span>
    </article>
  </div>

  <div class="columns">
    <section class="table-card">
      <header class="card-head">
        <h2>Devices</h2>
        <button class="add" on:click={onAdd}>+ Add device</button>
      </header>

      {#if rows.length === 0}
        <div class="empty">
          <strong>No devices yet</strong>
          <p>Install the agent on a Linux machine. It appears here within seconds of the installer finishing.</p>
        </div>
      {:else}
        <div class="table-scroll">
          <table>
            <thead>
              <tr>
                <th>Status</th>
                <th>Host</th>
                <th>System</th>
                <th class="num">CPU</th>
                <th class="num">RAM</th>
                <th>Alerts</th>
                <th class="num">Uptime</th>
                <th class="num">Last seen</th>
              </tr>
            </thead>
            <tbody>
              {#each rows as row (row.device.id)}
                <tr on:click={() => onSelect(row.device.id)} tabindex="0" on:keydown={(e) => e.key === 'Enter' && onSelect(row.device.id)}>
                  <td><StatusDot status={row.status} label={false} /></td>
                  <td>
                    <div class="host">
                      <span class="hostname">{deviceLabel(row.device)}</span>
                      {#if deviceTags(row.device).length > 0}
                        <span class="tags">
                          {#each deviceTags(row.device).slice(0, 3) as tag (tag)}<span class="tag">{tag}</span>{/each}
                        </span>
                      {/if}
                    </div>
                  </td>
                  <td class="system" title={row.device.hardware?.operating_system || ''}>
                    {row.device.hardware?.operating_system || '—'}
                    {#if row.device.hardware?.kernel_version}
                      <span class="kernel">{row.device.hardware.kernel_version}</span>
                    {/if}
                  </td>
                  <td class="num">
                    {#if row.device.hardware}
                      {row.device.hardware.cpu_usage_percent.toFixed(0)}%
                      <DeviceRow deviceId={row.device.id} />
                    {:else}—{/if}
                  </td>
                  <td class="num">
                    {#if row.device.hardware}
                      {memoryPercent(row.device.hardware).toFixed(0)}%
                    {:else}—{/if}
                  </td>
                  <td>
                    {#if row.insights.length === 0}
                      <span class="ok-chip">ok</span>
                    {:else}
                      <div class="chips">
                        {#each row.insights.slice(0, 2) as insight}
                          <SeverityBadge severity={insight.type} />
                        {/each}
                        {#if row.insights.length > 2}<span class="more">+{row.insights.length - 2}</span>{/if}
                      </div>
                    {/if}
                  </td>
                  <td class="num">{row.device.hardware ? formatDuration(row.device.hardware.uptime_seconds) : '—'}</td>
                  <td class="num" title={row.device.last_seen_at}>{formatRelative(row.device.last_seen_at)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>

    <aside class="alerts-card">
      <header class="card-head">
        <h2>Recent alerts</h2>
      </header>
      {#if alerts.length === 0}
        <div class="empty small">
          <strong>All clear</strong>
          <p>No device is currently reporting a problem.</p>
        </div>
      {:else}
        <ul class="alerts">
          {#each alerts as item (item.device.id + item.insight.text)}
            <li>
              <button on:click={() => onSelect(item.device.id)}>
                <SeverityBadge severity={item.insight.type} />
                <span class="alert-text">
                  <span class="alert-host">{deviceLabel(item.device)}</span>
                  <span class="alert-detail">{item.insight.text}</span>
                </span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </aside>
  </div>
</section>

<style>
  .home {
    display: grid;
    gap: 0.85rem;
  }

  .kpis {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: 0.6rem;
  }

  .kpi {
    display: grid;
    gap: 0.15rem;
    padding: 0.75rem 0.85rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .kpi.crit { border-color: rgba(248, 113, 113, 0.4); }
  .kpi.warn { border-color: rgba(251, 191, 36, 0.35); }

  .kpi .value {
    font-size: 1.7rem;
  }

  .kpi .sub {
    color: var(--text-faint);
    font-size: 0.72rem;
  }

  .kpi .sub b { font-weight: 600; }

  .columns {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 300px;
    gap: 0.75rem;
    align-items: start;
  }

  .table-card,
  .alerts-card {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.7rem 0.9rem;
    border-bottom: 1px solid var(--border);
  }

  .card-head h2 {
    margin: 0;
    font-size: 0.82rem;
    font-weight: 600;
    color: var(--text);
  }

  .add {
    padding: 0.3rem 0.6rem;
    border: 1px solid var(--accent-border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--accent);
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
  }

  .add:hover { background: var(--accent-dim); }

  .table-scroll { overflow-x: auto; }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.78rem;
  }

  th {
    text-align: left;
    padding: 0.5rem 0.7rem;
    color: var(--text-faint);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.07em;
    text-transform: uppercase;
    white-space: nowrap;
    border-bottom: 1px solid var(--border);
  }

  td {
    padding: 0.5rem 0.7rem;
    border-bottom: 1px solid var(--border);
    color: var(--text);
    vertical-align: middle;
  }

  tbody tr {
    cursor: pointer;
    transition: background var(--t-fast);
  }

  tbody tr:hover { background: var(--card-hover); }

  .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .host { display: grid; gap: 0.2rem; }
  .hostname { font-weight: 600; color: var(--text-bright); }

  .tags { display: flex; gap: 0.2rem; flex-wrap: wrap; }

  .tag {
    padding: 0.05rem 0.35rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-pill);
    color: var(--text-faint);
    font-size: 0.6rem;
  }

  .system { color: var(--text-muted); white-space: nowrap; }
  .kernel { display: block; color: var(--text-faint); font-size: 0.66rem; font-family: var(--mono); }

  .chips { display: flex; gap: 0.2rem; align-items: center; flex-wrap: wrap; }
  .more { color: var(--text-faint); font-size: 0.66rem; }

  .ok-chip {
    color: var(--ok);
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .alerts {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 26rem;
    overflow-y: auto;
  }

  .alerts button {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    width: 100%;
    padding: 0.55rem 0.9rem;
    border: 0;
    border-bottom: 1px solid var(--border);
    background: transparent;
    text-align: left;
    cursor: pointer;
    transition: background var(--t-fast);
  }

  .alerts button:hover { background: var(--card-hover); }

  .alert-text { display: grid; gap: 0.1rem; min-width: 0; }
  .alert-host { font-size: 0.76rem; font-weight: 600; color: var(--text); }
  .alert-detail { font-size: 0.72rem; color: var(--text-muted); }

  .empty {
    padding: 1.5rem 1rem;
    text-align: center;
    color: var(--text-muted);
  }

  .empty.small { padding: 1.2rem 0.8rem; }
  .empty strong { color: var(--text); display: block; margin-bottom: 0.25rem; }
  .empty p { margin: 0; font-size: 0.78rem; }

  @media (max-width: 1100px) {
    .columns { grid-template-columns: 1fr; }
  }
</style>
