<script lang="ts">
  import { onMount, type Snippet } from 'svelte'
  import {
    defaultPanels,
    labelFor,
    loadPanels,
    movePanelStep,
    panelStorageKey,
    reorderPanels,
    savePanels,
    setPanelHidden,
    togglePanelSpan,
    type PanelDefinition,
    type PanelState
  } from '../../lib/dashboard-layout'

  // A Grafana-shaped layout for a device screen: panels keep their content,
  // and the operator decides where each one sits, how wide it is and whether
  // it is shown. Only one screen uses it so far, so the component owns the
  // state instead of pushing it into a store.
  export let screen = 'dashboard'
  export let definitions: PanelDefinition[] = []
  export let panel: Snippet<[string]>

  let panels: PanelState[] = defaultPanels(definitions)
  let storageKey = ''
  let editing = false
  let dragId = ''
  let overId = ''
  let announce = ''
  let storageAvailable = true

  onMount(() => {
    storageKey = panelStorageKey(screen)
    panels = loadPanels(storageKey, definitions)
  })

  $: visible = panels.filter((item) => !item.hidden)
  $: hidden = panels.filter((item) => item.hidden)

  function persist(next: PanelState[], message = '') {
    panels = next
    if (storageKey && !savePanels(storageKey, next)) storageAvailable = false
    if (message) announce = message
  }

  function reset() {
    persist(defaultPanels(definitions), 'Layout reset to the default panels.')
  }

  function move(id: string, delta: number) {
    const next = movePanelStep(panels, id, delta)
    if (next === panels) return
    persist(next, `${labelFor(definitions, id)} moved.`)
  }

  function resize(id: string) {
    const panelState = panels.find((item) => item.id === id)
    const next = togglePanelSpan(panels, id)
    if (next === panels) return
    persist(next, `${labelFor(definitions, id)} is now ${panelState?.span === 12 ? 'half' : 'full'} width.`)
  }

  function hide(id: string) {
    persist(setPanelHidden(panels, id, true), `${labelFor(definitions, id)} hidden.`)
  }

  function show(id: string) {
    persist(setPanelHidden(panels, id, false), `${labelFor(definitions, id)} shown.`)
  }

  function startDrag(event: DragEvent, id: string) {
    dragId = id
    overId = ''
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move'
      event.dataTransfer.setData('text/plain', id)
    }
  }

  function dragOver(event: DragEvent, id: string) {
    if (!dragId || dragId === id) return
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
    overId = id
  }

  function drop(event: DragEvent, id: string) {
    event.preventDefault()
    const from = dragId || event.dataTransfer?.getData('text/plain') || ''
    dragId = ''
    overId = ''
    if (!from || from === id) return
    const next = reorderPanels(panels, from, id)
    if (next === panels) return
    persist(next, `${labelFor(definitions, from)} moved.`)
  }

  function endDrag() {
    dragId = ''
    overId = ''
  }

  function finish() {
    editing = false
    endDrag()
  }
</script>

