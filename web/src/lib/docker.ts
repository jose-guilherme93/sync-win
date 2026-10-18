// Docker screen data shapes and pure helpers.
//
// The API returns the same container twice in two slightly different shapes:
// the telemetry sample (and the cached state built from it) uses `name`, while
// a `list` command result from the agent uses `names`. Both also differ from
// the Docker Engine JSON in optional fields. Everything the UI renders goes
// through `normalizeContainer` so the components never branch on the source.

export type DockerPort = {
  ip?: string
  private_port: number
  public_port?: number
  type: string
}

export type DockerMount = {
  type: string
  source: string
  destination: string
  rw: boolean
  name?: string
}

export type DockerContainer = {
  id: string
  name: string
  image: string
  state: string
  status: string
  ports: DockerPort[]
  mounts: DockerMount[]
  created_at: number
}

export type DockerInfo = {
  version?: string
  total?: number
  running?: number
  stopped?: number
  paused?: number
  images?: number
  driver?: string
  ncpu?: number
}

export type DockerState = {
  available: boolean
  containers: DockerContainer[]
  info: DockerInfo | null
}

export type DockerStats = {
  cpu_usage_percent: number
  memory_usage_bytes: number
  memory_limit_bytes: number
  memory_percent: number
  network_rx_bytes: number
  network_tx_bytes: number
  block_read_bytes: number
  block_write_bytes: number
  pids: number
}

export type DockerComposeFile = {
  path: string
  content: string
  size_bytes: number
}

export type DockerCommandResult = {
  id?: string
  request_id?: string
  status: 'completed' | 'failed'
  message?: string
}

export type DockerContainerCounts = {
  total: number
  running: number
  stopped: number
  paused: number
}

export type DockerStateTone = 'ok' | 'warn' | 'crit' | 'idle'

// The four states the list can be narrowed to. `stopped` is a derived bucket:
// Docker has exited, dead, created and removing, and none of them is running.
export type DockerStatusFilter = 'all' | 'running' | 'paused' | 'stopped'

function asString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function asNumber(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : 0
  }
  return 0
}

function normalizePort(raw: any): DockerPort | null {
  if (!raw || typeof raw !== 'object') return null
  const privatePort = asNumber(raw.private_port)
  if (!privatePort) return null
  return {
    private_port: privatePort,
    public_port: asNumber(raw.public_port) || undefined,
    ip: asString(raw.ip) || undefined,
    type: asString(raw.type) || 'tcp'
  }
}

function normalizeMount(raw: any): DockerMount | null {
  if (!raw || typeof raw !== 'object') return null
  const destination = asString(raw.destination)
  if (!destination) return null
  return {
    type: asString(raw.type) || 'bind',
    source: asString(raw.source),
    destination,
    rw: raw.rw !== false,
    name: asString(raw.name) || undefined
  }
}

