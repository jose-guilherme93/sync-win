<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { Terminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'
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

  // Interactive SSH terminal. The ticket is requested over the authenticated
  // API, then redeemed on a same-origin WebSocket so the session cookie rides
  // along without ever exposing it to the page.
  let termEl: HTMLDivElement
  let term: Terminal | null = null
  let fitAddon: FitAddon | null = null
  let socket: WebSocket | null = null
  let connecting = false
  let connected = false
  let termError = ''
  let resizeObserver: ResizeObserver | null = null

  // State reported by the device: whether the server and the device allow
  // remote access, which account is the default, and which accounts exist.
  type RemoteInfo = {
    server_enabled: boolean
    device_enabled: boolean
    default_user: string
    users: string[]
    connected: boolean
  }
  let remoteInfo: RemoteInfo | null = null
  let remoteInfoError = ''
  let selectedUser = ''
  type Session = { id: string; user: string; started_at: string; ended_at?: string; reason?: string }
  let sessions: Session[] = []

  async function loadSessions() {
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/remote-access/sessions`), { headers: authHeaders })
      if (!response.ok) return
      const payload = await response.json()
      sessions = Array.isArray(payload.sessions) ? payload.sessions : []
    } catch {
      // Auditing is best-effort; the terminal must not depend on it.
    }
  }

  async function loadRemoteInfo() {
    remoteInfoError = ''
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/remote-access`), { headers: authHeaders })
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}))
        throw new Error(payload.error || `request failed (${response.status})`)
      }
      remoteInfo = await response.json()
      if (selectedUser === '' && remoteInfo?.default_user) selectedUser = remoteInfo.default_user
    } catch (e) {
      remoteInfoError = e instanceof Error ? e.message : 'Unable to load remote access state'
    }
    await loadSessions()
  }

  // Poll the reported state so a device that has just been enabled locally stops
  // reading as "off". The agent re-reports within a few seconds of the change.
  onMount(() => {
    void loadRemoteInfo()
    const timer = setInterval(() => {
      if (!connected) void loadRemoteInfo()
    }, 10000)
    return () => clearInterval(timer)
  })

  function ensureTerminal() {
    if (term || !termEl) return
    term = new Terminal({ cursorBlink: true, fontFamily: 'monospace', fontSize: 13, theme: { background: '#0b0f12' } })
    fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(termEl)
    fitAddon.fit()
    term.onData((data) => {
      // Keystrokes are binary frames; text frames are reserved for control.
      if (socket && socket.readyState === WebSocket.OPEN) socket.send(new TextEncoder().encode(data))
    })
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => {
        fitAddon?.fit()
        sendResize()
      })
      resizeObserver.observe(termEl)
    }
  }

  function sendResize() {
    if (socket && socket.readyState === WebSocket.OPEN && term) {
      socket.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
    }
  }

  async function openTerminal() {
    if (connecting || connected) return
    connecting = true
    termError = ''
    try {
      // Mount the terminal first so the session starts at the real size.
      ensureTerminal()
      fitAddon?.fit()
      const cols = term?.cols ?? 0
      const rows = term?.rows ?? 0
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/remote-access/session`), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ user: selectedUser.trim(), cols, rows })
      })
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}))
        throw new Error(payload.error || `request failed (${response.status})`)
      }
      const { ticket } = await response.json()
      if (!ticket) throw new Error('server did not return a session ticket')
      connectTerminal(ticket)
    } catch (e) {
      termError = e instanceof Error ? e.message : 'Unable to start the session'
    } finally {
      connecting = false
    }
  }

  function connectTerminal(ticket: string) {
    ensureTerminal()
    term?.reset()
    term?.writeln('\x1b[2mConnecting…\x1b[0m')
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${window.location.host}${apiURL(`/api/devices/${device.id}/terminal`)}?ticket=${encodeURIComponent(ticket)}`
    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    socket = ws
    ws.onopen = () => {
      connected = true
      fitAddon?.fit()
      sendResize()
      term?.focus()
    }
    ws.onmessage = (event) => {
      if (typeof event.data === 'string') term?.write(event.data)
      else term?.write(new Uint8Array(event.data as ArrayBuffer))
    }
    ws.onerror = () => { termError = 'The session connection failed' }
    ws.onclose = () => {
      connected = false
      if (socket === ws) socket = null
      term?.writeln('\r\n\x1b[2m[disconnected]\x1b[0m')
      void loadSessions()
    }
  }

  function closeTerminal() {
    socket?.close()
    socket = null
    connected = false
  }

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
    socket?.close()
    socket = null
    resizeObserver?.disconnect()
    term?.dispose()
    term = null
  })
