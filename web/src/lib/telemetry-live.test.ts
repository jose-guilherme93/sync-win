import { describe, expect, it } from 'vitest'
import { liveTail } from './telemetry-live'
import type { ChartPoint } from './telemetry-store'


describe('liveTail', () => {
  const point = (timestamp: string, cpu: number): ChartPoint => ({
    timestamp,
    cpu,
    memory: 10,
    netRx: 0,
    netTx: 0,
    temp: null
  })

  it('keeps only the trailing minute, anchored on the newest sample', () => {
    const points = [
      point('2026-11-13T12:00:00Z', 1),
      point('2026-11-13T12:00:30Z', 2),
      point('2026-11-13T12:00:50Z', 3),
      point('2026-11-13T12:01:10Z', 4)
    ]
    expect(liveTail(points, 60_000).map((p) => p.cpu)).toEqual([2, 3, 4])
  })

  it('returns nothing for an empty or unparseable series', () => {
    expect(liveTail([])).toEqual([])
    expect(liveTail([point('not-a-date', 1)])).toEqual([])
  })
})
