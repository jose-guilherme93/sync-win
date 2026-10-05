// Shared formatting helpers.
//
// Every timestamp in the API can arrive in three unusable shapes: absent,
// unparsable, or the Go zero time. The last one is the trap: it is a perfectly
// valid RFC3339 string, so a truthiness check passes and the arithmetic happily
// reports an age of ~739893 days. Anything that formats a timestamp must go
// through formatRelative so that shape collapses to the same "never" as the
// others.

// Go marshals an unset time.Time as this. It parses fine, which is exactly why
// it needs an explicit guard rather than relying on Date.parse failing.
const ZERO_TIMES = new Set([
  '0001-01-01T00:00:00Z',
  '0001-01-01 00:00:00 +0000 UTC',
  '0001-01-01T00:00:00.000Z',
  '0001-01-01',
  '',
  '0'
])

export type Timestamp = string | number | null | undefined

// Returns epoch milliseconds, or null when the value carries no usable instant.
export function parseTimestamp(value: Timestamp): number | null {
  if (value == null) return null

  if (typeof value === 'number') {
    if (!Number.isFinite(value) || value <= 0) return null
    // Values small enough to be seconds-since-epoch rather than milliseconds.
    // 1e11 ms is 1973; 1e11 s is the year 5138, so the cutoff is safe.
    return value < 1e11 ? value * 1000 : value
  }

  const raw = value.trim()
  if (!raw || ZERO_TIMES.has(raw)) return null

  const parsed = Date.parse(raw)
  return Number.isFinite(parsed) ? parsed : null
}

// True when the value is absent or the Go zero time.
export function isTimestampMissing(value: Timestamp): boolean {
  return parseTimestamp(value) == null
}

// Human relative age. Missing, unparsable and zero timestamps all render as
// `never` so a device that has never synced never shows a day count.
export function formatRelative(value: Timestamp, nowTs: number = Date.now()): string {
  const t = parseTimestamp(value)
  if (t == null) return 'never'

  const diff = Math.max(0, Math.floor((nowTs - t) / 1000))
  if (diff < 10) return 'just now'
  if (diff < 60) return `${diff}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

// Absolute local time, or an empty string when there is nothing to show. The
// result is meant for a `title` attribute, so it returns '' rather than a
// placeholder.
export function formatAbsolute(value: Timestamp): string {
  const t = parseTimestamp(value)
  if (t == null) return ''
  return new Date(t).toLocaleString()
}

export function formatBytes(value: number | null | undefined, digits = 1): string {
  if (value == null || !Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : digits)} ${units[index]}`
}

export function formatRate(value: number | null | undefined): string {
  return `${formatBytes(value)}/s`
}

export function formatDuration(seconds: number | null | undefined): string {
  const total = Math.max(0, Math.floor(seconds || 0))
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

export function clampPercent(value: number | null | undefined): number {
  if (value == null || !Number.isFinite(value)) return 0
  return Math.max(0, Math.min(value, 100))
}

// Shared threshold vocabulary so a colour always means the same thing wherever
// a meter is drawn.
export type Severity = 'ok' | 'warn' | 'crit'

export function severityFor(percent: number | null | undefined): Severity {
  const p = clampPercent(percent)
  if (p >= 90) return 'crit'
  if (p >= 75) return 'warn'
  return 'ok'
}

// Temperature uses the same three states at hardware-plausible boundaries.
export function tempSeverity(celsius: number | null | undefined): Severity {
  if (celsius == null || !Number.isFinite(celsius)) return 'ok'
  if (celsius >= 85) return 'crit'
  if (celsius >= 70) return 'warn'
  return 'ok'
}