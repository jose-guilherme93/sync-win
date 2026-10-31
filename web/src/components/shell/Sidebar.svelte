<script lang="ts">
  import Icon from '../ui/Icon.svelte'
  import Logo from '../ui/Logo.svelte'
  import { NAV_GROUPS, type Section } from '../../lib/router'
  import type { Device } from '../../lib/types'

  // Fleet navigation only. Device sections live on the device screen's own tab
  // row, so nothing here is scoped to a selection and nothing is ever disabled.
  export let devices: Device[] = []
  export let activeSection: Section = 'home'
  export let collapsed = false
  export let onNavigate: (section: Section) => void
  export let onAddDevice: () => void
  export let onToggleCollapse: () => void

  $: online = devices.filter((d) => {
    const seen = Date.parse(d.last_seen_at || '')
    return Number.isFinite(seen) && Date.now() - seen < 60_000
  }).length
</script>

<aside class="sidebar" class:collapsed aria-label="Main navigation">
  <div class="brand">
    <span class="mark" aria-hidden="true"><Logo size={20} /></span>
    {#if !collapsed}<span class="word">SyncWin</span>{/if}
    <button
      class="collapse"
      on:click={onToggleCollapse}
      title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
      aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
    >
      <Icon name="collapse" size={15} />
    </button>
  </div>

  <nav class="groups">
    {#each NAV_GROUPS as group (group.id)}
      {#if group.label}
        <p class="group-label">{group.label}</p>
      {/if}
      <ul>
        {#each group.items as item (item.section)}
          <li>
            <button
              class="nav-item"
              class:active={activeSection === item.section}
              title={collapsed ? item.label : item.label}
              on:click={() => onNavigate(item.section)}
            >
              <Icon name={item.icon} size={16} />
              {#if !collapsed}<span>{item.label}</span>{/if}
            </button>
          </li>
        {/each}
      </ul>
    {/each}
  </nav>

  <div class="fleet-foot">
    {#if collapsed}
      <button class="add-rail" on:click={onAddDevice} title="Add device" aria-label="Add device">
        <Icon name="plus" size={16} />
      </button>
    {:else}
      <div class="fleet-stats">
        <strong>{devices.length} device{devices.length === 1 ? '' : 's'}</strong>
        <span>{online} online</span>
      </div>
      <button class="add" on:click={onAddDevice}>
        <Icon name="plus" size={14} />
        <span>Add device</span>
      </button>
    {/if}
  </div>
</aside>

<style>
  .sidebar {
    width: 208px;
    flex: none;
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    padding: 0.85rem 0.6rem 0.75rem;
    background: var(--bg-elevated);
    border-right: 1px solid var(--border);
    overflow-y: auto;
  }

  .sidebar.collapsed {
    width: 52px;
    padding: 0.85rem 0.35rem 0.75rem;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0 0.55rem;
    min-height: 28px;
  }

  .sidebar.collapsed .brand {
    justify-content: center;
    padding: 0;
  }

  /* In the rail the mark would collide with the toggle, so the toggle becomes
     the single, centred control (the arrow now points out). */
  .sidebar.collapsed .mark,
  .sidebar.collapsed .word {
    display: none;
  }

  .sidebar.collapsed .collapse {
    margin: 0;
  }

  .mark {
    display: flex;
    align-items: center;
    color: var(--accent);
    line-height: 1;
    flex-shrink: 0;
  }

  .word {
    flex: 1;
    font-weight: 700;
    font-size: 0.95rem;
    letter-spacing: -0.01em;
    color: var(--text-bright);
  }

  .collapse {
    margin-left: auto;
    border: 0;
    background: transparent;
    color: var(--text-muted);
    padding: 0.15rem;
    border-radius: 6px;
    cursor: pointer;
  }

  .collapse:hover {
    color: var(--text);
    background: var(--card-hover);
  }

  .groups {
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    margin-top: 0.6rem;
  }

  .group-label {
    margin: 0.7rem 0 0.25rem;
    padding: 0 0.55rem;
    color: var(--text-muted);
    font-size: 0.62rem;
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
  }

  .sidebar.collapsed .group-label {
    display: none;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
  }

  .nav-item {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.55rem;
    padding: 0.4rem 0.55rem;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--text-muted);
    font: inherit;
    font-size: 0.79rem;
    text-align: left;
    cursor: pointer;
  }

  .nav-item:hover {
    background: var(--card-hover);
    color: var(--text);
  }

  .nav-item.active {
    background: var(--accent-dim);
    color: var(--accent);
    font-weight: 600;
  }

  .sidebar.collapsed .nav-item {
    justify-content: center;
    padding: 0.45rem 0;
  }

  .fleet-foot {
    margin-top: auto;
    display: grid;
    gap: 0.45rem;
    padding: 0.7rem 0.55rem 0;
    border-top: 1px solid var(--border);
  }

  .fleet-stats {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.4rem;
    font-size: 0.68rem;
    color: var(--text-muted);
  }

  .fleet-stats strong {
    color: var(--text);
    font-size: 0.72rem;
    font-weight: 600;
  }

  .add {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.35rem;
    border: 1px solid var(--border);
    border-radius: 7px;
    background: transparent;
    color: var(--text-muted);
    font: inherit;
    font-size: 0.72rem;
    padding: 0.35rem;
    cursor: pointer;
  }

  .add:hover {
    color: var(--accent);
    border-color: var(--accent-border);
  }

  .sidebar.collapsed .fleet-foot {
    justify-items: center;
    padding: 0.7rem 0 0;
  }

  .add-rail {
    border: 1px solid var(--border);
    border-radius: 7px;
    background: transparent;
    color: var(--text-muted);
    padding: 0.35rem;
    cursor: pointer;
  }

  .add-rail:hover {
    color: var(--accent);
    border-color: var(--accent-border);
  }
</style>
