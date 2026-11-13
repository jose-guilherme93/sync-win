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
  // A server built before per-process IO history sends three values. That is
  // absent data, not a zero: null keeps the chart from drawing a flat line at
  // the bottom, and the UI says the metric is not recorded instead.
  const ioValue = (value: unknown): number | null =>
    typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
  return { t, cpu, rss, ioRead: ioValue(raw[3]), ioWrite: ioValue(raw[4]) }
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
  // The toolbar's three selects. Empty means "all". The API has no facet
  // endpoint, so the options come from the rows it already returned and the
  // narrowing happens here; a select is never drawn without values behind it.
  user: string
  unit: string
  container: string
  /** Names present in a history heatmap cell the user clicked, or null. */
  windowNames: string[] | null
}

export const EMPTY_FILTER: ProcessFilter = {
  search: '',
  states: [],
  hideKernel: true,
  listeningOnly: false,
  user: '',
  unit: '',
  container: '',
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
    if (filter.user && row.user !== filter.user) return false
    if (filter.unit && row.unit !== filter.unit) return false
    if (filter.container && row.container !== filter.container) return false
    if (filter.windowNames && !filter.windowNames.includes(row.name)) return false
    return matchesSearch(row, needle)
  })
}

// Options for the three selects, taken from the rows the agent just reported.
// A value nobody has would be a control that can only empty the table.
export function filterOptions(rows: readonly ProcessRow[]): {
  users: string[]
  units: string[]
  containers: string[]
} {
  const unique = (values: string[]) => [...new Set(values.filter((value) => value !== ''))].sort((a, b) => a.localeCompare(b))
  return {
    users: unique(rows.map((row) => row.user)),
    units: unique(rows.map((row) => row.unit)),
    containers: unique(rows.map((row) => row.container))
  }
}

// How many rows each state chip would keep, counted with every other filter
// applied. The number is the promise the chip makes when it is clicked.
export function stateCounts(rows: readonly ProcessRow[], filter: ProcessFilter): Record<ProcessState, number> {
  const withoutStates = filterRows(rows, { ...filter, states: [] })
  const counts: Record<ProcessState, number> = { R: 0, S: 0, D: 0, Z: 0, T: 0, I: 0 }
  for (const row of withoutStates) counts[row.state] += 1
  return counts
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
    options.mode === 'tree'
      ? sorted.filter((row) => !present.has(row.ppid) || row.ppid === row.pid)
      : sorted
  for (const row of roots) emit(row, 0)

  return out
}

// The inspector pane's tree: every process, nested, with no collapse state.
// Children inherit their parent's place; a row whose parent is missing (a
// short-lived process, or one the agent did not report) starts a root of its
// own instead of vanishing. The trailing pass covers a ppid cycle, which would
// otherwise emit nothing at all.
export function treeEntries(
  rows: readonly ProcessRow[],
  sortKey: SortKey = 'cpu',
  sortDir: SortDir = 'desc'
): TableEntry[] {
  const sorted = sortRows(rows, sortKey, sortDir)
  const present = new Set(rows.map((row) => row.pid))
  const children = new Map<number, ProcessRow[]>()
  for (const row of sorted) {
    const list = children.get(row.ppid)
    if (list) list.push(row)
    else children.set(row.ppid, [row])
  }

  const out: TableEntry[] = []
  const seen = new Set<number>()

  const walk = (row: ProcessRow, depth: number) => {
    if (seen.has(row.pid)) return
    seen.add(row.pid)
    const kids = children.get(row.pid) ?? []
    out.push({ row, depth, hasChildren: kids.length > 0, expanded: true })
    for (const child of kids) walk(child, depth + 1)
  }

  for (const row of sorted) if (!present.has(row.ppid) || row.ppid === row.pid) walk(row, 0)
  for (const row of sorted) if (!seen.has(row.pid)) walk(row, 0)

  return out
}

// The ancestry walk for the detail pane, oldest first. It stops at the first
// pid it has already seen, so a reused ppid cannot loop forever.
export function parentChain(rows: readonly ProcessRow[], row: ProcessRow, limit = 6): ProcessRow[] {
  const byPid = new Map(rows.map((entry) => [entry.pid, entry]))
  const chain: ProcessRow[] = []
  const seen = new Set<number>()
  let current: ProcessRow | null = row
  while (current && seen.size < limit && !seen.has(current.pid)) {
    seen.add(current.pid)
    chain.unshift(current)
    if (current.ppid === current.pid) break
    current = byPid.get(current.ppid) ?? null
  }
  return chain
}

