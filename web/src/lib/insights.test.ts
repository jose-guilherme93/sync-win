import { describe, expect, it } from 'vitest'
import { generateInsights, THRESHOLDS } from './insights'
import type { HardwareStats } from './types'

// Only the fields the insight logic reads matter here.
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
    collected_at: '2024-06-15T12:00:00Z',
    ...overrides
  }
}

describe('generateInsights', () => {
  it('reports nothing for a missing payload', () => {
    expect(generateInsights(undefined)).toEqual([])
  })

  it('reports nothing for a healthy device', () => {
    expect(generateInsights(hw({ cpu_usage_percent: 10, cpu_temperature: 40 }))).toEqual([])
  })

  it('escalates CPU from warn to crit at the documented thresholds', () => {
    expect(generateInsights(hw({ cpu_usage_percent: THRESHOLDS.cpuWarn + 1 }))[0]).toEqual({
      text: expect.stringContaining('CPU'),
      type: 'warn'
    })
    expect(generateInsights(hw({ cpu_usage_percent: THRESHOLDS.cpuCrit + 1 }))[0].type).toBe('crit')
  })

  it('uses the same boundary for memory as it does for CPU', () => {
    // 80% of 100 bytes is 80, which is above ramWarn (75) and below ramCrit (90).
    const warn = generateInsights(hw({ memory_used_bytes: 80, memory_total_bytes: 100 }))
    expect(warn.some((i) => i.text.startsWith('RAM') && i.type === 'warn')).toBe(true)

    const crit = generateInsights(hw({ memory_used_bytes: 95, memory_total_bytes: 100 }))
    expect(crit.some((i) => i.text.startsWith('RAM') && i.type === 'crit')).toBe(true)
  })

  it('flags a hot CPU and ignores a missing sensor', () => {
    expect(generateInsights(hw({ cpu_temperature: 90 })).some((i) => i.text.includes('Temp') && i.type === 'crit')).toBe(true)
    // No sensor must not read as a hot sensor.
    expect(generateInsights(hw({})).some((i) => i.text.includes('Temp'))).toBe(false)
  })

  it('flags a full disk and names the mount', () => {
    const insights = generateInsights(hw({
      disk_partitions: [
        { mount: '/', device: '/dev/sda1', total_bytes: 100, used_bytes: 95, free_bytes: 5, used_percent: 95 }
      ]
    }))
    const disk = insights.find((i) => i.text.startsWith('/'))
    expect(disk?.type).toBe('crit')
    expect(disk?.text).toContain('/')
  })

  it('does not flag a comfortable disk', () => {
    const insights = generateInsights(hw({
      disk_partitions: [
        { mount: '/', device: '/dev/sda1', total_bytes: 100, used_bytes: 40, free_bytes: 60, used_percent: 40 }
      ]
    }))
    expect(insights).toEqual([])
  })

  it('flags a low battery only when discharging', () => {
    expect(generateInsights(hw({ battery_percent: 10, battery_status: 'discharging' })).some((i) => i.text.startsWith('Battery'))).toBe(true)
    // A low battery that is charging is not a problem.
    expect(generateInsights(hw({ battery_percent: 10, battery_status: 'charging' })).some((i) => i.text.startsWith('Battery'))).toBe(false)
  })

  it('flags an old kernel but not a recent one', () => {
    expect(generateInsights(hw({ kernel_version: '4.19.0' })).some((i) => i.text.startsWith('Kernel'))).toBe(true)
    expect(generateInsights(hw({ kernel_version: '6.8.0' })).some((i) => i.text.startsWith('Kernel'))).toBe(false)
  })

  it('flags network errors per interface', () => {
    const insights = generateInsights(hw({
      network_ifaces: [
        { name: 'eth0', rx_bytes: 0, tx_bytes: 0, rx_packets: 0, tx_packets: 0, rx_errors: 3, tx_errors: 0 }
      ]
    }))
    expect(insights.some((i) => i.text.includes('eth0'))).toBe(true)
  })
})
