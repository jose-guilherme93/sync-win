import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi } from 'vitest'

// The store is a module-level singleton, so each test re-imports a fresh copy.
describe('telemetry store', () => {
  beforeEach(() => vi.resetModules())

  it('derives memory percentage and coerces missing values', async () => {
    const mod = await import('./telemetry-store')
    mod.addTelemetryPoint('dev-1', {
      cpu_usage_percent: 10,
      memory_used_bytes: 50,
      memory_total_bytes: 200,
      net_rx_rate: 5
    })

    const points = get(mod.deviceHistoryStore('dev-1'))
    expect(points).toHaveLength(1)
    expect(points[0].cpu).toBe(10)
    expect(points[0].memory).toBe(25)
    expect(points[0].netRx).toBe(5)
    expect(points[0].netTx).toBe(0)
    expect(points[0].temp).toBeNull()
  })

  it('caps the per-device buffer at 120 points', async () => {
    const mod = await import('./telemetry-store')
    for (let i = 0; i < 130; i++) {
      mod.addTelemetryPoint('dev-2', { cpu_usage_percent: i })
    }

    const points = get(mod.deviceHistoryStore('dev-2'))
    expect(points).toHaveLength(120)
    expect(points[points.length - 1].cpu).toBe(129)
  })

  it('setHistoryFromAPI preserves live points newer than the API data', async () => {
    const mod = await import('./telemetry-store')
    const apiPoint = { timestamp: '2020-01-01T00:00:01.000Z', cpu: 1, memory: 1, netRx: 0, netTx: 0, temp: null }
    mod.setHistoryFromAPI('dev-3', [apiPoint])
    mod.addTelemetryPoint('dev-3', { cpu_usage_percent: 42 })
    const liveTs = get(mod.deviceHistoryStore('dev-3')).at(-1)?.timestamp

    mod.setHistoryFromAPI('dev-3', [apiPoint])

    const merged = get(mod.deviceHistoryStore('dev-3'))
    expect(merged).toHaveLength(2)
    expect(merged.at(-1)?.cpu).toBe(42)
    expect(merged.at(-1)?.timestamp).toBe(liveTs)
  })

  it('resetTelemetry clears every device so a new session starts empty', async () => {
    const mod = await import('./telemetry-store')
    mod.addTelemetryPoint('dev-a', { cpu_usage_percent: 10 })
    mod.addTelemetryPoint('dev-b', { cpu_usage_percent: 20 })

    mod.resetTelemetry()

    expect(get(mod.deviceHistoryStore('dev-a'))).toEqual([])
    expect(get(mod.deviceHistoryStore('dev-b'))).toEqual([])
  })
})
