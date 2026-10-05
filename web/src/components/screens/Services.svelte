<script lang="ts">
  import type { Device } from '../../lib/types'
  import { MOCK } from '../../lib/flags'
  import EmptyState from '../ui/EmptyState.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'

  export let device: Device

  type Service = { name: string; state: 'running' | 'failed' | 'stopped'; enabled: boolean; description: string }

  // Sample data, only ever shown when VITE_MOCK_SERVICES=true. It exists so the
  // layout can be reviewed before the endpoint lands; it is never presented as
  // real without the "sample data" banner below.
  const SAMPLE: Service[] = [
    { name: 'ssh.service', state: 'running', enabled: true, description: 'OpenBSD Secure Shell server' },
    { name: 'NetworkManager.service', state: 'running', enabled: true, description: 'Network Manager' },
    { name: 'docker.service', state: 'running', enabled: true, description: 'Docker Application Container Engine' },
    { name: 'bluetooth.service', state: 'stopped', enabled: false, description: 'Bluetooth service' },
    { name: 'cups.service', state: 'failed', enabled: true, description: 'CUPS Scheduler' }
  ]

  let query = ''
  let onlyProblems = false
  let services: Service[] = []

  // Live data is always empty until the endpoint exists; the flag only swaps in
  // the sample.
  $: services = MOCK.services ? SAMPLE : []
  $: usingSample = MOCK.services

  $: filtered = services.filter((s) => {
    if (onlyProblems && s.state === 'running') return false
    if (!query) return true
    const needle = query.toLowerCase()
    return `${s.name} ${s.description}`.toLowerCase().includes(needle)
  })

  function severity(state: Service['state']) {
    return state === 'failed' ? 'crit' : state === 'stopped' ? 'warn' : 'info'
  }
</script>

<section class="services">
  {#if usingSample}
    <div class="sample-banner" role="status">
      Sample data — the services endpoint is not implemented. Set on by
      <code>VITE_MOCK_SERVICES</code>.
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
        </div>
      {/if}
    </header>

    {#if services.length === 0}
      <EmptyState
        icon="⚙"
        title="Service status not collected"
        message="The agent does not enumerate systemd units yet. This screen needs a GET /api/devices/{id}/services endpoint that runs `systemctl list-units` on the agent and returns name, state and enabled flag."
      />
    {:else if filtered.length === 0}
      <EmptyState icon="🔍" title="No match" message="Adjust the filter." />
    {:else}
      <ul class="list">
        {#each filtered as svc (svc.name)}
          <li>
            <SeverityBadge severity={severity(svc.state)} />
            <span class="name mono" title={svc.name}>{svc.name}</span>
            <span class="desc" title={svc.description}>{svc.description}</span>
            <span class="state" class:text-warn={svc.state === 'stopped'} class:text-crit={svc.state === 'failed'}>{svc.state}</span>
            <span class="enabled">{svc.enabled ? 'enabled' : 'disabled'}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </article>

  <article class="card">
    <header class="card-head"><h2>Open ports</h2></header>
    <EmptyState
      icon="🔌"
      title="Not collected yet"
      message="Listening sockets are not reported by the agent. This screen needs a GET /api/devices/{id}/ports endpoint (ss -tulpn) so the dashboard can match a port to its owning service."
    />
  </article>
</section>

<style>
  .services { display: grid; gap: 0.85rem; }

  .sample-banner {
    padding: 0.5rem 0.75rem; border: 1px solid rgba(251, 191, 36, 0.35);
    border-radius: var(--radius-sm); background: var(--warn-dim);
    color: var(--warn); font-size: 0.76rem;
  }
  .sample-banner code { font-family: var(--mono); color: var(--text-bright); }

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

  @media (max-width: 800px) {
    .list li { grid-template-columns: auto 1fr auto; }
    .desc, .enabled { display: none; }
  }
</style>
