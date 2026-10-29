<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import { formatBytes, formatRate } from '../../lib/format'
  import type {
    Device,
    NetworkHistory,
    NetworkHistoryPoint,
    NetworkIface,
    NetworkSection
  } from '../../lib/types'
  import { portIdentity, uniquePorts, type OpenPort as Port } from '../../lib/ports'
  import {
    conntrackUsage,
    countedInterfaces,
    exposureOf,
    exposedPorts,
    healthSummary,
    interfaceKindLabel,
    linkTone,
    macVendor,
    neighbourTone,
    retransTone,
    signalTone,
    summarizeExposure,
    uncountedInterfaces
  } from '../../lib/network'
  import EmptyState from '../ui/EmptyState.svelte'
  import MiniSparkline from '../ui/MiniSparkline.svelte'
  import TimeSeriesChart, { type ChartSeries } from '../ui/TimeSeriesChart.svelte'

  export let device: Device

  const MAX_SAMPLES = 60
  // A best-effort hint for what a listening port usually serves. The agent runs
  // unprivileged, so `ss -p` cannot attribute a socket to another user's
  // process; a well-known port name is the honest fallback, and it is labelled
  // as an inference in the UI.
  const WELL_KNOWN: Record<number, string> = {
    20: 'ftp-data', 21: 'ftp', 22: 'ssh', 23: 'telnet', 25: 'smtp', 53: 'dns',
    67: 'dhcp', 68: 'dhcp', 80: 'http', 110: 'pop3', 123: 'ntp', 143: 'imap',
    161: 'snmp', 389: 'ldap', 443: 'https', 465: 'smtls', 514: 'syslog',
    587: 'submission', 631: 'ipp', 993: 'imaps', 995: 'pop3s', 1080: 'socks',
    2375: 'docker', 2376: 'docker-tls', 2377: 'docker-swarm', 3000: 'http-dev',
    3306: 'mysql', 3389: 'rdp', 4172: 'pulseaudio', 5432: 'postgres',
    5672: 'amqp', 5900: 'vnc', 6379: 'redis', 8080: 'http-alt', 8443: 'https-alt',
    9000: 'http-alt', 9090: 'prometheus', 11211: 'memcached', 27017: 'mongodb'
  }

  // The palette the traffic chart cycles through, taken from the shared plot
  // tokens so a link keeps the same colour on every screen.
  const SERIES = ['--chart-1', '--chart-2', '--chart-3', '--chart-4', '--chart-5']
  const SERIES_FALLBACK = ['#2663d9', '#2eb88a', '#f28c26', '#b546ec', '#ec2c6c']

  type RangeId = '1h' | '24h' | '7d'
  const RANGES: { id: RangeId; label: string; ms: number }[] = [
    { id: '1h', label: '1h', ms: 60 * 60_000 },
    { id: '24h', label: '24h', ms: 24 * 60 * 60_000 },
    { id: '7d', label: '7d', ms: 7 * 24 * 60 * 60_000 }
  ]

  let ports: Port[] = []
  let portsLoaded = false
  let portsError = ''

  // The slow half of the network view: gateway, resolvers, routes, neighbours.
  let topology: NetworkSection | null = null
  let topologyError = ''
  let topologyLoaded = false

  // The per-interface traffic history, straight from the server.
  let history: NetworkHistory | null = null
  let historyError = ''
  let historyLoading = false
  let historyWindow: { from: number; to: number } | null = null
  let range: RangeId = '24h'
  let direction: 'rx' | 'tx' = 'rx'

  let showVirtual = false

  // Live samples keyed by interface. They only live for this screen's
  // lifetime, which is exactly what a "last few minutes" view needs; the long
  // history comes from the server.
  let ifaceSeries: Record<string, { rx: number[]; tx: number[] }> = {}
  let totalRx: number[] = []
  let totalTx: number[] = []
  let lastStamp = ''
  let seriesDeviceId = ''

  let historyTimer: ReturnType<typeof setInterval> | null = null
  let topologyTimer: ReturnType<typeof setInterval> | null = null

  // The shell reuses this component when another device is selected. Without
  // this reset the sparklines would mix samples from two devices.
  function resetSeries(id: string) {
    if (!id || id === seriesDeviceId) return
    seriesDeviceId = id
    ifaceSeries = {}
    totalRx = []
    totalTx = []
    lastStamp = ''
  }
  $: resetSeries(device.id)

  $: hw = device.hardware
  $: ifaces = (hw?.network_ifaces ?? []).slice().sort((a, b) => {
    // Physical interfaces with traffic first, then by name, so the interesting
    // link is not buried under idle tunnels.
    const av = (a.rx_rate || 0) + (a.tx_rate || 0)
    const bv = (b.rx_rate || 0) + (b.tx_rate || 0)
    if (bv !== av) return bv - av
    return a.name.localeCompare(b.name)
  })
  $: counted = countedInterfaces(ifaces)
  $: virtualIfaces = uncountedInterfaces(ifaces)
  $: health = hw?.network_health
  $: healthNote = healthSummary(health)

  // The agent sums the counted interfaces. The fallback exists for an agent
  // that reports no rate at all, and it must not sum the container plumbing.
  $: totalRxRate = hw?.net_rx_rate ?? counted.reduce((n, i) => n + (i.rx_rate || 0), 0)
  $: totalTxRate = hw?.net_tx_rate ?? counted.reduce((n, i) => n + (i.tx_rate || 0), 0)
  $: totalBytes = counted.reduce((n, i) => n + i.rx_bytes + i.tx_bytes, 0)
  $: totalErrors = counted.reduce((n, i) => n + (i.rx_errors || 0) + (i.tx_errors || 0), 0)
  $: totalDrops = counted.reduce((n, i) => n + (i.rx_dropped || 0) + (i.tx_dropped || 0), 0)
  $: upCount = counted.filter((i) => (i.oper_state || '') === 'up').length
  $: kindSummary = summariseKinds(counted)

  $: exposure = summarizeExposure(ports)
  $: topExposed = exposedPorts(ports)
    .sort((a, b) => (exposureOf(a) === 'all' ? -1 : 0) - (exposureOf(b) === 'all' ? -1 : 0) || a.port - b.port)
    .slice(0, 5)

  $: routes = topology?.topology?.routes ?? []
  $: neighbors = topology?.topology?.neighbors ?? []
  $: wireless = topology?.topology?.wireless ?? []
  $: tunnels = topology?.topology?.tunnels ?? []
  $: firewall = topology?.firewall
  $: defaultGateway = topology?.topology?.default_gateway ?? ''
  $: dnsServers = topology?.topology?.dns ?? []
  $: searchDomains = topology?.topology?.search_domains ?? []
  $: topologyAge = topology?.topology?.collected_at ?? ''

  // Sample a new point whenever the agent reports a genuinely new collection.
  // Guarding on collected_at keeps a re-render from inserting duplicates.
  $: if (hw?.collected_at && hw.collected_at !== lastStamp) {
    lastStamp = hw.collected_at
    totalRx = push(totalRx, totalRxRate)
    totalTx = push(totalTx, totalTxRate)
    const next: Record<string, { rx: number[]; tx: number[] }> = {}
    for (const iface of ifaces) {
      const prev = ifaceSeries[iface.name] || { rx: [], tx: [] }
      next[iface.name] = {
        rx: push(prev.rx, iface.rx_rate || 0),
        tx: push(prev.tx, iface.tx_rate || 0)
      }
    }
    ifaceSeries = next
  }

  function push(series: number[], value: number): number[] {
    const out = [...series, value]
    return out.length > MAX_SAMPLES ? out.slice(out.length - MAX_SAMPLES) : out
  }

  function summariseKinds(list: NetworkIface[]): string {
    const counts = new Map<string, number>()
    for (const iface of list) {
      const label = interfaceKindLabel(iface.kind)
      counts.set(label, (counts.get(label) ?? 0) + 1)
    }
    return [...counts.entries()].map(([label, n]) => `${n} ${label}`).join(' · ')
  }

  function addresses(iface: NetworkIface): string[] {
    return (iface.addresses ?? []).filter(Boolean)
  }

  function host(address: string): string {
    return address || '*'
  }

  function vendor(iface: NetworkIface): string {
    return macVendor(iface.mac)
  }

  function trafficColor(index: number): string {
    const name = SERIES[index % SERIES.length]
    const fallback = SERIES_FALLBACK[index % SERIES_FALLBACK.length]
    if (typeof window === 'undefined' || !document?.documentElement) return fallback
    const resolved = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
    return resolved || fallback
  }

  onMount(() => {
    void loadPorts()
    void loadTopology()
    void loadHistory()
    // The topology moves on the order of minutes, the history on the order of
    // a minute. Nothing here is worth polling faster than that.
    topologyTimer = setInterval(() => void loadTopology(), 300_000)
    historyTimer = setInterval(() => void loadHistory(), 60_000)
  })

  onDestroy(() => {
    if (topologyTimer) clearInterval(topologyTimer)
    if (historyTimer) clearInterval(historyTimer)
  })

  async function loadPorts() {
    try {
      const res = await apiFetch(apiURL(`/api/devices/${device.id}/ports`))
      if (!res.ok) throw new Error(`ports request failed (${res.status})`)
      const list: Port[] = await res.json()
      // An older agent can report one socket twice; see lib/ports.ts for why a
      // repeated key erases this screen rather than skipping a row.
      ports = uniquePorts(list).sort((a, b) => a.port - b.port)
    } catch (e: any) {
      portsError = e?.message || 'Unable to load listening sockets'
    } finally {
      portsLoaded = true
    }
  }

  async function loadTopology() {
    try {
      const res = await apiFetch(apiURL(`/api/devices/${device.id}/network`))
      if (!res.ok) throw new Error(`network request failed (${res.status})`)
      topology = await res.json()
      topologyError = ''
    } catch (e: any) {
      topologyError = e?.message || 'Unable to load the network topology'
    } finally {
      topologyLoaded = true
    }
  }

  async function loadHistory() {
    historyLoading = true
    try {
      const window_ = RANGES.find((r) => r.id === range) ?? RANGES[1]
      const to = new Date()
      const from = new Date(to.getTime() - window_.ms)
      const res = await apiFetch(
        apiURL(`/api/devices/${device.id}/network/history?from=${from.toISOString()}&to=${to.toISOString()}`)
      )
      if (!res.ok) throw new Error(`history request failed (${res.status})`)
      history = await res.json()
      historyWindow = { from: from.getTime(), to: to.getTime() }
      historyError = ''
    } catch (e: any) {
      historyError = e?.message || 'Unable to load the traffic history'
    } finally {
      historyLoading = false
    }
  }

  function changeRange(next: RangeId) {
    if (next === range) return
    range = next
    void loadHistory()
  }

  // The peak of the window, per direction. It is the number an operator
  // looks for when a link "felt slow": not the average but the worst moment.
  $: peak = peakOf(history, direction)
  $: peakOther = peakOf(history, direction === 'rx' ? 'tx' : 'rx')

  function peakOf(data: NetworkHistory | null, dir: 'rx' | 'tx'): { value: number; at: string } {
    if (!data) return { value: 0, at: '' }
    let best = 0
    let at = ''
    for (const points of Object.values(data.interfaces ?? {})) {
      for (const point of points) {
        const value = dir === 'rx' ? point.rx_rate : point.tx_rate
        if (value > best) {
          best = value
          at = point.timestamp
        }
      }
    }
    return { value: best, at }
  }

  // The chart draws one stacked area per interface. The sum of the areas is
  // the total the device sent, and the height of one area is what one link
  // carried. Series share a stack id so the shared chart renders them as an
  // area stack instead of overlapping fills.
  $: traffic = buildTraffic(history, direction)

  function buildTraffic(
    data: NetworkHistory | null,
    dir: 'rx' | 'tx'
  ): { labels: string[]; series: ChartSeries[] } {
    if (!data) return { labels: [], series: [] }

    const names = Object.keys(data.interfaces ?? {}).sort()
    const stamps = new Set<string>()
    for (const points of Object.values(data.interfaces ?? {})) {
      for (const point of points) stamps.add(point.timestamp)
    }
    const ordered = [...stamps].sort()
    if (ordered.length < 2) return { labels: ordered, series: [] }

    const series = names.map((name, index) => {
      const byStamp = new Map<string, NetworkHistoryPoint>(
        (data.interfaces[name] ?? []).map((point) => [point.timestamp, point])
      )
      return {
        label: name,
        values: ordered.map((stamp) => {
          const point = byStamp.get(stamp)
          if (!point) return 0
          return dir === 'rx' ? point.rx_rate : point.tx_rate
        }),
        color: trafficColor(index),
        stack: 'traffic',
        // Shared by the tooltip and the legend stats, which do not go through
        // the axis formatter.
        format: formatRate
      }
    })

    return { labels: ordered, series }
  }

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
</script>

