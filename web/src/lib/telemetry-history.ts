import type { ChartPoint } from './telemetry-store'

// One telemetry sample in chart shape. Both history-v2 shapes land here:
// downsampled aggregates (cpu_avg, mem_avg, …) and raw payloads (the full
// hardware object). Keeping the mapping out of the screen is what lets the
// chart stats and the tests share one source of truth.
export function normalizeHistoryPoint(raw: any): ChartPoint | null {
  if (!raw) return null
  if (raw.cpu_avg != null || raw.mem_avg != null) {
    const temp = raw.temp_avg ?? raw.temp_max
    return {
      timestamp: raw.timestamp || '',
      cpu: raw.cpu_avg ?? 0,
      memory: raw.mem_avg ?? 0,
      netRx: raw.net_rx_avg ?? 0,
      netTx: raw.net_tx_avg ?? 0,
      // The agent reports 0 when no thermal zone exists; plotting it would draw
      // a fake 0°C line and fill the card with a meaningless average.
      temp: temp == null || temp <= 0 ? null : temp,
      power: raw.power_avg ?? undefined
    }
  }

  let hw = raw.payload ?? raw
  if (typeof hw === 'string') {
    try {
      hw = JSON.parse(hw)
    } catch {
      return null
    }
  }
  if (!hw || typeof hw !== 'object') return null

  const total = hw.memory_total_bytes || 0
  const parts = (hw.disk_partitions ?? []).filter((part: any) => (part.total_bytes || 0) > 0)
  const root = parts.find((part: any) => part.mount === '/')
  const disk = root ?? parts.reduce((largest: any, part: any) =>
    !largest || part.total_bytes > largest.total_bytes ? part : largest, null)

  return {
    timestamp: raw.timestamp || raw.received_at || '',
    cpu: hw.cpu_usage_percent || 0,
    memory: total ? ((hw.memory_used_bytes || 0) / total) * 100 : 0,
    netRx: hw.net_rx_rate || 0,
    netTx: hw.net_tx_rate || 0,
    temp: hw.cpu_temperature > 0 ? hw.cpu_temperature : null,
    disk: disk ? disk.used_percent : undefined,
    diskRead: hw.disk_read_rate,
    diskWrite: hw.disk_write_rate,
    power: hw.power_watts
  }
}

export function normalizeHistory(payload: any): ChartPoint[] {
  if (!Array.isArray(payload?.points)) return []
  return payload.points
    .map(normalizeHistoryPoint)
    .filter((point: ChartPoint | null): point is ChartPoint => point != null)
}

// values aligned with the timestamps of `points`; gaps stay null so a missing
// temperature sample does not shift the line against the X axis.
export function pointValues(points: ChartPoint[], key: keyof ChartPoint): (number | null)[] {
  return points.map((point) => {
    const value = point[key]
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  })
}

// Compact list for sparklines and statistics, with gaps and non-finite values
// removed.
export function seriesValues(points: ChartPoint[], key: keyof ChartPoint): number[] {
  return pointValues(points, key).filter((value): value is number => value != null)
}

export type SeriesStats = { last: number; avg: number; min: number; max: number }

// Statistics of the values that are actually plotted, so the legend can never
// disagree with the line.
export function seriesStats(values: readonly number[]): SeriesStats | null {
  const real = values.filter((value) => Number.isFinite(value))
  if (real.length === 0) return null
  const sum = real.reduce((total, value) => total + value, 0)
  return {
    last: real[real.length - 1],
    avg: sum / real.length,
    min: Math.min(...real),
    max: Math.max(...real)
  }
}
