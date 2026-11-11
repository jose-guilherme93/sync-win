import { describe, expect, it } from 'vitest'
import type { ProcessRow } from './types'
import {
  EMPTY_FILTER,
  buildHeatmap,
  buildTable,
  canSignal,
  eventMarkers,
  filterRows,
  formatUptime,
  historyWindowMs,
  ioText,
  parseProcessHistory,
  parseProcessSnapshot,
  signalBlockReason,
  sortRows,
  type ProcessFilter
} from './processes'

function row(overrides: Partial<ProcessRow> = {}): ProcessRow {
  return {
    pid: 100,
    ppid: 1,
    name: 'nginx',
    cmd: 'nginx: worker process',
    user: 'www-data',
    uid: 33,
    state: 'S',
    threads: 2,
    rss_bytes: 34 * 1024 * 1024,
    vsz_bytes: 100 * 1024 * 1024,
    swap_bytes: 0,
    cpu_percent: 4.2,
    cpu_time_seconds: 120,
    start_time_unix: 1_700_000_000,
    uptime_seconds: 3600,
    io_read_bps: 2048,
    io_write_bps: 1024,
    fd_count: 12,
    oom_score: 300,
    nice: 0,
    prio: 20,
    cgroup: '/system.slice/nginx.service',
    unit: 'nginx.service',
    container: '',
    ports: [{ proto: 'tcp', port: 80, addr: '0.0.0.0' }],
    kernel: false,
    ...overrides
  }
}

function snapshot(overrides: Record<string, unknown> = {}) {
  return {
    processes: [row()],
    stats: {
      total: 187,
      threads: 642,
      running: 3,
      sleeping: 180,
      stopped: 0,
      zombie: 1,
      blocked: 2,
      idle: 1,
      load1: 6.41,
      load5: 5.87,
      load15: 4.12,
      psi_cpu_some_avg10: 31.8,
      psi_mem_some_avg10: 2.1,
      psi_io_some_avg10: 0,
      oom_kills_24h: 1
    },
    collected_at: '2026-11-11T03:04:00Z',
    io_available: true,
    signals_enabled: true,
    ...overrides
  }
}

describe('parseProcessSnapshot', () => {
  it('reads the wire shape without inventing fields', () => {
    const snap = parseProcessSnapshot(snapshot())
    expect(snap.collected).toBe(true)
    expect(snap.io_available).toBe(true)
    expect(snap.signals_enabled).toBe(true)
    expect(snap.processes).toHaveLength(1)
    expect(snap.processes[0].name).toBe('nginx')
    expect(snap.processes[0].ports).toEqual([{ proto: 'tcp', port: 80, addr: '0.0.0.0' }])
    expect(snap.stats?.zombie).toBe(1)
    expect(snap.stats?.load1).toBe(6.41)
  })

  it('keeps "not collected" distinct from an empty list', () => {
    const snap = parseProcessSnapshot(snapshot({ processes: [], stats: null, processes_collected: false }))
    expect(snap.collected).toBe(false)
    expect(snap.processes).toEqual([])
  })

  it('treats a payload with neither rows nor stats as not collected', () => {
    const snap = parseProcessSnapshot({ collected_at: '2026-11-11T03:04:00Z' })
    expect(snap.collected).toBe(false)
    expect(snap.io_available).toBe(false)
    expect(snap.signals_enabled).toBe(false)
  })

  it('fails closed on the gates when the server omits them', () => {
    const snap = parseProcessSnapshot({ processes: [row()], collected_at: '' })
    expect(snap.io_available).toBe(false)
    expect(snap.signals_enabled).toBe(false)
  })

  it('rejects a payload that is not an object', () => {
    expect(() => parseProcessSnapshot(null)).toThrow()
    expect(() => parseProcessSnapshot('nope')).toThrow()
  })

  it('rejects processes and stats of the wrong type instead of guessing', () => {
    expect(() => parseProcessSnapshot(snapshot({ processes: 'nope' }))).toThrow()
    expect(() => parseProcessSnapshot(snapshot({ stats: [] }))).toThrow()
  })

  it('drops individual rows without a usable pid and keeps the rest', () => {
    const snap = parseProcessSnapshot(
      snapshot({ processes: [row({ pid: 0 }), row({ pid: 2201, name: 'postgres' }), { name: 'broken' }] })
    )
    expect(snap.processes).toHaveLength(1)
    expect(snap.processes[0].pid).toBe(2201)
  })

  it('normalises a row that arrives with missing fields', () => {
    const snap = parseProcessSnapshot(
      snapshot({ processes: [{ pid: 42, name: 'sshd', state: 'Q', threads: 'many' }] })
    )
    const parsed = snap.processes[0]
    expect(parsed.pid).toBe(42)
    expect(parsed.state).toBe('S')
    expect(parsed.threads).toBe(0)
    expect(parsed.io_read_bps).toBe(0)
    expect(parsed.ports).toEqual([])
    expect(parsed.kernel).toBe(false)
  })
})

