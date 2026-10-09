<script lang="ts">
  import { onMount } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import EmptyState from '../ui/EmptyState.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'

  export let deviceId: string

  type Service = {
    name: string
    status: string
    load_state?: string
    active_state?: string
    sub_state?: string
    unit_file_state?: string
    description?: string
    enabled: boolean
  }
  type Port = {
    protocol: string
    local_address: string
    port: number
    process?: string
    pid?: number
  }

  // Well-known port names. The agent runs unprivileged and cannot attribute a
  // socket to another user's process, so a name is the honest hint the UI can
  // offer — and it is labelled as a hint, never as fact.
  const WELL_KNOWN: Record<number, string> = {
    20: 'ftp-data', 21: 'ftp', 22: 'ssh', 23: 'telnet', 25: 'smtp', 53: 'dns',
    67: 'dhcp', 68: 'dhcp', 80: 'http', 110: 'pop3', 123: 'ntp', 143: 'imap',
    161: 'snmp', 389: 'ldap', 443: 'https', 445: 'smb', 465: 'smtps',
    514: 'syslog', 587: 'submission', 631: 'ipp', 993: 'imaps', 995: 'pop3s',
    1080: 'socks', 2049: 'nfs', 2375: 'docker', 2376: 'docker-tls',
    2377: 'docker-swarm', 3000: 'http-dev', 3306: 'mysql', 3389: 'rdp',
    4172: 'pulseaudio', 5432: 'postgres', 5672: 'amqp', 5900: 'vnc',
    6379: 'redis', 8080: 'http-alt', 8443: 'https-alt', 9000: 'http-alt',
    9090: 'prometheus', 11211: 'memcached', 27017: 'mongodb'
  }

  let query = ''
  let onlyProblems = false
  let portQuery = ''
  let services: Service[] = []
  let ports: Port[] = []
  let loading = true
  let error = ''
  let loaded = false
  let groupBy: 'name' | 'port' = 'name'

  let selectedService: Service | null = null
  let selectedPort: Port | null = null
  let drawerOpen = false

  onMount(load)

  async function load() {
    if (!deviceId) return
    loading = true
    error = ''
    try {
      const [svcRes, portRes] = await Promise.all([
        apiFetch(apiURL(`/api/devices/${deviceId}/services`)),
        apiFetch(apiURL(`/api/devices/${deviceId}/ports`))
      ])
      if (!svcRes.ok) throw new Error(`services request failed (${svcRes.status})`)
      if (!portRes.ok) throw new Error(`ports request failed (${portRes.status})`)
      services = await svcRes.json()
      ports = await portRes.json()
    } catch (e: any) {
      error = e?.message || 'Failed to load the system inventory'
      services = []
      ports = []
    } finally {
      loading = false
      loaded = true
    }
  }

  $: filteredServices = services.filter((s) => {
    if (onlyProblems && s.status === 'running') return false
    if (!query) return true
    const needle = query.toLowerCase()
    return `${s.name} ${s.description || ''} ${s.active_state || ''}`.toLowerCase().includes(needle)
  })

  $: filteredPorts = ports.filter((p) => {
    if (!portQuery) return true
    const needle = portQuery.toLowerCase()
    return `${p.protocol} ${p.local_address} ${p.port} ${p.process || ''} ${WELL_KNOWN[p.port] || ''}`.toLowerCase().includes(needle)
  })

  $: failedCount = services.filter((s) => s.status === 'failed').length
  $: runningCount = services.filter((s) => s.status === 'running').length
  $: stoppedCount = services.length - failedCount - runningCount
  $: exposedCount = ports.filter((p) => isPublic(p.local_address)).length

  function severity(status: string): 'crit' | 'warn' | 'info' {
    return status === 'failed' ? 'crit' : status === 'stopped' ? 'warn' : 'info'
  }

  function host(address: string): string {
    return address || '*'
  }

  // A listener on 0.0.0.0 / :: / empty is reachable from off-host, which is the
  // distinction a services/ports view exists to surface.
  function isPublic(address: string): boolean {
    return address === '' || address === '0.0.0.0' || address === '::' || address === '*' || address === '[::]'
  }

  function serviceBase(name: string): string {
    // Strip the unit type and any template/instance suffix (`foo@bar.service`).
    return name.replace(/\.service$/, '').replace(/@.*$/, '')
  }

  // Best-effort attribution between a unit and the sockets it might own. Either
  // side naming the other is enough; anything else is left unattributed rather
  // than guessed.
  $: relatedPorts = selectedService
    ? (() => {
        const base = serviceBase(selectedService!.name).toLowerCase()
        if (!base) return []
        return ports.filter((p) => {
          const known = (WELL_KNOWN[p.port] || '').toLowerCase()
          const proc = (p.process || '').toLowerCase()
          if (proc && (proc.includes(base) || base.includes(proc))) return true
          if (!known) return false
          return known === base || known.includes(base) || base.includes(known)
        })
      })()
    : []

  function selectService(s: Service) {
    selectedService = s
    selectedPort = null
    drawerOpen = true
  }

  function selectPort(p: Port) {
    selectedPort = p
    selectedService = null
    drawerOpen = true
  }

  function closeDrawer() {
    drawerOpen = false
    selectedService = null
    selectedPort = null
  }

  function matchServiceForPort(p: Port): Service | null {
    const known = (WELL_KNOWN[p.port] || '').toLowerCase()
    if (!known) return null
    return services.find((s) => serviceBase(s.name).toLowerCase() === known) || null
  }
