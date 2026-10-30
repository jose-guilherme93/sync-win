// The network map: every network this device takes part in, built from the
// snapshot the agent reports.
//
// There are two very different kinds of membership, and the map says so
// instead of flattening them:
//
//   - A LAN network is what the kernel has seen on an interface. The members
//     are the neighbour table (ARP and NDP), so a device that has not talked to
//     this host recently is simply not there.
//   - An overlay network (Tailscale) reports its own membership. A peer that is
//     offline is still a peer, and it stays on the map, greyed out.
//
// Everything here is pure and tested without a browser. The screen only lays
// the result out and draws it.

import { deviceLabel, type Device, type NetworkIface, type NetworkNeighbor, type NetworkSection, type OverlayNode } from './types'
import { macVendor } from './network'

export type TopologyNodeKind = 'self' | 'network' | 'gateway' | 'neighbour' | 'peer' | 'internet'
export type TopologyTone = 'ok' | 'warn' | 'crit' | 'offline'
export type TopologyNetworkKind = 'lan' | 'tailscale'

export type TopologyNode = {
  id: string
  kind: TopologyNodeKind
  label: string
  sublabel: string
  networkId: string
  ips: string[]
  mac?: string
  vendor?: string
  state?: string
  online: boolean
  tone: TopologyTone
  os?: string
  direct?: boolean
  relay?: string
  exitNode?: boolean
  exitNodeOption?: boolean
  lastSeen?: string
  tags?: string[]
  iface?: string
  // managedDeviceId links a node to another device in the same SyncWin fleet.
  // It is a convenience for navigation, never a claim about ownership.
  managedDeviceId?: string
  managedDeviceLabel?: string
}

export type TopologyEdgeKind = 'membership' | 'lan' | 'tailscale' | 'route'

export type TopologyEdge = {
  id: string
  from: string
  to: string
  kind: TopologyEdgeKind
  // direct is only meaningful on a tailscale edge: a relayed path is drawn
  // differently because it is a different kind of connection.
  direct?: boolean
}

export type TopologyNetwork = {
  id: string
  kind: TopologyNetworkKind
  title: string
  subtitle: string
  state: string
  error?: string
  iface?: string
  cidr?: string
  address?: string
  gateway?: string
  nodeIds: string[]
}

export type UnreadableTunnel = {
  interface: string
  kind: string
  state?: string
}

export type TopologyGraph = {
  selfId: string
  nodes: TopologyNode[]
  edges: TopologyEdge[]
  networks: TopologyNetwork[]
  // Tunnels whose peers cannot be read without root. They are named instead of
  // silently left out, so the map does not look like the whole story.
  unreadable: UnreadableTunnel[]
  stats: {
    networks: number
    devices: number
    online: number
    offline: number
    lan: number
    peers: number
  }
}

export const SELF_ID = 'self'
export const INTERNET_ID = 'internet'

