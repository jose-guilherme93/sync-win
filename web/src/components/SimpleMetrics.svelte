<script lang="ts">
  type Partition = { mount: string; device: string; total_bytes: number; used_bytes: number; free_bytes: number; used_percent: number }
  type Iface = { name: string; rx_bytes: number; tx_bytes: number; rx_rate?: number; tx_rate?: number; rx_packets: number; tx_packets: number; rx_errors: number; tx_errors: number }
  type DockerInfo = { version: string; total: number; running: number; stopped: number; paused: number; images: number; driver: string; ncpu: number }

  type HardwareStats = {
    cpu_usage_percent: number
    cpu_core_usage?: number[]
    cpu_model?: string
    memory_used_bytes: number
    memory_total_bytes: number
    memory_buffers_bytes?: number
    memory_cached_bytes?: number
    swap_used_bytes?: number
    swap_total_bytes?: number
    disk_read_rate: number
    disk_write_rate: number
    disk_partitions?: Partition[]
    uptime_seconds: number
    load_average: string
    power_watts: number
    collected_at: string
    agent_cpu_usage?: number
    agent_memory_bytes?: number
    agent_version?: string
    cpu_temperature?: number
    gpu_temperature_celsius?: number
    battery_percent?: number
    battery_status?: string
    network_ifaces?: Iface[]
    net_rx_rate?: number
    net_tx_rate?: number
    docker_available?: boolean
    docker_info?: DockerInfo
    lynis_available?: boolean
    operating_system?: string
    kernel_version?: string
  }

  export let device: { id: string; hostname: string; hardware?: HardwareStats }

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

  // One shared threshold set so a colour always means the same thing whichever
  // meter is showing it.
  function meterClass(percent: number) {
    if (percent >= 85) return 'crit'
    if (percent >= 60) return 'warn'
    return ''
  }

  function clampPercent(value: number | undefined | null) {
    return Math.max(0, Math.min(value || 0, 100))
  }

  $: hw = device.hardware
  $: cpu = clampPercent(hw?.cpu_usage_percent)
  $: memPct = memPercent(hw)
  $: swapPct = swapPercent(hw)
  $: disks = diskList(hw)
  $: cores = coreList(hw)
  $: ifaces = ifaceList(hw)
  $: netErrors = totalNetErrors(hw)
  $: load = loadParts(hw)
  $: hasBattery = Boolean(hw && hw.battery_percent != null && hw.battery_percent > 0)

  function memPercent(h?: HardwareStats) {
    if (!h?.memory_total_bytes) return 0
    return clampPercent(((h.memory_used_bytes || 0) / h.memory_total_bytes) * 100)
  }

  function swapPercent(h?: HardwareStats) {
    if (!h?.swap_total_bytes) return 0
    return clampPercent(((h.swap_used_bytes || 0) / h.swap_total_bytes) * 100)
  }

  // The agent reports one entry per filesystem, so these are distinct disks and
  // each gets its own horizontal meter.
  function diskList(h?: HardwareStats) {
    if (!h?.disk_partitions?.length) return []
    return h.disk_partitions
      .filter((p) => p.total_bytes > 0)
      .map((p) => ({
        mount: p.mount,
        percent: clampPercent(p.used_percent),
        used: p.used_bytes || 0,
        total: p.total_bytes || 0
      }))
      .sort((a, b) => b.total - a.total)
  }

  function coreList(h?: HardwareStats) {
    if (!h?.cpu_core_usage?.length) return []
    return h.cpu_core_usage.map((value, index) => ({ index, percent: clampPercent(value) }))
  }

  // Only interfaces actually carrying traffic earn a row, otherwise idle
  // adapters bury the signal.
  function ifaceList(h?: HardwareStats) {
    if (!h?.network_ifaces?.length) return []
    return h.network_ifaces
      .filter((i) => (i.rx_rate || 0) > 0 || (i.tx_rate || 0) > 0 || i.rx_errors > 0 || i.tx_errors > 0)
      .sort((a, b) => (b.rx_rate || 0) + (b.tx_rate || 0) - ((a.rx_rate || 0) + (a.tx_rate || 0)))
      .slice(0, 4)
  }

  function totalNetErrors(h?: HardwareStats) {
    if (!h?.network_ifaces) return 0
    return h.network_ifaces.reduce((acc, i) => acc + (i.rx_errors || 0) + (i.tx_errors || 0), 0)
  }

  function loadParts(h?: HardwareStats) {
    const parts = (h?.load_average || '').split(/\s+/).filter(Boolean)
    return { one: parts[0] || '—', five: parts[1] || '—', fifteen: parts[2] || '—' }
  }

  function agentImpactClass(h?: HardwareStats) {
    if (!h) return ''
    const cpuUse = h.agent_cpu_usage || 0
    const ram = h.memory_total_bytes ? ((h.agent_memory_bytes || 0) / h.memory_total_bytes) * 100 : 0
    const worst = Math.max(cpuUse, ram)
    if (worst > 5) return 'impact-high'
    if (worst > 2) return 'impact-mid'
    return 'impact-low'
  }
