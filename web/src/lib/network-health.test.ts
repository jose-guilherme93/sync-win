import { describe, expect, it } from 'vitest'
import {
  healthAdvice,
  healthRates,
  healthRateSeries,
  healthReading,
  perSecond,
  tcpBreakdown,
  udpBreakdown,
  type HealthSample
} from './network-health'
import type { NetworkHealth } from './types'

function health(overrides: Partial<NetworkHealth> = {}): NetworkHealth {
  return {
    tcp: {},
    udp: {},
    sockets: {},
    collected_at: '2026-10-28T12:00:00Z',
    ...overrides
  }
}

function sample(at: string, overrides: Partial<NetworkHealth> = {}): HealthSample {
  return { at, health: health(overrides) }
}

describe('healthRates', () => {
  it('turns two cumulative samples into per-second movement', () => {
    const previous = sample('2026-10-28T12:00:00Z', {
      tcp: { in_errs: 10, retrans_segs: 50 },
      udp: { in_errors: 20, no_ports: 100, rcvbuf_errors: 5, sndbuf_errors: 2 }
    })
    const current = sample('2026-10-28T12:00:10Z', {
      tcp: { in_errs: 30, retrans_segs: 70 },
      udp: { in_errors: 40, no_ports: 300, rcvbuf_errors: 15, sndbuf_errors: 6 }
    })

    const rates = healthRates(previous, current)!
    expect(rates.seconds).toBe(10)
    expect(rates.tcpInErrors).toBe(2)
    expect(rates.udpInErrors).toBe(2)
    expect(rates.udpNoPorts).toBe(20)
    expect(rates.udpRcvbuf).toBe(1)
    expect(rates.udpSndbuf).toBeCloseTo(0.4)
    expect(rates.retransmissions).toBe(2)
  })

  it('does not add receive-buffer drops twice', () => {
    // The kernel already counts rcvbuf_errors inside in_errors. Two inbound
    // drops and four send failures over ten seconds is 0.6/s, not 1.6/s.
    const previous = sample('2026-10-28T12:00:00Z', {
      udp: { in_errors: 100, rcvbuf_errors: 80, sndbuf_errors: 10 }
    })
    const current = sample('2026-10-28T12:00:10Z', {
      udp: { in_errors: 120, rcvbuf_errors: 90, sndbuf_errors: 14 }
    })

    const rates = healthRates(previous, current)!
    expect(rates.udpInErrors).toBe(2)
    expect(rates.udpRcvbuf).toBe(1)
    expect(rates.udpDropped).toBeCloseTo(2.4)
  })

  it('treats a counter reset as zero instead of a negative spike', () => {
    const previous = sample('2026-10-28T12:00:00Z', { tcp: { in_errs: 900, retrans_segs: 500 } })
    const current = sample('2026-10-28T12:00:10Z', { tcp: { in_errs: 3, retrans_segs: 1 } })

    const rates = healthRates(previous, current)!
    expect(rates.tcpInErrors).toBe(0)
    expect(rates.retransmissions).toBe(0)
  })

  it('refuses a missing or non-advancing timestamp', () => {
    const a = sample('2026-10-28T12:00:00Z')
    const same = sample('2026-10-28T12:00:00Z')
    const backwards = sample('2026-10-28T11:59:59Z')
    expect(healthRates(a, same)).toBeNull()
    expect(healthRates(a, backwards)).toBeNull()
    expect(healthRates(undefined, a)).toBeNull()
  })
})

describe('healthRateSeries', () => {
  it('emits one point per pair of samples, stamped with the newer sample', () => {
    const samples = [
      sample('2026-10-28T12:00:00Z', { udp: { in_errors: 0, no_ports: 0 } }),
      sample('2026-10-28T12:00:10Z', { udp: { in_errors: 10, no_ports: 100 } }),
      sample('2026-10-28T12:00:20Z', { udp: { in_errors: 30, no_ports: 300 } })
    ]

    const points = healthRateSeries(samples)
    expect(points).toHaveLength(2)
    expect(points[0].at).toBe('2026-10-28T12:00:10Z')
    expect(points[0].udp).toBe(1)
    expect(points[0].noPorts).toBe(10)
    expect(points[1].udp).toBe(2)
    expect(points[1].noPorts).toBe(20)
  })
})

