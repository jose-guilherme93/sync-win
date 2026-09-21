import { writable, derived, get } from 'svelte/store'

export type ChartPoint = {
  timestamp: string
  cpu: number
  memory: number
  netRx: number
  netTx: number
  temp: number | null
}

const MAX_POINTS = 120

const store = writable<Map<string, ChartPoint[]>>(new Map())

let subscribers = 0
let unsubscribeFn: (() => void) | null = null

function ensureSubscription() {
  if (subscribers > 0) return
  subscribers++
}

export function subscribeToDevice(deviceId: string): ChartPoint[] {
  ensureSubscription()
  const map = get(store)
  return map.get(deviceId) || []
}

export function addTelemetryPoint(deviceId: string, hw: Record<string, any>) {
  const map = get(store)
  const existing = map.get(deviceId) || []

  const memTotal = hw.memory_total_bytes || 1
  const memPct = hw.memory_used_bytes ? (hw.memory_used_bytes / memTotal) * 100 : 0

  let netRx = 0
  let netTx = 0
  if (hw.network_ifaces && Array.isArray(hw.network_ifaces)) {
    for (const iface of hw.network_ifaces) {
      if (iface.name === 'lo') continue
      netRx += iface.rx_rate || 0
      netTx += iface.tx_rate || 0
    }
  }

  const point: ChartPoint = {
    timestamp: new Date().toISOString(),
    cpu: hw.cpu_usage_percent || 0,
    memory: memPct,
    netRx,
    netTx,
    temp: hw.cpu_temperature || null
  }

  const updated = [...existing, point]
  if (updated.length > MAX_POINTS) {
    updated.splice(0, updated.length - MAX_POINTS)
  }

  const newMap = new Map(map)
  newMap.set(deviceId, updated)
  store.set(newMap)
}

export function setHistoryFromAPI(deviceId: string, points: ChartPoint[]) {
  const map = get(store)
  const existing = map.get(deviceId) || []
  if (existing.length > 0) return
  const newMap = new Map(map)
  newMap.set(deviceId, points.slice(-MAX_POINTS))
  store.set(newMap)
}

export function getDeviceHistory(deviceId: string): ChartPoint[] {
  const map = get(store)
  return map.get(deviceId) || []
}

export function deviceHistoryStore(deviceId: string) {
  return derived(store, ($map) => $map.get(deviceId) || [])
}
