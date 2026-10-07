import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DeviceLogs from './DeviceLogs.svelte'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

function page(over: Record<string, unknown> = {}) {
  return {
    entries: [],
    total: 0,
    counts: { error: 0, warn: 0, info: 0 },
    sources: [],
    limit: 200,
    offset: 0,
    truncated: false,
    ...over
  }
}

function logLine(over: Record<string, unknown> = {}) {
  return {
    id: 1,
    device_id: 'dev-1',
    ts: new Date().toISOString(),
    level: 'info',
    source: 'systemd',
    message: 'Started unit.',
    ...over
  }
}

const entries = [
  logLine({ id: 1, level: 'error', source: 'kernel', message: 'EXT4-fs error on nvme0n1p2' }),
  logLine({ id: 2, level: 'warn', source: 'dockerd', message: 'iptables missing' }),
  logLine({ id: 3, level: 'info', source: 'systemd', message: 'Started unit.' })
]

describe('DeviceLogs', () => {
  let calls: string[] = []
  let respond: (url: string) => Response = () => json(page())

  beforeEach(() => {
    calls = []
    respond = () => json(page({ entries, total: 3, counts: { error: 1, warn: 1, info: 1 }, sources: ['kernel', 'dockerd', 'systemd'] }))
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        calls.push(url)
        return respond(url)
      })
    )
    // jsdom has neither clipboard nor rAF guarantees the component relies on.
    vi.stubGlobal('navigator', {
      ...globalThis.navigator,
      clipboard: { writeText: vi.fn(async () => {}) }
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders stored entries with their level and source', async () => {
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())
    expect(screen.getByText(/iptables missing/)).toBeTruthy()
    expect(screen.getByText(/Started unit/)).toBeTruthy()
    expect(calls[0]).toContain('/api/devices/dev-1/logs')
  })

  it('shows the stored total in the header', async () => {
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/3 stored entries/)).toBeTruthy())
  })

  it('renders per-level counts on the severity chips', async () => {
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())
    const errorChip = screen.getByRole('button', { name: /^error/ })
    expect(errorChip.textContent).toContain('1')
  })

  it('requests the level filter when a chip is clicked', async () => {
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())
    calls = []

    await fireEvent.click(screen.getByRole('button', { name: /^error/ }))

    await vi.waitFor(() => expect(calls.some((u) => u.includes('level=error'))).toBe(true))
  })

  it('requests a source filter from the dropdown', async () => {
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())
    calls = []

    const select = screen.getByLabelText(/filter by source/i)
    await fireEvent.change(select, { target: { value: 'kernel' } })

    await vi.waitFor(() => expect(calls.some((u) => u.includes('source=kernel'))).toBe(true))
  })

  it('requests a time range when one is picked', async () => {
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())
    calls = []

    await fireEvent.click(screen.getByRole('button', { name: '1h' }))

    await vi.waitFor(() => expect(calls.some((u) => u.includes('since='))).toBe(true))
  })

  it('debounces search into a single request', async () => {
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())
    calls = []

    const box = screen.getByLabelText(/search log messages/i)
    await fireEvent.input(box, { target: { value: 'ext4' } })
    await fireEvent.input(box, { target: { value: 'ext4f' } })
    await fireEvent.input(box, { target: { value: 'ext4fs' } })

    await vi.waitFor(() => expect(calls.some((u) => u.includes('search=ext4fs'))).toBe(true), { timeout: 2000 })
    // Three keystrokes must not become three requests.
    expect(calls.filter((u) => u.includes('search=')).length).toBe(1)
  })

  it('loads an older page on demand', async () => {
    respond = () => json(page({ entries, total: 400, counts: { error: 1, warn: 1, info: 1 }, sources: ['kernel'] }))
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())
    calls = []

    await fireEvent.click(screen.getByRole('button', { name: /Load 200 older/i }))

    await vi.waitFor(() => expect(calls.some((u) => u.includes('offset=3'))).toBe(true))
  })

  it('explains an empty store instead of showing a blank panel', async () => {
    respond = () => json(page())
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/No device logs stored/)).toBeTruthy())
  })

  it('offers to clear filters when a filter excludes everything', async () => {
    // The store has rows, but none survive the active filter. Filtering happens
    // server-side, so the response legitimately comes back empty.
    respond = (url) =>
      url.includes('level=')
        ? json(page({ sources: ['kernel', 'dockerd', 'systemd'] }))
        : json(page({ entries, total: 3, counts: { error: 1, warn: 1, info: 1 }, sources: ['kernel', 'dockerd', 'systemd'] }))

    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: /^error/ }))

    await vi.waitFor(() => expect(screen.getByText(/No matching entries/)).toBeTruthy())
    expect(screen.queryByText(/No device logs stored/)).toBeNull()
  })

  it('surfaces a request failure with a retry', async () => {
    respond = () => json({ error: 'boom' }, 500)
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByRole('alert')).toBeTruthy())
    expect(screen.getAllByRole('button', { name: /retry/i }).length).toBeGreaterThan(0)
  })

  it('recovers when a retry succeeds', async () => {
    respond = () => json({ error: 'boom' }, 500)
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByRole('alert')).toBeTruthy())

    respond = () => json(page({ entries, total: 3, counts: { error: 1, warn: 1, info: 1 }, sources: ['kernel'] }))
    await fireEvent.click(screen.getAllByRole('button', { name: /retry/i })[0])

    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())
  })

  it('copies the loaded lines as text', async () => {
    render(DeviceLogs, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/EXT4-fs error/)).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: /copy/i }))

    const { clipboard } = globalThis.navigator
    await vi.waitFor(() => expect(clipboard.writeText).toHaveBeenCalled())
    const copied = (clipboard.writeText as ReturnType<typeof vi.fn>).mock.calls[0][0] as string
    expect(copied).toContain('EXT4-fs error')
  })

  it('does not fetch for an empty device id', async () => {
    render(DeviceLogs, { props: { deviceId: '' } })
    await new Promise((resolve) => setTimeout(resolve, 60))
    expect(calls.length).toBe(0)
  })
})