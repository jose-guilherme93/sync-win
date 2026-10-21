import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RemoteAccess from './RemoteAccess.svelte'

// xterm needs a real canvas/DOM; the behaviour under test is the session
// handshake, the tab strip and per-tab WebSocket routing, so the renderer is
// stubbed. Each instance records what was written to it so tests can prove
// output does not leak between tabs.
const xterm = vi.hoisted(() => ({ terminals: [] as any[] }))

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 120
    rows = 32
    writes: string[] = []
    dataHandler: ((data: string) => void) | null = null
    titleHandler: ((title: string) => void) | null = null
    focused = false
    disposed = false
    constructor() {
      xterm.terminals.push(this)
    }
    loadAddon() {}
    open() {}
    onData(handler: (data: string) => void) {
      this.dataHandler = handler
    }
    onTitleChange(handler: (title: string) => void) {
      this.titleHandler = handler
    }
    write(data: string | Uint8Array) {
      this.writes.push(typeof data === 'string' ? data : '<bytes>')
    }
    writeln(data: string) {
      this.writes.push(data)
    }
    reset() {
      this.writes = []
    }
    focus() {
      this.focused = true
    }
    dispose() {
      this.disposed = true
    }
  }
}))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: class { fit() {} } }))

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

const device = {
  id: 'dev-1',
  hostname: 'workstation',
  user_id: 'user-1',
  status: 'online',
  last_seen_at: '2026-10-07T12:00:00Z',
  last_sync_at: '2026-10-07T12:00:00Z',
  app_count: 0,
  preference_count: 0,
  saves_count: 0,
  saves_size_bytes: 0
}

function stubFetch(handler: (url: string, method: string, body: any) => Response | Promise<Response>) {
  const calls: { url: string; method: string; body: any }[] = []
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method || 'GET'
    const body = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined
    calls.push({ url, method, body })
    return handler(url, method, body)
  }))
  return calls
}

function remoteInfo(overrides: Record<string, unknown> = {}) {
  return json({
    server_enabled: true,
    device_enabled: true,
    default_user: 'alice',
    users: ['alice', 'bob'],
    connected: true,
    ...overrides
  })
}

// A ticket per open request, so a test can tell two concurrent sessions apart.
function ticketFetch(options: { session?: (body: any) => Response | Promise<Response> } = {}) {
  let tickets = 0
  return stubFetch((url, method, body) => {
    if (method === 'POST' && url.endsWith('/remote-access/session')) {
      if (options.session) return options.session(body)
      tickets += 1
      return json({ ticket: `tkt-${tickets}`, user: body?.user ?? '' })
    }
    if (url.endsWith('/remote-access/sessions')) return json({ sessions: [] })
    if (url.endsWith('/remote-access')) return remoteInfo()
    return json({}, 404)
  })
}

class FakeSocket {
  static OPEN = 1
  static instances: FakeSocket[] = []
  readyState = 0
  binaryType = ''
  sent: any[] = []
  closed = false
  onopen: (() => void) | null = null
  onclose: ((event?: { reason?: string }) => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: { data: string | ArrayBuffer }) => void) | null = null

  constructor(public url: string) {
    FakeSocket.instances.push(this)
  }
  open() {
    this.readyState = 1
    this.onopen?.()
  }
  send(data: any) {
    this.sent.push(data)
  }
  emit(data: string | ArrayBuffer) {
    this.onmessage?.({ data })
  }
  fail() {
    this.onerror?.()
  }
  close() {
    if (this.closed) return
    this.closed = true
    this.readyState = 3
    this.onclose?.()
  }
  closeWith(reason: string) {
    if (this.closed) return
    this.closed = true
    this.readyState = 3
    this.onclose?.({ reason })
  }
}

async function renderScreen(overrides: Record<string, unknown> = {}, props: Record<string, unknown> = {}) {
  render(RemoteAccess, {
    props: { device: { ...device, ...overrides }, authHeaders: { Authorization: 'Bearer owner-session' }, ...props }
  })
  await screen.findByRole('combobox')
}

async function openTerminal(count: number) {
  for (let i = 0; i < count; i++) await openAnotherTerminal()
}

