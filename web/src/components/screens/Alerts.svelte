<script lang="ts">
  import { onMount } from 'svelte'
  import { computedStatus, deviceLabel, type Device } from '../../lib/types'
  import { apiFetch, apiURL } from '../../lib/api'
  import { formatRelative } from '../../lib/format'
  import { generateInsights, THRESHOLDS, type Insight } from '../../lib/insights'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import EmptyState from '../ui/EmptyState.svelte'
  import StatusDot from '../ui/StatusDot.svelte'

  export let devices: Device[] = []
  export let authHeaders: Record<string, string> = {}
  export let onSelect: (id: string) => void

  type Alert = { id: string; device: Device; insight: Insight; status: string }
  type AlertAck = { id: string; acked_at: string }

  // The findings are derived from current telemetry, so only the
  // acknowledgement is persisted. The server stores it per owner, which is what
  // makes it survive a reload and follow the account to another browser, while
  // the rules stay in lib/insights.ts so alerts and meters cannot disagree.
  let acknowledged: Record<string, string> = {}
  let ackError = ''
  // True once the operator has changed something here. The initial GET must
  // never overwrite an ack that was clicked while it was still in flight.
  let acksDirty = false
  let filter: 'all' | 'crit' | 'warn' = 'all'

  // The id is the rule, not the reading: "CPU 92%" must stay acknowledged when
  // the next sample reads 95%. insight.key is stable per rule and subject.
  function alertId(deviceId: string, key: string) {
    return `${deviceId}:${key}`
  }

  $: all = devices
    .flatMap((device): Alert[] =>
      generateInsights(device.hardware).map((insight) => ({
        id: alertId(device.id, insight.key),
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
    // Every stored ack counts, including one whose condition has already
    // cleared: the operator needs a way to restore those too.
    acked: Object.keys(acknowledged).length
  }

  onMount(loadAcks)

  function describe(e: unknown): string {
    return e instanceof Error ? e.message : 'request failed'
  }

  async function loadAcks() {
    ackError = ''
    try {
      const res = await apiFetch(apiURL('/api/alerts'), { headers: authHeaders })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
      const data: { acks?: AlertAck[] } = await res.json()
      if (acksDirty) return
      const next: Record<string, string> = {}
      for (const ack of data.acks ?? []) next[ack.id] = ack.acked_at
      acknowledged = next
    } catch (e) {
      ackError = `Could not load acknowledgements: ${describe(e)}`
    }
  }

  async function acknowledge(id: string) {
    if (acknowledged[id]) return
    acksDirty = true
    const previous = acknowledged
    // Optimistic: the row disappears on click, and comes back if the save
    // fails. A persisted ack is the point of this screen, so a silent failure
    // would leave the operator believing it was stored.
    acknowledged = { ...acknowledged, [id]: new Date().toISOString() }
    ackError = ''
    try {
      const res = await apiFetch(apiURL('/api/alerts'), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ id })
      })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
      const ack: AlertAck = await res.json()
      acknowledged = { ...acknowledged, [ack.id]: ack.acked_at }
    } catch (e) {
      acknowledged = previous
      ackError = `Could not save the acknowledgement: ${describe(e)}`
    }
  }

  async function restoreAll() {
    acksDirty = true
    const previous = acknowledged
    acknowledged = {}
    ackError = ''
    try {
      const res = await apiFetch(apiURL('/api/alerts'), { method: 'DELETE', headers: authHeaders })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
    } catch (e) {
      acknowledged = previous
      ackError = `Could not restore the acknowledgements: ${describe(e)}`
    }
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

    {#if ackError}
      <p class="ack-error" role="alert">{ackError}</p>
    {/if}

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
      alerts and the meter colours, so the two can never disagree. Acknowledgements are stored
      on the server per account, so they survive a reload and follow you to another browser.
    </p>
    <ul class="rules">
      <li><span>CPU</span><b>warn &gt; {THRESHOLDS.cpuWarn}%</b><b class="crit">crit &gt; {THRESHOLDS.cpuCrit}%</b></li>
      <li><span>Memory</span><b>warn &gt; {THRESHOLDS.ramWarn}%</b><b class="crit">crit &gt; {THRESHOLDS.ramCrit}%</b></li>
      <li><span>Temperature</span><b>warn &gt; {THRESHOLDS.tempWarn}°C</b><b class="crit">crit &gt; {THRESHOLDS.tempCrit}°C</b></li>
      <li><span>Disk</span><b>warn &gt; {THRESHOLDS.diskWarn}%</b><b class="crit">crit &gt; {THRESHOLDS.diskCrit}%</b></li>
    </ul>
    <p class="rules-note">
      Device-state findings (offline, stale, sync error, duplicate identity, missing Lynis, outdated agent)
      live on the <b>Findings</b> screen.
    </p>
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

  .ack-error {
    margin: 0;
    padding: 0.55rem 0.9rem;
    border-bottom: 1px solid var(--border);
    color: var(--crit);
    font-size: 0.75rem;
  }

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
