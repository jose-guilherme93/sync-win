import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Alerts from './Alerts.svelte'
import type { Device } from '../../lib/types'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

// A device that produces exactly one critical finding: CPU 95%.
function device(overrides: Partial<Device> = {}): Device {
  const now = new Date().toISOString()
  return {
    id: 'dev-1',
    hostname: 'test-host',
    user_id: 'user-1',
    status: 'online',
    last_seen_at: now,
    last_sync_at: now,
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0,
    hardware: {
      cpu_usage_percent: 95,
      memory_used_bytes: 10,
      memory_total_bytes: 100,
      disk_read_rate: 0,
      disk_write_rate: 0,
      uptime_seconds: 0,
      load_average: '0 0 0',
      power_watts: 0,
      collected_at: now
    },
    ...overrides
  }
}

type Call = { url: string; init?: RequestInit }

function stubFetch(
  handler: (url: string, init?: RequestInit) => Response | Promise<Response>
): { calls: Call[] } {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      calls.push({ url, init })
      return handler(url, init)
    })
  )
  return { calls }
}

function route(acks: { id: string; acked_at: string }[] = [], status = 200) {
  return (url: string, init?: RequestInit): Response => {
    const method = init?.method ?? 'GET'
    if (url.includes('/api/alerts') && method === 'GET') return json({ acks }, status)
    if (url.includes('/api/alerts') && method === 'POST') {
      return json({ id: 'dev-1:cpu', acked_at: '2026-10-17T12:00:00Z' })
    }
    if (url.includes('/api/alerts') && method === 'DELETE') return json({ cleared: 1 })
    return json({})
  }
}

function renderAlerts(props: { devices?: Device[] } = {}) {
  return render(Alerts, {
    props: { devices: props.devices ?? [device()], onSelect: () => {} }
  })
}

describe('Alerts screen', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('renders a finding and persists the acknowledgement', async () => {
    const { calls } = stubFetch(route())
    renderAlerts()

    expect(await screen.findByText('CPU 95%')).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Acknowledge' }))

    await waitFor(() => {
      const post = calls.find((c) => c.init?.method === 'POST' && c.url.includes('/api/alerts'))
      expect(post).toBeTruthy()
      expect(JSON.parse(String(post?.init?.body))).toEqual({ id: 'dev-1:cpu' })
    })
    await waitFor(() => expect(screen.queryByText('CPU 95%')).toBeNull())
  })

  it('hides a finding whose acknowledgement came from the server', async () => {
    stubFetch(route([{ id: 'dev-1:cpu', acked_at: '2026-10-17T11:00:00Z' }]))
    renderAlerts()

    await waitFor(() => expect(screen.queryByText('CPU 95%')).toBeNull())
    expect(screen.getByText('Acknowledged')).toBeTruthy()
  })

  it('restores every acknowledgement on the server', async () => {
    const { calls } = stubFetch(route([{ id: 'dev-1:cpu', acked_at: '2026-10-17T11:00:00Z' }]))
    renderAlerts()

    const restore = await screen.findByRole('button', { name: 'Restore' })
    await fireEvent.click(restore)

    await waitFor(() => {
      expect(calls.some((c) => c.init?.method === 'DELETE')).toBe(true)
    })
    expect(await screen.findByText('CPU 95%')).toBeTruthy()
  })

  it('brings the finding back when the acknowledgement cannot be saved', async () => {
    stubFetch((url, init) => {
      if ((init?.method ?? 'GET') === 'POST') return json({ error: 'boom' }, 500)
      return route()(url, init)
    })
    renderAlerts()

    expect(await screen.findByText('CPU 95%')).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Acknowledge' }))

    expect((await screen.findByRole('alert')).textContent).toContain('Could not save')
    expect(await screen.findByText('CPU 95%')).toBeTruthy()
  })

  it('keeps the finding visible when the stored acks cannot be loaded', async () => {
    stubFetch((url, init) => route([], 500)(url, init))
    renderAlerts()

    expect((await screen.findByRole('alert')).textContent).toContain('Could not load')
    expect(screen.getByText('CPU 95%')).toBeTruthy()
  })
})
