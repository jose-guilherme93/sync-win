<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { formatAbsolute, formatBytes, formatRate, formatRelative } from '../../lib/format'
  import {
    deriveRates,
    filterLogLines,
    normalizeLogText,
    parseContainerInspect,
    portLabel,
    shortId,
    stateTone,
    statsFromMessage,
    stripLogTimestamps,
    type DockerAction,
    type DockerContainer,
    type DockerContainerDetail,
    type DockerRates,
    type DockerStats
  } from '../../lib/docker'
  import { runDockerCommand } from '../../lib/docker-client'
  import Icon from '../ui/Icon.svelte'
  import MiniSparkline from '../ui/MiniSparkline.svelte'
  import StatusPill from '../ui/StatusPill.svelte'

  export let container: DockerContainer
  export let deviceId: string
  export let authHeaders: Record<string, string> = {}
  export let busy = false
  export let onClose: () => void
  export let onAction: (action: DockerAction, container: DockerContainer) => void

  type Tab = 'overview' | 'stats' | 'logs' | 'terminal' | 'config'
  type History = { cpu: number[]; mem: number[]; rx: number[]; tx: number[]; read: number[]; write: number[] }

  let tab: Tab = 'overview'
  let visible = true
  let destroyed = false

  // Resource metrics. Each sample is one command round-trip, so the loop runs
  // only while the Stats tab is open and visible, with one request in flight.
  let stats: DockerStats | null = null
  let statsError = ''
  let statsLoading = false
  let statsTimer: ReturnType<typeof setTimeout> | null = null
  let statsRequest = 0
  let previousStats: DockerStats | null = null
  let previousAt = 0
  let rates: DockerRates = { rx_rate: 0, tx_rate: 0, read_rate: 0, write_rate: 0 }
  let history: History = { cpu: [], mem: [], rx: [], tx: [], read: [], write: [] }
  let lastState = container.state

  // Inspect detail (configuration, networks, health, labels).
  let detail: DockerContainerDetail | null = null
  let inspectError = ''
  let inspectLoading = false
  let inspectRequest = 0

  // Logs. The tail is polled while the tab is open; replacing the snapshot is
  // what keeps duplicate lines out of the view.
  let logText = ''
  let logError = ''
  let logLoading = false
  let logRequest = 0
  let logTimer: ReturnType<typeof setInterval> | null = null
  let logLive = true
  let logEl: HTMLPreElement | null = null
  let tail = 300
  let logQuery = ''
  let wrap = true
  let showTimestamps = false

  // Terminal. Commands are one-shot docker exec calls, not a shell session.
  let execCommand = ''
  let execOutput = ''
  let execRunning = false

  let copied = ''
  let copiedTimer: ReturnType<typeof setTimeout> | null = null

  let createdRelative = ''
  let createdAbsolute = ''

  $: isRunning = container.state === 'running'
  $: isPaused = container.state === 'paused'
  $: createdRelative = container.created_at ? formatRelative(container.created_at * 1000) : ''
  $: createdAbsolute = container.created_at ? formatAbsolute(container.created_at * 1000) : ''
  $: displayedLogs = filterLogLines(showTimestamps ? logText : stripLogTimestamps(logText), logQuery)
  $: logLineCount = displayedLogs ? displayedLogs.split('\n').filter(Boolean).length : 0
  $: detailMounts = detail?.mounts?.length ? detail.mounts : container.mounts

  // A start/stop from the panel or another client changes the container state.
  // Stop metrics that cannot exist and refresh the configuration once.
  $: if (container.state !== lastState) {
    lastState = container.state
    if (statsTabActive()) {
      if (container.state === 'running' && !statsError) startStats()
      else stopStats()
    }
    if (container.state !== 'running') statsError = ''
    void loadInspect()
  }

  function statsTabActive(): boolean {
    return tab === 'overview' || tab === 'stats'
  }

  // Escape closes the side panel, unless a confirmation dialog is above it:
  // that dialog owns Escape and dismissing both at once would lose the user's
  // place.
  function onKeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape') return
    if (document.querySelector('[role="alertdialog"]')) return
    onClose()
  }

  onMount(() => {
    visible = !document.hidden
    document.addEventListener('visibilitychange', onVisibility)
    document.addEventListener('keydown', onKeydown)
    void loadInspect()
    if (isRunning) startStats()
    return () => {
      destroyed = true
      document.removeEventListener('visibilitychange', onVisibility)
      document.removeEventListener('keydown', onKeydown)
      statsRequest++
      logRequest++
      inspectRequest++
      stopStats()
      stopLogsPoll()
      if (copiedTimer) clearTimeout(copiedTimer)
    }
  })

  function onVisibility() {
    visible = !document.hidden
    if (!visible) {
      stopStats()
      stopLogsPoll()
      return
    }
    if (statsTabActive() && isRunning && !statsError) startStats()
    if (tab === 'logs' && logLive) startLogsPoll()
  }

  function stopStats() {
    if (statsTimer) {
      clearTimeout(statsTimer)
      statsTimer = null
    }
  }

  function startStats() {
    stopStats()
    if (!isRunning || !visible) return
    // After a long pause the previous sample is too old to produce an honest
    // rate: the first new sample starts the window again.
    if (previousAt && Date.now() - previousAt > 10_000) {
      previousStats = null
      previousAt = 0
      rates = { rx_rate: 0, tx_rate: 0, read_rate: 0, write_rate: 0 }
    }
    void loadStats()
  }

  async function loadStats() {
    if (statsLoading || !isRunning || !visible) return
    const id = container.id
    const request = ++statsRequest
    statsLoading = true
    try {
      const result = await runDockerCommand(deviceId, 'stats', id, '', {
        headers: authHeaders,
        timeoutMs: 20_000
      })
      if (request !== statsRequest || id !== container.id) return
      const next = statsFromMessage(result.message)
      const at = Date.now()
      rates = deriveRates(previousStats, next, previousAt ? at - previousAt : 0)
      previousStats = next
      previousAt = at
      stats = next
      statsError = ''
      history = {
        cpu: [...history.cpu, next.cpu_usage_percent].slice(-60),
        mem: [...history.mem, next.memory_percent].slice(-60),
        rx: [...history.rx, rates.rx_rate].slice(-60),
        tx: [...history.tx, rates.tx_rate].slice(-60),
        read: [...history.read, rates.read_rate].slice(-60),
        write: [...history.write, rates.write_rate].slice(-60)
      }
    } catch (e: any) {
      if (request === statsRequest) {
        statsError = e?.message || 'Could not read container stats'
        stopStats()
      }
    } finally {
      if (request === statsRequest) {
        statsLoading = false
        if (!destroyed && statsTabActive() && isRunning && visible && !statsError) {
          statsTimer = setTimeout(() => void loadStats(), 500)
        }
      }
    }
  }

  async function loadInspect() {
    const id = container.id
    const request = ++inspectRequest
    inspectLoading = true
    try {
      const result = await runDockerCommand(deviceId, 'inspect', id, '', {
        headers: authHeaders,
        timeoutMs: 20_000
      })
      if (request !== inspectRequest) return
      detail = parseContainerInspect(result.message)
      inspectError = ''
    } catch (e: any) {
      if (request === inspectRequest) inspectError = e?.message || 'Could not read container details'
    } finally {
      if (request === inspectRequest) inspectLoading = false
    }
  }

  function setTab(next: Tab) {
    tab = next
    if (next === 'overview' || next === 'stats') startStats()
    else stopStats()
    if (next === 'logs') {
      if (logLive) startLogsPoll()
      else if (!logText && !logLoading) void loadLogs()
    } else {
      stopLogsPoll()
    }
  }

  function stopLogsPoll() {
    if (logTimer) {
      clearInterval(logTimer)
      logTimer = null
    }
  }

  function startLogsPoll() {
    stopLogsPoll()
    if (!visible) return
    void loadLogs()
    logTimer = setInterval(() => void loadLogs(), 3_000)
  }

  function isLogPinned(): boolean {
    if (!logEl) return true
    return logEl.scrollTop + logEl.clientHeight >= logEl.scrollHeight - 32
  }

  function scrollLogsToBottom() {
    if (!logEl) return
    logEl.scrollTop = logEl.scrollHeight
  }

  async function loadLogs() {
    if (logLoading) return
    const id = container.id
    const request = ++logRequest
    logLoading = true
    const pinned = isLogPinned()
    try {
      const result = await runDockerCommand(deviceId, 'logs', id, String(tail), {
        headers: authHeaders,
        timeoutMs: 35_000
      })
      if (request !== logRequest) return
      logText = normalizeLogText(result.message || '')
      logError = ''
      await tick()
      if (pinned) scrollLogsToBottom()
    } catch (e: any) {
      if (request === logRequest) {
        logError = e?.message || 'Could not read container logs'
        stopLogsPoll()
      }
    } finally {
      if (request === logRequest) logLoading = false
    }
  }

  function setLogLive(next: boolean) {
    logLive = next
    if (next) startLogsPoll()
    else stopLogsPoll()
  }

  function changeTail() {
    void loadLogs()
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
      if (copiedTimer) clearTimeout(copiedTimer)
      copiedTimer = setTimeout(() => {
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

  function healthTone(status: string): 'ok' | 'warn' | 'crit' | 'idle' {
    if (status === 'healthy') return 'ok'
    if (status === 'starting') return 'warn'
    if (status === 'unhealthy') return 'crit'
    return 'idle'
  }

  function shortImage(image: string): string {
    return image.split('@')[0]
  }

  function cpus(nano: number): string {
    if (!nano) return 'unlimited'
    return (nano / 1_000_000_000).toFixed(2).replace(/\.?0+$/, '') + ' CPU'
  }

  $: commandText = [...(detail?.entrypoint || []), ...(detail?.command || [])].join(' ')
</script>

<div class="detail-panel">
  <header class="detail-head">
    <div class="identity">
      <StatusPill label={container.state} tone={stateTone(container.state)} />
      <span class="panel-name mono" title={container.name}>{container.name}</span>
      <span class="id mono" title={container.id}>{shortId(container.id)}</span>
    </div>
    <div class="detail-actions">
      {#if isRunning}
        <button class="btn-ghost" disabled={busy} on:click={() => onAction('pause', container)}>Pause</button>
        <button class="btn-ghost" disabled={busy} on:click={() => onAction('restart', container)}>Restart</button>
        <button class="btn-ghost warn" disabled={busy} on:click={() => onAction('stop', container)}>Stop</button>
      {:else if isPaused}
        <button class="btn-primary" disabled={busy} on:click={() => onAction('unpause', container)}>Resume</button>
        <button class="btn-ghost warn" disabled={busy} on:click={() => onAction('stop', container)}>Stop</button>
      {:else if container.state !== 'restarting'}
        <button class="btn-primary" disabled={busy} on:click={() => onAction('start', container)}>Start</button>
      {/if}
      {#if isRunning}<button class="btn-ghost danger" disabled={busy} on:click={() => onAction('kill', container)}>Kill</button>{/if}
      <button class="btn-ghost danger" disabled={busy} on:click={() => onAction('remove', container)}>Remove</button>
      <button class="btn-icon" on:click={onClose} aria-label="Close container details"><Icon name="x" size={15} /></button>
    </div>
  </header>

  <nav class="tabs" aria-label="Container sections">
    <button class:active={tab === 'overview'} on:click={() => setTab('overview')}>Overview</button>
    <button class:active={tab === 'stats'} on:click={() => setTab('stats')}>Stats</button>
    <button class:active={tab === 'logs'} on:click={() => setTab('logs')}>Logs</button>
    <button class:active={tab === 'terminal'} on:click={() => setTab('terminal')}>Terminal</button>
    <button class:active={tab === 'config'} on:click={() => setTab('config')}>Config</button>
  </nav>

  <div class="tab-body">
    {#if tab === 'overview'}
      {#if isRunning}
        <div class="quick-metrics">
          <article class="quick">
            <span class="label">CPU</span>
            <span class="quick-value">{stats ? stats.cpu_usage_percent.toFixed(1) : '—'}<small>%</small></span>
            <div class="quick-spark">
              {#if history.cpu.length >= 2}<MiniSparkline series={history.cpu} height={26} />{/if}
            </div>
          </article>
          <article class="quick">
            <span class="label">Memory</span>
            <span class="quick-value">{stats ? stats.memory_percent.toFixed(1) : '—'}<small>%</small></span>
            <span class="quick-sub">{stats ? formatBytes(stats.memory_usage_bytes) : 'waiting'}</span>
            <div class="quick-spark">
              {#if history.mem.length >= 2}<MiniSparkline series={history.mem} height={26} color="var(--series-1)" />{/if}
            </div>
          </article>
          <article class="quick">
            <span class="label">Network</span>
            <span class="quick-value">{stats ? formatRate(rates.rx_rate) : '—'}</span>
            <span class="quick-sub">{stats ? `${formatRate(rates.tx_rate)} out` : 'waiting'}</span>
            <div class="quick-spark">
              {#if history.rx.length >= 2}<MiniSparkline series={history.rx} height={26} color="var(--series-4)" />{/if}
            </div>
          </article>
          <article class="quick">
            <span class="label">Disk</span>
            <span class="quick-value">{stats ? formatRate(rates.read_rate) : '—'}</span>
            <span class="quick-sub">{stats ? `${formatRate(rates.write_rate)} write` : 'waiting'}</span>
            <div class="quick-spark">
              {#if history.read.length >= 2}<MiniSparkline series={history.read} height={26} color="var(--series-3)" />{/if}
            </div>
          </article>
          <article class="quick">
            <span class="label">Processes</span>
            <span class="quick-value">{stats ? stats.pids : '—'}</span>
            <span class="quick-sub">live</span>
            <div class="quick-spark"></div>
          </article>
        </div>
      {:else}
        <p class="subtle not-running">This container is not running. Start it to stream CPU, memory, network and disk metrics.</p>
      {/if}

      <div class="summary">
        <div class="summary-item">
          <span class="label">Status</span>
          <span class="summary-value">{container.status || '—'}</span>
        </div>
        <div class="summary-item">
          <span class="label">Created</span>
          <span class="summary-value" title={createdAbsolute || undefined}>{container.created_at ? createdRelative : 'not reported'}</span>
        </div>
        <div class="summary-item">
          <span class="label">Image</span>
          <span class="summary-value mono" title={container.image}>{shortImage(container.image)}</span>
        </div>
        <div class="summary-item">
          <span class="label">Container ID</span>
          <span class="summary-value mono" title={container.id}>{shortId(container.id)}</span>
        </div>
      </div>

      <div class="flags">
        {#if detail?.health}
          <span title="Healthcheck"><StatusPill label={detail.health.status} tone={healthTone(detail.health.status)} /></span>
          {#if detail.health.failing_streak > 0}<span class="flag warn">{detail.health.failing_streak} failing checks</span>{/if}
        {/if}
        {#if detail?.privileged}<span class="flag crit">privileged</span>{/if}
        {#if detail?.readonly_rootfs}<span class="flag">read-only rootfs</span>{/if}
        {#if detail?.auto_remove}<span class="flag warn">auto-remove</span>{/if}
        {#if detail?.state.oom_killed}<span class="flag crit">OOM killed</span>{/if}
      </div>

      <div class="overview-lists">
        <section class="block">
          <h4>Published ports</h4>
          {#if container.ports.length === 0}
            <p class="subtle">No published ports.</p>
          {:else}
            <ul class="chips">
              {#each container.ports as port (port.private_port + '/' + port.type + '/' + (port.public_port || 0))}
                <li class="port-chip mono" title={port.ip ? `Bound on ${port.ip}` : undefined}>{portLabel(port)}</li>
              {/each}
            </ul>
          {/if}
        </section>

        <section class="block">
          <h4>Networks</h4>
          {#if !detail}
            <p class="subtle">{inspectLoading ? 'Reading networks…' : 'Network detail is not available.'}</p>
          {:else if detail.networks.length === 0}
            <p class="subtle">No attached networks.</p>
          {:else}
            <ul class="rows">
              {#each detail.networks as network (network.name)}
                <li>
                  <span class="row-key">{network.name}</span>
                  <span class="row-value mono">{network.ip_address || 'no address'}</span>
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      </div>

      <section class="block">
        <h4>Mounts</h4>
        {#if !detail && inspectLoading}
          <p class="subtle">Reading mounts…</p>
        {:else if detailMounts.length === 0}
          <p class="subtle">No mounts.</p>
        {:else}
          <ul class="rows">
            {#each detailMounts as mount (mount.destination + mount.source)}
              <li>
                <span class="row-key mono">{mount.destination}</span>
                <span class="row-value mono">{mount.source || mount.type}</span>
                <span class="row-tag">{mount.rw ? 'rw' : 'ro'}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      {#if inspectError}
        <div class="inline-error" role="alert">
          {inspectError}
          <button on:click={() => void loadInspect()}>Retry</button>
        </div>
      {/if}
    {:else if tab === 'stats'}
      {#if !isRunning}
        <p class="subtle not-running">This container is not running. Start it to stream CPU, memory, network and disk metrics.</p>
      {:else if statsError}
        <div class="inline-error" role="alert">
          {statsError}
          <button on:click={() => { statsError = ''; startStats() }}>Retry</button>
        </div>
      {:else}
        <div class="resources">
          <article class="resource">
            <span class="label">CPU</span>
            <span class="value">{stats ? stats.cpu_usage_percent.toFixed(1) : '—'}<small>%</small></span>
            <div class="spark">
              {#if history.cpu.length >= 2}<MiniSparkline series={history.cpu} height={58} />{/if}
            </div>
            <span class="sub">last {history.cpu.length} samples</span>
          </article>
          <article class="resource">
            <span class="label">Memory</span>
            <span class="value">{stats ? stats.memory_percent.toFixed(1) : '—'}<small>%</small></span>
            <span class="sub">{stats ? `${formatBytes(stats.memory_usage_bytes)} / ${formatBytes(stats.memory_limit_bytes)}` : 'waiting for sample'}</span>
            <div class="spark">
              {#if history.mem.length >= 2}<MiniSparkline series={history.mem} height={58} color="var(--series-1)" />{/if}
            </div>
          </article>
          <article class="resource">
            <span class="label">Network in</span>
            <span class="value">{stats ? formatRate(rates.rx_rate) : '—'}</span>
            <span class="sub">{stats ? `${formatRate(rates.tx_rate)} out` : 'waiting for sample'}</span>
            <div class="spark">
              {#if history.rx.length >= 2}<MiniSparkline series={history.rx} height={58} color="var(--series-4)" />{/if}
            </div>
          </article>
          <article class="resource">
            <span class="label">Disk read</span>
            <span class="value">{stats ? formatRate(rates.read_rate) : '—'}</span>
            <span class="sub">{stats ? `${formatRate(rates.write_rate)} write` : 'waiting for sample'}</span>
            <div class="spark">
              {#if history.read.length >= 2}<MiniSparkline series={history.read} height={58} color="var(--series-3)" />{/if}
            </div>
          </article>
          <article class="resource">
            <span class="label">Processes</span>
            <span class="value">{stats ? stats.pids : '—'}</span>
            <span class="sub">live samples</span>
            <div class="spark"></div>
          </article>
        </div>
        <p class="ticker">Live from <span class="mono">docker stats</span>. The next sample starts when the previous one returns.</p>
      {/if}
    {:else if tab === 'logs'}
      <div class="log-toolbar">
        <label class="field">
          <span class="label">Lines</span>
          <select bind:value={tail} on:change={changeTail} disabled={logLoading}>
            <option value={300}>300</option>
            <option value={1000}>1000</option>
            <option value={2000}>2000</option>
          </select>
        </label>
        <label class="field grow">
          <span class="label">Filter</span>
          <input type="search" bind:value={logQuery} placeholder="Filter lines" aria-label="Filter log lines" />
        </label>
        <label class="check"><input type="checkbox" bind:checked={wrap} /> Wrap</label>
        <label class="check"><input type="checkbox" bind:checked={showTimestamps} /> Timestamps</label>
        <div class="log-actions">
          <button class="btn-ghost" class:live={logLive} on:click={() => setLogLive(!logLive)}>
            <span class="dot"></span>{logLive ? 'Live' : 'Paused'}
          </button>
          <button class="btn-ghost" on:click={() => void loadLogs()} disabled={logLoading}>
            <Icon name="refresh" size={13} /> Refresh
          </button>
          <button class="btn-ghost" on:click={() => void copy(displayedLogs, 'logs')} disabled={!displayedLogs}>
            <Icon name="copy" size={13} /> {copied === 'logs' ? 'Copied' : 'Copy'}
          </button>
        </div>
      </div>
      {#if logError}
        <div class="inline-error" role="alert">
          {logError}
          <button on:click={() => { logError = ''; startLogsPoll() }}>Retry</button>
        </div>
      {:else if logLoading && !logText}
        <p class="subtle pad">Asking the device for the last {tail} lines…</p>
      {:else}
        <pre bind:this={logEl} class="output logs" class:wrap>{displayedLogs || 'No output in the selected window.'}</pre>
        <div class="log-foot">
          <span class="hint">{logLineCount} lines · {logLive ? 'following every 3s' : 'paused'}</span>
        </div>
      {/if}
    {:else if tab === 'terminal'}
      {#if !isRunning}
        <p class="subtle">Start the container to run a command inside it.</p>
      {:else}
        <p class="subtle">
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
            <Icon name="copy" size={12} /> {copied === 'exec' ? 'Copied' : 'Copy'}
          </button>
          <pre class="output">{execOutput}</pre>
        </div>
      {/if}
    {:else if tab === 'config'}
      <div class="config-head">
        <span class="hint">{inspectLoading ? 'Reading configuration…' : 'Read-only. Environment variables are never sent to the dashboard.'}</span>
        <button class="btn-ghost" on:click={() => void loadInspect()} disabled={inspectLoading}>
          <Icon name="refresh" size={13} /> Refresh
        </button>
      </div>

      {#if inspectError}
        <div class="inline-error" role="alert">{inspectError}</div>
      {/if}

      <dl class="facts">
        <div><dt>Platform</dt><dd>{detail?.platform || '—'}</dd></div>
        <div><dt>Restart</dt><dd>{detail?.restart_policy || '—'}</dd></div>
        <div><dt>Memory</dt><dd>{detail ? (detail.memory_limit_bytes ? formatBytes(detail.memory_limit_bytes) : 'unlimited') : '—'}</dd></div>
        <div><dt>CPUs</dt><dd>{detail ? cpus(detail.nano_cpus) : '—'}</dd></div>
        <div><dt>User</dt><dd class="mono">{detail?.user || 'image default'}</dd></div>
        <div><dt>Workdir</dt><dd class="mono">{detail?.working_dir || '—'}</dd></div>
      </dl>

      {#if commandText}
        <section class="block">
          <h4>Command</h4>
          <pre class="command mono">{commandText}</pre>
        </section>
      {/if}

      {#if detail && Object.keys(detail.labels).length > 0}
        <details class="block collapsible">
          <summary>
            Labels ({Object.keys(detail.labels).length})
            <span class="block-note">credential-looking labels are hidden</span>
          </summary>
          <ul class="rows">
            {#each Object.entries(detail.labels) as [key, value] (key)}
              <li>
                <span class="row-key mono">{key}</span>
                <span class="row-value mono">{value}</span>
              </li>
            {/each}
          </ul>
        </details>
      {/if}
    {/if}
  </div>
</div>

<style>
  .detail-panel {
    background: var(--card-inset);
    box-shadow: inset 3px 0 0 var(--accent);
    display: grid;
    align-content: start;
    gap: 0;
    min-height: 100%;
  }

  .detail-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    flex-wrap: wrap;
    position: sticky;
    top: 0;
    z-index: 3;
    padding: 0.8rem 1rem 0.7rem;
    background: var(--card-inset);
    border-bottom: 1px solid var(--border);
  }
  .identity { display: flex; align-items: center; gap: 0.55rem; flex-wrap: wrap; min-width: 0; }
  .panel-name {
    color: var(--text-bright);
    font-size: 0.95rem;
    font-weight: 600;
    max-width: 26rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .id { color: var(--text-muted); }
  .detail-actions { display: flex; align-items: center; gap: 0.35rem; flex-wrap: wrap; }

  .tabs {
    display: flex;
    gap: 0.2rem;
    padding: 0 0.85rem;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
  }
  .tabs button {
    border: 0;
    border-bottom: 2px solid transparent;
    background: transparent;
    color: var(--text-faint);
    padding: 0.5rem 0.6rem;
    font-size: 0.76rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .tabs button:hover { color: var(--text); }
  .tabs button.active { color: var(--accent); border-bottom-color: var(--accent); }

  .tab-body { padding: 0.85rem 1rem 1.2rem; display: grid; align-content: start; gap: 0.75rem; min-width: 0; }

  .quick-metrics {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
    gap: 0.5rem;
  }
  .quick {
    display: grid;
    gap: 0.05rem;
    padding: 0.5rem 0.6rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    min-width: 0;
  }
  .quick-value {
    color: var(--text-bright);
    font-size: 1.05rem;
    font-variant-numeric: tabular-nums;
    line-height: 1.15;
  }
  .quick-value small {
    margin-left: 0.1rem;
    color: var(--text-faint);
    font-size: 0.6rem;
    font-weight: 600;
    text-transform: uppercase;
  }
  .quick-sub {
    color: var(--text-faint);
    font-size: 0.65rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .quick-spark { height: 26px; margin-top: 0.1rem; }

  .summary {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
    gap: 0.5rem;
  }

  .overview-lists {
    display: grid;
    grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.2fr);
    gap: 0.8rem;
    align-items: start;
  }
  .summary-item {
    display: grid;
    gap: 0.15rem;
    padding: 0.55rem 0.7rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    min-width: 0;
  }
  .summary-value {
    color: var(--text);
    font-size: 0.82rem;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .resources {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
    gap: 0.6rem;
  }
  .resource {
    display: grid;
    gap: 0.1rem;
    padding: 0.6rem 0.7rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    min-width: 0;
  }
  .resource .value {
    color: var(--text-bright);
    font-size: 1.25rem;
    font-variant-numeric: tabular-nums;
    line-height: 1.15;
  }
  .resource .value small {
    margin-left: 0.15rem;
    color: var(--text-faint);
    font-size: 0.62rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .resource .sub {
    color: var(--text-faint);
    font-size: 0.67rem;
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .spark { min-height: 58px; margin-top: 0.2rem; }
  .ticker { margin: 0; color: var(--text-faint); font-size: 0.68rem; }

  .flags { display: flex; align-items: center; gap: 0.4rem; flex-wrap: wrap; }
  .flag {
    padding: 0.12rem 0.45rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-pill);
    background: var(--card);
    color: var(--text-muted);
    font-size: 0.64rem;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }
  .flag.warn { color: var(--warn); border-color: rgba(251, 191, 36, 0.35); }
  .flag.crit { color: var(--crit); border-color: rgba(248, 113, 113, 0.4); }

  .block { display: grid; gap: 0.4rem; }
  .block h4 {
    margin: 0;
    color: var(--text-faint);
    font-size: 0.68rem;
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }
  .block-note { margin-left: 0.4rem; color: var(--text-faint); font-weight: 400; letter-spacing: 0; text-transform: none; }
  .collapsible summary {
    display: flex;
    align-items: baseline;
    gap: 0.2rem;
    color: var(--text-faint);
    font-size: 0.68rem;
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    cursor: pointer;
    padding: 0.25rem 0;
  }
  .collapsible summary:hover { color: var(--text-muted); }
  .collapsible[open] summary { margin-bottom: 0.3rem; }
  .subtle { margin: 0; color: var(--text-faint); font-size: 0.75rem; line-height: 1.5; }
  .subtle.pad { padding: 0.6rem 0; }
  .not-running {
    padding: 0.6rem 0.7rem;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--card);
  }

  .config-head { display: flex; align-items: center; justify-content: space-between; gap: 0.6rem; flex-wrap: wrap; }
  .hint { color: var(--text-faint); font-size: 0.72rem; }
  .command {
    margin: 0;
    padding: 0.5rem 0.6rem;
    background: #08080a;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: 0.7rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .chips { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 0.3rem; }
  .port-chip {
    padding: 0.12rem 0.45rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-pill);
    background: var(--card);
    color: var(--text);
    font-size: 0.68rem;
  }

  .facts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
    gap: 0.45rem 1rem;
    margin: 0;
  }
  .facts > div { display: grid; grid-template-columns: 5rem minmax(0, 1fr); gap: 0.5rem; align-items: baseline; }
  .facts dt { color: var(--text-faint); font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.04em; }
  .facts dd { margin: 0; color: var(--text); font-size: 0.78rem; }

  .rows { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
  .rows li {
    display: grid;
    grid-template-columns: minmax(8rem, 0.8fr) minmax(0, 1.6fr) auto;
    gap: 0.6rem;
    align-items: baseline;
    padding: 0.3rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card);
  }
  .row-key { color: var(--text); font-size: 0.72rem; overflow-wrap: anywhere; }
  .row-value { color: var(--text-muted); font-size: 0.7rem; overflow-wrap: anywhere; }
  .row-tag { color: var(--text-faint); font-size: 0.62rem; text-transform: uppercase; letter-spacing: 0.05em; }

  .log-toolbar { display: flex; align-items: end; gap: 0.6rem; flex-wrap: wrap; }
  .field { display: grid; gap: 0.15rem; }
  .field.grow { flex: 1 1 14rem; min-width: 0; }
  .field input {
    padding: 0.3rem 0.5rem;
    background: var(--card);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    font-size: 0.74rem;
  }
  .field input:focus { outline: none; border-color: var(--accent-border); }
  select {
    padding: 0.3rem 0.45rem;
    background: var(--card);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    font-size: 0.74rem;
  }
  .log-toolbar select { width: auto; }
  .check {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    color: var(--text-muted);
    font-size: 0.72rem;
    cursor: pointer;
    user-select: none;
  }
  .check input[type='checkbox'] {
    appearance: none;
    -webkit-appearance: none;
    width: 30px;
    height: 17px;
    margin: 0;
    padding: 0;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-pill);
    background: var(--card-inset);
    position: relative;
    cursor: pointer;
    transition: background var(--t-fast), border-color var(--t-fast);
  }
  .check input[type='checkbox']::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 11px;
    height: 11px;
    border-radius: 50%;
    background: var(--text-faint);
    transition: transform var(--t-fast), background var(--t-fast);
  }
  .check input[type='checkbox']:checked {
    background: var(--accent-dim);
    border-color: var(--accent-border);
  }
  .check input[type='checkbox']:checked::after {
    transform: translateX(13px);
    background: var(--accent);
  }
  .check input[type='checkbox']:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
  }
  .log-actions { display: flex; gap: 0.35rem; flex-wrap: wrap; }

  .output {
    margin: 0;
    padding: 0.6rem 0.7rem;
    max-height: 58vh;
    overflow: auto;
    background: #08080a;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-family: var(--mono);
    font-size: 0.75rem;
    line-height: 1.55;
    white-space: pre;
  }
  .output.wrap { white-space: pre-wrap; overflow-wrap: anywhere; }
  .log-foot { display: flex; justify-content: space-between; }
  .output-wrap { position: relative; }
  .copy-floating {
    position: absolute;
    top: 0.35rem;
    right: 0.35rem;
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
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
    background: var(--card);
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
    padding: 0.45rem 0.6rem;
    border: 1px solid rgba(248, 113, 113, 0.35);
    border-radius: var(--radius-sm);
    background: var(--crit-dim);
    color: var(--crit);
    font-size: 0.74rem;
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

  .btn-ghost,
  .btn-primary {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.3rem 0.6rem;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    background: transparent;
    color: var(--text);
    font-size: 0.73rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn-ghost:hover:not(:disabled) { background: var(--card-hover); }
  .btn-ghost.warn { color: var(--warn); border-color: rgba(251, 191, 36, 0.35); }
  .btn-ghost.danger { color: var(--crit); border-color: rgba(248, 113, 113, 0.35); }
  .btn-ghost.live { color: var(--accent); border-color: var(--accent-border); }
  .btn-primary {
    background: var(--accent-dim);
    border-color: var(--accent-border);
    color: var(--accent);
  }
  .btn-icon {
    display: inline-flex;
    border: 0;
    background: transparent;
    color: var(--text-muted);
    padding: 0.2rem;
    cursor: pointer;
    border-radius: var(--radius-sm);
  }
  .btn-icon:hover { color: var(--text); background: var(--card-hover); }
  button:disabled { opacity: 0.5; cursor: default; }

  @media (max-width: 760px) {
    .overview-lists { grid-template-columns: 1fr; }
    .resources { grid-template-columns: 1fr 1fr; }
    .rows li { grid-template-columns: minmax(0, 1fr) auto; }
    .row-value { grid-column: 1 / -1; }
    .output { max-height: 50vh; }
  }
</style>
