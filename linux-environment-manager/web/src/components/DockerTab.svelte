<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy } from 'svelte'
  import { serverBase } from '../lib/api'

  type DockerContainer = {
    id: string
    names: string[]
    image: string
    state: string
    status: string
    ports?: { private_port: number; public_port?: number; type: string }[]
    mounts?: { type: string; source: string; destination: string; rw: boolean }[]
    created_at: number
  }

  type DockerStats = {
    cpu_usage_percent: number
    memory_usage_bytes: number
    memory_limit_bytes: number
    memory_percent: number
    network_rx_bytes: number
    network_tx_bytes: number
    block_read_bytes: number
    block_write_bytes: number
    pids: number
  }

  type DockerComposeFile = {
    path: string
    content: string
    size_bytes: number
  }

  type CommandResult = {
    id: string
    type: string
    status: string
    message: string
    name?: string
    source?: string
  }

  type DockerInfo = {
    version?: string
    total?: number
    running?: number
    stopped?: number
    paused?: number
    images?: number
    driver?: string
    ncpu?: number
  }

  export let deviceId: string
  export let authHeaders: Record<string, string> = {}

  const dispatch = createEventDispatcher()

  let containers: DockerContainer[] = []
  let dockerInfo: DockerInfo | null = null
  let loading = false
  let error = ''
  let statusFilter = 'all'
  let searchQuery = ''
  let autoRefresh = true
  let refreshInterval: ReturnType<typeof setInterval> | null = null
  let dockerAvailable = true
  let selectedContainer: DockerContainer | null = null

  // Right panel state
  let rightPanel: 'details' | 'logs' | 'exec' | 'compose' | 'prune' | 'stats' = 'details'

  // Stats state
  let containerStats: Record<string, DockerStats> = {}
  let statsLoading = false
  let statsHistory: Record<string, { cpu: number[]; mem: number[]; netRx: number[]; netTx: number[] }> = {}
  let statsInterval: ReturnType<typeof setInterval> | null = null

  // Compose state
  let composeFiles: DockerComposeFile[] = []
  let selectedCompose: DockerComposeFile | null = null
  let composeContent = ''
  let composeLoading = false
  let composeSaving = false

  // Exec state
  let execContainerId = ''
  let execCommand = ''
  let execOutput = ''
  let execRunning = false

  // Prune state
  let showPruneConfirm = false
  let pruneTarget = ''
  let pruneResult = ''
  let pruning = false

  // Logs state
  let logOutput = ''
  let logLoading = false

  // Action confirmation
  let confirmAction: { action: string; containerId: string; containerName: string } | null = null

  $: filteredContainers = containers.filter(c => {
    const matchesStatus = statusFilter === 'all' || c.state === statusFilter
    const matchesSearch = searchQuery === '' ||
      c.names.some(n => n.toLowerCase().includes(searchQuery.toLowerCase())) ||
      c.image.toLowerCase().includes(searchQuery.toLowerCase())
    return matchesStatus && matchesSearch
  })

  $: runningCount = containers.filter(c => c.state === 'running').length
  $: stoppedCount = containers.filter(c => c.state === 'exited').length
  $: pausedCount = containers.filter(c => c.state === 'paused').length

  function shortId(id: string): string {
    return id.substring(0, 12)
  }

  function containerName(c: DockerContainer): string {
    if (c.names && c.names.length > 0) return c.names[0].replace(/^\//, '')
    return 'unnamed'
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
  }

  function stateColor(state: string): string {
    switch (state) {
      case 'running': return '#22c55e'
      case 'exited': return '#ef4444'
      case 'paused': return '#f59e0b'
      case 'created': return '#6b7280'
      default: return '#6b7280'
    }
  }

  async function apiCall(method: string, path: string, body?: any): Promise<any> {
    const opts: RequestInit = { method, headers: { ...authHeaders } }
    if (body) {
      opts.headers = { ...opts.headers, 'Content-Type': 'application/json' }
      opts.body = JSON.stringify(body)
    }
    const res = await fetch(`${serverBase}${path}`, opts)
    if (!res.ok) throw new Error(`${res.status}`)
    if (res.status === 204) return null
    return res.json()
  }

  async function pollForResult(cmdId: string, maxAttempts = 30): Promise<CommandResult | null> {
    for (let i = 0; i < maxAttempts; i++) {
      await new Promise(r => setTimeout(r, 1000))
      try {
        const result = await apiCall('GET', `/api/devices/${deviceId}/docker/result/${cmdId}`)
        if (result && (result.status === 'completed' || result.status === 'failed')) {
          return result
        }
      } catch { /* continue polling */ }
    }
    return null
  }

  async function loadContainers() {
    loading = true
    error = ''
    try {
      const state = await apiCall('GET', `/api/devices/${deviceId}/docker`)
      if (state && state.containers) {
        containers = state.containers.map((c: any) => ({
          id: c.id,
          names: c.names || (c.name ? [c.name] : []),
          image: c.image,
          state: c.state,
          status: c.status || '',
          created_at: c.created_at || 0
        }))
        dockerAvailable = state.available !== false
        dockerInfo = state.info || null
      } else if (state && state.available === false) {
        dockerAvailable = false
        error = 'Docker is not available on this device. The agent may need docker group permissions.'
        stopAutoRefresh()
      }
    } catch (e: any) {
      error = e.message || 'Failed to load containers'
    } finally {
      loading = false
    }
  }

  async function containerAction(action: string, containerId: string) {
    try {
      const cmd = await apiCall('POST', `/api/devices/${deviceId}/docker/action`, {
        action,
        container_id: containerId
      })
      if (cmd && cmd.id) {
        const result = await pollForResult(cmd.id, 20)
        if (result) {
          if (result.status === 'failed') {
            const msg = result.message || `${action} failed`
            if (msg.includes('docker not available')) {
              error = 'Docker is not available on this device'
            } else {
              error = msg
            }
          } else {
            await loadContainers()
          }
        }
      }
    } catch (e: any) {
      error = e.message || `${action} failed`
    }
    confirmAction = null
  }

  async function showContainerLogs(containerId: string) {
    rightPanel = 'logs'
    logLoading = true
    logOutput = ''
    try {
      const cmd = await apiCall('POST', `/api/devices/${deviceId}/docker/logs`, {
        container_id: containerId,
        tail: 300
      })
      if (cmd && cmd.id) {
        const result = await pollForResult(cmd.id, 15)
        if (result && result.message) {
          logOutput = result.message
        } else if (result && result.status === 'failed') {
          logOutput = `Error: ${result.message}`
        } else {
          logOutput = 'Timed out waiting for logs'
        }
      }
    } catch (e: any) {
      logOutput = `Error: ${e.message}`
    } finally {
      logLoading = false
    }
  }

  async function runExec() {
    if (!execContainerId || !execCommand.trim()) return
    execRunning = true
    execOutput = ''
    try {
      const parts = execCommand.trim().split(/\s+/)
      const cmd = await apiCall('POST', `/api/devices/${deviceId}/docker/exec`, {
        container_id: execContainerId,
        command: parts
      })
      if (cmd && cmd.id) {
        const result = await pollForResult(cmd.id, 30)
        if (result) {
          execOutput = result.status === 'failed'
            ? `Error: ${result.message}`
            : result.message || '(no output)'
        }
      }
    } catch (e: any) {
      execOutput = `Error: ${e.message}`
    } finally {
      execRunning = false
    }
  }

  async function loadComposeFiles() {
    composeLoading = true
    try {
      const cmd = await apiCall('POST', `/api/devices/${deviceId}/docker/compose`, { action: 'read' })
      if (cmd && cmd.id) {
        const result = await pollForResult(cmd.id, 20)
        if (result && result.status === 'completed' && result.message) {
          composeFiles = JSON.parse(result.message)
        }
      }
    } catch { /* ignore */ } finally {
      composeLoading = false
    }
  }

  function selectCompose(file: DockerComposeFile) {
    selectedCompose = file
    composeContent = file.content
  }

  async function loadContainerStats(containerId: string) {
    statsLoading = true
    try {
      const cmd = await apiCall('POST', `/api/devices/${deviceId}/docker/action`, {
        action: 'stats',
        container_id: containerId
      })
      if (cmd && cmd.id) {
        const result = await pollForResult(cmd.id, 10)
        if (result && result.status === 'completed' && result.message) {
          const stats: DockerStats = JSON.parse(result.message)
          containerStats[containerId] = stats
          containerStats = containerStats
          if (!statsHistory[containerId]) {
            statsHistory[containerId] = { cpu: [], mem: [], netRx: [], netTx: [] }
          }
          const h = statsHistory[containerId]
          h.cpu.push(stats.cpu_usage_percent)
          h.mem.push(stats.memory_percent)
          h.netRx.push(stats.network_rx_bytes)
          h.netTx.push(stats.network_tx_bytes)
          if (h.cpu.length > 30) { h.cpu.shift(); h.mem.shift(); h.netRx.shift(); h.netTx.shift() }
          statsHistory = statsHistory
        }
      }
    } catch { /* ignore */ } finally {
      statsLoading = false
    }
  }

  function startStatsRefresh() {
    stopStatsRefresh()
    statsInterval = setInterval(() => {
      if (selectedContainer && rightPanel === 'stats') {
        loadContainerStats(selectedContainer.id)
      }
    }, 3000)
  }

  function stopStatsRefresh() {
    if (statsInterval) {
      clearInterval(statsInterval)
      statsInterval = null
    }
  }

  function sparklinePath(data: number[], width: number, height: number): string {
    if (data.length < 2) return ''
    const max = Math.max(...data, 1)
    const step = width / (data.length - 1)
    return data.map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)},${(height - (v / max) * height).toFixed(1)}`).join(' ')
  }

  async function saveCompose() {
    if (!selectedCompose) return
    composeSaving = true
    try {
      const cmd = await apiCall('POST', `/api/devices/${deviceId}/docker/compose`, {
        action: 'write',
        path: selectedCompose.path,
        content: composeContent
      })
      if (cmd && cmd.id) {
        await pollForResult(cmd.id, 10)
        await loadComposeFiles()
      }
    } catch (e: any) {
      error = e.message || 'Save failed'
    } finally {
      composeSaving = false
    }
  }

  async function deployCompose(path: string) {
    try {
      const cmd = await apiCall('POST', `/api/devices/${deviceId}/docker/compose`, {
        action: 'up',
        path
      })
      if (cmd && cmd.id) {
        const result = await pollForResult(cmd.id, 30)
        if (result && result.status === 'failed') {
          error = result.message || 'Deploy failed'
        } else {
          await loadContainers()
        }
      }
    } catch (e: any) {
      error = e.message || 'Deploy failed'
    }
  }

  async function confirmPrune() {
    pruning = true
    pruneResult = ''
    try {
      const cmd = await apiCall('POST', `/api/devices/${deviceId}/docker/prune`, { target: pruneTarget })
      if (cmd && cmd.id) {
        const result = await pollForResult(cmd.id, 30)
        if (result) {
          pruneResult = result.status === 'failed'
            ? `Error: ${result.message}`
            : result.message || 'Prune completed'
        }
      }
    } catch (e: any) {
      pruneResult = `Error: ${e.message}`
    } finally {
      pruning = false
      showPruneConfirm = false
    }
  }

  function selectContainer(c: DockerContainer) {
    if (selectedContainer?.id === c.id) {
      selectedContainer = null
      stopStatsRefresh()
    } else {
      selectedContainer = c
      rightPanel = 'details'
      if (c.state === 'running') {
        loadContainerStats(c.id)
      }
    }
  }

  function startAutoRefresh() {
    stopAutoRefresh()
    if (autoRefresh) {
      refreshInterval = setInterval(loadContainers, 10000)
    }
  }

  function stopAutoRefresh() {
    if (refreshInterval) {
      clearInterval(refreshInterval)
      refreshInterval = null
    }
  }

  function toggleAutoRefresh() {
    if (!dockerAvailable) return
    autoRefresh = !autoRefresh
    if (autoRefresh) startAutoRefresh()
    else stopAutoRefresh()
  }

  onMount(() => {
    loadContainers()
    startAutoRefresh()
  })

  onDestroy(() => {
    stopAutoRefresh()
    stopStatsRefresh()
  })
</script>

<div class="docker-tab">
  {#if error}
    <div class="error-bar">{error} <button on:click={() => (error = '')}>Dismiss</button></div>
  {/if}

  <div class="docker-layout">
    <!-- LEFT: Container List -->
    <div class="container-panel">
      <div class="panel-header">
        <div class="docker-stats-row">
          <span class="stat-chip running">{runningCount} running</span>
          <span class="stat-chip stopped">{stoppedCount} stopped</span>
          {#if pausedCount > 0}
            <span class="stat-chip paused">{pausedCount} paused</span>
          {/if}
        </div>
        <div class="panel-actions">
          <button class="btn-sm" on:click={loadContainers} disabled={loading}>
            {loading ? '...' : 'Refresh'}
          </button>
          <button class="btn-sm" class:active={autoRefresh} on:click={toggleAutoRefresh} disabled={!dockerAvailable}>
            {autoRefresh ? 'Auto' : 'Manual'}
          </button>
        </div>
      </div>

      <div class="filters-row">
        <input type="text" placeholder="Search..." bind:value={searchQuery} class="search-input" />
        <select bind:value={statusFilter} class="status-select">
          <option value="all">All</option>
          <option value="running">Running</option>
          <option value="exited">Stopped</option>
          <option value="paused">Paused</option>
        </select>
      </div>

      {#if dockerInfo}
        <div class="engine-info">
          <span>Docker {dockerInfo.version || '?'}</span>
          <span class="info-sep">|</span>
          <span>{dockerInfo.images || 0} images</span>
          <span class="info-sep">|</span>
          <span>{dockerInfo.ncpu || 0} CPUs</span>
        </div>
      {/if}

      <div class="container-list">
        {#if loading && containers.length === 0}
          <div class="loading-state">Loading containers...</div>
        {:else if filteredContainers.length === 0}
          <div class="empty-state">
            {containers.length === 0 ? 'No containers found.' : 'No matches.'}
          </div>
        {:else}
          {#each filteredContainers as container (container.id)}
            <div class="container-row" class:selected={selectedContainer?.id === container.id} on:click={() => selectContainer(container)}>
              <div class="container-main">
                <span class="state-dot" style="background: {stateColor(container.state)}"></span>
                <div class="container-info">
                  <span class="container-name">{containerName(container)}</span>
                  <span class="container-image">{container.image}</span>
                </div>
              </div>
              <div class="container-actions">
                {#if container.state === 'running'}
                  <button class="action-btn stop" title="Stop" on:click|stopPropagation={() => confirmAction = { action: 'stop', containerId: container.id, containerName: containerName(container) }}>Stop</button>
                  <button class="action-btn" title="Restart" on:click|stopPropagation={() => confirmAction = { action: 'restart', containerId: container.id, containerName: containerName(container) }}>Restart</button>
                {:else}
                  <button class="action-btn start" title="Start" on:click|stopPropagation={() => confirmAction = { action: 'start', containerId: container.id, containerName: containerName(container) }}>Start</button>
                {/if}
                <button class="action-btn" title="Logs" on:click|stopPropagation={() => { selectedContainer = container; execContainerId = container.id; showContainerLogs(container.id) }}>Logs</button>
                <button class="action-btn" title="Shell" on:click|stopPropagation={() => { selectedContainer = container; execContainerId = container.id; rightPanel = 'exec' }}>Shell</button>
              </div>
            </div>
          {/each}
        {/if}
      </div>

      <!-- Bottom actions -->
      <div class="panel-footer">
        <button class="btn-sm" on:click={() => { rightPanel = 'compose'; loadComposeFiles() }}>Compose</button>
        <button class="btn-sm" on:click={() => { rightPanel = 'prune' }}>Cleanup</button>
      </div>
    </div>

    <!-- RIGHT: Detail Panel -->
    <div class="detail-panel">
      {#if selectedContainer}
        <!-- Tabs -->
        <div class="detail-tabs">
          <button class="tab-btn" class:active={rightPanel === 'details'} on:click={() => { rightPanel = 'details' }}>Details</button>
          {#if selectedContainer.state === 'running'}
            <button class="tab-btn" class:active={rightPanel === 'stats'} on:click={() => { rightPanel = 'stats'; loadContainerStats(selectedContainer!.id); startStatsRefresh() }}>Stats</button>
          {/if}
          <button class="tab-btn" class:active={rightPanel === 'logs'} on:click={() => showContainerLogs(selectedContainer!.id)}>Logs</button>
          <button class="tab-btn" class:active={rightPanel === 'exec'} on:click={() => { rightPanel = 'exec'; execContainerId = selectedContainer!.id }}>Shell</button>
          <button class="tab-btn" class:active={rightPanel === 'compose'} on:click={() => { rightPanel = 'compose'; loadComposeFiles() }}>Compose</button>
          <button class="tab-btn" class:active={rightPanel === 'prune'} on:click={() => { rightPanel = 'prune' }}>Cleanup</button>
        </div>

        {#if rightPanel === 'details'}
          <div class="detail-content">
            <div class="detail-header">
              <span class="state-dot-lg" style="background: {stateColor(selectedContainer.state)}"></span>
              <div>
                <h3>{containerName(selectedContainer)}</h3>
                <p class="detail-subtitle">{selectedContainer.image}</p>
              </div>
            </div>

            <div class="detail-grid">
              <div class="detail-item">
                <span class="detail-label">State</span>
                <span class="state-badge {selectedContainer.state}">{selectedContainer.state}</span>
              </div>
              <div class="detail-item">
                <span class="detail-label">Status</span>
                <span>{selectedContainer.status || 'N/A'}</span>
              </div>
              <div class="detail-item">
                <span class="detail-label">ID</span>
                <code>{shortId(selectedContainer.id)}</code>
              </div>
              {#if selectedContainer.created_at}
                <div class="detail-item">
                  <span class="detail-label">Created</span>
                  <span>{new Date(selectedContainer.created_at * 1000).toLocaleString()}</span>
                </div>
              {/if}
            </div>

            {#if selectedContainer.ports && selectedContainer.ports.length > 0}
              <div class="detail-section">
                <h4>Ports</h4>
                <div class="chip-list">
                  {#each selectedContainer.ports as port}
                    <span class="port-chip">{port.private_port}/{port.type}{port.public_port ? ` \u2192 ${port.public_port}` : ''}</span>
                  {/each}
                </div>
              </div>
            {/if}

            {#if selectedContainer.mounts && selectedContainer.mounts.length > 0}
              <div class="detail-section">
                <h4>Mounts</h4>
                <div class="mount-list">
                  {#each selectedContainer.mounts as mount}
                    <div class="mount-entry">
                      <span class="mount-source">{mount.source}</span>
                      <span class="mount-arrow">\u2192</span>
                      <span class="mount-dest">{mount.destination}</span>
                      <span class="mount-rw">({mount.rw ? 'rw' : 'ro'})</span>
                    </div>
                  {/each}
                </div>
              </div>
            {/if}

            <div class="detail-actions">
              {#if selectedContainer.state === 'running'}
                <button class="btn-action stop" on:click={() => confirmAction = { action: 'stop', containerId: selectedContainer!.id, containerName: containerName(selectedContainer!) }}>Stop</button>
                <button class="btn-action" on:click={() => confirmAction = { action: 'restart', containerId: selectedContainer!.id, containerName: containerName(selectedContainer!) }}>Restart</button>
              {:else}
                <button class="btn-action start" on:click={() => confirmAction = { action: 'start', containerId: selectedContainer!.id, containerName: containerName(selectedContainer!) }}>Start</button>
              {/if}
              <button class="btn-action danger" on:click={() => confirmAction = { action: 'remove', containerId: selectedContainer!.id, containerName: containerName(selectedContainer!) }}>Remove</button>
            </div>
          </div>

        {:else if rightPanel === 'stats'}
          <div class="detail-content">
            <div class="detail-header">
              <h3>Stats: {containerName(selectedContainer)}</h3>
              <button class="btn-sm" on:click={() => loadContainerStats(selectedContainer!.id)} disabled={statsLoading}>
                {statsLoading ? 'Loading...' : 'Refresh'}
              </button>
            </div>
            {#if containerStats[selectedContainer!.id]}
              {@const st = containerStats[selectedContainer!.id]}
              {@const hist = statsHistory[selectedContainer!.id] || { cpu: [], mem: [], netRx: [], netTx: [] }}
              <div class="stats-grid">
                <!-- CPU -->
                <div class="stat-card">
                  <div class="stat-label">CPU</div>
                  <div class="stat-value">{st.cpu_usage_percent.toFixed(1)}%</div>
                  <div class="stat-bar">
                    <div class="stat-bar-fill cpu" style="width: {Math.min(st.cpu_usage_percent, 100)}%"></div>
                  </div>
                  {#if hist.cpu.length > 1}
                    <svg class="sparkline" viewBox="0 0 120 30" preserveAspectRatio="none">
                      <path d={sparklinePath(hist.cpu, 120, 30)} fill="none" stroke="#22c55e" stroke-width="1.5" />
                    </svg>
                  {/if}
                </div>
                <!-- Memory -->
                <div class="stat-card">
                  <div class="stat-label">Memory</div>
                  <div class="stat-value">{st.memory_percent.toFixed(1)}%</div>
                  <div class="stat-detail">{formatBytes(st.memory_usage_bytes)} / {formatBytes(st.memory_limit_bytes)}</div>
                  <div class="stat-bar">
                    <div class="stat-bar-fill mem" style="width: {Math.min(st.memory_percent, 100)}%"></div>
                  </div>
                  {#if hist.mem.length > 1}
                    <svg class="sparkline" viewBox="0 0 120 30" preserveAspectRatio="none">
                      <path d={sparklinePath(hist.mem, 120, 30)} fill="none" stroke="#8b5cf6" stroke-width="1.5" />
                    </svg>
                  {/if}
                </div>
                <!-- Network -->
                <div class="stat-card">
                  <div class="stat-label">Network I/O</div>
                  <div class="stat-detail net-row">
                    <span class="net-rx">&darr; {formatBytes(st.network_rx_bytes)}</span>
                    <span class="net-tx">&uarr; {formatBytes(st.network_tx_bytes)}</span>
                  </div>
                  {#if hist.netRx.length > 1}
                    {@const maxRx = Math.max(...hist.netRx, 1)}
                    {@const maxTx = Math.max(...hist.netTx, 1)}
                    {@const maxN = Math.max(maxRx, maxTx)}
                    <svg class="sparkline" viewBox="0 0 120 30" preserveAspectRatio="none">
                      <path d={sparklinePath(hist.netRx.map(v => v / maxN * 100), 120, 30)} fill="none" stroke="#38bdf8" stroke-width="1.5" />
                      <path d={sparklinePath(hist.netTx.map(v => v / maxN * 100), 120, 30)} fill="none" stroke="#f472b6" stroke-width="1.5" />
                    </svg>
                  {/if}
                </div>
                <!-- Block I/O + PIDs -->
                <div class="stat-card">
                  <div class="stat-label">Disk I/O</div>
                  <div class="stat-detail">
                    <span class="net-rx">&darr; {formatBytes(st.block_read_bytes)}</span>
                    <span class="net-tx">&uarr; {formatBytes(st.block_write_bytes)}</span>
                  </div>
                  <div class="stat-label" style="margin-top:0.3rem">Processes</div>
                  <div class="stat-value" style="font-size:0.9rem">{st.pids}</div>
                </div>
              </div>
            {:else if statsLoading}
              <div class="loading-state">Loading stats...</div>
            {:else}
              <div class="empty-state">Click Refresh to load resource usage.</div>
            {/if}
          </div>

        {:else if rightPanel === 'logs'}
          <div class="detail-content">
            <div class="detail-header">
              <h3>Logs: {containerName(selectedContainer)}</h3>
              <button class="btn-sm" on:click={() => showContainerLogs(selectedContainer!.id)} disabled={logLoading}>
                {logLoading ? 'Loading...' : 'Refresh'}
              </button>
            </div>
            {#if logLoading}
              <div class="loading-state">Loading logs...</div>
            {:else}
              <pre class="code-output">{logOutput || 'No logs available'}</pre>
            {/if}
          </div>

        {:else if rightPanel === 'exec'}
          <div class="detail-content">
            <div class="detail-header">
              <h3>Shell: {containerName(selectedContainer)}</h3>
            </div>
            <div class="exec-input-row">
              <input
                type="text"
                placeholder="Command (e.g. ls -la)"
                bind:value={execCommand}
                on:keydown={(e) => e.key === 'Enter' && runExec()}
                disabled={execRunning}
                class="exec-input"
              />
              <button class="btn-sm" on:click={runExec} disabled={execRunning || !execCommand.trim()}>
                {execRunning ? 'Running...' : 'Run'}
              </button>
            </div>
            {#if execOutput}
              <pre class="code-output">{execOutput}</pre>
            {/if}
          </div>

        {:else if rightPanel === 'compose'}
          <div class="detail-content">
            <div class="detail-header">
              <h3>Docker Compose Files</h3>
              <button class="btn-sm" on:click={loadComposeFiles} disabled={composeLoading}>
                {composeLoading ? 'Scanning...' : 'Scan'}
              </button>
            </div>
            {#if composeFiles.length > 0}
              <div class="compose-list">
                {#each composeFiles as file}
                  <div class="compose-file-row" class:selected={selectedCompose?.path === file.path}>
                    <button class="compose-select" on:click={() => selectCompose(file)}>
                      <span class="compose-path">{file.path}</span>
                      <span class="compose-size">{formatBytes(file.size_bytes)}</span>
                    </button>
                    <button class="btn-sm deploy" on:click={() => deployCompose(file.path)}>Deploy</button>
                  </div>
                {/each}
              </div>
              {#if selectedCompose}
                <div class="compose-editor">
                  <textarea bind:value={composeContent} class="code-editor" rows="16"></textarea>
                  <div class="editor-actions">
                    <button class="btn-sm" on:click={saveCompose} disabled={composeSaving}>
                      {composeSaving ? 'Saving...' : 'Save'}
                    </button>
                  </div>
                </div>
              {/if}
            {:else if composeLoading}
              <div class="loading-state">Scanning for compose files...</div>
            {:else}
              <div class="empty-state">No compose files found.</div>
            {/if}
          </div>

        {:else if rightPanel === 'prune'}
          <div class="detail-content">
            <div class="detail-header">
              <h3>System Cleanup</h3>
            </div>
            <p class="prune-desc">Remove unused Docker resources to free disk space.</p>
            <div class="prune-grid">
              <button class="prune-btn system" on:click={() => { pruneTarget = 'system'; showPruneConfirm = true }}>
                <strong>System Prune</strong>
                <span>All unused images, containers, networks</span>
              </button>
              <button class="prune-btn" on:click={() => { pruneTarget = 'image'; showPruneConfirm = true }}>
                <strong>Image Prune</strong>
                <span>Dangling images</span>
              </button>
              <button class="prune-btn" on:click={() => { pruneTarget = 'container'; showPruneConfirm = true }}>
                <strong>Container Prune</strong>
                <span>Stopped containers</span>
              </button>
              <button class="prune-btn" on:click={() => { pruneTarget = 'network'; showPruneConfirm = true }}>
                <strong>Network Prune</strong>
                <span>Unused networks</span>
              </button>
            </div>
            {#if pruneResult}
              <pre class="code-output">{pruneResult}</pre>
            {/if}
          </div>
        {/if}

      {:else}
        <div class="no-selection">
          <div class="no-selection-icon">🐳</div>
          <h3>Docker Management</h3>
          <p>Select a container from the list to view details, logs, or execute commands.</p>
          {#if dockerInfo}
            <div class="engine-summary">
              <div class="engine-stat">
                <span class="engine-num">{dockerInfo.running || 0}</span>
                <span class="engine-label">Running</span>
              </div>
              <div class="engine-stat">
                <span class="engine-num">{dockerInfo.stopped || 0}</span>
                <span class="engine-label">Stopped</span>
              </div>
              <div class="engine-stat">
                <span class="engine-num">{dockerInfo.images || 0}</span>
                <span class="engine-label">Images</span>
              </div>
              <div class="engine-stat">
                <span class="engine-num">{dockerInfo.total || 0}</span>
                <span class="engine-label">Total</span>
              </div>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </div>

  <!-- Confirmation Dialog -->
  {#if confirmAction}
    <div class="confirm-overlay" on:click={() => (confirmAction = null)}>
      <div class="confirm-dialog" on:click|stopPropagation>
        <h4>Confirm {confirmAction.action}</h4>
        <p>Are you sure you want to <strong>{confirmAction.action}</strong> container <code>{confirmAction.containerName}</code>?</p>
        {#if confirmAction.action === 'remove'}
          <p class="warning">This will permanently remove the container.</p>
        {/if}
        <div class="confirm-buttons">
          <button class="btn-sm cancel" on:click={() => (confirmAction = null)}>Cancel</button>
          <button class="btn-sm confirm" class:danger={confirmAction.action === 'remove' || confirmAction.action === 'kill'}
            on:click={() => containerAction(confirmAction!.action, confirmAction!.containerId)}>
            {confirmAction.action}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <!-- Prune Confirmation -->
  {#if showPruneConfirm}
    <div class="confirm-overlay" on:click={() => (showPruneConfirm = false)}>
      <div class="confirm-dialog" on:click|stopPropagation>
        <h4>Confirm {pruneTarget} prune</h4>
        <p>This will remove unused Docker resources. Continue?</p>
        <div class="confirm-buttons">
          <button class="btn-sm cancel" on:click={() => (showPruneConfirm = false)}>Cancel</button>
          <button class="btn-sm confirm danger" on:click={confirmPrune} disabled={pruning}>
            {pruning ? 'Pruning...' : 'Prune'}
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .docker-tab {
    display: flex;
    flex-direction: column;
    gap: 0;
    height: 100%;
  }

  .error-bar {
    padding: 0.4rem 0.6rem;
    background: rgba(239,68,68,0.15);
    border: 1px solid rgba(239,68,68,0.3);
    border-radius: 6px;
    color: #f87171;
    font-size: 0.78rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.5rem;
  }
  .error-bar button {
    background: none;
    border: none;
    color: #f87171;
    cursor: pointer;
    text-decoration: underline;
    font-size: 0.72rem;
  }

  /* Two-column layout */
  .docker-layout {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.5rem;
    min-height: 400px;
    flex: 1;
  }

  .container-panel, .detail-panel {
    background: rgba(15,23,42,0.3);
    border: 1px solid rgba(148,163,184,0.1);
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .panel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.5rem 0.6rem;
    border-bottom: 1px solid rgba(148,163,184,0.08);
    flex-wrap: wrap;
    gap: 0.3rem;
  }

  .panel-actions {
    display: flex;
    gap: 0.3rem;
  }

  .docker-stats-row {
    display: flex;
    gap: 0.3rem;
    flex-wrap: wrap;
  }

  .stat-chip {
    padding: 0.15rem 0.4rem;
    border-radius: 999px;
    font-size: 0.65rem;
    font-weight: 600;
  }
  .stat-chip.running { background: rgba(34,197,94,0.15); color: #4ade80; }
  .stat-chip.stopped { background: rgba(239,68,68,0.15); color: #f87171; }
  .stat-chip.paused { background: rgba(245,158,11,0.15); color: #fbbf24; }

  .engine-info {
    padding: 0.25rem 0.6rem;
    font-size: 0.68rem;
    color: #64748b;
    border-bottom: 1px solid rgba(148,163,184,0.08);
  }
  .info-sep { margin: 0 0.3rem; opacity: 0.4; }

  .filters-row {
    display: flex;
    gap: 0.3rem;
    padding: 0.4rem 0.6rem;
    border-bottom: 1px solid rgba(148,163,184,0.08);
  }

  .search-input, .status-select {
    padding: 0.3rem 0.45rem;
    background: rgba(15,23,42,0.6);
    border: 1px solid rgba(148,163,184,0.2);
    border-radius: 5px;
    color: #e2e8f0;
    font-size: 0.75rem;
  }
  .search-input { flex: 1; min-width: 0; }
  .status-select { width: auto; }

  .container-list {
    flex: 1;
    overflow-y: auto;
    display: grid;
    gap: 2px;
    padding: 0.3rem;
  }

  .loading-state, .empty-state {
    padding: 1.5rem;
    text-align: center;
    color: #64748b;
    font-size: 0.78rem;
  }

  .container-row {
    background: rgba(15,23,42,0.3);
    border: 1px solid transparent;
    border-radius: 6px;
    cursor: pointer;
    transition: border-color 0.15s;
  }
  .container-row:hover { border-color: rgba(148,163,184,0.2); }
  .container-row.selected { border-color: rgba(45,212,191,0.3); background: rgba(45,212,191,0.05); }

  .container-main {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.4rem 0.5rem;
    min-width: 0;
  }

  .state-dot, .state-dot-lg {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .state-dot-lg {
    width: 12px;
    height: 12px;
  }

  .container-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.05rem;
  }

  .container-name {
    color: #e2e8f0;
    font-size: 0.78rem;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .container-image {
    color: #64748b;
    font-size: 0.68rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .container-actions {
    display: flex;
    gap: 0.2rem;
    padding: 0 0.5rem 0.35rem;
  }

  .action-btn {
    background: rgba(148,163,184,0.08);
    border: 1px solid rgba(148,163,184,0.15);
    border-radius: 4px;
    padding: 0.15rem 0.35rem;
    cursor: pointer;
    font-size: 0.68rem;
    color: #94a3b8;
    transition: background 0.15s;
  }
  .action-btn:hover { background: rgba(148,163,184,0.2); }
  .action-btn.stop:hover { background: rgba(239,68,68,0.2); color: #f87171; }
  .action-btn.start:hover { background: rgba(34,197,94,0.2); color: #4ade80; }

  .panel-footer {
    display: flex;
    gap: 0.3rem;
    padding: 0.4rem 0.6rem;
    border-top: 1px solid rgba(148,163,184,0.08);
  }

  /* Detail panel */
  .detail-tabs {
    display: flex;
    gap: 0;
    border-bottom: 1px solid rgba(148,163,184,0.1);
  }

  .tab-btn {
    flex: 1;
    padding: 0.4rem 0.3rem;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: #64748b;
    font-size: 0.72rem;
    cursor: pointer;
    transition: all 0.15s;
  }
  .tab-btn:hover { color: #94a3b8; }
  .tab-btn.active { color: #2dd4bf; border-bottom-color: #2dd4bf; }

  .detail-content {
    flex: 1;
    overflow-y: auto;
    padding: 0.6rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }

  .detail-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .detail-header h3 {
    margin: 0;
    color: #e2e8f0;
    font-size: 0.88rem;
  }
  .detail-subtitle {
    margin: 0;
    color: #64748b;
    font-size: 0.72rem;
  }

  .detail-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.4rem;
  }

  .detail-item {
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
  }
  .detail-label {
    font-size: 0.65rem;
    color: #64748b;
    text-transform: uppercase;
    font-weight: 600;
  }

  .detail-section h4 {
    margin: 0 0 0.3rem;
    color: #94a3b8;
    font-size: 0.75rem;
    font-weight: 600;
  }

  .chip-list {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }

  .port-chip {
    padding: 0.1rem 0.35rem;
    background: rgba(59,130,246,0.1);
    border-radius: 4px;
    color: #93c5fd;
    font-size: 0.68rem;
  }

  .mount-list {
    display: grid;
    gap: 0.2rem;
  }

  .mount-entry {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    font-size: 0.68rem;
    padding: 0.2rem 0.35rem;
    background: rgba(168,85,247,0.08);
    border-radius: 4px;
    color: #c4b5fd;
  }
  .mount-arrow { color: #64748b; }
  .mount-rw { color: #64748b; font-size: 0.62rem; }

  .detail-actions {
    display: flex;
    gap: 0.3rem;
    margin-top: 0.3rem;
  }

  .btn-action {
    padding: 0.3rem 0.6rem;
    background: rgba(148,163,184,0.1);
    border: 1px solid rgba(148,163,184,0.2);
    border-radius: 5px;
    color: #94a3b8;
    font-size: 0.72rem;
    cursor: pointer;
  }
  .btn-action:hover { background: rgba(148,163,184,0.2); }
  .btn-action.stop:hover { background: rgba(239,68,68,0.2); color: #f87171; border-color: rgba(239,68,68,0.3); }
  .btn-action.start:hover { background: rgba(34,197,94,0.2); color: #4ade80; border-color: rgba(34,197,94,0.3); }
  .btn-action.danger:hover { background: rgba(239,68,68,0.2); color: #f87171; border-color: rgba(239,68,68,0.3); }

  /* Stats panel */
  .stats-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.5rem;
  }

  .stat-card {
    background: rgba(15,23,42,0.5);
    border: 1px solid rgba(148,163,184,0.1);
    border-radius: 8px;
    padding: 0.5rem 0.6rem;
  }

  .stat-label {
    font-size: 0.65rem;
    color: #64748b;
    text-transform: uppercase;
    font-weight: 600;
    letter-spacing: 0.03em;
  }

  .stat-value {
    font-size: 1.1rem;
    font-weight: 700;
    color: #e2e8f0;
    margin: 0.1rem 0;
  }

  .stat-detail {
    font-size: 0.68rem;
    color: #94a3b8;
    margin-bottom: 0.25rem;
  }

  .stat-bar {
    height: 4px;
    background: rgba(148,163,184,0.1);
    border-radius: 2px;
    overflow: hidden;
    margin: 0.25rem 0;
  }

  .stat-bar-fill {
    height: 100%;
    border-radius: 2px;
    transition: width 0.3s ease;
  }
  .stat-bar-fill.cpu { background: #22c55e; }
  .stat-bar-fill.mem { background: #8b5cf6; }

  .sparkline {
    width: 100%;
    height: 24px;
    margin-top: 0.2rem;
  }

  .net-row, .net-rx, .net-tx {
    display: inline-flex;
    align-items: center;
    gap: 0.15rem;
  }
  .net-rx { color: #38bdf8; margin-right: 0.5rem; }
  .net-tx { color: #f472b6; }

  .code-output {
    padding: 0.5rem;
    background: rgba(0,0,0,0.4);
    border: 1px solid rgba(148,163,184,0.15);
    border-radius: 6px;
    color: #a5f3fc;
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: 0.7rem;
    white-space: pre-wrap;
    max-height: 350px;
    overflow-y: auto;
  }

  .exec-input-row {
    display: flex;
    gap: 0.3rem;
  }

  .exec-input {
    flex: 1;
    padding: 0.35rem 0.5rem;
    background: rgba(15,23,42,0.8);
    border: 1px solid rgba(148,163,184,0.2);
    border-radius: 5px;
    color: #e2e8f0;
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: 0.75rem;
  }

  /* Compose */
  .compose-list {
    display: grid;
    gap: 0.25rem;
  }

  .compose-file-row {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    background: rgba(15,23,42,0.4);
    border: 1px solid rgba(148,163,184,0.1);
    border-radius: 5px;
    padding: 0.3rem 0.4rem;
  }
  .compose-file-row.selected { border-color: rgba(45,212,191,0.3); }

  .compose-select {
    flex: 1;
    background: none;
    border: none;
    color: #e2e8f0;
    cursor: pointer;
    text-align: left;
    display: flex;
    justify-content: space-between;
    font-size: 0.75rem;
    min-width: 0;
  }

  .compose-path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .compose-size {
    color: #64748b;
    font-size: 0.65rem;
    flex-shrink: 0;
    margin-left: 0.4rem;
  }

  .compose-editor {
    margin-top: 0.3rem;
  }

  .code-editor {
    width: 100%;
    background: rgba(15,23,42,0.8);
    border: 1px solid rgba(148,163,184,0.2);
    border-radius: 5px;
    color: #e2e8f0;
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: 0.72rem;
    padding: 0.4rem;
    resize: vertical;
    box-sizing: border-box;
  }

  .editor-actions {
    display: flex;
    justify-content: flex-end;
    margin-top: 0.3rem;
  }

  /* Prune */
  .prune-desc {
    color: #64748b;
    font-size: 0.75rem;
    margin: 0;
  }

  .prune-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.3rem;
  }

  .prune-btn {
    background: rgba(15,23,42,0.4);
    border: 1px solid rgba(148,163,184,0.15);
    border-radius: 6px;
    padding: 0.5rem;
    cursor: pointer;
    text-align: left;
    color: #e2e8f0;
    transition: border-color 0.15s;
  }
  .prune-btn:hover { border-color: rgba(245,158,11,0.3); }
  .prune-btn.system { grid-column: span 2; }
  .prune-btn strong {
    display: block;
    font-size: 0.75rem;
    margin-bottom: 0.15rem;
  }
  .prune-btn span {
    font-size: 0.65rem;
    color: #64748b;
  }

  /* No selection state */
  .no-selection {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    color: #64748b;
  }
  .no-selection-icon {
    font-size: 2.5rem;
    opacity: 0.5;
  }
  .no-selection h3 {
    margin: 0;
    color: #94a3b8;
    font-size: 0.9rem;
  }
  .no-selection p {
    margin: 0;
    font-size: 0.78rem;
    text-align: center;
    max-width: 280px;
  }

  .engine-summary {
    display: flex;
    gap: 1.2rem;
    margin-top: 0.8rem;
  }
  .engine-stat {
    text-align: center;
  }
  .engine-num {
    display: block;
    font-size: 1.3rem;
    font-weight: 700;
    color: #e2e8f0;
  }
  .engine-label {
    font-size: 0.65rem;
    color: #64748b;
    text-transform: uppercase;
  }

  /* Buttons */
  .btn-sm {
    padding: 0.2rem 0.45rem;
    background: rgba(148,163,184,0.1);
    border: 1px solid rgba(148,163,184,0.2);
    border-radius: 4px;
    color: #94a3b8;
    font-size: 0.7rem;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn-sm:hover { background: rgba(148,163,184,0.2); }
  .btn-sm:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-sm.active { background: rgba(45,212,191,0.15); color: #2dd4bf; border-color: rgba(45,212,191,0.3); }
  .btn-sm.deploy { background: rgba(59,130,246,0.15); color: #93c5fd; border-color: rgba(59,130,246,0.3); }
  .btn-sm.deploy:hover { background: rgba(59,130,246,0.25); }
  .btn-sm.confirm { background: rgba(59,130,246,0.2); color: #93c5fd; }
  .btn-sm.confirm.danger { background: rgba(239,68,68,0.2); color: #f87171; }
  .btn-sm.cancel { background: rgba(148,163,184,0.1); color: #94a3b8; }

  .state-badge {
    padding: 0.1rem 0.35rem;
    border-radius: 3px;
    font-size: 0.65rem;
    font-weight: 600;
    text-transform: uppercase;
  }
  .state-badge.running { background: rgba(34,197,94,0.15); color: #4ade80; }
  .state-badge.exited { background: rgba(239,68,68,0.15); color: #f87171; }
  .state-badge.paused { background: rgba(245,158,11,0.15); color: #fbbf24; }

  code {
    background: rgba(148,163,184,0.1);
    padding: 0.1rem 0.25rem;
    border-radius: 3px;
    font-size: 0.7rem;
    color: #94a3b8;
  }

  /* Confirm dialog */
  .confirm-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .confirm-dialog {
    background: #1e293b;
    border: 1px solid rgba(148,163,184,0.2);
    border-radius: 10px;
    padding: 1.2rem;
    max-width: 360px;
    width: 90%;
  }
  .confirm-dialog h4 {
    margin: 0 0 0.5rem;
    color: #e2e8f0;
  }
  .confirm-dialog p {
    margin: 0 0 0.5rem;
    color: #94a3b8;
    font-size: 0.82rem;
  }
  .confirm-dialog .warning {
    color: #fbbf24;
    font-size: 0.75rem;
  }
  .confirm-buttons {
    display: flex;
    gap: 0.4rem;
    justify-content: flex-end;
    margin-top: 0.8rem;
  }
</style>
