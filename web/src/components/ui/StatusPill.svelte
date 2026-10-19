<script lang="ts">
  // Container state as a pill. The tone is supplied by the caller so the same
  // component can label a service, a unit or a container without inventing a
  // second colour vocabulary.
  export let label: string
  export let tone: 'ok' | 'warn' | 'crit' | 'idle' = 'idle'
</script>

<span class="pill {tone}">{label}</span>

<style>
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.14rem 0.5rem;
    border: 1px solid transparent;
    border-radius: var(--radius-pill);
    font-size: 0.64rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    white-space: nowrap;
  }

  .pill::before {
    content: '';
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
  }

  .ok {
    color: var(--accent);
    background: var(--accent-dim);
    border-color: rgba(52, 211, 153, 0.28);
  }

  .warn {
    color: var(--warn);
    background: var(--warn-dim);
    border-color: rgba(251, 191, 36, 0.3);
  }

  .crit {
    color: var(--crit);
    background: var(--crit-dim);
    border-color: rgba(248, 113, 113, 0.35);
  }

  .idle {
    color: var(--text-muted);
    background: rgba(148, 163, 184, 0.1);
    border-color: var(--border-strong);
  }
</style>
