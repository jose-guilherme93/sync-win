<script lang="ts">
  import { runDockerCommand } from '../../lib/docker-client'
  import ConfirmDialog from '../ui/ConfirmDialog.svelte'

  export let deviceId: string
  export let authHeaders: Record<string, string> = {}
  export let onChanged: () => void = () => {}

  type PruneTarget = 'system' | 'image' | 'container' | 'network'

  const OPTIONS: { target: PruneTarget; label: string; description: string; consequence: string }[] = [
    {
      target: 'system',
      label: 'System prune',
      description: 'Stopped containers, unused networks, dangling images and build cache.',
      consequence: 'This removes stopped containers, unused networks, dangling images and build cache on the device.'
    },
    {
      target: 'image',
      label: 'Images',
      description: 'Dangling images that no container references.',
      consequence: 'This removes every dangling image on the device. Tagged images are kept.'
    },
    {
      target: 'container',
      label: 'Containers',
      description: 'Containers that are already stopped.',
      consequence: 'This permanently removes every stopped container on the device, including its writable layer.'
    },
    {
      target: 'network',
      label: 'Networks',
      description: 'Networks with no container attached.',
      consequence: 'This removes Docker networks that no container is using.'
    }
  ]

  let pending: PruneTarget | null = null
  let running = false
  let result = ''
  let error = ''

  $: pendingOption = OPTIONS.find((option) => option.target === pending) || null

  async function prune() {
    const target = pending
    if (!target) return
    running = true
    result = ''
    error = ''
    try {
      const response = await runDockerCommand(deviceId, `prune_${target}`, '', '', {
        headers: authHeaders,
        timeoutMs: 90_000
      })
      result = response.message || 'Prune completed'
      onChanged()
    } catch (e: any) {
      error = e?.message || 'The prune command failed'
    } finally {
      running = false
      pending = null
    }
  }
</script>

<article class="maintenance-card">
  <header class="card-head">
    <div>
      <h2>Cleanup</h2>
      <p class="sub">Reclaim disk space from resources nothing is using.</p>
    </div>
  </header>

  {#if error}
    <div class="inline-error" role="alert">{error}</div>
  {/if}

  <ul class="options">
    {#each OPTIONS as option (option.target)}
      <li>
        <div class="info">
          <span class="name">{option.label}</span>
          <span class="desc">{option.description}</span>
        </div>
        <button class="btn-ghost" on:click={() => (pending = option.target)} disabled={running}>
          Prune
        </button>
      </li>
    {/each}
  </ul>

  {#if result}
    <pre class="output">{result}</pre>
  {/if}
</article>

<ConfirmDialog
  open={pending != null}
  title={`Prune ${pendingOption?.label.toLowerCase() || 'resources'}?`}
  message={pendingOption?.consequence || ''}
  confirmLabel={running ? 'Pruning…' : 'Prune'}
  destructive={true}
  busy={running}
  on:confirm={prune}
  on:cancel={() => (pending = null)}
/>

<style>
  .maintenance-card {
    display: grid;
    gap: 0;
    align-content: start;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    min-width: 0;
  }

  .card-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.6rem;
    padding: 0.75rem 0.9rem;
    border-bottom: 1px solid var(--border);
  }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .sub { margin: 0.2rem 0 0; color: var(--text-faint); font-size: 0.72rem; line-height: 1.4; }

  .options { list-style: none; margin: 0; padding: 0; }
  .options li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.65rem 0.9rem;
    border-bottom: 1px solid var(--border);
  }
  .options li:last-child { border-bottom: 0; }
  .info { display: grid; gap: 0.1rem; min-width: 0; }
  .name { color: var(--text); font-size: 0.78rem; font-weight: 600; }
  .desc { color: var(--text-faint); font-size: 0.72rem; line-height: 1.4; }

  .inline-error {
    margin: 0.7rem 0.9rem 0;
    padding: 0.5rem 0.65rem;
    border: 1px solid rgba(248, 113, 113, 0.35);
    border-radius: var(--radius-sm);
    background: var(--crit-dim);
    color: var(--crit);
    font-size: 0.74rem;
  }

  .output {
    margin: 0.7rem 0.9rem 0.9rem;
    padding: 0.55rem 0.65rem;
    max-height: 14rem;
    overflow: auto;
    background: #08080a;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-family: var(--mono);
    font-size: 0.7rem;
    white-space: pre-wrap;
  }

  .btn-ghost {
    padding: 0.3rem 0.6rem;
    border: 1px solid rgba(251, 191, 36, 0.35);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--warn);
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn-ghost:hover:not(:disabled) { background: var(--warn-dim); }
  button:disabled { opacity: 0.5; cursor: default; }
</style>
