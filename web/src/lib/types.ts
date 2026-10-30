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
  cpu_clock_mhz?: number
  core_voltage_v?: number
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
  network_health?: NetworkHealth
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
  mac?: string
  mtu?: number
  oper_state?: string
  speed_mbps?: number
  addresses?: string[]
  rx_bytes: number
  tx_bytes: number
  rx_rate?: number
  tx_rate?: number
  rx_packets: number
  tx_packets: number
  rx_errors: number
  tx_errors: number
  rx_dropped?: number
  tx_dropped?: number
  // kind classifies the link: physical, wifi, tunnel, bridge, veth, loopback
  // or virtual. An agent that does not report it leaves it empty.
  kind?: string
  // counted says whether the interface feeds the fleet totals. The container
  // plumbing is reported so it can be seen, but it must never be summed with
  // the real links.
  counted?: boolean
  driver?: string
  duplex?: string
  carrier_changes?: number
  rx_fifo_errors?: number
  tx_fifo_errors?: number
  rx_collisions?: number
  tx_carrier_errors?: number
  multicast?: number
}

// The packet level health of the device stack, as the agent reports it.
export type NetworkHealth = {
  tcp?: TCPStats
  udp?: UDPStats
  sockets?: SocketCounts
  conntrack?: { count: number; max: number }
  collected_at?: string
}

export type TCPStats = {
  active_opens?: number
  passive_opens?: number
  attempt_fails?: number
  estab_resets?: number
  curr_estab?: number
  in_segs?: number
  out_segs?: number
  retrans_segs?: number
  in_errs?: number
  out_rsts?: number
  retrans_per_sec?: number
  retrans_percent?: number
}

export type UDPStats = {
  in_datagrams?: number
  no_ports?: number
  in_errors?: number
  out_datagrams?: number
  rcvbuf_errors?: number
  sndbuf_errors?: number
}

export type SocketCounts = {
  established?: number
  time_wait?: number
  close_wait?: number
  syn_sent?: number
  syn_recv?: number
  fin_wait1?: number
  fin_wait2?: number
  listen?: number
  udp?: number
}

// The slow half of the network view: how the device reaches the network and
// who else is on the segment.
export type NetworkSection = {
  topology?: NetworkTopology
  firewall?: FirewallState
}

export type NetworkTopology = {
  default_gateway?: string
  gateways?: string[]
  dns?: string[]
  search_domains?: string[]
  routes?: NetworkRoute[]
  neighbors?: NetworkNeighbor[]
  wireless?: WirelessLink[]
  tunnels?: TunnelLink[]
  overlays?: OverlayNetwork[]
  collected_at?: string
}

// OverlayNode is one machine in a private overlay network (today Tailscale).
// IPs are overlay addresses, not the physical ones.
export type OverlayNode = {
  hostname: string
  dns_name?: string
  os?: string
  ips?: string[]
  online: boolean
  direct?: boolean
  relay?: string
  exit_node?: boolean
  exit_node_option?: boolean
  tags?: string[]
  last_seen?: string
}

// OverlayNetwork is a private network the device joins through a daemon. An
// unreadable daemon is reported with `error`, which is different from a
// network with no peers.
export type OverlayNetwork = {
  kind: string
  name?: string
  magic_dns?: string
  interface?: string
  state: string
  self?: OverlayNode
  peers?: OverlayNode[]
  error?: string
}

export type NetworkRoute = {
  destination: string
  gateway?: string
  interface: string
  metric?: number
  kind: string
  family: string
}

export type NetworkNeighbor = {
  ip: string
  mac?: string
  interface: string
  state?: string
  family: string
}

export type WirelessLink = {
  interface: string
  signal_dbm?: number
  quality?: number
  noise_dbm?: number
}

export type TunnelLink = {
  interface: string
  kind: string
  state?: string
}

export type FirewallState = {
  ufw?: string
  firewalld?: string
  nftables?: string
}

export type NetworkHistoryPoint = {
  timestamp: string
  rx_rate: number
  tx_rate: number
  rx_max: number
  tx_max: number
  samples: number
}

export type NetworkHistory = {
  device_id: string
  resolution: string
  from: string
  to: string
  interfaces: Record<string, NetworkHistoryPoint[]>
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
  collection_interval_seconds?: number
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