export type ProcessGroup = { label: string; rows: ProcessRow[]; cpu: number; rss: number }

// Grouped layouts keep the same rows and only add a header every few of them;
// the header totals are the sum of what it contains, never a separate query.
export function groupRows(rows: readonly ProcessRow[], by: 'unit' | 'container'): ProcessGroup[] {
  const groups = new Map<string, ProcessRow[]>()
  for (const row of rows) {
    const label = by === 'unit' ? row.unit || 'no unit' : row.container || 'host processes'
    const list = groups.get(label)
    if (list) list.push(row)
    else groups.set(label, [row])
  }
  return [...groups.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([label, groupRows]) => ({
      label: by === 'unit' && label !== 'no unit' ? `${label} (unit)` : label,
      rows: groupRows,
      cpu: groupRows.reduce((total, row) => total + row.cpu_percent, 0),
      rss: groupRows.reduce((total, row) => total + row.rss_bytes, 0)
    }))
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
  if (!Number.isFinite(value)) return '—'
  if (value === 0) return '0.0%'
  if (value < 0.1) return '<0.1%'
  return `${value.toFixed(1)}%`
}

export function memoryShare(rssBytes: number, totalBytes: number): number {
  if (!totalBytes || totalBytes <= 0) return 0
  return Math.min(100, Math.max(0, (rssBytes / totalBytes) * 100))
}

