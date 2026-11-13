// Client for GET /api/devices/{id}/telemetry/series — the compact windowed
// series endpoint that replaces the fat history-v2 payload for chart screens.
//
// Contract: { window, resolution, from, to, points: [{ t, cpu, mem, disk, rx,
// tx, temp }] } with at most 900 points, `since` returning only t > since and
// an absent field arriving as null. Until the server ships it, the fetch falls
// back to history-v2 behind this single function so the screens keep working;
// the series path is the main path and history-v2 is the bridge.
//
// The 30 s refetch asks only for the delta (`since` = newest point already
// plotted): the window is never re-downloaded. mergeDelta is what keeps the
// fallback honest — history-v2 cannot honour `since`, so its refetches still
// return the whole window and the merge discards what is already on screen.

import type { ChartPoint } from './telemetry-store'
import { apiFetch, apiURL } from './api'
import { normalizeHistory } from './telemetry-history'

export type SeriesQuery = {
  /** Window id as the endpoint spells it: 30m, 1h, 12h, 24h, 7d, 30d. */
  window: string
  /** Window duration for the history-v2 fallback, which needs from/to dates. */
  windowMs: number
  /**
   * Newest point already plotted, in **milliseconds** (what Date.parse and
   * every ChartPoint timestamp use). The endpoint speaks unix seconds, and the
   * conversion happens here: sending ms made the server compare 1.79e12 against
   * second-resolution rows and answer empty, so every delta silently did
   * nothing.
   */
  since?: number
}

export type SeriesWindow = { from: number; to: number }

export type SeriesResult = {
  points: ChartPoint[]
  window: SeriesWindow | null
  resolution: string | null
  source: 'series' | 'history-v2'
}

type RawPoint = {
  t?: number | null
  cpu?: number | null
  mem?: number | null
  disk?: number | null
  rx?: number | null
  tx?: number | null
  temp?: number | null
}

// Unix seconds or milliseconds → milliseconds. Same 1e11 cutoff as format.ts:
// 1e11 ms is 1973, 1e11 s is the year 5138, so the guess is safe.
export function toMs(value: number | null | undefined): number | null {
  if (value == null || !Number.isFinite(value) || value <= 0) return null
  return value < 1e11 ? value * 1000 : value
}

function finite(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

// One contract point in chart shape. Absent fields stay null so pointValues
// renders them as gaps instead of inventing a zero — a missing temperature is
// not 0 °C, and a missing disk reading is not an empty disk.
export function parseSeriesPoint(raw: RawPoint | null | undefined): ChartPoint | null {
  if (!raw) return null
  const ms = toMs(raw.t)
  if (ms == null) return null
  const temp = finite(raw.temp)
  return {
    timestamp: new Date(ms).toISOString(),
    cpu: finite(raw.cpu),
    memory: finite(raw.mem),
    netRx: finite(raw.rx),
    netTx: finite(raw.tx),
    // The agent reports 0 when no thermal zone exists; plotting it would draw
    // a fake 0 °C line, so a non-positive reading is the sensor being absent.
    temp: temp != null && temp > 0 ? temp : null,
    disk: finite(raw.disk) ?? undefined
  } as ChartPoint
}

export function parseSeriesPayload(payload: any): {
  points: ChartPoint[]
  window: SeriesWindow | null
  resolution: string | null
} {
  if (!payload || !Array.isArray(payload.points)) return { points: [], window: null, resolution: null }
  const rawPoints: RawPoint[] = Array.isArray(payload.points) ? payload.points : []
  const points = rawPoints.map(parseSeriesPoint).filter((point): point is ChartPoint => point != null)
  const from = toMs(payload.from)
  const to = toMs(payload.to)
  const window = from != null && to != null ? { from, to } : null
  return {
    points,
    window,
    resolution: typeof payload.resolution === 'string' ? payload.resolution : null
  }
}

// The axis needs a window even when the server omits from/to: anchor on the
// newest sample and stretch back one window.
function deriveWindow(points: ChartPoint[], windowMs: number): SeriesWindow | null {
  const newest = newestTimestampMs(points)
  if (newest == null) return null
  return { from: newest - windowMs, to: newest }
}

export function newestTimestampMs(points: ChartPoint[]): number | null {
  for (let index = points.length - 1; index >= 0; index--) {
    const at = Date.parse(points[index].timestamp)
    if (Number.isFinite(at)) return at
  }
  return null
}

// Append a delta response to the points already plotted. Points that are on
// screen are never duplicated: identity is the timestamp, so a refetch that
// overlaps the window (or a fallback that cannot honour `since`) simply keeps
// what is already plotted and appends only the new tail.
export function mergeDelta(existing: ChartPoint[], incoming: ChartPoint[]): ChartPoint[] {
  if (incoming.length === 0) return existing
  const seen = new Set(existing.map((point) => point.timestamp))
  const additions = incoming.filter((point) => !seen.has(point.timestamp))
  if (additions.length === 0) return existing
  return [...existing, ...additions].sort((a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp))
}

export async function fetchTelemetrySeries(
  deviceId: string,
  headers: Record<string, string>,
  query: SeriesQuery
): Promise<SeriesResult> {
  const params = new URLSearchParams({ window: query.window })
  if (query.since != null) params.set('since', String(Math.floor(query.since / 1000)))
  const response = await apiFetch(apiURL(`/api/devices/${deviceId}/telemetry/series?${params.toString()}`), {
    headers
  })
  if (response.ok) {
    const parsed = parseSeriesPayload(await response.json())
    const window = parsed.window ?? deriveWindow(parsed.points, query.windowMs)
    if (parsed.points.length > 0 || window != null) {
      return { points: parsed.points, window, resolution: parsed.resolution, source: 'series' }
    }
    // An unparseable 200 body is the endpoint not being there yet; fall through
    // to history-v2 rather than showing an empty screen on a launch day.
  }

  // history-v2 bridge. It needs dates, not a window id, and cannot honour
  // `since` — the caller's mergeDelta absorbs the overlap.
  const to = new Date()
  const from = new Date(to.getTime() - query.windowMs)
  const fallback = await apiFetch(
    apiURL(
      `/api/devices/${deviceId}/telemetry/history-v2?` +
        new URLSearchParams({ from: from.toISOString(), to: to.toISOString(), resolution: 'auto' }).toString()
    ),
    { headers }
  )
  if (!fallback.ok) {
    throw new Error(`telemetry series request failed (${response.status})`)
  }
  return {
    points: normalizeHistory(await fallback.json()),
    window: { from: from.getTime(), to: to.getTime() },
    resolution: null,
    source: 'history-v2'
  }
}

// Right-to-left fill. The first paint shows the newest slice of the window;
// the remaining history enters in batches, growing leftward, without any
// refetch of what is already drawn. Reduced motion skips the batching and
// draws the whole window at once.
export const REVEAL_RECENT_FRACTION = 0.25
export const REVEAL_RECENT_MIN = 16

export function recentWindowCount(total: number): number {
  if (total <= 0) return 0
  return Math.min(total, Math.max(REVEAL_RECENT_MIN, Math.round(total * REVEAL_RECENT_FRACTION)))
}

export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(prefers-reduced-motion: reduce)').matches
    : false
}