</script>

<section class="services">
  {#if error}
    <div class="error-banner" role="alert">
      {error}
      <button class="retry" on:click={load}>Retry</button>
    </div>
  {/if}

  <div class="tiles">
    <article class="tile"><span class="label">Running</span><span class="value">{runningCount}</span></article>
    <article class="tile"><span class="label">Failed</span><span class="value" class:text-crit={failedCount > 0}>{failedCount}</span></article>
    <article class="tile"><span class="label">Stopped</span><span class="value" class:text-warn={stoppedCount > 0}>{stoppedCount}</span></article>
    <article class="tile"><span class="label">Listening</span><span class="value">{ports.length}</span><span class="sub">{exposedCount} public</span></article>
  </div>

  {#if loading && !loaded}
    <p class="loading">Loading system inventory…</p>
  {:else}
    <div class="split">
      <article class="card">
        <header class="card-head">
          <h2>Systemd services</h2>
          <div class="tools">
            <input type="search" bind:value={query} placeholder="Filter services" aria-label="Filter services" />
            <label class="check">
              <input type="checkbox" bind:checked={onlyProblems} />
              Problems
            </label>
          </div>
        </header>

        {#if error && services.length === 0}
          <EmptyState icon="⚠" title="Could not load services" message={error} />
        {:else if services.length === 0}
          <EmptyState
            icon="⚙"
            title="No services reported"
            message="This device has not reported a systemd inventory, or it has no systemd."
          />
        {:else if filteredServices.length === 0}
          <EmptyState icon="🔍" title="No match" message="Adjust the filter." />
        {:else}
          <ul class="list">
            {#each filteredServices as svc (svc.name)}
              <li>
                <button class="row" class:sel={selectedService?.name === svc.name} on:click={() => selectService(svc)}>
                  <SeverityBadge severity={severity(svc.status)} />
                  <span class="name mono" title={svc.name}>{svc.name}</span>
                  <span class="state" class:text-warn={svc.status === 'stopped'} class:text-crit={svc.status === 'failed'}>{svc.status}</span>
                  <span class="enabled" class:on={svc.enabled}>{svc.unit_file_state || (svc.enabled ? 'enabled' : 'disabled')}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </article>

      <article class="card">
        <header class="card-head">
          <h2>Open ports</h2>
          <div class="tools">
            <input type="search" bind:value={portQuery} placeholder="Filter ports" aria-label="Filter ports" />
          </div>
        </header>

        {#if error && ports.length === 0}
          <EmptyState icon="⚠" title="Could not load ports" message={error} />
        {:else if ports.length === 0}
          <EmptyState icon="🔌" title="No listening sockets" message="The agent found no sockets, or ss is not installed." />
        {:else if filteredPorts.length === 0}
          <EmptyState icon="🔍" title="No match" message="Adjust the filter." />
        {:else}
          <ul class="ports">
            {#each filteredPorts as p (`${p.protocol}-${p.local_address}-${p.port}`)}
              <li>
                <button class="row" class:sel={selectedPort && selectedPort.port === p.port && selectedPort.protocol === p.protocol && selectedPort.local_address === p.local_address} on:click={() => selectPort(p)}>
                  <span class="port mono">{p.port}</span>
                  <span class="proto mono">{p.protocol}</span>
                  <span class="addr mono">{host(p.local_address)}</span>
                  <span class="svc">{p.process || WELL_KNOWN[p.port] || '—'}</span>
                  {#if isPublic(p.local_address)}<span class="exposed" title="Reachable from off-host">public</span>{/if}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </article>
    </div>
  {/if}
</section>

{#if drawerOpen && (selectedService || selectedPort)}
  <div class="backdrop" role="presentation" on:click={closeDrawer}></div>
  <aside class="drawer" aria-label="Details">
    <header class="drawer-head">
      <h3>{selectedService ? 'Service' : 'Listening socket'}</h3>
      <button class="close" on:click={closeDrawer} aria-label="Close details">✕</button>
    </header>

    {#if selectedService}
      <div class="drawer-name mono">{selectedService.name}</div>
      {#if selectedService.description}<p class="drawer-desc">{selectedService.description}</p>{/if}

      <dl class="facts">
        <div><dt>State</dt><dd class:text-crit={selectedService.status === 'failed'} class:text-warn={selectedService.status === 'stopped'}>{selectedService.status}</dd></div>
        <div><dt>Enabled</dt><dd>{selectedService.enabled ? (selectedService.unit_file_state || 'enabled') : (selectedService.unit_file_state || 'disabled')}</dd></div>
        <div><dt>Active</dt><dd class="mono">{selectedService.active_state || '—'}</dd></div>
        <div><dt>Sub</dt><dd class="mono">{selectedService.sub_state || '—'}</dd></div>
        <div><dt>Load</dt><dd class="mono">{selectedService.load_state || '—'}</dd></div>
      </dl>

      <h4 class="drawer-sub">Related open ports</h4>
      {#if relatedPorts.length > 0}
        <ul class="rel">
          {#each relatedPorts as p (`${p.protocol}-${p.local_address}-${p.port}`)}
            <li><span class="mono port">{p.port}</span><span class="proto mono">{p.protocol}</span><span class="addr mono">{host(p.local_address)}</span></li>
          {/each}
        </ul>
      {:else}
        <p class="drawer-note">
          The agent runs unprivileged and cannot read other users' socket owners, so no listening
          socket can be attributed to this unit automatically.
        </p>
      {/if}
    {:else if selectedPort}
      <div class="drawer-name mono">{host(selectedPort.local_address)}:{selectedPort.port}</div>
      {#if isPublic(selectedPort.local_address)}
        <p class="drawer-note warn">This socket is bound to {host(selectedPort.local_address)}, so it is reachable from off-host.</p>
      {/if}

      <dl class="facts">
        <div><dt>Protocol</dt><dd class="mono">{selectedPort.protocol.toUpperCase()}</dd></div>
        <div><dt>Address</dt><dd class="mono">{host(selectedPort.local_address)}</dd></div>
        <div><dt>Port</dt><dd class="mono">{selectedPort.port}</dd></div>
        <div><dt>Process</dt><dd class="mono">{selectedPort.process || WELL_KNOWN[selectedPort.port] || 'unknown'}</dd></div>
        <div><dt>PID</dt><dd class="mono">{selectedPort.pid ?? '—'}</dd></div>
      </dl>

      {@const svc = matchServiceForPort(selectedPort)}
      <h4 class="drawer-sub">Matching unit</h4>
      {#if svc}
        <button class="drawer-link mono" on:click={() => selectService(svc)}>{svc.name}</button>
      {:else}
        <p class="drawer-note">No systemd unit matches this port by name.</p>
      {/if}
    {/if}
  </aside>
{/if}

<style>
  .services { display: grid; gap: 0.85rem; }

  .error-banner {
    display: flex; align-items: center; gap: 0.6rem; padding: 0.5rem 0.75rem;
    border: 1px solid rgba(248, 113, 113, 0.35); border-radius: var(--radius-sm);
    background: var(--crit-dim); color: var(--crit); font-size: 0.76rem;
  }
  .retry {
    margin-left: auto; padding: 0.2rem 0.5rem; background: transparent;
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    color: var(--text); font: inherit; font-size: 0.72rem; cursor: pointer;
  }
  .retry:hover { border-color: var(--accent-border); }

  .tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 0.6rem; }
  .tile {
    display: grid; gap: 0.15rem; padding: 0.7rem 0.85rem; background: var(--card);
    border: 1px solid var(--border); border-radius: var(--radius);
  }
  .tile .value { font-size: 1.4rem; color: var(--text-bright); font-variant-numeric: tabular-nums; }
  .tile .sub { color: var(--text-faint); font-size: 0.7rem; }

  .split { display: grid; grid-template-columns: 1fr 1fr; gap: 0.85rem; align-items: start; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }

  .tools { display: flex; align-items: center; gap: 0.6rem; }
  .tools input[type='search'] {
    padding: 0.35rem 0.55rem; background: var(--card-inset); border: 1px solid var(--border);
    border-radius: var(--radius-sm); color: var(--text); font: inherit; font-size: 0.75rem;
  }
  .tools input[type='search']:focus { outline: none; border-color: var(--accent-border); }
  .check { display: flex; align-items: center; gap: 0.3rem; color: var(--text-muted); font-size: 0.72rem; }

  .loading { margin: 0; padding: 0.9rem; color: var(--text-faint); font-size: 0.78rem; }

  .list, .ports { list-style: none; margin: 0; padding: 0; max-height: 62vh; overflow-y: auto; }
  .row {
    display: grid; align-items: center; gap: 0.55rem; width: 100%;
    padding: 0.5rem 0.9rem; border: 0; border-bottom: 1px solid var(--border);
    background: transparent; color: var(--text); text-align: left; cursor: pointer;
    font-size: 0.78rem; transition: background var(--t-fast);
  }
  .row:hover { background: var(--card-hover); }
  .row.sel { background: var(--accent-dim); box-shadow: inset 2px 0 0 var(--accent); }
  .list .row { grid-template-columns: auto minmax(120px, 1.4fr) auto auto; }
  .ports .row { grid-template-columns: 4rem 3rem minmax(0, 1fr) minmax(0, 1.2fr) auto; }

  .name { color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .state { color: var(--text); text-transform: capitalize; }
  .enabled { color: var(--text-faint); font-size: 0.7rem; }
  .enabled.on { color: var(--text-muted); }

  .proto { color: var(--text-muted); text-transform: uppercase; font-size: 0.68rem; }
  .addr { color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .port { color: var(--accent); }
  .svc { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .exposed {
    padding: 0.05rem 0.4rem; border-radius: var(--radius-pill);
    background: var(--warn-dim); color: var(--warn); font-size: 0.62rem; font-weight: 600;
  }

  .backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); z-index: 40; }
  .drawer {
    position: fixed; top: 0; right: 0; height: 100vh; width: min(380px, 92vw);
    background: var(--bg-elevated); border-left: 1px solid var(--border);
    box-shadow: var(--shadow-pop); z-index: 41; padding: 1rem; overflow-y: auto;
    animation: slide-in 180ms ease;
  }
  @keyframes slide-in { from { transform: translateX(16px); opacity: 0; } to { transform: none; opacity: 1; } }

  .drawer-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 0.6rem; }
  .drawer-head h3 { margin: 0; font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-faint); }
  .close { border: 0; background: transparent; color: var(--text-muted); cursor: pointer; font-size: 0.9rem; padding: 0.2rem; }
  .close:hover { color: var(--text); }

  .drawer-name { color: var(--text-bright); font-size: 0.95rem; font-weight: 600; word-break: break-all; }
  .drawer-desc { color: var(--text-muted); font-size: 0.78rem; margin: 0.3rem 0 0.6rem; }

  .facts { display: grid; gap: 0.4rem; margin: 0.8rem 0; }
  .facts > div { display: grid; grid-template-columns: 5rem 1fr; gap: 0.5rem; align-items: baseline; }
  .facts dt { color: var(--text-faint); font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.04em; }
  .facts dd { margin: 0; color: var(--text); font-size: 0.8rem; }

  .drawer-sub { margin: 1rem 0 0.4rem; font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-faint); }
  .rel { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
  .rel li { display: flex; gap: 0.6rem; align-items: baseline; font-size: 0.78rem; }
  .drawer-note { color: var(--text-faint); font-size: 0.72rem; line-height: 1.5; }
  .drawer-note.warn { color: var(--warn); }
  .drawer-link { border: 1px solid var(--border); background: var(--card-inset); color: var(--accent); border-radius: var(--radius-sm); padding: 0.35rem 0.6rem; cursor: pointer; font: inherit; }
  .drawer-link:hover { border-color: var(--accent-border); }

  @media (max-width: 900px) {
    .split { grid-template-columns: 1fr; }
  }
</style>