// buildTopology turns the device, its network snapshot and the rest of the
// fleet into one graph. `fleet` is only used to recognise another SyncWin
// device that appears on the segment or in the tailnet.
export function buildTopology(device: Device, section: NetworkSection | null, fleet: Device[] = []): TopologyGraph {
  const topology = section?.topology
  const nodes = new Map<string, TopologyNode>()
  const edges: TopologyEdge[] = []
  const networks: TopologyNetwork[] = []
  const fleetByIP = fleetIPIndex(fleet, device.id)
  const fleetByHost = fleetHostIndex(fleet, device.id)

  const ifaces = device.hardware?.network_ifaces ?? []
  const overlays = topology?.overlays ?? []
  const selfIPs = unique([...ifaces.flatMap(addressIPs), ...overlays.flatMap((overlay) => overlay.self?.ips ?? [])])

  const selfLabel = deviceLabel(device)
  nodes.set(SELF_ID, {
    id: SELF_ID,
    kind: 'self',
    label: selfLabel,
    // A device whose display name is its hostname would otherwise print the
    // same line twice under the central node.
    sublabel: device.hostname && device.hostname !== selfLabel ? device.hostname : 'this device',
    networkId: SELF_ID,
    ips: selfIPs,
    online: true,
    tone: 'ok'
  })

  // The LAN view: one network per interface that carries an address. The hub
  // is what makes the map readable when an interface has dozens of neighbours;
  // every member is attached to it instead of to the device.
  for (const iface of ifaces) {
    if (!isLanInterface(iface)) continue
    const address = ifaceIPv4(iface) ?? ifaceIPv6(iface)
    if (!address) continue

    const addressValue = ipOnly(address)
    const cidr = ipv4Network(address) ?? address
    const route = (topology?.routes ?? []).find((r) => r.kind === 'default' && r.interface === iface.name && r.gateway && r.gateway !== '0.0.0.0' && r.gateway !== '::')
    const gateway = route?.gateway ?? (topology?.default_gateway || '')
    const isWifi = iface.kind === 'wifi'
    const networkId = `lan:${iface.name}`
    const members: string[] = []

    nodes.set(networkId, {
      id: networkId,
      kind: 'network',
      label: isWifi ? 'Wi-Fi' : 'LAN',
      sublabel: `${iface.name} · ${cidr}`,
      networkId,
      ips: [],
      online: iface.oper_state !== 'down',
      tone: iface.oper_state === 'down' ? 'offline' : 'ok',
      iface: iface.name
    })
    edges.push({ id: `${SELF_ID}->${networkId}`, from: SELF_ID, to: networkId, kind: 'membership' })

    const neighbourIPs = new Set<string>()
    const neighbours = (topology?.neighbors ?? [])
      .filter((neighbour) => neighbour.interface === iface.name && neighbour.ip)
      .filter((neighbour) => neighbour.ip !== addressValue)
      .sort(compareNeighbours)

    let gatewayAdded = false
    const addGateway = (ip: string) => {
      const node = gatewayNode(ip, networkId, fleetByIP)
      if (!nodes.has(node.id)) {
        nodes.set(node.id, node)
        members.push(node.id)
      }
      const edgeId = `${networkId}->${node.id}`
      if (!edges.some((edge) => edge.id === edgeId)) {
        edges.push({ id: edgeId, from: networkId, to: node.id, kind: 'lan' })
      }
      gatewayAdded = true
    }

    for (const neighbour of neighbours) {
      if (neighbourIPs.has(neighbour.ip)) continue
      neighbourIPs.add(neighbour.ip)
      // The gateway is usually in the neighbour table too. It is drawn as one
      // gateway node, not as just another device, so the route out is visible.
      if (gateway && neighbour.ip === gateway) {
        addGateway(neighbour.ip)
        continue
      }
      const node = neighbourNode(networkId, iface.name, neighbour, fleetByIP)
      nodes.set(node.id, node)
      members.push(node.id)
      edges.push({ id: `${networkId}->${node.id}`, from: networkId, to: node.id, kind: 'lan' })
    }

    if (gateway && !gatewayAdded) addGateway(gateway)

    networks.push({
      id: networkId,
      kind: 'lan',
      title: isWifi ? 'Wireless network' : 'Local network',
      subtitle: `${iface.name} · ${cidr}`,
      state: iface.oper_state || 'unknown',
      iface: iface.name,
      cidr,
      address: addressValue,
      gateway,
      nodeIds: members
    })
  }

  // A neighbour can name an interface the hardware snapshot does not describe
  // (an older agent, a bridge that is not classed as a real link, an interface
  // that went away between the two collections). The kernel saw the devices,
  // so they still belong on the map; only the address of the segment is
  // unknown.
  const coveredInterfaces = new Set(networks.filter((network) => network.kind === 'lan').map((network) => network.iface))
  const orphanGroups = new Map<string, NetworkNeighbor[]>()
  for (const neighbour of topology?.neighbors ?? []) {
    if (!neighbour.interface || !neighbour.ip || coveredInterfaces.has(neighbour.interface)) continue
    // Container bridges also have neighbours (the containers), but they are
    // not networks the device participates in as a host. Drawing docker0 would
    // bury the real segment under virtual ones.
    if (isPlumbingInterface(neighbour.interface)) continue
    const group = orphanGroups.get(neighbour.interface) ?? []
    group.push(neighbour)
    orphanGroups.set(neighbour.interface, group)
  }
  for (const [iface, group] of orphanGroups) {
    const networkId = `lan:${iface}`
    const members: string[] = []
    nodes.set(networkId, {
      id: networkId,
      kind: 'network',
      label: 'LAN',
      sublabel: `${iface} · ${group.length} neighbour${group.length === 1 ? '' : 's'}`,
      networkId,
      ips: [],
      online: true,
      tone: 'ok',
      iface
    })
    edges.push({ id: `${SELF_ID}->${networkId}`, from: SELF_ID, to: networkId, kind: 'membership' })
    const seen = new Set<string>()
    for (const neighbour of group.sort(compareNeighbours)) {
      if (seen.has(neighbour.ip)) continue
      seen.add(neighbour.ip)
      const node = neighbourNode(networkId, iface, neighbour, fleetByIP)
      nodes.set(node.id, node)
      members.push(node.id)
      edges.push({ id: `${networkId}->${node.id}`, from: networkId, to: node.id, kind: 'lan' })
    }
    networks.push({
      id: networkId,
      kind: 'lan',
      title: 'Local network',
      subtitle: `${iface} · ${members.length} neighbour${members.length === 1 ? '' : 's'}`,
      state: 'unknown',
      iface,
      nodeIds: members
    })
  }

  // The overlay view: the daemon's own membership list. Offline peers stay on
  // the map because they are still part of the network.
  overlays.forEach((overlay, index) => {
    const networkId = `overlay:${overlay.kind}:${index}`
    const selfHost = overlay.self?.hostname?.toLowerCase() ?? ''
    const peers = (overlay.peers ?? []).filter((peer) => peer.hostname && peer.hostname.toLowerCase() !== selfHost)
    const members: string[] = []
    const tone: TopologyTone = overlay.error ? 'crit' : overlay.state === 'running' ? 'ok' : 'offline'

    nodes.set(networkId, {
      id: networkId,
      kind: 'network',
      label: overlayLabel(overlay.kind),
      sublabel: overlay.name || overlay.magic_dns || overlay.interface || 'private network',
      networkId,
      ips: [],
      online: overlay.state === 'running' && !overlay.error,
      tone,
      state: overlay.state
    })
    edges.push({ id: `${SELF_ID}->${networkId}`, from: SELF_ID, to: networkId, kind: 'membership' })

    peers.forEach((peer, peerIndex) => {
      const node = peerNode(networkId, overlay.kind, peer, peerIndex, fleetByHost)
      nodes.set(node.id, node)
      members.push(node.id)
      edges.push({
        id: `${networkId}->${node.id}`,
        from: networkId,
        to: node.id,
        kind: 'tailscale',
        direct: peer.direct === true
      })
    })

    networks.push({
      id: networkId,
      kind: 'tailscale',
      title: overlayLabel(overlay.kind),
      subtitle: overlay.name || overlay.magic_dns || overlay.interface || 'private network',
      state: overlay.error ? 'unavailable' : overlay.state,
      error: overlay.error,
      iface: overlay.interface,
      gateway: undefined,
      nodeIds: members
    })
  })

  // The default route leaves through a gateway. Drawing it as one shared
  // "Internet" node keeps the LAN view from claiming the gateway is the
  // endpoint of the network.
  const gateways = [...nodes.values()].filter((node) => node.kind === 'gateway')
  if (gateways.length > 0) {
    nodes.set(INTERNET_ID, {
      id: INTERNET_ID,
      kind: 'internet',
      label: 'Internet',
      sublabel: 'default route',
      networkId: 'internet',
      ips: [],
      online: true,
      tone: 'ok'
    })
    for (const gateway of gateways) {
      edges.push({ id: `${gateway.id}->${INTERNET_ID}`, from: gateway.id, to: INTERNET_ID, kind: 'route' })
    }
  }

  const unreadable = (topology?.tunnels ?? [])
    .filter((tunnel) => !overlays.some((overlay) => overlay.kind === tunnel.kind || overlay.interface === tunnel.interface))
    .map((tunnel) => ({ interface: tunnel.interface, kind: tunnel.kind, state: tunnel.state }))

  const nodeList = [...nodes.values()]
  const devices = nodeList.filter((node) => node.kind === 'neighbour' || node.kind === 'peer' || node.kind === 'gateway')
  return {
    selfId: SELF_ID,
    nodes: nodeList,
    edges,
    networks,
    unreadable,
    stats: {
      networks: networks.length,
      devices: devices.length,
      online: devices.filter((node) => node.online).length,
      offline: devices.filter((node) => !node.online).length,
      lan: nodeList.filter((node) => node.kind === 'neighbour' || node.kind === 'gateway').length,
      peers: nodeList.filter((node) => node.kind === 'peer').length
    }
  }
}

