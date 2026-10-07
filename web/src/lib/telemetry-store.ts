import { writable, derived, type Readable } from 'svelte/store'

export type ChartPoint = {
  timestamp: string
  cpu: number
  memory: number
  netRx: number
  netTx: number
  temp: number | null
}

const MAX_POINTS = 120

// Single shared map of device id -> recent telemetry points. Components
// subscribe to `deviceHistoryStore(id)` instead of polling `get()` inside
// reactive blocks, so updates propagate without recreating charts.
const store = writable<Map<string, ChartPoint[]>>(new Map())

function pointFromHardware(hw: Record<string, any>, timestamp: string): ChartPoint {
  const memTotal = hw.memory_total_bytes || 1
  const memPct = hw.memory_used_bytes ? (hw.memory_used_bytes / memTotal) * 100 : 0

  return {
    timestamp,
    cpu: hw.cpu_usage_percent || 0,
    memory: memPct,
    // The agent totals the real interfaces. Summing network_ifaces here would
    // reintroduce the container double counting the agent already filters out.
    netRx: hw.net_rx_rate || 0,
    netTx: hw.net_tx_rate || 0,
    temp: hw.cpu_temperature || null
  }
}

export function addTelemetryPoint(deviceId: string, hw: Record<string, any>) {
  store.update((map) => {
    const existing = map.get(deviceId) || []
    const timestamp = hw.collected_at || new Date().toISOString()
    if (existing.at(-1)?.timestamp === timestamp) return map
    const updated = [...existing, pointFromHardware(hw, timestamp)]
    if (updated.length > MAX_POINTS) {
      updated.splice(0, updated.length - MAX_POINTS)
    }
    const next = new Map(map)
    next.set(deviceId, updated)
    return next
  })
}

// setHistoryFromAPI seeds the store from the server history. Live points that
// arrived while the request was in flight are preserved and appended after the
// API points, so the chart never loses data or ignores the fetched history.
export function setHistoryFromAPI(deviceId: string, points: ChartPoint[]) {
  store.update((map) => {
    const existing = map.get(deviceId) || []
    const lastApiTs = points.length > 0 ? new Date(points[points.length - 1].timestamp).getTime() : 0
    const liveNewer = existing.filter((p) => new Date(p.timestamp).getTime() > lastApiTs)
    const merged = [...points, ...liveNewer].slice(-MAX_POINTS)
    const next = new Map(map)
    next.set(deviceId, merged)
    return next
  })
}

// resetTelemetry clears all live telemetry. Used on sign-out so one account's
// charts never bleed into the next session on the same browser.
export function resetTelemetry() {
  store.set(new Map())
}

export function deviceHistoryStore(deviceId: string): Readable<ChartPoint[]> {
  return derived(store, ($map) => $map.get(deviceId) || [])
}