</script>

<div class="simple-metrics">
  {#if hw}
    <!-- Headline meters. CPU, memory and storage all use the same horizontal
         treatment so a glance compares like with like. -->
    <div class="block">
      <div class="meter">
        <div class="meter-head">
          <span class="meter-label">CPU</span>
          <span class="meter-value">{cpu.toFixed(0)}<small>%</small></span>
        </div>
        <div class="bar"><i class={meterClass(cpu)} style="width: {cpu}%"></i></div>
      </div>

      <div class="meter">
        <div class="meter-head">
          <span class="meter-label">Memory</span>
          <span class="meter-value">{memPct.toFixed(0)}<small>%</small></span>
        </div>
        <div class="bar"><i class="bar-mem {meterClass(memPct)}" style="width: {memPct}%"></i></div>
        <span class="meter-sub">{formatBytes(hw.memory_used_bytes || 0)} of {formatBytes(hw.memory_total_bytes || 0)}</span>
      </div>

      {#if hw.swap_total_bytes && hw.swap_total_bytes > 0}
        <div class="meter">
          <div class="meter-head">
            <span class="meter-label">Swap</span>
            <span class="meter-value">{swapPct.toFixed(0)}<small>%</small></span>
          </div>
          <div class="bar"><i class="bar-swap {meterClass(swapPct)}" style="width: {swapPct}%"></i></div>
          <span class="meter-sub">{formatBytes(hw.swap_used_bytes || 0)} of {formatBytes(hw.swap_total_bytes)}</span>
        </div>
      {/if}
    </div>

    {#if disks.length > 0}
      <div class="block">
        <span class="section-label">Storage</span>
        {#each disks as disk (disk.mount)}
          <div class="meter">
            <div class="meter-head">
              <span class="meter-label disk-mount">{disk.mount}</span>
              <span class="meter-value">{disk.percent.toFixed(0)}<small>%</small></span>
            </div>
            <div class="bar"><i class="bar-disk {meterClass(disk.percent)}" style="width: {disk.percent}%"></i></div>
            <span class="meter-sub">{formatBytes(disk.used)} of {formatBytes(disk.total)}</span>
          </div>
        {/each}
      </div>
    {/if}

    {#if cores.length > 1}
      <div class="block">
        <span class="section-label">CPU per core · {cores.length} cores</span>
        <div class="core-bars" role="img" aria-label="CPU usage per core">
          {#each cores as core (core.index)}
            <div
              class="core-bar"
              class:hot={core.percent >= 85}
              class:warm={core.percent >= 60 && core.percent < 85}
              style="height: {Math.max(core.percent, 4)}%"
              title="Core {core.index}: {core.percent.toFixed(0)}%"
            ></div>
          {/each}
        </div>
      </div>
    {/if}

    <div class="block">
      <span class="section-label">Throughput</span>
      <div class="facts">
        <div class="fact">
          <span class="fact-label">Net down</span>
          <strong class="down">{formatRate(hw.net_rx_rate || 0)}</strong>
        </div>
        <div class="fact">
          <span class="fact-label">Net up</span>
          <strong class="up">{formatRate(hw.net_tx_rate || 0)}</strong>
        </div>
        <div class="fact">
          <span class="fact-label">Disk read</span>
          <strong>{formatRate(hw.disk_read_rate || 0)}</strong>
        </div>
        <div class="fact">
          <span class="fact-label">Disk write</span>
          <strong>{formatRate(hw.disk_write_rate || 0)}</strong>
        </div>
      </div>
    </div>

    <div class="block">
      <span class="section-label">System</span>
      <div class="facts">
        <div class="fact">
          <span class="fact-label">Uptime</span>
          <strong>{formatDuration(hw.uptime_seconds || 0)}</strong>
        </div>
        <div class="fact">
          <span class="fact-label">Load 1/5/15</span>
          <strong>{load.one} {load.five} {load.fifteen}</strong>
        </div>
        <div class="fact">
          <span class="fact-label">Power</span>
          <strong>{(hw.power_watts || 0).toFixed(1)} W</strong>
        </div>
        <div class="fact">
          <span class="fact-label">CPU temp</span>
          <strong class:hot-text={Boolean(hw.cpu_temperature && hw.cpu_temperature >= 85)} class:warm-text={Boolean(hw.cpu_temperature && hw.cpu_temperature >= 70 && hw.cpu_temperature < 85)}>
            {hw.cpu_temperature ? `${hw.cpu_temperature.toFixed(0)}°C` : '—'}
          </strong>
        </div>
        {#if hw.gpu_temperature_celsius}
          <div class="fact">
            <span class="fact-label">GPU temp</span>
            <strong>{hw.gpu_temperature_celsius.toFixed(0)}°C</strong>
          </div>
        {/if}
        {#if hasBattery}
          <div class="fact">
            <span class="fact-label">Battery</span>
            <strong class:hot-text={(hw.battery_percent || 0) < 15 && hw.battery_status !== 'charging'}>
              {(hw.battery_percent || 0).toFixed(0)}%{hw.battery_status === 'charging' ? ' ⚡' : ''}
            </strong>
          </div>
        {/if}
        <div class="fact">
          <span class="fact-label">Kernel</span>
          <strong class="truncate" title={hw.kernel_version || ''}>{hw.kernel_version || '—'}</strong>
        </div>
        <div class="fact">
          <span class="fact-label">Agent</span>
          <strong class={agentImpactClass(hw)}>
            {hw.agent_cpu_usage != null ? `${hw.agent_cpu_usage.toFixed(1)}%` : '—'}
            {#if hw.agent_memory_bytes}<span class="fact-sub">{formatBytes(hw.agent_memory_bytes)}</span>{/if}
          </strong>
        </div>
      </div>
    </div>

    {#if ifaces.length > 0 || netErrors > 0}
      <div class="block">
        <span class="section-label">Interfaces</span>
        {#each ifaces as iface (iface.name)}
          <div class="iface">
            <span class="iface-name">{iface.name}</span>
            <span class="iface-rates">
              <span class="down">↓ {formatRate(iface.rx_rate || 0)}</span>
              <span class="up">↑ {formatRate(iface.tx_rate || 0)}</span>
              {#if iface.rx_errors > 0 || iface.tx_errors > 0}
                <span class="iface-errors" title="{iface.rx_errors} rx / {iface.tx_errors} tx errors">{iface.rx_errors + iface.tx_errors} err</span>
              {/if}
            </span>
          </div>
        {/each}
        {#if netErrors > 0 && ifaces.length === 0}
          <span class="iface-errors">{netErrors} network errors</span>
        {/if}
      </div>
    {/if}

    {#if hw.docker_available}
      <div class="block">
        <span class="section-label">Docker</span>
        <div class="facts">
          <div class="fact">
            <span class="fact-label">Containers</span>
            <strong>{hw.docker_info ? `${hw.docker_info.running}/${hw.docker_info.total}` : 'available'}</strong>
          </div>
          {#if hw.docker_info}
            <div class="fact">
              <span class="fact-label">Images</span>
              <strong>{hw.docker_info.images}</strong>
            </div>
            <div class="fact">
              <span class="fact-label">Engine</span>
              <strong class="truncate" title={hw.docker_info.driver}>{hw.docker_info.version}</strong>
            </div>
          {/if}
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
    gap: 0.7rem;
    padding: 0.4rem 0 0.25rem;
  }

  .block {
    display: grid;
    gap: 0.4rem;
  }

  .section-label {
    color: #94a3b8;
    font-size: 0.72rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  /* Headline meters: label and value on one line, thick bar underneath. */
  .meter {
    display: grid;
    gap: 0.25rem;
  }

  .meter-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.5rem;
  }

  .meter-label {
    color: #cbd5e1;
    font-size: 0.86rem;
    font-weight: 600;
  }

  .meter-value {
    color: #f8fafc;
    font-size: 1.02rem;
    font-variant-numeric: tabular-nums;
    font-weight: 600;
  }

  .meter-value small {
    color: #64748b;
    font-size: 0.72rem;
    font-weight: 500;
  }

  .meter-sub {
    color: #94a3b8;
    font-size: 0.74rem;
    font-variant-numeric: tabular-nums;
  }

  .disk-mount {
    color: #e2e8f0;
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 0.8rem;
  }

  .bar {
    height: 12px;
    overflow: hidden;
    border-radius: 6px;
    background: rgba(148, 163, 184, 0.18);
  }

  .bar i {
    display: block;
    height: 100%;
    border-radius: inherit;
    background: #2dd4bf;
    transition: width 0.4s ease;
  }

  .bar i.warn,
  .core-bar.warm {
    background: #fbbf24;
  }

  .bar i.crit,
  .core-bar.hot {
    background: #f87171;
  }

  .bar-mem {
    background: #3b82f6;
  }

  .bar-disk {
    background: #a78bfa;
  }

  .bar-swap {
    background: #94a3b8;
  }

  /* One column per logical core reads better than N horizontal bars once the
     machine has many cores. */
  .core-bars {
    display: flex;
    align-items: flex-end;
    gap: 3px;
    height: 44px;
    padding: 4px 0;
    border-radius: 6px;
    background: rgba(148, 163, 184, 0.1);
  }

  .core-bar {
    flex: 1 1 0;
    min-width: 3px;
    max-width: 14px;
    border-radius: 2px;
    background: #2dd4bf;
    transition: height 0.4s ease;
  }

  .facts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(104px, 1fr));
    gap: 0.45rem 0.7rem;
  }

  .fact {
    display: flex;
    min-width: 0;
    flex-direction: column;
    gap: 0.1rem;
  }

  .fact-label {
    color: #94a3b8;
    font-size: 0.7rem;
  }

  .fact strong {
    display: flex;
    align-items: baseline;
    gap: 0.3rem;
    color: #f1f5f9;
    font-size: 0.9rem;
    font-variant-numeric: tabular-nums;
    font-weight: 600;
  }

  .fact strong.down {
    color: #2dd4bf;
  }

  .fact strong.up {
    color: #a78bfa;
  }

  .fact-sub {
    color: #94a3b8;
    font-size: 0.68rem;
    font-weight: 400;
  }

  .truncate {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .hot-text {
    color: #f87171 !important;
  }

  .warm-text {
    color: #fbbf24 !important;
  }

  .iface {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.5rem;
  }

  .iface-name {
    color: #e2e8f0;
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 0.78rem;
  }

  .iface-rates {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    font-size: 0.74rem;
    font-variant-numeric: tabular-nums;
  }

  .iface-rates .down {
    color: #2dd4bf;
  }

  .iface-rates .up {
    color: #a78bfa;
  }

  .iface-errors {
    color: #f87171;
    font-size: 0.7rem;
  }

  .impact-low {
    color: #86efac !important;
  }

  .impact-mid {
    color: #fbbf24 !important;
  }

  .impact-high {
    color: #f87171 !important;
  }

  .no-hw {
    margin: 0;
    color: #94a3b8;
    font-size: 0.86rem;
    text-align: center;
    padding: 0.7rem;
  }
</style>