// Clock time of the sample, in the browser's zone. An unset start time is not
// midnight in 1970: it stays absent.
export function formatClock(unixSeconds: number): string {
  if (!Number.isFinite(unixSeconds) || unixSeconds <= 0) return '—'
  return new Date(unixSeconds * 1000).toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

// How long a process has used the CPU, in the shape top prints it.
export function formatCpuTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '0s'
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}:${String(Math.round(seconds % 60)).padStart(2, '0')}`
  if (seconds < 86_400) {
    return `${Math.floor(seconds / 3600)}:${String(Math.floor((seconds % 3600) / 60)).padStart(2, '0')}:${String(
      Math.round(seconds % 60)
    ).padStart(2, '0')}`
  }
  return `${Math.floor(seconds / 86_400)}d ${Math.floor((seconds % 86_400) / 3600)}h`
}

// ---- History -------------------------------------------------------------

// The heatmap is a fixed grid: 288 columns of five minutes is 24 hours, drawn
// as 3 px bars with a 1 px gap. The grid keeps its width even when the device
// has only been reporting for an hour, because a short window drawn wide would
// read as a busy day.
export const HEAT_COLUMN_SECONDS = 300
export const HEAT_COLUMNS = 288
export const HEAT_CELL_WIDTH = 4

export type HeatmapColumn = { from: number; to: number }

export type HeatmapRow = {
  name: string
  /** The single pid behind the name, when there is exactly one. */
  pid: number | null
  cells: (number | null)[]
  /** Σ of each column's average CPU, in core-minutes of a 5-minute bucket. */
  coreMin: number
  peak: number
}

export type Heatmap = {
  columns: HeatmapColumn[]
  rows: HeatmapRow[]
  max: number
}

export type HeatmapOptions = {
  /** Right edge of the grid, unix seconds. Defaults to the end of the newest bucket. */
  end?: number
  columns?: number
  limit?: number
  /** Current pid per name, so a row can show which process it is. */
  pids?: ReadonlyMap<string, number>
}

// A minute bucket's CPU is already an average over that minute; a five-minute
// column is the mean of the minutes it holds, and a minute with no row is a gap
// (the process was not alive), never a zero.
export function buildHeatmap(
  buckets: readonly ProcessHistoryBucket[],
  options: HeatmapOptions = {}
): Heatmap {
  const count = Math.max(1, options.columns ?? HEAT_COLUMNS)
  const step = HEAT_COLUMN_SECONDS
  const newest = buckets.reduce((acc, bucket) => Math.max(acc, bucket.t), 0)
  const end =
    options.end ??
    (newest > 0
      ? Math.ceil((newest + 1) / step) * step
      : Math.ceil(Date.now() / 1000 / step) * step)
  const start = end - count * step

  const sums = new Map<string, number[]>()
  const samples = new Map<string, number[]>()
  for (const bucket of buckets) {
    const column = Math.floor((bucket.t - start) / step)
    if (column < 0 || column >= count) continue
    for (const entry of bucket.top) {
      let sum = sums.get(entry.name)
      let seen = samples.get(entry.name)
      if (!sum || !seen) {
        sum = new Array<number>(count).fill(0)
        seen = new Array<number>(count).fill(0)
        sums.set(entry.name, sum)
        samples.set(entry.name, seen)
      }
      sum[column] += entry.cpu
      seen[column] += 1
    }
  }

  const averages = new Map<string, (number | null)[]>()
  const peaks = new Map<string, number>()
  for (const [name, sum] of sums) {
    const seen = samples.get(name)!
    let peak = 0
    const cells = sum.map((value, index) => {
      if (seen[index] === 0) return null
      const average = value / seen[index]
      peak = Math.max(peak, average)
      return average
    })
    averages.set(name, cells)
    peaks.set(name, peak)
  }

  const ranked = [...peaks.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  // Idle noise: a row whose peak is under 2% of the busiest one says nothing
  // about the window and only widens the grid.
  const top = ranked.length > 0 ? ranked[0][1] : 0
  const limit = options.limit ?? 10
  const names = ranked
    .filter(([, peak], index) => index === 0 || peak >= top * 0.02)
    .slice(0, limit)
    .map(([name]) => name)

  let max = 0
  const rows: HeatmapRow[] = names.map((name) => {
    const cells = averages.get(name) ?? new Array<number | null>(count).fill(null)
    let coreMin = 0
    for (const value of cells) {
      if (value == null) continue
      coreMin += (value / 100) * (step / 60)
      max = Math.max(max, value)
    }
    return {
      name,
      pid: options.pids?.get(name) ?? null,
      cells,
      coreMin,
      peak: peaks.get(name) ?? 0
    }
  })

  const columns: HeatmapColumn[] = Array.from({ length: count }, (_, index) => ({
    from: start + index * step,
    to: start + (index + 1) * step
  }))

  return { columns, rows, max }
}

// The single busiest cell in the heatmap, for the annotation line. Returns null
// when there is no data, so the screen can say "collecting" instead of a zero.
export function busiestCell(
  heatmap: Heatmap
): { value: number; name: string; index: number; from: number; to: number } | null {
  let best: { value: number; name: string; index: number } | null = null
  for (const row of heatmap.rows) {
    row.cells.forEach((cell, index) => {
      if (cell == null) return
      if (!best || cell > best.value) best = { value: cell, name: row.name, index }
    })
  }
  if (!best) return null
  const cell = best as { value: number; name: string; index: number }
  const column = heatmap.columns[cell.index]
  const from = column?.from ?? 0
  return { value: cell.value, name: cell.name, index: cell.index, from, to: column?.to ?? from + 300 }
}

// Ruler labels sit on the bar grid, one every four hours, plus the right edge.
// They are placed in pixels, not percentages: a percentage of a scrolled grid
// drifts away from the bars it is supposed to mark.
export function heatTicks(
  columns: readonly HeatmapColumn[],
  spacingHours = 4
): { column: number; left: number; label: string }[] {
  if (columns.length === 0) return []
  const spacingSeconds = spacingHours * 3600
  const offset = new Date(columns[0].from * 1000).getTimezoneOffset() * 60
  const out: { column: number; left: number; label: string }[] = []
  columns.forEach((column, index) => {
    const local = column.from - offset
    if (local % spacingSeconds !== 0) return
    const date = new Date(column.from * 1000)
    out.push({
      column: index,
      left: index * HEAT_CELL_WIDTH,
      label: date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
    })
  })
  if (out.length >= 2) return out
  // A zone whose offset does not land on the grid still gets a ruler, spaced
  // by columns instead of by the clock.
  out.length = 0
  const spacing = Math.max(1, Math.round(spacingSeconds / HEAT_COLUMN_SECONDS))
  for (let index = 0; index < columns.length; index += spacing) {
    const date = new Date(columns[index].from * 1000)
    out.push({
      column: index,
      left: index * HEAT_CELL_WIDTH,
      label: date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
    })
  }
  return out
}

export function bucketRangeLabel(t: number, seconds = HEAT_COLUMN_SECONDS): string {
  const from = new Date(t * 1000)
  const to = new Date((t + seconds) * 1000)
  const fmt = (d: Date) => d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  return `${fmt(from)}–${fmt(to)}`
}

// Absolute share of capacity, so a cell means the same thing on every row and
// two screenshots are comparable. The square root lifts the long idle tail: a
// host using 0.4% still draws a visible bar instead of a black hole, while the
// order and the 0–95% scale are kept.
// The colour answers "where in this window was the host hottest", so it is
// relative to the busiest cell of the window: an absolute 0-95% ramp leaves an
// idle-but-real host as one faint texture, which is exactly what made the grid
// unreadable. The absolute value stays in the tooltip and in the row summary.
export function heatCellAlpha(value: number, max = 0): number {
  if (!(max > 0)) return 0.5
  const ratio = Math.max(0, Math.min(1, value / max))
  return Math.min(0.96, 0.18 + 0.78 * Math.sqrt(ratio))
}

export function heatCellColor(value: number, max = 0): string {
  return `rgba(96,165,250,${heatCellAlpha(value, max).toFixed(2)})`
}

// A share of capacity in one decimal below ten percent. An idle machine has
// rows at 0.1%, and "peak 0%" would read as no data instead of little use.
export function formatShare(value: number): string {
  if (!Number.isFinite(value)) return '—'
  if (value > 0 && value < 1) return value.toFixed(2)
  if (value < 10) return value.toFixed(1)
  return value.toFixed(0)
}

export function formatCoreMin(value: number): string {
  if (!Number.isFinite(value)) return '—'
  return value > 0 && value < 10 ? value.toFixed(1) : value.toFixed(0)
}

// One marker per notable event: a machine churns through hundreds of
// started/exited pairs an hour, and drawing all of them turns the plot into a
// wall of red lines. OOM kills, restarts and failed exits are what explain a
// spike; everything else stays in the event list.
const NOTABLE_EVENTS: Record<ProcessEventType, boolean> = {
  started: false,
  exited: false,
  oom_killed: true,
  restarted: true
}

export function eventMarkers(
  events: readonly ProcessEvent[],
  from: number,
  to: number,
  limit = 12
): { event: ProcessEvent; left: number }[] {
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return []
  const span = to - from
  const seen = new Set<string>()
  const picked: ProcessEvent[] = []
  for (const event of events) {
    if (event.t < from / 1000 || event.t > to / 1000) continue
    const notable = NOTABLE_EVENTS[event.type] || (event.type === 'exited' && (event.exit_code ?? 0) !== 0)
    if (!notable) continue
    const key = `${Math.floor(event.t / 60)}:${event.type}`
    if (seen.has(key)) continue
    seen.add(key)
    picked.push(event)
  }
  picked.sort((a, b) => a.t - b.t)
  const capped = picked.length > limit ? picked.slice(picked.length - limit) : picked
  return capped.map((event) => ({ event, left: ((event.t * 1000 - from) / span) * 100 }))
}

// A systemd host restarts the same helper every few seconds; unrolled, those
// pairs bury the one OOM kill that matters. Repeated identical events inside
// one window collapse into a count, newest timestamp kept.
export type CollapsedEvent = ProcessEvent & { count: number }

export function collapseEvents(events: readonly ProcessEvent[], windowSeconds = 300): CollapsedEvent[] {
  const groups = new Map<string, CollapsedEvent>()
  for (const event of events) {
    // Same event, same name, same window: one row with a count. Binned by the
    // window so two incidents hours apart never merge into one.
    const key = `${event.type}:${event.name}:${Math.floor(event.t / windowSeconds)}`
    const group = groups.get(key)
    if (group) {
      group.count += 1
      group.t = Math.max(group.t, event.t)
      group.pid = event.pid
      if (event.exit_code != null) group.exit_code = event.exit_code
      continue
    }
    groups.set(key, { ...event, count: 1 })
  }
  return [...groups.values()].sort((a, b) => a.t - b.t)
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
export type SavedView = {
  id: string
  label: string
  filter: Partial<ProcessFilter>
  sort?: { key: SortKey; dir: SortDir }
}

export const SAVED_VIEWS: SavedView[] = [
  { id: 'web', label: 'Web Stack', filter: { search: 'nginx', states: [] } },
  { id: 'zombies', label: 'Zombies', filter: { states: ['Z'], hideKernel: false } },
  { id: 'topio', label: 'Top IO', filter: { search: '', listeningOnly: false }, sort: { key: 'io', dir: 'desc' } }
]
