<script lang="ts">
  import Icon from '../ui/Icon.svelte'
  import DeviceList from './DeviceList.svelte'
  import { NAV_GROUPS, type Section } from '../../lib/router'
  import { deviceLabel, memoryPercent, type Device } from '../../lib/types'
  import { formatRelative } from '../../lib/format'

  export let devices: Device[] = []
  export let selectedId: string | null = null
  export let activeSection: Section = 'home'
  export let collapsed = false
  export let onSelectDevice: (id: string) => void
  export let onNavigate: (section: Section) => void
  export let onAddDevice: () => void
  export let onToggleCollapse: () => void

  $: selected = devices.find((d) => d.id === selectedId) || null
  $: hw = selected?.hardware
  $: hasDevice = Boolean(selected)
</script>

<aside class="sidebar" class:collapsed aria-label="Main navigation">
  <div class="brand">
    <span class="mark" aria-hidden="true">⬢</span>
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
      {@const disabled = Boolean(group.deviceScoped) && !hasDevice}
      {#if group.label}
        <p class="group-label" class:disabled>{group.label}</p>
      {/if}
      <ul>
        {#each group.items as item (item.section)}
          <li>
            <button
              class="nav-item"
              class:active={activeSection === item.section}
              disabled={disabled}
              title={collapsed ? item.label : disabled ? 'Select a device first' : item.label}
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

  {#if !collapsed}
    <div class="devices-section">
      <p class="group-label">Devices</p>
      <DeviceList {devices} selectedId onSelect={onSelectDevice} onAdd={onAddDevice} />
    </div>
  {/if}

  {#if selected && !collapsed}
    <div class="footer" aria-label="Selected device summary">
      <div class="footer-name" title={deviceLabel(selected)}>{deviceLabel(selected)}</div>
      <div class="footer-stats">
        <div>
          <span class="k">CPU</span>
          <strong>{hw ? `${hw.cpu_usage_percent.toFixed(0)}%` : '—'}</strong>
        </div>
        <div>
          <span class="k">TEMP</span>
          <strong>{hw?.cpu_temperature != null ? `${hw.cpu_temperature.toFixed(0)}°` : '—'}</strong>
        </div>
        <div>
          <span class="k">RAM</span>
          <strong>{hw ? `${memoryPercent(hw).toFixed(0)}%` : '—'}</strong>
        </div>
      </div>
      <div class="footer-seen" title={selected.last_seen_at}>seen {formatRelative(selected.last_seen_at)}</div>
    </div>
  {/if}
</aside>

<style>
  .sidebar {
    display: grid;
    grid-template-rows: auto auto 1fr auto;
    gap: 0.6rem;
    width: var(--sidebar-w);
    height: 100vh;
    padding: 0.75rem 0;
    background: var(--bg-elevated);
    border-right: 1px solid var(--border);
    overflow: hidden;
    flex-shrink: 0;
    transition: width var(--t);
  }

  .sidebar.collapsed {
    width: var(--sidebar-w-collapsed);
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0 0.75rem;
    min-height: 28px;
  }

  .mark {
    color: var(--accent);
    font-size: 1.1rem;
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
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text-faint);
    cursor: pointer;
    flex-shrink: 0;
    transition: color var(--t-fast), background var(--t-fast);
  }

  .collapse:hover {
    color: var(--text);
    background: var(--card-hover);
  }

  .sidebar.collapsed .collapse {
    transform: rotate(180deg);
  }

  .groups {
    overflow-y: auto;
    min-height: 0;
    padding: 0 0.5rem;
    scrollbar-width: thin;
  }

  .groups ul {
    list-style: none;
    margin: 0 0 0.5rem;
    padding: 0;
    display: grid;
    gap: 1px;
  }

  .group-label {
    margin: 0.35rem 0 0.25rem;
    padding: 0 0.5rem;
    color: var(--text-faint);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
  }

  .group-label.disabled {
    opacity: 0.5;
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 0.55rem;
    width: 100%;
    padding: 0.42rem 0.5rem;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--text-muted);
    font-size: 0.8rem;
    font-weight: 500;
    text-align: left;
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--t-fast), color var(--t-fast);
  }

  .nav-item:hover:not(:disabled) {
    background: var(--card-hover);
    color: var(--text);
  }

  /* The single accent marks the current section. No other nav affordance uses
     colour, so this reads unambiguously. */
  .nav-item.active {
    background: var(--accent-dim);
    color: var(--accent);
  }

  .nav-item:disabled {
    opacity: 0.35;
    cursor: default;
  }

  .devices-section {
    display: grid;
    grid-template-rows: auto 1fr;
    gap: 0.25rem;
    min-height: 0;
    overflow: hidden;
  }

  .devices-section .group-label {
    padding-left: 1rem;
  }

  .footer {
    display: grid;
    gap: 0.4rem;
    margin: 0 0.5rem;
    padding: 0.6rem 0.7rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .footer-name {
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .footer-stats {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 0.4rem;
  }

  .footer-stats .k {
    display: block;
    color: var(--text-faint);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.08em;
  }

  .footer-stats strong {
    color: var(--text-bright);
    font-size: 0.9rem;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
  }

  .footer-seen {
    color: var(--text-faint);
    font-size: 0.68rem;
  }
</style>
