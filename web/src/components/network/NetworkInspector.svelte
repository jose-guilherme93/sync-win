<script lang="ts">
  import type { Section } from '../../lib/router'
  import { formatBytes, formatRate } from '../../lib/format'
  import type {
    Device,
    NetworkHistory,
    NetworkHistoryPoint,
    NetworkIface,
    NetworkSection
  } from '../../lib/types'
  import { exposureOf, interfaceKindLabel, summarizeExposure, type NetworkLens } from '../../lib/network'
  import {
    healthAdvice,
    healthRateSeries,
    healthRates,
    healthReading,
    perSecond,
    tcpBreakdown,
    udpBreakdown,
    type HealthSample
  } from '../../lib/network-health'
  import type { OpenPort } from '../../lib/ports'
  import EmptyState from '../ui/EmptyState.svelte'
  import TimeSeriesChart from '../ui/TimeSeriesChart.svelte'

  export let lens: NetworkLens | null = null
  export let device: Device
  export let healthSamples: HealthSample[] = []
  export let history: NetworkHistory | null = null
  export let rangeLabel = '24h'
  export let ports: OpenPort[] = []
  export let portsLoaded = false
  export let portsError = ''
  export let topology: NetworkSection | null = null
  export let ifaceSeries: Record<string, { rx: number[]; tx: number[] }> = {}
  export let liveStamps: string[] = []
  export let onClose: () => void = () => {}
  export let onNavigate: (section: Section) => void = () => {}

  const TITLES: Record<NetworkLens, string> = {
    download: 'Download',
    upload: 'Upload',
    peak: `Peak · ${rangeLabel}`,
    links: 'Links',
    health: 'Packet health',
    exposure: 'Reachable from the network'
  }

  const SERIES = ['var(--accent)', 'var(--series-1)', 'var(--series-2)', 'var(--series-3)', 'var(--series-4)', 'var(--series-5)']

  let closeButton: HTMLButtonElement | null = null
  let drawerElement: HTMLDivElement | null = null
  let lastFocused: HTMLElement | null = null
  let wasOpen = false
  let copied = ''

  // Focus moves into the drawer and back to the tile that opened it. Without
  // the restore, closing with Escape leaves focus on a detached button and the
  // next Tab starts from the top of the page.
  $: if (lens && !wasOpen) {
    wasOpen = true
    lastFocused = typeof document !== 'undefined' ? (document.activeElement as HTMLElement | null) : null
    queueMicrotask(() => closeButton?.focus())
  }
  $: if (!lens && wasOpen) {
    wasOpen = false
    lastFocused?.focus?.()
    lastFocused = null
  }

  function onKeydown(event: KeyboardEvent) {
    if (!lens) return
    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
      return
    }
    if (event.key !== 'Tab' || !drawerElement) return
    const focusable = [...drawerElement.querySelectorAll<HTMLElement>(
      'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'
    )]
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }

  async function copyCommand(id: string, command: string) {
    try {
      await navigator.clipboard.writeText(command)
      copied = id
      setTimeout(() => {
        if (copied === id) copied = ''
      }, 2200)
    } catch {
      // Clipboard permission can be denied; the command stays selectable.
    }
  }

  $: hw = device.hardware
  $: ifaces = (hw?.network_ifaces ?? []).slice().sort((a, b) => {
    const av = (a.rx_rate || 0) + (a.tx_rate || 0)
    const bv = (b.rx_rate || 0) + (b.tx_rate || 0)
    if (bv !== av) return bv - av
    return a.name.localeCompare(b.name)
  })
  $: counted = ifaces.filter((iface) => iface.counted !== false)
  $: health = hw?.network_health
  $: latestRates = healthRates(healthSamples.at(-2), healthSamples.at(-1))
  $: reading = healthReading(health, latestRates)
  $: ratePoints = healthRateSeries(healthSamples)
  $: udp = udpBreakdown(health)
  $: tcp = tcpBreakdown(health)
  $: advice = healthAdvice(health, latestRates)
  $: exposure = summarizeExposure(ports)
  $: exposed = ports
    .filter((port) => {
      const value = exposureOf(port)
      return value === 'all' || value === 'specific'
    })
    .sort((a, b) => (exposureOf(a) === 'all' ? -1 : 0) - (exposureOf(b) === 'all' ? -1 : 0) || a.port - b.port)
  $: topo = topology?.topology
  $: gateway = topo?.default_gateway || ''
  $: resolvers = topo?.dns ?? []
  $: interval = device.collection_interval_seconds || 10
  $: sampleAge = ageOf(health?.collected_at ?? '')

  $: totalRx = hw?.net_rx_rate ?? counted.reduce((sum, iface) => sum + (iface.rx_rate || 0), 0)
  $: totalTx = hw?.net_tx_rate ?? counted.reduce((sum, iface) => sum + (iface.tx_rate || 0), 0)
  $: totalRxBytes = counted.reduce((sum, iface) => sum + iface.rx_bytes, 0)
  $: totalTxBytes = counted.reduce((sum, iface) => sum + iface.tx_bytes, 0)

  $: direction = (lens === 'upload' ? 'tx' : 'rx') as 'rx' | 'tx'
  $: liveDirection = direction === 'rx' ? totalRx : totalTx
  $: liveDirectionBytes = direction === 'rx' ? totalRxBytes : totalTxBytes
  $: liveSeries = counted
    .map((iface, index) => ({
      label: iface.name,
      values: (ifaceSeries[iface.name]?.[direction] ?? []) as number[],
      color: SERIES[ifaces.findIndex((item) => item.name === iface.name) % SERIES.length],
      iface,
      index
    }))
    .filter((entry) => entry.values.some((value: number) => value > 0))
    .slice(0, 6)
  $: peakRows = peakRowsFor(history?.interfaces ?? {})
  $: overallPeak = peakRows.reduce((best, row) => {
    const value = direction === 'rx' ? row.rx : row.tx
    return value > best.value ? { value, at: direction === 'rx' ? row.rxAt : row.txAt, iface: row.name } : best
  }, { value: 0, at: '', iface: '' })

  $: healthSeries = [
    { label: 'UDP dropped', values: ratePoints.map((point) => point.udp), color: 'var(--crit)', format: perSecond },
    { label: 'No listener', values: ratePoints.map((point) => point.noPorts), color: 'var(--warn)', format: perSecond },
    { label: 'TCP errors', values: ratePoints.map((point) => point.tcp), color: 'var(--series-1)', format: perSecond }
  ]
  $: healthLabels = ratePoints.map((point) => point.at)

  function ageOf(stamp: string): string {
    if (!stamp) return 'never'
    const parsed = Date.parse(stamp)
    if (!Number.isFinite(parsed)) return 'never'
    const diff = Math.max(0, Date.now() - parsed)
    if (diff < 60_000) return 'just now'
    if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
    return `${Math.floor(diff / 3_600_000)}h ago`
  }

  function clockOf(stamp: string): string {
    if (!stamp) return ''
    const parsed = new Date(stamp)
    if (Number.isNaN(parsed.getTime())) return ''
    return parsed.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }

  function share(part: number, total: number): number {
    if (!total || total <= 0) return 0
    return Math.min(100, Math.max(0, (part / total) * 100))
  }

  function addresses(iface: NetworkIface): string[] {
    return (iface.addresses ?? []).filter(Boolean)
  }

  function peakRowsFor(interfaces: NetworkHistory['interfaces']): { name: string; rx: number; rxAt: string; tx: number; txAt: string }[] {
    return Object.entries(interfaces)
      .map(([name, points]) => {
        const rx = peakOf(points, 'rx')
        const tx = peakOf(points, 'tx')
        return { name, rx: rx.value, rxAt: rx.at, tx: tx.value, txAt: tx.at }
      })
      .sort((a, b) => (direction === 'rx' ? b.rx - a.rx : b.tx - a.tx))
  }

  function peakOf(points: NetworkHistoryPoint[], dir: 'rx' | 'tx'): { value: number; at: string } {
    let value = 0
    let at = ''
    for (const point of points) {
      const current = dir === 'rx' ? point.rx_rate : point.tx_rate
      if (current > value) {
        value = current
        at = point.timestamp
      }
    }
    return { value, at }
  }

  function toneClass(tone: 'ok' | 'warn' | 'crit'): string {
    return tone === 'crit' ? 'crit' : tone === 'warn' ? 'warn' : 'ok'
  }
