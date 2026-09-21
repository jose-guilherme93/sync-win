<script lang="ts">
  type HardwareStats = {
    cpu_usage_percent: number
    memory_used_bytes: number
    memory_total_bytes: number
    disk_read_rate: number
    disk_write_rate: number
    uptime_seconds: number
    load_average: string
    power_watts: number
    collected_at: string
    agent_cpu_usage?: number
    agent_memory_bytes?: number
    cpu_temperature?: number
    battery_percent?: number
    battery_status?: string
    network_ifaces?: { name: string; rx_bytes: number; tx_bytes: number; rx_rate?: number; tx_rate?: number; rx_packets: number; tx_packets: number; rx_errors: number; tx_errors: number }[]
    swap_used_bytes?: number
    swap_total_bytes?: number
  }

  type Device = {
    id: string
    hostname: string
    user_id: string
    status: string
    hardware?: HardwareStats
  }

  export let device: Device

  function memoryPercent(h?: HardwareStats) {
    if (!h || !h.memory_total_bytes) return 0
    return (h.memory_used_bytes / h.memory_total_bytes) * 100
  }

  function meterClass(percent: number) {
    if (percent >= 85) return 'crit'
    if (percent >= 60) return 'warn'
    return ''
  }

  function formatBytes(value: number) {
    if (!Number.isFinite(value) || value <= 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
    return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
  }

  function formatRate(value: number) {
    return `${formatBytes(value)}/s`
  }

  function formatDuration(value: number) {
    const days = Math.floor(value / 86400)
    const hours = Math.floor((value % 86400) / 3600)
    const minutes = Math.floor((value % 3600) / 60)
    return days > 0 ? `${days}d ${hours}h` : `${hours}h ${minutes}m`
  }

  // Soma a taxa de download (RX) em tempo real de todas as interfaces (exceto loopback).
  // O agente calcula rx_rate/tx_rate como delta de bytes/s a cada ciclo de telemetria.
  function totalNetRxRate(h?: HardwareStats): number {
    if (!h?.network_ifaces) return 0
    let total = 0
    for (const iface of h.network_ifaces) {
      if (iface.name === 'lo') continue
      total += iface.rx_rate ?? 0
    }
    return total
  }

  // Soma a taxa de upload (TX) em tempo real de todas as interfaces (exceto loopback).
  function totalNetTxRate(h?: HardwareStats): number {
    if (!h?.network_ifaces) return 0
    let total = 0
    for (const iface of h.network_ifaces) {
      if (iface.name === 'lo') continue
      total += iface.tx_rate ?? 0
    }
    return total
  }

  // Mantidas para compatibilidade (bytes totais acumulados), usadas em outros contextos.
  function totalNetRx(h?: HardwareStats) {
    if (!h?.network_ifaces) return 0
    let total = 0
    for (const iface of h.network_ifaces) {
      if (iface.name === 'lo') continue
      total += iface.rx_bytes || 0
    }
    return total
  }

  function totalNetTx(h?: HardwareStats) {
    if (!h?.network_ifaces) return 0
    let total = 0
    for (const iface of h.network_ifaces) {
      if (iface.name === 'lo') continue
      total += iface.tx_bytes || 0
    }
    return total
  }

  function agentImpactClass(h?: HardwareStats) {
    if (!h) return ''
    const cpu = h.agent_cpu_usage || 0
    const ram = h.memory_total_bytes ? ((h.agent_memory_bytes || 0) / h.memory_total_bytes) * 100 : 0
    const max = Math.max(cpu, ram)
    if (max > 5) return 'impact-high'
    if (max > 2) return 'impact-mid'
    return 'impact-low'
  }

  $: hw = device.hardware
  $: cpu = hw?.cpu_usage_percent || 0
  $: memPct = memoryPercent(hw)
</script>

<div class="simple-metrics">
  {#if hw}
    <div class="metric-row">
      <div class="metric-cell">
        <span class="metric-label">CPU</span>
        <strong>{cpu.toFixed(0)}%</strong>
        <div class="mini-bar"><i class={meterClass(cpu)} style="width: {Math.min(cpu, 100)}%"></i></div>
      </div>
      <div class="metric-cell">
        <span class="metric-label">RAM</span>
        <strong>{memPct.toFixed(0)}%</strong>
        <small>{formatBytes(hw.memory_used_bytes || 0)}</small>
        <div class="mini-bar"><i class={meterClass(memPct)} style="width: {Math.min(memPct, 100)}%"></i></div>
      </div>
      <div class="metric-cell">
        <span class="metric-label">Disk I/O</span>
        <strong>&uarr; {formatRate(hw.disk_read_rate || 0)}</strong>
        <small>&darr; {formatRate(hw.disk_write_rate || 0)}</small>
      </div>
      <div class="metric-cell">
        <span class="metric-label">Temp</span>
        <strong>{hw.cpu_temperature ? `${hw.cpu_temperature.toFixed(0)}C` : '—'}</strong>
        <small>{formatDuration(hw.uptime_seconds || 0)}</small>
      </div>
    </div>
    <div class="metric-row">
      <div class="metric-cell">
        <span class="metric-label">Network</span>
        <strong>&darr; {formatRate(totalNetRxRate(hw))}</strong>
        <small>&uarr; {formatRate(totalNetTxRate(hw))}</small>
      </div>
      <div class="metric-cell">
        <span class="metric-label">Battery</span>
        <strong>{hw.battery_percent != null && hw.battery_percent > 0 ? `${hw.battery_percent.toFixed(0)}%` : '—'}</strong>
        <small>{hw.battery_status === 'charging' ? 'charging' : ''}</small>
      </div>
      <div class="metric-cell">
        <span class="metric-label">Power</span>
        <strong>{(hw.power_watts || 0).toFixed(1)} W</strong>
        <small>load {hw.load_average?.split(' ')[0] || 'n/a'}</small>
      </div>
      <div class="metric-cell">
        <span class="metric-label">Agent</span>
        <strong class={agentImpactClass(hw)}>{hw.agent_cpu_usage != null ? `${hw.agent_cpu_usage.toFixed(1)}%` : '—'}</strong>
        <small>{hw.agent_memory_bytes ? formatBytes(hw.agent_memory_bytes) : ''}</small>
      </div>
    </div>
    {#if hw.swap_total_bytes && hw.swap_total_bytes > 0}
      <div class="metric-row single">
        <div class="metric-cell wide">
          <span class="metric-label">Swap</span>
          <strong>{((hw.swap_used_bytes || 0) / hw.swap_total_bytes * 100).toFixed(0)}%</strong>
          <small>{formatBytes(hw.swap_used_bytes || 0)} / {formatBytes(hw.swap_total_bytes)}</small>
          <div class="mini-bar"><i class={meterClass((hw.swap_used_bytes || 0) / hw.swap_total_bytes * 100)} style="width: {Math.min((hw.swap_used_bytes || 0) / hw.swap_total_bytes * 100, 100)}%"></i></div>
        </div>
      </div>
    {/if}
  {:else}
    <p class="no-hw">No hardware data yet</p>
  {/if}
</div>

<style>
  .simple-metrics {
    display: grid;
    gap: 0.35rem;
    padding: 0.5rem 0;
  }

  .metric-row {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 0.3rem;
  }

  .metric-row.single {
    grid-template-columns: 1fr;
  }

  .metric-cell {
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    padding: 0.4rem 0.45rem;
    border: 1px solid rgba(148, 163, 184, 0.12);
    border-radius: 6px;
    background: rgba(15, 23, 42, 0.35);
  }

  .metric-cell.wide {
    max-width: 100%;
  }

  .metric-label {
    color: #94a3b8;
    font-size: 0.6rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    line-height: 1;
  }

  .metric-cell strong {
    color: #f8fafc;
    font-size: 0.82rem;
    line-height: 1.3;
  }

  .metric-cell small {
    color: #94a3b8;
    font-size: 0.58rem;
    line-height: 1.2;
  }

  .mini-bar {
    width: 100%;
    height: 5px;
    margin-top: auto;
    overflow: hidden;
    border-radius: 999px;
    background: rgba(148, 163, 184, 0.15);
  }

  .mini-bar i {
    display: block;
    height: 100%;
    border-radius: inherit;
    background: #2dd4bf;
    transition: width 0.4s ease;
  }

  .mini-bar i.warn {
    background: #fbbf24;
  }

  .mini-bar i.crit {
    background: #f87171;
  }

  .impact-low {
    color: #86efac;
  }

  .impact-mid {
    color: #fbbf24;
  }

  .impact-high {
    color: #f87171;
  }

  .no-hw {
    margin: 0;
    color: #94a3b8;
    font-size: 0.72rem;
    text-align: center;
    padding: 0.4rem;
  }

  @media (max-width: 600px) {
    .metric-row {
      grid-template-columns: repeat(2, 1fr);
    }
  }
</style>
