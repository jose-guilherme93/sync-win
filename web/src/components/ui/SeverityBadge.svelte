<script lang="ts">
  export let severity: 'info' | 'warn' | 'crit' | string
  export let count: number | null = null
</script>

<span class="sev {severity}">
  {severity}
  {#if count != null}<b>{count}</b>{/if}
</span>

<style>
  .sev {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.15rem 0.5rem;
    border-radius: var(--radius-pill);
    border: 1px solid transparent;
    font-size: 0.66rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }

  .sev b {
    font-variant-numeric: tabular-nums;
  }

  .info {
    color: var(--series-4);
    background: rgba(34, 211, 238, 0.12);
    border-color: rgba(34, 211, 238, 0.3);
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
</style>