describe('ioText', () => {
  it('renders an em dash when IO is unavailable', () => {
    expect(ioText(row({ io_read_bps: 4096 }), false)).toBe('—')
  })

  it('renders an em dash for a kernel thread or a zombie even when IO is on', () => {
    expect(ioText(row({ kernel: true }), true)).toBe('—')
    expect(ioText(row({ state: 'Z' }), true)).toBe('—')
  })

  it('renders a real zero, not an em dash, when the kernel reported zero', () => {
    expect(ioText(row({ io_read_bps: 0, io_write_bps: 0 }), true)).toBe('0 B/s / 0 B/s')
  })

  it('renders both directions', () => {
    expect(ioText(row({ io_read_bps: 2048, io_write_bps: 1024 }), true)).toBe('2.0 KB/s / 1.0 KB/s')
  })
})

describe('sortRows', () => {
  const rows = [
    row({ pid: 3, name: 'zsh', cpu_percent: 1, rss_bytes: 300 }),
    row({ pid: 1, name: 'nginx', cpu_percent: 40, rss_bytes: 100 }),
    row({ pid: 2, name: 'node', cpu_percent: 10, rss_bytes: 200 })
  ]

  it('sorts by CPU descending by default', () => {
    expect(sortRows(rows, 'cpu', 'desc').map((r) => r.pid)).toEqual([1, 2, 3])
  })

  it('reverses direction without mutating the input', () => {
    expect(sortRows(rows, 'cpu', 'asc').map((r) => r.pid)).toEqual([3, 2, 1])
    expect(rows.map((r) => r.pid)).toEqual([3, 1, 2])
  })

  it('sorts pid numerically and name alphabetically', () => {
    expect(sortRows(rows, 'pid', 'asc').map((r) => r.pid)).toEqual([1, 2, 3])
    expect(sortRows(rows, 'name', 'asc').map((r) => r.name)).toEqual(['nginx', 'node', 'zsh'])
  })

  it('sorts IO by read plus write', () => {
    const io = [
      row({ pid: 1, io_read_bps: 10, io_write_bps: 0 }),
      row({ pid: 2, io_read_bps: 0, io_write_bps: 5000 })
    ]
    expect(sortRows(io, 'io', 'desc').map((r) => r.pid)).toEqual([2, 1])
  })
})

describe('filterRows', () => {
  const rows = [
    row({ pid: 2401, name: 'postgres', cmd: 'postgres: autovacuum worker', user: 'postgres', unit: 'postgresql.service' }),
    row({ pid: 1, name: 'systemd', cmd: '/sbin/init', user: 'root', unit: 'init.scope', state: 'S', ports: [] }),
    row({ pid: 4200, name: 'kworker/0:1', cmd: '', user: 'root', unit: '', kernel: true, ports: [] }),
    row({ pid: 5100, name: 'dead', cmd: 'dead', user: 'root', unit: '', state: 'Z', ports: [] }),
    row({ pid: 6000, name: 'node', cmd: 'node /app/server.js', user: 'deploy', unit: '', ports: [] })
  ]

  function withFilter(overrides: Partial<ProcessFilter>): string[] {
    return filterRows(rows, { ...EMPTY_FILTER, hideKernel: true, ...overrides }).map((r) => r.name)
  }

  it('searches name, cmdline, pid, user and unit', () => {
    expect(withFilter({ search: 'nginx' })).toEqual([])
    expect(withFilter({ search: 'autovacuum' })).toEqual(['postgres'])
    expect(withFilter({ search: '2401' })).toEqual(['postgres'])
    expect(withFilter({ search: 'deploy' })).toEqual(['node'])
    expect(withFilter({ search: 'postgresql.service' })).toEqual(['postgres'])
  })

  it('filters by state chips and keeps all states when none are chosen', () => {
    expect(withFilter({ states: ['Z'] })).toEqual(['dead'])
    expect(withFilter({ states: [] })).toContain('systemd')
    expect(withFilter({ states: ['Z', 'S'] })).toEqual(['postgres', 'systemd', 'dead', 'node'])
  })

  it('hides kernel threads by default and shows them on request', () => {
    expect(withFilter({})).not.toContain('kworker/0:1')
    expect(withFilter({ hideKernel: false })).toContain('kworker/0:1')
  })

  it('keeps only listeners when asked', () => {
    expect(withFilter({ listeningOnly: true })).toEqual(['postgres'])
  })

  it('restricts to the names of a history bucket window', () => {
    expect(withFilter({ windowNames: ['node', 'postgres'] })).toEqual(['postgres', 'node'])
  })

  it('is case insensitive and trims the search', () => {
    expect(withFilter({ search: '  POSTGRES ' })).toEqual(['postgres'])
  })
})

