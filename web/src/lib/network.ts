// Helpers for the Network screen.
//
// The screen shows what an operator needs to answer three questions: is the
// link healthy, what is using the bandwidth, and what is reachable from
// outside. The helpers here hold the rules that answer those questions, so
// the screen only has to render them and the tests can cover the rules
// without a browser.

import type { NetworkHealth, NetworkIface, NetworkNeighbor } from './types'
import type { OpenPort } from './ports'

// The link classes the agent reports. An interface with no kind comes from an
// older agent, which only reported the real links.
export type LinkKind = 'physical' | 'wifi' | 'tunnel' | 'bridge' | 'veth' | 'loopback' | 'virtual'

// KIND_LABELS is what the screen prints next to the interface name.
const KIND_LABELS: Record<string, string> = {
  physical: 'physical',
  wifi: 'wireless',
  tunnel: 'tunnel',
  bridge: 'bridge',
  veth: 'container',
  loopback: 'loopback',
  virtual: 'virtual'
}

export function interfaceKindLabel(kind?: string): string {
  if (!kind) return 'link'
  return KIND_LABELS[kind] ?? kind
}

// The container plumbing is reported so an operator can see which bridge is
// carrying the traffic, but it is not part of the fleet totals. An agent that
// does not report the flag predates it, and every interface it sends is a
// real one.
export function isCounted(iface: NetworkIface): boolean {
  return iface.counted !== false
}

export function countedInterfaces(ifaces: NetworkIface[]): NetworkIface[] {
  return ifaces.filter(isCounted)
}

export function uncountedInterfaces(ifaces: NetworkIface[]): NetworkIface[] {
  return ifaces.filter((iface) => !isCounted(iface))
}

// How a socket is exposed. A socket bound to every address answers on the
// network; one bound to the loopback answers only on the machine.
export type Exposure = 'all' | 'specific' | 'loopback' | 'unknown'

export function exposureOf(port: OpenPort): Exposure {
  const address = (port.local_address ?? '').trim()
  if (address === '') return 'all'
  if (address === '*' || address === '0.0.0.0' || address === '::' || address === '[::]') return 'all'
  if (address === '127.0.0.1' || address === '::1' || address === '[::1]') return 'loopback'
  if (address.startsWith('127.')) return 'loopback'
  return 'specific'
}

// exposedPorts are the sockets something on the network can reach.
export function exposedPorts(ports: OpenPort[]): OpenPort[] {
  return ports.filter((port) => {
    const exposure = exposureOf(port)
    return exposure === 'all' || exposure === 'specific'
  })
}

export type ExposureSummary = {
  total: number
  exposed: number
  loopback: number
}

export function summarizeExposure(ports: OpenPort[]): ExposureSummary {
  const summary: ExposureSummary = { total: ports.length, exposed: 0, loopback: 0 }
  for (const port of ports) {
    const exposure = exposureOf(port)
    if (exposure === 'all' || exposure === 'specific') summary.exposed++
    if (exposure === 'loopback') summary.loopback++
  }
  return summary
}

// linkTone grades a link by its error counters. A dropped packet is a hint of
// congestion; an error is a broken path.
export function linkTone(iface: NetworkIface): 'ok' | 'warn' | 'crit' {
  const errors = (iface.rx_errors || 0) + (iface.tx_errors || 0)
  const drops = (iface.rx_dropped || 0) + (iface.tx_dropped || 0)
  const carrier = (iface.tx_carrier_errors || 0) + (iface.rx_collisions || 0) + (iface.rx_fifo_errors || 0)
  if (errors > 0 || carrier > 0) return 'crit'
  if (drops > 0) return 'warn'
  return 'ok'
}

// retransTone grades the retransmission share. A couple of percent is normal
// on a busy link; more than ten means the path is losing packets.
export function retransTone(percent?: number): 'ok' | 'warn' | 'crit' {
  if (percent === undefined || percent === null) return 'ok'
  if (percent >= 10) return 'crit'
  if (percent >= 2) return 'warn'
  return 'ok'
}

// conntrackUsage is the share of the connection table in use. The table
// filling up is the usual reason a Linux host starts dropping connections.
export function conntrackUsage(count?: number, max?: number): number {
  if (!count || !max || max <= 0) return 0
  return Math.min(100, Math.round((count / max) * 1000) / 10)
}

// signalTone grades a wireless signal in dBm. -70 is the usual floor for a
// usable link, -85 is where it starts to fall apart.
export function signalTone(dbm?: number): 'ok' | 'warn' | 'crit' {
  if (dbm === undefined || dbm === null || dbm === 0) return 'ok'
  if (dbm >= -67) return 'ok'
  if (dbm >= -80) return 'warn'
  return 'crit'
}

