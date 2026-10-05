<script lang="ts">
  import StatusDot from '../ui/StatusDot.svelte'
  import Icon from '../ui/Icon.svelte'
  import { computedStatus, deviceLabel, memoryPercent, type Device } from '../../lib/types'

  // The device list inside the sidebar. Selecting a row is the only navigation
  // gesture for switching device, which is what replaces the old click-to-open
  // modal.
  export let devices: Device[] = []
  export let selectedId: string | null = null
  export let onSelect: (id: string) => void
  export let onAdd: () => void

  let query = ''
  let statusFilter: 'all' | 'online' | 'attention' | 'offline' = 'all'

  $: filtered = devices.filter((device) => {
    if (query) {
      const needle = query.toLowerCase()
      const hay = `${deviceLabel(device)} ${device.hostname} ${device.user_id}`.toLowerCase()
      if (!hay.includes(needle)) return false
    }
    const status = computedStatus(device)
    if (statusFilter === 'all') return true
    if (statusFilter === 'online') return status === 'online'
    // "attention" groups everything that is reachable but unhealthy, so the user
    // does not have to check two separate filters.
    if (statusFilter === 'attention') return status === 'stale' || status === 'error' || status === 'duplicate'
    return status === 'offline'
  })

  $: counts = {
    all: devices.length,
    online: devices.filter((d) => computedStatus(d) === 'online').length,
    attention: devices.filter((d) => ['stale', 'error', 'duplicate'].includes(computedStatus(d))).length,
    offline: devices.filter((d) => computedStatus(d) === 'offline').length
  }
</script>

<div class="device-list">
  <div class="search">
    <Icon name="search" size={14} />
    <input bind:value={query} type="search" placeholder="Search devices" aria-label="Search devices" />
  </div>

  <div class="filters" role="group" aria-label="Filter by status">
    {#each [['all', 'All'], ['online', 'Online'], ['attention', 'Attention'], ['offline', 'Offline']] as [key, label] (key)}
      <button
        class:active={statusFilter === key}
        on:click={() => (statusFilter = key as typeof statusFilter)}
        title={`${counts[key as keyof typeof counts]} device(s)`}
      >
        {label}
      </button>
    {/each}
  </div>

  <ul class="rows">
    {#each filtered as device (device.id)}
      {@const status = computedStatus(device)}
      {@const hw = device.hardware}
      <li>
        <button
          class="row"
          class:selected={device.id === selectedId}
          on:click={() => onSelect(device.id)}
          aria-current={device.id === selectedId ? 'true' : undefined}
        >
          <StatusDot {status} label={false} />
          <span class="name" title={deviceLabel(device)}>{deviceLabel(device)}</span>
          {#if hw}
            <span class="cpu" title="CPU usage">{hw.cpu_usage_percent.toFixed(0)}%</span>
          {:else}
            <span class="cpu muted">—</span>
          {/if}
        </button>
      </li>
    {:else}
      <li class="none">No devices match</li>
    {/each}
  </ul>

  <button class="add" on:click={onAdd}>
    <Icon name="plus" size={15} />
    Add device
  </button>
</div>

<style>
  .device-list {
    display: grid;
    grid-template-rows: auto auto 1fr auto;
    gap: 0.5rem;
    min-height: 0;
    padding: 0 0.5rem;
  }

  .search {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.4rem 0.55rem;
    background: var(--card-inset);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-faint);
  }

  .search input {
    flex: 1;
    min-width: 0;
    border: 0;
    background: transparent;
    color: var(--text);
    font-size: 0.78rem;
    padding: 0;
  }

  .search input:focus {
    outline: none;
  }

  .search:focus-within {
    border-color: var(--accent-border);
  }

  .filters {
    display: flex;
    gap: 0.15rem;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .filters button {
    flex: 0 0 auto;
    padding: 0.2rem 0.45rem;
    border: 0;
    border-radius: var(--radius-pill);
    background: transparent;
    color: var(--text-faint);
    font-size: 0.66rem;
    font-weight: 600;
    cursor: pointer;
    transition: background var(--t-fast), color var(--t-fast);
  }

  .filters button:hover {
    color: var(--text);
    background: var(--card-hover);
  }

  .filters button.active {
    background: var(--accent-dim);
    color: var(--accent);
  }

  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    min-height: 0;
    display: grid;
    gap: 1px;
    align-content: start;
    scrollbar-width: thin;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    width: 100%;
    padding: 0.35rem 0.5rem;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--text);
    font-size: 0.78rem;
    text-align: left;
    cursor: pointer;
    transition: background var(--t-fast);
  }

  .row:hover {
    background: var(--card-hover);
  }

  .row.selected {
    background: var(--accent-dim);
    color: var(--text-bright);
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .cpu {
    color: var(--text-muted);
    font-size: 0.68rem;
    font-variant-numeric: tabular-nums;
    flex-shrink: 0;
  }

  .cpu.muted {
    color: var(--text-faint);
  }

  .none {
    padding: 0.75rem 0.5rem;
    color: var(--text-faint);
    font-size: 0.76rem;
    text-align: center;
  }

  .add {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.35rem;
    width: 100%;
    padding: 0.45rem;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    font-size: 0.76rem;
    font-weight: 600;
    cursor: pointer;
    transition: border-color var(--t-fast), color var(--t-fast);
  }

  .add:hover {
    border-color: var(--accent-border);
    color: var(--accent);
  }
</style>