</script>

<svelte:window on:keydown={onKeydown} />

{#if lens}
  <div class="backdrop" role="presentation" on:click={onClose}></div>
  <div bind:this={drawerElement} class="drawer" role="dialog" aria-modal="true" aria-labelledby="inspector-title">
    <header class="drawer-head">
      <div class="drawer-title">
        <h2 id="inspector-title">{TITLES[lens]}</h2>
        <span class="scope">
          Live · sample {lens === 'health' ? sampleAge : ageOf(hw?.collected_at ?? '')}
          {#if lens === 'health' && latestRates && latestRates.seconds > 0}
            · compared over {latestRates.seconds.toFixed(0)}s
          {/if}
        </span>
      </div>
      <button bind:this={closeButton} class="close" on:click={onClose} aria-label="Close inspector">✕</button>
    </header>

    <div class="drawer-body">
      {#if lens === 'download' || lens === 'upload'}
        <div class="hero">
          <span class="hero-value">{formatRate(liveDirection)}</span>
          <span class="hero-label">{lens === 'download' ? 'download' : 'upload'} now</span>
          <span class="hero-sub">
            {formatBytes(liveDirectionBytes)} since boot
            {#if overallPeak.value > 0}· {rangeLabel} peak {formatRate(overallPeak.value)}{/if}
          </span>
        </div>

        <article class="block">
          <header class="block-head">
            <h3>Live contributions</h3>
            <span class="hint">{liveSeries.length} of {counted.length} links</span>
          </header>
          {#if liveStamps.length < 2}
            <p class="note">The chart appears after the second sample. The agent reports every {interval}s.</p>
          {:else}
            <TimeSeriesChart
              labels={liveStamps}
              series={liveSeries.map(({ label, values, color }) => ({ label, values, color, format: formatRate }))}
              leftUnit=""
              leftFormat={formatRate}
              height={220}
              emptyMessage="No interface reported traffic in this window."
            />
          {/if}
        </article>

        <article class="block">
          <header class="block-head">
            <h3>Per-link breakdown</h3>
            <span class="hint">rate · share of total</span>
          </header>
          {#if counted.length === 0}
            <EmptyState icon="🌐" title="No counted link" message="The agent reported only container or virtual interfaces." />
          {:else}
            <table class="table">
              <thead>
                <tr><th>Link</th><th class="num">Now</th><th>Share</th><th class="num">Since boot</th><th class="num">Errors</th></tr>
              </thead>
              <tbody>
                {#each counted as iface (iface.name)}
                  {@const rate = direction === 'rx' ? (iface.rx_rate || 0) : (iface.tx_rate || 0)}
                  {@const rateTotal = direction === 'rx' ? totalRx : totalTx}
                  <tr>
                    <td class="mono">{iface.name}</td>
                    <td class="num">{formatRate(rate)}</td>
                    <td>
                      <span class="share"><i style="width: {share(rate, rateTotal)}%"></i></span>
                      <span class="share-value">{share(rate, rateTotal).toFixed(0)}%</span>
                    </td>
                    <td class="num">{formatBytes(direction === 'rx' ? iface.rx_bytes : iface.tx_bytes)}</td>
                    <td class="num" class:text-crit={(iface.rx_errors || 0) + (iface.tx_errors || 0) > 0}>
                      {(iface.rx_errors || 0) + (iface.tx_errors || 0)}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
          <p class="note">
            Rates are deltas of the kernel counters between agent samples; totals exclude container and virtual
            links so the same bytes are not counted twice. Per-process attribution is not available: the agent
            runs unprivileged and cannot see another user's socket owner.
          </p>
        </article>
      {:else if lens === 'peak'}
        <div class="hero">
          <span class="hero-value">{formatRate(overallPeak.value)}</span>
          <span class="hero-label">{direction === 'rx' ? 'download' : 'upload'} peak · {rangeLabel}</span>
          <span class="hero-sub">
            {#if overallPeak.value > 0}
              {overallPeak.iface} · {clockOf(overallPeak.at)}
            {:else}
              no sample in this window
            {/if}
          </span>
        </div>

        <article class="block">
          <header class="block-head">
            <h3>Peak per link</h3>
            <span class="hint">{peakRows.length} recorded</span>
          </header>
          {#if peakRows.length === 0}
            <EmptyState
              icon="📈"
              title="No traffic history for this window"
              message="The server records one sample per link per minute. A quiet link still appears once it reports."
            />
          {:else}
            <table class="table">
              <thead>
                <tr><th>Link</th><th class="num">Download</th><th>At</th><th class="num">Upload</th><th>At</th></tr>
              </thead>
              <tbody>
                {#each peakRows as row (row.name)}
                  <tr>
                    <td class="mono">{row.name}</td>
                    <td class="num">{formatRate(row.rx)}</td>
                    <td class="dim">{clockOf(row.rxAt) || '—'}</td>
                    <td class="num">{formatRate(row.tx)}</td>
                    <td class="dim">{clockOf(row.txAt) || '—'}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
          <p class="note">
            History keeps one sample per link per minute, rolled into five-minute buckets after two days. This is
            the busiest bucket average, not an instantaneous spike; the live series above shows the last few
            minutes at the agent's own interval.
          </p>
        </article>
      {:else if lens === 'links'}
        <div class="hero">
          <span class="hero-value">{ifaces.filter((iface) => (iface.oper_state || '') === 'up').length}<small> / {ifaces.length} up</small></span>
          <span class="hero-label">interfaces</span>
          <span class="hero-sub">
            {counted.length} in the totals
            {#if ifaces.length > counted.length}· {ifaces.length - counted.length} container/virtual{/if}
          </span>
        </div>

        <article class="block">
          <header class="block-head">
            <h3>Every reported link</h3>
            <span class="hint">including excluded plumbing</span>
          </header>
          {#if ifaces.length === 0}
            <EmptyState icon="🌐" title="No interfaces reported" message="The agent found no network interface to report." />
          {:else}
            <table class="table">
              <thead>
                <tr><th>Link</th><th>Kind / state</th><th class="num">Speed</th><th class="num">Down</th><th class="num">Up</th><th>Totals</th></tr>
              </thead>
              <tbody>
                {#each ifaces as iface (iface.name)}
                  <tr class:dim-row={iface.counted === false}>
                    <td>
                      <div class="mono strong">{iface.name}</div>
                      {#if addresses(iface).length}
                        <div class="addresses">{addresses(iface).join(' · ')}</div>
                      {/if}
                    </td>
                    <td>
                      <div>{interfaceKindLabel(iface.kind)}</div>
                      <div class="dim">{iface.oper_state || 'unknown'}</div>
                    </td>
                    <td class="num">{iface.speed_mbps ? `${iface.speed_mbps} Mb/s` : '—'}</td>
                    <td class="num">{formatRate(iface.rx_rate || 0)}</td>
                    <td class="num">{formatRate(iface.tx_rate || 0)}</td>
                    <td>
                      {#if iface.counted === false}
                        <span class="chip muted">excluded</span>
                      {:else}
                        <span class="chip">counted</span>
                      {/if}
                    </td>
                  </tr>
                  {#if (iface.rx_errors || 0) + (iface.tx_errors || 0) + (iface.rx_dropped || 0) + (iface.tx_dropped || 0) > 0}
                    <tr class="error-row">
                      <td colspan="6">
                        <span class="text-crit">{(iface.rx_errors || 0) + (iface.tx_errors || 0)} errors</span>
                        · <span class="text-warn">{(iface.rx_dropped || 0) + (iface.tx_dropped || 0)} dropped</span>
                        {#if (iface.carrier_changes || 0) > 0}· {iface.carrier_changes} carrier changes{/if}
                        {#if (iface.rx_collisions || 0) > 0}· {iface.rx_collisions} collisions{/if}
                        {#if (iface.rx_fifo_errors || 0) + (iface.tx_fifo_errors || 0) > 0}
                          · {(iface.rx_fifo_errors || 0) + (iface.tx_fifo_errors || 0)} FIFO
                        {/if}
                      </td>
                    </tr>
                  {/if}
                {/each}
              </tbody>
            </table>
          {/if}
          <p class="note">
            Container bridges, veth pairs and loopback appear here but are excluded from the screen's totals: the
            same traffic crosses a veth, its bridge and docker0, so summing them multiplies the number.
          </p>
        </article>
      {:else if lens === 'health'}
        <div class="hero reading {toneClass(reading.tone)}">
          <span class="hero-value value-reading">{reading.title}</span>
          <span class="hero-label">{reading.detail}</span>
          <span class="hero-sub">
            {#if latestRates && latestRates.seconds > 0}
              Compared with the previous sample {latestRates.seconds.toFixed(0)}s earlier.
            {:else}
              Waiting for a second sample to separate history from a current failure.
            {/if}
            Counters are cumulative since boot.
          </span>
        </div>

        <article class="block">
          <header class="block-head">
            <h3>Happening now</h3>
            <span class="hint">per second between samples</span>
          </header>
          {#if latestRates && latestRates.seconds > 0}
            <div class="rate-grid">
              <div class="rate-cell">
                <span class="label">UDP dropped</span>
                <span class="rate-value" class:text-crit={latestRates.udpDropped > 0}>{perSecond(latestRates.udpDropped)}</span>
                <span class="sub">receive + send buffer</span>
              </div>
              <div class="rate-cell">
                <span class="label">No listener</span>
                <span class="rate-value" class:text-warn={latestRates.udpNoPorts > 0}>{perSecond(latestRates.udpNoPorts)}</span>
                <span class="sub">discarded, not queued</span>
              </div>
              <div class="rate-cell">
                <span class="label">Receive buffer</span>
                <span class="rate-value" class:text-crit={latestRates.udpRcvbuf > 0}>{perSecond(latestRates.udpRcvbuf)}</span>
                <span class="sub">subset of drops</span>
              </div>
              <div class="rate-cell">
                <span class="label">TCP input errors</span>
                <span class="rate-value" class:text-crit={latestRates.tcpInErrors > 0}>{perSecond(latestRates.tcpInErrors)}</span>
                <span class="sub">damaged segments</span>
              </div>
            </div>
          {:else}
            <p class="note">The first sample only establishes a baseline. The next one, every {interval}s, says what is moving.</p>
          {/if}
          {#if ratePoints.length > 1}
            <TimeSeriesChart
              labels={healthLabels}
              series={healthSeries}
              leftUnit=""
              leftFormat={perSecond}
              height={200}
              emptyMessage="No packet movement in the samples seen since this screen opened."
            />
          {:else}
            <p class="note">A line needs two rate points: keep the screen open for one more sample. The chart keeps everything seen while it is open.</p>
          {/if}
        </article>

        <article class="block">
          <header class="block-head">
            <h3>UDP · since boot</h3>
            <span class="hint">{udp.inDatagrams.toLocaleString()} in · {udp.outDatagrams.toLocaleString()} out</span>
          </header>
          <div class="counter-list">
            <div class="counter-row">
              <div class="counter-name">
                <strong>No listener</strong>
                <span>a datagram arrived for a port nothing is bound to</span>
              </div>
              <div class="counter-bar"><i class="warn" style="width: {udp.noListenerShare}%"></i></div>
              <div class="counter-num">
                <strong>{udp.noListener.toLocaleString()}</strong>
                <span>{udp.noListenerShare.toFixed(2)}% of inbound</span>
              </div>
            </div>
            <div class="counter-row">
              <div class="counter-name">
                <strong>Receive buffer full</strong>
                <span>the application did not read fast enough</span>
              </div>
              <div class="counter-bar"><i class="crit" style="width: {share(udp.receiveBuffer, udp.inDatagrams + udp.receiveBuffer)}%"></i></div>
              <div class="counter-num">
                <strong>{udp.receiveBuffer.toLocaleString()}</strong>
                <span>already inside in_errors</span>
              </div>
            </div>
            <div class="counter-row">
              <div class="counter-name">
                <strong>Other inbound errors</strong>
                <span>checksum, format or delivery failure</span>
              </div>
              <div class="counter-bar"><i class="crit" style="width: {share(udp.otherInbound, udp.inDatagrams + udp.otherInbound)}%"></i></div>
              <div class="counter-num">
                <strong>{udp.otherInbound.toLocaleString()}</strong>
                <span>in_errors minus buffer</span>
              </div>
            </div>
            <div class="counter-row">
              <div class="counter-name">
                <strong>Send buffer full</strong>
                <span>the host could not queue an outgoing datagram</span>
              </div>
              <div class="counter-bar"><i class="warn" style="width: {udp.sendShare}%"></i></div>
              <div class="counter-num">
                <strong>{udp.sendBuffer.toLocaleString()}</strong>
                <span>{udp.sendShare.toFixed(2)}% of outbound</span>
              </div>
            </div>
            <div class="counter-row total">
              <div class="counter-name">
                <strong>Dropped in total</strong>
                <span>inbound errors + outgoing send failures</span>
              </div>
              <div class="counter-bar"><i class="crit" style="width: {udp.droppedShare}%"></i></div>
              <div class="counter-num">
                <strong>{udp.dropped.toLocaleString()}</strong>
                <span>{udp.droppedShare.toFixed(2)}% of all datagrams</span>
              </div>
            </div>
          </div>
          <p class="note">
            These counters are not additive: the kernel counts receive-buffer drops inside <span class="mono">in_errors</span>,
            so the total adds the send failures only once. A large lifetime count with no movement is history, not an alarm.
          </p>
        </article>

        <article class="block">
          <header class="block-head">
            <h3>TCP · since boot</h3>
            <span class="hint">{tcp.inSegments.toLocaleString()} in · {tcp.outSegments.toLocaleString()} out</span>
          </header>
          <div class="facts">
            <div><dt>Input errors</dt><dd class:text-crit={tcp.inErrors > 0}>{tcp.inErrors.toLocaleString()} <small>{tcp.errorShare.toFixed(3)}%</small></dd></div>
            <div><dt>Retransmitted</dt><dd>{tcp.retransmissions.toLocaleString()} <small>{tcp.retransPercent.toFixed(2)}%</small></dd></div>
            <div><dt>Attempt fails</dt><dd>{tcp.attemptFails.toLocaleString()}</dd></div>
            <div><dt>Established resets</dt><dd>{tcp.estabResets.toLocaleString()}</dd></div>
            <div><dt>Out resets</dt><dd>{tcp.outResets.toLocaleString()}</dd></div>
          </div>
        </article>

        <article class="block">
          <header class="block-head">
            <h3>Sockets and conntrack</h3>
            <span class="hint">whole machine</span>
          </header>
          <div class="socket-grid">
            <div><span class="label">Established</span><strong>{(health?.sockets?.established ?? 0).toLocaleString()}</strong></div>
            <div><span class="label">Listening</span><strong>{(health?.sockets?.listen ?? 0).toLocaleString()}</strong></div>
            <div><span class="label">UDP</span><strong>{(health?.sockets?.udp ?? 0).toLocaleString()}</strong></div>
            <div><span class="label">Time wait</span><strong>{(health?.sockets?.time_wait ?? 0).toLocaleString()}</strong></div>
            <div class:warn-cell={(health?.sockets?.close_wait ?? 0) > 100}>
              <span class="label">Close wait</span><strong>{(health?.sockets?.close_wait ?? 0).toLocaleString()}</strong>
            </div>
            <div><span class="label">Syn sent</span><strong>{(health?.sockets?.syn_sent ?? 0).toLocaleString()}</strong></div>
            <div><span class="label">Syn recv</span><strong>{(health?.sockets?.syn_recv ?? 0).toLocaleString()}</strong></div>
            <div><span class="label">Fin wait</span><strong>{((health?.sockets?.fin_wait1 ?? 0) + (health?.sockets?.fin_wait2 ?? 0)).toLocaleString()}</strong></div>
          </div>
          {#if health?.conntrack}
            {@const usage = health.conntrack.max > 0 ? Math.min(100, (health.conntrack.count / health.conntrack.max) * 100) : 0}
            <div class="conntrack">
              <div class="conntrack-head">
                <span>Conntrack</span>
                <span>{health.conntrack.count.toLocaleString()} / {health.conntrack.max.toLocaleString()} · {usage.toFixed(1)}%</span>
              </div>
              <div class="counter-bar"><i class={usage >= 80 ? 'crit' : usage >= 60 ? 'warn' : ''} style="width: {usage}%"></i></div>
            </div>
          {:else}
            <p class="note">Conntrack was not reported on this sample.</p>
          {/if}
        </article>

        <article class="block">
          <header class="block-head">
            <h3>What to check</h3>
            <span class="hint">on the device</span>
          </header>
          <div class="advice">
            {#each advice as item (item.id)}
              <div class="advice-item">
                <strong>{item.title}</strong>
                <p>{item.body}</p>
                {#if item.command}
                  <div class="command">
                    <code>{item.command}</code>
                    <button on:click={() => copyCommand(item.id, item.command!)} aria-label={`Copy command: ${item.title}`}>
                      {copied === item.id ? 'Copied' : 'Copy'}
                    </button>
                  </div>
                {/if}
              </div>
            {/each}
          </div>
          <p class="note">
            Scope: whole machine, cumulative since boot. The agent reads <span class="mono">/proc/net/snmp</span> and
            <span class="mono">/proc/net/sockstat</span> as an unprivileged user, so it cannot name the process or
            remote host behind a counter. The commands above run on the device and can.
          </p>
        </article>
      {:else if lens === 'exposure'}
        <div class="hero">
          <span class="hero-value">{exposure.exposed}<small> / {exposure.total}</small></span>
          <span class="hero-label">sockets reachable from the network</span>
          <span class="hero-sub">
            {exposure.loopback} bound to loopback · gateway {gateway || 'not reported'}
            {#if resolvers.length}· {resolvers.length} resolver{resolvers.length === 1 ? '' : 's'}{/if}
          </span>
        </div>

        <article class="block">
          <header class="block-head">
            <h3>Listening sockets</h3>
            <span class="hint">{exposed.length} reachable</span>
          </header>
          {#if portsError}
            <p class="error">{portsError}</p>
          {:else if !portsLoaded}
            <p class="note">Loading the socket inventory…</p>
          {:else if exposed.length === 0}
            <EmptyState
              icon="🔌"
              title={exposure.total === 0 ? 'No listening sockets' : 'Nothing reachable from outside'}
              message={exposure.total === 0
                ? 'The agent reports no socket in the listening state.'
                : 'Every listening socket is bound to the loopback address.'}
            />
          {:else}
            <table class="table">
              <thead>
                <tr><th class="num">Port</th><th>Proto</th><th>Bound to</th><th>Process / likely service</th></tr>
              </thead>
              <tbody>
                {#each exposed.slice(0, 50) as port (port.protocol + port.local_address + port.port + (port.pid ?? ''))}
                  <tr>
                    <td class="num mono strong">{port.port}</td>
                    <td class="mono">{port.protocol}</td>
                    <td class="mono">{port.local_address || '*'}{#if exposureOf(port) === 'all'}<span class="chip crit-chip">all</span>{/if}</td>
                    <td>{port.process || '—'}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
            {#if exposed.length > 50}<p class="note">{exposed.length - 50} more reachable sockets not shown.</p>{/if}
          {/if}
          <p class="note">
            Reachable means bound to a non-loopback address. A firewall may still block it, and the unprivileged
            agent cannot read the firewall rules, only the state of the service. The full inventory lives in
            Services.
          </p>
          <button class="primary" on:click={() => { onClose(); onNavigate('services') }}>Open Services</button>
        </article>

        <article class="block">
          <header class="block-head"><h3>Resolvers</h3></header>
          {#if resolvers.length === 0}
            <p class="note">No resolver reported by the agent.</p>
          {:else}
            <div class="chips">{#each resolvers as resolver (resolver)}<span class="chip mono">{resolver}</span>{/each}</div>
          {/if}
        </article>
      {/if}
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 70;
    background: rgba(0, 0, 0, 0.55);
  }
  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    z-index: 71;
    width: min(590px, 94vw);
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated);
    border-left: 1px solid var(--border-strong);
    box-shadow: var(--shadow-pop);
  }

  .drawer-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.85rem 1rem;
    border-bottom: 1px solid var(--border);
    flex: none;
  }
  .drawer-title { display: grid; gap: 0.18rem; min-width: 0; }
  .drawer-head h2 { margin: 0; font-size: 0.95rem; color: var(--text-bright); }
  .scope { color: var(--text-faint); font-size: 0.7rem; }
  .close {
    flex: none;
    width: 2rem;
    height: 2rem;
    display: grid;
    place-items: center;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 0.85rem;
  }
  .close:hover { border-color: var(--border-strong); color: var(--text); }
  .close:focus-visible { outline: 1px solid var(--accent-border); outline-offset: 2px; }

  .drawer-body {
    padding: 0.9rem 1rem 1.5rem;
    overflow-y: auto;
    display: grid;
    gap: 0.8rem;
    align-content: start;
  }

  .hero { display: grid; gap: 0.18rem; padding: 0.2rem 0; }
  .hero-value { color: var(--text-bright); font-size: 1.9rem; font-weight: 500; font-variant-numeric: tabular-nums; line-height: 1.15; }
  .hero-value small { font-size: 0.9rem; color: var(--text-muted); font-weight: 400; }
  .value-reading { font-size: 1.1rem; line-height: 1.35; }
  .hero-label { color: var(--text-muted); font-size: 0.8rem; }
  .hero-sub { color: var(--text-faint); font-size: 0.72rem; line-height: 1.5; }
  .reading.ok .value-reading { color: var(--ok); }
  .reading.warn .value-reading { color: var(--warn); }
  .reading.crit .value-reading { color: var(--crit); }

  .block {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--card);
    padding: 0.7rem 0.8rem;
    min-width: 0;
  }
  .block-head { display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem; margin-bottom: 0.55rem; flex-wrap: wrap; }
  .block-head h3 { margin: 0; font-size: 0.78rem; font-weight: 600; color: var(--text); }
  .hint { color: var(--text-faint); font-size: 0.68rem; }
  .note { margin: 0.5rem 0 0; color: var(--text-faint); font-size: 0.69rem; line-height: 1.55; }
  .error { margin: 0.2rem 0; color: var(--crit); font-size: 0.74rem; }

  .rate-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 0.5rem; margin-bottom: 0.55rem; }
  .rate-cell { display: grid; gap: 0.12rem; padding: 0.45rem 0.55rem; background: var(--card-inset); border-radius: var(--radius-sm); }
  .rate-cell .label { color: var(--text-faint); font-size: 0.62rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .rate-value { color: var(--text-bright); font-size: 1rem; font-variant-numeric: tabular-nums; }
  .rate-cell .sub { color: var(--text-faint); font-size: 0.62rem; }

  .table { width: 100%; border-collapse: collapse; font-size: 0.73rem; }
  .table th { text-align: left; padding: 0.35rem 0.35rem; color: var(--text-faint); font-size: 0.62rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em; border-bottom: 1px solid var(--border); }
  .table td { padding: 0.36rem 0.35rem; border-bottom: 1px solid var(--border); color: var(--text); vertical-align: top; }
  .table tr:last-child td { border-bottom: 0; }
  .table .num { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
  .mono { font-family: var(--mono); }
  .strong { color: var(--text-bright); font-weight: 600; }
  .dim { color: var(--text-faint); }
  .dim-row { opacity: 0.72; }
  .addresses { color: var(--text-faint); font-size: 0.65rem; margin-top: 0.1rem; }
  .error-row td { padding-top: 0; border-bottom: 1px solid var(--border); font-size: 0.66rem; }
  .text-crit { color: var(--crit); }
  .text-warn { color: var(--warn); }

  .share { display: block; width: 100%; min-width: 3rem; height: 4px; background: var(--card-hover); border-radius: var(--radius-pill); overflow: hidden; }
  .share i { display: block; height: 100%; background: var(--accent); }
  .share-value { color: var(--text-faint); font-size: 0.62rem; }

  .chip { display: inline-block; padding: 0.05rem 0.35rem; border: 1px solid var(--border); border-radius: var(--radius-pill); color: var(--text-muted); font-size: 0.63rem; }
  .chip.muted { border-style: dashed; color: var(--text-faint); }
  .crit-chip { margin-left: 0.3rem; border-color: var(--crit-border, rgba(248, 113, 113, 0.4)); color: var(--crit); }
  .chips { display: flex; gap: 0.35rem; flex-wrap: wrap; }

  .counter-list { display: grid; gap: 0.45rem; }
  .counter-row { display: grid; grid-template-columns: minmax(0, 1.5fr) minmax(3rem, 1fr) minmax(5.5rem, auto); gap: 0.55rem; align-items: center; }
  .counter-name { display: grid; gap: 0.08rem; min-width: 0; }
  .counter-name strong { color: var(--text); font-size: 0.72rem; font-weight: 600; }
  .counter-name span { color: var(--text-faint); font-size: 0.63rem; }
  .counter-bar { height: 6px; background: var(--card-hover); border-radius: var(--radius-pill); overflow: hidden; }
  .counter-bar i { display: block; height: 100%; min-width: 2px; background: var(--accent); }
  .counter-bar i.crit { background: var(--crit); }
  .counter-bar i.warn { background: var(--warn); }
  .counter-num { display: grid; gap: 0.05rem; text-align: right; }
  .counter-num strong { color: var(--text-bright); font-size: 0.78rem; font-variant-numeric: tabular-nums; }
  .counter-num span { color: var(--text-faint); font-size: 0.6rem; }
  .counter-row.total { padding-top: 0.4rem; border-top: 1px solid var(--border); }
  .counter-row.total .counter-name strong { color: var(--text-bright); }

  .facts { display: grid; gap: 0.3rem; }
  .facts > div { display: flex; justify-content: space-between; gap: 0.6rem; font-size: 0.73rem; }
  .facts dt { color: var(--text-muted); }
  .facts dd { margin: 0; color: var(--text); font-variant-numeric: tabular-nums; }
  .facts small { color: var(--text-faint); }

  .socket-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(100px, 1fr)); gap: 0.45rem; }
  .socket-grid > div { display: grid; gap: 0.1rem; padding: 0.4rem 0.5rem; background: var(--card-inset); border-radius: var(--radius-sm); }
  .socket-grid .label { color: var(--text-faint); font-size: 0.6rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .socket-grid strong { color: var(--text-bright); font-size: 0.88rem; font-variant-numeric: tabular-nums; }
  .socket-grid .warn-cell { border: 1px solid rgba(251, 191, 36, 0.4); }
  .socket-grid .warn-cell strong { color: var(--warn); }

  .conntrack { margin-top: 0.55rem; display: grid; gap: 0.3rem; }
  .conntrack-head { display: flex; justify-content: space-between; gap: 0.5rem; color: var(--text-muted); font-size: 0.68rem; }

  .advice { display: grid; gap: 0.55rem; }
  .advice-item { display: grid; gap: 0.2rem; padding: 0.5rem 0.6rem; background: var(--card-inset); border-radius: var(--radius-sm); border-left: 2px solid var(--border-strong); }
  .advice-item strong { color: var(--text-bright); font-size: 0.73rem; }
  .advice-item p { margin: 0; color: var(--text-muted); font-size: 0.69rem; line-height: 1.5; }
  .command { display: flex; align-items: center; gap: 0.4rem; margin-top: 0.2rem; }
  .command code { flex: 1; min-width: 0; padding: 0.28rem 0.4rem; background: var(--card-hover); border-radius: var(--radius-sm); color: var(--text); font-family: var(--mono); font-size: 0.66rem; overflow-x: auto; white-space: nowrap; }
  .command button { flex: none; padding: 0.15rem 0.4rem; border: 1px solid var(--border); border-radius: var(--radius-sm); background: transparent; color: var(--text-muted); font-size: 0.62rem; cursor: pointer; }
  .command button:hover { border-color: var(--accent-border); color: var(--text); }

  .primary {
    margin-top: 0.5rem;
    padding: 0.35rem 0.7rem;
    border: 1px solid var(--accent-border);
    border-radius: var(--radius-sm);
    background: var(--accent-dim);
    color: var(--accent);
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
  }

  @media (max-width: 640px) {
    .drawer { width: 100%; }
    .counter-row { grid-template-columns: minmax(0, 1fr) minmax(3rem, 0.7fr); }
    .counter-num { grid-column: 1 / -1; text-align: left; }
  }
</style>
