import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RemoteActions from './RemoteActions.svelte'

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

describe('RemoteActions screen', () => {
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
    render(RemoteActions, { props: { device, authHeaders: { Authorization: 'Bearer owner-session' } } })

    await fireEvent.click(screen.getAllByRole('button', { name: 'Run…' })[0])
    expect(screen.getByRole('alertdialog')).toBeTruthy()
    expect(calls).toHaveLength(0)
    await fireEvent.click(screen.getByRole('button', { name: 'Restart agent' }))

    expect(await screen.findByText('Agent restart requested')).toBeTruthy()
    expect(calls[0]).toMatchObject({
      url: '/api/devices/dev-1/actions',
      method: 'POST',
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
    render(RemoteActions, { props: { device } })

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
    render(RemoteActions, { props: { device: { ...device, status: 'offline' } } })

    await fireEvent.click(screen.getAllByRole('button', { name: 'Run…' })[0])
    await fireEvent.click(screen.getByRole('button', { name: 'Restart agent' }))
    expect(await screen.findByText(/Queued; device is offline\. Waiting for reconnection/)).toBeTruthy()
  })

  it('shows the server flag error and does not report a fake success', async () => {
    stubFetch((_url, method) => method === 'POST'
      ? json({ error: 'remote mutations are disabled' }, 503)
      : json({}, 404))
    render(RemoteActions, { props: { device } })

    await fireEvent.click(screen.getAllByRole('button', { name: 'Run…' })[0])
    await fireEvent.click(screen.getByRole('button', { name: 'Restart agent' }))
    expect((await screen.findByRole('alert')).textContent).toContain('remote mutations are disabled')
    expect(screen.getByText('Failed: remote mutations are disabled')).toBeTruthy()
  })

  it('requires explicit confirmation for package updates and reboot', async () => {
    const calls = stubFetch((_url, method) => method === 'POST'
      ? json({ error: 'remote mutations are disabled' }, 503)
      : json({}, 404))
    render(RemoteActions, { props: { device } })

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
})