<div class="layout">
  <div class="toolbar">
    {#if editing}
      <span class="hint">Drag a panel by its handle, or use the arrows. Saved in this browser.</span>
      {#if !storageAvailable}<span class="warn">Could not save the layout here.</span>{/if}
      <div class="toolbar-actions">
        <button class="ghost" on:click={reset}>Reset to default</button>
        <button class="primary" on:click={finish}>Done</button>
      </div>
    {:else}
      <button class="ghost customize" on:click={() => (editing = true)} aria-label="Customize dashboard layout">
        Customize layout
      </button>
    {/if}
  </div>

  {#if editing && hidden.length > 0}
    <div class="hidden-bar">
      <span class="hidden-label">Hidden</span>
      {#each hidden as item (item.id)}
        <button class="show" on:click={() => show(item.id)} aria-label={`Show ${labelFor(definitions, item.id)}`}>
          + {labelFor(definitions, item.id)}
        </button>
      {/each}
    </div>
  {/if}

  <div class="grid">
    {#each visible as item, index (item.id)}
      <section
        class="panel"
        role="group"
        aria-label={labelFor(definitions, item.id)}
        class:dragging={dragId === item.id}
        class:drag-over={overId === item.id}
        style="grid-column: span {item.span}"
        on:dragover={(event) => dragOver(event, item.id)}
        on:drop={(event) => drop(event, item.id)}
      >
        {#if editing}
          <div class="panel-bar">
            <span
              class="grip"
              draggable="true"
              role="button"
              tabindex="0"
              title={`Drag ${labelFor(definitions, item.id)}, or press the left and right arrow keys`}
              aria-label={`Move ${labelFor(definitions, item.id)} with the arrow keys, or drag it`}
              on:dragstart={(event) => startDrag(event, item.id)}
              on:dragend={endDrag}
              on:keydown={(event) => {
                if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
                event.preventDefault()
                move(item.id, event.key === 'ArrowLeft' ? -1 : 1)
              }}
            >⠿</span>
            <span class="panel-name">{labelFor(definitions, item.id)}</span>
            <div class="panel-actions">
              <button
                on:click={() => move(item.id, -1)}
                disabled={index === 0}
                aria-label={`Move ${labelFor(definitions, item.id)} earlier`}
                title="Move earlier"
              >←</button>
              <button
                on:click={() => move(item.id, 1)}
                disabled={index === visible.length - 1}
                aria-label={`Move ${labelFor(definitions, item.id)} later`}
                title="Move later"
              >→</button>
              <button
                on:click={() => resize(item.id)}
                aria-label={`Make ${labelFor(definitions, item.id)} ${item.span === 12 ? 'half' : 'full'} width`}
                title={item.span === 12 ? 'Half width' : 'Full width'}
              >{item.span === 12 ? '½' : 'Full'}</button>
              <button on:click={() => hide(item.id)} aria-label={`Hide ${labelFor(definitions, item.id)}`} title="Hide panel">Hide</button>
            </div>
          </div>
        {/if}
        <div class="panel-body">
          {@render panel(item.id)}
        </div>
      </section>
    {/each}
  </div>

  <span class="sr-only" aria-live="polite">{announce}</span>
</div>

<style>
  .layout { display: grid; gap: 0.55rem; min-width: 0; }

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 0.6rem;
    min-height: 1.4rem;
    flex-wrap: wrap;
  }
  .toolbar-actions { display: flex; gap: 0.4rem; margin-left: auto; }
  .hint { color: var(--text-faint); font-size: 0.7rem; }
  .warn { color: var(--warn); font-size: 0.7rem; }

  .ghost {
    padding: 0.22rem 0.55rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-pill);
    background: transparent;
    color: var(--text-muted);
    font-size: 0.68rem;
    cursor: pointer;
  }
  .ghost:hover { border-color: var(--accent-border); color: var(--text); }
  .customize { justify-self: end; }
  .primary {
    padding: 0.22rem 0.7rem;
    border: 1px solid var(--accent-border);
    border-radius: var(--radius-pill);
    background: var(--accent-dim);
    color: var(--accent);
    font-size: 0.68rem;
    font-weight: 600;
    cursor: pointer;
  }

  .hidden-bar {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-wrap: wrap;
    padding: 0.4rem 0.55rem;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--card-inset);
  }
  .hidden-label { color: var(--text-faint); font-size: 0.66rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .show {
    padding: 0.15rem 0.45rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-pill);
    background: transparent;
    color: var(--text-muted);
    font-size: 0.68rem;
    cursor: pointer;
  }
  .show:hover { border-color: var(--accent-border); color: var(--text); }

  .grid {
    display: grid;
    grid-template-columns: repeat(12, minmax(0, 1fr));
    gap: 0.85rem;
    align-items: start;
    min-width: 0;
  }
  .panel { min-width: 0; }
  .panel.dragging { opacity: 0.45; }
  .panel.drag-over > .panel-body { outline: 1px dashed var(--accent); outline-offset: 3px; border-radius: var(--radius); }
  .panel-body { min-width: 0; }

  .panel-bar {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    margin-bottom: 0.35rem;
    padding: 0.22rem 0.45rem;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--card-inset);
    font-size: 0.68rem;
  }
  .grip {
    color: var(--text-faint);
    cursor: grab;
    user-select: none;
    padding: 0 0.15rem;
  }
  .grip:active { cursor: grabbing; }
  .grip:focus-visible { outline: 1px solid var(--accent-border); outline-offset: 2px; border-radius: 3px; }
  .panel-name { color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .panel-actions { display: flex; gap: 0.2rem; margin-left: auto; flex-wrap: wrap; }
  .panel-actions button {
    min-width: 1.5rem;
    padding: 0.08rem 0.3rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    font-size: 0.66rem;
    line-height: 1.5;
    cursor: pointer;
  }
  .panel-actions button:hover:not(:disabled) { border-color: var(--accent-border); color: var(--text); }
  .panel-actions button:disabled { opacity: 0.35; cursor: default; }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  @media (max-width: 900px) {
    .grid { grid-template-columns: 1fr; }
    .panel { grid-column: 1 / -1 !important; }
    .toolbar { justify-content: flex-start; }
  }
</style>
