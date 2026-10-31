// Packet-level health, read from the kernel counters the agent already sends.
//
// The important distinction this module exists to preserve is "cumulative
// since boot" versus "happening now". A machine that has been up for a month
// can show six figures of UDP errors that all happened during one incident
// weeks ago; showing that number as a live alarm trains the operator to ignore
// the screen. Every reading here therefore takes an optional rate, computed
// from two consecutive agent samples, and says which one it is using.

import type { NetworkHealth, TCPStats, UDPStats } from './types'
import { conntrackUsage, retransTone } from './network'

export type HealthTone = 'ok' | 'warn' | 'crit'

// One agent sample. The timestamp comes from collected_at, not from when the
// browser received it, so a stalled device is not reported as fresh.
export type HealthSample = {
  at: string
  health: NetworkHealth
}

// Per-second movement between two samples. A counter that went backwards is a
// reset (reboot or interface reset), not a negative rate; it contributes zero.
export type HealthRates = {
  seconds: number
  tcpInErrors: number
  udpInErrors: number
  udpNoPorts: number
  udpRcvbuf: number
  udpSndbuf: number
  // UDP datagrams the host discarded: inbound errors (which already include
  // receive-buffer drops) plus send-buffer drops. NoPorts is counted separately
  // and is not an error in the same sense.
  udpDropped: number
  retransmissions: number
}

export type HealthRatePoint = {
  at: string
  tcp: number
  udp: number
  noPorts: number
  buffer: number
}

export function healthRates(previous?: HealthSample, current?: HealthSample): HealthRates | null {
  if (!previous || !current) return null
  const from = Date.parse(previous.at)
  const to = Date.parse(current.at)
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return null
  const seconds = (to - from) / 1000
  if (seconds <= 0) return null

  const delta = (before?: number, after?: number): number => {
    const a = before ?? 0
    const b = after ?? 0
    // A counter never decreases. If it does, the device restarted or the
    // interface did; the honest rate for that interval is zero, not a spike.
    return b >= a ? b - a : 0
  }

  const tcpInErrors = delta(previous.health.tcp?.in_errs, current.health.tcp?.in_errs)
  const udpInErrors = delta(previous.health.udp?.in_errors, current.health.udp?.in_errors)
  const udpNoPorts = delta(previous.health.udp?.no_ports, current.health.udp?.no_ports)
  const udpRcvbuf = delta(previous.health.udp?.rcvbuf_errors, current.health.udp?.rcvbuf_errors)
  const udpSndbuf = delta(previous.health.udp?.sndbuf_errors, current.health.udp?.sndbuf_errors)
  const retransmissions = delta(previous.health.tcp?.retrans_segs, current.health.tcp?.retrans_segs)

  return {
    seconds,
    tcpInErrors: tcpInErrors / seconds,
    udpInErrors: udpInErrors / seconds,
    udpNoPorts: udpNoPorts / seconds,
    udpRcvbuf: udpRcvbuf / seconds,
    udpSndbuf: udpSndbuf / seconds,
    udpDropped: (udpInErrors + udpSndbuf) / seconds,
    retransmissions: retransmissions / seconds
  }
}

// The rate series the inspector charts. One point per pair of samples, stamped
// with the newer sample so the x-axis matches the device clock.
export function healthRateSeries(samples: HealthSample[]): HealthRatePoint[] {
  const points: HealthRatePoint[] = []
  for (let i = 1; i < samples.length; i++) {
    const rates = healthRates(samples[i - 1], samples[i])
    if (!rates) continue
    points.push({
      at: samples[i].at,
      tcp: rates.tcpInErrors,
      udp: rates.udpDropped,
      noPorts: rates.udpNoPorts,
      buffer: rates.udpRcvbuf + rates.udpSndbuf
    })
  }
  return points
}

// The kernel's UDP counters are not additive:
//
//   in_errors      receive failures, and it already includes rcvbuf_errors
//                  (the kernel increments both when the socket buffer is full)
//   rcvbuf_errors  the subset dropped because the application did not read fast
//                  enough
//   sndbuf_errors  outgoing datagrams dropped because the send buffer was full
//   no_ports       datagrams that arrived for a port with no listener; counted
//                  separately and not part of in_errors
//
// Adding all four is the obvious mistake and inflates the number twice. This
// breakdown keeps the subsets labelled as subsets.
export type UdpBreakdown = {
  inDatagrams: number
  outDatagrams: number
  noListener: number
  inErrors: number
  receiveBuffer: number
  otherInbound: number
  sendBuffer: number
  dropped: number
  inboundShare: number
  noListenerShare: number
  sendShare: number
  droppedShare: number
}

