<script lang="ts">
  import Icon from '../ui/Icon.svelte'
  import { DEVICE_TABS, type Section } from '../../lib/router'

  // The device screen owns every section it has. One scrollable row, groups
  // separated by a hairline, active tab kept in view when the device opens or
  // the section changes from elsewhere (a notification, for example).
  export let active: Section
  export let onPick: (section: Section) => void

  let scroller: HTMLElement | null = null

  function keepActiveVisible() {
    if (!scroller) return
    const el = scroller.querySelector<HTMLElement>('[data-active="true"]')
    if (!el) return
    const left = el.offsetLeft
    const right = left + el.offsetWidth
    if (left < scroller.scrollLeft || right > scroller.scrollLeft + scroller.clientWidth) {
      scroller.scrollTo({ left: left - 24, behavior: 'smooth' })
    }
  }

  $: if (scroller && active) keepActiveVisible()
</script>

<nav class="tabs" aria-label="Device sections" bind:this={scroller}>
  {#each DEVICE_TABS as group, index (group.id)}
    {#if index > 0}
      <span class="sep" aria-hidden="true"></span>
    {/if}
    {#each group.items as tab (tab.section)}
      <button
        type="button"
        class="tab"
        class:active={active === tab.section}
        data-active={active === tab.section}
        aria-current={active === tab.section ? 'page' : undefined}
        title={tab.label}
        on:click={() => onPick(tab.section)}
      >
        <Icon name={tab.icon} size={14} />
        <span>{tab.label}</span>
      </button>
    {/each}
  {/each}
</nav>

<style>
  .tabs {
    display: flex;
    align-items: stretch;
    gap: 2px;
    padding: 0 0.85rem;
    border-bottom: 1px solid var(--border);
    background: var(--bg);
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: none;
    flex: none;
  }

  .tabs::-webkit-scrollbar {
    display: none;
  }

  .tab {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    height: 42px;
    padding: 0 0.6rem;
    border: 0;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    background: transparent;
    color: var(--text-muted);
    font: inherit;
    font-size: 0.76rem;
    font-weight: 500;
    white-space: nowrap;
    cursor: pointer;
  }

  .tab:hover {
    color: var(--text);
    background: transparent;
  }

  .tab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
    font-weight: 600;
  }

  .sep {
    width: 1px;
    align-self: center;
    height: 18px;
    background: var(--border);
    margin: 0 0.4rem;
    flex: none;
  }

  @media (max-width: 720px) {
    .tab span {
      display: none;
    }

    .tab {
      padding: 0 0.7rem;
    }
  }
</style>
