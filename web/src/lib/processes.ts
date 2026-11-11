// Process screen model: payload validation, filtering, sorting, tree walking
// and the small formatters the table and the drawer share.
//
// The rules here mirror the wire spec (docs/process-tab-spec.md §2) and the
// agent's command guard (§3). Nothing in this file invents a field: what the
// agent does not report stays absent, and absent is rendered as an em dash,
// never as a zero that reads like a measurement.

import { formatRate } from './format'
import type {
  ProcessEvent,
  ProcessEventType,
  ProcessHistory,
  ProcessHistoryBucket,
  ProcessHistorySeries,
  ProcessRow,
  ProcessSignal,
  ProcessSnapshot,
  ProcessState,
  ProcessStats
} from './types'

export const PROCESS_STATES: ProcessState[] = ['R', 'S', 'D', 'Z', 'T', 'I']

// One-letter state, spelled out for the tooltip. These are the single words
// that actually carry information on this screen.
export const STATE_LABELS: Record<ProcessState, string> = {
  R: 'running',
  S: 'sleeping',
  D: 'blocked on IO',
  Z: 'zombie',
  T: 'stopped',
  I: 'idle kernel thread'
}

export const SIGNALS: ProcessSignal[] = ['TERM', 'KILL', 'STOP', 'CONT']

// Signals per device per minute, from the agent guard. The screen counts down
// as it sends, so the button can say why it stopped.
export const SIGNAL_RATE_LIMIT = 10

const HISTORY_RANGE_MS: Record<string, number> = {
  '1h': 3_600_000,
  '24h': 86_400_000,
  '7d': 604_800_000
}

export const HISTORY_RANGES = ['1h', '24h', '7d'] as const
export type HistoryRange = (typeof HISTORY_RANGES)[number]

export function historyWindowMs(range: string): number {
  return HISTORY_RANGE_MS[range] ?? HISTORY_RANGE_MS['24h']
}

// Targets the agent refuses regardless of policy. The web mirrors them so a
// control can be shown as blocked before a request is ever made; the agent
// still rejects them, which is the boundary that matters.
const PROTECTED_NAMES = new Set([
  'systemd',
  'init',
  'systemd-journald',
  'dockerd',
  'containerd',
  'sshd',
  'sync-win-agent'
])

const PROTECTED_UNITS = new Set([
  'systemd-journald.service',
  'ssh.service',
  'sshd.service',
  'docker.service',
  'containerd.service',
  'sync-win-agent.service'
])

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function num(value: unknown, fallback = 0): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

function str(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}

function state(value: unknown): ProcessState {
  return typeof value === 'string' && (PROCESS_STATES as string[]).includes(value)
    ? (value as ProcessState)
    : 'S'
}

function ports(value: unknown) {
  if (!Array.isArray(value)) return []
  const out: ProcessRow['ports'] = []
  for (const raw of value) {
    if (!isRecord(raw)) continue
    const proto = str(raw.proto)
    if (proto !== 'tcp' && proto !== 'tcp6' && proto !== 'udp' && proto !== 'udp6') continue
    const port = num(raw.port, -1)
    if (!Number.isInteger(port) || port < 0 || port > 65_535) continue
    out.push({ proto, port, addr: str(raw.addr) })
  }
  return out
}

function parseStats(raw: unknown): ProcessStats | null {
  if (!isRecord(raw)) return null
  return {
    total: num(raw.total),
    threads: num(raw.threads),
    running: num(raw.running),
    sleeping: num(raw.sleeping),
    stopped: num(raw.stopped),
    zombie: num(raw.zombie),
    blocked: num(raw.blocked),
    idle: num(raw.idle),
    load1: num(raw.load1),
    load5: num(raw.load5),
    load15: num(raw.load15),
    psi_cpu_some_avg10: num(raw.psi_cpu_some_avg10),
    psi_mem_some_avg10: num(raw.psi_mem_some_avg10),
    psi_io_some_avg10: num(raw.psi_io_some_avg10),
    oom_kills_24h: num(raw.oom_kills_24h)
  }
}

function parseRow(raw: unknown): ProcessRow | null {
  if (!isRecord(raw)) return null
  const pid = num(raw.pid, 0)
  if (!Number.isInteger(pid) || pid <= 0) return null
  return {
    pid,
    ppid: num(raw.ppid),
    name: str(raw.name),
    cmd: str(raw.cmd),
    user: str(raw.user),
    uid: num(raw.uid),
    state: state(raw.state),
    threads: num(raw.threads),
    rss_bytes: num(raw.rss_bytes),
    vsz_bytes: num(raw.vsz_bytes),
    swap_bytes: num(raw.swap_bytes),
    cpu_percent: num(raw.cpu_percent),
    cpu_time_seconds: num(raw.cpu_time_seconds),
    start_time_unix: num(raw.start_time_unix),
    uptime_seconds: num(raw.uptime_seconds),
    io_read_bps: num(raw.io_read_bps),
    io_write_bps: num(raw.io_write_bps),
    fd_count: num(raw.fd_count),
    oom_score: num(raw.oom_score, -1),
    nice: num(raw.nice),
    prio: num(raw.prio),
    cgroup: str(raw.cgroup),
    unit: str(raw.unit),
    container: str(raw.container),
    ports: ports(raw.ports),
    kernel: raw.kernel === true
  }
}

