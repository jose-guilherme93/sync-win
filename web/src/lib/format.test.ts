import { describe, expect, it } from 'vitest'
import {
  clampPercent,
  formatAbsolute,
  formatBytes,
  formatDuration,
  formatRelative,
  isTimestampMissing,
  parseTimestamp,
  severityFor,
  tempSeverity
} from './format'

const NOW = Date.parse('2024-06-15T12:00:00Z')

describe('parseTimestamp', () => {
  it('returns null for absent values', () => {
    expect(parseTimestamp(null)).toBeNull()
    expect(parseTimestamp(undefined)).toBeNull()
    expect(parseTimestamp('')).toBeNull()
    expect(parseTimestamp('   ')).toBeNull()
  })

  it('returns null for the Go zero time', () => {
    // This is the value behind the "739893d ago" bug: it is a valid RFC3339
    // string, so a truthiness check would pass and the arithmetic would run.
    expect(parseTimestamp('0001-01-01T00:00:00Z')).toBeNull()
    expect(parseTimestamp('0001-01-01T00:00:00.000Z')).toBeNull()
    expect(parseTimestamp('0001-01-01')).toBeNull()
  })

  it('returns null for the numeric zero time', () => {
    expect(parseTimestamp(0)).toBeNull()
    expect(parseTimestamp('0')).toBeNull()
  })

  it('returns null for unparsable strings', () => {
    expect(parseTimestamp('not a date')).toBeNull()
  })

  it('parses a normal RFC3339 timestamp', () => {
    expect(parseTimestamp('2024-06-15T11:59:00Z')).toBe(Date.parse('2024-06-15T11:59:00Z'))
  })

  it('treats small numbers as epoch seconds', () => {
    expect(parseTimestamp(1718452800)).toBe(1718452800000)
  })

  it('treats large numbers as epoch milliseconds', () => {
    expect(parseTimestamp(1718452800000)).toBe(1718452800000)
  })
})

describe('formatRelative', () => {
  it('reports never for the Go zero time instead of a huge day count', () => {
    expect(formatRelative('0001-01-01T00:00:00Z', NOW)).toBe('never')
  })

  it('reports never for absent values', () => {
    expect(formatRelative(null, NOW)).toBe('never')
    expect(formatRelative(undefined, NOW)).toBe('never')
    expect(formatRelative('', NOW)).toBe('never')
  })

  it('reports never for unparsable values', () => {
    expect(formatRelative('garbage', NOW)).toBe('never')
  })

  it('never produces a day count larger than 36500 for a zero time', () => {
    // Guards the original symptom directly.
    const out = formatRelative('0001-01-01T00:00:00Z', NOW)
    expect(out).not.toMatch(/ago$/)
    expect(out).not.toContain('739893')
  })

  it('reports just now under ten seconds', () => {
    expect(formatRelative(NOW - 3000, NOW)).toBe('just now')
  })

  it('reports seconds, minutes, hours and days', () => {
    expect(formatRelative(NOW - 45_000, NOW)).toBe('45s ago')
    expect(formatRelative(NOW - 5 * 60_000, NOW)).toBe('5m ago')
    expect(formatRelative(NOW - 3 * 3_600_000, NOW)).toBe('3h ago')
    expect(formatRelative(NOW - 2 * 86_400_000, NOW)).toBe('2d ago')
  })

  it('clamps future timestamps to just now', () => {
    expect(formatRelative(NOW + 60_000, NOW)).toBe('just now')
  })
})

describe('isTimestampMissing', () => {
  it('detects every unusable shape', () => {
    expect(isTimestampMissing('0001-01-01T00:00:00Z')).toBe(true)
    expect(isTimestampMissing(null)).toBe(true)
    expect(isTimestampMissing('')).toBe(true)
  })

  it('detects a usable timestamp', () => {
    expect(isTimestampMissing('2024-06-15T11:59:00Z')).toBe(false)
  })
})

describe('formatAbsolute', () => {
  it('returns an empty string when there is nothing to show', () => {
    // It feeds a title attribute, so a placeholder would leak into the tooltip.
    expect(formatAbsolute(null)).toBe('')
    expect(formatAbsolute('0001-01-01T00:00:00Z')).toBe('')
  })

  it('formats a usable timestamp', () => {
    expect(formatAbsolute('2024-06-15T11:59:00Z')).not.toBe('')
  })
})

describe('formatBytes', () => {
  it('handles zero, negative and invalid input', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-5)).toBe('0 B')
    expect(formatBytes(null)).toBe('0 B')
    expect(formatBytes(undefined)).toBe('0 B')
    expect(formatBytes(Number.NaN)).toBe('0 B')
  })

  it('scales through the units', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(1024 ** 3)).toBe('1.0 GB')
  })
})

describe('formatDuration', () => {
  it('handles missing input', () => {
    expect(formatDuration(null)).toBe('0m')
    expect(formatDuration(0)).toBe('0m')
  })

  it('reports minutes, hours and days', () => {
    expect(formatDuration(300)).toBe('5m')
    expect(formatDuration(3900)).toBe('1h 5m')
    expect(formatDuration(90000)).toBe('1d 1h')
  })
})

describe('clampPercent', () => {
  it('clamps into 0..100', () => {
    expect(clampPercent(-10)).toBe(0)
    expect(clampPercent(150)).toBe(100)
    expect(clampPercent(42.5)).toBe(42.5)
  })

  it('treats missing input as zero', () => {
    expect(clampPercent(null)).toBe(0)
    expect(clampPercent(undefined)).toBe(0)
    expect(clampPercent(Number.NaN)).toBe(0)
  })
})

describe('severityFor', () => {
  it('maps usage to the three states', () => {
    expect(severityFor(10)).toBe('ok')
    expect(severityFor(75)).toBe('warn')
    expect(severityFor(95)).toBe('crit')
  })
})

describe('tempSeverity', () => {
  it('maps celsius to the three states', () => {
    expect(tempSeverity(45)).toBe('ok')
    expect(tempSeverity(75)).toBe('warn')
    expect(tempSeverity(90)).toBe('crit')
  })

  it('treats a missing reading as ok rather than crit', () => {
    // An absent sensor is not a hot sensor.
    expect(tempSeverity(null)).toBe('ok')
    expect(tempSeverity(undefined)).toBe('ok')
  })
})