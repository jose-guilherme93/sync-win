import { describe, expect, it } from 'vitest'
import type { Device, NetworkSection } from './types'
import { buildTopology, ipOnly, ipv4Network, layoutTopology, SELF_ID } from './topology'

function device(overrides: Partial<Device> = {}): Device {
  return {
    id: 'dev-1',
    hostname: 'workstation',
    user_id: 'user',
    status: 'online',
    last_seen_at: '2026-10-30T10:00:00Z',
    last_sync_at: '2026-10-30T10:00:00Z',
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0,
    ...overrides
  }
}

function section(overrides: Partial<NetworkSection> = {}): NetworkSection {
  return {
    topology: {
      default_gateway: '192.168.2.1',
      routes: [{ destination: '0.0.0.0', gateway: '192.168.2.1', interface: 'enp3s0', kind: 'default', family: 'inet' }],
      neighbors: [],
      ...overrides.topology
    },
    firewall: overrides.firewall
  }
}

describe('address helpers', () => {
  it('drops the prefix and the IPv6 zone', () => {
    expect(ipOnly('192.168.2.5/24')).toBe('192.168.2.5')
    expect(ipOnly('fe80::1%eth0/64')).toBe('fe80::1')
  })

  it('turns an address into the segment an operator recognises', () => {
    expect(ipv4Network('192.168.2.37/24')).toBe('192.168.2.0/24')
    expect(ipv4Network('10.1.2.3/16')).toBe('10.1.0.0/16')
    expect(ipv4Network('fd7a::1/64')).toBe('')
  })
})

describe('LAN topology', () => {
  const local = device({
    hardware: {
      network_ifaces: [
        { name: 'enp3s0', kind: 'physical', oper_state: 'up', addresses: ['192.168.2.5/24'], rx_bytes: 0, tx_bytes: 0, rx_packets: 0, tx_packets: 0, rx_errors: 0, tx_errors: 0 },
        { name: 'lo', kind: 'loopback', addresses: ['127.0.0.1/8'], rx_bytes: 0, tx_bytes: 0, rx_packets: 0, tx_packets: 0, rx_errors: 0, tx_errors: 0 },
        { name: 'docker0', kind: 'bridge', addresses: ['172.17.0.1/16'], rx_bytes: 0, tx_bytes: 0, rx_packets: 0, tx_packets: 0, rx_errors: 0, tx_errors: 0 }
      ],
      memory_used_bytes: 0,
      memory_total_bytes: 0,
      cpu_usage_percent: 0,
      disk_read_rate: 0,
      disk_write_rate: 0,
      uptime_seconds: 0,
      load_average: '0',
      power_watts: 0,
      collected_at: '2026-10-30T10:00:00Z'
    }
  })

  const snapshot = section({
    topology: {
      default_gateway: '192.168.2.1',
      routes: [{ destination: '0.0.0.0', gateway: '192.168.2.1', interface: 'enp3s0', kind: 'default', family: 'inet' }],
      neighbors: [
        { ip: '192.168.2.1', mac: 'aa:bb:cc:00:00:01', interface: 'enp3s0', state: 'reachable', family: 'inet' },
        { ip: '192.168.2.20', mac: '02:00:00:00:00:20', interface: 'enp3s0', state: 'reachable', family: 'inet' },
        { ip: '192.168.2.30', mac: 'aa:bb:cc:00:00:30', interface: 'enp3s0', state: 'stale', family: 'inet' },
        { ip: '192.168.2.20', mac: '02:00:00:00:00:20', interface: 'enp3s0', state: 'reachable', family: 'inet' }
      ]
    }
  })

  it('builds one network from the real interface and leaves the plumbing out', () => {
    const graph = buildTopology(local, snapshot)
    expect(graph.networks).toHaveLength(1)
    expect(graph.networks[0].cidr).toBe('192.168.2.0/24')
    expect(graph.networks[0].subtitle).toContain('enp3s0')
    expect(graph.nodes.some((node) => node.id.includes('docker0'))).toBe(false)
  })

  it('keeps the gateway once even though it is also a neighbour', () => {
    const graph = buildTopology(local, snapshot)
    const gateways = graph.nodes.filter((node) => node.kind === 'gateway')
    expect(gateways).toHaveLength(1)
    expect(gateways[0].ips).toEqual(['192.168.2.1'])
    expect(graph.stats.lan).toBe(3)
  })

  it('deduplicates the neighbour table and labels a stale entry as warning, not offline', () => {
    const graph = buildTopology(local, snapshot)
    const neighbours = graph.nodes.filter((node) => node.kind === 'neighbour')
    expect(neighbours.map((node) => node.ips[0])).toEqual(['192.168.2.20', '192.168.2.30'])
    const stale = neighbours.find((node) => node.ips[0] === '192.168.2.30')
    expect(stale?.online).toBe(true)
    expect(stale?.tone).toBe('warn')
  })

  it('draws the default route out through the gateway', () => {
    const graph = buildTopology(local, snapshot)
    const internet = graph.nodes.find((node) => node.kind === 'internet')
    expect(internet).toBeTruthy()
    expect(graph.edges.some((edge) => edge.kind === 'route' && edge.to === internet!.id)).toBe(true)
  })

  it('keeps neighbours whose interface is not in the hardware snapshot', () => {
    const graph = buildTopology(
      device(),
      section({
        topology: {
          neighbors: [{ ip: '192.168.9.4', mac: 'aa:bb:cc:00:00:04', interface: 'enp9s0', state: 'stale', family: 'inet' }]
        }
      })
    )
    expect(graph.networks).toHaveLength(1)
    expect(graph.networks[0]).toMatchObject({ kind: 'lan', iface: 'enp9s0', state: 'unknown' })
    expect(graph.networks[0].nodeIds).toHaveLength(1)
  })

  it('never turns a docker bridge into a workspace network', () => {
    const graph = buildTopology(
      device(),
      section({
        topology: {
          neighbors: [
            { ip: '172.17.0.2', mac: '02:42:ac:11:00:02', interface: 'docker0', state: 'reachable', family: 'inet' },
            { ip: '172.18.0.3', mac: '02:42:ac:12:00:03', interface: 'br-08b1487fb01c', state: 'reachable', family: 'inet' }
          ]
        }
      })
    )
    expect(graph.networks).toHaveLength(0)
    expect(graph.stats.devices).toBe(0)
  })

  it('recognises another fleet device by its address', () => {
    const peer = device({
      id: 'dev-2',
      hostname: 'nas',
      hardware: {
        network_ifaces: [
          { name: 'eth0', kind: 'physical', addresses: ['192.168.2.20/24'], rx_bytes: 0, tx_bytes: 0, rx_packets: 0, tx_packets: 0, rx_errors: 0, tx_errors: 0 }
        ],
        memory_used_bytes: 0,
        memory_total_bytes: 0,
        cpu_usage_percent: 0,
        disk_read_rate: 0,
        disk_write_rate: 0,
        uptime_seconds: 0,
        load_average: '0',
        power_watts: 0,
        collected_at: '2026-10-30T10:00:00Z'
      }
    })
    const graph = buildTopology(local, snapshot, [local, peer])
    const node = graph.nodes.find((candidate) => candidate.ips[0] === '192.168.2.20')
    expect(node?.label).toBe('nas')
    expect(node?.managedDeviceId).toBe('dev-2')
    expect(node?.managedDeviceLabel).toBe('nas')
  })
})

