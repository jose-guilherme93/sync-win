<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy } from 'svelte'

  // Every destructive action goes through this dialog. It is deliberately not a
  // generic modal: it always states the consequence, always requires an
  // explicit confirmation, and focuses the cancel button so that pressing Enter
  // on a stray keystroke does not confirm the destructive path.
  export let open = false
  export let title = 'Are you sure?'
  export let message = ''
  export let confirmLabel = 'Confirm'
  export let cancelLabel = 'Cancel'
  export let destructive = true
  export let busy = false
  // Optional typed confirmation. When set, the confirm button stays disabled
  // until the user types this exact string — used for "delete device and data".
  export let requireText = ''

  const dispatch = createEventDispatcher()
  let typed = ''
  let cancelButton: HTMLButtonElement | null = null

  $: confirmed = !requireText || typed.trim() === requireText

  function confirm() {
    if (!confirmed || busy) return
    dispatch('confirm')
  }

  function cancel() {
    if (busy) return
    dispatch('cancel')
  }

  function onKeydown(event: KeyboardEvent) {
    if (!open) return
    if (event.key === 'Escape') cancel()
  }

  // Reset the typed phrase each time the dialog opens so a previous attempt
  // never satisfies the next confirmation.
  $: if (open) {
    typed = ''
    queueMicrotask(() => cancelButton?.focus())
  }

  onMount(() => document.addEventListener('keydown', onKeydown))
  onDestroy(() => document.removeEventListener('keydown', onKeydown))
</script>

{#if open}
  <div class="backdrop" role="presentation">
    <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title" aria-describedby="confirm-message">
      <h3 id="confirm-title">{title}</h3>
      {#if message}<p id="confirm-message">{message}</p>{/if}

      {#if requireText}
        <label class="typed">
          Type <code>{requireText}</code> to confirm
          <input bind:value={typed} placeholder={requireText} autocomplete="off" spellcheck="false" />
        </label>
      {/if}

      <div class="actions">
        <button bind:this={cancelButton} class="cancel" on:click={cancel} disabled={busy}>{cancelLabel}</button>
        <button
          class="confirm {destructive ? 'danger' : 'primary'}"
          on:click={confirm}
          disabled={!confirmed || busy}
        >
          {busy ? 'Working…' : confirmLabel}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: grid;
    place-items: center;
    padding: 1rem;
    background: rgba(0, 0, 0, 0.7);
    backdrop-filter: blur(4px);
  }

  .dialog {
    width: min(420px, 100%);
    display: grid;
    gap: 0.85rem;
    padding: 1.25rem;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    box-shadow: var(--shadow-pop);
  }

  h3 {
    margin: 0;
    font-size: 1.05rem;
    color: var(--text-bright);
  }

  p {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.85rem;
    line-height: 1.5;
  }

  .typed {
    display: grid;
    gap: 0.35rem;
    color: var(--text-muted);
    font-size: 0.8rem;
  }

  code {
    font-family: var(--mono);
    color: var(--crit);
    background: rgba(0, 0, 0, 0.35);
    padding: 0.05rem 0.3rem;
    border-radius: 4px;
  }

  input {
    padding: 0.45rem 0.6rem;
    background: var(--card-inset);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-family: var(--mono);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }

  button {
    padding: 0.5rem 0.9rem;
    border-radius: var(--radius-sm);
    font-weight: 600;
    font-size: 0.82rem;
    cursor: pointer;
    transition: background var(--t-fast), border-color var(--t-fast);
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .cancel {
    background: transparent;
    border: 1px solid var(--border-strong);
    color: var(--text);
  }

  .cancel:hover:not(:disabled) {
    background: var(--card-hover);
  }

  .primary {
    border: 1px solid var(--accent-border);
    background: var(--accent-dim);
    color: var(--accent);
  }

  .primary:hover:not(:disabled) {
    background: rgba(52, 211, 153, 0.25);
  }

  .danger {
    border: 1px solid rgba(248, 113, 113, 0.45);
    background: var(--crit-dim);
    color: var(--crit);
  }

  .danger:hover:not(:disabled) {
    background: rgba(248, 113, 113, 0.25);
  }
</style>
