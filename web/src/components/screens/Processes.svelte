<script lang="ts">
  import { formatBytes, formatDuration, formatRelative } from '../../lib/format'
  import type { Device, ProcessInfo, ProcessSummary } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'

  // Processes come from the agent's last telemetry payload. The agent reports
  // the top consumers plus every stopped/zombie row and a tally of the whole
  // machine, so this screen can be diagnostic without keeping a per-process
  // history or paying for an extra collection pass.
  export let device: Device

  type SortKey = 'cpu' | 'memory' | 'pid' | 'name' | 'user' | 'threads' | 'elapsed'
  type StateFilter = 'all' | 'running' | 'sleeping' | 'disk' | 'stopped' | 'zombie' | 'problem'

  let sort: SortKey = 'cpu'
  let direction: 'asc' | 'desc' = 'desc'
  let query = ''
  let stateFilter: StateFilter = 'all'
  let selectedPid: number | null = null

  $: hw = device.hardware
  $: totalMem = hw?.memory_total_bytes || 0
  $: cores = hw?.cpu_core_usage?.length || 0
  $: summary = hw?.process_summary ?? fallbackSummary(hw)
  $: summaryComplete = Boolean(hw?.process_summary)
  $: reported = mergeReported(hw)
  $: filtered = applyFilter(reported, query, stateFilter)
  $: sorted = applySort(filtered, sort, direction)
  $: selected = reported.find((proc) => proc.pid === selectedPid) ?? null
  $: stateCounts = countStates(reported)
  $: stateFilters = [
    ['all', `All ${reported.length}`],
    ['running', `Running ${stateCounts.running}`],
    ['sleeping', `Sleeping ${stateCounts.sleeping + stateCounts.idle}`],
    ['disk', `Disk ${stateCounts.disk}`],
    ['stopped', `Stopped ${stateCounts.stopped}`],
    ['zombie', `Zombie ${stateCounts.zombie}`],
    ['problem', `Problems ${stateCounts.stopped + stateCounts.zombie + stateCounts.disk}`]
  ] as [StateFilter, string][]

  function mergeReported(hardware: Device['hardware']): ProcessInfo[] {
    if (!hardware) return []
    const byPid = new Map<number, ProcessInfo>()
    for (const list of [hardware.top_cpu_processes, hardware.top_mem_processes, hardware.stopped_processes, hardware.zombie_processes]) {
      for (const proc of list ?? []) {
        const existing = byPid.get(proc.pid)
        // The same PID can appear in both top lists; merge so a row from the
        // memory list does not lose the command line reported by the CPU one.
        byPid.set(proc.pid, existing ? { ...existing, ...proc } : proc)
      }
    }
    return [...byPid.values()]
  }

  function fallbackSummary(hardware: Device['hardware']): ProcessSummary {
    const list = mergeReported(hardware)
    const counts = countStates(list)
    return {
      total: list.length,
      running: counts.running,
      sleeping: counts.sleeping + counts.idle,
      disk_sleep: counts.disk,
      stopped: counts.stopped,
      zombie: counts.zombie,
      other: counts.other,
      threads: list.reduce((sum, proc) => sum + (proc.threads || 0), 0)
    }
  }

  function countStates(list: ProcessInfo[]) {
    const counts = { running: 0, sleeping: 0, disk: 0, stopped: 0, zombie: 0, idle: 0, other: 0 }
    for (const proc of list) {
      switch (proc.state) {
        case 'R': counts.running++; break
        case 'S': counts.sleeping++; break
        case 'D': counts.disk++; break
        case 'T':
        case 't': counts.stopped++; break
        case 'Z': counts.zombie++; break
        case 'I': counts.idle++; break
        default: counts.other++
      }
    }
    return counts
  }

  type StateInfo = { label: string; tone: 'running' | 'sleeping' | 'disk' | 'stopped' | 'zombie' | 'other'; meaning: string }

  function stateInfo(state?: string): StateInfo {
    switch (state) {
      case 'R': return { label: 'Running', tone: 'running', meaning: 'Currently running on a CPU.' }
      case 'S': return { label: 'Sleeping', tone: 'sleeping', meaning: 'Waiting for an event, signal or timer.' }
      case 'D': return { label: 'Disk sleep', tone: 'disk', meaning: 'Uninterruptible sleep, usually waiting on I/O. It cannot be killed until the I/O finishes.' }
      case 'T':
      case 't': return { label: 'Stopped', tone: 'stopped', meaning: 'Stopped by SIGSTOP or a debugger. It consumes no CPU until continued.' }
      case 'Z': return { label: 'Zombie', tone: 'zombie', meaning: 'Exited, waiting for its parent to reap the exit status. It consumes no CPU or memory.' }
      case 'I': return { label: 'Idle', tone: 'sleeping', meaning: 'Idle kernel thread.' }
      case 'X':
      case 'x': return { label: 'Dead', tone: 'zombie', meaning: 'Dead; it should disappear shortly.' }
      default: return { label: state || 'Unknown', tone: 'other', meaning: 'State not reported by the agent.' }
    }
  }

  function matchesState(proc: ProcessInfo, filter: StateFilter): boolean {
    if (filter === 'all') return true
    if (filter === 'problem') return ['T', 't', 'Z', 'D'].includes(proc.state || '')
    if (filter === 'disk') return proc.state === 'D'
    if (filter === 'stopped') return proc.state === 'T' || proc.state === 't'
    if (filter === 'zombie') return proc.state === 'Z'
    if (filter === 'running') return proc.state === 'R'
    return proc.state === 'S' || proc.state === 'I'
  }

  function applyFilter(list: ProcessInfo[], text: string, filter: StateFilter): ProcessInfo[] {
    const needle = text.trim().toLowerCase()
    return list.filter((proc) => {
      if (!matchesState(proc, filter)) return false
      if (!needle) return true
      return String(proc.pid).includes(needle)
        || proc.name.toLowerCase().includes(needle)
        || (proc.command || '').toLowerCase().includes(needle)
        || (proc.user || '').toLowerCase().includes(needle)
    })
  }

  function sortValue(proc: ProcessInfo, key: SortKey): number | string {
    switch (key) {
      case 'memory': return proc.mem_rss_bytes
      case 'pid': return proc.pid
      case 'name': return proc.name.toLowerCase()
      case 'user': return (proc.user || '').toLowerCase()
      case 'threads': return proc.threads || 0
      case 'elapsed': return proc.elapsed_seconds || 0
      default: return proc.cpu_percent
    }
  }

  function applySort(list: ProcessInfo[], key: SortKey, dir: 'asc' | 'desc'): ProcessInfo[] {
    const sorted = [...list]
    sorted.sort((a, b) => {
      const av = sortValue(a, key)
      const bv = sortValue(b, key)
      let result = 0
      if (typeof av === 'string' || typeof bv === 'string') {
        result = String(av).localeCompare(String(bv))
      } else {
        result = av - bv
      }
      if (result === 0) result = key === 'cpu' ? a.mem_rss_bytes - b.mem_rss_bytes : a.pid - b.pid
      return dir === 'asc' ? result : -result
    })
    return sorted
  }

  function sortBy(key: SortKey) {
    if (sort === key) {
      direction = direction === 'asc' ? 'desc' : 'asc'
    } else {
      sort = key
      direction = key === 'name' || key === 'user' ? 'asc' : 'desc'
    }
  }

  function age(seconds?: number): string {
    if (seconds == null) return '—'
    if (seconds < 60) return `${Math.max(0, Math.floor(seconds))}s`
    return formatDuration(seconds)
  }

  function memShare(bytes: number): number {
    return totalMem > 0 ? (bytes / totalMem) * 100 : 0
  }

  function selectRow(pid: number) {
    selectedPid = selectedPid === pid ? null : pid
  }
