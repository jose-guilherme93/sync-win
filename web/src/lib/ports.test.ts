import { describe, expect, it } from 'vitest'
import { portIdentity, uniquePorts, type OpenPort } from './ports'

// The three spellings `ss` emits for one UDP socket after the collector folds
// them: 0.0.0.0, * and an empty host all become "".
const DUPLICATED: OpenPort[] = [
  { protocol: 'udp', local_address: '', port: 5353 },
  { protocol: 'udp', local_address: '', port: 5353 },
  { protocol: 'udp', local_address: '::', port: 5353 },
  { protocol: 'udp', local_address: '::', port: 5353 },
  { protocol: 'tcp', local_address: '', port: 22 }
]

describe('portIdentity', () => {
  it('is equal for two records that are equal in every field', () => {
    const a: OpenPort = { protocol: 'udp', local_address: '', port: 5353 }
    const b: OpenPort = { protocol: 'udp', local_address: '', port: 5353 }
    expect(portIdentity(a)).toBe(portIdentity(b))
  })

  it('tells apart the same address held by two processes', () => {
    const a: OpenPort = { protocol: 'udp', local_address: '', port: 5353, process: 'avahi', pid: 1 }
    const b: OpenPort = { protocol: 'udp', local_address: '', port: 5353, process: 'other', pid: 2 }
    expect(portIdentity(a)).not.toBe(portIdentity(b))
  })
})

describe('uniquePorts', () => {
  it('collapses the repeated sockets the agent can report', () => {
    const unique = uniquePorts(DUPLICATED)
    expect(unique).toHaveLength(3)
  })

  it('produces keys that are unique, so a keyed each cannot throw', () => {
    // This is the property the screens depend on: identical counts, not just
    // "fewer rows".
    const keys = uniquePorts(DUPLICATED).map(portIdentity)
    expect(new Set(keys).size).toBe(keys.length)
  })

  it('keeps the first occurrence and its order', () => {
    const unique = uniquePorts(DUPLICATED)
    expect(unique[0]).toEqual({ protocol: 'udp', local_address: '', port: 5353 })
    expect(unique[2]).toEqual({ protocol: 'tcp', local_address: '', port: 22 })
  })

  it('keeps two sockets that share an address but not an owner', () => {
    const ports: OpenPort[] = [
      { protocol: 'udp', local_address: '', port: 8125, process: 'telegraf', pid: 11 },
      { protocol: 'udp', local_address: '', port: 8125, process: 'statsd', pid: 22 }
    ]
    expect(uniquePorts(ports)).toHaveLength(2)
  })

  it('leaves an already unique list untouched', () => {
    const ports: OpenPort[] = [{ protocol: 'tcp', local_address: '', port: 22 }]
    expect(uniquePorts(ports)).toEqual(ports)
  })
})