// Structural failures throw so a wrong shape cannot be drawn as an empty host.
// It is not empty: it is unreadable, and the screen has to say which.
export function parseProcessSnapshot(raw: unknown): ProcessSnapshot {
  if (!isRecord(raw)) throw new Error('processes: not a JSON object')
  if (raw.processes !== undefined && !Array.isArray(raw.processes)) {
    throw new Error('processes: `processes` is not a list')
  }
  if (raw.stats !== undefined && raw.stats !== null && !isRecord(raw.stats)) {
    throw new Error('processes: `stats` is not an object')
  }

  const processes = (Array.isArray(raw.processes) ? raw.processes : [])
    .map(parseRow)
    .filter((row): row is ProcessRow => row != null)
  const stats = parseStats(raw.stats)
  // Absent flag: only a snapshot carrying rows or stats is a collected one.
  // An empty payload is the "not collected" state, never an idle machine.
  const explicit = typeof raw.processes_collected === 'boolean' ? raw.processes_collected : null

  return {
    processes,
    stats,
    collected_at: str(raw.collected_at),
    // Both gates fail closed: an agent that never learned it may signal must
    // not produce an enabled button.
    io_available: raw.io_available === true,
    signals_enabled: raw.signals_enabled === true,
    collected: explicit ?? (stats != null || processes.length > 0)
  }
}

function isEventType(value: unknown): value is ProcessEventType {
  return value === 'started' || value === 'exited' || value === 'oom_killed' || value === 'restarted'
}

function parseEvent(raw: unknown): ProcessEvent | null {
  if (!isRecord(raw)) return null
  if (!isEventType(raw.type)) return null
  const t = num(raw.t, 0)
  if (t <= 0) return null
  const event: ProcessEvent = { t, type: raw.type, pid: num(raw.pid), name: str(raw.name) }
  if (typeof raw.exit_code === 'number' && Number.isFinite(raw.exit_code)) event.exit_code = raw.exit_code
  return event
}

function parsePoint(raw: unknown): ProcessHistory['series'][number]['points'][number] | null {
  if (!Array.isArray(raw) || raw.length < 3) return null
  const [t, cpu, rss] = raw as unknown[]
  if (typeof t !== 'number' || !Number.isFinite(t) || t <= 0) return null
  if (typeof cpu !== 'number' || !Number.isFinite(cpu)) return null
  if (typeof rss !== 'number' || !Number.isFinite(rss)) return null
  return [t, cpu, rss]
}

function parseSeries(raw: unknown): ProcessHistorySeries | null {
  if (!isRecord(raw)) return null
  const points = (Array.isArray(raw.points) ? raw.points : [])
    .map(parsePoint)
    .filter((point): point is ProcessHistorySeries['points'][number] => point != null)
  const name = str(raw.name)
  if (!name) return null
  return { name, points }
}

function parseBucket(raw: unknown): ProcessHistoryBucket | null {
  if (!isRecord(raw)) return null
  const t = num(raw.t, 0)
  if (t <= 0) return null
  const top = (Array.isArray(raw.top) ? raw.top : [])
    .filter((entry): entry is Record<string, unknown> => isRecord(entry))
    .map((entry) => ({ name: str(entry.name), cpu: num(entry.cpu) }))
    .filter((entry) => entry.name !== '')
  return { t, top }
}

export function parseProcessHistory(raw: unknown): ProcessHistory {
  if (!isRecord(raw)) throw new Error('processes/history: not a JSON object')
  const series = (Array.isArray(raw.series) ? raw.series : [])
    .map(parseSeries)
    .filter((entry): entry is ProcessHistorySeries => entry != null)
  const events = (Array.isArray(raw.events) ? raw.events : [])
    .map(parseEvent)
    .filter((entry): entry is ProcessEvent => entry != null)
    .sort((a, b) => a.t - b.t)
  const buckets = (Array.isArray(raw.buckets) ? raw.buckets : [])
    .map(parseBucket)
    .filter((entry): entry is ProcessHistoryBucket => entry != null)
    .sort((a, b) => a.t - b.t)
  return { series, events, buckets }
}

// ---- IO presence ---------------------------------------------------------

