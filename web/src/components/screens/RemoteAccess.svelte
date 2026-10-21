<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte'
  import { Terminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'
  import { deviceLabel, type Device } from '../../lib/types'
  import { formatRelative } from '../../lib/format'
  import { apiFetch, apiURL } from '../../lib/api'
  import {
    MAX_TERMINAL_TABS,
    addTerminalTab,
    canOpenTerminal,
    closeTerminalTab,
    nextActiveTerminal,
    terminalAccountLabel,
    type TerminalTab
  } from '../../lib/terminal-tabs'

  export let device: Device
  export let authHeaders: Record<string, string> = {}

  // Interactive SSH terminals. Each tab owns a ticket, a WebSocket and an
  // xterm instance, so two or three sessions can run at once and switching
  // tabs never reconnects. Tickets are requested over the authenticated API
  // and redeemed on a same-origin WebSocket, so the session cookie rides along
  // without ever being exposed to the page.
  type LiveSession = {
    term: Terminal
    fitAddon: FitAddon
    socket: WebSocket | null
    el: HTMLDivElement
    observer: ResizeObserver | null
    disposed: boolean
  }

  let tabs: TerminalTab[] = []
  let activeTabId = ''
  let activeTab: TerminalTab | null = null
  let connectedCount = 0
  let canAddTab = true
  let nextTabId = 1
  let termError = ''
  const live = new Map<string, LiveSession>()

  $: activeTab = tabs.find((tab) => tab.id === activeTabId) ?? null
  $: connectedCount = tabs.filter((tab) => tab.status === 'connected').length
  $: canAddTab = canOpenTerminal(tabs)

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
    remeasureAfterRender()
  }

  // The terminal fills the part of the scroll area below its own top instead of
  // using a viewport guess: a topbar, a screen header, alerts and the tab strip
  // all change that offset. Measured against the scroll container so scrolling
  // does not resize it.
  let termHeight = 0
  let measureObserver: ResizeObserver | null = null

  function measureTerminal() {
    const main = document.querySelector<HTMLElement>('main.content')
    const pane = (activeTabId ? live.get(activeTabId)?.el : null)
      ?? document.querySelector<HTMLElement>('.terminal-card .terminal')
    if (!main || !pane) return
    const mainRect = main.getBoundingClientRect()
    const paneRect = pane.getBoundingClientRect()
    const topInContent = paneRect.top - mainRect.top + main.scrollTop
    termHeight = Math.max(240, Math.floor(main.clientHeight - topInContent - 24))
  }

  function remeasureAfterRender() {
    void tick().then(measureTerminal)
  }

  // Poll the reported state so a device that has just been enabled locally stops
  // reading as "off". The agent re-reports within a few seconds of the change.
  onMount(() => {
    void loadRemoteInfo()
    remeasureAfterRender()
    const timer = setInterval(() => {
      if (connectedCount === 0) void loadRemoteInfo()
    }, 10000)
    const onResize = () => measureTerminal()
    window.addEventListener('resize', onResize)
    if (typeof ResizeObserver !== 'undefined') {
      // Notices and alerts change the terminal's top without resizing the pane.
      const main = document.querySelector('main.content')
      if (main) {
        measureObserver = new ResizeObserver(() => measureTerminal())
        measureObserver.observe(main)
      }
    }
    return () => {
      clearInterval(timer)
      window.removeEventListener('resize', onResize)
      measureObserver?.disconnect()
    }
  })

  function findTab(id: string): TerminalTab | null {
    return tabs.find((tab) => tab.id === id) ?? null
  }

  function tabName(tab: TerminalTab): string {
    return tab.title || tab.label
  }

  // OSC titles are arbitrary terminal output: strip control characters and cap
  // the length so one program cannot wreck the strip.
  function cleanTitle(title: string): string {
    return title.replace(/[\u0000-\u001f\u007f]/g, ' ').trim().slice(0, 48)
  }

  // The server closes the browser socket with a reason when the device never
  // attaches. "session unavailable" is the generic one; name the likely cause.
  function closeMessage(reason?: string): string {
    const text = (reason || '').trim()
    if (text === 'session unavailable') {
      return 'The device refused the session. It may already have all six terminals open.'
    }
    return text || 'The session ended before it connected.'
  }

  function updateTab(id: string, patch: Partial<TerminalTab>) {
    if (!tabs.some((tab) => tab.id === id)) return
    tabs = tabs.map((tab) => tab.id === id ? { ...tab, ...patch } : tab)
  }

  // One xterm instance per pane. The pane element is keyed by tab, so a tab
  // keeps its renderer (and its scrollback) for as long as it exists.
  function mountTerminal(el: HTMLDivElement, tabId: string) {
    const term = new Terminal({ cursorBlink: true, fontFamily: 'monospace', fontSize: 13, theme: { background: '#0b0f12' } })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(el)
    const session: LiveSession = { term, fitAddon, socket: null, el, observer: null, disposed: false }
    live.set(tabId, session)
    term.onData((data) => {
      // Keystrokes are binary frames; text frames are reserved for control.
      if (session.socket && session.socket.readyState === WebSocket.OPEN) {
        session.socket.send(new TextEncoder().encode(data))
      }
    })
    // A program that sets the window title (btop, vim, less...) names its tab.
    term.onTitleChange((title) => {
      updateTab(tabId, { title: cleanTitle(title) })
    })
    if (typeof ResizeObserver !== 'undefined') {
      session.observer = new ResizeObserver(() => {
        if (tabId !== activeTabId) return
        fitSession(tabId)
        sendResize(tabId)
      })
      session.observer.observe(el)
    }
    if (tabId === activeTabId) fitSession(tabId)
    return {
      // Keyed each-blocks keep the element for the life of the tab, so the
      // parameter never changes and there is no update step; cleanup runs when
      // the tab is closed.
      destroy: () => disposeSession(tabId)
    }
  }

  function fitSession(id: string) {
    const session = live.get(id)
    if (!session) return
    // An inactive tab is display:none, so it has no measurable size and fitting
    // it would collapse the renderer. It is fitted when it becomes active.
    if (session.el.offsetWidth === 0 || session.el.offsetHeight === 0) return
    try {
      session.fitAddon.fit()
    } catch {
      // xterm throws while the element has no layout (first paint or a hidden
      // tab). The next resize or activation refits it.
    }
  }

  function sendResize(id: string) {
    const session = live.get(id)
    if (session?.socket && session.socket.readyState === WebSocket.OPEN) {
      session.socket.send(JSON.stringify({ type: 'resize', cols: session.term.cols, rows: session.term.rows }))
    }
  }

  function disposeSession(id: string) {
    const session = live.get(id)
    if (!session) return
    session.disposed = true
    session.observer?.disconnect()
    session.socket?.close()
    session.socket = null
    session.term.dispose()
    live.delete(id)
  }

  async function openTerminal() {
    if (!canOpenTerminal(tabs)) {
      termError = `At most ${MAX_TERMINAL_TABS} terminals can be open at once. Close one to open another.`
      return
    }
    termError = ''
    const id = `tab-${nextTabId++}`
    const user = selectedUser.trim()
    const tab: TerminalTab = {
      id,
      user,
      label: terminalAccountLabel(user),
      title: '',
      status: 'connecting',
      error: ''
    }
    tabs = addTerminalTab(tabs, tab)
    activeTabId = id
    // Let the new pane mount before the session starts so it gets the real size.
    await tick()
    remeasureAfterRender()
    void revealTab(id)
    await connectTab(id)
  }

  async function connectTab(id: string) {
    const session = live.get(id)
    const tab = findTab(id)
    if (!session || !tab) return
    updateTab(id, { status: 'connecting', error: '' })
    try {
      fitSession(id)
      const cols = session.term.cols ?? 0
      const rows = session.term.rows ?? 0
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/remote-access/session`), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ user: tab.user, cols, rows })
      })
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}))
        throw new Error(payload.error || `request failed (${response.status})`)
      }
      const { ticket } = await response.json()
      if (!ticket) throw new Error('server did not return a session ticket')
      // The tab may have been closed while the ticket request was in flight.
      if (!live.has(id)) return
      connectSocket(id, ticket)
    } catch (e) {
      if (!live.has(id)) return
      updateTab(id, { status: 'error', error: e instanceof Error ? e.message : 'Unable to start the session' })
      remeasureAfterRender()
    }
  }

  function connectSocket(id: string, ticket: string) {
    const session = live.get(id)
    if (!session) return
    session.term.reset()
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${window.location.host}${apiURL(`/api/devices/${device.id}/terminal`)}?ticket=${encodeURIComponent(ticket)}`
    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    session.socket = ws
    ws.onopen = () => {
      if (session.socket !== ws) return
      updateTab(id, { status: 'connected' })
      fitSession(id)
      sendResize(id)
      session.term.focus()
    }
    ws.onmessage = (event) => {
      if (typeof event.data === 'string') session.term.write(event.data)
      else session.term.write(new Uint8Array(event.data as ArrayBuffer))
    }
    ws.onerror = () => {
      if (session.socket !== ws) return
      updateTab(id, { status: 'error', error: 'The session connection failed' })
      remeasureAfterRender()
    }
    ws.onclose = (event) => {
      if (session.socket === ws) session.socket = null
      if (session.disposed) return
      const tab = findTab(id)
      // A socket that never opened was refused by the device (usually its
      // session cap); say so instead of silently printing [disconnected].
      if (tab && tab.status === 'connecting') {
        updateTab(id, { status: 'error', error: closeMessage(event?.reason) })
      } else {
        updateTab(id, { status: 'closed' })
      }
      session.term.writeln('\r\n\x1b[2m[disconnected]\x1b[0m')
      void loadSessions()
    }
  }

  async function selectTab(id: string) {
    if (id === activeTabId) return
    activeTabId = id
    await tick()
    fitSession(id)
    sendResize(id)
    live.get(id)?.term.focus()
    remeasureAfterRender()
    void revealTab(id)
  }

  async function closeTab(id: string) {
    const next = nextActiveTerminal(tabs, id)
    if (id === activeTabId) activeTabId = next
    tabs = closeTerminalTab(tabs, id)
    termError = ''
    // The pane unmounts and its action disposes the terminal and closes the
    // socket, so the other sessions are untouched. The socket close is
    // deliberate, so it does not announce itself; refresh the audit here.
    await tick()
    void loadSessions()
    if (activeTabId) {
      fitSession(activeTabId)
      sendResize(activeTabId)
      live.get(activeTabId)?.term.focus()
      remeasureAfterRender()
      void revealTab(activeTabId)
    }
  }

  // Keep the active tab visible when the strip is wider than the card.
  async function revealTab(id: string) {
    await tick()
    const element = document.getElementById(`term-tab-${id}`)
    // jsdom has no scrollIntoView; the guard also covers older browsers.
    element?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
  }

  function disconnectTab(id: string) {
    live.get(id)?.socket?.close()
  }

  onDestroy(() => {
    for (const id of [...live.keys()]) disposeSession(id)
  })
</script>

<section class="remote">
  <header class="screen-head">
    <h2>Remote access</h2>
    <span class="target">target: {deviceLabel(device)}</span>
  </header>

  <div class="remote-grid">
  <article class="card terminal-card">
    <header class="card-head term-head">
      <h2>SSH terminal</h2>
      <div class="term-tools">
        <span class="muted">{tabs.length}/{MAX_TERMINAL_TABS} · {connectedCount} connected</span>
        {#if remoteInfo && remoteInfo.users.length > 0}
          <select class="account" bind:value={selectedUser} aria-label="account for new terminal">
            {#each remoteInfo.users as user (user)}
              <option value={user}>{user}</option>
            {/each}
          </select>
        {:else}
          <input
            class="account"
            type="text"
            placeholder="user (optional)"
            bind:value={selectedUser}
            aria-label="account for new terminal"
          />
        {/if}
        {#if activeTab && activeTab.status === 'connected'}
          <button class="ghost" on:click={() => disconnectTab(activeTab.id)}>Disconnect</button>
        {/if}
      </div>
    </header>
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
    <div class="tabrow">
      <div class="tabstrip" role="tablist" aria-label="Terminal sessions">
        {#each tabs as tab (tab.id)}
          <div class="tab" class:active={tab.id === activeTabId}>
            <button
              type="button"
              role="tab"
              id={`term-tab-${tab.id}`}
              aria-selected={tab.id === activeTabId}
              aria-controls={`term-panel-${tab.id}`}
              on:click={() => selectTab(tab.id)}
            >
              <span
                class="tab-dot"
                class:on={tab.status === 'connected'}
                class:err={tab.status === 'error'}
                aria-hidden="true"
              ></span>
              <span class="tab-label">{tabName(tab)}</span>
            </button>
            <button
              type="button"
              class="tab-close"
              aria-label={`Close ${tabName(tab)} terminal`}
              on:click={() => closeTab(tab.id)}
            >×</button>
          </div>
        {/each}
      </div>
      <button
        type="button"
        class="tab-add"
        aria-label="New terminal tab"
        title={canAddTab ? 'New terminal' : `Close a tab to open another (max ${MAX_TERMINAL_TABS})`}
        on:click={openTerminal}
        disabled={!canAddTab}
      >+</button>
    </div>
    {#if termError}<div class="error" role="alert">{termError}</div>{/if}
    {#if activeTab?.error}
      <div class="error" role="alert">{activeTab.error}</div>
    {/if}
    {#if tabs.length === 0}
      <div class="terminal empty-terminal" style:height={termHeight ? `${termHeight}px` : null}>
        No terminal open. Press + to start one.
      </div>
    {:else}
      {#each tabs as tab (tab.id)}
        <div
          class="terminal"
          class:hidden={tab.id !== activeTabId}
          id={`term-panel-${tab.id}`}
          role="tabpanel"
          aria-labelledby={`term-tab-${tab.id}`}
          style:height={termHeight ? `${termHeight}px` : null}
          use:mountTerminal={tab.id}
        ></div>
      {/each}
    {/if}
  </article>

  <aside class="card sessions-card">
    <header class="card-head">
      <h2>Sessions</h2>
      <span class="muted">{sessions.length === 0 ? 'audit' : `${sessions.length} recent`}</span>
    </header>
    {#if sessions.length === 0}
      <p class="sessions-empty">
        No sessions yet. Opening a terminal records the account and when it ran here.
      </p>
    {:else}
      <ul class="sessions">
        {#each sessions as session (session.id)}
          <li>
            <span class="who">{session.user || 'default'}</span>
            <span class="when">{formatRelative(session.started_at)}</span>
            <span class="outcome" class:open={!session.ended_at}>
              {session.ended_at ? (session.reason || 'closed') : 'open'}
            </span>
          </li>
        {/each}
      </ul>
    {/if}
  </aside>
  </div>
</section>

<style>
  .remote { display: grid; gap: 0.75rem; }
  .screen-head { display: flex; align-items: baseline; justify-content: space-between; gap: 0.75rem; }
  .screen-head h2 { margin: 0; font-size: 0.95rem; color: var(--text-bright); }
  .target { color: var(--text-faint); font-size: 0.75rem; }

  .remote-grid {
    /* The aside is taken out of flow so its long audit list cannot stretch the
       row: the terminal card owns the height and the aside fills it. */
    position: relative;
    padding-right: 272px;
  }

  .notice {
    padding: 0.6rem 0.8rem; border: 1px solid rgba(251, 191, 36, 0.35);
    border-radius: var(--radius-sm); background: var(--warn-dim);
    color: var(--warn); font-size: 0.78rem; line-height: 1.5;
  }
  .error { padding: 0.6rem 0.8rem; border: 1px solid rgba(248, 113, 113, 0.4); border-radius: var(--radius-sm); background: var(--crit-dim); color: var(--crit); font-size: 0.78rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); padding: 0.9rem; overflow: hidden; }
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; margin-bottom: 0.6rem; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .muted { color: var(--text-faint); font-size: 0.72rem; }

  .terminal-card code { background: var(--bg-elev); padding: 0 0.25rem; border-radius: 3px; font-size: 0.72rem; }
  .term-head { margin-bottom: 0.25rem; }
  .term-tools { display: flex; align-items: center; gap: 0.4rem; min-width: 0; }
  .term-tools .account {
    width: 9.5rem;
    max-width: 38vw;
    padding: 0.34rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-elev);
    color: var(--text);
    font-size: 0.75rem;
  }
  .term-tools button.ghost {
    margin: 0;
    padding: 0.34rem 0.6rem;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    font-size: 0.75rem;
  }
  .term-tools button.ghost:hover {
    border-color: var(--accent-border);
    background: transparent;
    color: var(--text);
  }
  .terminal {
    /* termHeight (measured against the scroll area) sets the real height; this
       is only the pre-measurement fallback. */
    height: calc(100vh - 260px);
    height: calc(100dvh - 260px);
    min-height: 240px;
    margin-top: 0;
    padding: 0.4rem;
    overflow: hidden;
    border: 1px solid var(--border);
    border-style: none solid solid;
    border-radius: 0 0 var(--radius-sm) var(--radius-sm);
    background: #0b0f12;
  }
  .terminal.hidden { display: none; }
  .empty-terminal {
    display: grid;
    place-items: center;
    padding: 1rem;
    color: var(--text-faint);
    font-size: 0.8rem;
    text-align: center;
    border-style: dashed;
  }

  .tabrow { display: flex; align-items: stretch; gap: 0.25rem; margin-top: 0.1rem; }
  .tabstrip {
    display: flex;
    align-items: flex-end;
    gap: 0.25rem;
    flex: 0 1 auto;
    min-width: 0;
    overflow-x: auto;
  }
  .tab {
    display: flex;
    align-items: center;
    border: 1px solid var(--border);
    border-bottom: 0;
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
    background: var(--bg-elev);
  }
  .tab.active { border-color: var(--accent-border); background: #0b0f12; }
  .tab [role='tab'] {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    margin: 0;
    padding: 0.32rem 0.2rem 0.32rem 0.55rem;
    border: 0;
    background: transparent;
    color: var(--text-muted);
    font-size: 0.75rem;
    cursor: pointer;
    white-space: nowrap;
  }
  .tab.active [role='tab'] { color: var(--text-bright); }
  .tab [role='tab']:hover { background: transparent; color: var(--text); }
  .tab-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--text-faint); flex: none; }
  .tab-dot.on { background: var(--ok); }
  .tab-dot.err { background: var(--crit); }
  .tab-close {
    margin: 0;
    padding: 0.2rem 0.45rem;
    border: 0;
    background: transparent;
    color: var(--text-faint);
    font-size: 0.95rem;
    line-height: 1;
    cursor: pointer;
    border-radius: var(--radius-sm);
  }
  .tab-close:hover { background: var(--crit-dim); color: var(--crit); }
  .tab-add {
    display: grid;
    place-items: center;
    flex: none;
    width: 1.7rem;
    height: 1.7rem;
    margin: 0 0 0.05rem 0;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    font-size: 1.05rem;
    line-height: 1;
    cursor: pointer;
  }
  .tab-add:hover:not(:disabled) {
    border-color: var(--accent-border);
    background: var(--accent-dim);
    color: var(--accent);
  }
  .tab-add:disabled { opacity: 0.4; cursor: default; }
  .tab-label { max-width: 9.5rem; overflow: hidden; text-overflow: ellipsis; }

  .sessions-card {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: 260px;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .sessions-card .card-head { flex: none; }
  .sessions { list-style: none; margin: 0; padding: 0; overflow-y: auto; min-height: 0; flex: 1; }
  .sessions li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0.1rem 0.5rem;
    padding: 0.45rem 0;
    border-bottom: 1px solid var(--border);
    align-items: center;
  }
  .sessions li:last-child { border-bottom: 0; }
  .sessions .who { grid-column: 1; grid-row: 1; color: var(--text); font-weight: 600; font-size: 0.75rem; }
  .sessions .when { grid-column: 1; grid-row: 2; color: var(--text-faint); font-size: 0.7rem; }
  .sessions .outcome { grid-column: 2; grid-row: 1 / span 2; color: var(--text-muted); font-size: 0.7rem; }
  .sessions .outcome.open { color: var(--ok); }
  .sessions-empty { margin: 0; color: var(--text-faint); font-size: 0.75rem; line-height: 1.5; }

  @media (max-width: 760px) {
    .remote-grid { padding-right: 0; }
    .sessions-card {
      position: static;
      width: auto;
      max-height: 220px;
      margin-top: 0.75rem;
    }
  }
</style>
