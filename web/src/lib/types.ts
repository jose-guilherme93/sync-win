// Shapes shared between the shell and the screens.
//
// These were duplicated across App.svelte, SimpleMetrics.svelte and
// DeviceModal.svelte with slightly different fields, which is how the two
// components drifted into formatting the same timestamp two different ways.
// New code imports from here; the legacy components keep their local aliases
// until they are migrated.

export type Status = 'online' | 'stale' | 'offline' | 'error' | 'duplicate'

export type HardwareStats = {
  cpu_usage_percent: number
  cpu_core_usage?: number[]
  cpu_model?: string
  cpu_temperature?: number
  memory_used_bytes: number
  memory_total_bytes: number
  memory_buffers_bytes?: number
  memory_cached_bytes?: number
  swap_used_bytes?: number
  swap_total_bytes?: number
  disk_read_bytes?: number
  disk_write_bytes?: number
  disk_read_rate: number
  disk_write_rate: number
  disk_partitions?: DiskPartition[]
  network_ifaces?: NetworkIface[]
  net_rx_rate?: number
  net_tx_rate?: number
  uptime_seconds: number
  load_average: string
  power_watts: number
  collected_at: string
  architecture?: string
  desktop_environment?: string
  locale?: string
  timezone?: string
  agent_version?: string
  agent_cpu_usage?: number
  agent_memory_bytes?: number
  gpu_temperature_celsius?: number
  battery_percent?: number
  battery_status?: string
  top_cpu_processes?: ProcessInfo[]
  top_mem_processes?: ProcessInfo[]
  docker_available?: boolean
  docker_info?: DockerInfo
  docker_containers?: DockerContainer[]
  lynis_available?: boolean
  lynis_install_cmd?: string
  operating_system?: string
  kernel_version?: string
}

export type DiskPartition = {
  mount: string
  device: string
  total_bytes: number
  used_bytes: number
  free_bytes: number
  used_percent: number
}

export type NetworkIface = {
  name: string
  rx_bytes: number
  tx_bytes: number
  rx_rate?: number
  tx_rate?: number
  rx_packets: number
  tx_packets: number
  rx_errors: number
  tx_errors: number
}

export type ProcessInfo = {
  pid: number
  name: string
  cpu_percent: number
  mem_rss_bytes: number
}

export type DockerInfo = {
  version: string
  total: number
  running: number
  stopped: number
  paused: number
  images: number
  driver: string
  ncpu: number
}

export type DockerContainer = {
  id: string
  name: string
  image: string
  state: string
  status: string
  ports?: string
  cpu_percent?: number
  mem_usage?: number
  mem_limit?: number
}

export type Device = {
  id: string
  hostname: string
  user_id: string
  status: string
  last_seen_at: string
  last_sync_at: string
  created_at?: string
  hardware?: HardwareStats
  apps?: AppInfo[]
  app_count: number
  preference_count: number
  saves_count: number
  saves_size_bytes: number
  saves_last_synced_at?: string
  last_error?: string
  last_error_at?: string
  tags?: string[]
  display_name?: string
}

export type AppInfo = { name: string; version: string; source: string; path?: string }

// Client-side status fallback. The API reports a status, but it can lag the
// last_seen_at by a poll interval, so the UI recomputes from the timestamp and
// only trusts an explicit error/duplicate.
export function computedStatus(device: Pick<Device, 'status' | 'last_seen_at'>): Status {
  if (device.status === 'error' || device.status === 'duplicate') return device.status
  const seen = Date.parse(device.last_seen_at)
  if (!Number.isFinite(seen)) return 'offline'
  const age = (Date.now() - seen) / 1000
  if (age > 300) return 'offline'
  if (age > 30) return 'stale'
  return 'online'
}

// Primary label for a device: a user-chosen display name wins over hostname.
export function deviceLabel(device: Pick<Device, 'hostname' | 'display_name'>): string {
  return device.display_name?.trim() || device.hostname
}

export function deviceTags(device: Pick<Device, 'tags'>): string[] {
  return device.tags ?? []
}

export function memoryPercent(hw?: HardwareStats): number {
  if (!hw?.memory_total_bytes) return 0
  return Math.min(100, Math.max(0, ((hw.memory_used_bytes || 0) / hw.memory_total_bytes) * 100))
}

export function swapPercent(hw?: HardwareStats): number {
  if (!hw?.swap_total_bytes) return 0
  return Math.min(100, Math.max(0, ((hw.swap_used_bytes || 0) / hw.swap_total_bytes) * 100))
}
