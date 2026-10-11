<script lang="ts">
  import { onMount } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import { formatBytes, formatRate } from '../../lib/format'
  import type { Device, NetworkIface } from '../../lib/types'
  import { portIdentity, uniquePorts, type OpenPort as Port } from '../../lib/ports'
  import EmptyState from '../ui/EmptyState.svelte'
  import MiniSparkline from '../ui/MiniSparkline.svelte'

  export let device: Device

  const MAX_SAMPLES = 60
  // A best-effort hint for what a listening port usually serves. The agent runs
  // unprivileged, so `ss -p` cannot attribute a socket to another user's
  // process; a well-known port name is the honest fallback, and it is labelled
  // as an inference in the UI.
  const WELL_KNOWN: Record<number, string> = {
    20: 'ftp-data', 21: 'ftp', 22: 'ssh', 23: 'telnet', 25: 'smtp', 53: 'dns',
    67: 'dhcp', 68: 'dhcp', 80: 'http', 110: 'pop3', 123: 'ntp', 143: 'imap',
    161: 'snmp', 389: 'ldap', 443: 'https', 465: 'smtps', 514: 'syslog',
    587: 'submission', 631: 'ipp', 993: 'imaps', 995: 'pop3s', 1080: 'socks',
    2375: 'docker', 2376: 'docker-tls', 2377: 'docker-swarm', 3000: 'http-dev',
    3306: 'mysql', 3389: 'rdp', 4172: 'pulseaudio', 5432: 'postgres',
    5672: 'amqp', 5900: 'vnc', 6379: 'redis', 8080: 'http-alt', 8443: 'https-alt',
    9000: 'http-alt', 9090: 'prometheus', 11211: 'memcached', 27017: 'mongodb'
  }

  let ports: Port[] = []
  let portsLoaded = false
  let portsError = ''

  // Live samples keyed by interface. They only live for this screen's lifetime,
  // which is exactly what a "last few minutes" view needs; the long history
  // lives in the shared telemetry store used by the overview.
  let ifaceSeries: Record<string, { rx: number[]; tx: number[] }> = {}
  let totalRx: number[] = []
  let totalTx: number[] = []
  let lastStamp = ''

  $: hw = device.hardware
  $: ifaces = (hw?.network_ifaces ?? []).slice().sort((a, b) => {
    // Physical interfaces with traffic first, then by name, so the interesting
    // link is not buried under idle tunnels.
    const av = (a.rx_rate || 0) + (a.tx_rate || 0)
    const bv = (b.rx_rate || 0) + (b.tx_rate || 0)
    if (bv !== av) return bv - av
    return a.name.localeCompare(b.name)
  })

  $: totalRxRate = hw?.net_rx_rate ?? ifaces.reduce((n, i) => n + (i.rx_rate || 0), 0)
  $: totalTxRate = hw?.net_tx_rate ?? ifaces.reduce((n, i) => n + (i.tx_rate || 0), 0)
  $: totalErrors = ifaces.reduce((n, i) => n + (i.rx_errors || 0) + (i.tx_errors || 0), 0)
  $: totalDrops = ifaces.reduce((n, i) => n + (i.rx_dropped || 0) + (i.tx_dropped || 0), 0)
  $: upCount = ifaces.filter((i) => (i.oper_state || '') === 'up').length

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

  function addresses(iface: NetworkIface): string[] {
    return (iface.addresses ?? []).filter(Boolean)
  }

  function linkTone(iface: NetworkIface): 'ok' | 'warn' | 'crit' {
    const errors = (iface.rx_errors || 0) + (iface.tx_errors || 0)
    const drops = (iface.rx_dropped || 0) + (iface.tx_dropped || 0)
    if (errors > 0) return 'crit'
    if (drops > 0) return 'warn'
    return 'ok'
  }

  function host(address: string): string {
    return address || '*'
  }

  onMount(loadPorts)

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
</script>

