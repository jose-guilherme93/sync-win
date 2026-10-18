<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { formatBytes, formatRelative, formatAbsolute } from '../../lib/format'
  import { portLabel, shortId, stateTone, statsFromMessage, type DockerContainer, type DockerStats } from '../../lib/docker'
  import { runDockerCommand } from '../../lib/docker-client'
  import StatCard from '../ui/StatCard.svelte'
  import StatusPill from '../ui/StatusPill.svelte'
  import Icon from '../ui/Icon.svelte'

  export let container: DockerContainer
  export let deviceId: string
  export let authHeaders: Record<string, string> = {}
  export let busy = false
  export let onClose: () => void
  export let onAction: (action: 'start' | 'stop' | 'restart' | 'kill' | 'remove', container: DockerContainer) => void

  type Tab = 'overview' | 'resources' | 'logs' | 'shell'

  let tab: Tab = 'overview'
  let shownId = ''

  // Resource stats. One request is a full command round-trip through the agent,
  // so the interval is deliberately slower than the telemetry poll and never
  // overlaps itself.
  let stats: DockerStats | null = null
  let statsError = ''
  let statsLoading = false
  let statsTimer: ReturnType<typeof setInterval> | null = null
  let statsRequest = 0
  let history = { cpu: [] as number[], mem: [] as number[], netRx: [] as number[], netTx: [] as number[] }

  let logs = ''
  let logsError = ''
  let logsLoading = false
  let logsRequest = 0
  let tail = 300
  let copied = ''

  let execCommand = ''
  let execOutput = ''
  let execRunning = false
  let closeButton: HTMLButtonElement | null = null

  // Escape closes the drawer, unless a confirmation dialog is above it: that
  // dialog owns Escape and dismissing both at once would lose the user's place.
  function onKeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape') return
    if (document.querySelector('[role="alertdialog"]')) return
    onClose()
  }

  onMount(() => {
    document.addEventListener('keydown', onKeydown)
    closeButton?.focus()
    return () => document.removeEventListener('keydown', onKeydown)
  })

  // The drawer is reused when another row is opened, so every per-container
  // buffer must reset or the new container would inherit the old one's logs.
  $: if (container.id !== shownId) {
    shownId = container.id
    tab = 'overview'
    stopStats()
    stats = null
    statsError = ''
    history = { cpu: [], mem: [], netRx: [], netTx: [] }
    logs = ''
    logsError = ''
    logsLoading = false
    execCommand = ''
    execOutput = ''
  }

  // A container can stop while its Resources tab is open (another client, a
  // crash). Stop asking for stats that can no longer exist.
  $: if (container.state !== 'running') stopStats()

  onDestroy(stopStats)

  function stopStats() {
    if (statsTimer) {
      clearInterval(statsTimer)
      statsTimer = null
    }
  }

  function startStats() {
    stopStats()
    if (container.state !== 'running') return
    void loadStats()
    statsTimer = setInterval(() => void loadStats(), 5_000)
  }

  async function loadStats() {
    if (statsLoading || container.state !== 'running') return
    const id = container.id
    const request = ++statsRequest
    statsLoading = true
    try {
      const result = await runDockerCommand(deviceId, 'stats', id, '', {
        headers: authHeaders,
        timeoutMs: 25_000
      })
      if (request !== statsRequest || id !== container.id) return
      const next = statsFromMessage(result.message)
      stats = next
      statsError = ''
      history = {
        cpu: [...history.cpu, next.cpu_usage_percent].slice(-30),
        mem: [...history.mem, next.memory_percent].slice(-30),
        netRx: [...history.netRx, next.network_rx_bytes].slice(-30),
        netTx: [...history.netTx, next.network_tx_bytes].slice(-30)
      }
    } catch (e: any) {
      if (request === statsRequest) statsError = e?.message || 'Could not read container stats'
    } finally {
      if (request === statsRequest) statsLoading = false
    }
  }

  function setTab(next: Tab) {
    tab = next
    if (next === 'resources') startStats()
    else stopStats()
    if (next === 'logs' && !logs && !logsLoading) void loadLogs()
  }

  async function loadLogs() {
    const id = container.id
    const request = ++logsRequest
    logsLoading = true
    logsError = ''
    try {
      const result = await runDockerCommand(deviceId, 'logs', id, String(tail), {
        headers: authHeaders,
        timeoutMs: 35_000
      })
      if (request !== logsRequest) return
      logs = result.message || ''
    } catch (e: any) {
      if (request === logsRequest) {
        logs = ''
        logsError = e?.message || 'Could not read container logs'
      }
    } finally {
      if (request === logsRequest) logsLoading = false
    }
  }

  async function runExec() {
    const parts = execCommand.trim().split(/\s+/).filter(Boolean)
    if (parts.length === 0 || execRunning) return
    execRunning = true
    execOutput = ''
    try {
      const result = await runDockerCommand(deviceId, 'exec', container.id, JSON.stringify(parts), {
        headers: authHeaders,
        timeoutMs: 45_000
      })
      execOutput = result.message || '(no output)'
    } catch (e: any) {
      execOutput = `Error: ${e?.message || 'command failed'}`
    } finally {
      execRunning = false
    }
  }

  async function copy(text: string, key: string) {
    if (!text) return
    try {
      await navigator.clipboard.writeText(text)
      copied = key
      setTimeout(() => {
        if (copied === key) copied = ''
      }, 1_500)
    } catch {
      // Clipboard access can be denied; the text stays selectable on screen.
    }
  }

  function minOf(values: number[]): number | null {
    return values.length ? Math.min(...values) : null
  }

  function maxOf(values: number[]): number | null {
    return values.length ? Math.max(...values) : null
  }

  $: createdRelative = formatRelative(container.created_at ? container.created_at * 1000 : null)
  $: createdAbsolute = formatAbsolute(container.created_at ? container.created_at * 1000 : null)
  $: isRunning = container.state === 'running'
