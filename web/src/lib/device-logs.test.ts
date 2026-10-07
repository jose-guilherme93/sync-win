import { describe, expect, it } from 'vitest'
import {
  PAGE_SIZE,
  UNKNOWN_SOURCE,
  buildQuery,
  countByLevel,
  displayTime,
  fullTimestamp,
  matchesFilter,
  sinceFor,
  toClipboardText,
  type DeviceLogEntry
} from './device-logs'

function entry(over: Partial<DeviceLogEntry> = {}): DeviceLogEntry {
  return {
    id: 1,
    device_id: 'dev-1',
    ts: '2026-10-07T12:00:00Z',
    level: 'info',
    source: 'systemd',
    message: 'Started unit.',
    ...over
  }
}

describe('sinceFor', () => {
  const now = Date.parse('2026-10-07T12:00:00Z')

  it('returns no bound for the all range', () => {
    expect(sinceFor('all', now)).toBe('')
  })

  it('subtracts the window and emits RFC3339', () => {
    expect(sinceFor('15m', now)).toBe('2026-10-07T11:45:00.000Z')
    expect(sinceFor('1h', now)).toBe('2026-10-07T11:00:00.000Z')
    expect(sinceFor('24h', now)).toBe('2026-10-06T12:00:00.000Z')
  })

  it('produces a bound the server accepts', () => {
    // The handler rejects anything time.Parse(time.RFC3339) refuses.
    expect(Number.isFinite(Date.parse(sinceFor('6h', now)))).toBe(true)
  })
})

describe('buildQuery', () => {
  it('omits defaults so the API returns everything', () => {
    const params = buildQuery({ level: 'all', source: '', search: '', since: '', limit: 200, offset: 0 })
    expect(params.get('level')).toBeNull()
    expect(params.get('source')).toBeNull()
    expect(params.get('search')).toBeNull()
    expect(params.get('since')).toBeNull()
    expect(params.get('limit')).toBe(String(PAGE_SIZE))
    expect(params.get('offset')).toBe('0')
  })

  it('includes only the filters that are set', () => {
    const params = buildQuery({ level: 'error', source: 'kernel', search: ' disk ', since: 'x', limit: 50, offset: 10 })
    expect(params.get('level')).toBe('error')
    expect(params.get('source')).toBe('kernel')
    expect(params.get('search')).toBe('disk')
    expect(params.get('since')).toBe('x')
    expect(params.get('limit')).toBe('50')
    expect(params.get('offset')).toBe('10')
  })
})

describe('matchesFilter', () => {
  const line = entry({ level: 'error', source: 'kernel', message: 'EXT4-fs error on nvme0n1p2' })

  it('accepts everything when no filter is active', () => {
    expect(matchesFilter(line, 'all', '', '')).toBe(true)
  })

  it('matches on level', () => {
    expect(matchesFilter(line, 'error', '', '')).toBe(true)
    expect(matchesFilter(line, 'warn', '', '')).toBe(false)
  })

  it('requires the source to match exactly', () => {
    expect(matchesFilter(line, 'all', 'kernel', '')).toBe(true)
    expect(matchesFilter(line, 'all', 'kern', '')).toBe(false)
  })

  it('searches the message case-insensitively and ignores padding', () => {
    expect(matchesFilter(line, 'all', '', 'ext4')).toBe(true)
    expect(matchesFilter(line, 'all', '', '  EXT4  ')).toBe(true)
    expect(matchesFilter(line, 'all', '', 'nvme0n1p2')).toBe(true)
    expect(matchesFilter(line, 'all', '', 'nothing here')).toBe(false)
  })

  it('combines filters', () => {
    expect(matchesFilter(line, 'error', 'kernel', 'ext4')).toBe(true)
    expect(matchesFilter(line, 'warn', 'kernel', 'ext4')).toBe(false)
  })
})

describe('displayTime', () => {
  it('renders only the clock for an entry from today', () => {
    const now = new Date('2026-10-07T12:00:00Z')
    const out = displayTime('2026-10-07T11:59:00Z', now.getTime())
    expect(out).not.toMatch(/\d{4}/)
    expect(out.length).toBeLessThanOrEqual(8)
  })

  it('includes the date for an older entry', () => {
    const now = new Date('2026-10-07T12:00:00Z')
    const out = displayTime('2026-10-05T23:10:00Z', now.getTime())
    expect(out.length).toBeGreaterThan(8)
  })

  it('passes through an unparseable timestamp instead of showing NaN', () => {
    expect(displayTime('not-a-date', Date.now())).toBe('not-a-date')
    expect(displayTime('', Date.now())).toBe('—')
  })
})

describe('fullTimestamp', () => {
  it('keeps the zone so a line can be correlated with local journalctl', () => {
    expect(fullTimestamp('2026-10-07T12:00:00Z')).toMatch(/2026/)
  })

  it('returns an empty string for missing input', () => {
    expect(fullTimestamp('')).toBe('')
  })
})

describe('countByLevel', () => {
  it('tallies each level', () => {
    const counts = countByLevel([
      entry({ level: 'error' }),
      entry({ level: 'error' }),
      entry({ level: 'warn' }),
      entry({ level: 'info' })
    ])
    expect(counts).toEqual({ error: 2, warn: 1, info: 1 })
  })

  it('ignores an unrecognised level rather than creating a key', () => {
    const counts = countByLevel([entry({ level: 'debug' })])
    expect(Object.keys(counts).sort()).toEqual(['error', 'info', 'warn'])
  })
})

describe('toClipboardText', () => {
  it('emits one tab-free, pasteable line per entry', () => {
    const text = toClipboardText([
      entry({ id: 1, level: 'error', source: 'kernel', message: 'EXT4-fs error', ts: '2026-10-07T12:00:00Z' }),
      entry({ id: 2, level: 'info', source: 'systemd', message: 'Started unit.', ts: '2026-10-07T12:00:01Z' })
    ])
    const lines = text.split('\n')
    expect(lines).toHaveLength(2)
    expect(lines[0]).toContain('EXT4-fs error')
    expect(lines[0]).toContain('kernel')
    expect(lines[0]).toContain('ERROR')
    expect(lines[1]).toContain('INFO')
  })

  it('labels a line with no source', () => {
    expect(toClipboardText([entry({ source: '' })])).toContain(UNKNOWN_SOURCE)
  })

  it('returns an empty string for no entries', () => {
    expect(toClipboardText([])).toBe('')
  })
})