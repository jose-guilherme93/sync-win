<script lang="ts">
  // One renderer for the detail, used by the drawer in the table layout and by
  // the persistent inspector in the tree layout. Two copies of this markup had
  // already drifted apart once; the pane and the drawer must agree by
  // construction, not by review.
  import Icon from '../ui/Icon.svelte'
  import TimeSeriesChart from '../ui/TimeSeriesChart.svelte'
  import { formatBytes } from '../../lib/format'
  import { historyWindowMs } from '../../lib/processes'
  import {
    HISTORY_RANGES,
    STATE_LABELS,
    canSignal,
    childrenOf,
    formatClock,
    formatCpuPercent,
    formatCpuTime,
    formatUptime,
    ioText,
    parentChain,
    signalBlockReason,
    type HistoryRange
  } from '../../lib/processes'
  import type { ProcessHistory, ProcessRow, ProcessSignal, ProcessSnapshot } from '../../lib/types'

  export let row: ProcessRow
  export let rows: ProcessRow[] = []
  export let history: ProcessHistory | null = null
  export let historyRange: HistoryRange = '24h'
  export let ioAvailable = false
  export let signalsEnabled = false

  export let showHeader = true
  export let showClose = false
  export let onClose: () => void = () => {}
  export let onSelect: (row: ProcessRow) => void = () => {}
  export let onRangeChange: (range: HistoryRange) => void = () => {}
  export let onSignal: (row: ProcessRow, signal: ProcessSignal) => void = () => {}
  export let onKill: (row: ProcessRow, event?: MouseEvent) => void = () => {}
  export let onRenice: (row: ProcessRow, nice: number) => void = () => {}
  export let onIonice: (row: ProcessRow, ioClass: number, level: number) => void = () => {}

  type Metric = 'cpu' | 'rss' | 'io'
  let metric: Metric = 'cpu'
  let copied = false

  $: chain = parentChain(rows, row)
  $: kids = childrenOf(rows, row.pid)
  $: labels = history
    ? [...new Set(history.series.flatMap((series) => series.points.map((point) => new Date(point.t * 1000).toISOString())))].sort()
    : []
  $: historyEnd = labels.length ? Date.parse(labels[labels.length - 1]) : null
  $: processSeries = history?.series.find((series) => series.name === row.name) ?? null
  $: timeline = timelineSeries(processSeries, labels, metric)
  $: blocked = signalBlockReason(row, {
    processes: rows,
    stats: null,
    collected_at: '',
    io_available: ioAvailable,
    signals_enabled: signalsEnabled,
    collected: true
  } as ProcessSnapshot)

  function timelineSeries(
    series: ProcessHistory['series'][number] | null,
    times: string[],
    which: Metric
  ): { label: string; values: (number | null)[]; color: string }[] {
    if (!series) return []
    const byTime = new Map(series.points.map((point) => [point.t, point]))
    if (which === 'io') {
      // IO rates arrive in bytes per second; the tab says KB/s, so convert
      // here rather than asking the reader to divide. A null stays null: an
      // older server never recorded IO, and a zero would draw a flat line.
      const pick = (key: 'ioRead' | 'ioWrite') =>
        times.map((label) => {
          const point = byTime.get(Math.floor(Date.parse(label) / 1000))
          const value = point?.[key]
          return value == null ? null : value / 1024
        })
      return [
        { label: 'read KB/s', values: pick('ioRead'), color: 'var(--series-1)' },
        { label: 'write KB/s', values: pick('ioWrite'), color: 'var(--series-2)' }
      ]
    }
    const values = times.map((label) => {
      const point = byTime.get(Math.floor(Date.parse(label) / 1000))
      if (!point) return null
      return which === 'rss' ? point.rss / (1024 * 1024) : point.cpu
    })
    return [
      {
        label: which === 'rss' ? 'RSS MB' : 'CPU % of capacity',
        values,
        color: which === 'rss' ? 'var(--series-4)' : 'var(--series-1)'
      }
    ]
  }

  async function copyCommand() {
    const text = row.cmd || row.name
    try {
      await navigator.clipboard?.writeText(text)
      copied = true
      setTimeout(() => (copied = false), 1200)
    } catch {
      // A clipboard the browser refuses is not a reason to lose the view.
    }
  }
</script>