// Accepts either API shape and returns null for entries that cannot be acted
// on. A container without an id would render a row whose buttons address
// nothing, so it is dropped instead.
export function normalizeContainer(raw: any): DockerContainer | null {
  if (!raw || typeof raw !== 'object') return null
  const id = asString(raw.id)
  if (!id) return null

  const names: string[] = Array.isArray(raw.names) ? raw.names : []
  const firstName = names.length > 0 ? asString(names[0]) : ''
  const name = (asString(raw.name) || firstName).replace(/^\//, '') || id.slice(0, 12)

  // Docker reports one entry per host address, so a mapping published on both
  // IPv4 and IPv6 arrives twice. The dashboard shows the host-to-container
  // mapping, not the bind addresses, so keep the first of each mapping.
  const ports: DockerPort[] = []
  const seenPorts = new Set<string>()
  for (const item of Array.isArray(raw.ports) ? raw.ports : []) {
    const port = normalizePort(item)
    if (!port) continue
    const key = `${port.private_port}/${port.public_port || 0}/${port.type}`
    if (seenPorts.has(key)) continue
    seenPorts.add(key)
    ports.push(port)
  }

  const mounts: DockerMount[] = []
  for (const item of Array.isArray(raw.mounts) ? raw.mounts : []) {
    const mount = normalizeMount(item)
    if (mount) mounts.push(mount)
  }

  return {
    id,
    name,
    image: asString(raw.image) || 'unknown image',
    state: (asString(raw.state) || 'unknown').toLowerCase(),
    status: asString(raw.status),
    ports,
    mounts,
    created_at: asNumber(raw.created_at)
  }
}

export function normalizeContainers(raw: unknown): DockerContainer[] {
  if (!Array.isArray(raw)) return []
  const containers: DockerContainer[] = []
  for (const item of raw) {
    const container = normalizeContainer(item)
    if (container) containers.push(container)
  }
  return containers
}

// Parses the message of a completed `list` command. A malformed body is an
// error, not an empty device: showing "no containers" for a decode failure is
// exactly the absent-versus-broken confusion the screen must avoid.
export function parseContainerList(message: string | undefined): DockerContainer[] {
  if (!message || !message.trim()) return []
  let parsed: unknown
  try {
    parsed = JSON.parse(message)
  } catch {
    throw new Error('The device returned an unreadable container list')
  }
  if (!Array.isArray(parsed)) throw new Error('The device returned an unexpected container list')
  return normalizeContainers(parsed)
}

export function normalizeDockerState(raw: any): DockerState {
  if (!raw || typeof raw !== 'object') {
    return { available: true, containers: [], info: null }
  }
  return {
    // Only an explicit `false` means Docker is missing. An older server that
    // omits the field must not hide the whole screen.
    available: raw.available !== false,
    containers: normalizeContainers(raw.containers),
    info: raw.info && typeof raw.info === 'object' ? raw.info : null
  }
}

export function containerState(container: DockerContainer): Exclude<DockerStatusFilter, 'all'> {
  if (container.state === 'running') return 'running'
  if (container.state === 'paused') return 'paused'
  // Everything else — exited, dead, created, removing, restarting — is not
  // running, and the filter has exactly one bucket for that.
  return 'stopped'
}

export function stateTone(state: string): DockerStateTone {
  switch (state) {
    case 'running':
      return 'ok'
    case 'paused':
    case 'restarting':
      return 'warn'
    case 'dead':
      return 'crit'
    default:
      return 'idle'
  }
}

export function containerCounts(containers: DockerContainer[]): DockerContainerCounts {
  const counts: DockerContainerCounts = { total: containers.length, running: 0, stopped: 0, paused: 0 }
  for (const container of containers) {
    if (container.state === 'running') counts.running += 1
    else if (container.state === 'paused') counts.paused += 1
    else counts.stopped += 1
  }
  return counts
}

export function filterContainers(
  containers: DockerContainer[],
  query: string,
  status: DockerStatusFilter
): DockerContainer[] {
  const needle = query.trim().toLowerCase()
  return containers.filter((container) => {
    if (status !== 'all' && containerState(container) !== status) return false
    if (!needle) return true
    const haystack = [
      container.name,
      container.image,
      container.id,
      container.state,
      container.status,
      ...container.ports.map(portLabel)
    ]
      .join(' ')
      .toLowerCase()
    return haystack.includes(needle)
  })
}

export function shortId(id: string): string {
  return id.slice(0, 12)
}

// Docker's port object is container-first, so the arrow points at the port
// inside the container: "8080 → 80/tcp" reads host to container.
export function portLabel(port: DockerPort): string {
  const container = `${port.private_port}/${port.type || 'tcp'}`
  return port.public_port ? `${port.public_port} → ${container}` : container
}

export function portsSummary(container: DockerContainer, limit = 2): string {
  if (container.ports.length === 0) return ''
  const shown = container.ports.slice(0, limit).map(portLabel).join(', ')
  const rest = container.ports.length - limit
  return rest > 0 ? `${shown} +${rest}` : shown
}

// Parses the message of a completed compose_read command. The agent returns an
// array of {path, content, size_bytes}; anything else is a broken reply, which
// must not be rendered as "no compose files".
export function parseComposeFiles(message: string | undefined): DockerComposeFile[] {
  if (!message || !message.trim()) return []
  let parsed: unknown
  try {
    parsed = JSON.parse(message)
  } catch {
    throw new Error('The device returned an unreadable compose file list')
  }
  // Older agents marshal a nil slice as `null`. That means "none found", and
  // treating it as a broken reply would show an error next to an empty list.
  if (parsed == null) return []
  if (!Array.isArray(parsed)) throw new Error('The device returned an unexpected compose file list')
  const files: DockerComposeFile[] = []
  for (const item of parsed) {
    if (!item || typeof item !== 'object') continue
    const path = asString((item as any).path)
    if (!path) continue
    files.push({
      path,
      content: typeof (item as any).content === 'string' ? (item as any).content : '',
      size_bytes: asNumber((item as any).size_bytes)
    })
  }
  return files
}

// The agent answers a request with a status and, on failure, its reason. Two
// reasons are common enough to deserve a sentence instead of the terse wire
// string; anything else travels through unchanged.
export function friendlyDockerError(message: string | undefined, action = ''): string {
  const raw = (message || '').trim()
  const doing = action ? `Could not ${action}: ` : ''
  if (!raw) return `${doing}the device returned no reason`
  if (raw.includes('command disabled by local policy')) {
    return `${doing}Docker ${action || 'command'} is disabled by this device's local policy.`
  }
  if (raw.includes('docker not available')) {
    return 'Docker is not available on this device. The agent may need access to the Docker socket.'
  }
  return `${doing}${raw}`
}

export function statsFromMessage(message: string | undefined): DockerStats {
  if (!message || !message.trim()) throw new Error('The device returned no container statistics')
  let parsed: any
  try {
    parsed = JSON.parse(message)
  } catch {
    throw new Error('The device returned unreadable container statistics')
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('The device returned unexpected container statistics')
  }
  return {
    cpu_usage_percent: asNumber(parsed.cpu_usage_percent),
    memory_usage_bytes: asNumber(parsed.memory_usage_bytes),
    memory_limit_bytes: asNumber(parsed.memory_limit_bytes),
    memory_percent: asNumber(parsed.memory_percent),
    network_rx_bytes: asNumber(parsed.network_rx_bytes),
    network_tx_bytes: asNumber(parsed.network_tx_bytes),
    block_read_bytes: asNumber(parsed.block_read_bytes),
    block_write_bytes: asNumber(parsed.block_write_bytes),
    pids: asNumber(parsed.pids)
  }
}
