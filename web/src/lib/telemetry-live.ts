// Live mode plumbing for the Overview screen.
//
// Live is manual (the topbar range, never the default), draws a one-minute
// trailing window, and is fed by a single-device telemetry poll instead of the
// fleet download. The server lease below is what makes the server tell the
// agent to report faster — the client never negotiates an interval of its own.
//
// Security notes: every payload is parsed defensively (an unexpected shape is
// ignored, not fed into the store), the lease body is a fixed-shape JSON built
// from booleans only, and no response value is ever interpolated into markup.

import { apiFetch, apiURL } from './api'
import type { ChartPoint } from './telemetry-store'

// One poll tick per second. The agent only speeds up because the server says
// so; this cadence exists so the chart fills without waiting for the 10 s
// fleet cycle.
export const LIVE_POLL_MS = 1000
// Hard ceiling: Live ends itself after this long.
export const LIVE_MAX_MS = 15 * 60_000
// Lease renewals ride the poll tick counter: every 45th tick (45 s).
export const LIVE_RENEW_EVERY = 45
// The window the chart draws.
export const LIVE_WINDOW_MS = 60_000
// Where the seed comes from when nothing is in memory yet.
export const LIVE_SEED_WINDOW = '30m'

// POST /api/devices/{id}/live {"active":bool} → {"active":bool,"until":unix}.
// The endpoint may not exist yet (404) or the lease service may be down (503):
// both are non-fatal. Live keeps drawing from the dashboard-side poll and just
// does not accelerate the agent, so no error from here ever reaches the UI.
export async function requestLiveLease(
  deviceId: string,
  headers: Record<string, string>,
  active: boolean
): Promise<void> {
  try {
    await apiFetch(apiURL(`/api/devices/${deviceId}/live`), {
      method: 'POST',
      headers: { ...headers, 'Content-Type': 'application/json' },
      body: JSON.stringify({ active })
    })
  } catch {
    // The lease is an accelerator, not a dependency.
  }
}

// The last `windowMs` of a longer series: what Live seeds its window with when
// the in-memory store has nothing yet, so the chart never opens empty. The
// window is anchored on the newest sample, not on wall-clock now, so an agent
// with a slower cadence still shows its most recent minute.
export function liveTail(points: ChartPoint[], windowMs: number = LIVE_WINDOW_MS): ChartPoint[] {
  let newest: number | null = null
  for (const point of points) {
    const at = Date.parse(point.timestamp)
    if (Number.isFinite(at) && (newest == null || at > newest)) newest = at
  }
  if (newest == null) return []
  return points.filter((point) => {
    const at = Date.parse(point.timestamp)
    return Number.isFinite(at) && at >= newest - windowMs
  })
}