describe('buildTable', () => {
  const parent = row({ pid: 1430, ppid: 1, name: 'chrome', cpu_percent: 0.9 })
  const childA = row({ pid: 5620, ppid: 1430, name: 'chrome', cpu_percent: 0.9 })
  const childB = row({ pid: 5641, ppid: 1430, name: 'chrome', cpu_percent: 2.2 })
  const grandchild = row({ pid: 5644, ppid: 5641, name: 'chrome', cpu_percent: 1.2 })
  const other = row({ pid: 1, ppid: 0, name: 'systemd', cpu_percent: 0.1 })
  const rows = [parent, childA, childB, grandchild, other]

  it('keeps flat order and no depth when nothing is expanded', () => {
    const table = buildTable(rows, { mode: 'flat', sortKey: 'cpu', sortDir: 'desc', expanded: new Set() })
    expect(table.map((entry) => entry.row.pid)).toEqual([5641, 5644, 1430, 5620, 1])
    expect(table.every((entry) => entry.depth === 0)).toBe(true)
    expect(table.find((entry) => entry.row.pid === 1430)?.hasChildren).toBe(true)
  })

  it('inlines children under an expanded parent once, indented', () => {
    const table = buildTable(rows, {
      mode: 'flat',
      sortKey: 'cpu',
      sortDir: 'desc',
      expanded: new Set([1430])
    })
    expect(table.map((entry) => entry.row.pid)).toEqual([5641, 5644, 1430, 5620, 1])
    expect(table.find((entry) => entry.row.pid === 5620)?.depth).toBe(1)
  })

  it('walks the tree from roots and honours expansion', () => {
    const collapsed = buildTable(rows, { mode: 'tree', sortKey: 'cpu', sortDir: 'desc', expanded: new Set([1]) })
    expect(collapsed.map((entry) => entry.row.pid)).toEqual([1, 1430])
    expect(collapsed.find((entry) => entry.row.pid === 1430)?.depth).toBe(1)
    expect(collapsed.find((entry) => entry.row.pid === 1430)?.hasChildren).toBe(true)
    expect(collapsed.find((entry) => entry.row.pid === 1430)?.expanded).toBe(false)

    const expanded = buildTable(rows, {
      mode: 'tree',
      sortKey: 'cpu',
      sortDir: 'desc',
      expanded: new Set([1, 1430, 5641])
    })
    expect(expanded.map((entry) => entry.row.pid)).toEqual([1, 1430, 5641, 5644, 5620])
    expect(expanded.find((entry) => entry.row.pid === 5644)?.depth).toBe(3)
  })

  it('treats a row whose parent is not in the sample as a root', () => {
    const orphan = row({ pid: 9021, ppid: 489, name: 'rsync' })
    const table = buildTable([orphan], { mode: 'tree', sortKey: 'cpu', sortDir: 'desc', expanded: new Set() })
    expect(table.map((entry) => entry.row.pid)).toEqual([9021])
    expect(table[0].depth).toBe(0)
  })
})

describe('signal gate', () => {
  const snap = parseProcessSnapshot(snapshot())

  it('blocks everything when the server gate is off', () => {
    const off = parseProcessSnapshot(snapshot({ signals_enabled: false }))
    expect(signalBlockReason(row(), off)).toBe('signals off')
    expect(canSignal(row(), off)).toBe(false)
  })

  it('rejects the protected targets the agent rejects', () => {
    expect(signalBlockReason(row({ pid: 1, name: 'systemd' }), snap)).toBe('pid 1')
    expect(signalBlockReason(row({ kernel: true }), snap)).toBe('kernel thread')
    expect(signalBlockReason(row({ state: 'Z' }), snap)).toBe('zombie')
    expect(signalBlockReason(row({ name: 'sshd' }), snap)).toBe('protected')
    expect(signalBlockReason(row({ name: 'dockerd' }), snap)).toBe('protected')
    expect(signalBlockReason(row({ name: 'containerd-shim' }), snap)).toBe('protected')
    expect(signalBlockReason(row({ name: 'containerd-shim-runc-v2' }), snap)).toBe('protected')
    expect(signalBlockReason(row({ name: 'sync-win-agent' }), snap)).toBe('protected')
    expect(signalBlockReason(row({ unit: 'docker.service' }), snap)).toBe('protected')
    expect(signalBlockReason(row({ unit: 'ssh.service' }), snap)).toBe('protected')
  })

  it('allows an ordinary process', () => {
    expect(signalBlockReason(row(), snap)).toBeNull()
    expect(canSignal(row(), snap)).toBe(true)
  })
})