describe('UDP breakdown', () => {
  it('labels the subsets instead of adding every counter', () => {
    const udp = udpBreakdown(health({
      udp: {
        in_datagrams: 10_000,
        out_datagrams: 5_000,
        in_errors: 100,
        rcvbuf_errors: 80,
        sndbuf_errors: 5,
        no_ports: 1_000
      }
    }))

    expect(udp.noListener).toBe(1_000)
    expect(udp.receiveBuffer).toBe(80)
    expect(udp.otherInbound).toBe(20)
    expect(udp.sendBuffer).toBe(5)
    expect(udp.inErrors).toBe(100)
    // 100 inbound errors + 5 send failures. The 80 receive-buffer drops are
    // already inside the 100 and must not be counted again.
    expect(udp.dropped).toBe(105)
    expect(udp.dropped).not.toBe(100 + 80 + 5)
    expect(udp.noListenerShare).toBeCloseTo(1000 / 11_100 * 100)
    expect(udp.inboundShare).toBeCloseTo(100 / 11_100 * 100)
    expect(udp.sendShare).toBeCloseTo(5 / 5_005 * 100)
    expect(udp.droppedShare).toBeCloseTo(105 / (11_100 + 5_005) * 100)
  })

  it('never reports a receive buffer larger than the error total', () => {
    const udp = udpBreakdown(health({ udp: { in_errors: 10, rcvbuf_errors: 40 } }))
    expect(udp.receiveBuffer).toBe(10)
    expect(udp.otherInbound).toBe(0)
  })

  it('is all zeros without counters', () => {
    const udp = udpBreakdown(undefined)
    expect(udp.dropped).toBe(0)
    expect(udp.noListener).toBe(0)
    expect(udp.inboundShare).toBe(0)
    expect(udp.droppedShare).toBe(0)
  })
})

describe('TCP breakdown', () => {
  it('reports the error share against inbound segments', () => {
    const tcp = tcpBreakdown(health({ tcp: { in_segs: 1_000, in_errs: 10, retrans_segs: 30, retrans_percent: 1.5 } }))
    expect(tcp.inErrors).toBe(10)
    expect(tcp.errorShare).toBeCloseTo((10 / 1_010) * 100)
    expect(tcp.retransPercent).toBe(1.5)
  })
})

