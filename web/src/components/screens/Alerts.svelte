<script lang="ts">
  import { computedStatus, deviceLabel, type Device } from '../../lib/types'
  import { formatRelative } from '../../lib/format'
  import { generateInsights, THRESHOLDS, type Insight } from '../../lib/insights'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import EmptyState from '../ui/EmptyState.svelte'
  import StatusDot from '../ui/StatusDot.svelte'

  export let devices: Device[] = []
  export let onSelect: (id: string) => void

  type Alert = { id: string; device: Device; insight: Insight; status: string }

  // Alerts are derived from current telemetry, so there is nothing to persist
  // server-side yet. Acknowledgement is therefore session-local: it hides an
  // alert for this browser without pretending the underlying condition is
  // resolved. Once /api/alerts exists this becomes the server flag.
  let acknowledged: Record<string, boolean> = {}
  let filter: 'all' | 'crit' | 'warn' = 'all'

  function alertId(deviceId: string, text: string) {
    return `${deviceId}:${text}`
  }

  $: all = devices
    .flatMap((device): Alert[] =>
      generateInsights(device.hardware).map((insight) => ({
        id: alertId(device.id, insight.text),
        device,
        insight,
        status: computedStatus(device)
      }))
    )
    .sort((a, b) => {
      if (a.insight.type !== b.insight.type) return a.insight.type === 'crit' ? -1 : 1
      return deviceLabel(a.device).localeCompare(deviceLabel(b.device))
    })

  $: visible = all.filter((a) => {
    if (acknowledged[a.id]) return false
    if (filter === 'all') return true
    return a.insight.type === filter
  })

  $: counts = {
    crit: all.filter((a) => a.insight.type === 'crit').length,
    warn: all.filter((a) => a.insight.type === 'warn').length,
    acked: Object.values(acknowledged).filter(Boolean).length
  }

  function acknowledge(id: string) {
    acknowledged = { ...acknowledged, [id]: true }
  }

  function restoreAll() {
    acknowledged = {}
  }
</script>

