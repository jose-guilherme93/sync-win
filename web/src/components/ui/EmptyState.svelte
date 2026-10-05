<script lang="ts">
  // Shared empty state so every screen answers the same two questions: what is
  // missing, and what can the user do about it.
  export let title = 'Nothing here yet'
  export let message = ''
  export let actionLabel = ''
  export let onAction: (() => void) | null = null
  export let icon = ''
</script>

<div class="empty-state">
  {#if icon}<div class="icon" aria-hidden="true">{icon}</div>{/if}
  <strong>{title}</strong>
  {#if message}<p>{message}</p>{/if}
  {#if actionLabel && onAction}
    <button class="action" on:click={onAction}>{actionLabel}</button>
  {/if}
</div>

<style>
  .empty-state {
    display: grid;
    justify-items: center;
    gap: 0.5rem;
    padding: 2.5rem 1.25rem;
    text-align: center;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius);
    color: var(--text-muted);
    font-size: 0.84rem;
  }

  .icon {
    font-size: 1.6rem;
    opacity: 0.7;
  }

  strong {
    color: var(--text);
    font-size: 1rem;
  }

  p {
    margin: 0;
    max-width: 34rem;
    line-height: 1.5;
  }

  .action {
    margin-top: 0.35rem;
    padding: 0.5rem 1rem;
    border-radius: var(--radius-sm);
    border: 1px solid var(--accent-border);
    background: var(--accent-dim);
    color: var(--accent);
    font-weight: 600;
    font-size: 0.82rem;
    cursor: pointer;
    transition: background var(--t-fast);
  }

  .action:hover {
    background: rgba(52, 211, 153, 0.25);
  }
</style>