describe('parseProcessHistory', () => {
  it('keeps the documented series, events and buckets', () => {
    const history = parseProcessHistory({
      series: [
        {
          name: 'postgres',
          points: [
            [1_700_000_000, 12.5, 512],
            [1_700_000_060, 41.2, 530]
          ]
        }
      ],
      events: [
        { t: 1_700_000_060, type: 'exited', pid: 2401, name: 'postgres', exit_code: 0 },
        { t: 1_700_000_000, type: 'started', pid: 2401, name: 'postgres' }
      ],
      buckets: [
        { t: 1_700_000_060, top: [{ name: 'postgres', cpu: 41.2 }] },
        { t: 1_700_000_000, top: [{ name: 'postgres', cpu: 12.5 }] }
      ]
    })
    expect(history.series[0].points).toHaveLength(2)
    expect(history.events.map((e) => e.type)).toEqual(['started', 'exited'])
    expect(history.buckets.map((b) => b.t)).toEqual([1_700_000_000, 1_700_000_060])
  })

  it('rejects a payload that is not an object', () => {
    expect(() => parseProcessHistory(undefined)).toThrow()
    expect(() => parseProcessHistory(42)).toThrow()
  })

  it('drops unusable points and unknown event types instead of plotting them', () => {
    const history = parseProcessHistory({
      series: [{ name: 'node', points: [[1_700_000_000, 'x', null], [1_700_000_060, 2, 30]] }],
      events: [{ t: 1, type: 'exploded', pid: 1, name: 'x' }, { t: 2, type: 'started', pid: 2, name: 'y' }],
      buckets: []
    })
    expect(history.series[0].points).toEqual([[1_700_000_060, 2, 30]])
    expect(history.events).toHaveLength(1)
  })

  it('returns empty collections for a valid but young window', () => {
    const history = parseProcessHistory({ series: [], events: [], buckets: [] })
    expect(history.series).toEqual([])
    expect(history.buckets).toEqual([])
  })
})

describe('historyWindowMs', () => {
  it('maps the three ranges the endpoint accepts', () => {
    expect(historyWindowMs('1h')).toBe(3_600_000)
    expect(historyWindowMs('24h')).toBe(86_400_000)
    expect(historyWindowMs('7d')).toBe(604_800_000)
  })
})

describe('buildHeatmap', () => {
  it('orders names by peak CPU and leaves absent cells blank', () => {
    const map = buildHeatmap(
      [
        { t: 1, top: [{ name: 'a', cpu: 10 }] },
        { t: 2, top: [{ name: 'a', cpu: 0 }, { name: 'b', cpu: 80 }] },
        { t: 3, top: [] }
      ],
      8
    )
    expect(map.names).toEqual(['b', 'a'])
    expect(map.rows[0].cells).toEqual([null, 80, null])
    expect(map.rows[1].cells).toEqual([10, 0, null])
    expect(map.max).toBe(80)
  })

  it('caps the number of rows', () => {
    const buckets = [
      {
        t: 1,
        top: Array.from({ length: 12 }, (_, i) => ({ name: `p${i}`, cpu: i }))
      }
    ]
    expect(buildHeatmap(buckets, 8).names).toHaveLength(8)
  })
})

describe('eventMarkers', () => {
  it('positions markers inside the window and drops the rest', () => {
    const markers = eventMarkers(
      [
        { t: 50, type: 'started', pid: 1, name: 'a' },
        { t: 150, type: 'exited', pid: 2, name: 'b' },
        { t: 900, type: 'started', pid: 3, name: 'c' }
      ],
      0,
      200_000
    )
    expect(markers.map((m) => Math.round(m.left))).toEqual([25, 75])
  })

  it('returns nothing for an empty or inverted window', () => {
    expect(eventMarkers([{ t: 5, type: 'started', pid: 1, name: 'a' }], 10, 10)).toEqual([])
  })
})

describe('formatUptime', () => {
  it('keeps the two most significant units', () => {
    expect(formatUptime(40)).toBe('<1m')
    expect(formatUptime(41 * 60)).toBe('41m')
    expect(formatUptime(2 * 3600 + 16 * 60)).toBe('2h 16m')
    expect(formatUptime(18 * 86400 + 4 * 3600)).toBe('18d 4h')
  })
})