<section class="alerts-screen">
  <div class="kpis">
    <article class="kpi" class:crit={counts.crit > 0}>
      <span class="label">Critical</span>
      <span class="value">{counts.crit}</span>
    </article>
    <article class="kpi" class:warn={counts.warn > 0}>
      <span class="label">Warning</span>
      <span class="value">{counts.warn}</span>
    </article>
    <article class="kpi">
      <span class="label">Acknowledged</span>
      <span class="value">{counts.acked}</span>
      {#if counts.acked > 0}
        <button class="reset" on:click={restoreAll}>Restore</button>
      {/if}
    </article>
    <article class="kpi">
      <span class="label">Devices affected</span>
      <span class="value">{new Set(all.map((a) => a.device.id)).size}</span>
    </article>
  </div>

  <article class="card">
    <header class="card-head">
      <h2>Findings</h2>
      <div class="filters" role="group" aria-label="Filter alerts">
        {#each [['all', 'All'], ['crit', 'Critical'], ['warn', 'Warning']] as [key, label] (key)}
          <button class:active={filter === key} on:click={() => (filter = key as typeof filter)}>{label}</button>
        {/each}
      </div>
    </header>

    {#if visible.length === 0}
      <EmptyState
        icon="✓"
        title={all.length === 0 ? 'All clear' : 'Nothing to show'}
        message={all.length === 0
          ? 'No device is currently reporting a problem.'
          : 'Every outstanding alert has been acknowledged. Restore them to see them again.'}
      />
    {:else}
      <ul class="list">
        {#each visible as alert (alert.id)}
          <li>
            <div class="row-main">
              <SeverityBadge severity={alert.insight.type} />
              <button class="device-link" on:click={() => onSelect(alert.device.id)}>
                <StatusDot status={alert.status} label={false} />
                <span class="host">{deviceLabel(alert.device)}</span>
              </button>
              <span class="detail">{alert.insight.text}</span>
              <span class="when" title={alert.device.last_seen_at}>{formatRelative(alert.device.last_seen_at)}</span>
              <button class="ack" on:click={() => acknowledge(alert.id)}>Acknowledge</button>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </article>

  <article class="card">
    <header class="card-head"><h2>Rules</h2></header>
    <p class="rules-note">
      Thresholds are read from <code>lib/insights.ts</code>. Editing them there changes both the
      alerts and the meter colours, so the two can never disagree. Server-side rule
      configuration is not implemented yet.
    </p>
    <ul class="rules">
      <li><span>CPU</span><b>warn &gt; {THRESHOLDS.cpuWarn}%</b><b class="crit">crit &gt; {THRESHOLDS.cpuCrit}%</b></li>
      <li><span>Memory</span><b>warn &gt; {THRESHOLDS.ramWarn}%</b><b class="crit">crit &gt; {THRESHOLDS.ramCrit}%</b></li>
      <li><span>Temperature</span><b>warn &gt; {THRESHOLDS.tempWarn}°C</b><b class="crit">crit &gt; {THRESHOLDS.tempCrit}°C</b></li>
      <li><span>Disk</span><b>warn &gt; {THRESHOLDS.diskWarn}%</b><b class="crit">crit &gt; {THRESHOLDS.diskCrit}%</b></li>
      <li><span>Offline</span><b>crit &gt; 5 min silent</b></li>
    </ul>
  </article>
</section>

<style>
  .alerts-screen { display: grid; gap: 0.85rem; }

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
  .kpi .value { font-size: 1.7rem; }

  .reset {
    justify-self: start;
    margin-top: 0.2rem;
    padding: 0.2rem 0.5rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    font-size: 0.68rem;
    cursor: pointer;
  }

  .card {
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

  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }

  .filters { display: flex; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
  .filters button {
    padding: 0.25rem 0.5rem; border: 0; background: transparent;
    color: var(--text-faint); font-size: 0.68rem; font-weight: 600; cursor: pointer;
  }
  .filters button:hover { background: var(--card-hover); color: var(--text); }
  .filters button.active { background: var(--accent-dim); color: var(--accent); }

  .list { list-style: none; margin: 0; padding: 0; }
  .list li { border-bottom: 1px solid var(--border); }
  .list li:last-child { border-bottom: 0; }

  .row-main {
    display: grid;
    grid-template-columns: auto minmax(120px, 1fr) minmax(0, 2fr) auto auto;
    align-items: center;
    gap: 0.6rem;
    padding: 0.55rem 0.9rem;
    font-size: 0.78rem;
  }

  .device-link {
    display: inline-flex; align-items: center; gap: 0.4rem;
    padding: 0; border: 0; background: transparent;
    color: var(--text); font: inherit; font-weight: 600; cursor: pointer; text-align: left;
  }
  .device-link:hover .host { color: var(--accent); }
  .host { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .detail { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .when { color: var(--text-faint); font-size: 0.7rem; white-space: nowrap; }

  .ack {
    padding: 0.25rem 0.55rem; border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm); background: transparent;
    color: var(--text-muted); font-size: 0.68rem; cursor: pointer; white-space: nowrap;
  }
  .ack:hover { border-color: var(--accent-border); color: var(--accent); }

  .rules-note { margin: 0; padding: 0.75rem 0.9rem 0.25rem; color: var(--text-muted); font-size: 0.78rem; line-height: 1.5; }
  .rules-note code { font-family: var(--mono); color: var(--text); }

  .rules { list-style: none; margin: 0; padding: 0.5rem 0.9rem 0.9rem; display: grid; gap: 0.35rem; }
  .rules li { display: flex; align-items: center; gap: 0.75rem; font-size: 0.78rem; flex-wrap: wrap; }
  .rules span { min-width: 100px; color: var(--text-muted); }
  .rules b { color: var(--warn); font-weight: 600; font-variant-numeric: tabular-nums; }
  .rules b.crit { color: var(--crit); }

  @media (max-width: 800px) {
    .row-main { grid-template-columns: auto 1fr auto; }
    .detail, .when { display: none; }
  }
</style>