describe('health reading', () => {
  it('says when nothing was reported', () => {
    expect(healthReading(undefined).tone).toBe('warn')
  })

  it('does not call a lifetime error count a current failure', () => {
    // This was the regression in the screenshot: four TCP input errors since
    // boot turned the tile red on every render, forever.
    const reading = healthReading(health({ tcp: { in_errs: 4 } }))
    expect(reading.tone).toBe('ok')
    expect(reading.detail).toMatch(/cumulative/i)
  })

  it('warns when the lifetime loss share is too large to call healthy', () => {
    const reading = healthReading(health({
      udp: { in_datagrams: 1_000_000, in_errors: 262_366, rcvbuf_errors: 262_000, no_ports: 1_200 }
    }))
    expect(reading.tone).toBe('warn')
    expect(reading.title).toMatch(/cumulative packet errors are high/i)
  })

  it('does not invent a loss share when the denominator was not reported', () => {
    // An older agent can send in_errs without in_segs; 4/4 is not 100% loss.
    expect(healthReading(health({ tcp: { in_errs: 4 } })).tone).toBe('ok')
  })

  it('still alarms on cumulative loss and a full connection table', () => {
    expect(healthReading(health({ tcp: { retrans_percent: 15 } })).tone).toBe('crit')
    expect(healthReading(health({ conntrack: { count: 900, max: 1000 } })).tone).toBe('crit')
    expect(healthReading(health({ tcp: { retrans_percent: 4 } })).tone).toBe('warn')
  })

  it('uses the rate when it has one', () => {
    const quiet = healthRates(
      sample('2026-10-28T12:00:00Z', { udp: { in_errors: 262_366, no_ports: 12 } }),
      sample('2026-10-28T12:00:10Z', { udp: { in_errors: 262_366, no_ports: 12 } })
    )
    const reading = healthReading(health({ udp: { in_errors: 262_366, no_ports: 12 } }), quiet)
    expect(reading.tone).toBe('ok')
    expect(reading.title).toMatch(/No new packet errors/i)

    const tcpNow = healthRates(
      sample('2026-10-28T12:00:00Z', { tcp: { in_errs: 0 } }),
      sample('2026-10-28T12:00:10Z', { tcp: { in_errs: 10 } })
    )
    const tcpReading = healthReading(health({ tcp: { in_errs: 10 } }), tcpNow)
    expect(tcpReading.tone).toBe('crit')
    expect(tcpReading.title).toMatch(/TCP input errors are arriving/i)
  })

  it('keeps no-listener datagrams out of the critical bucket', () => {
    const rates = healthRates(
      sample('2026-10-28T12:00:00Z', { udp: { no_ports: 0 } }),
      sample('2026-10-28T12:00:10Z', { udp: { no_ports: 100 } })
    )
    const reading = healthReading(health({ udp: { no_ports: 100 } }), rates)
    expect(reading.tone).toBe('warn')
    expect(reading.title).toMatch(/no listener/i)
  })

  it('calls a full receive buffer critical when it is actively dropping', () => {
    const rates = healthRates(
      sample('2026-10-28T12:00:00Z', { udp: { in_errors: 0, rcvbuf_errors: 0 } }),
      sample('2026-10-28T12:00:10Z', { udp: { in_errors: 40, rcvbuf_errors: 20 } })
    )
    const reading = healthReading(health({ udp: { in_errors: 40, rcvbuf_errors: 20 } }), rates)
    expect(reading.tone).toBe('crit')
    expect(reading.title).toMatch(/receive buffer/i)
  })
})

describe('health advice', () => {
  it('names the no-listener case and the command that finds the listeners', () => {
    const advice = healthAdvice(health({ udp: { no_ports: 1_200 } }))
    const item = advice.find((entry) => entry.id === 'udp-noports')
    expect(item).toBeTruthy()
    expect(item?.body).toMatch(/no matching socket/i)
    expect(item?.command).toContain('ss -lunp')
  })

  it('explains a full receive buffer as a consumer problem', () => {
    const advice = healthAdvice(health({ udp: { in_errors: 100, rcvbuf_errors: 80 } }))
    const item = advice.find((entry) => entry.id === 'udp-rcvbuf')
    expect(item).toBeTruthy()
    expect(item?.body).toMatch(/never reached the application/i)
    expect(item?.command).toContain('ss -uanp')
  })

  it('says the cumulative counters are history when nothing is moving', () => {
    const advice = healthAdvice(health({ udp: { in_errors: 262_366 } }))
    // The error count alone still produces the TCP/UDP checks, so use a quiet
    // health block for the "nothing to do" case.
    const quiet = healthAdvice(health())
    expect(quiet.find((entry) => entry.id === 'quiet')).toBeTruthy()
    expect(quiet[0].body).toMatch(/cumulative since boot/i)
    expect(advice.length).toBeGreaterThan(0)
  })

  it('tells the operator when the device reported nothing', () => {
    expect(healthAdvice(undefined)[0].id).toBe('none')
  })
})

describe('perSecond', () => {
  it('keeps small rates readable and large ones compact', () => {
    expect(perSecond(0)).toBe('0.00/s')
    expect(perSecond(1.234)).toBe('1.23/s')
    expect(perSecond(42)).toBe('42/s')
  })
})
