import { describe, expect, it } from 'vitest'
import { normalizeHistory, normalizeHistoryPoint, pointValues, seriesStats, seriesValues, type SeriesStats } from './telemetry-history'

describe('telemetry history normalisation', () => {
  it('reads a downsampled aggregate bucket', () => {
    const point = normalizeHistoryPoint({
      timestamp: '2026-10-21T12:00:00Z',
      cpu_avg: 42,
      cpu_min: 10,
      cpu_max: 80,
      mem_avg: 61,
      mem_min: 50,
      mem_max: 70,
      net_rx_avg: 1200,
      net_tx_avg: 300,
      temp_avg: 55,
      temp_max: 70,
      power_avg: 14.5,
      sample_count: 6
    })

    expect(point).toMatchObject({
      timestamp: '2026-10-21T12:00:00Z',
      cpu: 42,
      memory: 61,
      netRx: 1200,
      netTx: 300,
      temp: 55,
      power: 14.5
    })
    // Aggregated buckets do not carry disk I/O.
    expect(point?.diskRead).toBeUndefined()
    expect(point?.disk).toBeUndefined()
  })

  it('falls back to temp_max when the bucket has no average', () => {
    const point = normalizeHistoryPoint({ timestamp: 't', cpu_avg: 1, mem_avg: 2, temp_max: 72 })
    expect(point?.temp).toBe(72)
  })

  it('treats a zero temperature as no sensor', () => {
    expect(normalizeHistoryPoint({ timestamp: 't', cpu_avg: 1, mem_avg: 2, temp_avg: 0 })?.temp).toBeNull()
    expect(normalizeHistoryPoint({ timestamp: 't', payload: { cpu_temperature: 0 } })?.temp).toBeNull()
  })

  it('reads a raw hardware payload, including disk and power', () => {
    const point = normalizeHistoryPoint({
      timestamp: '2026-10-21T12:00:00Z',
      payload: JSON.stringify({
        cpu_usage_percent: 25,
        memory_used_bytes: 2 * 1024 ** 3,
        memory_total_bytes: 8 * 1024 ** 3,
        net_rx_rate: 2048,
        net_tx_rate: 512,
        cpu_temperature: 48,
        disk_read_rate: 100,
        disk_write_rate: 200,
        power_watts: 9.5,
        disk_partitions: [
          { mount: '/etc/hosts', total_bytes: 10, used_percent: 99 },
          { mount: '/', total_bytes: 100, used_percent: 61 }
        ]
      })
    })

    expect(point).toMatchObject({
      cpu: 25,
      memory: 25,
      netRx: 2048,
      netTx: 512,
      temp: 48,
      disk: 61,
      diskRead: 100,
      diskWrite: 200,
      power: 9.5
    })
  })

  it('uses the largest partition when a container has no root mount', () => {
    const point = normalizeHistoryPoint({
      timestamp: 't',
      payload: {
        disk_partitions: [
          { mount: '/etc/hosts', total_bytes: 100, used_percent: 40 },
          { mount: '/etc/resolv.conf', total_bytes: 10, used_percent: 90 }
        ]
      }
    })

    expect(point?.disk).toBe(40)
  })

  it('drops malformed points instead of inventing a zero', () => {
    expect(normalizeHistoryPoint(null)).toBeNull()
    expect(normalizeHistoryPoint({ payload: '{not json' })).toBeNull()
    expect(normalizeHistory({ points: [null, { timestamp: 't', payload: { cpu_usage_percent: 3 } }] })).toHaveLength(1)
    expect(normalizeHistory({})).toEqual([])
  })

  it('keeps history points aligned with their timestamps', () => {
    const points = [
      { timestamp: 'a', cpu: 1, memory: 2, netRx: 3, netTx: 4, temp: 50 },
      { timestamp: 'b', cpu: 5, memory: 6, netRx: 7, netTx: 8, temp: null }
    ]

    expect(pointValues(points, 'temp')).toEqual([50, null])
    expect(seriesValues(points, 'temp')).toEqual([50])
    expect(seriesValues(points, 'cpu')).toEqual([1, 5])
  })
})

describe('series statistics', () => {
  it('reports last, average, minimum and maximum', () => {
    const stats = seriesStats([12, 18, 30])
    expect(stats).toEqual<SeriesStats>({ last: 30, avg: 20, min: 12, max: 30 })
  })

  it('handles one value and an empty series', () => {
    expect(seriesStats([7])).toEqual<SeriesStats>({ last: 7, avg: 7, min: 7, max: 7 })
    expect(seriesStats([])).toBeNull()
  })
})
