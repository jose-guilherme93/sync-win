<script lang="ts">
  import { onMount } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import type { AgentUsagePoint, Device } from '../../lib/types'
  import { formatBytes, formatRelative } from '../../lib/format'
  import { agentHealth, agentStateLabel, summarizeAgentFleet, type AgentHealth } from '../../lib/agent-health'

  export let devices: Device[] = []
  export let shippedAgentVersion = ''

  type Row = AgentHealth & { device: Device; ramTotal: number; cores: number; series: AgentUsagePoint[] }

  let usage: Record<string, AgentUsagePoint[]> = {}
  let usageError = ''
  let usageFrom = 0
  let usageTo = 0
  let serverVersion = ''
  let signatureOk: boolean | null = null
  let factsLoaded = false
  let usageLoaded = false

  const SPARK_W = 96
  const SPARK_H = 26

  $: rows = devices
    .map((device) => {
      const health = agentHealth(device, shippedAgentVersion)
      const hw = device.hardware
      return {
        ...health,
        device,
        ramTotal: hw?.memory_total_bytes ?? 0,
        cores: hw?.cpu_core_usage?.length ?? 0,
        series: usage[device.id] ?? []
      } as Row
    })
    .sort((a, b) => (b.memory ?? -1) - (a.memory ?? -1) || a.label.localeCompare(b.label))
  $: summary = summarizeAgentFleet(devices, shippedAgentVersion)
  $: target = serverVersion || shippedAgentVersion
  $: reported = rows.filter((row) => row.ramTotal > 0)

  function ramShare(row: Row): number {
    if (!row.memory || !row.ramTotal) return 0
    return (row.memory / row.ramTotal) * 100
  }

  function pct(value: number): string {
    if (value === 0) return '0%'
    if (value < 0.01) return '<0.01%'
    return `${value.toFixed(2)}%`
  }

  function percentValue(value: number | null): string {
    return value == null ? '—' : `${value.toFixed(2)}%`
  }

  function stateClass(state: AgentHealth['state']): string {
    switch (state) {
      case 'current':
        return 'ok'
      case 'outdated':
      case 'updating':
        return 'warn'
      case 'failed':
      case 'blocked':
        return 'crit'
      default:
        return 'off'
    }
  }

  // Two rules decide this line: the x axis is time, so a device that has been
  // reporting for ten minutes draws ten minutes at the right edge instead of
  // stretching them over two hours; and the y axis is scaled to the series' own
  // band, because a 12 MB line against a 16 GB axis would be indistinguishable
  // from a dead agent.
  function sparkPoints(series: AgentUsagePoint[]): string {
    if (series.length < 2) return ''
    const span = Math.max(1, usageTo - usageFrom)
    const values = series.map((point) => point.rss ?? 0)
    const max = Math.max(...values)
    const min = Math.min(...values)
    const pad = Math.max((max - min) * 2, max * 0.02, 1)
    const lo = Math.max(0, min - pad)
    const hi = max + pad
    const band = hi - lo || 1
    const points = series
      .map((point) => {
        const t = Date.parse(point.t)
        if (!Number.isFinite(t)) return null
        const x = ((t - usageFrom) / span) * SPARK_W
        const y = SPARK_H - ((point.rss - lo) / band) * SPARK_H
        return `${x.toFixed(1)},${y.toFixed(1)}`
      })
      .filter((value): value is string => value != null)
    if (points.length < 2) return ''
    return points.join(' ')
  }

  function sparkDots(series: AgentUsagePoint[]): { x: number; y: number }[] {
    if (series.length !== 1) return []
    const point = series[0]
    const t = Date.parse(point.t)
    if (!Number.isFinite(t)) return []
    const span = Math.max(1, usageTo - usageFrom)
    const x = ((t - usageFrom) / span) * SPARK_W
    const max = Math.max(point.rss ?? 0, 1)
    return [{ x, y: SPARK_H - ((point.rss ?? 0) / (max * 1.2)) * SPARK_H }]
  }

  function peak(series: AgentUsagePoint[]): string {
    if (series.length === 0) return 'no history'
    const max = Math.max(...series.map((point) => point.rss ?? 0))
    return `peak ${formatBytes(max)} · ${series.length} samples · 2h`
  }

  async function loadUsage() {
    try {
      const res = await apiFetch(apiURL('/api/agents/usage'))
      if (!res.ok) throw new Error(`usage request failed (${res.status})`)
      const payload = await res.json()
      usage = payload?.devices ?? {}
      usageFrom = Date.parse(payload?.from) || Date.now() - 2 * 60 * 60_000
      usageTo = Date.parse(payload?.to) || Date.now()
    } catch (e) {
      usageError = e instanceof Error ? e.message : 'Could not load the agent history'
    } finally {
      usageLoaded = true
    }
  }

  async function loadServer() {
    try {
      const [versionRes, signatureRes] = await Promise.all([
        apiFetch(apiURL('/api/agent/version')),
        apiFetch(apiURL('/api/agent/signature'))
      ])
      if (versionRes.ok) {
        const payload = await versionRes.json().catch(() => ({}))
        if (typeof payload?.version === 'string') serverVersion = payload.version
      }
      signatureOk = signatureRes.ok
    } catch {
      signatureOk = null
    } finally {
      factsLoaded = true
    }
  }

  onMount(() => {
    void loadUsage()
    void loadServer()
  })
