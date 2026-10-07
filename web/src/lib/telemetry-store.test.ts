import { get } from 'svelte/store'
import { afterEach, describe, expect, it } from 'vitest'
import { addTelemetryPoint, deviceHistoryStore, resetTelemetry } from './telemetry-store'

describe('telemetry history store', () => {
  afterEach(resetTelemetry)

  it('uses collection time and does not duplicate a sample during faster UI polling', () => {
    const hardware = {
      collected_at: '2026-10-07T12:00:00Z',
      cpu_usage_percent: 42,
      memory_used_bytes: 25,
      memory_total_bytes: 100
    }

    addTelemetryPoint('device-1', hardware)
    addTelemetryPoint('device-1', hardware)

    const points = get(deviceHistoryStore('device-1'))
    expect(points).toHaveLength(1)
    expect(points[0]).toMatchObject({ timestamp: hardware.collected_at, cpu: 42, memory: 25 })
  })
})
