<script lang="ts">
  // A single status indicator. Colour is the primary signal but the label is
  // always present for colour-blind users and screen readers, and the title
  // carries the reason (matching the STATUS_HINTS copy in App.svelte).
  export let status: 'online' | 'stale' | 'offline' | 'error' | 'duplicate' | string = 'offline'
  export let label = true
  export let title = ''

  const HINTS: Record<string, string> = {
    online: 'Agent reported within the last 30 seconds',
    stale: 'No report for over 30 seconds',
    offline: 'No report for more than 5 minutes',
    error: 'The agent reported a sync failure',
    duplicate: 'Another device is reporting the same hardware fingerprint'
  }

  // stale is "attention" rather than "healthy": the device is reachable in
  // principle but has gone quiet, which is what the user needs to act on.
  $: tone = status === 'online' ? 'ok'
    : status === 'error' ? 'crit'
    : status === 'stale' ? 'warn'
    : status === 'duplicate' ? 'warn'
    : 'offline'

  $: hint = title || HINTS[status] || 'Unknown status'
</script>

<span class="status-dot {tone}" title={hint}>
  <i class="dot" aria-hidden="true"></i>
  {#if label}<span class="text">{status}</span>{/if}
  <span class="sr-only">{hint}</span>
</span>

<style>
  .status-dot {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.72rem;
    font-weight: 600;
    text-transform: capitalize;
    white-space: nowrap;
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
    background: currentColor;
  }

  .ok { color: var(--ok); }
  .warn { color: var(--warn); }
  .crit { color: var(--crit); }
  .offline { color: var(--offline); }

  /* Only the healthy state breathes. A pulsing red dot on every offline device
     would make the list feel like an emergency. */
  .ok .dot { animation: status-pulse 2.4s ease-in-out infinite; }

  @keyframes status-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.45; }
  }

  .text { color: var(--text); }

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

  @media (prefers-reduced-motion: reduce) {
    .ok .dot { animation: none; }
  }
</style>