async function openAnotherTerminal() {
  const before = FakeSocket.instances.length
  await fireEvent.click(screen.getByRole('button', { name: 'New terminal tab' }))
  await vi.waitFor(() => expect(FakeSocket.instances).toHaveLength(before + 1))
  FakeSocket.instances[before].open()
}

describe('RemoteAccess screen', () => {
  beforeEach(() => {
    xterm.terminals = []
    FakeSocket.instances = []
    vi.stubGlobal('WebSocket', FakeSocket as unknown as typeof WebSocket)
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('waits for confirmation, queues a fixed action, and displays its result', async () => {
    const calls = stubFetch((url, method) => {
      if (method === 'POST') return json({ id: 'cmd-1' }, 202)
      if (url.endsWith('/commands/cmd-1')) return json({ status: 'completed', message: 'Agent restart requested' })
      return json({ error: 'unexpected request' }, 404)
    })
    render(RemoteAccess, { props: { device, authHeaders: { Authorization: 'Bearer owner-session' } } })

    await fireEvent.click(screen.getAllByRole('button', { name: 'Run…' })[0])
    expect(screen.getByRole('alertdialog')).toBeTruthy()
    expect(calls.filter((call) => call.method === 'POST')).toHaveLength(0)
    await fireEvent.click(screen.getByRole('button', { name: 'Restart agent' }))

    expect(await screen.findByText('Agent restart requested')).toBeTruthy()
    expect(calls.find((call) => call.method === 'POST')).toMatchObject({
      url: '/api/devices/dev-1/actions',
      body: { action: 'restart_agent' }
    })
    expect(calls.some((call) => call.url.endsWith('/commands/cmd-1'))).toBe(true)
  })

  it('shows an agent policy failure instead of claiming the action succeeded', async () => {
    stubFetch((url, method) => {
      if (method === 'POST') return json({ id: 'cmd-2' }, 202)
      if (url.endsWith('/commands/cmd-2')) return json({ status: 'failed', message: 'command disabled by local policy' })
      return json({}, 404)
    })
    render(RemoteAccess, { props: { device } })

    await fireEvent.click(screen.getAllByRole('button', { name: 'Run…' })[0])
    await fireEvent.click(screen.getByRole('button', { name: 'Restart agent' }))
    expect(await screen.findByText('Failed: command disabled by local policy')).toBeTruthy()
  })

  it('keeps offline-device commands visibly queued', async () => {
    stubFetch((url, method) => {
      if (method === 'POST') return json({ id: 'cmd-3' }, 202)
      if (url.endsWith('/commands/cmd-3')) return json({ status: 'queued' })
      return json({}, 404)
    })
    render(RemoteAccess, { props: { device: { ...device, status: 'offline' } } })

    await fireEvent.click(screen.getAllByRole('button', { name: 'Run…' })[0])
    await fireEvent.click(screen.getByRole('button', { name: 'Restart agent' }))
    expect(await screen.findByText(/Queued; device is offline\. Waiting for reconnection/)).toBeTruthy()
  })

  it('shows the server flag error and does not report a fake success', async () => {
    stubFetch((_url, method) => method === 'POST'
      ? json({ error: 'remote mutations are disabled' }, 503)
      : json({}, 404))
    render(RemoteAccess, { props: { device } })

    await fireEvent.click(screen.getAllByRole('button', { name: 'Run…' })[0])
    await fireEvent.click(screen.getByRole('button', { name: 'Restart agent' }))
    expect((await screen.findByRole('alert')).textContent).toContain('remote mutations are disabled')
    expect(screen.getByText('Failed: remote mutations are disabled')).toBeTruthy()
  })

  it('requires explicit confirmation for package updates and reboot', async () => {
    const calls = stubFetch((_url, method) => method === 'POST'
      ? json({ error: 'remote mutations are disabled' }, 503)
      : json({}, 404))
    render(RemoteAccess, { props: { device } })

    for (const [index, confirmLabel, consequence] of [
      [1, 'Apply updates', 'Updates may replace the running kernel'],
      [2, 'Reboot now', 'The reboot is scheduled about one minute']
    ] as const) {
      await fireEvent.click(screen.getAllByRole('button', { name: 'Run…' })[index])
      const dialog = screen.getByRole('alertdialog')
      expect(dialog.textContent).toContain(consequence)
      expect(calls.filter((call) => call.method === 'POST')).toHaveLength(index - 1)
      await fireEvent.click(within(dialog).getByRole('button', { name: confirmLabel }))
      await vi.waitFor(() => expect(calls.filter((call) => call.method === 'POST')).toHaveLength(index))
      await screen.findByText('Failed: remote mutations are disabled')
      await vi.waitFor(() => expect(screen.getAllByRole('button', { name: 'Run…' })[0].hasAttribute('disabled')).toBe(false))
    }
  })

  it('opens a terminal in a tab as the chosen account', async () => {
    const calls = ticketFetch()
    await renderScreen()
    await openTerminal(1)

    expect(calls.filter((call) => call.method === 'POST')).toHaveLength(1)
    expect(calls.find((call) => call.method === 'POST')?.body).toMatchObject({ user: 'alice', cols: 120, rows: 32 })
    expect(screen.getByRole('tab', { name: /alice/ }).getAttribute('aria-selected')).toBe('true')
    expect(FakeSocket.instances[0].url).toContain('/api/devices/dev-1/terminal?ticket=tkt-1')
  })

  it('opens a second terminal in its own tab with its own session', async () => {
    const calls = ticketFetch()
    await renderScreen()
    await openTerminal(2)

    expect(screen.getAllByRole('tab')).toHaveLength(2)
    expect(FakeSocket.instances).toHaveLength(2)
    expect(FakeSocket.instances[0].url).toContain('ticket=tkt-1')
    expect(FakeSocket.instances[1].url).toContain('ticket=tkt-2')
    expect(calls.filter((call) => call.method === 'POST')).toHaveLength(2)
    // The first session stays connected when the second opens.
    expect(FakeSocket.instances[0].closed).toBe(false)
  })

  it('shows the account on repeated tabs until a program sets a title', async () => {
    ticketFetch()
    await renderScreen()
    await openTerminal(2)

    expect(screen.getAllByRole('tab', { name: 'alice' })).toHaveLength(2)

    xterm.terminals[1].titleHandler?.('btop')

    expect(await screen.findByRole('tab', { name: 'btop' })).toBeTruthy()
    expect(screen.getAllByRole('tab', { name: 'alice' })).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'Close btop terminal' })).toBeTruthy()
  })

  it('keeps every session alive while switching between tabs', async () => {
    ticketFetch()
    await renderScreen()
    await openTerminal(2)

    const [first, second] = screen.getAllByRole('tab')
    await fireEvent.click(first)
    expect(first.getAttribute('aria-selected')).toBe('true')
    expect(second.getAttribute('aria-selected')).toBe('false')
    expect(FakeSocket.instances.every((socket) => !socket.closed)).toBe(true)

    // Output lands on the terminal instance of the tab that received it.
    FakeSocket.instances[0].emit('from-first\r\n')
    FakeSocket.instances[1].emit('from-second\r\n')
    expect(xterm.terminals[0].writes).toContain('from-first\r\n')
    expect(xterm.terminals[0].writes).not.toContain('from-second\r\n')
    expect(xterm.terminals[1].writes).toContain('from-second\r\n')
  })

  it('sends keystrokes only to the socket of the focused tab', async () => {
    ticketFetch()
    await renderScreen()
    await openTerminal(2)

    xterm.terminals[1].dataHandler?.('uptime\n')
    // Control frames (the resize on open) are JSON text; only binary frames
    // are keystrokes, so look at those.
    const keystrokes = FakeSocket.instances[1].sent.filter((frame) => typeof frame !== 'string')
    expect(keystrokes).toHaveLength(1)
    expect(new TextDecoder().decode(keystrokes[0])).toBe('uptime\n')
    expect(FakeSocket.instances[0].sent.every((frame) => typeof frame === 'string')).toBe(true)
  })

  it('closes only the tab that was closed and keeps the other session', async () => {
    const calls = ticketFetch()
    await renderScreen()
    await openTerminal(2)

    await fireEvent.click(screen.getAllByRole('button', { name: 'Close alice terminal' })[0])

    expect(FakeSocket.instances[0].closed).toBe(true)
    expect(FakeSocket.instances[1].closed).toBe(false)
    expect(screen.getAllByRole('tab')).toHaveLength(1)
    expect(screen.getAllByRole('tab')[0].getAttribute('aria-selected')).toBe('true')
    // Closing a tab must not ask the server for another session.
    expect(calls.filter((call) => call.method === 'POST')).toHaveLength(2)
  })

  it('stops offering a seventh terminal at the concurrent-session limit', async () => {
    const calls = ticketFetch()
    await renderScreen()
    await openTerminal(6)

    const button = screen.getByRole('button', { name: 'New terminal tab' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect(screen.getByText(/6 of 6 terminals open\. Close a tab to open another\./)).toBeTruthy()

    await fireEvent.click(button)
    expect(calls.filter((call) => call.method === 'POST')).toHaveLength(6)
  })

  it('reports a failed ticket in its own tab without disturbing the others', async () => {
    let sessions = 0
    ticketFetch({
      session: () => {
        sessions += 1
        if (sessions === 2) return json({ error: 'device is not connected to the tunnel' }, 409)
        return json({ ticket: `tkt-${sessions}` })
      }
    })
    await renderScreen()
    await openTerminal(1)
    await fireEvent.click(screen.getByRole('button', { name: 'New terminal tab' }))
    await vi.waitFor(() => expect(screen.getAllByRole('tab')).toHaveLength(2))

    expect((await screen.findByRole('alert')).textContent).toContain('device is not connected to the tunnel')
    expect(FakeSocket.instances).toHaveLength(1)
    expect(FakeSocket.instances[0].closed).toBe(false)
    expect(screen.getAllByRole('tab')[0].getAttribute('aria-selected')).toBe('false')
  })

  it('does not open a socket when the tab closes while its ticket is in flight', async () => {
    const pending: { resolve: ((response: Response) => void) | null } = { resolve: null }
    ticketFetch({ session: () => new Promise<Response>((resolve) => { pending.resolve = resolve }) })
    await renderScreen()

    await fireEvent.click(screen.getByRole('button', { name: 'New terminal tab' }))
    await vi.waitFor(() => expect(screen.getAllByRole('tab')).toHaveLength(1))
    await fireEvent.click(screen.getByRole('button', { name: 'Close alice terminal' }))
    pending.resolve?.(json({ ticket: 'late-ticket' }))
    await vi.waitFor(() => expect(screen.queryAllByRole('tab')).toHaveLength(0))

    expect(FakeSocket.instances).toHaveLength(0)
  })

  it('disconnects the active tab but keeps its output on screen', async () => {
    ticketFetch()
    await renderScreen()
    await openTerminal(1)

    await fireEvent.click(screen.getByRole('button', { name: 'Disconnect' }))

    expect(FakeSocket.instances[0].closed).toBe(true)
    expect(screen.getAllByRole('tab')).toHaveLength(1)
    expect(screen.queryByRole('button', { name: 'Disconnect' })).toBeNull()
    expect(screen.getByText(/not connected/)).toBeTruthy()
  })

  it('uses the account chosen before the tab was created', async () => {
    const calls = ticketFetch()
    await renderScreen()

    await fireEvent.change(screen.getByRole('combobox'), { target: { value: 'bob' } })
    await openTerminal(1)
    // Changing the picker afterwards must not retarget the running session.
    await fireEvent.change(screen.getByRole('combobox'), { target: { value: 'alice' } })
    await openTerminal(1)

    const posts = calls.filter((call) => call.method === 'POST')
    expect(posts.map((call) => call.body.user)).toEqual(['bob', 'alice'])
    expect(screen.getByRole('tab', { name: 'bob' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'alice' })).toBeTruthy()
  })

  it('routes tickets that resolve out of order to their own tabs', async () => {
    const pending: Array<(response: Response) => void> = []
    ticketFetch({ session: () => new Promise<Response>((resolve) => { pending.push(resolve) }) })
    await renderScreen()

    await fireEvent.click(screen.getByRole('button', { name: 'New terminal tab' }))
    await fireEvent.click(screen.getByRole('button', { name: 'New terminal tab' }))
    await vi.waitFor(() => expect(pending).toHaveLength(2))

    // The second request answers first; each tab must still redeem its own ticket.
    pending[1](json({ ticket: 'tkt-second' }))
    await vi.waitFor(() => expect(FakeSocket.instances).toHaveLength(1))
    pending[0](json({ ticket: 'tkt-first' }))
    await vi.waitFor(() => expect(FakeSocket.instances).toHaveLength(2))

    expect(FakeSocket.instances[0].url).toContain('ticket=tkt-second')
    expect(FakeSocket.instances[1].url).toContain('ticket=tkt-first')
  })

  it('says why a session the device refused never connected', async () => {
    ticketFetch()
    await renderScreen()
    await fireEvent.click(screen.getByRole('button', { name: 'New terminal tab' }))
    await vi.waitFor(() => expect(FakeSocket.instances).toHaveLength(1))

    // The server closes with this reason when the agent never attaches, which
    // is what happens at the device's six-session cap.
    FakeSocket.instances[0].closeWith('session unavailable')

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('refused the session')
    expect(alert.textContent).toContain('six terminals')
    expect(screen.getByRole('tab')).toBeTruthy()
  })

  it('marks only the tab whose socket failed', async () => {    ticketFetch()
    await renderScreen()
    await openTerminal(2)

    FakeSocket.instances[1].fail()

    expect((await screen.findByRole('alert')).textContent).toContain('The session connection failed')
    expect(screen.getAllByRole('alert')).toHaveLength(1)
    expect(FakeSocket.instances[0].closed).toBe(false)
    expect(xterm.terminals[0].disposed).toBe(false)
  })

  it('closes the active middle tab and activates its neighbour', async () => {
    ticketFetch()
    await renderScreen()
    await openTerminal(3)

    const [, middle] = screen.getAllByRole('tab')
    await fireEvent.click(middle)
    await fireEvent.click(screen.getAllByRole('button', { name: /^Close .* terminal$/ })[1])

    const remaining = screen.getAllByRole('tab')
    expect(remaining).toHaveLength(2)
    expect(remaining[1].getAttribute('aria-selected')).toBe('true')
    expect(FakeSocket.instances[1].closed).toBe(true)
    expect(FakeSocket.instances[0].closed).toBe(false)
    expect(FakeSocket.instances[2].closed).toBe(false)
  })

  it('returns to the empty state after the last tab closes and can open a fresh one', async () => {
    const calls = ticketFetch()
    await renderScreen()
    await openTerminal(1)

    await fireEvent.click(screen.getByRole('button', { name: 'Close alice terminal' }))
    expect(screen.getByText(/No terminal open/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'New terminal tab' }) as HTMLButtonElement).disabled).toBe(false)
    expect(xterm.terminals[0].disposed).toBe(true)

    await openTerminal(1)
    expect(FakeSocket.instances).toHaveLength(2)
    expect(FakeSocket.instances[1].url).toContain('ticket=tkt-2')
    expect(calls.filter((call) => call.method === 'POST')).toHaveLength(2)
  })

  it('warns when the device reports remote access off, but still lets you try', async () => {
    stubFetch((url) => {
      if (url.endsWith('/remote-access')) {
        return remoteInfo({ device_enabled: false, default_user: '', users: ['alice'] })
      }
      return json({}, 404)
    })
    render(RemoteAccess, { props: { device } })

    expect(await screen.findByText(/last reported remote access as off/)).toBeTruthy()
    // The reported flag can lag a local `set ssh on`, so the button stays usable
    // and the agent is the authority that refuses a disabled session.
    expect((screen.getByRole('button', { name: 'New terminal tab' }) as HTMLButtonElement).disabled).toBe(false)
    expect(screen.getByRole('combobox')).toBeTruthy()
  })
})
