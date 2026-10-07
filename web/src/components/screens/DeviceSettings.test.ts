import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import DeviceSettings from './DeviceSettings.svelte'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

const device = {
  id: 'dev-1',
  hostname: 'host-1',
  user_id: 'user-1',
  status: 'online',
  last_seen_at: '2026-10-07T12:00:00Z',
  last_sync_at: '2026-10-07T12:00:00Z',
  app_count: 0,
  preference_count: 0,
  saves_count: 0,
  saves_size_bytes: 0
}

describe('DeviceSettings screen', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('loads persisted settings and saves identity, tags, and collection interval', async () => {
    const calls: { method: string; url: string; body?: any }[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method || 'GET'
      const body = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined
      calls.push({ method, url, body })
      if (method === 'GET') return json({ display_name: 'Desk', tags: ['gaming'], collection_interval_seconds: 10 })
      return json(body)
    }))

    render(DeviceSettings, { props: { device, authHeaders: { Authorization: 'Bearer session' }, onRemove: vi.fn() } })

    const name = await screen.findByLabelText('Display name') as HTMLInputElement
    const tags = screen.getByLabelText('Tags comma separated') as HTMLInputElement
    const interval = screen.getByLabelText('Collection interval') as HTMLSelectElement
    await vi.waitFor(() => expect(name.value).toBe('Desk'))
    expect(name.value).toBe('Desk')
    expect(tags.value).toBe('gaming')
    expect(interval.value).toBe('10')

    await fireEvent.input(name, { target: { value: 'Office PC' } })
    await fireEvent.input(tags, { target: { value: 'work, gaming' } })
    await fireEvent.change(interval, { target: { value: '30' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    expect((await screen.findByRole('status')).textContent).toContain('Saved')
    expect(calls.map((call) => call.method)).toEqual(['GET', 'PATCH'])
    expect(calls[1].url).toBe('/api/devices/dev-1')
    expect(calls[1].body).toEqual({
      display_name: 'Office PC',
      tags: ['work', 'gaming'],
      collection_interval_seconds: 30
    })
  })

  it('shows a server validation error instead of claiming the settings were saved', async () => {
    vi.stubGlobal('fetch', vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'PATCH') return json({ error: 'collection interval must be 5, 10, 30, or 60 seconds' }, 400)
      return json({ display_name: '', tags: [], collection_interval_seconds: 10 })
    }))

    render(DeviceSettings, { props: { device, onRemove: vi.fn() } })
    const name = await screen.findByLabelText('Display name')
    await fireEvent.input(name, { target: { value: 'Changed' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    expect((await screen.findByRole('alert')).textContent).toContain('collection interval must be 5, 10, 30, or 60 seconds')
    expect(screen.queryByText('Saved')).toBeNull()
  })

  it('reloads settings when selection changes to another device', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      return url.includes('dev-2')
        ? json({ display_name: 'Second PC', tags: ['lab'], collection_interval_seconds: 60 })
        : json({ display_name: 'First PC', tags: [], collection_interval_seconds: 10 })
    }))

    const { rerender } = render(DeviceSettings, { props: { device, onRemove: vi.fn() } })
    const name = await screen.findByLabelText('Display name') as HTMLInputElement
    await vi.waitFor(() => expect(name.value).toBe('First PC'))
    await rerender({ device: { ...device, id: 'dev-2', hostname: 'host-2' } })
    await vi.waitFor(() => expect(name.value).toBe('Second PC'))
    expect((screen.getByLabelText('Collection interval') as HTMLSelectElement).value).toBe('60')
  })
})