<section class="network">
  {#if !hw}
    <EmptyState icon="🌐" title="No telemetry yet" message="The agent has not reported network counters for this device." />
  {:else}
    <div class="summary">
      <article class="stat">
        <span class="label">Download</span>
        <span class="value">{formatRate(totalRxRate)}</span>
        <MiniSparkline series={totalRx} color="var(--accent)" height={26} label="Download rate" />
      </article>
      <article class="stat">
        <span class="label">Upload</span>
        <span class="value">{formatRate(totalTxRate)}</span>
        <MiniSparkline series={totalTx} color="var(--series-1)" height={26} label="Upload rate" />
      </article>
      <article class="stat">
        <span class="label">Interfaces</span>
        <span class="value">{upCount}<small> / {ifaces.length} up</small></span>
        <span class="sub">{formatBytes(ifaces.reduce((n, i) => n + i.rx_bytes + i.tx_bytes, 0))} total since boot</span>
      </article>
      <article class="stat">
        <span class="label">Errors / drops</span>
        <span class="value" class:text-crit={totalErrors > 0} class:text-warn={totalErrors === 0 && totalDrops > 0}>{totalErrors}<small> err</small></span>
        <span class="sub">{totalDrops} dropped</span>
      </article>
    </div>

    <article class="card">
      <header class="card-head">
        <h2>Interfaces</h2>
        <span class="hint">live throughput per link</span>
      </header>

      {#if ifaces.length === 0}
        <EmptyState icon="🌐" title="No interfaces reported" message="The agent found no real network interface to report." />
      {:else}
        <div class="ifaces">
          {#each ifaces as iface (iface.name)}
            {@const tone = linkTone(iface)}
            <div class="iface">
              <div class="iface-top">
                <div class="iface-id">
                  <span class="dot {tone}"></span>
                  <span class="name mono">{iface.name}</span>
                  <span class="state">{iface.oper_state || 'unknown'}</span>
                </div>
                <div class="iface-links">
                  {#if iface.speed_mbps}<span class="chip">{iface.speed_mbps} Mb/s</span>{/if}
                  {#if iface.mtu}<span class="chip">MTU {iface.mtu}</span>{/if}
                  {#if iface.mac}<span class="chip mono">{iface.mac}</span>{/if}
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
                  <MiniSparkline series={ifaceSeries[iface.name]?.rx || []} color="var(--accent)" height={22} />
                </div>
                <div class="rate">
                  <span class="rate-k">↑ {formatRate(iface.tx_rate || 0)}</span>
                  <MiniSparkline series={ifaceSeries[iface.name]?.tx || []} color="var(--series-1)" height={22} />
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
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </article>

    <article class="card">
      <header class="card-head">
        <h2>Listening sockets</h2>
        <span class="hint">{ports.length} local listeners</span>
      </header>
      {#if portsError}
        <p class="error">{portsError}</p>
      {:else if !portsLoaded}
        <p class="loading">Loading sockets…</p>
      {:else if ports.length === 0}
        <EmptyState icon="🔌" title="No listening sockets reported" message="The agent found no sockets, or ss is not installed." />
      {:else}
        <table class="socks">
          <thead>
            <tr><th>Port</th><th>Proto</th><th>Address</th><th>Likely service</th><th>Process</th></tr>
          </thead>
          <tbody>
            {#each ports as p (portIdentity(p))}
              <tr>
                <td class="mono port">{p.port}</td>
                <td class="mono proto">{p.protocol}</td>
                <td class="mono">{host(p.local_address)}</td>
                <td>{p.process || WELL_KNOWN[p.port] || '—'}</td>
                <td class="mono dim">{p.pid ? `pid ${p.pid}` : '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </article>
  {/if}
</section>

<style>
  .network { display: grid; gap: 0.85rem; }

  .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 0.6rem; }
  .stat {
    display: grid; gap: 0.25rem; align-content: start;
    padding: 0.8rem 0.9rem; background: var(--card);
    border: 1px solid var(--border); border-radius: var(--radius);
  }
  .stat .value { font-size: 1.45rem; }
  .stat .value small { font-size: 0.8rem; color: var(--text-muted); font-weight: 400; }
  .stat .sub { color: var(--text-faint); font-size: 0.72rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .hint { color: var(--text-faint); font-size: 0.7rem; }

  .ifaces { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 0.7rem; padding: 0.8rem; }
  .iface { display: grid; gap: 0.45rem; padding: 0.75rem 0.85rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); }

  .iface-top { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; flex-wrap: wrap; }
  .iface-id { display: flex; align-items: center; gap: 0.45rem; min-width: 0; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--offline); flex-shrink: 0; }
  .dot.ok { background: var(--ok); }
  .dot.warn { background: var(--warn); }
  .dot.crit { background: var(--crit); }
  .name { color: var(--text-bright); font-weight: 600; }
  .state { color: var(--text-faint); font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .iface-links { display: flex; gap: 0.3rem; flex-wrap: wrap; }
  .chip { padding: 0.1rem 0.4rem; border: 1px solid var(--border); border-radius: var(--radius-pill); color: var(--text-muted); font-size: 0.66rem; }

  .addrs { display: flex; flex-wrap: wrap; gap: 0.35rem; }
  .addr { padding: 0.12rem 0.4rem; background: var(--card-hover); border-radius: var(--radius-sm); color: var(--text); font-size: 0.7rem; }

  .rates { display: grid; grid-template-columns: 1fr 1fr; gap: 0.6rem; }
  .rate { display: grid; gap: 0.1rem; }
  .rate-k { color: var(--text-muted); font-size: 0.72rem; font-variant-numeric: tabular-nums; }

  .counters { display: flex; flex-wrap: wrap; gap: 0.6rem; color: var(--text-faint); font-size: 0.68rem; font-variant-numeric: tabular-nums; border-top: 1px solid var(--border); padding-top: 0.4rem; }

  .socks { width: 100%; border-collapse: collapse; font-size: 0.76rem; }
  .socks th { text-align: left; padding: 0.45rem 0.9rem; color: var(--text-faint); font-size: 0.66rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em; border-bottom: 1px solid var(--border); }
  .socks td { padding: 0.42rem 0.9rem; border-bottom: 1px solid var(--border); color: var(--text); }
  .socks tr:last-child td { border-bottom: 0; }
  .port { color: var(--accent); }
  .proto { color: var(--text-muted); text-transform: uppercase; font-size: 0.68rem; }
  .dim { color: var(--text-faint); }
  .error { margin: 0; padding: 0.9rem; color: var(--crit); font-size: 0.76rem; }
  .loading { margin: 0; padding: 0.9rem; color: var(--text-faint); font-size: 0.76rem; }

  @media (max-width: 720px) {
    .rates { grid-template-columns: 1fr; }
  }
</style>
