<script lang="ts">
  import { onMount } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import EmptyState from '../ui/EmptyState.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'

  export let deviceId: string

  // These mirror the server payloads. The agent maps systemd onto the
  // running/failed/stopped buckets, so `status` is the dashboard's vocabulary
  // and the raw systemd columns are only used as detail.
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

  let query = ''
  let onlyProblems = false
  let portQuery = ''
  let services: Service[] = []
  let ports: Port[] = []
  let loading = true
  let error = ''
  // False until a request has completed, so "never reported" stays
  // distinguishable from "reported nothing".
  let loaded = false

  onMount(load)

  // A device with hundreds of units and sockets changes slowly, and the agent
  // only refreshes its snapshot every five minutes, so this fetches once per
  // device rather than polling.
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
    return `${p.protocol} ${p.local_address} ${p.port} ${p.process || ''}`.toLowerCase().includes(needle)
  })

  // The agent sorts failed units first, so this count comes straight from the
  // data instead of re-deriving the ordering in the browser.
  $: failedCount = services.filter((s) => s.status === 'failed').length
  $: runningCount = services.filter((s) => s.status === 'running').length

  function severity(status: string) {
    return status === 'failed' ? 'crit' : status === 'stopped' ? 'warn' : 'info'
  }

  // A wildcard listener has no routable host; showing "*" reads better than an
  // empty cell next to the port number.
  function host(address: string) {
    return address || '*'
  }
</script>

