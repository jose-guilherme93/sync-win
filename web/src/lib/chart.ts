// Shared time-axis helpers for every telemetry plot.
//
// The charts used a category axis, which gives every sample the same width no
// matter when it was taken. A sparse window therefore stretched two points
// across the whole card, and irregular gaps (a dropped poll, a downsampled
// bucket) looked like real, evenly spaced samples. Chart.js was also left to
// connect the first point with a spline, which overshot the y range and drew a
// peak that never existed.
//
// A linear x axis over the window's real timestamps fixes both: each sample
// sits at its true position, the plot fills from the left as data arrives, and
// a window with only a few samples stays mostly empty instead of being
// stretched.

export type TimeDomain = { from: number; to: number }

const SECOND = 1_000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

// Nice wall-clock steps. Chart.js' default "nice number" ticks land on values
// like 07:05 and 07:13, which read like arbitrary samples. Aligning to these
// steps gives the familiar 18:20, 18:25, 18:30 axis.
const TIME_STEPS = [
  5 * SECOND,
  10 * SECOND,
  15 * SECOND,
  20 * SECOND,
  30 * SECOND,
  MINUTE,
  2 * MINUTE,
  5 * MINUTE,
  10 * MINUTE,
  15 * MINUTE,
  30 * MINUTE,
  HOUR,
  2 * HOUR,
  3 * HOUR,
  6 * HOUR,
  12 * HOUR,
  DAY,
  2 * DAY,
  7 * DAY,
  14 * DAY,
  30 * DAY
]

export function parseTimestamp(value: string): number {
  const ms = Date.parse(value)
  return Number.isFinite(ms) ? ms : NaN
}

// The x range a chart should cover. `windowMs` makes the plot a fixed window
// that scrolls; the newest sample anchors its right edge so the line always
// reaches the live edge, and a device that stops reporting keeps its last
// window instead of sliding into an empty chart.
export function timeDomain(
  labels: readonly string[],
  windowMs: number | null = null,
  windowEnd: number | null = null
): TimeDomain | null {
  let first = NaN
  let last = NaN
  for (const label of labels) {
    const ms = parseTimestamp(label)
    if (!Number.isFinite(ms)) continue
    if (!Number.isFinite(first)) first = ms
    last = ms
  }
  if (!Number.isFinite(last)) return null

  const to = windowEnd != null && Number.isFinite(windowEnd)
    ? Math.max(windowEnd, last)
    : last
  if (windowMs != null && windowMs > 0) return { from: to - windowMs, to }
  return { from: first, to: Math.max(to, first + 1) }
}

// Evenly spaced ticks aligned to a round time step. Capped at `limit` so the
// axis never crowds; the last step list entry covers a one-month window.
export function buildTimeTicks(from: number, to: number, limit = 8): number[] {
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from || limit < 1) return []
  const span = to - from
  const step = TIME_STEPS.find((candidate) => span / candidate <= limit) ?? TIME_STEPS[TIME_STEPS.length - 1]
  const ticks: number[] = []
  for (let tick = Math.ceil(from / step) * step; tick <= to; tick += step) ticks.push(tick)
  return ticks
}

export function formatTimeTick(ms: number, spanMs: number): string {
  const date = new Date(ms)
  if (!Number.isFinite(date.getTime())) return ''
  if (spanMs > 36 * HOUR) {
    return date.toLocaleDateString(undefined, { day: '2-digit', month: 'short' })
  }
  // A one-minute live window has several ticks inside the same minute; without
  // seconds they would all read the same time.
  if (spanMs <= 10 * MINUTE) {
    return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }
  return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

export function formatTooltipTime(ms: number): string {
  const date = new Date(ms)
  if (!Number.isFinite(date.getTime())) return ''
  const day = date.toLocaleDateString(undefined, { month: 'short', day: '2-digit' })
  const time = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  return `${day}, ${time}`
}

// Chart.js and the canvas API do not read CSS custom properties and do not
// accept hsl() strings in gradient colour stops. Series colours are resolved
// from the theme before they get here, so plain #rrggbb is what remains.
export function withAlpha(color: string, alpha: number): string {
  const hex = /^#([0-9a-f]{6})$/i.exec(color.trim())
  if (!hex) return color
  const value = parseInt(hex[1], 16)
  return `rgba(${(value >> 16) & 255}, ${(value >> 8) & 255}, ${value & 255}, ${alpha})`
}
