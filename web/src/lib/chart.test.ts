import { describe, expect, it } from 'vitest'
import { buildTimeTicks, timeDomain, withAlpha } from './chart'

// All timestamps are UTC so the assertions do not depend on the machine's
// timezone. The tick values themselves are epoch milliseconds, which are
// absolute; only their labels are localised.
const base = Date.UTC(2026, 9, 28, 18, 0, 0)
const minute = 60_000

describe('timeDomain', () => {
  it('anchors a fixed window on the requested end', () => {
    const labels = [
      new Date(base).toISOString(),
      new Date(base + minute).toISOString(),
      new Date(base + 2 * minute).toISOString()
    ]
    expect(timeDomain(labels, 60 * minute, base + 5 * minute)).toEqual({
      from: base - 55 * minute,
      to: base + 5 * minute
    })
  })

  it('anchors on the newest sample when no end is given', () => {
    const labels = [new Date(base).toISOString(), new Date(base + minute).toISOString()]
    expect(timeDomain(labels, 10 * minute)).toEqual({
      from: base - 9 * minute,
      to: base + minute
    })
  })

  it('falls back to the data span without a window', () => {
    const labels = [new Date(base).toISOString(), new Date(base + minute).toISOString()]
    expect(timeDomain(labels)).toEqual({ from: base, to: base + minute })
  })

  it('returns null when no label parses', () => {
    expect(timeDomain(['', 'not-a-date'], 60_000)).toBeNull()
  })
})

describe('buildTimeTicks', () => {
  it('aligns ticks to a round step and stays inside the window', () => {
    const ticks = buildTimeTicks(base, base + 60 * minute)
    expect(ticks.length).toBeGreaterThan(2)
    expect(ticks.length).toBeLessThanOrEqual(8)
    for (const tick of ticks) {
      expect(tick).toBeGreaterThanOrEqual(base)
      expect(tick).toBeLessThanOrEqual(base + 60 * minute)
      // Five-minute alignment: a sample at 18:05, not at 18:03.
      expect(tick % (5 * minute)).toBe(0)
    }
  })

  it('uses a wider step for a week so the axis does not crowd', () => {
    const week = 7 * 24 * 60 * minute
    const ticks = buildTimeTicks(base, base + week)
    expect(ticks.length).toBeLessThanOrEqual(8)
    expect(ticks.length).toBeGreaterThan(1)
  })

  it('returns nothing for an empty or inverted range', () => {
    expect(buildTimeTicks(base, base)).toEqual([])
    expect(buildTimeTicks(base, base - minute)).toEqual([])
  })
})

describe('withAlpha', () => {
  it('turns a hex colour into an rgba string', () => {
    expect(withAlpha('#2663d9', 0.32)).toBe('rgba(38, 99, 217, 0.32)')
  })

  it('leaves a non-hex colour alone', () => {
    expect(withAlpha('hsl(220 70% 50%)', 0.2)).toBe('hsl(220 70% 50%)')
  })
})