<section class="services">
  {#if error}
    <div class="error-banner" role="alert">
      {error}
      <button class="retry" on:click={load}>Retry</button>
    </div>
  {/if}

  <article class="card">
    <header class="card-head">
      <h2>Systemd services</h2>
      {#if services.length > 0}
        <div class="tools">
          <input type="search" bind:value={query} placeholder="Filter services" aria-label="Filter services" />
          <label class="check">
            <input type="checkbox" bind:checked={onlyProblems} />
            Problems only
          </label>
          <button class="refresh" on:click={load} disabled={loading}>Refresh</button>
        </div>
      {/if}
    </header>

    {#if services.length > 0}
      <p class="summary">
        <span class="crit">{failedCount} failed</span>
        <span>{runningCount} running</span>
        <span>{services.length} total</span>
      </p>
    {/if}

    {#if loading && !loaded}
      <p class="loading">Loading services…</p>
    {:else if error && services.length === 0}
      <EmptyState icon="⚠" title="Could not load services" message={error} />
    {:else if !loaded}
      <EmptyState
        icon="⚙"
        title="Not collected yet"
        message="This device has not reported a systemd inventory yet. The agent uploads one every five minutes."
      />
    {:else if services.length === 0}
      <EmptyState
        icon="⚙"
        title="No services reported"
        message="The agent collected an empty list. On a system without systemd this is expected."
      />
    {:else if filteredServices.length === 0}
      <EmptyState icon="🔍" title="No match" message="Adjust the filter." />
    {:else}
      <ul class="list">
        {#each filteredServices as svc (svc.name)}
          <li>
            <SeverityBadge severity={severity(svc.status)} />
            <span class="name mono" title={svc.name}>{svc.name}</span>
            <span class="desc" title={svc.description || ''}>{svc.description || svc.sub_state || ''}</span>
            <span class="state" class:text-warn={svc.status === 'stopped'} class:text-crit={svc.status === 'failed'}>{svc.status}</span>
            <span class="enabled" class:on={svc.enabled}>{svc.enabled ? (svc.unit_file_state || 'enabled') : (svc.unit_file_state || 'disabled')}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </article>

  <article class="card">
    <header class="card-head">
      <h2>Open ports</h2>
      {#if ports.length > 0}
        <div class="tools">
          <input type="search" bind:value={portQuery} placeholder="Filter ports" aria-label="Filter ports" />
        </div>
      {/if}
    </header>

    {#if loading && !loaded}
      <p class="loading">Loading ports…</p>
    {:else if error && ports.length === 0}
      <EmptyState icon="🔌" title="Could not load ports" message={error} />
    {:else if !loaded}
      <EmptyState
        icon="🔌"
        title="Not collected yet"
        message="This device has not reported its listening sockets yet. The agent uploads them every five minutes."
      />
    {:else if ports.length === 0}
      <EmptyState
        icon="🔌"
        title="No listening sockets reported"
        message="The agent found no sockets matching the configured states, or ss is not installed."
      />
    {:else if filteredPorts.length === 0}
      <EmptyState icon="🔍" title="No match" message="Adjust the filter." />
    {:else}
      <ul class="ports">
        {#each filteredPorts as p (`${p.protocol}-${p.local_address}-${p.port}`)}
          <li>
            <span class="proto mono">{p.protocol}</span>
            <span class="addr mono">{host(p.local_address)}</span>
            <span class="port mono">{p.port}</span>
            <span class="process" title={p.process ? `${p.process} (pid ${p.pid})` : ''}>{p.process || 'unknown process'}</span>
            {#if p.pid}<span class="pid mono">pid {p.pid}</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  </article>
</section>

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

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }

  .tools { display: flex; align-items: center; gap: 0.6rem; }
  .tools input[type='search'] {
    padding: 0.35rem 0.55rem; background: var(--card-inset); border: 1px solid var(--border);
    border-radius: var(--radius-sm); color: var(--text); font: inherit; font-size: 0.75rem;
  }
  .tools input[type='search']:focus { outline: none; border-color: var(--accent-border); }
  .refresh {
    padding: 0.3rem 0.55rem; background: transparent; border: 1px solid var(--border);
    border-radius: var(--radius-sm); color: var(--text-muted); font: inherit;
    font-size: 0.72rem; cursor: pointer;
  }
  .refresh:hover:not(:disabled) { border-color: var(--accent-border); color: var(--text); }
  .refresh:disabled { opacity: 0.5; cursor: default; }
  .check { display: flex; align-items: center; gap: 0.3rem; color: var(--text-muted); font-size: 0.72rem; }

  .summary { display: flex; gap: 0.9rem; margin: 0; padding: 0.45rem 0.9rem; border-bottom: 1px solid var(--border); color: var(--text-faint); font-size: 0.72rem; }
  .summary .crit { color: var(--crit); }
  .loading { margin: 0; padding: 0.9rem; color: var(--text-faint); font-size: 0.78rem; }

  .list { list-style: none; margin: 0; padding: 0; }
  .list li {
    display: grid; grid-template-columns: auto minmax(140px, 1fr) minmax(0, 2fr) auto auto;
    align-items: center; gap: 0.6rem; padding: 0.5rem 0.9rem;
    border-bottom: 1px solid var(--border); font-size: 0.78rem;
  }
  .list li:last-child { border-bottom: 0; }
  .name { color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .desc { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .state { color: var(--text); text-transform: capitalize; }
  .enabled { color: var(--text-faint); font-size: 0.7rem; }
  .enabled.on { color: var(--text-muted); }

  .ports { list-style: none; margin: 0; padding: 0; }
  .ports li {
    display: grid; grid-template-columns: 3rem minmax(0, 1fr) 4rem minmax(0, 1.4fr) auto;
    align-items: center; gap: 0.6rem; padding: 0.45rem 0.9rem;
    border-bottom: 1px solid var(--border); font-size: 0.78rem;
  }
  .ports li:last-child { border-bottom: 0; }
  .proto { color: var(--text-muted); text-transform: uppercase; font-size: 0.7rem; }
  .addr { color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .port { color: var(--accent); }
  .process { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .pid { color: var(--text-faint); font-size: 0.7rem; }

  @media (max-width: 800px) {
    .list li { grid-template-columns: auto 1fr auto; }
    .desc, .enabled { display: none; }
    .ports li { grid-template-columns: 3rem minmax(0, 1fr) 4rem; }
    .process, .pid { display: none; }
  }
</style>