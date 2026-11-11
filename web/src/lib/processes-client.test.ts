import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  fetchProcessHistory,
  fetchProcessSnapshot,
  ioniceProcess,
  reniceProcess,
  signalProcess
} from './processes-client'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
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

const SNAPSHOT = {
  processes: [
    {
      pid: 2401,
      ppid: 1980,
      name: 'postgres',
      cmd: 'postgres: autovacuum worker',
      user: 'postgres',
      state: 'R',
      threads: 1,
      rss_bytes: 1024,
      cpu_percent: 41.2,
      start_time_unix: 1_700_000_000,
      io_read_bps: 1024,
      io_write_bps: 512,
      ports: [],
      kernel: false
    }
  ],
  stats: { total: 187, threads: 642, zombie: 1, load1: 6.41 },
  collected_at: '2026-11-11T03:04:00Z',
  io_available: false,
  signals_enabled: true
}

describe('fetchProcessSnapshot', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('reads the device route and normalises the payload', async () => {
    const { calls } = stubFetch(() => json(SNAPSHOT))
    const snap = await fetchProcessSnapshot('dev-1')
    expect(calls[0].url).toBe('/api/devices/dev-1/processes')
    expect(snap.processes[0].pid).toBe(2401)
    expect(snap.io_available).toBe(false)
    expect(snap.collected).toBe(true)
  })

  it('surfaces the server reason instead of a bare status', async () => {
    stubFetch(() => json({ error: 'device is offline' }, 503))
    await expect(fetchProcessSnapshot('dev-1')).rejects.toThrow('device is offline')
  })

  it('reports a status line when the body carries no reason', async () => {
    stubFetch(() => new Response('bad gateway', { status: 502 }))
    await expect(fetchProcessSnapshot('dev-1')).rejects.toThrow('request failed (502)')
  })

  it('rejects a payload of the wrong shape rather than drawing it', async () => {
    stubFetch(() => json({ processes: 'nope' }))
    await expect(fetchProcessSnapshot('dev-1')).rejects.toThrow('processes')
  })
})

describe('fetchProcessHistory', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends the range the endpoint documents', async () => {
    const { calls } = stubFetch(() => json({ series: [], events: [], buckets: [] }))
    await fetchProcessHistory('dev-1', '7d')
    expect(calls[0].url).toBe('/api/devices/dev-1/processes/history?range=7d')
  })

  it('refuses a range the endpoint does not accept', async () => {
    const { calls } = stubFetch(() => json({ series: [], events: [], buckets: [] }))
    await expect(fetchProcessHistory('dev-1', '30d' as never)).rejects.toThrow('range')
    expect(calls).toHaveLength(0)
  })
})

describe('signalProcess', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('posts the signal with the start time the snapshot showed', async () => {
    const { calls } = stubFetch(() => json({}, 200))
    await signalProcess('dev-1', 2401, 'TERM', 1_700_000_000)
    expect(calls[0].url).toBe('/api/devices/dev-1/processes/2401/signal')
    expect(calls[0].init?.method).toBe('POST')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({
      signal: 'TERM',
      start_time_unix: 1_700_000_000
    })
  })

  it('keeps the reason a closed gate or policy answers with', async () => {
    stubFetch(() => json({ error: 'mutations disabled' }, 409))
    await expect(signalProcess('dev-1', 2401, 'TERM', 1_700_000_000)).rejects.toThrow('mutations disabled')
  })

  it('keeps the pid-reuse rejection', async () => {
    stubFetch(() => json({ error: 'pid reused' }, 409))
    await expect(signalProcess('dev-1', 2401, 'KILL', 1_700_000_000)).rejects.toThrow('pid reused')
  })

  it('refuses a signal outside the agent allowlist before any request', async () => {
    const { calls } = stubFetch(() => json({}))
    await expect(signalProcess('dev-1', 2401, 'HUP' as never, 1_700_000_000)).rejects.toThrow('signal')
    expect(calls).toHaveLength(0)
  })

  it('refuses pid 1, a non-integer pid, or a missing start time locally', async () => {
    const { calls } = stubFetch(() => json({}))
    await expect(signalProcess('dev-1', 1, 'TERM', 1_700_000_000)).rejects.toThrow('pid')
    await expect(signalProcess('dev-1', 1.5, 'TERM', 1_700_000_000)).rejects.toThrow('pid')
    await expect(signalProcess('dev-1', 2401, 'TERM', 0)).rejects.toThrow('start time')
    expect(calls).toHaveLength(0)
  })

  it('accepts a queued answer as done', async () => {
    stubFetch(() => new Response('', { status: 202 }))
    await expect(signalProcess('dev-1', 2401, 'STOP', 1_700_000_000)).resolves.toBeUndefined()
  })
})

describe('reniceProcess', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('posts a value inside the kernel range', async () => {
    const { calls } = stubFetch(() => json({}))
    await reniceProcess('dev-1', 2401, -5)
    expect(calls[0].url).toBe('/api/devices/dev-1/processes/2401/nice')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ nice: -5 })
  })

  it('rejects a value the kernel would refuse', async () => {
    const { calls } = stubFetch(() => json({}))
    await expect(reniceProcess('dev-1', 2401, -100)).rejects.toThrow('nice')
    await expect(reniceProcess('dev-1', 2401, 20)).rejects.toThrow('nice')
    await expect(reniceProcess('dev-1', 2401, 2.5)).rejects.toThrow('nice')
    expect(calls).toHaveLength(0)
  })
})

describe('ioniceProcess', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('posts a class and level inside the kernel range', async () => {
    const { calls } = stubFetch(() => json({}))
    await ioniceProcess('dev-1', 2401, 2, 4)
    expect(calls[0].url).toBe('/api/devices/dev-1/processes/2401/ionice')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ class: 2, level: 4 })
  })

  it('rejects a class or level the kernel would refuse', async () => {
    const { calls } = stubFetch(() => json({}))
    await expect(ioniceProcess('dev-1', 2401, 99, 0)).rejects.toThrow('ionice')
    await expect(ioniceProcess('dev-1', 2401, 2, 8)).rejects.toThrow('ionice')
    expect(calls).toHaveLength(0)
  })
})
