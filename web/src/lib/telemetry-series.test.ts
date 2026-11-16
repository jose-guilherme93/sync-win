import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  fetchTelemetrySeries,
  mergeDelta,
  parseSeriesPayload,
  parseSeriesPoint,
  recentWindowCount,
  toMs
} from './telemetry-series'
import type { ChartPoint } from './telemetry-store'

const point = (t: number, extra: Record<string, unknown> = {}) => ({ t, cpu: 10, mem: 40, rx: 100, tx: 50, temp: 52, disk: 55, ...extra })

describe('parseSeriesPoint', () => {
  it('maps the contract shape onto chart points, unix seconds included', () => {
    const parsed = parseSeriesPoint(point(1_769_000_000))
    expect(parsed?.timestamp).toBe(new Date(1_769_000_000_000).toISOString())
    expect(parsed?.cpu).toBe(10)
    expect(parsed?.memory).toBe(40)
    expect(parsed?.netRx).toBe(100)
    expect(parsed?.netTx).toBe(50)
    expect(parsed?.temp).toBe(52)
    expect(parsed?.disk).toBe(55)
  })

  it('keeps an absent field as a gap, not as a zero', () => {
    const parsed = parseSeriesPoint({ t: 1_769_000_000, cpu: 12 })
    expect(parsed?.cpu).toBe(12)
    expect(parsed?.memory).toBeNull()
    expect(parsed?.temp).toBeNull()
    expect(parsed?.netRx).toBeNull()
    // A temperature of 0 is the agent's "no sensor", not a reading.
    expect(parseSeriesPoint({ t: 1_769_000_000, temp: 0 })?.temp).toBeNull()
  })

  it('rejects a point without a usable timestamp', () => {
    expect(parseSeriesPoint({ cpu: 10 })).toBeNull()
    expect(parseSeriesPoint(null)).toBeNull()
  })

  it('toMs accepts seconds and milliseconds', () => {
    expect(toMs(1_769_000_000)).toBe(1_769_000_000_000)
    expect(toMs(1_769_000_000_000)).toBe(1_769_000_000_000)
    expect(toMs(null)).toBeNull()
  })
})

describe('parseSeriesPayload', () => {
  it('reads points and the answered window', () => {
    const payload = { window: '1h', resolution: 'raw', from: 1_769_000_000, to: 1_769_003_600, points: [point(1_769_000_000), { t: 1_769_000_060 }] }
    const parsed = parseSeriesPayload(payload)
    expect(parsed.points).toHaveLength(2)
    expect(parsed.resolution).toBe('raw')
    expect(parsed.window).toEqual({ from: 1_769_000_000_000, to: 1_769_003_600_000 })
  })

  it('tolerates a malformed body', () => {
    expect(parseSeriesPayload(null).points).toEqual([])
    expect(parseSeriesPayload({}).window).toBeNull()
  })
})

describe('mergeDelta', () => {
  const at = (t: number): ChartPoint => ({ timestamp: new Date(t * 1000).toISOString(), cpu: t, memory: 1, netRx: 0, netTx: 0, temp: null })

  it('appends only the tail, keeping every plotted timestamp once', () => {
    const existing = [at(100), at(140), at(180)]
    // The refetch overlaps the window: two points already on screen plus one new.
    const incoming = [at(140), at(180), at(220)]
    const merged = mergeDelta(existing, incoming)
    expect(merged.map((p) => p.timestamp)).toEqual([
      at(100).timestamp,
      at(140).timestamp,
      at(180).timestamp,
      at(220).timestamp
    ])
  })

  it('returns the same array when the delta brings nothing new', () => {
    const existing = [at(100)]
    expect(mergeDelta(existing, [at(100)])).toBe(existing)
    expect(mergeDelta(existing, [])).toBe(existing)
  })
})

describe('fetchTelemetrySeries', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('requests the series endpoint with the window and honours since', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      new Response(JSON.stringify({ window: '1h', resolution: 'raw', from: 1000, to: 4600, points: [point(4600)] }), { status: 200 })
    )
    vi.stubGlobal('fetch', fetchMock)

    // `since` is milliseconds, like every ChartPoint timestamp; the endpoint
    // takes unix seconds.
    const result = await fetchTelemetrySeries('dev-1', {}, { window: '1h', windowMs: 3_600_000, since: 4_000_000 })

    const url = String(fetchMock.mock.calls[0][0])
    expect(url).toContain('/api/devices/dev-1/telemetry/series')
    expect(url).toContain('window=1h')
    expect(url).toContain('since=4000')
    expect(result.source).toBe('series')
    expect(result.points).toHaveLength(1)
    expect(result.window).toEqual({ from: 1000_000, to: 4600_000 })
  })

  it('falls back to history-v2 behind the same function when the endpoint is missing', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).includes('/telemetry/series')) {
        return new Response('not found', { status: 404 })
      }
      return new Response(
        JSON.stringify({
          points: [
            { timestamp: '2026-10-21T12:00:00Z', cpu_avg: 10, mem_avg: 40, temp_avg: 0, net_rx_avg: 100, net_tx_avg: 50 }
          ]
        }),
        { status: 200 }
      )
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await fetchTelemetrySeries('dev-1', {}, { window: '1h', windowMs: 3_600_000 })

    expect(result.source).toBe('history-v2')
    expect(result.points).toHaveLength(1)
    expect(result.points[0].cpu).toBe(10)
    expect(result.window).not.toBeNull()
  })

  it('throws with the primary endpoint status when both paths fail', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('nope', { status: 503 }))
    )
    await expect(fetchTelemetrySeries('dev-1', {}, { window: '1h', windowMs: 3_600_000 })).rejects.toThrow(/\(503\)/)
  })
})

describe('recentWindowCount', () => {
  it('is a quarter of the window with a floor, and never more than the total', () => {
    expect(recentWindowCount(900)).toBe(225)
    expect(recentWindowCount(40)).toBe(16)
    expect(recentWindowCount(8)).toBe(8)
    expect(recentWindowCount(0)).toBe(0)
  })
})