</script>

<section class="processes">
  <div class="status-strip">
    <span>sampled {hw ? formatRelative(hw.collected_at) : '—'}</span>
    <span>{cores} cores · load {hw?.load_average || '—'}</span>
    {#if summaryComplete}
      <span>{reported.length} reported of {summary.total} processes</span>
    {:else}
      <span class="warn-text">Counts are the reported subset; this agent predates the full process tally.</span>
    {/if}
  </div>

  <div class="summary" aria-label="Process summary">
    <article class="tile"><span class="label">Total</span><span class="value">{summary.total}</span><span class="sub">{reported.length} reported</span></article>
    <article class="tile"><span class="label">Running</span><span class="value">{summary.running}</span><span class="sub">on CPU</span></article>
    <article class="tile"><span class="label">Sleeping</span><span class="value">{summary.sleeping}</span><span class="sub">waiting</span></article>
    <article class="tile" class:alert={summary.disk_sleep > 0}><span class="label">Disk sleep</span><span class="value">{summary.disk_sleep}</span><span class="sub">blocked on I/O</span></article>
    <article class="tile" class:alert={summary.stopped > 0}><span class="label">Stopped</span><span class="value">{summary.stopped}</span><span class="sub">SIGSTOP / traced</span></article>
    <article class="tile" class:alert={summary.zombie > 0}><span class="label">Zombies</span><span class="value">{summary.zombie}</span><span class="sub">waiting to be reaped</span></article>
    <article class="tile"><span class="label">Threads</span><span class="value">{summary.threads}</span><span class="sub">all processes</span></article>
  </div>

  {#if summary.stopped > 0 || summary.zombie > 0 || summary.disk_sleep > 0}
    <div class="alert-strip" role="status">
      <span>
        <strong>{summary.stopped} stopped</strong> ·
        <strong>{summary.zombie} zombie{summary.zombie === 1 ? '' : 's'}</strong> ·
        <strong>{summary.disk_sleep} in disk sleep</strong>
      </span>
      <button on:click={() => (stateFilter = 'problem')}>Inspect problems</button>
    </div>
  {/if}

  <article class="card">
    <header class="card-head">
      <div class="head-title">
        <h2>Reported processes</h2>
        <span class="hint">{sorted.length} shown · top consumers and problem states</span>
      </div>
      <div class="toolbar">
        <input
          class="search"
          type="search"
          placeholder="Filter name, command, user or PID"
          bind:value={query}
          aria-label="Filter processes"
        />
      </div>
    </header>

    <div class="filters" role="group" aria-label="Filter by state">
      {#each stateFilters as [filter, label] (filter)}
        <button class:active={stateFilter === filter} on:click={() => (stateFilter = filter)} aria-pressed={stateFilter === filter}>
          {label}
        </button>
      {/each}
    </div>

    {#if !hw}
      <p class="muted pad">No telemetry yet.</p>
    {:else if reported.length === 0}
      <EmptyState
        icon="cog"
        title="No process data"
        message="The agent did not report a process snapshot for this cycle. It reports the top consumers and problem states, so an idle machine can legitimately report none."
      />
    {:else if sorted.length === 0}
      <p class="muted pad">No process matches this filter.</p>
    {:else}
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th class:active={sort === 'pid'}><button on:click={() => sortBy('pid')}>PID {sort === 'pid' ? (direction === 'asc' ? '▲' : '▼') : ''}</button></th>
              <th class:active={sort === 'name'}><button on:click={() => sortBy('name')}>Process {sort === 'name' ? (direction === 'asc' ? '▲' : '▼') : ''}</button></th>
              <th class:active={sort === 'user'}><button on:click={() => sortBy('user')}>User {sort === 'user' ? (direction === 'asc' ? '▲' : '▼') : ''}</button></th>
              <th>State</th>
              <th class="num" class:active={sort === 'cpu'}><button on:click={() => sortBy('cpu')}>CPU {sort === 'cpu' ? (direction === 'asc' ? '▲' : '▼') : ''}</button></th>
              <th class="num" class:active={sort === 'memory'}><button on:click={() => sortBy('memory')}>Memory {sort === 'memory' ? (direction === 'asc' ? '▲' : '▼') : ''}</button></th>
              <th class="num" class:active={sort === 'threads'}><button on:click={() => sortBy('threads')}>Threads {sort === 'threads' ? (direction === 'asc' ? '▲' : '▼') : ''}</button></th>
              <th class="num" class:active={sort === 'elapsed'}><button on:click={() => sortBy('elapsed')}>Age {sort === 'elapsed' ? (direction === 'asc' ? '▲' : '▼') : ''}</button></th>
            </tr>
          </thead>
          <tbody>
            {#each sorted as proc (proc.pid)}
              {@const state = stateInfo(proc.state)}
              <tr
                class:selected={selectedPid === proc.pid}
                on:click={() => selectRow(proc.pid)}
                on:keydown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault()
                    selectRow(proc.pid)
                  }
                }}
                tabindex="0"
              >
                <td class="pid">
                  <span class="mono">{proc.pid}</span>
                  <span class="ppid">ppid {proc.ppid ?? '—'}</span>
                </td>
                <td class="proc">
                  <span class="name">{proc.name}</span>
                  <span class="command" title={proc.command || proc.name}>{proc.command || proc.name}</span>
                </td>
                <td class="user">{proc.user || '—'}</td>
                <td><span class="state" class:running={state.tone === 'running'} class:sleeping={state.tone === 'sleeping'} class:disk={state.tone === 'disk'} class:stopped={state.tone === 'stopped'} class:zombie={state.tone === 'zombie'}>{state.label}</span></td>
                <td class="num cpu">
                  <span class="cpu-value">{proc.cpu_percent.toFixed(1)}%</span>
                  <span class="mini-bar"><i style="width: {Math.min(proc.cpu_percent, 100)}%"></i></span>
                </td>
                <td class="num mem">
                  <span>{formatBytes(proc.mem_rss_bytes)}</span>
                  {#if totalMem}<span class="pct">{memShare(proc.mem_rss_bytes).toFixed(1)}%</span>{/if}
                  <span class="mini-bar mem"><i style="width: {Math.min(memShare(proc.mem_rss_bytes), 100)}%"></i></span>
                </td>
                <td class="num">{proc.threads ?? '—'}</td>
                <td class="num">{age(proc.elapsed_seconds)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <p class="footnote">
        CPU is the share of all cores (one saturated core on four cores reads 25%); threads are
        listed separately. The agent reports the top consumers plus stopped, zombie and disk-sleep
        rows; the tiles count the whole machine.
      </p>
    {/if}
  </article>

  {#if selected}
    {@const state = stateInfo(selected.state)}
    <article class="card detail" aria-label="Process detail">
      <header class="card-head">
        <div class="head-title">
          <h2>{selected.name}</h2>
          <span class="hint">PID {selected.pid}</span>
        </div>
        <button class="ghost" on:click={() => (selectedPid = null)}>Close</button>
      </header>
      <p class="command-full mono">{selected.command || selected.name}</p>
      <dl class="facts">
        <div><dt>State</dt><dd><span class="state" class:running={state.tone === 'running'} class:sleeping={state.tone === 'sleeping'} class:disk={state.tone === 'disk'} class:stopped={state.tone === 'stopped'} class:zombie={state.tone === 'zombie'}>{state.label}</span> · {state.meaning}</dd></div>
        <div><dt>User</dt><dd>{selected.user || '—'}</dd></div>
        <div><dt>PID / PPID</dt><dd class="mono">{selected.pid} / {selected.ppid ?? '—'}</dd></div>
        <div><dt>Threads</dt><dd>{selected.threads ?? '—'}</dd></div>
        <div><dt>CPU share</dt><dd>{selected.cpu_percent.toFixed(1)}% of all cores</dd></div>
        <div><dt>Resident memory</dt><dd>{formatBytes(selected.mem_rss_bytes)} ({memShare(selected.mem_rss_bytes).toFixed(1)}% of RAM)</dd></div>
        <div><dt>Virtual memory</dt><dd>{selected.mem_virt_bytes ? formatBytes(selected.mem_virt_bytes) : '—'}</dd></div>
        <div><dt>Age</dt><dd>{age(selected.elapsed_seconds)}</dd></div>
      </dl>
    </article>
  {/if}
</section>

<style>
  .processes { display: grid; gap: 0.75rem; }

  .status-strip {
    display: flex;
    flex-wrap: wrap;
    gap: 0.3rem 1rem;
    color: var(--text-faint);
    font-size: 0.72rem;
  }
  .warn-text { color: var(--warn); }

  .summary {
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    gap: 0.5rem;
  }
  .tile {
    display: grid;
    gap: 0.1rem;
    padding: 0.55rem 0.65rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }
  .tile.alert { border-color: rgba(248, 113, 113, 0.45); }
  .tile .label { color: var(--text-faint); font-size: 0.66rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .tile .value { color: var(--text-bright); font-size: 1.25rem; font-variant-numeric: tabular-nums; }
  .tile.alert .value { color: var(--crit); }
  .tile .sub { color: var(--text-faint); font-size: 0.62rem; }

  .alert-strip {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.55rem 0.75rem;
    border: 1px solid rgba(248, 113, 113, 0.4);
    border-radius: var(--radius-sm);
    background: var(--crit-dim);
    color: var(--crit);
    font-size: 0.76rem;
  }
  .alert-strip button {
    flex: none;
    padding: 0.2rem 0.55rem;
    border: 1px solid rgba(248, 113, 113, 0.45);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--crit);
    font-size: 0.7rem;
    font-weight: 600;
    cursor: pointer;
  }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.6rem; padding: 0.65rem 0.8rem; border-bottom: 1px solid var(--border); }
  .head-title { display: flex; align-items: baseline; gap: 0.5rem; flex-wrap: wrap; min-width: 0; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .hint { color: var(--text-faint); font-size: 0.7rem; }

  .search {
    width: 17rem;
    max-width: 45vw;
    padding: 0.3rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-elevated);
    color: var(--text);
    font-size: 0.74rem;
  }

  .filters { display: flex; flex-wrap: wrap; gap: 0.3rem; padding: 0.5rem 0.8rem; border-bottom: 1px solid var(--border); }
  .filters button {
    padding: 0.2rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-faint);
    font-size: 0.68rem;
    cursor: pointer;
  }
  .filters button.active { border-color: var(--accent-border); background: var(--accent-dim); color: var(--accent); }

  .table-scroll { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 0.75rem; }
  th {
    padding: 0.45rem 0.6rem;
    text-align: left;
    color: var(--text-faint);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
  }
  th button { padding: 0; border: 0; background: transparent; color: inherit; font: inherit; letter-spacing: inherit; text-transform: inherit; cursor: pointer; }
  th.active { color: var(--accent); }
  td { padding: 0.42rem 0.6rem; border-bottom: 1px solid var(--border); vertical-align: middle; color: var(--text); }
  tbody tr { cursor: pointer; }
  tbody tr:hover { background: var(--card-hover); }
  tbody tr.selected { background: var(--accent-dim); }
  .num { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
  .mono { font-family: var(--mono); }

  .pid { white-space: nowrap; }
  .pid .mono { color: var(--text-muted); }
  .ppid { display: block; color: var(--text-faint); font-size: 0.62rem; }
  .proc { max-width: 26rem; }
  .name { display: block; color: var(--text-bright); font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .command { display: block; color: var(--text-faint); font-family: var(--mono); font-size: 0.66rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .user { color: var(--text-muted); white-space: nowrap; }

  .state {
    display: inline-block;
    padding: 0.05rem 0.4rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-pill);
    font-size: 0.64rem;
    white-space: nowrap;
  }
  .state.running { color: var(--ok); border-color: rgba(52, 211, 153, 0.35); }
  .state.sleeping { color: var(--text-muted); }
  .state.disk { color: var(--warn); border-color: rgba(251, 191, 36, 0.35); }
  .state.stopped, .state.zombie { color: var(--crit); border-color: rgba(248, 113, 113, 0.4); }

  .cpu-value { display: block; color: var(--text); }
  .mini-bar { display: block; height: 3px; width: 4.5rem; margin: 0.15rem 0 0 auto; background: var(--card-inset); }
  .mini-bar i { display: block; height: 100%; background: var(--accent); }
  .mini-bar.mem i { background: var(--series-1); }
  .mem .pct { display: block; color: var(--text-faint); font-size: 0.64rem; }

  .muted { color: var(--text-faint); font-size: 0.78rem; }
  .pad { padding: 1rem 0.8rem; }
  .footnote { margin: 0; padding: 0.55rem 0.8rem 0.7rem; color: var(--text-faint); font-size: 0.68rem; line-height: 1.5; }

  .detail { padding: 0; }
  .command-full {
    margin: 0;
    padding: 0.55rem 0.8rem;
    border-bottom: 1px solid var(--border);
    color: var(--text);
    font-size: 0.72rem;
    line-height: 1.5;
    word-break: break-all;
    background: var(--card-inset);
  }
  .facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 0.5rem 1rem; margin: 0; padding: 0.7rem 0.8rem; }
  .facts div { display: grid; gap: 0.1rem; }
  .facts dt { color: var(--text-faint); font-size: 0.64rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .facts dd { margin: 0; color: var(--text-muted); font-size: 0.74rem; line-height: 1.5; }

  .ghost {
    padding: 0.25rem 0.6rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    font-size: 0.7rem;
    cursor: pointer;
  }
  .ghost:hover { border-color: var(--accent-border); color: var(--text); }

  @media (max-width: 1100px) {
    .summary { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  }
  @media (max-width: 620px) {
    .summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .search { width: 100%; max-width: none; }
    .card-head { flex-wrap: wrap; }
  }
</style>
