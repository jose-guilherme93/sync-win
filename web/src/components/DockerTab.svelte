<script lang="ts">
  import { onMount } from 'svelte'
  import { formatRelative } from '../lib/format'
  import {
    containerCounts,
    filterContainers,
    portsSummary,
    stateTone,
    type DockerContainer,
    type DockerInfo,
    type DockerState,
    type DockerStatusFilter
  } from '../lib/docker'
  import { fetchContainerList, fetchDockerState, openDockerStream, runDockerCommand } from '../lib/docker-client'
  import type { HardwareStats } from '../lib/types'
  import ContainerDrawer from './docker/ContainerDrawer.svelte'
  import ComposeCard from './docker/ComposeCard.svelte'
  import CleanupCard from './docker/CleanupCard.svelte'
  import ConfirmDialog from './ui/ConfirmDialog.svelte'
  import EmptyState from './ui/EmptyState.svelte'
  import Icon from './ui/Icon.svelte'
  import Skeleton from './ui/Skeleton.svelte'
  import StatusPill from './ui/StatusPill.svelte'

  export let deviceId: string
  export let authHeaders: Record<string, string> = {}
  // The screen already has the hardware sample when it renders. Using it for
  // the engine line and counts means the header is populated on the first
  // paint instead of after the Docker state request returns.
  export let hardware: HardwareStats | undefined = undefined

  type ActionName = 'start' | 'stop' | 'restart' | 'kill' | 'remove'

  let containers: DockerContainer[] = []
  let info: DockerInfo | null = null
  let available = true
  let loaded = false
  let loading = true
  let refreshing = false
  let error = ''
  let updatedAt = 0
  let now = Date.now()
  let streamLive = false

  let query = ''
  let statusFilter: DockerStatusFilter = 'all'
  let selectedId = ''

  let actionBusyId = ''
  let pendingAction: { action: ActionName; container: DockerContainer } | null = null
  let notice = ''
  let noticeTimer: ReturnType<typeof setTimeout> | null = null
  let confirmDialog = { title: '', message: '', label: '', destructive: false }

  let clockTimer: ReturnType<typeof setInterval> | null = null
  let closeStream: (() => void) | null = null
  let currentDevice = ''

  $: counts = containerCounts(containers)
  $: engine = info || hardware?.docker_info || null
  $: engineAvailable = loaded ? available : hardware?.docker_available !== false
  $: filtered = filterContainers(containers, query, statusFilter)
  $: selectedContainer = containers.find((container) => container.id === selectedId) || null
  $: syncedLabel = updatedAt ? formatRelative(updatedAt, now) : ''

  // Tiles show the live list once it exists and fall back to the engine summary
  // in the hardware sample for the first paint.
  $: runningCount = loaded ? counts.running : (engine?.running ?? 0)
  $: stoppedCount = loaded ? counts.stopped : (engine?.stopped ?? 0)
  $: pausedCount = loaded ? counts.paused : (engine?.paused ?? 0)

  $: if (currentDevice && deviceId !== currentDevice) {
    currentDevice = deviceId
    start()
  }

  onMount(() => {
    currentDevice = deviceId
    start()
    clockTimer = setInterval(() => (now = Date.now()), 10_000)
    return () => {
      if (clockTimer) clearInterval(clockTimer)
      if (noticeTimer) clearTimeout(noticeTimer)
      closeStream?.()
      closeStream = null
    }
  })

  function start() {
    closeStream?.()
    closeStream = null
    containers = []
    info = null
    available = true
    loaded = false
    loading = true
    error = ''
    selectedId = ''
    updatedAt = 0
    streamLive = false

    closeStream = openDockerStream(deviceId, {
      onState: applyState,
      onOpen: () => (streamLive = true),
      onError: () => (streamLive = false)
    })
    void loadCached()
  }

  function applyState(state: DockerState) {
    containers = state.containers
    if (state.info) info = state.info
    available = state.available
    loaded = true
    updatedAt = Date.now()
    if (selectedId && !containers.some((container) => container.id === selectedId)) selectedId = ''
  }

  async function loadCached() {
    loading = true
    try {
      applyState(await fetchDockerState(deviceId, { headers: authHeaders }))
    } catch (e: any) {
      error = e?.message || 'Could not load the Docker state for this device'
    } finally {
      loading = false
      loaded = true
    }
    // The cached list is sampled by the agent about once a minute, and a device
    // that has never been sampled has an empty cache. Asking the device is what
    // turns "the Docker screen is empty/slow" into a couple of seconds.
    if (available) void refreshList()
  }

  async function refreshList() {
    if (refreshing) return
    refreshing = true
    error = ''
    try {
      const list = await fetchContainerList(deviceId, { headers: authHeaders, timeoutMs: 45_000 })
      containers = list
      available = true
      loaded = true
      updatedAt = Date.now()
      if (selectedId && !list.some((container) => container.id === selectedId)) selectedId = ''
    } catch (e: any) {
      error = e?.message || 'The device did not return its container list'
    } finally {
      refreshing = false
    }
  }

  function quickAction(action: ActionName, container: DockerContainer) {
    if (action === 'stop' || action === 'restart' || action === 'remove') {
      pendingAction = { action, container }
      return
    }
    void performAction(action, container)
  }

  async function performAction(action: ActionName, container: DockerContainer) {
    actionBusyId = container.id
    error = ''
    try {
      await runDockerCommand(deviceId, action, container.id, '', {
        headers: authHeaders,
        timeoutMs: 60_000
      })
      if (action === 'remove') selectedId = ''
      flash(actionNotice(action, container.name))
      // A mutation is followed by a real list command: waiting for the agent's
      // minute sample would leave the row showing the state it just left.
      await refreshList()
    } catch (e: any) {
      error = e?.message || `Could not ${action} ${container.name}`
    } finally {
      actionBusyId = ''
      pendingAction = null
    }
  }

  function actionNotice(action: ActionName, name: string): string {
    switch (action) {
      case 'start':
        return `Started ${name}`
      case 'stop':
        return `Stopped ${name}`
      case 'restart':
        return `Restarted ${name}`
      case 'kill':
        return `Killed ${name}`
      case 'remove':
        return `Removed ${name}`
      default:
        return 'Done'
    }
  }

  function flash(message: string) {
    notice = message
    if (noticeTimer) clearTimeout(noticeTimer)
    noticeTimer = setTimeout(() => (notice = ''), 4_000)
  }

  function stateLabel(state: string): string {
    return state.replace(/_/g, ' ')
  }

  function openDetails(container: DockerContainer) {
    selectedId = container.id
  }

  function closeDetails() {
    selectedId = ''
  }

  function confirmCopy(action: { action: ActionName; container: DockerContainer } | null): { title: string; message: string; label: string; destructive: boolean } {
    if (!action) return { title: '', message: '', label: '', destructive: false }
    const name = action.container.name
    if (action.action === 'remove') {
      return {
        title: 'Remove container?',
        message: `This deletes ${name} and its writable layer. Named volumes and the image are kept.`,
        label: 'Remove',
        destructive: true
      }
    }
    if (action.action === 'stop') {
      return {
        title: 'Stop container?',
        message: `${name} will stop and its processes will end. The container stays on disk and can be started again.`,
        label: 'Stop',
        destructive: false
      }
    }
    return {
      title: 'Restart container?',
      message: `${name} will restart. Anything it is serving will be briefly unavailable.`,
      label: 'Restart',
      destructive: false
    }
  }

  // The argument is load-bearing: a dependency read only inside a called
  // function is invisible to Svelte's analysis, so this reactive statement
  // would never re-run and the dialog would open blank.
  $: confirmDialog = confirmCopy(pendingAction)