export function udpBreakdown(health?: NetworkHealth): UdpBreakdown {
  const udp: UDPStats = health?.udp ?? {}
  const inDatagrams = udp.in_datagrams ?? 0
  const outDatagrams = udp.out_datagrams ?? 0
  const inErrors = udp.in_errors ?? 0
  const receiveBuffer = Math.min(udp.rcvbuf_errors ?? 0, inErrors)
  const otherInbound = Math.max(0, inErrors - receiveBuffer)
  const sendBuffer = udp.sndbuf_errors ?? 0
  const noListener = udp.no_ports ?? 0
  const inbound = inDatagrams + inErrors + noListener
  const outbound = outDatagrams + sendBuffer
  return {
    inDatagrams,
    outDatagrams,
    noListener,
    inErrors,
    receiveBuffer,
    otherInbound,
    sendBuffer,
    dropped: inErrors + sendBuffer,
    inboundShare: share(inErrors, inbound),
    noListenerShare: share(noListener, inbound),
    sendShare: share(sendBuffer, outbound),
    // The total spans both directions, so its share is against everything the
    // host handled, not just the inbound half.
    droppedShare: share(inErrors + sendBuffer, inbound + outbound)
  }
}

export type TcpBreakdown = {
  inSegments: number
  outSegments: number
  inErrors: number
  retransmissions: number
  retransPercent: number
  attemptFails: number
  estabResets: number
  outResets: number
  errorShare: number
}

export function tcpBreakdown(health?: NetworkHealth): TcpBreakdown {
  const tcp: TCPStats = health?.tcp ?? {}
  const inSegments = tcp.in_segs ?? 0
  const inErrors = tcp.in_errs ?? 0
  return {
    inSegments,
    outSegments: tcp.out_segs ?? 0,
    inErrors,
    retransmissions: tcp.retrans_segs ?? 0,
    retransPercent: tcp.retrans_percent ?? 0,
    attemptFails: tcp.attempt_fails ?? 0,
    estabResets: tcp.estab_resets ?? 0,
    outResets: tcp.out_rsts ?? 0,
    errorShare: share(inErrors, inSegments + inErrors)
  }
}

function share(part: number, total: number): number {
  if (!total || total <= 0) return 0
  return Math.min(100, (part / total) * 100)
}

export type HealthReading = {
  title: string
  detail: string
  tone: HealthTone
}

