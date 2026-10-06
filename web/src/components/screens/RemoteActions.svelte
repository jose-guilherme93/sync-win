<script lang="ts">
  import { deviceLabel, type Device } from '../../lib/types'
  import { formatRelative } from '../../lib/format'
  import ConfirmDialog from '../ui/ConfirmDialog.svelte'
  import EmptyState from '../ui/EmptyState.svelte'

  export let device: Device

  // Remote actions are the most dangerous surface in the dashboard, so each one
  // states its consequence and goes through a confirmation dialog. The server
  // command queue currently accepts install_app / restore_saves / exclude_file
  // and the Docker verbs; reboot, package updates and agent restart need new
  // command types before these buttons can do more than explain themselves.
  type Action = {
    id: string
    label: string
    description: string
    consequence: string
    confirm: string
    dangerous: boolean
  }

  const ACTIONS: Action[] = [
    {
      id: 'restart-agent',
      label: 'Restart agent',
      description: 'Restart the SyncWin agent process on the device.',
      consequence: 'The device stops reporting until the service comes back, usually a few seconds.',
      confirm: 'Restart agent',
      dangerous: false
    },
    {
      id: 'update-packages',
      label: 'Apply package updates',
      description: 'Run the system package manager upgrade on the device.',
      consequence: 'This can replace a running kernel and will restart services. It cannot be undone from here.',
      confirm: 'Apply updates',
      dangerous: true
    },
    {
      id: 'reboot',
      label: 'Reboot device',
      description: 'Reboot the machine.',
      consequence: 'Every session on the device is terminated. The device disappears from the fleet until it boots.',
      confirm: 'Reboot now',
      dangerous: true
    }
  ]

  let pending: Action | null = null
  let busy = false

  // Session-local audit trail. Once the server records actions this becomes a
  // read of the audit log rather than browser state.
  type Entry = { at: number; action: string; by: string; outcome: string }
  let log: Entry[] = []

  const BY = 'you'

  function request(action: Action) {
    pending = action
  }

  function confirm() {
    if (!pending) return
    busy = true
    const action = pending
    log = [
      { at: Date.now(), action: action.label, by: BY, outcome: 'Blocked — command type not implemented server-side' },
      ...log
    ]
    pending = null
    busy = false
  }
</script>

<section class="actions">
  <header class="screen-head">
    <h2>Remote actions</h2>
    <span class="target">target: {deviceLabel(device)}</span>
  </header>
  <div class="notice" role="status">
    These actions are not enabled: the server command queue does not yet accept
    <code>reboot</code>, <code>update-packages</code> or <code>restart-agent</code>. The buttons
    document the intended confirmation flow and record attempts in the log below, but nothing is
    sent to the device.
  </div>

  <div class="grid">
    {#each ACTIONS as action (action.id)}
      <article class="card" class:danger={action.dangerous}>
        <h3>{action.label}</h3>
        <p>{action.description}</p>
        <p class="consequence">{action.consequence}</p>
        <button class:danger={action.dangerous} on:click={() => request(action)}>Run…</button>
      </article>
    {/each}
  </div>

  <article class="card">
    <header class="card-head"><h2>Audit log</h2><span class="muted">this session only</span></header>
    {#if log.length === 0}
      <EmptyState icon="📋" title="No actions yet" message="Every action you attempt is recorded here with its outcome." />
    {:else}
      <ul class="log">
        {#each log as entry, i (i)}
          <li>
            <span class="when">{formatRelative(entry.at)}</span>
            <span class="who">{entry.by}</span>
            <span class="what">{entry.action}</span>
            <span class="outcome">{entry.outcome}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </article>
</section>

<ConfirmDialog
  open={pending != null}
  title={pending ? pending.label : ''}
  message={pending ? pending.consequence : ''}
  confirmLabel={pending ? pending.confirm : 'Confirm'}
  destructive={pending?.dangerous ?? true}
  {busy}
  on:confirm={confirm}
  on:cancel={() => (pending = null)}
/>

<style>
  .actions { display: grid; gap: 0.85rem; }
  .screen-head { display: flex; align-items: baseline; justify-content: space-between; gap: 0.75rem; }
  .screen-head h2 { margin: 0; font-size: 0.95rem; color: var(--text-bright); }
  .target { color: var(--text-faint); font-size: 0.75rem; }

  .notice {
    padding: 0.6rem 0.8rem; border: 1px solid rgba(251, 191, 36, 0.35);
    border-radius: var(--radius-sm); background: var(--warn-dim);
    color: var(--warn); font-size: 0.78rem; line-height: 1.5;
  }
  .notice code { font-family: var(--mono); color: var(--text-bright); }

  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 0.75rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); padding: 0.9rem; overflow: hidden; }
  .card.danger { border-color: rgba(248, 113, 113, 0.35); }

  .card h3 { margin: 0 0 0.35rem; font-size: 0.88rem; color: var(--text-bright); }
  .card p { margin: 0 0 0.4rem; color: var(--text-muted); font-size: 0.78rem; line-height: 1.5; }
  .consequence { color: var(--text-faint) !important; font-size: 0.74rem !important; }

  button {
    margin-top: 0.4rem; padding: 0.4rem 0.8rem;
    border: 1px solid var(--accent-border); border-radius: var(--radius-sm);
    background: var(--accent-dim); color: var(--accent);
    font-weight: 600; font-size: 0.78rem; cursor: pointer;
  }
  button:hover { background: rgba(52, 211, 153, 0.25); }
  button.danger { border-color: rgba(248, 113, 113, 0.45); background: var(--crit-dim); color: var(--crit); }
  button.danger:hover { background: rgba(248, 113, 113, 0.25); }

  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; margin-bottom: 0.6rem; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .muted { color: var(--text-faint); font-size: 0.72rem; }

  .log { list-style: none; margin: 0; padding: 0; }
  .log li {
    display: grid; grid-template-columns: auto auto 1fr 1fr; gap: 0.6rem;
    padding: 0.45rem 0; border-bottom: 1px solid var(--border);
    font-size: 0.75rem; align-items: baseline;
  }
  .log li:last-child { border-bottom: 0; }
  .when { color: var(--text-faint); white-space: nowrap; }
  .who { color: var(--text-muted); }
  .what { color: var(--text); font-weight: 600; }
  .outcome { color: var(--warn); }

  @media (max-width: 700px) {
    .log li { grid-template-columns: 1fr; gap: 0.1rem; }
  }
</style>
