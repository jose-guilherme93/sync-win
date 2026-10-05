<script lang="ts">
  import Icon from '../ui/Icon.svelte'
  import StatusDot from '../ui/StatusDot.svelte'
  import { computedStatus, deviceLabel, type Device } from '../../lib/types'
  import { formatRelative } from '../../lib/format'

  // The topbar answers "which device am I looking at, is it alive, and how
  // fresh is this data" without the user having to scroll. The refresh selector
  // controls the shared poll interval owned by App.svelte.
  export let device: Device | null = null
  export let sectionTitle = ''
  export let refreshMs = 10000
  export let onRefreshChange: (ms: number) => void
  export let onManualRefresh: () => void
  export let onOpenSearch: () => void
  export let onOpenNotifications: () => void
  export let onOpenUserMenu: () => void
  export let accountInitial = '?'
  export let unreadCount = 0
  export let onOpenMobileNav: (() => void) | null = null

  const REFRESH_OPTIONS = [1000, 5000, 10000, 30000, 60000]

  function refreshLabel(ms: number) {
    return ms >= 60000 ? `${ms / 60000}m` : `${ms / 1000}s`
  }
</script>

<header class="topbar">
  {#if onOpenMobileNav}
    <button class="icon-btn mobile" on:click={onOpenMobileNav} aria-label="Open navigation">
      <Icon name="menu" size={18} />
    </button>
  {/if}

  <div class="identity">
    {#if device}
      {@const status = computedStatus(device)}
      <h1 title={deviceLabel(device)}>{deviceLabel(device)}</h1>
      <div class="meta">
        <StatusDot {status} />
        <span class="sep" aria-hidden="true">·</span>
        <span title={device.last_seen_at}>seen {formatRelative(device.last_seen_at)}</span>
        {#if device.hardware?.operating_system}
          <span class="sep" aria-hidden="true">·</span>
          <span class="os" title={device.hardware.operating_system}>{device.hardware.operating_system}</span>
        {/if}
      </div>
    {:else}
      <h1>{sectionTitle || 'Fleet'}</h1>
      <div class="meta"><span>All devices</span></div>
    {/if}
  </div>

  <div class="actions">
    <button class="search-trigger" on:click={onOpenSearch}>
      <Icon name="search" size={14} />
      <span>Search</span>
      <kbd>Ctrl K</kbd>
    </button>

    <div class="refresh" role="group" aria-label="Refresh interval">
      {#each REFRESH_OPTIONS as ms (ms)}
        <button
          class:active={refreshMs === ms}
          on:click={() => onRefreshChange(ms)}
          title={`Refresh every ${refreshLabel(ms)}`}
        >
          {refreshLabel(ms)}
        </button>
      {/each}
    </div>

    <button class="icon-btn" on:click={onManualRefresh} title="Refresh now" aria-label="Refresh now">
      <Icon name="refresh" size={16} />
    </button>

    <button class="icon-btn" on:click={onOpenNotifications} title="Notifications" aria-label="Notifications">
      <Icon name="bell" size={16} />
      {#if unreadCount > 0}<span class="badge">{unreadCount}</span>{/if}
    </button>

    <button class="avatar" on:click={onOpenUserMenu} aria-label="Account menu">{accountInitial}</button>
  </div>
</header>

<style>
  .topbar {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    min-height: var(--topbar-h);
    padding: 0.5rem 1rem;
    border-bottom: 1px solid var(--border);
    background: var(--bg);
  }

  .identity {
    min-width: 0;
    flex: 1;
  }

  .identity h1 {
    margin: 0;
    font-size: 1.05rem;
    font-weight: 600;
    color: var(--text-bright);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .meta {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    margin-top: 0.1rem;
    color: var(--text-muted);
    font-size: 0.72rem;
    min-width: 0;
  }

  .meta .sep {
    color: var(--text-faint);
  }

  .os {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-shrink: 0;
  }

  .search-trigger {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    padding: 0.35rem 0.6rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card-inset);
    color: var(--text-faint);
    font-size: 0.75rem;
    cursor: pointer;
    transition: border-color var(--t-fast), color var(--t-fast);
  }

  .search-trigger:hover {
    border-color: var(--border-strong);
    color: var(--text);
  }

  kbd {
    font-family: var(--mono);
    font-size: 0.62rem;
    padding: 0.05rem 0.3rem;
    border: 1px solid var(--border-strong);
    border-radius: 4px;
    color: var(--text-faint);
  }

  .refresh {
    display: flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }

  .refresh button {
    padding: 0.3rem 0.45rem;
    border: 0;
    background: transparent;
    color: var(--text-faint);
    font-size: 0.66rem;
    font-weight: 600;
    cursor: pointer;
    transition: background var(--t-fast), color var(--t-fast);
  }

  .refresh button:hover {
    background: var(--card-hover);
    color: var(--text);
  }

  .refresh button.active {
    background: var(--accent-dim);
    color: var(--accent);
  }

  .icon-btn {
    position: relative;
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card-inset);
    color: var(--text-muted);
    cursor: pointer;
    transition: border-color var(--t-fast), color var(--t-fast);
  }

  .icon-btn:hover {
    border-color: var(--border-strong);
    color: var(--text);
  }

  .badge {
    position: absolute;
    top: -5px;
    right: -5px;
    min-width: 15px;
    height: 15px;
    padding: 0 3px;
    border-radius: var(--radius-pill);
    background: var(--crit);
    color: #fff;
    font-size: 0.6rem;
    font-weight: 700;
    display: grid;
    place-items: center;
  }

  .avatar {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    border: 1px solid var(--border-strong);
    background: var(--accent-dim);
    color: var(--accent);
    font-weight: 700;
    font-size: 0.82rem;
    cursor: pointer;
    display: grid;
    place-items: center;
  }

  .mobile {
    display: none;
  }

  @media (max-width: 900px) {
    .mobile {
      display: grid;
    }

    .search-trigger span,
    .search-trigger kbd,
    .refresh {
      display: none;
    }
  }
</style>