// healthReading is the one-line answer to "is this happening now?". It only
// reaches for a cumulative counter when it has no rate to compare against, and
// even then it does not call a lifetime error count a current failure.
export function healthReading(health?: NetworkHealth, rates?: HealthRates | null): HealthReading {
  if (!health) {
    return {
      title: 'No packet data reported yet',
      detail: 'The agent has not sent the kernel counters for this device.',
      tone: 'warn'
    }
  }

  const retransPercent = health.tcp?.retrans_percent ?? 0
  const retrans = retransTone(retransPercent)
  const conntrack = health.conntrack ? conntrackUsage(health.conntrack.count, health.conntrack.max) : 0
  const closeWait = health.sockets?.close_wait ?? 0
  const udp = udpBreakdown(health)
  const tcp = tcpBreakdown(health)
  // A lifetime loss share only means something when the denominator was
  // reported. Four in_errs with no in_segs is an older agent, not 100% loss.
  const cumulativeLoss = (udp.inDatagrams > 0 && udp.droppedShare >= 1)
    || (tcp.inSegments > 0 && tcp.errorShare >= 0.1)

  if (rates && rates.seconds > 0) {
    if (rates.tcpInErrors > 0) {
      return {
        title: 'TCP input errors are arriving now',
        detail: `${perSecond(rates.tcpInErrors)} since the previous sample; the kernel is discarding malformed or unchecksummed segments.`,
        tone: 'crit'
      }
    }
    if (rates.udpRcvbuf > 0) {
      return {
        title: 'UDP packets are being dropped at the receive buffer',
        detail: `${perSecond(rates.udpRcvbuf)}: an application is not reading its socket fast enough.`,
        tone: rates.udpRcvbuf >= 1 ? 'crit' : 'warn'
      }
    }
    if (retrans === 'crit') {
      return {
        title: 'Heavy packet loss on the path',
        detail: `${retransPercent.toFixed(2)}% of outgoing TCP segments have been retransmitted since boot.`,
        tone: 'crit'
      }
    }
    if (conntrack >= 80) {
      return {
        title: 'The connection table is nearly full',
        detail: `${conntrack}% of the conntrack table is in use.`,
        tone: 'crit'
      }
    }
    if (rates.udpInErrors > 0) {
      return {
        title: 'Malformed UDP datagrams are arriving now',
        detail: `${perSecond(rates.udpInErrors)} since the previous sample, outside the receive-buffer drops.`,
        tone: 'warn'
      }
    }
    if (rates.udpSndbuf > 0) {
      return {
        title: 'Outgoing UDP datagrams are being dropped',
        detail: `${perSecond(rates.udpSndbuf)}: the local send buffer was full.`,
        tone: 'warn'
      }
    }
    if (rates.udpNoPorts > 0) {
      return {
        title: 'Datagrams are arriving for ports with no listener',
        detail: `${perSecond(rates.udpNoPorts)} since the previous sample. Usually discovery traffic or a service that restarted; the packets are discarded, not queued.`,
        tone: 'warn'
      }
    }
    if (closeWait > 100) {
      return {
        title: 'Many sockets are stuck in CLOSE_WAIT',
        detail: `${closeWait.toLocaleString()} sockets are waiting for the application to close them.`,
        tone: 'warn'
      }
    }
    if (retrans === 'warn') {
      return {
        title: 'Some TCP packets are being retransmitted',
        detail: `${retransPercent.toFixed(2)}% since boot; the path is losing packets.`,
        tone: 'warn'
      }
    }
    const cumulative = (health.tcp?.in_errs ?? 0)
      + (health.udp?.in_errors ?? 0)
      + (health.udp?.no_ports ?? 0)
      + (health.udp?.sndbuf_errors ?? 0)
    return {
      title: 'No new packet errors',
      detail: cumulative > 0
        ? `Nothing moved during the last ${Math.round(rates.seconds)}s, though the cumulative counters below are not zero.`
        : `Nothing moved during the last ${Math.round(rates.seconds)}s, and the cumulative counters are still zero.`,
      tone: 'ok'
    }
  }

  // One sample is not enough to separate history from a current failure. The
  // old screen treated a lifetime in_errs of 4 as a red alarm forever, which is
  // how a real incident becomes background noise.
  if (conntrack >= 80) {
    return { title: 'The connection table is nearly full', detail: `${conntrack}% of the conntrack table is in use.`, tone: 'crit' }
  }
  if (retrans === 'crit') {
    return {
      title: 'Heavy packet loss on the path',
      detail: `${retransPercent.toFixed(2)}% of outgoing TCP segments have been retransmitted since boot.`,
      tone: 'crit'
    }
  }
  if (closeWait > 100) {
    return { title: 'Many sockets are stuck in CLOSE_WAIT', detail: `${closeWait.toLocaleString()} sockets are waiting for the application to close them.`, tone: 'warn' }
  }
  if (retrans === 'warn') {
    return {
      title: 'Some TCP packets are being retransmitted',
      detail: `${retransPercent.toFixed(2)}% since boot; watching for the next sample.`,
      tone: 'warn'
    }
  }
  if (cumulativeLoss) {
    return {
      title: 'Cumulative packet errors are high',
      detail: `${udp.droppedShare.toFixed(2)}% of UDP datagrams have been dropped since boot. It may be old, but it is too large to call healthy; the next sample shows whether it is still moving.`,
      tone: 'warn'
    }
  }
  return {
    title: 'The stack looks healthy',
    detail: 'Cumulative error counters are history, not a live alarm. The next sample says whether they are still moving.',
    tone: 'ok'
  }
}

