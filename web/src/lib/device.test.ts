import { describe, expect, it } from 'vitest'
import { mergeDeviceDetail } from './device'
import type { Device, HardwareStats } from './types'

function hw(overrides: Partial<HardwareStats> = {}): HardwareStats {
  return {
    cpu_usage_percent: 0,
    memory_used_bytes: 0,
    memory_total_bytes: 100,
    disk_read_rate: 0,
    disk_write_rate: 0,
    uptime_seconds: 0,
    load_average: '0 0 0',
    power_watts: 0,
    collected_at: '2026-10-18T00:00:00Z',
    ...overrides
  }
}

function device(overrides: Partial<Device> = {}): Device {
  return {
    id: 'dev-1',
    hostname: 'host',
    user_id: 'user-1',
    status: 'online',
    last_seen_at: '2026-10-18T00:00:00Z',
    last_sync_at: '2026-10-18T00:00:00Z',
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0,
    ...overrides
  }
}

describe('mergeDeviceDetail', () => {
  it('returns whichever side exists', () => {
    expect(mergeDeviceDetail(undefined, undefined)).toBeNull()
    const detail = device({ hostname: 'detail' })
    expect(mergeDeviceDetail(detail, undefined)).toBe(detail)
    const live = device({ hostname: 'live' })
    expect(mergeDeviceDetail(undefined, live)).toBe(live)
  })

  it('takes the fresh lifecycle and scalar telemetry from the list projection', () => {
    const detail = device({
      status: 'online',
      last_seen_at: '2026-10-18T00:00:00Z',
      hardware: hw({ cpu_usage_percent: 5, cpu_temperature: 40 })
    })
    const live = device({
      status: 'offline',
      last_seen_at: '2026-10-18T00:10:00Z',
      hardware: hw({ cpu_usage_percent: 80, cpu_temperature: 70 })
    })

    const merged = mergeDeviceDetail(detail, live)!
    expect(merged.status).toBe('offline')
    expect(merged.last_seen_at).toBe('2026-10-18T00:10:00Z')
    expect(merged.hardware?.cpu_usage_percent).toBe(80)
    expect(merged.hardware?.cpu_temperature).toBe(70)
  })

  it('keeps detail-only fields that the list projection does not carry', () => {
    const detail = {
      ...device({
        hardware: hw({
          top_cpu_processes: [{ pid: 1, name: 'one', cpu_percent: 10, mem_rss_bytes: 1 }]
        })
      }),
      securityPosture: { failedServices: ['demo.service'] }
    }
    const live = device({ hardware: hw({ cpu_usage_percent: 30 }) })

    const merged = mergeDeviceDetail(detail as Device, live)! as Device & {
      securityPosture?: { failedServices: string[] }
    }
    expect(merged.hardware?.top_cpu_processes?.[0].name).toBe('one')
    expect(merged.hardware?.cpu_usage_percent).toBe(30)
    expect(merged.securityPosture?.failedServices).toEqual(['demo.service'])
  })
})