// The kernel never records file IO for a kernel thread or a reaped process, and
// /proc/<pid>/io is unreadable without the right privileges. All of those are
// "—"; a readable zero stays "0 B/s".
export function ioText(row: ProcessRow, ioAvailable: boolean): string {
  if (!ioAvailable || row.kernel || row.state === 'Z') return '—'
  return `${formatRate(row.io_read_bps)} / ${formatRate(row.io_write_bps)}`
}

// ---- Sorting -------------------------------------------------------------

export type SortKey = 'cpu' | 'rss' | 'io' | 'pid' | 'name' | 'threads' | 'uptime'
export type SortDir = 'asc' | 'desc'

export const SORT_KEYS: SortKey[] = ['cpu', 'rss', 'io', 'pid', 'name', 'threads', 'uptime']

function sortValue(row: ProcessRow, key: SortKey): number | string {
  switch (key) {
    case 'rss':
      return row.rss_bytes
    case 'io':
      return row.io_read_bps + row.io_write_bps
    case 'pid':
      return row.pid
    case 'name':
      return row.name.toLowerCase()
    case 'threads':
      return row.threads
    case 'uptime':
      return row.uptime_seconds
    case 'cpu':
    default:
      return row.cpu_percent
  }
}

export function sortRows(rows: readonly ProcessRow[], key: SortKey, dir: SortDir): ProcessRow[] {
  const factor = dir === 'asc' ? 1 : -1
  return [...rows].sort((a, b) => {
    const left = sortValue(a, key)
    const right = sortValue(b, key)
    if (typeof left === 'string' || typeof right === 'string') {
      const order = String(left).localeCompare(String(right))
      if (order !== 0) return order * factor
    } else if (left !== right) {
      return (left - right) * factor
    }
    // Stable tie-break so two rows with the same value never swap places
    // between polls, and a parent never jumps below its child on a tie.
    return a.pid - b.pid
  })
}

// ---- Filtering -----------------------------------------------------------

export type ProcessFilter = {
  search: string
  states: ProcessState[]
  hideKernel: boolean
  listeningOnly: boolean
  /** Names present in a history heatmap cell the user clicked, or null. */
  windowNames: string[] | null
}

export const EMPTY_FILTER: ProcessFilter = {
  search: '',
  states: [],
  hideKernel: true,
  listeningOnly: false,
  windowNames: null
}

function matchesSearch(row: ProcessRow, needle: string): boolean {
  if (!needle) return true
  return (
    String(row.pid).includes(needle) ||
    row.name.toLowerCase().includes(needle) ||
    row.cmd.toLowerCase().includes(needle) ||
    row.user.toLowerCase().includes(needle) ||
    row.unit.toLowerCase().includes(needle) ||
    row.container.toLowerCase().includes(needle)
  )
}

export function filterRows(rows: readonly ProcessRow[], filter: ProcessFilter): ProcessRow[] {
  const needle = filter.search.trim().toLowerCase()
  const states = new Set(filter.states)
  return rows.filter((row) => {
    if (filter.hideKernel && row.kernel) return false
    if (states.size > 0 && !states.has(row.state)) return false
    if (filter.listeningOnly && row.ports.length === 0) return false
    if (filter.windowNames && !filter.windowNames.includes(row.name)) return false
    return matchesSearch(row, needle)
  })
}

// ---- Flat table and ppid tree, one renderer ------------------------------

export type TableEntry = { row: ProcessRow; depth: number; hasChildren: boolean; expanded: boolean }

export type TableOptions = {
  mode: 'flat' | 'tree'
  sortKey: SortKey
  sortDir: SortDir
  expanded: ReadonlySet<number>
}

export function childCounts(rows: readonly ProcessRow[]): Map<number, number> {
  const counts = new Map<number, number>()
  for (const row of rows) counts.set(row.ppid, (counts.get(row.ppid) ?? 0) + 1)
  return counts
}

export function childrenOf(rows: readonly ProcessRow[], pid: number): ProcessRow[] {
  return sortRows(
    rows.filter((row) => row.ppid === pid),
    'cpu',
    'desc'
  )
}

// Both layouts share this walk. Flat keeps the sorted order and splices an
// expanded row's children in below it; Tree starts at every row whose parent is
// not in the sample (an orphan is a root, not a hidden process) and descends.
export function buildTable(rows: readonly ProcessRow[], options: TableOptions): TableEntry[] {
  const sorted = sortRows(rows, options.sortKey, options.sortDir)
  const present = new Set(rows.map((row) => row.pid))
  const children = new Map<number, ProcessRow[]>()
  for (const row of sorted) {
    const list = children.get(row.ppid)
    if (list) list.push(row)
    else children.set(row.ppid, [row])
  }

  const out: TableEntry[] = []
  const seen = new Set<number>()

  const emit = (row: ProcessRow, depth: number) => {
    if (seen.has(row.pid)) return
    seen.add(row.pid)
    const kids = children.get(row.pid) ?? []
    const isExpanded = kids.length > 0 && options.expanded.has(row.pid)
    out.push({ row, depth, hasChildren: kids.length > 0, expanded: isExpanded })
    if (isExpanded) for (const child of kids) emit(child, depth + 1)
  }

  const roots =
    options.mode === 'tree' ? sorted.filter((row) => !present.has(row.ppid)) : sorted
  for (const row of roots) emit(row, 0)

  return out
}