describe('overlay topology', () => {
  const snapshot: NetworkSection = {
    topology: {
      overlays: [
        {
          kind: 'tailscale',
          name: 'example.github',
          magic_dns: 'tail5df2c3.ts.net',
          interface: 'tailscale0',
          state: 'running',
          self: { hostname: 'workstation', online: true, ips: ['100.64.0.1'] },
          peers: [
            { hostname: 'server', online: true, direct: true, os: 'linux', ips: ['100.64.0.2'] },
            { hostname: 'laptop', online: false, relay: 'sao', os: 'macOS', ips: ['100.64.0.3'] }
          ]
        }
      ],
      tunnels: [
        { interface: 'tailscale0', kind: 'tailscale', state: 'up' },
        { interface: 'wg0', kind: 'wireguard', state: 'up' }
      ]
    }
  }

  it('keeps offline peers on the map', () => {
    const graph = buildTopology(device(), snapshot)
    expect(graph.networks).toHaveLength(1)
    expect(graph.networks[0]).toMatchObject({ kind: 'tailscale', state: 'running' })
    const peers = graph.nodes.filter((node) => node.kind === 'peer')
    expect(peers).toHaveLength(2)
    expect(peers.find((peer) => peer.label === 'laptop')?.tone).toBe('offline')
    expect(graph.stats.peers).toBe(2)
    expect(graph.stats.online).toBe(1)
  })

  it('marks a direct path and a relayed path differently', () => {
    const graph = buildTopology(device(), snapshot)
    const edges = graph.edges.filter((edge) => edge.kind === 'tailscale')
    expect(edges.find((edge) => edge.direct === true)).toBeTruthy()
    expect(edges.find((edge) => edge.direct === false)).toBeTruthy()
  })

  it('folds the tailscale address into the self node', () => {
    const graph = buildTopology(device(), snapshot)
    expect(graph.nodes.find((node) => node.id === SELF_ID)?.ips).toContain('100.64.0.1')
  })

  it('names a tunnel whose peers need root instead of hiding it', () => {
    const graph = buildTopology(device(), snapshot)
    expect(graph.unreadable).toEqual([{ interface: 'wg0', kind: 'wireguard', state: 'up' }])
  })

  it('reports an unreadable overlay as an error, not as an empty network', () => {
    const graph = buildTopology(device(), {
      topology: {
        overlays: [
          {
            kind: 'tailscale',
            interface: 'tailscale0',
            state: 'unknown',
            error: 'reading /var/run/tailscale/tailscaled.sock failed: permission denied',
            peers: []
          }
        ]
      }
    })
    expect(graph.networks[0].state).toBe('unavailable')
    expect(graph.networks[0].error).toContain('permission denied')
    expect(graph.nodes.find((node) => node.kind === 'network')?.tone).toBe('crit')
  })

  it('recognises a fleet device by the tailnet hostname', () => {
    const peer = device({ id: 'dev-2', hostname: 'server' })
    const graph = buildTopology(device(), snapshot, [peer])
    const node = graph.nodes.find((candidate) => candidate.label === 'server')
    expect(node?.managedDeviceId).toBe('dev-2')
  })
})