// A small table of the OUIs that show up on real fleets. It is a prefix
// table, not a registry: the screen says "vendor" and the dashboard says
// nothing stronger than that.
const OUI_VENDORS: Record<string, string> = {
  '00:0c:29': 'VMware',
  '00:50:56': 'VMware',
  '08:00:27': 'VirtualBox',
  '52:54:00': 'QEMU/KVM',
  '00:15:5d': 'Hyper-V',
  '00:1b:21': 'Xen',
  '02:42:ac': 'Docker',
  'b8:27:eb': 'Raspberry Pi',
  'dc:a6:32': 'Raspberry Pi',
  'e4:5f:01': 'Raspberry Pi',
  '26:8a:e7': 'Raspberry Pi',
  '00:03:93': 'Apple',
  'f0:18:98': 'Apple',
  '3c:22:fb': 'Apple',
  '8c:85:90': 'Apple',
  '00:1b:63': 'Apple',
  '00:23:ae': 'Dell',
  'b8:2a:72': 'Dell',
  'f8:bc:12': 'Dell',
  '00:14:22': 'Dell',
  'd8:9e:f3': 'Dell',
  '00:26:b9': 'Dell',
  '3c:d9:2b': 'Supermicro',
  '00:1e:0f': 'Supermicro',
  '00:25:b3': 'Supermicro',
  '0c:42:45': 'Supermicro',
  '00:0f:b0': 'Hewlett Packard',
  '00:1a:a0': 'Hewlett Packard',
  '00:1c:42': 'Parallels',
  '1c:1b:0d': 'Intel',
  '3c:97:0e': 'Intel',
  '8c:16:45': 'Intel',
  'a4:bb:6d': 'Intel',
  '34:64:9a': 'Intel',
  '6c:2b:59': 'Intel',
  'b4:96:91': 'Intel',
  '00:1f:3b': 'Intel',
  'e4:54:e8': 'Intel',
  '94:65:2d': 'Intel',
  '54:e1:ad': 'Intel',
  'f0:db:f8': 'Intel',
  '2c:f0:5d': 'Intel',
  '48:8a:2a': 'Intel',
  '70:85:c2': 'Intel',
  '00:0c:42': 'Realtek',
  '00:e0:4c': 'Realtek',
  '00:16:6e': 'Realtek',
  '00:1d:7d': 'Giga-Byte',
  '00:24:e4': 'Giga-Byte',
  '00:1a:0b': 'Tyan',
  '00:1f:33': 'Netgear',
  'a0:40:a0': 'Netgear',
  '00:14:6c': 'Netgear',
  '9c:3d:cf': 'Netgear',
  '00:18:4f': 'Netgear',
  'c0:25:e9': 'Netgear',
  'b4:2e:99': 'Netgear',
  '00:80:77': 'Netgear',
  '00:15:f2': 'Netgear',
  '1c:bf:ce': 'Shenzhen',
  '60:e3:27': 'SMC Networks',
  '00:0a:95': 'Sun',
  '00:1b:11': 'Google',
  'f4:f5:e8': 'Google',
  '00:1a:11': 'Google',
  'a4:77:33': 'Google',
  'f0:de:f1': 'Google'
}
// macVendor guesses the vendor behind a MAC address from its OUI prefix.
// A locally administered address (the second least significant bit of the
// first octet is set) has no vendor at all, which is the case for every
// virtual interface and for a randomised Wi-Fi address.
export function macVendor(mac?: string): string {
  if (!mac) return ''
  const normalized = mac.trim().toLowerCase().replace(/-/g, ':')
  const parts = normalized.split(':')
  if (parts.length < 3) return ''
  const first = parseInt(parts[0], 16)
  if (Number.isNaN(first)) return ''
  // A locally administered address has no OUI.
  if (first & 0x02) return 'local'
  const prefix = `${parts[0]}:${parts[1]}:${parts[2]}`
  return OUI_VENDORS[prefix] ?? ''
}

// neighbourTone grades a neighbour entry. A failed entry is a device that
// did not answer.
export function neighbourTone(neighbor: NetworkNeighbor): 'ok' | 'warn' | 'crit' {
  if (neighbor.state === 'failed') return 'crit'
  if (neighbor.state === 'stale') return 'warn'
  return 'ok'
}

// healthSummary is the one-line reading of the packet health, used by the
// header of the health card.
export function healthSummary(health?: NetworkHealth): { title: string; tone: 'ok' | 'warn' | 'crit' } {
  if (!health) return { title: 'No packet data reported yet', tone: 'warn' }
  const retrans = health.tcp?.retrans_percent
  const conntrack = health.conntrack ? conntrackUsage(health.conntrack.count, health.conntrack.max) : 0
  const closeWait = health.sockets?.close_wait ?? 0
  if ((health.tcp?.in_errs ?? 0) > 0) return { title: 'The kernel is reporting TCP input errors', tone: 'crit' }
  if (conntrack >= 80) return { title: 'The connection table is nearly full', tone: 'crit' }
  if (retrans !== undefined && retrans >= 10) return { title: 'Heavy packet loss on the path', tone: 'crit' }
  if (closeWait > 100) return { title: 'Many sockets are stuck in CLOSE_WAIT', tone: 'warn' }
  if (conntrack >= 60) return { title: 'The connection table is filling up', tone: 'warn' }
  if (retrans !== undefined && retrans >= 2) return { title: 'Some packets are being retransmitted', tone: 'warn' }
  return { title: 'The stack looks healthy', tone: 'ok' }
}
