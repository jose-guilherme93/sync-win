import { writable } from 'svelte/store'

// Time windows for the telemetry charts, in the shape Beszel uses: a realtime
// minute plus progressively wider history. This is a *window*, not a refresh
// rate — the old 1s/5s/10s/30s/1m buttons conflated the two, so a reader could
// not tell what range they were looking at.
//
// Each window carries the poll cadence that suits it: the live minute is
// refreshed every 5s, while a 30-day chart is refetched every 5 minutes rather
// than hammering the server for data that barely changes.
//
// The server picks the resolution from the window automatically
// (ResolutionForInterval: <=2h raw, <=8h 1m, <=3d 5m, else 1h), so these
// windows line up with what retention actually keeps.
export type RangeId = 'live' | '30m' | '1h' | '12h' | '24h' | '7d' | '30d'

export type TimeRange = {
  id: RangeId
  label: string
  ms: number
  refreshMs: number
}

export const RANGES: TimeRange[] = [
  { id: 'live', label: 'Live', ms: 60_000, refreshMs: 5_000 },
  { id: '30m', label: '30m', ms: 30 * 60_000, refreshMs: 10_000 },
  { id: '1h', label: '1h', ms: 60 * 60_000, refreshMs: 30_000 },
  { id: '12h', label: '12h', ms: 12 * 60 * 60_000, refreshMs: 60_000 },
  { id: '24h', label: '24h', ms: 24 * 60 * 60_000, refreshMs: 60_000 },
  { id: '7d', label: '7d', ms: 7 * 24 * 60 * 60_000, refreshMs: 300_000 },
  { id: '30d', label: '30d', ms: 30 * 24 * 60 * 60_000, refreshMs: 300_000 }
]

export const DEFAULT_RANGE: RangeId = '1h'

export const selectedRange = writable<RangeId>(DEFAULT_RANGE)

export function rangeById(id: RangeId): TimeRange {
  return RANGES.find((r) => r.id === id) ?? RANGES[2]
}