describe('without a snapshot', () => {
  it('still shows the device and no invented networks', () => {
    const graph = buildTopology(device(), null)
    expect(graph.nodes).toHaveLength(1)
    expect(graph.nodes[0].kind).toBe('self')
    expect(graph.networks).toHaveLength(0)
    expect(graph.stats).toEqual({ networks: 0, devices: 0, online: 0, offline: 0, lan: 0, peers: 0 })
  })
})

describe('layout', () => {
  const snapshot = section({
    topology: {
      default_gateway: '192.168.2.1',
      routes: [{ destination: '0.0.0.0', gateway: '192.168.2.1', interface: 'enp3s0', kind: 'default', family: 'inet' }],
      neighbors: Array.from({ length: 12 }, (_, index) => ({
        ip: `192.168.2.${10 + index}`,
        mac: `aa:bb:cc:00:00:${(10 + index).toString(16)}`,
        interface: 'enp3s0',
        state: 'reachable',
        family: 'inet'
      }))
    }
  })
  const local = device({
    hardware: {
      network_ifaces: [
        { name: 'enp3s0', kind: 'physical', addresses: ['192.168.2.5/24'], rx_bytes: 0, tx_bytes: 0, rx_packets: 0, tx_packets: 0, rx_errors: 0, tx_errors: 0 }
      ],
      memory_used_bytes: 0,
      memory_total_bytes: 0,
      cpu_usage_percent: 0,
      disk_read_rate: 0,
      disk_write_rate: 0,
      uptime_seconds: 0,
      load_average: '0',
      power_watts: 0,
      collected_at: '2026-10-30T10:00:00Z'
    }
  })

  it('positions every node inside the canvas', () => {
    const graph = buildTopology(local, snapshot)
    const positions = layoutTopology(graph, 1000, 640)
    expect(positions.size).toBe(graph.nodes.length)
    for (const point of positions.values()) {
      expect(Number.isFinite(point.x)).toBe(true)
      expect(Number.isFinite(point.y)).toBe(true)
      expect(point.x).toBeGreaterThan(0)
      expect(point.x).toBeLessThan(1000)
      expect(point.y).toBeGreaterThan(0)
      expect(point.y).toBeLessThan(640)
    }
  })

  it('is deterministic for the same graph', () => {
    const graph = buildTopology(local, snapshot)
    const first = layoutTopology(graph, 1000, 640)
    const second = layoutTopology(graph, 1000, 640)
    expect([...first.entries()]).toEqual([...second.entries()])
  })

  it('keeps the self node in the middle', () => {
    const graph = buildTopology(local, snapshot)
    const point = layoutTopology(graph, 1000, 640).get(SELF_ID)
    expect(point).toEqual({ x: 500, y: 320 })
  })

  it('stacks the hubs above and below the device on a narrow canvas', () => {
    const graph = buildTopology(local, {
      topology: {
        ...snapshot.topology,
        overlays: [
          {
            kind: 'tailscale',
            name: 'example.github',
            state: 'running',
            self: { hostname: 'workstation', online: true, ips: ['100.64.0.1'] },
            peers: [{ hostname: 'server', online: true, ips: ['100.64.0.2'] }]
          }
        ]
      }
    })
    const positions = layoutTopology(graph, 420, 600)
    const hubs = graph.nodes.filter((node) => node.kind === 'network')
    expect(hubs).toHaveLength(2)
    const points = hubs.map((hub) => positions.get(hub.id)!)
    for (const point of points) expect(point.x).toBeCloseTo(210)
    expect(points[0].y).toBeLessThan(300)
    expect(points[1].y).toBeGreaterThan(300)
  })
})
