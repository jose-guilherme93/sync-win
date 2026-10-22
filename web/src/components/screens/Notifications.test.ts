import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Notifications from './Notifications.svelte'
import type { Device } from '../../lib/types'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

const EVENTS = [
  { type: 'device_offline', label: 'Device goes offline', description: 'The agent stopped reporting.', severity: 'crit' },
  { type: 'device_online', label: 'Device comes back online', description: 'The agent reported again.', severity: 'good' },
  { type: 'sync_error', label: 'Sync error', description: 'The agent reported a failure.', severity: 'warn' },
  { type: 'device_enrolled', label: 'New device enrolled', description: 'A new device registered.', severity: 'info' }
]

const HISTORY = [
  {
    id: 1,
    type: 'device_offline',
    device_id: 'dev-1',
    hostname: 'test-host',
    message: 'Device went offline',
    created_at: '2026-10-18T12:00:00Z',
    read: false
  },
  {
    id: 2,
    type: 'sync_error',
    device_id: 'dev-1',
    hostname: 'test-host',
    message: 'A sync failed',
    created_at: '2026-10-18T12:01:00Z',
    read: true
  }
]

function device(): Device {
  return {
    id: 'dev-1',
    hostname: 'test-host',
    user_id: 'user-1',
    status: 'online',
    last_seen_at: '2026-10-18T12:00:00Z',
    last_sync_at: '2026-10-18T12:00:00Z',
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0
  }
}

type Call = { url: string; init?: RequestInit }

function stubFetch(): { calls: Call[] } {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      calls.push({ url, init })
      const method = init?.method ?? 'GET'

      if (url.includes('/api/notifications/events')) return json(EVENTS)
      if (url.includes('/api/notifications/config')) {
        if (method === 'PUT') return json({ ok: true })
        return json([
          {
            provider: 'web_inbox',
            enabled: true,
            events: ['device_offline', 'device_online', 'sync_error', 'device_enrolled'],
            configured: true,
            config: { sound: 'beep', volume: 0.5, events_set: true }
          },
          { provider: 'telegram', enabled: false, events: [], configured: false, config: {} },
          { provider: 'webhook', enabled: false, events: [], configured: false, config: {} }
        ])
      }
      if (url.includes('/api/notifications/inbox/read')) return new Response(null, { status: 204 })
      if (url.includes('/api/notifications/inbox')) return json(HISTORY)
      return json({})
    })
  )
  return { calls }
}

function renderScreen(props: { onMarkedRead?: (ids: number[], all: boolean) => void } = {}) {
  return render(Notifications, {
    props: {
      devices: [device()],
      onSelect: () => {},
      onMarkedRead: props.onMarkedRead ?? (() => {})
    }
  })
}

describe('Notifications center', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('shows the inbox and filters read notifications', async () => {
    stubFetch()
    renderScreen()

    expect(await screen.findByText('Device went offline')).toBeTruthy()
    expect(await screen.findByText('A sync failed')).toBeTruthy()

    await fireEvent.click(screen.getByLabelText('Unread only'))
    expect(screen.getByText('Device went offline')).toBeTruthy()
    expect(screen.queryByText('A sync failed')).toBeNull()
  })

  it('marks everything read on the server and tells the shell', async () => {
    const { calls } = stubFetch()
    const onMarkedRead = vi.fn()
    renderScreen({ onMarkedRead })

    await screen.findByText('Device went offline')
    await fireEvent.click(screen.getByText('Mark all read'))

    await waitFor(() => {
      const read = calls.find((c) => c.url.includes('/api/notifications/inbox/read') && c.init?.method === 'POST')
      expect(read).toBeTruthy()
      expect(JSON.parse(String(read?.init?.body))).toEqual({ all: true })
    })
    expect(onMarkedRead).toHaveBeenCalledWith([], true)
  })

  it('renders the event catalogue and saves sound, volume and event preferences', async () => {
    const { calls } = stubFetch()
    renderScreen()

    await fireEvent.click(screen.getByRole('button', { name: 'Preferences' }))
    expect((await screen.findAllByText('Device goes offline')).length).toBeGreaterThan(0)

    await fireEvent.click(screen.getByRole('button', { name: 'Pulse' }))
    const volume = screen.getByRole('slider') as HTMLInputElement
    await fireEvent.input(volume, { target: { value: '0.8' } })
    await fireEvent.click(screen.getByText('Save in-app preferences'))

    await waitFor(() => {
      const put = calls.find((c) => c.url.includes('/api/notifications/config') && c.init?.method === 'PUT')
      expect(put).toBeTruthy()
      const body = JSON.parse(String(put?.init?.body))
      expect(body.provider).toBe('web_inbox')
      // The popup list travels with the preferences: the server told the
      // dashboard which events interrupt, and the dashboard writes back what
      // the person chose.
      expect(body.config).toEqual({
        sound: 'pulse',
        volume: 0.8,
        events_set: true,
        popups: ['device_offline', 'sync_error'],
        popups_set: true
      })
    })
    expect(await screen.findByText(/Saved\./)).toBeTruthy()
  })

  it('lets the person choose which events pop up', async () => {
    const { calls } = stubFetch()
    renderScreen()

    await fireEvent.click(screen.getByRole('button', { name: 'Preferences' }))
    await screen.findAllByText('Device goes offline')
    // The same event labels appear in the Telegram and the webhook cards, so
    // the click has to be scoped to the in-app card.
    const inApp = within(screen.getByText('In-app alerts').closest('.card') as HTMLElement)
    // "Device comes back online" is not a popup by default.
    await fireEvent.click(inApp.getByRole('button', { name: 'Device comes back online' }))
    await fireEvent.click(screen.getByText('Save in-app preferences'))

    await waitFor(() => {
      const put = calls.find((c) => c.url.includes('/api/notifications/config') && c.init?.method === 'PUT')
      expect(put).toBeTruthy()
      const body = JSON.parse(String(put?.init?.body))
      expect(body.config.popups).toEqual(['device_offline', 'sync_error', 'device_online'])
      expect(body.config.popups_set).toBe(true)
    })
  })

  it('keeps the inbox visible when the event catalogue cannot be loaded', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/notifications/events')) return json({ error: 'boom' }, 500)
        if (url.includes('/api/notifications/config')) return json([])
        if (url.includes('/api/notifications/inbox')) return json(HISTORY)
        return json({})
      })
    )
    renderScreen()

    expect(await screen.findByText('Device went offline')).toBeTruthy()
  })
})
