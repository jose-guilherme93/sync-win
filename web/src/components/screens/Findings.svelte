<script lang="ts">
  import { computedStatus, deviceLabel, type Device } from '../../lib/types'
  import { formatRelative } from '../../lib/format'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import EmptyState from '../ui/EmptyState.svelte'
  import StatusDot from '../ui/StatusDot.svelte'

  export let devices: Device[] = []
  export let onSelect: (id: string) => void

  type Severity = 'info' | 'warn' | 'crit'
  type Finding = {
    id: string
    device: Device
    status: string
    severity: Severity
    label: string
    detail: string
  }

  // Findings are the device-level problems that resource thresholds cannot
  // express: a device that stopped reporting, an agent that reported an error,
  // a shared hardware identity, a missing hardening tool, or an agent that is
  // behind the rest of the fleet. Alerts owns the CPU/RAM/disk/temp breaches.
  function compareVersions(a: string, b: string) {
    const pa = a.split('.').map((n) => parseInt(n, 10) || 0)
    const pb = b.split('.').map((n) => parseInt(n, 10) || 0)
    for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
      const diff = (pa[i] || 0) - (pb[i] || 0)
      if (diff !== 0) return diff
    }
    return 0
  }

  // Derived from the fleet rather than a hardcoded constant, so "outdated" keeps
  // meaning "behind the newest agent anyone here is running". Same rule as the
  // Home summary, kept in one place per screen rather than a server flag.
  $: newestAgent = devices
    .map((d) => d.hardware?.agent_version)
    .filter((v): v is string => Boolean(v))
    .sort(compareVersions)
    .pop() || ''

  $: findings = devices
    .flatMap((device): Finding[] => {
      const status = computedStatus(device)
      const hw = device.hardware
      const out: Finding[] = []
      const push = (severity: Severity, label: string, detail: string) => {
        out.push({ id: `${device.id}:${label}`, device, status, severity, label, detail })
      }

      if (status === 'error') push('crit', 'Sync error', device.last_error || 'The agent reported a sync failure.')
      if (status === 'offline') push('warn', 'Offline', `No report for ${formatRelative(device.last_seen_at)}.`)
      if (status === 'stale') push('warn', 'Stale', `Last report ${formatRelative(device.last_seen_at)}.`)
      if (status === 'duplicate') push('warn', 'Duplicate identity', device.last_error || 'Shares a hardware fingerprint with another device.')
      if (!hw) push('info', 'No telemetry', 'The device has not reported hardware telemetry yet.')
      if (hw && hw.lynis_available === false) push('warn', 'Lynis not installed', 'Security audits are unavailable until Lynis is installed.')
      if (hw?.agent_version && newestAgent && compareVersions(hw.agent_version, newestAgent) < 0) {
        push('warn', 'Outdated agent', `${hw.agent_version} is behind ${newestAgent}.`)
      }
      return out
    })
    .sort((a, b) => {
      const rank: Record<Severity, number> = { crit: 0, warn: 1, info: 2 }
      return rank[a.severity] - rank[b.severity] || deviceLabel(a.device).localeCompare(deviceLabel(b.device)) || a.label.localeCompare(b.label)
    })

  $: counts = {
    crit: findings.filter((f) => f.severity === 'crit').length,
    warn: findings.filter((f) => f.severity === 'warn').length,
    info: findings.filter((f) => f.severity === 'info').length,
    devices: new Set(findings.map((f) => f.device.id)).size
  }
</script>

<section class="findings-screen">
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
      <span class="label">Informational</span>
      <span class="value">{counts.info}</span>
    </article>
    <article class="kpi">
      <span class="label">Devices affected</span>
      <span class="value">{counts.devices}</span>
    </article>
  </div>

  <article class="card">
    <header class="card-head"><h2>Device findings</h2></header>
    {#if findings.length === 0}
      <EmptyState
        icon="✓"
        title="No findings"
        message={devices.length === 0
          ? 'No devices are reporting yet.'
          : 'Every device is online, error-free, and running the newest agent.'}
      />
    {:else}
      <ul class="list">
        {#each findings as finding (finding.id)}
          <li>
            <div class="row-main">
              <SeverityBadge severity={finding.severity} />
              <button class="device-link" on:click={() => onSelect(finding.device.id)}>
                <StatusDot status={finding.status} label={false} />
                <span class="host">{deviceLabel(finding.device)}</span>
              </button>
              <span class="label-text">{finding.label}</span>
              <span class="detail">{finding.detail}</span>
              <span class="when" title={finding.device.last_seen_at}>{formatRelative(finding.device.last_seen_at)}</span>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </article>

  <article class="card">
    <header class="card-head"><h2>What is checked here</h2></header>
    <p class="rules-note">
      Findings cover device state and configuration. Resource thresholds (CPU, memory, temperature, disk)
      live on the <b>Alerts</b> screen; the two never show the same item.
    </p>
    <ul class="rules">
      <li><span>Sync error</span><b class="crit">critical</b></li>
      <li><span>Offline / stale</span><b>warning</b></li>
      <li><span>Duplicate identity</span><b>warning</b></li>
      <li><span>Lynis not installed</span><b>warning</b></li>
      <li><span>Outdated agent</span><b>warning</b></li>
      <li><span>No telemetry</span><b class="info">info</b></li>
    </ul>
  </article>
</section>

<style>
  .findings-screen { display: grid; gap: 0.85rem; }

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

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }

  .list { list-style: none; margin: 0; padding: 0; }
  .list li { border-bottom: 1px solid var(--border); }
  .list li:last-child { border-bottom: 0; }

  .row-main {
    display: grid;
    grid-template-columns: auto minmax(120px, 1fr) auto minmax(0, 2fr) auto;
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

  .label-text { color: var(--text); white-space: nowrap; }
  .detail { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .when { color: var(--text-faint); font-size: 0.7rem; white-space: nowrap; }

  .rules-note { margin: 0; padding: 0.75rem 0.9rem 0.25rem; color: var(--text-muted); font-size: 0.78rem; line-height: 1.5; }
  .rules-note b { color: var(--text); }
  .rules { list-style: none; margin: 0; padding: 0.5rem 0.9rem 0.9rem; display: grid; gap: 0.35rem; }
  .rules li { display: flex; align-items: center; gap: 0.75rem; font-size: 0.78rem; }
  .rules span { min-width: 160px; color: var(--text-muted); }
  .rules b { color: var(--warn); font-weight: 600; }
  .rules b.crit { color: var(--crit); }
  .rules b.info { color: var(--series-4); }

  @media (max-width: 800px) {
    .row-main { grid-template-columns: auto 1fr auto; }
    .label-text, .detail, .when { display: none; }
  }
</style>