</script>

<section class="agents">
  <header class="page-head">
    <div>
      <p class="eyebrow">Fleet</p>
      <h1>Agents</h1>
      <p class="muted">
        What each agent costs on its own machine: the share of RAM it holds, the CPU it uses, and
        how steady that has been over the last two hours.
      </p>
    </div>
    <div class="server-facts">
      <div>
        <span class="k">Server ships</span>
        <span class="mono">{target || '—'}</span>
      </div>
      <div>
        <span class="k">Signature</span>
        <span class:crit={signatureOk === false}>{!factsLoaded ? 'checking…' : signatureOk === null ? 'unknown' : signatureOk ? 'available' : 'missing'}</span>
      </div>
    </div>
  </header>

  <div class="summary">
    <span><b>{summary.total}</b> devices</span>
    <span><b>{formatBytes(summary.agentMemoryBytes)}</b> agent RSS</span>
    <span><b>{percentValue(summary.agentCPUPercent)}</b> of all cores</span>
    <span class="sep"></span>
    <span><span class="dot ok"></span>{summary.current} current</span>
    <span><span class="dot warn"></span>{summary.outdated + summary.updating} behind</span>
    <span><span class="dot crit"></span>{summary.needsAttention} blocked</span>
    {#if summary.unknown > 0}<span><span class="dot"></span>{summary.unknown} no report</span>{/if}
    {#if usageError}<span class="err">{usageError}</span>{/if}
  </div>

  {#if rows.length === 0}
    <p class="empty">No devices enrolled yet.</p>
  {:else}
    <div class="list">
      {#each rows as row (row.id)}
        <article class="row">
          <div class="who">
            <div class="name">
              <span class="state-dot {stateClass(row.state)}" aria-hidden="true"></span>
              <span>{row.label}</span>
            </div>
            <div class="meta mono">
              {row.installed}
              {#if row.ramTotal}· {formatBytes(row.ramTotal)}{/if}
              {#if row.cores}· {row.cores} cores{/if}
            </div>
            <div class="state">
              {agentStateLabel(row.state)}
              {#if row.error}
                <span class="err" title={row.error}>· {row.error}</span>
              {:else if row.lastAttempt !== 'never'}
                <span class="faint">· attempt {row.lastAttempt}</span>
              {/if}
            </div>
          </div>

          <div class="metric">
            <div class="label">
              <span>RAM held</span>
              <span class="mono">
                {#if row.memory == null}
                  not reported
                {:else if row.ramTotal === 0}
                  {formatBytes(row.memory)} of unknown capacity
                {:else}
                  {formatBytes(row.memory)} · {pct(ramShare(row))}
                {/if}
              </span>
            </div>
            <div class="track" title={row.ramTotal ? `${formatBytes(row.memory ?? 0)} of ${formatBytes(row.ramTotal)}` : 'machine RAM not reported'}>
              <div class="fill" style={`width:${Math.min(100, ramShare(row))}%`}></div>
            </div>
            <div class="hint">
              {#if row.ramTotal}
                of {formatBytes(row.ramTotal)} total
              {:else}
                machine RAM not reported
              {/if}
            </div>
          </div>

          <div class="metric">
            <div class="label">
              <span>CPU used</span>
              <span class="mono">{percentValue(row.cpu)}</span>
            </div>
            <div class="track" title="share of every core on the machine">
              <div class="fill" style={`width:${Math.min(100, row.cpu ?? 0)}%`}></div>
            </div>
            <div class="hint">
              {#if row.cores}
                share of all {row.cores} cores
              {:else}
                share of all cores
              {/if}
            </div>
          </div>

          <div class="spark" title={peak(row.series)}>
            {#if row.series.length > 1}
              <svg width={SPARK_W} height={SPARK_H} viewBox={`0 0 ${SPARK_W} ${SPARK_H}`} aria-label="Agent RSS over the last two hours">
                <polyline points={sparkPoints(row.series)} fill="none" stroke="var(--accent)" stroke-width="1.4" />
                <line x1="0" y1={SPARK_H} x2={SPARK_W} y2={SPARK_H} stroke="var(--border)" />
              </svg>
            {:else if row.series.length === 1}
              <svg width={SPARK_W} height={SPARK_H} viewBox={`0 0 ${SPARK_W} ${SPARK_H}`} aria-label="One agent RSS sample so far">
                {#each sparkDots(row.series) as dot}
                  <circle cx={dot.x} cy={dot.y} r="1.8" fill="var(--accent)" />
                {/each}
                <line x1="0" y1={SPARK_H} x2={SPARK_W} y2={SPARK_H} stroke="var(--border)" />
              </svg>
            {:else}
              <div class="no-spark">{usageLoaded ? 'no history' : 'checking…'}</div>
            {/if}
          </div>
        </article>
      {/each}
    </div>

    <p class="note">
      The track is the machine's own capacity, so the fill is the agent's real share; a fill you
      cannot see is the normal result. The line is the same RSS over the raw two-hour window,
      scaled to its own band, and its peak is in the tooltip. {reported.length} of {rows.length}
      devices report both the agent cost and the machine's total RAM.
    </p>
    {#if summary.blocked > 0}
      <p class="note crit">
        {summary.blocked} device{summary.blocked === 1 ? '' : 's'} cannot self-update: the installed
        agent was built without the update signing key. Reinstall once from the dashboard.
      </p>
    {/if}
    {#if signatureOk === false}
      <p class="note crit">
        The server serves no agent signature, so no device can update until it does.
      </p>
    {/if}
  {/if}
</section>

<style>
  .agents {
    display: grid;
    gap: 1rem;
    max-width: 1100px;
  }

  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
  }

  .page-head h1 {
    margin: 0.1rem 0 0.3rem;
    font-size: 1.25rem;
    color: var(--text-bright);
  }

  .eyebrow {
    margin: 0;
    color: var(--text-faint);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
  }

  .muted {
    color: var(--text-muted);
    font-size: 0.82rem;
    margin: 0;
    max-width: 52ch;
  }

  .server-facts {
    display: flex;
    gap: 1.25rem;
    flex-shrink: 0;
  }

  .server-facts > div {
    display: grid;
    gap: 0.1rem;
  }

  .k {
    color: var(--text-faint);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .server-facts .mono,
  .server-facts span:not(.k) {
    font-size: 0.8rem;
    color: var(--text);
  }

  .crit {
    color: var(--crit);
  }

  .summary {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.35rem 1rem;
    color: var(--text-muted);
    font-size: 0.78rem;
  }

  .summary b {
    color: var(--text-bright);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }

  .summary .sep {
    width: 1px;
    height: 12px;
    background: var(--border-strong);
  }

  .dot,
  .state-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    margin-right: 0.35rem;
    background: var(--text-faint);
    vertical-align: middle;
  }

  .dot.ok,
  .state-dot.ok {
    background: var(--ok);
  }

  .dot.warn,
  .state-dot.warn {
    background: var(--warn);
  }

  .dot.crit,
  .state-dot.crit {
    background: var(--crit);
  }

  .list {
    display: grid;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card);
  }

  .row {
    display: grid;
    grid-template-columns: minmax(180px, 1.1fr) 1fr 1fr 108px;
    gap: 1.25rem;
    align-items: center;
    padding: 0.7rem 0.9rem;
    border-top: 1px solid var(--border);
  }

  .row:first-child {
    border-top: 0;
  }

  .name {
    display: flex;
    align-items: center;
    color: var(--text-bright);
    font-weight: 600;
    font-size: 0.85rem;
  }

  .meta {
    margin-top: 0.15rem;
    color: var(--text-faint);
    font-size: 0.7rem;
  }

  .state {
    margin-top: 0.2rem;
    color: var(--text-muted);
    font-size: 0.72rem;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .err {
    color: var(--crit);
  }

  .metric {
    display: grid;
    gap: 0.25rem;
    min-width: 0;
  }

  .label {
    display: flex;
    justify-content: space-between;
    gap: 0.5rem;
    color: var(--text-faint);
    font-size: 0.7rem;
  }

  .label span:last-child {
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }

  .track {
    height: 5px;
    border-radius: 3px;
    background: var(--border);
    overflow: hidden;
  }

  /* A real share of a 16 GB machine is invisible at any sane scale; the floor
     keeps it present without inventing size. */
  .fill {
    height: 100%;
    min-width: 2px;
    border-radius: 3px;
    background: var(--accent);
  }

  .hint {
    color: var(--text-faint);
    font-size: 0.65rem;
  }

  .spark {
    display: grid;
    place-items: center;
    min-height: 28px;
  }

  .no-spark {
    color: var(--text-faint);
    font-size: 0.65rem;
  }

  .mono {
    font-family: var(--mono);
    font-variant-numeric: tabular-nums;
  }

  .faint {
    color: var(--text-faint);
  }

  .note {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.78rem;
    line-height: 1.55;
  }

  .note.crit {
    color: var(--crit);
  }

  .empty {
    margin: 0;
    color: var(--text-muted);
  }

  @media (max-width: 820px) {
    .row {
      grid-template-columns: 1fr 1fr;
      gap: 0.75rem 1rem;
    }

    .spark {
      display: none;
    }
  }
</style>
