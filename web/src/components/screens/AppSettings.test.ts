import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AppSettings from './AppSettings.svelte'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

const ENTRIES = [
  {
    id: 2,
    timestamp: '2026-10-15T12:00:00Z',
    level: 'ERROR',
    category: 'http',
    event: 'http_error',
    message: 'device request failed',
    device_id: 'dev-1',
    status: 500,
    duration_ms: 12
  },
  {
    id: 1,
    timestamp: '2026-10-15T11:59:00Z',
    level: 'INFO',
    category: 'sync',
    event: 'sync_completed',
    message: 'synced 3 files'
  }
]

const SETTINGS = {
  level: 'INFO',
  console_level: 'WARN',
  retention_days: 14,
  levels: ['TRACE', 'DEBUG', 'INFO', 'NOTICE', 'WARN', 'ERROR'],
  categories: ['system', 'http', 'sync']
}

function stubFetch(
  handler: (url: string, init?: RequestInit) => Response | Promise<Response>
): { calls: { url: string; init?: RequestInit }[] } {
  const calls: { url: string; init?: RequestInit }[] = []
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

function route(
  url: string,
  opts: { entries?: unknown[]; total?: number; status?: number; settings?: unknown } = {}
): Response {
  const { entries = ENTRIES, total = ENTRIES.length, status = 200, settings = SETTINGS } = opts
  if (url.includes('/api/log-settings')) return json(settings, status)
  // Before the /api/logs catch-all: the token request is a POST whose body is
  // not a page.
  if (url.includes('/api/logs/stream-token')) return json({ ticket: 'ticket-1' })
  if (url.includes('/api/logs/stats')) {
    return json({ total, by_level: { ERROR: 1, INFO: 1 }, errors_24h: 1 })
  }
  if (url.includes('/api/logs')) return json({ entries, total }, status)
  return json([], status)
}

// jsdom has no EventSource. The fake records the URL the screen opened and lets
// a test push one server event.
class FakeEventSource {
  static instances: FakeEventSource[] = []
  url: string
  closed = false
  onopen: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  close() {
    this.closed = true
  }

  emit(entry: unknown) {
    this.onmessage?.({ data: JSON.stringify(entry) } as MessageEvent)
  }
}

describe('Application settings screen', () => {
  afterEach(() => {
    cleanup()
    FakeEventSource.instances = []
    vi.unstubAllGlobals()
  })

  it('renders stored log entries with their level and event', async () => {
    stubFetch((url) => route(url))
    render(AppSettings)

    expect(await screen.findByText('http_error')).toBeTruthy()
    expect(screen.getByText('device request failed')).toBeTruthy()
    expect(screen.getByText('sync_completed')).toBeTruthy()
  })

  it('summarises the totals from the stats endpoint', async () => {
    stubFetch((url) => route(url))
    render(AppSettings)

    // The by-level chips come straight from /api/logs/stats.
    expect(await screen.findByText('ERROR 1')).toBeTruthy()
    expect(screen.getByText('INFO 1')).toBeTruthy()
  })

  it('sends the level filter to the API when one is picked', async () => {
    const { calls } = stubFetch((url) => route(url))
    render(AppSettings)
    await screen.findByText('http_error')

    const level = await screen.findByLabelText('Level')
    await fireEvent.change(level, { target: { value: 'WARN' } })

    const asked = calls.filter((c) => c.url.includes('/api/logs?'))
    expect(asked.some((c) => c.url.includes('level=WARN'))).toBe(true)
  })

  it('sends a request id to trace one request through the log', async () => {
    const { calls } = stubFetch((url) => route(url))
    render(AppSettings)
    await screen.findByText('http_error')

    const request = await screen.findByLabelText('Request ID')
    await fireEvent.input(request, { target: { value: 'req_abc123' } })

    // The field is debounced, so wait for the follow-up query.
    await vi.waitFor(() => {
      const asked = calls.filter((c) => c.url.includes('/api/logs?'))
      expect(asked.some((c) => c.url.includes('request_id=req_abc123'))).toBe(true)
    })
  })

  it('renders the persisted settings values', async () => {
    stubFetch((url) => route(url))
    render(AppSettings)

    const level = (await screen.findByLabelText('Persistence level')) as HTMLSelectElement
    expect(level.value).toBe('INFO')
    const retention = (await screen.findByLabelText('Retention (days)')) as HTMLInputElement
    expect(retention.value).toBe('14')
  })

  it('saves the settings and confirms', async () => {
    const { calls } = stubFetch((url, init) => {
      if (url.includes('/api/log-settings') && init?.method === 'PUT') {
        return json({ ...SETTINGS, level: 'DEBUG' })
      }
      return route(url)
    })
    render(AppSettings)

    await screen.findByLabelText('Persistence level')
    const level = (await screen.findByLabelText('Persistence level')) as HTMLSelectElement
    await fireEvent.change(level, { target: { value: 'DEBUG' } })
    await fireEvent.click(screen.getByText('Save settings'))

    const put = calls.find((c) => c.init?.method === 'PUT' && c.url.includes('/api/log-settings'))
    expect(put).toBeTruthy()
    expect(await screen.findByText(/Saved\./)).toBeTruthy()
  })

  it('surfaces a failed log request with a retry instead of a blank list', async () => {
    let fail = true
    stubFetch((url) => {
      if (url.includes('/api/logs?')) return fail ? json({ error: 'boom' }, 500) : route(url)
      return route(url)
    })
    render(AppSettings)

    expect(await screen.findByRole('alert')).toBeTruthy()

    fail = false
    await fireEvent.click(screen.getByText('Retry'))
    expect(await screen.findByText('http_error')).toBeTruthy()
  })

  it('streams new entries when live is turned on', async () => {
    vi.stubGlobal('EventSource', FakeEventSource)
    const { calls } = stubFetch((url) => route(url))
    render(AppSettings)
    await screen.findByText('http_error')

    await fireEvent.click(screen.getByRole('button', { name: /Live/ }))

    await vi.waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
    expect(calls.some((c) => c.init?.method === 'POST' && c.url.includes('/api/logs/stream-token'))).toBe(true)
    const stream = FakeEventSource.instances[0]
    expect(stream.url).toContain('ticket=ticket-1')
    expect(stream.url).not.toContain('limit=')

    stream.emit({
      id: 77,
      timestamp: '2026-10-17T12:00:00Z',
      level: 'ERROR',
      category: 'http',
      event: 'streamed_event',
      message: 'pushed live'
    })

    expect(await screen.findByText('streamed_event')).toBeTruthy()
    expect(screen.getByText('pushed live')).toBeTruthy()
  })

  it('reopens the stream with the new filter instead of polling', async () => {
    vi.stubGlobal('EventSource', FakeEventSource)
    stubFetch((url) => route(url))
    render(AppSettings)
    await screen.findByText('http_error')

    await fireEvent.click(screen.getByRole('button', { name: /Live/ }))
    await vi.waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))

    const level = await screen.findByLabelText('Level')
    await fireEvent.change(level, { target: { value: 'WARN' } })

    await vi.waitFor(() => expect(FakeEventSource.instances).toHaveLength(2))
    expect(FakeEventSource.instances[0].closed).toBe(true)
    expect(FakeEventSource.instances[1].url).toContain('level=WARN')
  })

  it('closes the stream when live is turned off', async () => {
    vi.stubGlobal('EventSource', FakeEventSource)
    stubFetch((url) => route(url))
    render(AppSettings)
    await screen.findByText('http_error')

    const live = screen.getByRole('button', { name: /Live/ })
    await fireEvent.click(live)
    await vi.waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))

    await fireEvent.click(live)
    expect(FakeEventSource.instances[0].closed).toBe(true)
  })
})