</script>

<section class="docker-tab">
  {#if error}
    <div class="docker-error" role="alert">
      <Icon name="alert" size={14} />
      <span>{error}</span>
      <button on:click={() => void refreshList()} disabled={refreshing}>Retry</button>
    </div>
  {/if}
  {#if notice}
    <div class="notice" role="status">{notice}</div>
  {/if}

  <div class="tiles">
    <article class="tile">
      <span class="label">Running</span>
      <span class="value" class:text-ok={runningCount > 0}>{runningCount}</span>
      <span class="sub">{refreshing ? 'syncing with device…' : loaded ? 'from this device' : 'last engine sample'}</span>
    </article>
    <article class="tile">
      <span class="label">Stopped</span>
      <span class="value">{stoppedCount}</span>
      <span class="sub">containers on disk</span>
    </article>
    <article class="tile">
      <span class="label">Paused</span>
      <span class="value" class:text-warn={pausedCount > 0}>{pausedCount}</span>
      <span class="sub">{pausedCount > 0 ? 'needs attention' : 'none paused'}</span>
    </article>
    <article class="tile">
      <span class="label">Images</span>
      <span class="value">{engine?.images ?? 0}</span>
      <span class="sub">{engine?.version ? `engine ${engine.version}` : 'engine not reported'}</span>
    </article>
  </div>

  <article class="containers-card">
    <header class="card-head">
      <div class="head-left">
        <h2>Containers</h2>
        <span class="engine-line">
          {#if engine?.driver}{engine.driver} driver{/if}
          {#if engine?.ncpu}· {engine.ncpu} CPUs{/if}
          {#if syncedLabel}· synced {syncedLabel}{/if}
        </span>
      </div>
      <div class="head-right">
        <span class="live" class:on={streamLive} title={streamLive ? 'Updates arrive over the live stream' : 'The live stream is reconnecting'}>
          <i></i>{streamLive ? 'Live' : 'Reconnecting'}
        </span>
        <button class="btn-ghost" on:click={() => void refreshList()} disabled={refreshing}>
          <Icon name="refresh" size={13} /> {refreshing ? 'Asking device…' : 'Refresh'}
        </button>
      </div>
    </header>

    <div class="list-toolbar">
      <div class="search">
        <Icon name="search" size={14} />
        <input type="search" bind:value={query} placeholder="Search name, image, id or port" aria-label="Filter containers" />
        {#if query}
          <button class="clear" on:click={() => (query = '')} aria-label="Clear filter"><Icon name="x" size={12} /></button>
        {/if}
      </div>
      <div class="segmented" role="group" aria-label="Filter by container state">
        <button class:active={statusFilter === 'all'} aria-pressed={statusFilter === 'all'} on:click={() => (statusFilter = 'all')}>
          All <b>{counts.total}</b>
        </button>
        <button class:active={statusFilter === 'running'} aria-pressed={statusFilter === 'running'} on:click={() => (statusFilter = 'running')}>
          Running <b>{counts.running}</b>
        </button>
        <button class:active={statusFilter === 'stopped'} aria-pressed={statusFilter === 'stopped'} on:click={() => (statusFilter = 'stopped')}>
          Stopped <b>{counts.stopped}</b>
        </button>
        <button class:active={statusFilter === 'paused'} aria-pressed={statusFilter === 'paused'} on:click={() => (statusFilter = 'paused')}>
          Paused <b>{counts.paused}</b>
        </button>
      </div>
    </div>

    {#if !engineAvailable}
      <EmptyState
        icon="🐳"
        title="Docker not detected"
        message="The agent could not reach /var/run/docker.sock on this device. Install Docker, or add the agent's user to the docker group, then scan again."
        actionLabel="Scan again"
        onAction={() => void loadCached()}
      />
    {:else if loading && !loaded}
      <div class="pad"><Skeleton variant="list" rows={5} /></div>
    {:else if loaded && containers.length === 0}
      <EmptyState
        icon="📦"
        title="No containers"
        message="Docker is running on this device, but there are no containers — running or stopped."
      />
    {:else if filtered.length === 0}
      <EmptyState icon="🔍" title="No match" message="Adjust the search or the state filter." />
    {:else}
      <ul class="rows">
        {#each filtered as container (container.id)}
          <li class="row" class:sel={selectedId === container.id}>
            <button class="row-main" on:click={() => openDetails(container)} aria-label={`Open details for ${container.name}`}>
              <StatusPill label={stateLabel(container.state)} tone={stateTone(container.state)} />
              <span class="ident">
                <span class="name mono">{container.name}</span>
                <span class="image">{container.image}</span>
              </span>
              <span class="ports" class:no-ports={container.ports.length === 0}>
                {portsSummary(container) || 'no ports'}
              </span>
              <span class="state-text" title={container.status}>{container.status || '—'}</span>
              <span class="chev"><Icon name="chevron" size={15} /></span>
            </button>
            <div class="row-actions">
              {#if container.state === 'running'}
                <button class="btn-ghost" disabled={actionBusyId === container.id} on:click={() => quickAction('stop', container)}>
                  {actionBusyId === container.id ? 'Working…' : 'Stop'}
                </button>
              {:else if container.state === 'exited' || container.state === 'created' || container.state === 'dead'}
                <button class="btn-ghost accent" disabled={actionBusyId === container.id} on:click={() => quickAction('start', container)}>
                  {actionBusyId === container.id ? 'Working…' : 'Start'}
                </button>
              {/if}
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </article>

  {#if engineAvailable}
    <div class="bottom">
      <ComposeCard {deviceId} {authHeaders} onChanged={() => void refreshList()} />
      <CleanupCard {deviceId} {authHeaders} onChanged={() => void refreshList()} />
    </div>
  {/if}
</section>

{#if selectedContainer}
  <ContainerDrawer
    container={selectedContainer}
    {deviceId}
    {authHeaders}
    busy={actionBusyId === selectedContainer.id}
    onClose={closeDetails}
    onAction={(action, container) => quickAction(action, container)}
  />
{/if}

<ConfirmDialog
  open={pendingAction != null}
  title={confirmDialog.title}
  message={confirmDialog.message}
  confirmLabel={confirmDialog.label}
  destructive={confirmDialog.destructive}
  busy={actionBusyId === pendingAction?.container.id}
  on:confirm={() => pendingAction && void performAction(pendingAction.action, pendingAction.container)}
  on:cancel={() => (pendingAction = null)}
/>

<style>
  .docker-tab { display: grid; gap: 0.85rem; }

  .docker-error {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.5rem 0.75rem;
    border: 1px solid rgba(248, 113, 113, 0.35);
    border-radius: var(--radius-sm);
    background: var(--crit-dim);
    color: var(--crit);
    font-size: 0.76rem;
  }
  .docker-error span { flex: 1; min-width: 0; }
  .docker-error button {
    border: 1px solid rgba(248, 113, 113, 0.4);
    background: transparent;
    color: var(--crit);
    border-radius: var(--radius-sm);
    padding: 0.2rem 0.55rem;
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
  }

  .notice {
    padding: 0.5rem 0.75rem;
    border: 1px solid var(--accent-border);
    border-radius: var(--radius-sm);
    background: var(--accent-dim);
    color: var(--accent);
    font-size: 0.76rem;
  }

  .tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 0.6rem; }
  .tile {
    display: grid;
    gap: 0.15rem;
    padding: 0.7rem 0.85rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }
  .tile .value { font-size: 1.4rem; color: var(--text-bright); font-variant-numeric: tabular-nums; }
  .tile .sub { color: var(--text-faint); font-size: 0.7rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .containers-card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }

  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.6rem;
    flex-wrap: wrap;
    padding: 0.7rem 0.9rem;
    border-bottom: 1px solid var(--border);
  }
  .head-left { display: flex; align-items: baseline; gap: 0.6rem; flex-wrap: wrap; min-width: 0; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .engine-line { color: var(--text-faint); font-size: 0.7rem; }
  .head-right { display: flex; align-items: center; gap: 0.5rem; }

  .live { display: inline-flex; align-items: center; gap: 0.3rem; color: var(--text-faint); font-size: 0.68rem; font-weight: 600; }
  .live i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
  .live.on { color: var(--accent); }

  .list-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.6rem;
    flex-wrap: wrap;
    padding: 0.55rem 0.9rem;
    border-bottom: 1px solid var(--border);
  }

  .search {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex: 1 1 14rem;
    min-width: 0;
    max-width: 24rem;
    padding: 0.28rem 0.5rem;
    background: var(--card-inset);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    color: var(--text-faint);
  }
  .search:focus-within { border-color: var(--accent-border); }
  .search input {
    flex: 1;
    min-width: 0;
    border: 0;
    background: transparent;
    color: var(--text);
    font: inherit;
    font-size: 0.76rem;
    padding: 0.15rem 0;
  }
  .search input:focus { outline: none; }
  .clear { border: 0; background: transparent; color: var(--text-faint); cursor: pointer; padding: 0.1rem; display: inline-flex; }
  .clear:hover { color: var(--text); }

  .segmented { display: inline-flex; border: 1px solid var(--border-strong); border-radius: var(--radius-sm); overflow: hidden; }
  .segmented button {
    border: 0;
    border-right: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    padding: 0.32rem 0.55rem;
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .segmented button:last-child { border-right: 0; }
  .segmented button:hover { background: var(--card-hover); color: var(--text); }
  .segmented button.active { background: var(--accent-dim); color: var(--accent); }
  .segmented b { margin-left: 0.2rem; font-variant-numeric: tabular-nums; color: var(--text-faint); font-weight: 600; }
  .segmented button.active b { color: var(--accent); }

  .pad { padding: 0.85rem 0.9rem; }

  .rows { list-style: none; margin: 0; padding: 0; max-height: 58vh; overflow-y: auto; }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    border-bottom: 1px solid var(--border);
    transition: background var(--t-fast);
  }
  .row:last-child { border-bottom: 0; }
  .row:hover { background: var(--card-hover); }
  .row.sel { background: var(--accent-dim); box-shadow: inset 2px 0 0 var(--accent); }

  .row-main {
    display: grid;
    grid-template-columns: auto minmax(9rem, 1.6fr) minmax(0, 1fr) minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.6rem;
    width: 100%;
    min-width: 0;
    padding: 0.55rem 0.9rem;
    border: 0;
    background: transparent;
    color: var(--text);
    text-align: left;
    cursor: pointer;
    font-size: 0.78rem;
  }
  .row-main:focus-visible { outline-offset: -2px; }

  .ident { display: grid; gap: 0.05rem; min-width: 0; }
  .name { color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .image { color: var(--text-faint); font-size: 0.68rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ports { color: var(--text-muted); font-size: 0.72rem; font-variant-numeric: tabular-nums; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ports.no-ports { color: var(--text-faint); }
  .state-text { color: var(--text-muted); font-size: 0.72rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .chev { color: var(--text-faint); display: inline-flex; }

  .row-actions { display: flex; gap: 0.35rem; padding-right: 0.8rem; }

  .btn-ghost {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.28rem 0.55rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text);
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn-ghost:hover:not(:disabled) { background: var(--card-hover); }
  .btn-ghost.accent { color: var(--accent); border-color: var(--accent-border); }
  .btn-ghost:disabled { opacity: 0.5; cursor: default; }

  .bottom { display: grid; grid-template-columns: minmax(0, 1.15fr) minmax(0, 0.85fr); gap: 0.85rem; align-items: start; }

  @media (max-width: 980px) {
    .bottom { grid-template-columns: 1fr; }
    .row-main { grid-template-columns: auto minmax(0, 1.6fr) auto; }
    .ports, .state-text { display: none; }
    .row-actions { padding-right: 0.6rem; }
  }

  @media (max-width: 620px) {
    .row-main { grid-template-columns: auto minmax(0, 1fr) auto; padding: 0.5rem 0.6rem; }
    .row-actions { display: none; }
    .list-toolbar { padding: 0.5rem 0.6rem; }
  }
</style>