<section class="network">
  {#if !hw}
    <EmptyState icon="🌐" title="No telemetry yet" message="The agent has not reported network counters for this device." />
  {:else}
    <div class="summary">
      <article class="stat">
        <span class="label">Download</span>
        <span class="value">{formatRate(totalRxRate)}</span>
        <MiniSparkline series={totalRx} color="var(--chart-2)" height={26} label="Download rate" />
        <span class="sub">{formatBytes(counted.reduce((n, i) => n + i.rx_bytes, 0))} since boot</span>
      </article>
      <article class="stat">
        <span class="label">Upload</span>
        <span class="value">{formatRate(totalTxRate)}</span>
        <MiniSparkline series={totalTx} color="var(--chart-5)" height={26} label="Upload rate" />
        <span class="sub">{formatBytes(counted.reduce((n, i) => n + i.tx_bytes, 0))} since boot</span>
      </article>
      <article class="stat">
        <span class="label">Peak · {RANGES.find((r) => r.id === range)?.label}</span>
        <span class="value">{formatRate(peak.value)}</span>
        <span class="sub">
          {direction === 'rx' ? 'download' : 'upload'} · {peak.at ? clockOf(peak.at) : 'no data'}
          {#if peakOther.value > 0}· other direction {formatRate(peakOther.value)}{/if}
        </span>
      </article>
      <article class="stat">
        <span class="label">Links</span>
        <span class="value">{upCount}<small> / {counted.length} up</small></span>
        <span class="sub">{kindSummary || 'no link reported'}</span>
      </article>
      <article class="stat" class:stat-crit={healthNote.tone === 'crit'} class:stat-warn={healthNote.tone === 'warn'}>
        <span class="label">Stack health</span>
        <span class="value value-sm">{healthNote.title}</span>
        <span class="sub">
          {#if health?.tcp}
            {(health.tcp.retrans_percent ?? 0).toFixed(2)}% retransmitted
            {#if health.conntrack}
              · conntrack {health.conntrack.count}/{health.conntrack.max}
            {/if}
          {:else}
            no packet counters reported
          {/if}
        </span>
      </article>
      <article class="stat" class:stat-warn={exposure.exposed > 0}>
        <span class="label">Reachable from the network</span>
        <span class="value">{exposure.exposed}<small> / {exposure.total} sockets</small></span>
        <span class="sub">
          {defaultGateway ? `gateway ${defaultGateway}` : 'no gateway reported'}
          {#if dnsServers.length}· {dnsServers.length} resolver{dnsServers.length === 1 ? '' : 's'}{/if}
        </span>
      </article>
    </div>

    <article class="card">
      <header class="card-head">
        <h2>Traffic by interface</h2>
        <div class="head-tools">
          <div class="segmented" role="group" aria-label="Direction">
            <button class:active={direction === 'rx'} on:click={() => (direction = 'rx')}>Download</button>
            <button class:active={direction === 'tx'} on:click={() => (direction = 'tx')}>Upload</button>
          </div>
          <div class="segmented" role="group" aria-label="Range">
            {#each RANGES as option (option.id)}
              <button class:active={range === option.id} on:click={() => changeRange(option.id)}>{option.label}</button>
            {/each}
          </div>
        </div>
      </header>

      {#if historyError}
        <p class="error">{historyError}</p>
      {:else if historyLoading && !history}
        <p class="loading">Loading the traffic history…</p>
      {:else if !history || Object.keys(history.interfaces ?? {}).length === 0}
        <EmptyState
          icon="📈"
          title="No traffic history yet"
          message="The server keeps one sample per interface per minute. The chart fills as the device reports."
        />
      {:else}
        <TimeSeriesChart
          labels={traffic.labels}
          series={traffic.series}
          leftUnit=""
          leftFormat={formatRate}
          height={260}
          windowMs={RANGES.find((r) => r.id === range)?.ms ?? null}
          windowEnd={historyWindow?.to ?? null}
          emptyMessage="No traffic samples in this window yet."
        />
        <p class="foot-note">
          {history.resolution} buckets · {Object.keys(history.interfaces).length} interface{Object.keys(history.interfaces).length === 1 ? '' : 's'} recorded
          {#if historyLoading}· refreshing…{/if}
        </p>
      {/if}
    </article>

    <article class="card">
      <header class="card-head">
        <h2>Interfaces</h2>
        <span class="hint">
          {counted.length} link{counted.length === 1 ? '' : 's'} in the totals
          {#if virtualIfaces.length > 0}
            · <button class="link-btn" on:click={() => (showVirtual = !showVirtual)}>
              {showVirtual ? 'hide' : 'show'} {virtualIfaces.length} container/virtual
            </button>
          {/if}
        </span>
      </header>

      {#if ifaces.length === 0}
        <EmptyState icon="🌐" title="No interfaces reported" message="The agent found no network interface to report." />
      {:else}
        <div class="ifaces">
          {#each (showVirtual ? ifaces : counted) as iface (iface.name)}
            {@const tone = linkTone(iface)}
            <div class="iface" class:uncounted={iface.counted === false}>
              <div class="iface-top">
                <div class="iface-id">
                  <span class="dot {tone}"></span>
                  <span class="name mono">{iface.name}</span>
                  <span class="kind">{interfaceKindLabel(iface.kind)}</span>
                  <span class="state">{iface.oper_state || 'unknown'}</span>
                  {#if iface.counted === false}
                    <span class="chip chip-muted" title="This link duplicates traffic that is already counted on a real interface">not in totals</span>
                  {/if}
                </div>
                <div class="iface-links">
                  {#if iface.speed_mbps}<span class="chip">{iface.speed_mbps} Mb/s</span>{/if}
                  {#if iface.duplex}<span class="chip">{iface.duplex}</span>{/if}
                  {#if iface.mtu}<span class="chip">MTU {iface.mtu}</span>{/if}
                  {#if iface.driver}<span class="chip mono">{iface.driver}</span>{/if}
                  {#if iface.mac}<span class="chip mono">{iface.mac}</span>{/if}
                  {#if vendor(iface)}<span class="chip">{vendor(iface)}</span>{/if}
                </div>
              </div>

              {#if addresses(iface).length > 0}
                <div class="addrs">
                  {#each addresses(iface) as addr (addr)}
                    <span class="addr mono">{addr}</span>
                  {/each}
                </div>
              {/if}

              <div class="rates">
                <div class="rate">
                  <span class="rate-k">↓ {formatRate(iface.rx_rate || 0)}</span>
                  <MiniSparkline series={ifaceSeries[iface.name]?.rx || []} color="var(--chart-2)" height={22} />
                </div>
                <div class="rate">
                  <span class="rate-k">↑ {formatRate(iface.tx_rate || 0)}</span>
                  <MiniSparkline series={ifaceSeries[iface.name]?.tx || []} color="var(--chart-5)" height={22} />
                </div>
              </div>

              <div class="counters">
                <span title="total received">{formatBytes(iface.rx_bytes)} rx</span>
                <span title="total transmitted">{formatBytes(iface.tx_bytes)} tx</span>
                <span>{iface.rx_packets.toLocaleString()} / {iface.tx_packets.toLocaleString()} pkt</span>
                {#if iface.rx_errors || iface.tx_errors}
                  <span class="text-crit">{iface.rx_errors + iface.tx_errors} err</span>
                {/if}
                {#if (iface.rx_dropped || 0) + (iface.tx_dropped || 0) > 0}
                  <span class="text-warn">{(iface.rx_dropped || 0) + (iface.tx_dropped || 0)} drop</span>
                {/if}
                {#if (iface.rx_fifo_errors || 0) + (iface.tx_fifo_errors || 0) > 0}
                  <span class="text-crit">{(iface.rx_fifo_errors || 0) + (iface.tx_fifo_errors || 0)} fifo</span>
                {/if}
                {#if (iface.rx_collisions || 0) > 0}
                  <span class="text-crit">{iface.rx_collisions} coll</span>
                {/if}
                {#if (iface.tx_carrier_errors || 0) > 0}
                  <span class="text-crit">{iface.tx_carrier_errors} carrier</span>
                {/if}
                {#if (iface.carrier_changes || 0) > 0}
                  <span class="text-warn">{iface.carrier_changes} carrier changes</span>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </article>

    <div class="split">
      <article class="card">
        <header class="card-head">
          <h2>IP configuration</h2>
          <span class="hint">{topologyLoaded ? `updated ${ageOf(topologyAge)}` : 'loading…'}</span>
        </header>
        {#if topologyError}
          <p class="error">{topologyError}</p>
        {:else if !topologyLoaded}
          <p class="loading">Loading the topology…</p>
        {:else}
          <dl class="kv">
            <div><dt>Default gateway</dt><dd class="mono">{defaultGateway || '—'}</dd></div>
            <div><dt>Resolvers</dt><dd class="mono">{dnsServers.length ? dnsServers.join(', ') : '—'}</dd></div>
            <div><dt>Search domains</dt><dd class="mono">{searchDomains.length ? searchDomains.join(', ') : '—'}</dd></div>
          </dl>
          {#if routes.length > 0}
            <table class="socks">
              <thead>
                <tr><th>Destination</th><th>Gateway</th><th>Interface</th><th>Metric</th></tr>
              </thead>
              <tbody>
                {#each routes.slice(0, 12) as route (route.family + route.destination + route.interface)}
                  <tr>
                    <td class="mono">{route.destination}</td>
                    <td class="mono dim">{route.gateway || '—'}</td>
                    <td class="mono">{route.interface}</td>
                    <td class="dim">{route.metric || '—'}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
            {#if routes.length > 12}
              <p class="foot-note">{routes.length - 12} more routes not shown</p>
            {/if}
          {:else}
            <EmptyState icon="🧭" title="No routes reported" message="The agent could not read the routing table." />
          {/if}
        {/if}
      </article>

      <article class="card">
        <header class="card-head">
          <h2>Packet health</h2>
          <span class="hint">{health?.collected_at ? `updated ${ageOf(health.collected_at)}` : 'no data'}</span>
        </header>
        {#if !health}
          <EmptyState icon="🩺" title="No packet data" message="The agent has not reported the TCP and socket counters yet." />
        {:else}
          <div class="health-grid">
            <div class="health-cell">
              <span class="label">Retransmissions</span>
              <span class="value value-sm" class:text-crit={retransTone(health.tcp?.retrans_percent) === 'crit'} class:text-warn={retransTone(health.tcp?.retrans_percent) === 'warn'}>
                {(health.tcp?.retrans_percent ?? 0).toFixed(2)}%
              </span>
              <span class="sub">{(health.tcp?.retrans_per_sec ?? 0).toFixed(2)}/s · {(health.tcp?.retrans_segs ?? 0).toLocaleString()} since boot</span>
            </div>
            <div class="health-cell">
              <span class="label">Established</span>
              <span class="value value-sm">{(health.sockets?.established ?? 0).toLocaleString()}</span>
              <span class="sub">{(health.sockets?.listen ?? 0).toLocaleString()} listening · {(health.sockets?.udp ?? 0).toLocaleString()} udp</span>
            </div>
            <div class="health-cell">
              <span class="label">Socket backlog</span>
              <span class="value value-sm">{(health.sockets?.time_wait ?? 0).toLocaleString()}</span>
              <span class="sub">time_wait · {(health.sockets?.close_wait ?? 0).toLocaleString()} close_wait</span>
            </div>
            <div class="health-cell">
              <span class="label">Conntrack</span>
              <span class="value value-sm">{(health.conntrack?.count ?? 0).toLocaleString()}</span>
              <span class="sub">of {(health.conntrack?.max ?? 0).toLocaleString()} · {conntrackUsage(health.conntrack?.count, health.conntrack?.max)}% used</span>
            </div>
          </div>
          <div class="bar-row">
            <span class="bar-label">TCP errors</span>
            <div class="bar"><div class="bar-fill" style="width: {Math.min(100, health.tcp?.in_errs ?? 0)}%" class:bar-crit={(health.tcp?.in_errs ?? 0) > 0}></div></div>
            <span class="bar-value">{(health.tcp?.in_errs ?? 0).toLocaleString()}</span>
          </div>
          <div class="bar-row">
            <span class="bar-label">UDP errors</span>
            <div class="bar"><div class="bar-fill" style="width: {Math.min(100, (health.udp?.in_errors ?? 0) + (health.udp?.rcvbuf_errors ?? 0) + (health.udp?.sndbuf_errors ?? 0))}%" class:bar-crit={((health.udp?.in_errors ?? 0) + (health.udp?.rcvbuf_errors ?? 0) + (health.udp?.sndbuf_errors ?? 0)) > 0}></div></div>
            <span class="bar-value">{((health.udp?.in_errors ?? 0) + (health.udp?.rcvbuf_errors ?? 0) + (health.udp?.sndbuf_errors ?? 0)).toLocaleString()}</span>
          </div>
        {/if}
      </article>
    </div>

    <div class="split">
      <article class="card">
        <header class="card-head">
          <h2>Neighbours</h2>
          <span class="hint">{neighbors.length} on the segment</span>
        </header>
        {#if neighbors.length === 0}
          <EmptyState icon="👥" title="No neighbours seen" message="The neighbour table is empty or could not be read." />
        {:else}
          <table class="socks">
            <thead>
              <tr><th>Address</th><th>Hardware</th><th>Vendor</th><th>Interface</th><th>State</th></tr>
            </thead>
            <tbody>
              {#each neighbors.slice(0, 20) as neighbor (neighbor.family + neighbor.ip + neighbor.interface)}
                <tr>
                  <td class="mono">{neighbor.ip}</td>
                  <td class="mono dim">{neighbor.mac || '—'}</td>
                  <td class="dim">{macVendor(neighbor.mac) || '—'}</td>
                  <td class="mono">{neighbor.interface || '—'}</td>
                  <td>
                    <span class="dot {neighbourTone(neighbor)}"></span>
                    {neighbor.state || 'unknown'}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
          {#if neighbors.length > 20}
            <p class="foot-note">{neighbors.length - 20} more neighbours not shown</p>
          {/if}
        {/if}
      </article>

      <article class="card">
        <header class="card-head">
          <h2>Exposure</h2>
          <span class="hint">{exposure.exposed} reachable · {exposure.loopback} local</span>
        </header>
        {#if portsError}
          <p class="error">{portsError}</p>
        {:else if !portsLoaded}
          <p class="loading">Loading sockets…</p>
        {:else if topExposed.length === 0}
          <EmptyState
            icon="🔌"
            title={exposure.total === 0 ? 'No listening sockets' : 'Nothing reachable from outside'}
            message={exposure.total === 0
              ? 'The agent reports no socket in the listening state.'
              : 'Every listening socket is bound to the loopback address.'}
          />
        {:else}
          <table class="socks">
            <thead>
              <tr><th>Port</th><th>Proto</th><th>Bound to</th><th>Likely service</th></tr>
            </thead>
            <tbody>
              {#each topExposed as p (portIdentity(p))}
                <tr>
                  <td class="mono port">{p.port}</td>
                  <td class="mono proto">{p.protocol}</td>
                  <td class="mono">
                    {host(p.local_address)}
                    {#if exposureOf(p) === 'all'}<span class="dot crit"></span>{/if}
                  </td>
                  <td>{p.process || WELL_KNOWN[p.port] || '—'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
          <p class="foot-note">The full socket inventory lives in the Services screen.</p>
        {/if}
      </article>
    </div>

    <div class="split">
      <article class="card">
        <header class="card-head">
          <h2>Wireless</h2>
          <span class="hint">{wireless.length} radio{wireless.length === 1 ? '' : 's'}</span>
        </header>
        {#if wireless.length === 0}
          <EmptyState icon="📶" title="No wireless radios" message="The device reports no wireless interface." />
        {:else}
          <div class="wireless">
            {#each wireless as link (link.interface)}
              <div class="wl">
                <span class="wl-name mono">{link.interface}</span>
                <span class="wl-signal {signalTone(link.signal_dbm)}">{link.signal_dbm} dBm</span>
                <span class="wl-quality">quality {link.quality ?? '—'}</span>
                <span class="wl-noise">noise {link.noise_dbm ?? '—'}</span>
              </div>
            {/each}
          </div>
          <p class="foot-note">The agent is unprivileged, so the SSID, the channel and the bitrate are not readable.</p>
        {/if}
      </article>

      <article class="card">
        <header class="card-head">
          <h2>Tunnels and firewall</h2>
          <span class="hint">{tunnels.length} tunnel{tunnels.length === 1 ? '' : 's'}</span>
        </header>
        {#if tunnels.length === 0}
          <EmptyState icon="🛣️" title="No tunnels" message="The device reports no VPN or overlay interface." />
        {:else}
          <table class="socks">
            <thead>
              <tr><th>Interface</th><th>Kind</th><th>State</th></tr>
            </thead>
            <tbody>
              {#each tunnels as tunnel (tunnel.interface)}
                <tr>
                  <td class="mono">{tunnel.interface}</td>
                  <td>{tunnel.kind}</td>
                  <td>{tunnel.state || 'unknown'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        {#if firewall}
          <dl class="kv">
            <div><dt>ufw</dt><dd class:firewall-on={firewall.ufw === 'active'}>{firewall.ufw || 'unknown'}</dd></div>
            <div><dt>firewalld</dt><dd class:firewall-on={firewall.firewalld === 'active'}>{firewall.firewalld || 'unknown'}</dd></div>
            <div><dt>nftables</dt><dd class:firewall-on={firewall.nftables === 'active'}>{firewall.nftables || 'unknown'}</dd></div>
          </dl>
          <p class="foot-note">The agent runs unprivileged: the rules are not readable, only the state of the service.</p>
        {/if}
      </article>
    </div>

    <p class="screen-foot">
      {totalBytes > 0 ? `${formatBytes(totalBytes)} through the counted links since boot · ` : ''}
      {totalErrors > 0 ? `${totalErrors} errors · ` : ''}
      {totalDrops > 0 ? `${totalDrops} dropped · ` : ''}
      collected {ageOf(hw?.collected_at ?? '')}
    </p>
  {/if}
</section>

<style>
  .network { display: grid; gap: 0.85rem; }

  .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 0.6rem; }
  .stat {
    display: grid; gap: 0.25rem; align-content: start;
    padding: 0.8rem 0.9rem; background: var(--card);
    border: 1px solid var(--border); border-radius: var(--radius);
  }
  .stat .value { font-size: 1.45rem; }
  .stat .value small { font-size: 0.8rem; color: var(--text-muted); font-weight: 400; }
  .stat .value-sm { font-size: 0.95rem; line-height: 1.3; }
  .stat .sub { color: var(--text-faint); font-size: 0.72rem; }
  .stat-warn { border-color: var(--warn-border, rgba(251, 191, 36, 0.35)); }
  .stat-crit { border-color: var(--crit-border, rgba(248, 113, 113, 0.35)); }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .hint { color: var(--text-faint); font-size: 0.7rem; }
  .head-tools { display: flex; gap: 0.4rem; flex-wrap: wrap; }

  .segmented { display: inline-flex; border: 1px solid var(--border); border-radius: var(--radius-pill); overflow: hidden; }
  .segmented button {
    background: transparent; border: 0; color: var(--text-muted);
    font-size: 0.68rem; padding: 0.2rem 0.55rem; cursor: pointer;
  }
  .segmented button.active { background: var(--accent-dim); color: var(--text-bright); }

  .link-btn {
    background: none; border: 0; padding: 0; color: var(--accent);
    font-size: 0.7rem; cursor: pointer; text-decoration: underline;
  }

  .foot-note { margin: 0; padding: 0 0.9rem 0.7rem; color: var(--text-faint); font-size: 0.68rem; }
  .screen-foot { margin: 0; color: var(--text-faint); font-size: 0.7rem; }

  .ifaces { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 0.7rem; padding: 0.8rem; }
  .iface { display: grid; gap: 0.45rem; padding: 0.75rem 0.85rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); }
  .iface.uncounted { opacity: 0.75; }

  .iface-top { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; flex-wrap: wrap; }
  .iface-id { display: flex; align-items: center; gap: 0.45rem; min-width: 0; flex-wrap: wrap; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--offline); flex-shrink: 0; display: inline-block; }
  .dot.ok { background: var(--ok); }
  .dot.warn { background: var(--warn); }
  .dot.crit { background: var(--crit); }
  .name { color: var(--text-bright); font-weight: 600; }
  .kind { color: var(--text-muted); font-size: 0.66rem; border: 1px solid var(--border); border-radius: var(--radius-pill); padding: 0.05rem 0.4rem; }
  .state { color: var(--text-faint); font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .iface-links { display: flex; gap: 0.3rem; flex-wrap: wrap; }
  .chip { padding: 0.1rem 0.4rem; border: 1px solid var(--border); border-radius: var(--radius-pill); color: var(--text-muted); font-size: 0.66rem; }
  .chip-muted { border-style: dashed; color: var(--text-faint); }

  .addrs { display: flex; flex-wrap: wrap; gap: 0.35rem; }
  .addr { padding: 0.12rem 0.4rem; background: var(--card-hover); border-radius: var(--radius-sm); color: var(--text); font-size: 0.7rem; }

  .rates { display: grid; grid-template-columns: 1fr 1fr; gap: 0.6rem; }
  .rate { display: grid; gap: 0.1rem; }
  .rate-k { color: var(--text-muted); font-size: 0.72rem; font-variant-numeric: tabular-nums; }

  .counters { display: flex; flex-wrap: wrap; gap: 0.6rem; color: var(--text-faint); font-size: 0.68rem; font-variant-numeric: tabular-nums; border-top: 1px solid var(--border); padding-top: 0.4rem; }

  .split { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 0.85rem; align-items: start; }

  .kv { margin: 0; padding: 0.6rem 0.9rem; display: grid; gap: 0.35rem; }
  .kv > div { display: flex; justify-content: space-between; gap: 0.6rem; font-size: 0.74rem; }
  .kv dt { color: var(--text-muted); }
  .kv dd { margin: 0; color: var(--text); text-align: right; }
  .firewall-on { color: var(--ok); }

  .health-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 0.5rem; padding: 0.8rem 0.9rem 0.4rem; }
  .health-cell { display: grid; gap: 0.15rem; }
  .health-cell .label { color: var(--text-faint); font-size: 0.66rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .health-cell .value { font-size: 1.05rem; }
  .health-cell .sub { color: var(--text-faint); font-size: 0.66rem; }

  .bar-row { display: grid; grid-template-columns: 5.5rem 1fr 3.5rem; align-items: center; gap: 0.5rem; padding: 0.2rem 0.9rem; }
  .bar-label { color: var(--text-muted); font-size: 0.68rem; }
  .bar { height: 6px; background: var(--card-hover); border-radius: var(--radius-pill); overflow: hidden; }
  .bar-fill { height: 100%; background: var(--accent); border-radius: var(--radius-pill); min-width: 2px; }
  .bar-fill.bar-crit { background: var(--crit); }
  .bar-value { color: var(--text-faint); font-size: 0.68rem; text-align: right; font-variant-numeric: tabular-nums; }

  .wireless { display: grid; gap: 0.4rem; padding: 0.8rem 0.9rem; }
  .wl { display: flex; align-items: center; gap: 0.7rem; flex-wrap: wrap; font-size: 0.74rem; }
  .wl-name { color: var(--text-bright); }
  .wl-signal { color: var(--ok); }
  .wl-signal.warn { color: var(--warn); }
  .wl-signal.crit { color: var(--crit); }
  .wl-quality, .wl-noise { color: var(--text-faint); }

  .socks { width: 100%; border-collapse: collapse; font-size: 0.76rem; }
  .socks th { text-align: left; padding: 0.45rem 0.9rem; color: var(--text-faint); font-size: 0.66rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em; border-bottom: 1px solid var(--border); }
  .socks td { padding: 0.42rem 0.9rem; border-bottom: 1px solid var(--border); color: var(--text); }
  .socks tr:last-child td { border-bottom: 0; }
  .port { color: var(--accent); }
  .proto { color: var(--text-muted); text-transform: uppercase; font-size: 0.68rem; }
  .dim { color: var(--text-faint); }
  .error { margin: 0; padding: 0.9rem; color: var(--crit); font-size: 0.76rem; }
  .loading { margin: 0; padding: 0.9rem; color: var(--text-faint); font-size: 0.76rem; }
  .text-crit { color: var(--crit); }
  .text-warn { color: var(--warn); }

  @media (max-width: 720px) {
    .rates { grid-template-columns: 1fr; }
  }
</style>