// ---- Signal gate ---------------------------------------------------------

// Mirrors the agent guard (§3). Returns null when nothing local blocks the
// signal; the agent is still the authority.
export function signalBlockReason(row: ProcessRow, snapshot: ProcessSnapshot): string | null {
  if (!snapshot.signals_enabled) return 'signals off'
  if (row.pid <= 1) return 'pid 1'
  if (row.kernel) return 'kernel thread'
  if (row.state === 'Z') return 'zombie'
  if (PROTECTED_NAMES.has(row.name) || row.name.startsWith('containerd-shim')) return 'protected'
  if (PROTECTED_UNITS.has(row.unit)) return 'protected'
  return null
}

export function canSignal(row: ProcessRow, snapshot: ProcessSnapshot): boolean {
  return signalBlockReason(row, snapshot) == null
}

// ---- Formatters ----------------------------------------------------------

export function formatUptime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '<1m'
  if (seconds < 60) return '<1m'
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86_400) {
    const hours = Math.floor(seconds / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    return minutes ? `${hours}h ${minutes}m` : `${hours}h`
  }
  const days = Math.floor(seconds / 86_400)
  const hours = Math.floor((seconds % 86_400) / 3600)
  return hours ? `${days}d ${hours}h` : `${days}d`
}

export function formatCpuPercent(value: number): string {
  return `${value.toFixed(1)}%`
}

export function memoryShare(rssBytes: number, totalBytes: number): number {
  if (!totalBytes || totalBytes <= 0) return 0
  return Math.min(100, Math.max(0, (rssBytes / totalBytes) * 100))
}

// ---- History -------------------------------------------------------------

export type Heatmap = {
  names: string[]
  rows: { name: string; cells: (number | null)[] }[]
  max: number
}

// Rows are the busiest names in the window, columns are the server's buckets.
// A name with no CPU in a bucket is a gap (null), never a zero: a process that
// was not running did not use nothing, it was not there.
export function buildHeatmap(buckets: readonly ProcessHistoryBucket[], limit = 8): Heatmap {
  const peaks = new Map<string, number>()
  for (const bucket of buckets) {
    for (const entry of bucket.top) {
      peaks.set(entry.name, Math.max(peaks.get(entry.name) ?? 0, entry.cpu))
    }
  }
  const names = [...peaks.entries()]
    .sort((a, b) => (b[1] - a[1]) || a[0].localeCompare(b[0]))
    .slice(0, limit)
    .map(([name]) => name)

  let max = 0
  const rows = names.map((name) => {
    const cells = buckets.map((bucket) => {
      const hits = bucket.top.filter((entry) => entry.name === name)
      if (hits.length === 0) return null
      const value = Math.max(...hits.map((entry) => entry.cpu))
      max = Math.max(max, value)
      return value
    })
    return { name, cells }
  })

  return { names, rows, max }
}

// Event markers positioned on the same linear time domain the plots use, so a
// spike and the event that caused it line up. Events outside the window are
// dropped rather than pinned to an edge.
export function eventMarkers(
  events: readonly ProcessEvent[],
  from: number,
  to: number
): { event: ProcessEvent; left: number }[] {
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return []
  const span = to - from
  return events
    .filter((event) => event.t >= from / 1000 && event.t <= to / 1000)
    .sort((a, b) => a.t - b.t)
    .map((event) => ({ event, left: ((event.t * 1000 - from) / span) * 100 }))
}

// One label per lifecycle event, one word where one word is enough.
export const EVENT_LABELS: Record<ProcessEventType, string> = {
  started: 'started',
  exited: 'exited',
  oom_killed: 'oom killed',
  restarted: 'restarted'
}

// The saved views the toolbar offers. Each is a filter preset, not a separate
// query, so it can never disagree with the table below it.
export type SavedView = { id: string; label: string; filter: Partial<ProcessFilter> }

export const SAVED_VIEWS: SavedView[] = [
  { id: 'web', label: 'web', filter: { search: 'nginx', states: [] } },
  { id: 'zombies', label: 'zombies', filter: { states: ['Z'], hideKernel: false } },
  { id: 'topio', label: 'top IO', filter: { search: '', listeningOnly: true } }
]