</script>

<div class="backdrop" role="presentation" on:click={onClose}></div>
<div class="drawer" aria-label="Container details" role="dialog" aria-modal="true">
  <header class="drawer-head">
    <div class="ident">
      <div class="title-row">
        <StatusPill label={container.state} tone={stateTone(container.state)} />
        <h2>{container.name}</h2>
      </div>
      <span class="image mono" title={container.image}>{container.image}</span>
    </div>
    <button class="close" bind:this={closeButton} on:click={onClose} aria-label="Close details"><Icon name="x" size={16} /></button>
  </header>

  <nav class="tabs" aria-label="Container sections">
    <button class:active={tab === 'overview'} on:click={() => setTab('overview')}>Overview</button>
    <button class:active={tab === 'resources'} on:click={() => setTab('resources')}>Resources</button>
    <button class:active={tab === 'logs'} on:click={() => setTab('logs')}>Logs</button>
    <button class:active={tab === 'shell'} on:click={() => setTab('shell')}>Shell</button>
  </nav>

  <div class="body">
    {#if tab === 'overview'}
      <dl class="facts">
        <div><dt>State</dt><dd>{container.state}</dd></div>
        <div><dt>Status</dt><dd>{container.status || '—'}</dd></div>
        <div>
          <dt>Created</dt>
          <dd title={createdAbsolute || undefined}>{container.created_at ? createdRelative : 'not reported'}</dd>
        </div>
        <div><dt>ID</dt><dd class="mono">{shortId(container.id)}</dd></div>
      </dl>

      <section class="block">
        <h3>Published ports</h3>
        {#if container.ports.length === 0}
          <p class="subtle">No published ports. The container is reachable only from other containers.</p>
        {:else}
          <ul class="chips">
            {#each container.ports as port (port.private_port + '/' + port.type + '/' + (port.public_port || 0))}
              <li class="port-chip mono" title={port.ip ? `Bound on ${port.ip}` : undefined}>{portLabel(port)}</li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="block">
        <h3>Mounts</h3>
        {#if container.mounts.length === 0}
          <p class="subtle">No mounts. The container filesystem is private to the container.</p>
        {:else}
          <ul class="mounts">
            {#each container.mounts as mount (mount.destination + mount.source)}
              <li>
                <div class="mount-head">
                  <span class="mount-type">{mount.type}</span>
                  <span class="mount-mode">{mount.rw ? 'rw' : 'ro'}</span>
                </div>
                <div class="mount-paths mono">
                  {#if mount.source}<span class="src">{mount.source}</span><span class="arrow">→</span>{/if}
                  <span class="dest">{mount.destination}</span>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {:else if tab === 'resources'}
      {#if !isRunning}
        <p class="note">Resource usage is only available while the container is running.</p>
      {:else if statsError}
        <div class="inline-error" role="alert">
          {statsError}
          <button on:click={() => void loadStats()} disabled={statsLoading}>Retry</button>
        </div>
      {:else if stats}
        <div class="stats-grid">
          <StatCard
            label="CPU"
            value={stats.cpu_usage_percent.toFixed(1)}
            unit="%"
            series={history.cpu}
            min={minOf(history.cpu)}
            max={maxOf(history.cpu)}
          />
          <StatCard
            label="Memory"
            value={stats.memory_percent.toFixed(1)}
            unit="%"
            sub={`${formatBytes(stats.memory_usage_bytes)} / ${formatBytes(stats.memory_limit_bytes)}`}
            series={history.mem}
            seriesColor="var(--series-1)"
            min={minOf(history.mem)}
            max={maxOf(history.mem)}
          />
          <StatCard label="Network in" value={formatBytes(stats.network_rx_bytes)} sub="since container start" />
          <StatCard label="Network out" value={formatBytes(stats.network_tx_bytes)} sub="since container start" />
          <StatCard label="Block read" value={formatBytes(stats.block_read_bytes)} sub="since container start" />
          <StatCard label="Block write" value={formatBytes(stats.block_write_bytes)} sub="since container start" />
          <StatCard label="Processes" value={stats.pids} />
        </div>
        <div class="stats-foot">
          <span class="subtle">Sampled every 5s while this tab is open.</span>
          <button class="btn-ghost" on:click={() => void loadStats()} disabled={statsLoading}>
            <Icon name="refresh" size={13} /> {statsLoading ? 'Reading…' : 'Read now'}
          </button>
        </div>
      {:else}
        <div class="stats-placeholder">
          <p class="subtle">{statsLoading ? 'Reading resource usage from the device…' : 'No sample yet.'}</p>
        </div>
      {/if}
    {:else if tab === 'logs'}
      <div class="log-toolbar">
        <label class="tail">
          <span class="label">Lines</span>
          <select bind:value={tail} on:change={() => void loadLogs()} disabled={logsLoading}>
            <option value={100}>100</option>
            <option value={300}>300</option>
            <option value={1000}>1000</option>
            <option value={2000}>2000</option>
          </select>
        </label>
        <div class="toolbar-actions">
          <button class="btn-ghost" on:click={() => void copy(logs, 'logs')} disabled={!logs}>
            {copied === 'logs' ? 'Copied' : 'Copy'}
          </button>
          <button class="btn-ghost" on:click={() => void loadLogs()} disabled={logsLoading}>
            <Icon name="refresh" size={13} /> {logsLoading ? 'Reading…' : 'Refresh'}
          </button>
        </div>
      </div>
      {#if logsError}
        <div class="inline-error" role="alert">
          {logsError}
          <button on:click={() => void loadLogs()}>Retry</button>
        </div>
      {:else if logsLoading && !logs}
        <p class="note">Asking the device for the last {tail} lines…</p>
      {:else}
        <pre class="output">{logs || 'No output in the selected window.'}</pre>
      {/if}
    {:else if tab === 'shell'}
      {#if !isRunning}
        <p class="note">Start the container to run a command inside it.</p>
      {:else}
        <p class="note">
          Runs <span class="mono">docker exec</span> on the device. Each command is independent —
          no shell session is kept, and pipes or redirection are not interpreted.
        </p>
      {/if}
      <form class="exec" on:submit|preventDefault={runExec}>
        <input
          bind:value={execCommand}
          placeholder="ls -la /app"
          aria-label="Command to run in the container"
          disabled={execRunning || !isRunning}
          autocomplete="off"
          spellcheck="false"
        />
        <button class="btn-primary" type="submit" disabled={execRunning || !isRunning || !execCommand.trim()}>
          {execRunning ? 'Running…' : 'Run'}
        </button>
      </form>
      {#if execOutput}
        <div class="output-wrap">
          <button class="copy-floating" on:click={() => void copy(execOutput, 'exec')}>
            {copied === 'exec' ? 'Copied' : 'Copy'}
          </button>
          <pre class="output">{execOutput}</pre>
        </div>
      {/if}
    {/if}
  </div>

  <footer class="actions">
    {#if isRunning}
      <button class="btn-ghost" disabled={busy} on:click={() => onAction('restart', container)}>
        {busy ? 'Working…' : 'Restart'}
      </button>
      <button class="btn-ghost warn" disabled={busy} on:click={() => onAction('stop', container)}>Stop</button>
    {:else if container.state === 'paused' || container.state === 'restarting'}
      <!-- Docker cannot start or restart a paused/transitional container; the
           honest action is to say so instead of offering a button that fails. -->
      <span class="footer-note">This container must finish or be unpaused on the device.</span>
    {:else}
      <button class="btn-primary" disabled={busy} on:click={() => onAction('start', container)}>
        {busy ? 'Working…' : 'Start'}
      </button>
    {/if}
    <button class="btn-ghost danger" disabled={busy} on:click={() => onAction('remove', container)}>Remove</button>
  </footer>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: rgba(0, 0, 0, 0.5);
  }

  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    z-index: 41;
    display: flex;
    flex-direction: column;
    width: min(470px, 94vw);
    height: 100vh;
    background: var(--bg-elevated);
    border-left: 1px solid var(--border);
    box-shadow: var(--shadow-pop);
    animation: slide-in 180ms ease;
  }

  @keyframes slide-in {
    from { transform: translateX(16px); opacity: 0; }
    to { transform: none; opacity: 1; }
  }

  .drawer-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 1rem 1rem 0.75rem;
    border-bottom: 1px solid var(--border);
  }

  .ident { display: grid; gap: 0.3rem; min-width: 0; }
  .title-row { display: flex; align-items: center; gap: 0.55rem; flex-wrap: wrap; }
  .title-row h2 {
    margin: 0;
    color: var(--text-bright);
    font-size: 1.02rem;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .image { color: var(--text-faint); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .close {
    border: 0;
    background: transparent;
    color: var(--text-muted);
    padding: 0.2rem;
    cursor: pointer;
    border-radius: var(--radius-sm);
  }
  .close:hover { color: var(--text); background: var(--card-hover); }

  .tabs {
    display: flex;
    gap: 0.2rem;
    padding: 0.45rem 0.85rem;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
  }
  .tabs button {
    border: 0;
    border-bottom: 2px solid transparent;
    background: transparent;
    color: var(--text-faint);
    padding: 0.4rem 0.5rem;
    font-size: 0.76rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .tabs button:hover { color: var(--text); }
  .tabs button.active { color: var(--accent); border-bottom-color: var(--accent); }

  .body {
    flex: 1;
    overflow-y: auto;
    padding: 0.9rem 1rem 1.2rem;
    display: grid;
    align-content: start;
    gap: 0.9rem;
  }

  .facts { display: grid; gap: 0.55rem; margin: 0; }
  .facts > div { display: grid; grid-template-columns: 5.5rem 1fr; gap: 0.5rem; align-items: baseline; }
  .facts dt { color: var(--text-faint); font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.04em; }
  .facts dd { margin: 0; color: var(--text); font-size: 0.8rem; overflow-wrap: anywhere; }

  .block { display: grid; gap: 0.45rem; }
  .block h3 {
    margin: 0;
    color: var(--text-faint);
    font-size: 0.7rem;
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }

  .subtle { color: var(--text-faint); font-size: 0.76rem; line-height: 1.5; margin: 0; }
  .note { margin: 0; color: var(--text-muted); font-size: 0.76rem; line-height: 1.5; }

  .chips { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 0.3rem; }
  .port-chip {
    padding: 0.15rem 0.45rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-pill);
    background: var(--card-inset);
    color: var(--text);
    font-size: 0.68rem;
  }

  .mounts { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.4rem; }
  .mounts li {
    padding: 0.45rem 0.55rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card-inset);
    display: grid;
    gap: 0.2rem;
  }
  .mount-head { display: flex; align-items: center; gap: 0.4rem; }
  .mount-type, .mount-mode {
    font-size: 0.62rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .mount-mode { margin-left: auto; color: var(--text-muted); }
  .mount-paths { display: flex; gap: 0.4rem; flex-wrap: wrap; color: var(--text); font-size: 0.7rem; overflow-wrap: anywhere; }
  .arrow { color: var(--text-faint); }
  .dest { color: var(--accent); }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 0.55rem;
  }
  .stats-foot { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; flex-wrap: wrap; }
  .stats-placeholder { display: grid; gap: 0.5rem; }

  .log-toolbar { display: flex; align-items: end; justify-content: space-between; gap: 0.6rem; flex-wrap: wrap; }
  .tail { display: grid; gap: 0.2rem; }
  .toolbar-actions { display: flex; gap: 0.35rem; }
  select {
    padding: 0.3rem 0.45rem;
    background: var(--card-inset);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    font-size: 0.75rem;
  }

  .output {
    margin: 0;
    padding: 0.6rem 0.7rem;
    max-height: 52vh;
    overflow: auto;
    background: #08080a;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-family: var(--mono);
    font-size: 0.7rem;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .output-wrap { position: relative; }
  .copy-floating {
    position: absolute;
    top: 0.35rem;
    right: 0.35rem;
    padding: 0.15rem 0.45rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--bg-elevated);
    color: var(--text-muted);
    font-size: 0.65rem;
    cursor: pointer;
  }

  .exec { display: flex; gap: 0.4rem; }
  .exec input {
    flex: 1;
    min-width: 0;
    padding: 0.4rem 0.55rem;
    background: var(--card-inset);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-family: var(--mono);
    font-size: 0.75rem;
  }
  .exec input:focus { outline: none; border-color: var(--accent-border); }

  .inline-error {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.5rem 0.65rem;
    border: 1px solid rgba(248, 113, 113, 0.35);
    border-radius: var(--radius-sm);
    background: var(--crit-dim);
    color: var(--crit);
    font-size: 0.75rem;
  }
  .inline-error button {
    margin-left: auto;
    border: 1px solid rgba(248, 113, 113, 0.4);
    background: transparent;
    color: var(--crit);
    border-radius: var(--radius-sm);
    padding: 0.15rem 0.5rem;
    font-size: 0.7rem;
    cursor: pointer;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 0.45rem;
    padding: 0.7rem 1rem;
    border-top: 1px solid var(--border);
    background: var(--bg-elevated);
  }
  .footer-note { color: var(--text-faint); font-size: 0.7rem; margin-right: auto; }

  button.btn-ghost,
  button.btn-primary {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.38rem 0.7rem;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    background: transparent;
    color: var(--text);
    font-size: 0.75rem;
    font-weight: 600;
    cursor: pointer;
  }
  button.btn-ghost:hover:not(:disabled) { background: var(--card-hover); }
  button.btn-ghost.warn { color: var(--warn); border-color: rgba(251, 191, 36, 0.35); }
  button.btn-ghost.danger { color: var(--crit); border-color: rgba(248, 113, 113, 0.35); }
  button.btn-primary {
    background: var(--accent-dim);
    border-color: var(--accent-border);
    color: var(--accent);
  }
  button:disabled { opacity: 0.5; cursor: default; }

  @media (max-width: 620px) {
    .drawer { width: 100vw; }
    .stats-grid { grid-template-columns: 1fr 1fr; }
  }
</style>
