import { describe, expect, it } from 'vitest'
import {
  conntrackUsage,
  countedInterfaces,
  exposureOf,
  exposedPorts,
  healthSummary,
  interfaceKindLabel,
  isCounted,
  linkTone,
  macVendor,
  neighbourTone,
  retransTone,
  signalTone,
  summarizeExposure,
  uncountedInterfaces
} from './network'
import type { NetworkIface } from './types'

function iface(overrides: Partial<NetworkIface> = {}): NetworkIface {
  return {
    name: 'eth0',
    rx_bytes: 0,
    tx_bytes: 0,
    rx_packets: 0,
    tx_packets: 0,
    rx_errors: 0,
    tx_errors: 0,
    ...overrides
  }
}

describe('counted interfaces', () => {
  it('treats an interface without the flag as counted', () => {
    // An agent that predates the flag only reported the real links.
    expect(isCounted(iface())).toBe(true)
  })

  it('keeps the container plumbing out of the totals but still reports it', () => {
    const list = [iface({ name: 'eth0' }), iface({ name: 'veth1', counted: false })]
    expect(countedInterfaces(list).map((i) => i.name)).toEqual(['eth0'])
    expect(uncountedInterfaces(list).map((i) => i.name)).toEqual(['veth1'])
  })
})

describe('socket exposure', () => {
  it('reads an empty address as every interface', () => {
    expect(exposureOf({ protocol: 'tcp', local_address: '', port: 22 })).toBe('all')
    expect(exposureOf({ protocol: 'tcp', local_address: '*', port: 22 })).toBe('all')
    expect(exposureOf({ protocol: 'tcp', local_address: '0.0.0.0', port: 22 })).toBe('all')
    expect(exposureOf({ protocol: 'tcp6', local_address: '::', port: 22 })).toBe('all')
  })

  it('reads a loopback address as local', () => {
    expect(exposureOf({ protocol: 'tcp', local_address: '127.0.0.1', port: 8080 })).toBe('loopback')
    expect(exposureOf({ protocol: 'tcp', local_address: '127.0.0.53', port: 53 })).toBe('loopback')
    expect(exposureOf({ protocol: 'tcp6', local_address: '::1', port: 8080 })).toBe('loopback')
  })

  it('reads a specific address as reachable from the network', () => {
    expect(exposureOf({ protocol: 'tcp', local_address: '192.168.2.10', port: 22 })).toBe('specific')
  })

  it('counts what the network can reach', () => {
    const ports = [
      { protocol: 'tcp', local_address: '', port: 22 },
      { protocol: 'tcp', local_address: '127.0.0.1', port: 5432 },
      { protocol: 'tcp', local_address: '192.168.2.10', port: 8080 }
    ]
    const summary = summarizeExposure(ports)
    expect(summary.total).toBe(3)
    expect(summary.exposed).toBe(2)
    expect(summary.loopback).toBe(1)
    expect(exposedPorts(ports).map((p) => p.port)).toEqual([22, 8080])
  })
})

describe('link grading', () => {
  it('is healthy without errors or drops', () => {
    expect(linkTone(iface())).toBe('ok')
  })

  it('warns on dropped packets', () => {
    expect(linkTone(iface({ rx_dropped: 3 }))).toBe('warn')
  })

  it('critics on errors and on the hardware counters', () => {
    expect(linkTone(iface({ tx_errors: 1 }))).toBe('crit')
    expect(linkTone(iface({ rx_collisions: 12 }))).toBe('crit')
    expect(linkTone(iface({ tx_carrier_errors: 2 }))).toBe('crit')
  })
})

describe('thresholds', () => {
  it('grades the retransmission share', () => {
    expect(retransTone(0.5)).toBe('ok')
    expect(retransTone(3)).toBe('warn')
    expect(retransTone(12)).toBe('crit')
    expect(retransTone(undefined)).toBe('ok')
  })

  it('grades a wireless signal', () => {
    expect(signalTone(-45)).toBe('ok')
    expect(signalTone(-72)).toBe('warn')
    expect(signalTone(-88)).toBe('crit')
    expect(signalTone(undefined)).toBe('ok')
  })

  it('computes the conntrack usage', () => {
    expect(conntrackUsage(50, 100)).toBe(50)
    expect(conntrackUsage(0, 100)).toBe(0)
    expect(conntrackUsage(10, 0)).toBe(0)
  })
})

describe('mac vendor', () => {
  it('reads a known OUI', () => {
    expect(macVendor('00:0c:29:ab:cd:ef')).toBe('VMware')
    expect(macVendor('b8:27:eb:11:22:33')).toBe('Raspberry Pi')
  })

  it('reports a locally administered address as local', () => {
    // The second least significant bit of the first octet marks a random
    // address: every virtual interface and every randomised Wi-Fi address.
    expect(macVendor('02:42:ac:11:22:33')).toBe('local')
  })

  it('says nothing about an unknown vendor', () => {
    // 0xa8 has the locally administered bit clear, so this is a real OUI that
    // is simply not in the table.
    expect(macVendor('a8:bb:cc:dd:ee:ff')).toBe('')
    expect(macVendor('')).toBe('')
  })
})

describe('neighbour state', () => {
  it('grades a neighbour entry', () => {
    expect(neighbourTone({ ip: '10.0.0.1', interface: 'eth0', state: 'reachable', family: 'inet' })).toBe('ok')
    expect(neighbourTone({ ip: '10.0.0.1', interface: 'eth0', state: 'stale', family: 'inet' })).toBe('warn')
    expect(neighbourTone({ ip: '10.0.0.1', interface: 'eth0', state: 'failed', family: 'inet' })).toBe('crit')
  })
})

describe('health summary', () => {
  it('says when nothing was reported', () => {
    expect(healthSummary(undefined).tone).toBe('warn')
  })

  it('is healthy without counters', () => {
    expect(healthSummary({}).tone).toBe('ok')
  })

  it('critics on TCP input errors', () => {
    expect(healthSummary({ tcp: { in_errs: 4 } }).tone).toBe('crit')
  })

  it('critics on a full connection table', () => {
    expect(healthSummary({ conntrack: { count: 900, max: 1000 } }).tone).toBe('crit')
  })

  it('warns on heavy loss', () => {
    expect(healthSummary({ tcp: { retrans_percent: 15 } }).tone).toBe('crit')
    expect(healthSummary({ tcp: { retrans_percent: 4 } }).tone).toBe('warn')
  })
})

describe('kind labels', () => {
  it('names the kinds the agent reports', () => {
    expect(interfaceKindLabel('physical')).toBe('physical')
    expect(interfaceKindLabel('wifi')).toBe('wireless')
    expect(interfaceKindLabel('veth')).toBe('container')
    expect(interfaceKindLabel(undefined)).toBe('link')
  })
})