export function perSecond(value: number): string {
  if (!Number.isFinite(value) || value < 0) return '0/s'
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(2)}/s`
}

export type HealthAdvice = {
  id: string
  title: string
  body: string
  command?: string
}

// healthAdvice turns the counters into the next diagnostic step. It is
// deliberately specific about what each counter can and cannot prove: the
// agent is unprivileged, so it cannot attribute a packet to a process.
export function healthAdvice(health?: NetworkHealth, rates?: HealthRates | null): HealthAdvice[] {
  if (!health) {
    return [{
      id: 'none',
      title: 'No counters to inspect',
      body: 'This device has not reported the packet counters. Check that its agent is current and online.'
    }]
  }
  const udp = udpBreakdown(health)
  const tcp = tcpBreakdown(health)
  const advice: HealthAdvice[] = []

  if (tcp.inErrors > 0) {
    advice.push({
      id: 'tcp-errors',
      title: 'TCP input errors are usually damaged frames',
      body: `${tcp.inErrors.toLocaleString()} segments were rejected since boot (${tcp.errorShare.toFixed(3)}% of inbound TCP). Checksum failures point at the link, the driver or a middlebox; compare with the interface error counters above.`,
      command: 'nstat -az TcpInErrs TcpRetransSegs'
    })
  }
  if (udp.receiveBuffer > 0) {
    advice.push({
      id: 'udp-rcvbuf',
      title: 'An application is not reading its UDP socket fast enough',
      body: `${udp.receiveBuffer.toLocaleString()} datagrams were dropped because the receive buffer was full. The packets never reached the application; this is a consumer problem, not a network one. The process may be busy, blocked or using a tiny SO_RCVBUF.`,
      command: 'sudo ss -uanp'
    })
  }
  if (udp.otherInbound > 0) {
    advice.push({
      id: 'udp-inerrors',
      title: 'Some UDP datagrams failed before delivery',
      body: `${udp.otherInbound.toLocaleString()} inbound datagrams failed for a reason other than a full receive buffer (typically checksum or format). Check the interface error counters and the sending host.`,
      command: 'nstat -az UdpInErrors UdpInCsumErrors'
    })
  }
  if (udp.sendBuffer > 0) {
    advice.push({
      id: 'udp-sndbuf',
      title: 'The local send buffer is full',
      body: `${udp.sendBuffer.toLocaleString()} outgoing datagrams were dropped because the socket could not queue them. The application is sending faster than the kernel or the link can drain.`,
      command: 'sysctl net.core.wmem_default net.core.wmem_max'
    })
  }
  if (udp.noListener > 0) {
    advice.push({
      id: 'udp-noports',
      title: 'Datagrams are arriving with no listening socket',
      body: `${udp.noListener.toLocaleString()} datagrams (${udp.noListenerShare.toFixed(2)}% of inbound UDP) had no matching socket. Discovery protocols, broadcast chatter and a service that just restarted are the usual sources. These are discarded immediately; they only matter if the volume is growing and something expected to listen is down.`,
      command: 'sudo ss -lunp'
    })
  }
  if (tcp.retransPercent >= 2) {
    advice.push({
      id: 'retrans',
      title: 'The path is losing packets',
      body: `${tcp.retransPercent.toFixed(2)}% of outgoing TCP segments have been retransmitted since boot. Above a couple of percent the link or a hop is dropping traffic. Check the interface counters and, on Wi-Fi, the signal.`,
      command: 'nstat -az TcpRetransSegs'
    })
  }
  if ((health.sockets?.close_wait ?? 0) > 100) {
    advice.push({
      id: 'close-wait',
      title: 'Applications are not closing their sockets',
      body: `${(health.sockets?.close_wait ?? 0).toLocaleString()} sockets are in CLOSE_WAIT, which means the peer closed and the local application has not. This is an application bug or a stuck worker, and it consumes file descriptors.`,
      command: 'ss -tan state close-wait'
    })
  }
  if (health.conntrack && conntrackUsage(health.conntrack.count, health.conntrack.max) >= 60) {
    advice.push({
      id: 'conntrack',
      title: 'The connection table is filling up',
      body: `${health.conntrack.count.toLocaleString()} of ${health.conntrack.max.toLocaleString()} entries are in use. When it fills, new connections are dropped. Look for a scanner, a leak or too many short-lived connections.`,
      command: 'conntrack -C; sysctl net.netfilter.nf_conntrack_max'
    })
  }

  if (advice.length === 0) {
    advice.push({
      id: 'quiet',
      title: 'Nothing is moving in this sample',
      body: rates && rates.seconds > 0
        ? `No new packet errors arrived during the last ${Math.round(rates.seconds)}s. The cumulative counts below are since boot; they are history, not a current alarm.`
        : 'The counters are cumulative since boot. Wait for the next sample to see whether they are still moving.'
    })
  }
  return advice
}