</script>

<section class="actions">
  <header class="screen-head">
    <h2>Remote access</h2>
    <span class="target">target: {deviceLabel(device)}</span>
  </header>

  <article class="card terminal-card">
    <header class="card-head">
      <h2>SSH terminal</h2>
      <span class="muted">{connected ? 'connected' : 'not connected'}</span>
    </header>
    <p class="consequence">
      Opens a shell as the chosen user through the device's own sshd. Remote access must be
      enabled on the device first (<code>sync-win-agent set ssh on</code>). Leave the user empty
      to use the device's configured account.
    </p>
    {#if remoteInfo && !remoteInfo.server_enabled}
      <div class="notice">Remote access is disabled on the server (<code>SYNCWIN_ENABLE_REMOTE_ACCESS</code>).</div>
    {:else if remoteInfo && !remoteInfo.device_enabled}
      <div class="notice">
        The device last reported remote access as off. If you just enabled it locally
        (<code>sudo sync-win-agent set ssh on</code>), give it a few seconds — you can still try.
      </div>
    {:else if remoteInfo && !remoteInfo.connected}
      <div class="notice">The device is not connected to the remote tunnel yet.</div>
    {/if}
    {#if remoteInfoError}<div class="error">{remoteInfoError}</div>{/if}
    <div class="term-controls">
      {#if remoteInfo && remoteInfo.users.length > 0}
        <select bind:value={selectedUser} disabled={connected || connecting} aria-label="remote user">
          {#each remoteInfo.users as user (user)}
            <option value={user}>{user}</option>
          {/each}
        </select>
      {:else}
        <input
          type="text"
          placeholder="user (optional)"
          bind:value={selectedUser}
          disabled={connected || connecting}
          aria-label="remote user"
        />
      {/if}
      {#if connected}
        <button on:click={closeTerminal}>Disconnect</button>
      {:else}
        <button on:click={openTerminal} disabled={connecting}>{connecting ? 'Connecting…' : 'Open terminal'}</button>
      {/if}
    </div>
    {#if termError}<div class="error" role="alert">{termError}</div>{/if}
    <div class="terminal" bind:this={termEl}></div>
  </article>

  <article class="card">
    <header class="card-head"><h2>Recent sessions</h2><span class="muted">audit</span></header>
    {#if sessions.length === 0}
      <EmptyState icon="🖥️" title="No sessions yet" message="Opened terminals are recorded here with the account and when they ran." />
    {:else}
      <ul class="sessions">
        {#each sessions as session (session.id)}
          <li>
            <span class="who">{session.user || 'default'}</span>
            <span class="when">{formatRelative(session.started_at)}</span>
            <span class="outcome">{session.ended_at ? (session.reason || 'closed') : 'open'}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </article>

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

  .terminal-card code { background: var(--bg-elev); padding: 0 0.25rem; border-radius: 3px; font-size: 0.72rem; }
  .term-controls { display: flex; gap: 0.5rem; margin: 0.5rem 0; }
  .term-controls input,
  .term-controls select {
    flex: 1; padding: 0.45rem 0.6rem; border: 1px solid var(--border);
    border-radius: var(--radius-sm); background: var(--bg-elev); color: var(--text);
    font-size: 0.78rem;
  }
  .terminal {
    /* Fill most of the viewport height (minus the topbar, screen header and the
       terminal controls) and stay drag-resizable. The ResizeObserver refits the
       terminal to the container on resize. */
    /* The vh line is the fallback; dvh overrides it on browsers that support
       it. If dvh were the only rule, an older browser would drop the whole
       declaration and the terminal would collapse to its content height. */
    height: calc(100vh - 210px);
    height: calc(100dvh - 210px);
    min-height: 520px;
    margin-top: 0.5rem;
    padding: 0.4rem;
    overflow: hidden;
    resize: vertical;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: #0b0f12;
  }

  .sessions { list-style: none; margin: 0; padding: 0; }
  .sessions li {
    display: grid; grid-template-columns: 1fr auto auto; gap: 0.75rem;
    padding: 0.4rem 0; border-bottom: 1px solid var(--border);
    font-size: 0.75rem; align-items: baseline;
  }
  .sessions li:last-child { border-bottom: 0; }
  .sessions .who { color: var(--text); font-weight: 600; }
  .sessions .when { color: var(--text-faint); white-space: nowrap; }
  .sessions .outcome { color: var(--text-muted); }

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
