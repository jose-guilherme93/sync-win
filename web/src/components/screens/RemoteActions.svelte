<script lang="ts">
  import { onDestroy } from 'svelte'
  import { deviceLabel, type Device } from '../../lib/types'
  import { formatRelative } from '../../lib/format'
  import { apiFetch, apiURL } from '../../lib/api'
  import ConfirmDialog from '../ui/ConfirmDialog.svelte'
  import EmptyState from '../ui/EmptyState.svelte'

  export let device: Device
  export let authHeaders: Record<string, string> = {}

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
      id: 'restart_agent',
      label: 'Restart agent',
      description: 'Restart the systemd-managed SyncWin agent process.',
      consequence: 'The agent reports success, exits, and systemd restarts its service.',
      confirm: 'Restart agent',
      dangerous: false
    },
    {
      id: 'update_packages',
      label: 'Apply package updates',
      description: 'Update installed APT, Pacman/AUR, and Flatpak packages when available.',
      consequence: 'Updates may replace the running kernel and restart services. This cannot be undone from here.',
      confirm: 'Apply updates',
      dangerous: true
    },
    {
      id: 'reboot_device',
      label: 'Reboot device',
      description: 'Schedule a system reboot using the host shutdown utility.',
      consequence: 'This ends every session on the device. The reboot is scheduled about one minute after confirmation.',
      confirm: 'Reboot now',
      dangerous: true
    }
  ]

  let pending: Action | null = null
  let busy = false
  let error = ''
  let destroyed = false
  let pollTimer: ReturnType<typeof setTimeout> | null = null
  let cancelPollDelay: (() => void) | null = null

  type Entry = { id: number; at: number; action: string; by: string; target: string; outcome: string }
  let log: Entry[] = []
  let nextEntryId = 1

  function request(action: Action) {
    pending = action
  }

  function updateEntry(id: number, outcome: string) {
    log = log.map((entry) => entry.id === id ? { ...entry, outcome } : entry)
  }

  async function confirm() {
    if (!pending || busy) return
    busy = true
    const action = pending
    const deviceId = device.id
    const target = deviceLabel(device)
    const agentStatus = device.status
    pending = null
    error = ''
    const entryId = nextEntryId++
    log = [{ id: entryId, at: Date.now(), action: action.label, by: 'you', target, outcome: 'Sending command…' }, ...log]
    try {
      const response = await apiFetch(apiURL(`/api/devices/${deviceId}/actions`), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: action.id })
      })
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}))
        throw new Error(payload.error || `request failed (${response.status})`)
      }
      const command = await response.json()
      if (!command?.id) throw new Error('Server did not return a command ID')
      await pollCommand(deviceId, agentStatus, command.id, entryId)
    } catch (e) {
      const message = e instanceof Error ? e.message : 'Action request failed'
      error = message
      updateEntry(entryId, `Failed: ${message}`)
    } finally {
      busy = false
    }
  }

  async function pollCommand(deviceId: string, agentStatus: string, commandId: string, entryId: number, maxAttempts = 60) {
    let lastError = ''
    for (let i = 0; i < maxAttempts; i++) {
      if (destroyed) return
      try {
        const response = await apiFetch(apiURL(`/api/devices/${deviceId}/commands/${commandId}`), { headers: authHeaders })
        if (!response.ok) {
          const payload = await response.json().catch(() => ({}))
          throw new Error(payload.error || `status request failed (${response.status})`)
        }
        const command = await response.json()
        lastError = ''
        if (command.status === 'completed') {
          updateEntry(entryId, command.message || 'Completed')
          return
        }
        if (command.status === 'failed') {
          updateEntry(entryId, `Failed: ${command.message || 'Agent rejected the action'}`)
          return
        }
        updateEntry(entryId, agentStatus === 'online'
          ? 'Queued; waiting for the agent…'
          : `Queued; device is ${agentStatus}. Waiting for reconnection…`)
      } catch (e) {
        lastError = e instanceof Error ? e.message : 'Unable to check action status'
      }
      if (i < maxAttempts - 1) {
        await new Promise<void>((resolve) => {
          const finish = () => {
            if (pollTimer) clearTimeout(pollTimer)
            pollTimer = null
            cancelPollDelay = null
            resolve()
          }
          cancelPollDelay = finish
          pollTimer = setTimeout(finish, 3000)
        })
      }
    }
    if (destroyed) return
    const message = lastError
      ? `Unable to check command status: ${lastError}`
      : 'Still queued; the agent may be offline. The command remains pending.'
    error = lastError
    updateEntry(entryId, message)
  }

  onDestroy(() => {
    destroyed = true
    cancelPollDelay?.()
  })
</script>

<section class="actions">
  <header class="screen-head">
    <h2>Remote actions</h2>
    <span class="target">target: {deviceLabel(device)}</span>
  </header>
  <div class="notice">
    These use fixed operations. The server's remote-mutations flag and the agent's local policy
    must both allow an action; package updates and reboot always require confirmation.
  </div>
  {#if error}<div class="error" role="alert">{error}</div>{/if}

  <div class="grid">
    {#each ACTIONS as action (action.id)}
      <article class="card" class:danger={action.dangerous}>
        <h3>{action.label}</h3>
        <p>{action.description}</p>
        <p class="consequence">{action.consequence}</p>
        <button class:danger={action.dangerous} on:click={() => request(action)} disabled={busy}>Run…</button>
      </article>
    {/each}
  </div>

  <article class="card">
    <header class="card-head"><h2>Recent action results</h2><span class="muted">this view only</span></header>
    {#if log.length === 0}
      <EmptyState icon="📋" title="No actions yet" message="Results appear here after the server and agent report the outcome." />
    {:else}
      <ul class="log">
        {#each log as entry (entry.id)}
          <li>
            <span class="when">{formatRelative(entry.at)}</span>
            <span class="who">{entry.by}</span>
            <span class="what">{entry.action}</span>
            <span class="target-name">{entry.target}</span>
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
  .error { padding: 0.6rem 0.8rem; border: 1px solid rgba(248, 113, 113, 0.4); border-radius: var(--radius-sm); background: var(--crit-dim); color: var(--crit); font-size: 0.78rem; }

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
  button:disabled { opacity: 0.55; cursor: default; }
  button.danger { border-color: rgba(248, 113, 113, 0.45); background: var(--crit-dim); color: var(--crit); }
  button.danger:hover { background: rgba(248, 113, 113, 0.25); }

  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; margin-bottom: 0.6rem; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .muted { color: var(--text-faint); font-size: 0.72rem; }

  .log { list-style: none; margin: 0; padding: 0; }
  .log li {
    display: grid; grid-template-columns: auto auto 1fr 1fr 1fr; gap: 0.6rem;
    padding: 0.45rem 0; border-bottom: 1px solid var(--border);
    font-size: 0.75rem; align-items: baseline;
  }
  .log li:last-child { border-bottom: 0; }
  .when { color: var(--text-faint); white-space: nowrap; }
  .who { color: var(--text-muted); }
  .what { color: var(--text); font-weight: 600; }
  .target-name { color: var(--text-muted); }
  .outcome { color: var(--warn); }

  @media (max-width: 700px) {
    .log li { grid-template-columns: 1fr; gap: 0.1rem; }
  }
</style>