<div class="detail">
  {#if showHeader}
    <header class="dhead">
      <div class="t">
        <div class="name" title={row.name}>{row.name}</div>
        <div class="sub">pid {row.pid} · ppid {row.ppid} · {row.user}</div>
      </div>
      {#if showClose}
        <button class="close" on:click={onClose} aria-label="Close"><Icon name="x" size={13} /></button>
      {/if}
    </header>
  {/if}

  <div class="cmdblock">
    <code>{row.cmd || row.name}</code>
    <button class="copybtn" on:click={copyCommand} aria-label="Copy command line" title={copied ? 'Copied' : 'Copy'}>
      <Icon name={copied ? 'check' : 'copy'} size={11} />
    </button>
  </div>

  <dl class="facts">
    <div><dt>User</dt><dd class="mono">{row.user || '—'}</dd></div>
    <div>
      <dt>State</dt>
      <dd class="meaning">
        <span class="statebadge state-{row.state}">{row.state}</span>
        <span class="meaning-text">{STATE_LABELS[row.state]}</span>
      </dd>
    </div>
    <div><dt>Threads</dt><dd class="num">{row.threads || '—'}</dd></div>
    <div><dt>FDs</dt><dd class="num">{row.kernel || row.state === 'Z' ? '—' : row.fd_count || '—'}</dd></div>
    <div><dt>OOM score</dt><dd class="num">{row.state === 'Z' || row.oom_score < 0 ? '—' : row.oom_score}</dd></div>
    <div><dt>Nice</dt><dd class="num">{row.nice}</dd></div>
    <div>
      <dt>Started</dt>
      <dd class="mono" title={formatUptime(row.uptime_seconds)}>{formatClock(row.start_time_unix)} · {formatUptime(row.uptime_seconds)}</dd>
    </div>
    <div><dt>CPU time</dt><dd class="num">{formatCpuTime(row.cpu_time_seconds)}</dd></div>
    <div><dt>RSS / swap</dt><dd class="num">{formatBytes(row.rss_bytes)} / {formatBytes(row.swap_bytes)}</dd></div>
    <div><dt>Unit</dt><dd class="mono ellipsis" title={row.unit}>{row.unit || '—'}</dd></div>
    <div><dt>Cgroup</dt><dd class="mono ellipsis" title={row.cgroup}>{row.cgroup || '—'}</dd></div>
    <div><dt>Container</dt><dd class="mono ellipsis" title={row.container}>{row.container || '—'}</dd></div>
  </dl>

  <h4 class="dsub">Parent chain</h4>
  <div class="chain">
    {#each chain as entry (entry.pid)}
      {#if entry.pid !== chain[0].pid}<span class="sep">→</span>{/if}
      <button on:click={() => onSelect(entry)} title={entry.name}>
        {entry.pid} {entry.name.length > 18 ? `${entry.name.slice(0, 18)}…` : entry.name}
      </button>
    {/each}
  </div>

  <h4 class="dsub">Direct children ({kids.length})</h4>
  {#if kids.length > 0}
    <ul class="kids">
      {#each kids as child (child.pid)}
        <li>
          <button type="button" class="kid" on:click={() => onSelect(child)}>
            <span class="pidm">{child.pid}</span>
            <span class="bright ellipsis">{child.name}</span>
            <span class="statebadge state-{child.state}">{child.state}</span>
            <span class="cpu num" class:hot={child.cpu_percent >= 80}>{formatCpuPercent(child.cpu_percent)}</span>
          </button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="none">none</p>
  {/if}

  <h4 class="dsub">Timeline</h4>
  <div class="tlbar">
    <span class="segmented" role="group" aria-label="Timeline metric">
      <button class:active={metric === 'cpu'} on:click={() => (metric = 'cpu')}>CPU %</button>
      <button class:active={metric === 'rss'} on:click={() => (metric = 'rss')}>RSS MB</button>
      <button class:active={metric === 'io'} on:click={() => (metric = 'io')}>IO read / write KB/s</button>
    </span>
    <span class="segmented" role="group" aria-label="Timeline range">
      {#each HISTORY_RANGES as range (range)}
        <button class:active={historyRange === range} on:click={() => onRangeChange(range)}>{range}</button>
      {/each}
    </span>
  </div>
  <TimeSeriesChart
    labels={labels}
    series={timeline}
    leftUnit={metric === 'io' ? 'KB/s' : metric === 'rss' ? 'MB' : '%'}
    windowMs={historyWindowMs(historyRange)}
    windowEnd={historyEnd}
    height={150}
    emptyMessage={metric === 'io' ? 'IO is not recorded over time.' : 'No samples for this process in the window.'}
  />

  <h4 class="dsub">Listening sockets</h4>
  {#if row.ports.length > 0}
    <ul class="sockets">
      {#each row.ports as port (port.proto + port.port)}
        <li>
          <span class="mono accent">{port.port}</span>
          <span class="mono faint upper">{port.proto}</span>
          <span class="mono">{port.addr || '*'}</span>
          {#if port.addr === '0.0.0.0' || port.addr === '::'}
            <span class="pub">off-host</span>
          {/if}
        </li>
      {/each}
    </ul>
  {:else}
    <p class="none">none</p>
  {/if}

  <div class="actions">
    <h4 class="dsub" style="margin:0 0 0.1rem">Actions</h4>
    <div class="gates">
      <span>server gate <b>{signalsEnabled ? 'on' : 'off'}</b></span>
      <span>agent policy <b>allow_process_signals</b></span>
      <span>audited</span>
    </div>
    <div class="abtns">
      <button class="btn warn" disabled={blocked != null} on:click={() => onSignal(row, 'TERM')} title="SIGTERM · graceful, audited">Terminate</button>
      <button class="btn crit" disabled={blocked != null} on:click={(e) => onKill(row, e)} title="SIGKILL · destructive, audited">Force kill</button>
      <button class="btn" disabled={blocked != null} on:click={() => onSignal(row, 'STOP')} title="SIGSTOP · pause, audited">Stop</button>
      <button class="btn" disabled={blocked != null} on:click={() => onSignal(row, 'CONT')} title="SIGCONT · resume">Continue</button>
    </div>
    {#if blocked}
      <p class="blocked">{blocked} — signal actions blocked</p>
    {/if}
    {#if signalsEnabled && !blocked}
      <div class="renice">
        <label>Renice <input type="number" min="-20" max="19" value={row.nice} on:change={(e) => onRenice(row, Number((e.currentTarget as HTMLInputElement).value))} /></label>
        <label>Ionice <input type="number" min="1" max="3" value={2} on:change={(e) => onIonice(row, Number((e.currentTarget as HTMLInputElement).value), 4)} /></label>
      </div>
    {/if}
  </div>
</div>

<style>
  .detail { display: grid; gap: 0.15rem; min-width: 0; }
  .dhead { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; }
  .dhead .t { flex: 1; min-width: 0; }
  .dhead .name { font-size: 0.95rem; font-weight: 600; color: var(--text-bright); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .dhead .sub { font-family: var(--mono); color: var(--text-faint); font-size: 0.7rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .close { border: 1px solid var(--border); background: transparent; color: var(--text-muted); cursor: pointer; border-radius: var(--radius-sm); padding: 0.25rem; flex: none; }
  .close:hover { color: var(--text); }
  .cmdblock { display: flex; gap: 0.4rem; align-items: flex-start; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 0.45rem 0.55rem; }
  .cmdblock code { flex: 1; font-family: var(--mono); font-size: 0.68rem; color: var(--text-muted); word-break: break-all; line-height: 1.5; }
  .copybtn { flex: none; border: 1px solid var(--border); background: transparent; color: var(--text-muted); cursor: pointer; border-radius: 6px; padding: 0.2rem; display: inline-flex; }
  .copybtn:hover { color: var(--accent); border-color: var(--accent-border); }

  .facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 0.35rem 0.8rem; margin: 0.6rem 0; }
  .facts > div { display: grid; gap: 0.05rem; min-width: 0; }
  .facts dt { color: var(--text-faint); font-size: 0.62rem; text-transform: uppercase; letter-spacing: 0.05em; font-weight: 600; }
  .facts dd { margin: 0; color: var(--text); font-size: 0.76rem; font-variant-numeric: tabular-nums; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .facts dd.meaning { display: inline-flex; align-items: center; gap: 0.3rem; }
  .num { font-variant-numeric: tabular-nums; }
  .mono { font-family: var(--mono); }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }

  .statebadge { display: inline-flex; align-items: center; justify-content: center; min-width: 1.05rem; padding: 0.05rem 0.3rem; border-radius: 4px; font-size: 9.5px; font-weight: 700; font-family: var(--mono); }
  .state-R { background: var(--accent-dim); color: var(--accent); }
  .state-S { background: var(--card-inset); color: var(--text-muted); }
  .state-D { background: var(--warn-dim); color: var(--warn); }
  .state-Z { background: var(--crit-dim); color: var(--crit); }
  .state-T, .state-I { background: var(--card-inset); color: var(--text-faint); }
  .meaning-text { font-size: 0.7rem; color: var(--text-faint); }

  .dsub { margin: 0.8rem 0 0.35rem; font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-faint); font-weight: 600; }
  .chain { display: flex; flex-wrap: wrap; align-items: center; gap: 0.25rem; font-family: var(--mono); font-size: 0.7rem; }
  .chain button { border: 1px solid var(--border); background: var(--card-inset); color: var(--text-muted); border-radius: 5px; padding: 0.12rem 0.35rem; cursor: pointer; font-family: var(--mono); font-size: 0.68rem; }
  .chain button:hover { color: var(--text); border-color: var(--border-strong); }
  .chain .sep { color: var(--text-faint); }
  .kids { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.2rem; }
  .kids li { border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--card-inset); }
  .kid { display: flex; align-items: center; gap: 0.5rem; width: 100%; padding: 0.22rem 0.45rem; font-size: 0.74rem; border: 0; background: transparent; color: inherit; font: inherit; text-align: left; cursor: pointer; border-radius: inherit; }
  .kid:hover { border-color: var(--border-strong); background: var(--card-hover); }
  .kid .cpu { margin-left: auto; color: var(--text-muted); }
  .kid .cpu.hot { color: var(--warn); }
  .pidm { font-family: var(--mono); color: var(--text-faint); font-size: 0.68rem; }
  .bright { color: var(--text-bright); }
  .none { margin: 0.1rem 0; color: var(--text-faint); font-size: 0.7rem; }

  .tlbar { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; flex-wrap: wrap; margin-bottom: 0.4rem; }
  .segmented { display: inline-flex; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
  .segmented button { padding: 0.28rem 0.5rem; background: transparent; border: 0; border-right: 1px solid var(--border); color: var(--text-faint); font-size: 0.71rem; font-weight: 600; cursor: pointer; white-space: nowrap; }
  .segmented button:last-child { border-right: 0; }
  .segmented button:hover { color: var(--text); }
  .segmented button.active { background: var(--accent-dim); color: var(--accent); }

  .sockets { list-style: none; margin: 0; padding: 0; }
  .sockets li { display: flex; gap: 0.55rem; align-items: baseline; font-size: 0.74rem; padding: 0.25rem 0; }
  .sockets .accent { color: var(--accent); }
  .sockets .faint { color: var(--text-muted); font-size: 0.66rem; }
  .sockets .upper { text-transform: uppercase; }
  .pub { padding: 0.05rem 0.4rem; border-radius: 999px; background: var(--warn-dim); color: var(--warn); font-size: 0.6rem; font-weight: 600; }

  .actions { margin-top: 0.9rem; border-top: 1px solid var(--border); padding-top: 0.7rem; display: grid; gap: 0.5rem; }
  .gates { display: flex; flex-wrap: wrap; gap: 0.3rem 0.6rem; font-size: 0.66rem; color: var(--text-faint); }
  .gates b { color: var(--text-muted); font-weight: 600; }
  .abtns { display: flex; flex-wrap: wrap; gap: 0.4rem; }
  .btn { display: inline-flex; align-items: center; gap: 0.3rem; padding: 0.3rem 0.55rem; background: transparent; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-muted); font: inherit; font-size: 0.72rem; cursor: pointer; }
  .btn:hover:not(:disabled) { border-color: var(--accent-border); color: var(--text); }
  .btn:disabled { opacity: 0.45; cursor: default; }
  .btn.warn { border-color: rgba(251, 191, 36, 0.45); color: var(--warn); }
  .btn.crit { border-color: rgba(248, 113, 113, 0.45); color: var(--crit); }
  .blocked { margin: 0; color: var(--warn); font-size: 0.7rem; }
  .renice { display: flex; gap: 0.75rem; }
  .renice label { display: flex; align-items: center; gap: 0.3rem; color: var(--text-faint); font-size: 0.72rem; }
  .renice input { width: 4rem; padding: 0.32rem 0.5rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font: inherit; font-size: 0.75rem; }
</style>
