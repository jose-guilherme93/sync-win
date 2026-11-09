<script lang="ts">
  import { onMount } from 'svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import { formatBytes, formatRate } from '../../lib/format'
  import type { Device, DiskDevice, DiskPartition, SmartTest, StorageDailyPoint } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'

  export let device: Device

  type MountRow = DiskPartition & {
    perDay: number | null
    etaDays: number | null
    severity: 'crit' | 'warn' | 'ok'
    reason: string
    detail: string
  }

  const MAX_SAMPLES = 60
  const RANGES = [7, 30, 90]

  let rangeDays = 30
  let history: Record<string, StorageDailyPoint[]> = {}
  let historyError = ''
  let historyLoadedFor = ''

  let readSeries: number[] = []
  let writeSeries: number[] = []
  let lastStamp = ''
  let seriesDeviceId = ''

  // The shell reuses this component when another device is selected. Without
  // this reset the sparklines would mix samples from two devices.
  function resetSeries(id: string) {
    if (!id || id === seriesDeviceId) return
    seriesDeviceId = id
    readSeries = []
    writeSeries = []
    lastStamp = ''
    history = {}
    historyLoadedFor = ''
  }
  $: resetSeries(device.id)

  $: hw = device.hardware
  $: partitions = (hw?.disk_partitions ?? [])
    .filter((p) => p.total_bytes > 0)
    .sort((a, b) => b.total_bytes - a.total_bytes)
  $: diskDevices = hw?.disk_devices ?? []
  $: totalBytes = partitions.reduce((n, p) => n + p.total_bytes, 0)
  $: usedBytes = partitions.reduce((n, p) => n + p.used_bytes, 0)
  $: freeBytes = partitions.reduce((n, p) => n + p.free_bytes, 0)
  $: usedPercent = totalBytes > 0 ? (usedBytes / totalBytes) * 100 : 0
  $: rows = partitions.map((partition) => toRow(partition, history))
  $: findings = rows.filter((row) => row.severity !== 'ok')
  $: growth7 = growthOver(7)
  $: smartTests = collectTests(diskDevices)
  $: readOnly = partitions.filter((p) => p.read_only)
  $: capacityLabel = totalBytes > 0 ? formatBytes(totalBytes) : 'unknown'

  $: if (hw?.collected_at && hw.collected_at !== lastStamp) {
    lastStamp = hw.collected_at
    readSeries = push(readSeries, hw.disk_read_rate || 0)
    writeSeries = push(writeSeries, hw.disk_write_rate || 0)
  }

  $: if (device.id && device.id !== historyLoadedFor) {
    historyLoadedFor = device.id
    void loadHistory()
  }

  function push(series: number[], value: number): number[] {
    const out = [...series, value]
    return out.length > MAX_SAMPLES ? out.slice(out.length - MAX_SAMPLES) : out
  }

  function toRow(partition: DiskPartition, historyMap: Record<string, StorageDailyPoint[]>): MountRow {
    const points = historyMap[partition.mount] ?? []
    const perDay = growthPerDay(points)
    const free = partition.free_bytes
    const etaDays = perDay && perDay > 0 ? Math.floor(free / perDay) : null
    const inodePct = partition.inodes_used_percent ?? 0
    let severity: MountRow['severity'] = 'ok'
    let reason = ''
    if (partition.used_percent >= 90 || inodePct >= 90 || (etaDays != null && etaDays <= 21)) {
      severity = 'crit'
    } else if (partition.used_percent >= 80 || inodePct >= 75 || (etaDays != null && etaDays <= 90)) {
      severity = 'warn'
    }
    if (partition.read_only) {
      severity = 'crit'
      reason = 'mounted read-only'
    } else if (inodePct >= 75 && partition.used_percent < 75) {
      reason = `inodes at ${inodePct.toFixed(0)}% while blocks still fit`
    } else if (etaDays != null && etaDays <= 90) {
      reason = `full in ~${etaDays}d at the current rate`
    } else if (partition.used_percent >= 80) {
      reason = `${partition.used_percent.toFixed(0)}% used`
    }
    const parts = [reason || `${formatBytes(free)} free`]
    if (partition.inodes_total) parts.push(`inodes ${inodePct.toFixed(0)}%`)
    return { ...partition, perDay, etaDays, severity, reason, detail: parts.join(' · ') }
  }

  function growthPerDay(points: StorageDailyPoint[]): number | null {
    if (points.length < 2) return null
    const first = points[0]
    const last = points[points.length - 1]
    const days = Math.max(1, daysBetween(first.day, last.day))
    return (last.used_bytes - first.used_bytes) / days
  }

  function daysBetween(from: string, to: string): number {
    const a = Date.parse(from)
    const b = Date.parse(to)
    if (!Number.isFinite(a) || !Number.isFinite(b)) return 1
    return Math.round((b - a) / 86_400_000)
  }

  function growthOver(days: number): number | null {
    let total = 0
    let any = false
    for (const points of Object.values(history)) {
      const tail = points.slice(-days)
      if (tail.length < 2) continue
      total += tail[tail.length - 1].used_bytes - tail[0].used_bytes
      any = true
    }
    return any ? total : null
  }

  function collectTests(devices: DiskDevice[]): (SmartTest & { device: string })[] {
    const out: (SmartTest & { device: string })[] = []
    for (const disk of devices) {
      for (const test of disk.smart?.tests ?? []) {
        out.push({ ...test, device: disk.name })
      }
    }
    return out.slice(0, 12)
  }

  function smartState(disk: DiskDevice): { label: string; ok: boolean; detail: string } {
    const smart = disk.smart
    if (!smart) return { label: 'not reported', ok: false, detail: 'the agent did not send SMART for this disk' }
    if (!smart.available) {
      return { label: 'unavailable', ok: false, detail: smart.reason || 'smartctl could not read this device' }
    }
    if (!smart.passed) return { label: 'failed', ok: false, detail: 'the drive reported a failing health check' }
    if ((smart.pending ?? 0) > 0) return { label: 'pending sectors', ok: false, detail: 'sectors waiting to be remapped' }
    if ((smart.crc_errors ?? 0) > 0) return { label: 'link errors', ok: false, detail: 'UDMA CRC errors suggest cable or controller' }
    return { label: 'passed', ok: true, detail: 'the drive reported a passing health check' }
  }

  function diskKind(disk: DiskDevice): string {
    if (disk.removable) return 'USB'
    return disk.rotational ? 'HDD' : 'SSD'
  }

  function inodeText(partition: DiskPartition): string {
    if (!partition.inodes_total) return '—'
    return `${(partition.inodes_used_percent ?? 0).toFixed(0)}%`
  }

  function barClass(crit: boolean): string {
    return crit ? 'fill hi' : 'fill'
  }

  // Two points are enough to draw a slope; one says "history starts today".
  function historyPoints(mount: string): StorageDailyPoint[] {
    return history[mount] ?? []
  }

  function historyPolyline(points: StorageDailyPoint[], total: number, width = 320, height = 96): string {
    if (points.length < 2) return ''
    return points
      .map((point, i) => {
        const x = total <= 1 ? 0 : (i / (total - 1)) * width
        const percent = point.total_bytes > 0 ? (point.used_bytes / point.total_bytes) * 100 : 0
        const y = height - (Math.min(100, percent) / 100) * height
        return `${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
  }

  function shade(index: number): string {
    const palette = ['#e7e9ee', '#b6bac4', '#9398a6', '#6b7280', '#4b4f57', '#33363c']
    return palette[index % palette.length]
  }

  async function loadHistory() {
    const id = device.id
    historyError = ''
    try {
      const res = await apiFetch(apiURL(`/api/devices/${id}/storage/history?days=${rangeDays}`))
      if (!res.ok) throw new Error(`history request failed (${res.status})`)
      const payload = await res.json()
      history = payload?.mounts ?? {}
    } catch (e) {
      historyError = e instanceof Error ? e.message : 'Could not load the storage history'
    }
  }

  function setRange(days: number) {
    if (rangeDays === days) return
    rangeDays = days
    void loadHistory()
  }

  function seriesPoints(values: number[], width: number, height: number): string {
    if (values.length < 2) return ''
    const max = Math.max(...values, 1)
    return values
      .map((value, index) => {
        const x = (index / (values.length - 1)) * width
        const y = height - (value / max) * height
        return `${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
  }

  function maxOf(values: number[]): number {
    return values.length === 0 ? 0 : Math.max(...values)
  }

  onMount(() => {
    void loadHistory()
  })
</script>

<section class="storage">
  {#if !hw}
    <EmptyState icon="disk" title="No telemetry yet" message="The agent has not reported disk metrics for this device." />
  {:else}
    <header class="page-head">
      <div>
        <p class="eyebrow">Hardware</p>
        <h1>Storage</h1>
        <p class="muted">
          Capacity, growth, inode and device health for every filesystem this machine has mounted.
          The daily history keeps {rangeDays} days; raw samples stay two hours.
        </p>
      </div>
      <div class="range" role="group" aria-label="History window">
        {#each RANGES as days (days)}
          <button class:active={rangeDays === days} on:click={() => setRange(days)}>{days}d</button>
        {/each}
      </div>
    </header>

    <div class="verdict">
      <div class="vbox">
        <span class="k">Capacity</span>
        <span class="v">{capacityLabel}</span>
        <span class="s">{formatBytes(usedBytes)} used · {formatBytes(freeBytes)} free</span>
      </div>
      <div class="vbox">
        <span class="k">Filesystems</span>
        <span class="v">{partitions.length}</span>
        <span class="s">{findings.length} need attention</span>
      </div>
      <div class="vbox">
        <span class="k">Growth · 7d</span>
        <span class="v">{growth7 == null ? '—' : `+${formatBytes(growth7)}`}</span>
        <span class="s">{growth7 == null ? 'needs more than a day of history' : 'summed over the mounted filesystems'}</span>
      </div>
      <div class="vbox">
        <span class="k">I/O now</span>
        <span class="v">{formatRate(hw.disk_read_rate || 0)} / {formatRate(hw.disk_write_rate || 0)}</span>
        <span class="s">read / write</span>
      </div>
      <div class="vbox">
        <span class="k">Disks</span>
        <span class="v">{diskDevices.length || '—'}</span>
        <span class="s">{diskDevices.length ? `${diskDevices.filter((d) => d.smart?.available).length} with SMART` : 'not reported by this agent'}</span>
      </div>
    </div>

    {#if historyError}
      <p class="warn-line" role="status">{historyError}</p>
    {/if}

    <h2>Precisa de atenção</h2>
    {#if findings.length === 0}
      <p class="muted">Nothing above the thresholds: no filesystem past 80% or with inodes past 75%, and no growth that projects a full disk.</p>
    {:else}
      <div class="findings">
        {#each findings as row (row.mount)}
          <article class="finding" class:hi={row.severity === 'crit'}>
            <div>
              <div class="t">{row.mount} · {row.used_percent.toFixed(0)}% used</div>
              <div class="d">{row.detail}</div>
            </div>
            <div class="a">
              <span class="flag" class:hi={row.severity === 'crit'}>{row.severity === 'crit' ? 'critical' : 'warning'}</span>
              {#if row.etaDays != null}<br>~{row.etaDays}d{/if}
            </div>
          </article>
        {/each}
      </div>
    {/if}

    <h2>Filesystems</h2>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Mount</th>
            <th>Device</th>
            <th>FS</th>
            <th>Size</th>
            <th>Used</th>
            <th class="num">%</th>
            <th class="num">Free / avail</th>
            <th class="num">Reserved</th>
            <th class="num">Inodes</th>
            <th>Options</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as row (row.mount)}
            <tr class:att={row.severity !== 'ok'}>
              <td class="strong">{row.mount}</td>
              <td class="mono faint">{row.device}</td>
              <td class="faint">{row.fstype || '—'}</td>
              <td class="mono">{formatBytes(row.total_bytes)}</td>
              <td>
                <div class="track">
                  <div class={barClass(row.severity === 'crit')} style={`width:${Math.min(100, row.used_percent)}%`}></div>
                </div>
              </td>
              <td class="num mono">{row.used_percent.toFixed(0)}%</td>
              <td class="num mono">{formatBytes(row.free_bytes)}{#if row.avail_bytes != null && row.avail_bytes !== row.free_bytes} / {formatBytes(row.avail_bytes)}{/if}</td>
              <td class="num mono faint">{row.reserved_bytes ? formatBytes(row.reserved_bytes) : '—'}</td>
              <td class="num mono" class:strong={row.severity !== 'ok' && (row.inodes_total ?? 0) > 0}>{inodeText(row)}</td>
              <td class="mono faint options">{row.options || '—'}{#if row.read_only} <span class="flag hi">ro</span>{/if}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <div class="grid2">
      <div class="card">
        <h3>Used % by filesystem · {rangeDays}d</h3>
        {#if Object.values(history).every((points) => points.length < 2)}
          <p class="muted small">No daily history yet. The server records one row per mount per day from the first telemetry of the day, so this chart fills in from tomorrow.</p>
        {:else}
          {@const longest = Math.max(...Object.values(history).map((points) => points.length))}
          <svg width="100%" height="104" viewBox="0 0 320 104" preserveAspectRatio="none" aria-label="Used percent per filesystem">
            <line x1="0" y1="4" x2="320" y2="4" stroke="var(--border)" />
            <line x1="0" y1="52" x2="320" y2="52" stroke="var(--border)" />
            <line x1="0" y1="100" x2="320" y2="100" stroke="var(--border)" />
            {#each Object.entries(history) as [mount, points], index (mount)}
              {#if points.length > 1}
                <polyline points={historyPolyline(points, longest)} fill="none" stroke={shade(index)} stroke-width="1.3" />
              {/if}
            {/each}
          </svg>
          <div class="legend">
            {#each Object.entries(history) as [mount, points], index (mount)}
              <span><i style={`background:${shade(index)}`}></i>{mount} {points.length > 0 ? ((points[points.length - 1].used_bytes / Math.max(1, points[points.length - 1].total_bytes)) * 100).toFixed(0) : '—'}%</span>
            {/each}
          </div>
        {/if}
      </div>

      <div class="card">
        <h3>Growth and forecast</h3>
        <table>
          <thead><tr><th>Mount</th><th class="num">per day</th><th class="num">free</th><th>ETA</th></tr></thead>
          <tbody>
            {#each rows as row (row.mount)}
              <tr>
                <td class="strong">{row.mount}</td>
                <td class="num mono">{row.perDay == null ? '—' : formatBytes(row.perDay)}</td>
                <td class="num mono">{formatBytes(row.free_bytes)}</td>
                <td class="mono">{row.etaDays == null ? (row.perDay != null && row.perDay <= 0 ? 'stable' : '—') : `${row.etaDays}d`}</td>
              </tr>
            {/each}
          </tbody>
        </table>
        <p class="note">A linear fit over the stored days. A mount that stopped growing shows "stable" instead of a date.</p>
      </div>
    </div>

    <div class="grid2">
      <div class="card">
        <h3>Throughput · this session · read / write</h3>
        <svg width="100%" height="72" viewBox="0 0 320 72" preserveAspectRatio="none" aria-label="Disk throughput">
          <line x1="0" y1="70" x2="320" y2="70" stroke="var(--border)" />
          <polyline points={seriesPoints(writeSeries, 320, 64)} fill="none" stroke="var(--text-bright)" stroke-width="1.3" />
          <polyline points={seriesPoints(readSeries, 320, 64)} fill="none" stroke="var(--text-faint)" stroke-width="1.3" />
        </svg>
        <div class="legend">
          <span><i class="bright"></i>write {formatRate(maxOf(writeSeries))} peak</span>
          <span><i class="dim"></i>read {formatRate(maxOf(readSeries))} peak</span>
        </div>
      </div>

      <div class="card">
        <h3>Latency and saturation · per disk</h3>
        {#if diskDevices.length === 0}
          <p class="muted small">This agent does not report per-disk I/O. Update it to receive await and utilization.</p>
        {:else}
          <table>
            <thead><tr><th>Disk</th><th class="num">IOPS</th><th class="num">await</th><th class="num">busy</th></tr></thead>
            <tbody>
              {#each diskDevices as disk (disk.name)}
                <tr>
                  <td class="mono strong">{disk.name}</td>
                  <td class="num mono">{(disk.iops ?? 0).toFixed(0)}</td>
                  <td class="num mono">{(disk.await_ms ?? 0).toFixed(1)} ms</td>
                  <td class="num mono">{(disk.util_percent ?? 0).toFixed(0)}%</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
        <p class="note">Measured between two telemetry samples, not since boot.</p>
      </div>
    </div>

    <h2>Discos físicos</h2>
    {#if diskDevices.length === 0}
      <p class="muted">This agent reports filesystems but not block devices. A new agent adds model, rotation, temperature and SMART.</p>
    {:else}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Device</th><th>Model</th><th>Type</th><th class="num">Size</th>
              <th class="num">Temp</th><th class="num">Power-on</th><th class="num">Written</th>
              <th class="num">Wear</th><th class="num">Realloc</th><th class="num">Pending</th><th class="num">CRC</th>
              <th>SMART</th>
            </tr>
          </thead>
          <tbody>
            {#each diskDevices as disk (disk.name)}
              {@const state = smartState(disk)}
              <tr class:att={!state.ok}>
                <td class="mono strong">{disk.name}</td>
                <td>{disk.model || '—'}</td>
                <td class="faint">{diskKind(disk)}</td>
                <td class="num mono">{formatBytes(disk.size_bytes)}</td>
                <td class="num mono">{disk.temperature_c ? `${disk.temperature_c.toFixed(0)} °C` : '—'}</td>
                <td class="num mono">{disk.smart?.power_on_hours ? `${disk.smart.power_on_hours} h` : '—'}</td>
                <td class="num mono">{disk.smart?.data_written_bytes ? formatBytes(disk.smart.data_written_bytes) : '—'}</td>
                <td class="num mono">{disk.smart?.wear_percent != null ? `${disk.smart.wear_percent}%` : '—'}</td>
                <td class="num mono">{disk.smart?.reallocated ?? '—'}</td>
                <td class="num mono">{disk.smart?.pending ?? '—'}</td>
                <td class="num mono">{disk.smart?.crc_errors ?? '—'}</td>
                <td title={state.detail}><span class="flag" class:hi={!state.ok}>{state.label}</span></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    {#if smartTests.length > 0}
      <div class="card">
        <h3>SMART self-tests · newest first</h3>
        <div class="tests">
          {#each smartTests as test, index (`${test.device}-${index}`)}
            <div class="test-row">
              <span class="mono strong">{test.device}</span>
              <span>{test.type || 'test'}</span>
              <span class="mono faint">{test.hours ? `at ${test.hours} h power-on` : '—'}</span>
              <span class="mono">{test.result || '—'}</span>
            </div>
          {/each}
        </div>
        <p class="note">SMART stores the power-on hour of a test, not a calendar date. Running a test needs smartctl with root.</p>
      </div>
    {/if}

    <h2>Estado dos mounts</h2>
    <div class="card">
      <dl class="mounts">
        <div><dt>Read-only mounts</dt><dd>{readOnly.length === 0 ? 'none' : readOnly.map((p) => p.mount).join(', ')}</dd></div>
        <div><dt>Swap</dt><dd>{hw.swap_total_bytes ? `${formatBytes(hw.swap_used_bytes || 0)} of ${formatBytes(hw.swap_total_bytes)}` : 'not configured'}</dd></div>
        <div><dt>Without noatime</dt><dd>{partitions.filter((p) => !(p.options || '').includes('noatime')).length} of {partitions.length}</dd></div>
        <div><dt>Inodes</dt><dd>{partitions.filter((p) => p.inodes_total).length} filesystems report them</dd></div>
      </dl>
    </div>

    <p class="note">
      Snapshot per mount: size, used, free vs available, reserved blocks, inodes and mount options.
      Daily history: one row per mount per day, kept 400 days. Not collected yet: LVM thin pools,
      Btrfs/ZFS scrub, deleted-but-open files and docker system df — each needs a privileged or
      additional read that this agent does not do.
    </p>
  {/if}
</section>

<style>
  .storage {
    display: grid;
    gap: 0.9rem;
  }

  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
  }

  .eyebrow {
    margin: 0;
    color: var(--text-faint);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
  }

  h1 {
    margin: 0.1rem 0 0.25rem;
    font-size: 1.25rem;
    color: var(--text-bright);
  }

  h2 {
    margin: 0.2rem 0 0;
    padding-bottom: 0.35rem;
    border-bottom: 1px solid var(--border);
    color: var(--text-faint);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.1em;
    text-transform: uppercase;
  }

  .muted {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.82rem;
    max-width: 70ch;
  }

  .small {
    font-size: 0.75rem;
  }

  .range {
    display: flex;
    flex-shrink: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }

  .range button {
    border: 0;
    border-left: 1px solid var(--border);
    background: transparent;
    color: var(--text-faint);
    font: inherit;
    font-size: 0.7rem;
    padding: 0.3rem 0.55rem;
    cursor: pointer;
  }

  .range button:first-child {
    border-left: 0;
  }

  .range button.active {
    background: var(--card-hover);
    color: var(--text-bright);
  }

  .verdict {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: 0.55rem;
  }

  .vbox {
    display: grid;
    gap: 0.15rem;
    padding: 0.55rem 0.7rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .vbox .k {
    color: var(--text-faint);
    font-size: 8px;
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
  }

  .vbox .v {
    color: var(--text-bright);
    font-size: 0.95rem;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }

  .vbox .s {
    color: var(--text-faint);
    font-size: 0.68rem;
  }

  .findings {
    display: grid;
    gap: 0.4rem;
  }

  .finding {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 0.3rem 0.8rem;
    align-items: center;
    padding: 0.5rem 0.7rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .finding.hi {
    border-color: var(--border-strong);
  }

  .finding .t {
    color: var(--text-bright);
    font-weight: 600;
    font-size: 0.8rem;
  }

  .finding .d {
    color: var(--text-muted);
    font-size: 0.72rem;
  }

  .finding .a {
    color: var(--text-faint);
    font-size: 0.72rem;
    text-align: right;
    white-space: nowrap;
  }

  .flag {
    display: inline-block;
    padding: 0 0.3rem;
    border: 1px solid var(--border-strong);
    border-radius: 3px;
    color: var(--text);
    font-size: 0.6rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .flag.hi {
    border-color: var(--text);
    background: var(--text);
    color: var(--bg);
    font-weight: 700;
  }

  .table-wrap {
    overflow-x: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card);
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.73rem;
  }

  th {
    text-align: left;
    padding: 0.35rem 0.5rem;
    border-bottom: 1px solid var(--border);
    color: var(--text-faint);
    font-size: 8px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    white-space: nowrap;
  }

  td {
    padding: 0.35rem 0.5rem;
    border-top: 1px solid var(--border);
    white-space: nowrap;
    vertical-align: middle;
  }

  tbody tr:first-child td {
    border-top: 0;
  }

  tr.att td {
    background: var(--card-hover);
  }

  tr.att td:first-child {
    box-shadow: inset 2px 0 0 var(--text-bright);
  }

  .num {
    text-align: right;
  }

  .strong {
    color: var(--text-bright);
    font-weight: 600;
  }

  .mono {
    font-family: var(--mono);
    font-variant-numeric: tabular-nums;
  }

  .faint {
    color: var(--text-faint);
  }

  .options {
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .track {
    width: 100%;
    min-width: 64px;
    height: 4px;
    border-radius: 2px;
    background: var(--border);
    overflow: hidden;
  }

  .fill {
    height: 100%;
    min-width: 2px;
    border-radius: 2px;
    background: var(--text-muted);
  }

  .fill.hi {
    background: var(--text-bright);
  }

  .grid2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.6rem;
  }

  .card {
    display: grid;
    gap: 0.4rem;
    padding: 0.7rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .card h3 {
    margin: 0;
    color: var(--text-faint);
    font-size: 8px;
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 0.6rem;
    color: var(--text-faint);
    font-size: 0.66rem;
  }

  .legend i {
    display: inline-block;
    width: 9px;
    height: 2px;
    margin-right: 4px;
    vertical-align: middle;
  }

  .legend i.bright {
    background: var(--text-bright);
  }

  .legend i.dim {
    background: var(--text-faint);
  }

  .note {
    margin: 0;
    color: var(--text-faint);
    font-size: 0.7rem;
    line-height: 1.5;
  }

  .warn-line {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.75rem;
  }

  .tests {
    display: grid;
  }

  .test-row {
    display: grid;
    grid-template-columns: 90px 1fr 160px 1fr;
    gap: 0.6rem;
    padding: 0.3rem 0;
    border-top: 1px solid var(--border);
    font-size: 0.72rem;
    color: var(--text-muted);
  }

  .test-row:first-child {
    border-top: 0;
  }

  .mounts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
    gap: 0.5rem 1rem;
    margin: 0;
  }

  .mounts dt {
    color: var(--text-faint);
    font-size: 8px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .mounts dd {
    margin: 0.1rem 0 0;
    color: var(--text);
    font-size: 0.75rem;
  }

  @media (max-width: 860px) {
    .grid2 {
      grid-template-columns: 1fr;
    }
  }
</style>
