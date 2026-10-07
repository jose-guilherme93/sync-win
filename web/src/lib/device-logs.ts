// Shared helpers for the device log viewer.
//
// The viewer is rendered from two places (the device Logs tab and the sidebar
// Logs section), so the formatting and filtering rules live here rather than
// being duplicated in two components.

export type DeviceLogLevel = 'error' | 'warn' | 'info'

export type DeviceLogEntry = {
  id: number
  device_id: string
  ts: string
  level: string
  source: string
  message: string
}

// Mirrors store.DeviceLogPage.
export type DeviceLogPage = {
  entries: DeviceLogEntry[]
  total: number
  counts: Record<string, number>
  sources: string[]
  limit: number
  offset: number
  truncated: boolean
}

export const LOG_LEVELS: DeviceLogLevel[] = ['error', 'warn', 'info']

export type LevelFilter = 'all' | DeviceLogLevel

export type TimeRange = 'all' | '15m' | '1h' | '6h' | '24h'

// Window lengths offered in the range picker, matching what an operator usually
// wants after noticing something on a device.
export const TIME_RANGES: { id: TimeRange; label: string; ms: number }[] = [
  { id: '15m', label: '15m', ms: 15 * 60_000 },
  { id: '1h', label: '1h', ms: 60 * 60_000 },
  { id: '6h', label: '6h', ms: 6 * 60 * 60_000 },
  { id: '24h', label: '24h', ms: 24 * 60 * 60_000 }
]

export const PAGE_SIZE = 200

// A line the agent sent with no parseable source. journalctl writes these for
// entries with no identifiable unit.
export const UNKNOWN_SOURCE = 'system'

// sinceFor turns a range picker value into the RFC3339 lower bound the API
// expects. 'all' yields an empty string, which the server reads as no bound.
export function sinceFor(range: TimeRange, now = Date.now()): string {
  if (range === 'all') return ''
  const entry = TIME_RANGES.find((r) => r.id === range)
  if (!entry) return ''
  return new Date(now - entry.ms).toISOString()
}

// displayTime renders the wall clock of an entry. The server stores RFC3339 UTC,
// so the browser's locale decides the rendering; the date is included only when
// the entry is not from today, which keeps a live list narrow.
export function displayTime(ts: string, now = Date.now()): string {
  const parsed = Date.parse(ts)
  if (!Number.isFinite(parsed)) return ts || '—'
  const d = new Date(parsed)
  const clock = d.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false
  })
  const sameDay = new Date(now).toDateString() === d.toDateString()
  return sameDay ? clock : `${d.toLocaleDateString(undefined, { month: 'short', day: '2-digit' })} ${clock}`
}

// fullTimestamp is the value for the title attribute, where losing the date or
// the zone would make a line impossible to correlate with a local journalctl.
export function fullTimestamp(ts: string): string {
  const parsed = Date.parse(ts)
  if (!Number.isFinite(parsed)) return ts || ''
  return new Date(parsed).toString()
}

// matchesFilter is the client-side predicate used to highlight whether a line
// would survive the active filters. Filtering itself happens server-side; this
// only decides how a row is tinted.
export function matchesFilter(entry: DeviceLogEntry, level: LevelFilter, source: string, search: string): boolean {
  if (level !== 'all' && entry.level !== level) return false
  if (source && entry.source !== source) return false
  const needle = search.trim().toLowerCase()
  if (needle && !entry.message.toLowerCase().includes(needle)) return false
  return true
}

// countByLevel sums a set of entries per level. The API returns authoritative
// counts, but the toolbar needs a local total for the "all" chip after a client
// side highlight pass.
export function countByLevel(entries: DeviceLogEntry[]): Record<DeviceLogLevel, number> {
  const out: Record<DeviceLogLevel, number> = { error: 0, warn: 0, info: 0 }
  for (const e of entries) {
    if (e.level in out) out[e.level as DeviceLogLevel] += 1
  }
  return out
}

// buildQuery renders the viewer filter state as URLSearchParams. Kept here so the
// component and its tests cannot disagree about parameter names.
export function buildQuery(opts: {
  level: LevelFilter
  source: string
  search: string
  since: string
  limit: number
  offset: number
}): URLSearchParams {
  const params = new URLSearchParams()
  if (opts.level !== 'all') params.set('level', opts.level)
  if (opts.source) params.set('source', opts.source)
  if (opts.search.trim()) params.set('search', opts.search.trim())
  if (opts.since) params.set('since', opts.since)
  params.set('limit', String(opts.limit))
  params.set('offset', String(opts.offset))
  return params
}

// toClipboardText renders entries as plain text for the copy action, so the
// clipboard gets something pasteable into a bug report rather than a table.
export function toClipboardText(entries: DeviceLogEntry[]): string {
  return entries
    .map((e) => `${e.ts}  ${e.level.toUpperCase().padEnd(5)}  ${e.source || UNKNOWN_SOURCE}  ${e.message}`)
    .join('\n')
}