// isLanInterface picks the interfaces that represent a real segment. Container
// bridges, veth pairs and loopback are plumbing: they would add a network that
// exists only inside this host.
function isLanInterface(iface: NetworkIface): boolean {
  switch (iface.kind) {
    case undefined:
    case '':
    case 'physical':
    case 'wifi':
      return true
    default:
      return false
  }
}

// isPlumbingInterface recognises the names the agent classes as container,
// VM or tunnel plumbing. The fallback path only sees a neighbour's interface
// name, not its kind, so it applies the same prefixes here.
function isPlumbingInterface(name: string): boolean {
  const prefixes = ['lo', 'veth', 'br-', 'br0', 'docker', 'virbr', 'vnet', 'cni', 'flannel', 'cali', 'kube', 'tailscale', 'wg', 'tun', 'tap', 'zt', 'utun', 'ppp', 'ipsec']
  return prefixes.some((prefix) => name === prefix || name.startsWith(prefix))
}

function ifaceIPv4(iface: NetworkIface): string {
  return (iface.addresses ?? []).find((address) => /^(\d{1,3}\.){3}\d{1,3}\//.test(address)) ?? ''
}

function ifaceIPv6(iface: NetworkIface): string {
  return (iface.addresses ?? []).find((address) => address.includes(':') && !address.startsWith('fe80:')) ?? ''
}

function addressIPs(iface: NetworkIface): string[] {
  return (iface.addresses ?? []).map(ipOnly).filter(Boolean)
}

// neighbourNode is one device the kernel has seen on the segment.
function neighbourNode(networkId: string, iface: string, neighbour: NetworkNeighbor, fleetByIP: Map<string, Device>): TopologyNode {
  const ip = neighbour.ip
  const managed = fleetByIP.get(ip)
  const vendor = macVendor(neighbour.mac)
  // A stale entry is a device the kernel has not had to confirm recently. It
  // is not an offline device: the MAC binding is still there. Only a failed
  // entry is a negative answer.
  const online = neighbour.state !== 'failed'
  return {
    id: `${networkId}:${ip}`,
    kind: 'neighbour',
    label: managed ? deviceLabel(managed) : ip,
    sublabel: managed ? ip : vendor || neighbour.state || 'on the segment',
    networkId,
    ips: [ip],
    mac: neighbour.mac,
    vendor: vendor || undefined,
    state: neighbour.state,
    online,
    tone: neighbour.state === 'failed' ? 'crit' : neighbour.state === 'stale' ? 'warn' : 'ok',
    iface,
    managedDeviceId: managed?.id,
    managedDeviceLabel: managed ? deviceLabel(managed) : undefined
  }
}

function gatewayNode(ip: string, networkId: string, fleetByIP: Map<string, Device>): TopologyNode {
  const managed = fleetByIP.get(ip)
  return {
    id: `gateway:${ip}`,
    kind: 'gateway',
    label: managed ? deviceLabel(managed) : 'Gateway',
    sublabel: ip,
    networkId,
    ips: [ip],
    online: true,
    tone: 'ok',
    managedDeviceId: managed?.id,
    managedDeviceLabel: managed ? deviceLabel(managed) : undefined
  }
}

function peerNode(networkId: string, kind: string, peer: OverlayNode, index: number, fleetByHost: Map<string, Device>): TopologyNode {
  const managed = fleetByHost.get(peer.hostname.toLowerCase()) ?? fleetByHost.get(dnsHost(peer.dns_name).toLowerCase())
  const address = peer.ips?.[0] ?? ''
  return {
    id: `${networkId}:${peer.hostname}:${index}`,
    kind: 'peer',
    label: managed ? deviceLabel(managed) : peer.hostname,
    sublabel: address || peer.os || overlayLabel(kind),
    networkId,
    ips: peer.ips ?? [],
    online: peer.online,
    tone: peer.online ? 'ok' : 'offline',
    os: peer.os,
    direct: peer.direct,
    relay: peer.relay,
    exitNode: peer.exit_node,
    exitNodeOption: peer.exit_node_option,
    lastSeen: peer.last_seen,
    tags: peer.tags,
    state: peer.online ? 'online' : 'offline',
    managedDeviceId: managed?.id,
    managedDeviceLabel: managed ? deviceLabel(managed) : undefined
  }
}

function overlayLabel(kind: string): string {
  switch (kind) {
    case 'tailscale':
      return 'Tailscale'
    case 'wireguard':
      return 'WireGuard'
    case 'zerotier':
      return 'ZeroTier'
    default:
      return kind ? kind.charAt(0).toUpperCase() + kind.slice(1) : 'Overlay'
  }
}

// fleetIPIndex maps every address another fleet device reports to that device.
// It is how a node that the kernel only knows by address becomes recognisable.
function fleetIPIndex(fleet: Device[], selfId: string): Map<string, Device> {
  const index = new Map<string, Device>()
  for (const device of fleet) {
    if (device.id === selfId) continue
    for (const iface of device.hardware?.network_ifaces ?? []) {
      for (const address of iface.addresses ?? []) {
        const ip = ipOnly(address)
        if (ip) index.set(ip, device)
      }
    }
  }
  return index
}

function fleetHostIndex(fleet: Device[], selfId: string): Map<string, Device> {
  const index = new Map<string, Device>()
  for (const device of fleet) {
    if (device.id === selfId) continue
    if (device.hostname) index.set(device.hostname.toLowerCase(), device)
    if (device.display_name) index.set(device.display_name.toLowerCase(), device)
  }
  return index
}

function compareNeighbours(a: NetworkNeighbor, b: NetworkNeighbor): number {
  const rank = (neighbour: NetworkNeighbor) => (neighbour.state === 'reachable' ? 0 : neighbour.state === 'stale' ? 1 : 2)
  return rank(a) - rank(b) || compareIP(a.ip, b.ip)
}

function compareIP(a: string, b: string): number {
  const head = (ip: string) => ip.split('.').reduce((value, part) => value * 256 + (parseInt(part, 10) || 0), 0)
  if (a.includes('.') && b.includes('.')) return head(a) - head(b)
  return a.localeCompare(b)
}

// ipOnly drops the prefix length, the IPv6 zone and the brackets.
export function ipOnly(address: string): string {
  const trimmed = address.trim().replace(/^\[|\]$/g, '')
  const withoutZone = trimmed.split('%')[0]
  return withoutZone.split('/')[0]
}

// ipv4Network turns 192.168.2.37/24 into 192.168.2.0/24, which is the label an
// operator recognises as the segment.
export function ipv4Network(address: string): string {
  const parts = address.trim().split('/')
  if (parts.length !== 2) return ''
  const octets = parts[0].split('.').map((part) => parseInt(part, 10))
  const bits = parseInt(parts[1], 10)
  if (octets.length !== 4 || octets.some((octet) => !Number.isFinite(octet)) || !Number.isFinite(bits) || bits < 0 || bits > 32) return ''
  const value = octets.reduce((acc, octet) => (acc * 256 + octet) >>> 0, 0)
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0
  const network = (value & mask) >>> 0
  return `${(network >>> 24) & 255}.${(network >>> 16) & 255}.${(network >>> 8) & 255}.${network & 255}/${bits}`
}

function dnsHost(dnsName?: string): string {
  return (dnsName ?? '').replace(/\.$/, '').split('.')[0] ?? ''
}

function unique(values: string[]): string[] {
  return [...new Set(values.filter(Boolean))]
}

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

export type TopologyPoint = { x: number; y: number }

// Collision sizes are larger than the drawn circles on purpose: the label
// under a node is part of its footprint, and a map where every caption sits on
// top of the next is unreadable even when the circles do not touch.
const DEVICE_SIZE = 46
const SELF_SIZE = 50
const HUB_SIZE = 64
const INTERNET_SIZE = 38
const PADDING = 58

// layoutTopology places the self node in the middle, one hub per network
// around it, and the members in a disc around their hub. A small deterministic
// force simulation does the packing: hubs and the device stay fixed, members
// repel each other and are pulled towards their network. The result is stable
// for the same input, so opening the screen twice does not shuffle the map.
export function layoutTopology(graph: TopologyGraph, width = 1200, height = 720): Map<string, TopologyPoint> {
  const positions = new Map<string, TopologyPoint>()
  const cx = width / 2
  const cy = height / 2
  const self = graph.nodes.find((node) => node.kind === 'self')
  const hubs = graph.nodes.filter((node) => node.kind === 'network')
  const members = graph.nodes.filter((node) => node.kind !== 'self' && node.kind !== 'network')

  positions.set(self?.id ?? SELF_ID, { x: cx, y: cy })

  const hubAnchors = new Map<string, TopologyPoint>()
  // Two 156px hub cards plus the device do not fit side by side on a phone.
  // On a narrow canvas the hubs are stacked above and below the device instead;
  // the members still orbit their own hub.
  const stacked = width < 560
  const radius = stacked ? height * 0.3 : Math.min(width * 0.22, height * 0.38)
  hubs.forEach((hub, index) => {
    let angle: number
    if (stacked) {
      angle = hubs.length === 1 ? -Math.PI / 2 : index === 0 ? -Math.PI / 2 : Math.PI / 2
    } else if (hubs.length === 1) {
      angle = 0
    } else if (hubs.length === 2) {
      angle = index === 0 ? Math.PI : 0
    } else {
      angle = -Math.PI / 2 + (index * 2 * Math.PI) / hubs.length
    }
    const point = { x: cx + Math.cos(angle) * radius, y: cy + Math.sin(angle) * radius }
    hubAnchors.set(hub.id, point)
    positions.set(hub.id, point)
  })

  // Members start in a small seeded cloud around their hub. The seed is
  // derived from the node id, so the same graph always settles the same way.
  const seeds = new Map<string, { angle: number; distance: number }>()
  members.forEach((node) => {
    const anchor = hubAnchors.get(node.networkId) ?? { x: cx, y: cy }
    const angle = seeded(node.id, 1) * Math.PI * 2
    const distance = 70 + seeded(node.id, 2) * 90
    positions.set(node.id, {
      x: anchor.x + Math.cos(angle) * distance,
      y: anchor.y + Math.sin(angle) * distance
    })
    seeds.set(node.id, { angle, distance })
  })

  const movable = members.map((node) => node.id)
  const fixed = new Set([self?.id ?? SELF_ID, ...hubs.map((hub) => hub.id)])
  const sizes = new Map<string, number>()
  const nodeById = new Map<string, TopologyNode>()
  for (const node of graph.nodes) {
    sizes.set(node.id, nodeSize(node.kind))
    nodeById.set(node.id, node)
  }

  // A busy segment and a large tailnet can put two hundred nodes on one map.
  // The simulation is quadratic, so the cap keeps the first paint responsive
  // without changing the result for a normal device.
  const iterations = graph.nodes.length > 120 ? 170 : graph.nodes.length > 70 ? 230 : 280
  for (let step = 0; step < iterations; step++) {
    // Springs: a member is pulled towards its network hub at a comfortable
    // distance. Internet follows its gateway instead.
    for (const id of movable) {
      const node = nodeById.get(id)
      if (!node) continue
      const point = positions.get(id)
      if (!point) continue
      const anchor = node.kind === 'internet'
        ? gatewayAnchor(graph, positions)
        : hubAnchors.get(node.networkId)
      if (!anchor) continue

      const dx = point.x - anchor.x
      const dy = point.y - anchor.y
      const distance = Math.max(1, Math.hypot(dx, dy))
      const target = node.kind === 'internet' ? 132 : 128
      const pull = (distance - target) * 0.012
      point.x -= (dx / distance) * pull
      point.y -= (dy / distance) * pull
      // A whisper of gravity keeps a network with one member from drifting.
      point.x += (cx - point.x) * 0.0015
      point.y += (cy - point.y) * 0.0015
    }

    // Repulsion and collision. Hubs and the self node are fixed obstacles.
    for (let i = 0; i < graph.nodes.length; i++) {
      const a = graph.nodes[i]
      const pa = positions.get(a.id)
      if (!pa) continue
      for (let j = i + 1; j < graph.nodes.length; j++) {
        const b = graph.nodes[j]
        const pb = positions.get(b.id)
        if (!pb) continue
        if (fixed.has(a.id) && fixed.has(b.id)) continue

        let dx = pb.x - pa.x
        let dy = pb.y - pa.y
        let distance = Math.hypot(dx, dy)
        if (distance < 0.01) {
          const seed = seeds.get(b.id) ?? { angle: 1, distance: 1 }
          dx = Math.cos(seed.angle) * 0.1
          dy = Math.sin(seed.angle) * 0.1
          distance = 0.1
        }
        if (distance > 460) continue

        const repulsion = Math.min(4.5, 2600 / (distance * distance))
        const pushX = (dx / distance) * repulsion
        const pushY = (dy / distance) * repulsion

        const minimum = (sizes.get(a.id) ?? DEVICE_SIZE) + (sizes.get(b.id) ?? DEVICE_SIZE)
        let overlapX = 0
        let overlapY = 0
        if (distance < minimum) {
          const overlap = (minimum - distance) * 0.35
          overlapX = (dx / distance) * overlap
          overlapY = (dy / distance) * overlap
        }

        if (!fixed.has(b.id)) {
          pb.x += pushX + overlapX
          pb.y += pushY + overlapY
        }
        if (!fixed.has(a.id)) {
          pa.x -= pushX + overlapX
          pa.y -= pushY + overlapY
        }
      }
    }

    for (const id of movable) {
      const point = positions.get(id)
      if (!point) continue
      point.x = clamp(point.x, PADDING, width - PADDING)
      point.y = clamp(point.y, PADDING, height - PADDING)
    }
  }

  return positions
}

function gatewayAnchor(graph: TopologyGraph, positions: Map<string, TopologyPoint>): TopologyPoint | null {
  const gateway = graph.nodes.find((node) => node.kind === 'gateway')
  return gateway ? positions.get(gateway.id) ?? null : null
}

function nodeSize(kind: TopologyNodeKind): number {
  switch (kind) {
    case 'self':
      return SELF_SIZE
    case 'network':
      return HUB_SIZE
    case 'internet':
      return INTERNET_SIZE
    default:
      return DEVICE_SIZE
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

// seeded produces a stable number in [0,1) from a string and a salt.
function seeded(value: string, salt: number): number {
  let hash = 2166136261 ^ salt
  for (let i = 0; i < value.length; i++) {
    hash ^= value.charCodeAt(i)
    hash = Math.imul(hash, 16777619)
  }
  return ((hash >>> 0) % 100000) / 100000
}
