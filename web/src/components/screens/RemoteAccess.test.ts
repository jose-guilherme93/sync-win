import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RemoteAccess from './RemoteAccess.svelte'

// xterm needs a real canvas/DOM; the terminal behaviour under test is the API
// handshake and the WebSocket, so the renderer is stubbed.
vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    loadAddon() {}
    open() {}
    onData() {}
    write() {}
    writeln() {}
    reset() {}
    focus() {}
    dispose() {}
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

function stubFetch(handler: (url: string, method: string, body: any) => Response) {
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

describe('RemoteAccess screen', () => {
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

  it('requests a session ticket and opens the terminal WebSocket', async () => {
    const calls = stubFetch((url) => {
      if (url.endsWith('/remote-access/session')) return json({ ticket: 'tkt-1', user: 'alice' })
      if (url.endsWith('/remote-access')) {
        return json({ server_enabled: true, device_enabled: true, default_user: 'alice', users: ['alice', 'bob'], connected: true })
      }
      return json({}, 404)
    })
    const sockets: string[] = []
    class FakeWebSocket {
      static OPEN = 1
      readyState = 1
      binaryType = ''
      onopen: (() => void) | null = null
      onclose: (() => void) | null = null
      onerror: (() => void) | null = null
      onmessage: ((event: unknown) => void) | null = null
      constructor(url: string) {
        sockets.push(url)
      }
      send() {}
      close() {
        this.onclose?.()
      }
    }
    vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket)

    render(RemoteAccess, { props: { device, authHeaders: { Authorization: 'Bearer owner-session' } } })
    await fireEvent.click(screen.getByRole('button', { name: 'Open terminal' }))

    await vi.waitFor(() =>
      expect(calls.some((call) => call.url.endsWith('/remote-access/session'))).toBe(true)
    )
    await vi.waitFor(() => expect(sockets).toHaveLength(1))
    expect(sockets[0]).toContain('/api/devices/dev-1/terminal?ticket=tkt-1')
  })

  it('warns when the device reports remote access off, but still lets you try', async () => {
    stubFetch((url) => {
      if (url.endsWith('/remote-access')) {
        return json({ server_enabled: true, device_enabled: false, default_user: '', users: ['alice'], connected: true })
      }
      return json({}, 404)
    })
    render(RemoteAccess, { props: { device } })

    expect(await screen.findByText(/last reported remote access as off/)).toBeTruthy()
    // The reported flag can lag a local `set ssh on`, so the button stays usable
    // and the agent is the authority that refuses a disabled session.
    expect((screen.getByRole('button', { name: 'Open terminal' }) as HTMLButtonElement).disabled).toBe(false)
    expect(screen.getByRole('combobox')).toBeTruthy()
  })
})
